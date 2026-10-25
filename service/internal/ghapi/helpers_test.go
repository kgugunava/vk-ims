package ghapi

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"time"
)

// fakeSleep records the pauses a client asks for instead of waiting, so that
// retry and rate-limit tests run instantly and can check the pauses.
type fakeSleep struct {
	mu     sync.Mutex
	delays []time.Duration
}

func (f *fakeSleep) sleep(ctx context.Context, d time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.delays = append(f.delays, d)
	return ctx.Err()
}

func (f *fakeSleep) recorded() []time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]time.Duration(nil), f.delays...)
}

// newTestClient returns a client that never really sleeps and logs nothing.
func newTestClient(cfg Config) (*Client, *fakeSleep) {
	cfg.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	client := New(cfg)
	pauses := &fakeSleep{}
	client.sleep = pauses.sleep
	return client, pauses
}

// testClient is newTestClient for tests that do not look at the pauses.
func testClient(cfg Config) *Client {
	client, _ := newTestClient(cfg)
	return client
}
