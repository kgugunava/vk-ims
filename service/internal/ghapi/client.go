package ghapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
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
	BaseURL string // without a trailing slash
	Token   string // empty for unauthenticated requests
}

// Client makes the requests of one run, strictly one at a time.
type Client struct {
	http    *http.Client
	baseURL string
	token   string
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
	}
}

// FetchPage gets one page of the listing.
func (c *Client) FetchPage(ctx context.Context, target string) (Page, error) {
	req, err := c.newRequest(ctx, target, "")
	if err != nil {
		return Page{}, fmt.Errorf("get %s: %w: %v", target, ErrRequestFailed, err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return Page{}, fmt.Errorf("get %s: %w: %v", target, ErrRequestFailed, err)
	}
	body, err := readBody(resp)
	if err != nil {
		return Page{}, fmt.Errorf("get %s: %w: %v", target, ErrRequestFailed, err)
	}

	switch resp.StatusCode {
	case http.StatusOK:
		items, err := parseItems(body)
		if err != nil {
			return Page{}, fmt.Errorf("get %s: %w", target, err)
		}
		return Page{Items: items, NextURL: NextLink(resp.Header.Get("Link"), target)}, nil
	case http.StatusUnauthorized:
		return Page{}, fmt.Errorf("get %s: %w", target, ErrUnauthorized)
	case http.StatusNotFound:
		return Page{}, fmt.Errorf("get %s: %w", target, ErrNotFound)
	default:
		return Page{}, fmt.Errorf("get %s: %w: status %d", target, ErrRequestFailed, resp.StatusCode)
	}
}

// readBody reads the whole body, so the connection can be reused, and
// closes it.
func readBody(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxBodyBytes {
		return nil, fmt.Errorf("body larger than %d bytes", maxBodyBytes)
	}
	return body, nil
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
