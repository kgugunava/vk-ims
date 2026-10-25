package ghapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/kgugunava/vk-ims/service/internal/trace"
)

const (
	requestTimeout = 30 * time.Second
	maxBodyBytes   = 64 << 20
)

// Page is one page of the issues listing.
type Page struct {
	Items   []json.RawMessage
	NextURL string // the Link rel="next" target; empty on the last page
}

// Pauses before the repeats of a request that failed on the network or with
// a 5xx status: at most three repeats.
var retryDelays = []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second}

// Config holds what a Client needs to reach the API.
type Config struct {
	BaseURL string        // without a trailing slash
	Token   string        // empty for unauthenticated requests
	Trace   *trace.Writer // nil when --trace is not given
	Logger  *slog.Logger  // nil means slog.Default()
}

// Client makes the requests of one run, strictly one at a time.
type Client struct {
	http    *http.Client
	baseURL string
	token   string
	trace   *trace.Writer
	logger  *slog.Logger
	// now and sleep are replaced in tests, so that retry and rate-limit
	// paths run instantly and their pauses can be checked.
	now   func() time.Time
	sleep func(ctx context.Context, d time.Duration) error
}

// New returns a Client for cfg.
func New(cfg Config) *Client {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Client{
		http: &http.Client{
			Timeout: requestTimeout,
			// Redirects are not followed by net/http but by FetchPage, so that
			// each hop is its own attempt in the trace.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		baseURL: cfg.BaseURL,
		token:   cfg.Token,
		trace:   cfg.Trace,
		logger:  logger,
		now:     time.Now,
		sleep:   sleepContext,
	}
}

// sleepContext waits for d or until ctx is cancelled (Ctrl+C), whichever
// comes first.
func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// response is what one attempt received.
type response struct {
	status int // 0 if no response arrived
	header http.Header
	body   []byte
}

// FetchPage gets one page of the listing. Every attempt is written to the
// trace before its outcome is acted on.
func (c *Client) FetchPage(ctx context.Context, target string) (Page, error) {
	retries := 0   // repeats after network errors and 5xx
	rateWaits := 0 // pauses after rate-limit responses
	for attempt := 1; ; attempt++ {
		resp, err := c.do(ctx, target, "")
		if err != nil {
			c.record(target, resp.status, attempt, false, transportErrorKind(err))
			if retryErr := c.backoff(ctx, &retries, target, err.Error()); retryErr != nil {
				return Page{}, fmt.Errorf("get %s: %w: %v", target, retryErr, err)
			}
			continue
		}
		c.record(target, resp.status, attempt, false, "")

		if resp.status >= http.StatusInternalServerError {
			if retryErr := c.backoff(ctx, &retries, target, fmt.Sprintf("status %d", resp.status)); retryErr != nil {
				return Page{}, fmt.Errorf("get %s: %w: status %d", target, retryErr, resp.status)
			}
			continue
		}

		if IsRateLimited(resp.status, resp.header, resp.body) {
			pause := RateLimitPause(resp.header, c.now())
			if rateWaits >= maxRateLimitWaits || pause > maxRateLimitPause {
				return Page{}, fmt.Errorf("get %s: %w: status %d, next attempt in %v",
					target, ErrRateLimited, resp.status, pause)
			}
			rateWaits++
			c.logger.Warn("rate limited, waiting", "url", target, "status", resp.status, "pause", pause)
			if err := c.sleep(ctx, pause); err != nil {
				return Page{}, fmt.Errorf("get %s: %w", target, err)
			}
			continue
		}

		switch resp.status {
		case http.StatusOK:
			items, err := parseItems(resp.body)
			if err != nil {
				return Page{}, fmt.Errorf("get %s: %w", target, err)
			}
			return Page{Items: items, NextURL: NextLink(resp.header.Get("Link"), target)}, nil
		case http.StatusUnauthorized:
			return Page{}, fmt.Errorf("get %s: %w", target, ErrUnauthorized)
		case http.StatusNotFound:
			return Page{}, fmt.Errorf("get %s: %w", target, ErrNotFound)
		default:
			return Page{}, fmt.Errorf("get %s: %w: status %d", target, ErrRequestFailed, resp.status)
		}
	}
}

// backoff waits before the next repeat of a failed request and counts it in
// *retries. Once the repeats are used up it returns ErrRequestFailed; if the
// wait is interrupted, the context's error.
func (c *Client) backoff(ctx context.Context, retries *int, target, reason string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if *retries >= len(retryDelays) {
		return ErrRequestFailed
	}
	delay := retryDelays[*retries]
	*retries++
	c.logger.Warn("request failed, retrying", "url", target, "reason", reason, "delay", delay)
	return c.sleep(ctx, delay)
}

// do sends one GET and reads the whole body. If the body cannot be read, the
// status is still returned along with the error.
func (c *Client) do(ctx context.Context, target, etag string) (response, error) {
	req, err := c.newRequest(ctx, target, etag)
	if err != nil {
		return response{}, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return response{}, err
	}
	defer resp.Body.Close()

	result := response{status: resp.StatusCode, header: resp.Header}
	result.body, err = io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if err != nil {
		return result, err
	}
	if len(result.body) > maxBodyBytes {
		return result, fmt.Errorf("body larger than %d bytes", maxBodyBytes)
	}
	return result, nil
}

func (c *Client) record(target string, status, attempt int, fromCache bool, errKind string) {
	entry := trace.Entry{
		TS:        c.now().UTC().Format(time.RFC3339),
		Method:    http.MethodGet,
		URL:       target,
		Status:    status,
		Attempt:   attempt,
		FromCache: fromCache,
	}
	if errKind != "" {
		entry.Error = &errKind
	}
	c.trace.Record(entry)
}

// transportErrorKind is the trace's short description of a failed attempt.
func transportErrorKind(err error) string {
	var netErr net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout()) {
		return "timeout"
	}
	return "network"
}

// parseItems requires a JSON array; its elements are checked later, by the
// report's collector.
func parseItems(body []byte) ([]json.RawMessage, error) {
	// json.Unmarshal accepts null for a slice; the contract is an array.
	if !bytes.HasPrefix(bytes.TrimSpace(body), []byte("[")) {
		return nil, fmt.Errorf("%w: body is not a JSON array", ErrInvalidResponse)
	}
	var items []json.RawMessage
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidResponse, err)
	}
	return items, nil
}
