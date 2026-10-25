package ghapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

// Config holds what a Client needs to reach the API.
type Config struct {
	BaseURL string        // without a trailing slash
	Token   string        // empty for unauthenticated requests
	Trace   *trace.Writer // nil when --trace is not given
}

// Client makes the requests of one run, strictly one at a time.
type Client struct {
	http    *http.Client
	baseURL string
	token   string
	trace   *trace.Writer
	now     func() time.Time
}

// New returns a Client for cfg.
func New(cfg Config) *Client {
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
		now:     time.Now,
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
	for attempt := 1; ; attempt++ {
		resp, err := c.do(ctx, target, "")
		if err != nil {
			c.record(target, resp.status, attempt, false, transportErrorKind(err))
			return Page{}, fmt.Errorf("get %s: %w: %v", target, ErrRequestFailed, err)
		}
		c.record(target, resp.status, attempt, false, "")

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
