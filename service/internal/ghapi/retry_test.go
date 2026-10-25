package ghapi

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"sync"
	"testing"
	"time"
)

// scripted answers the n-th request with steps[n]; after the script ends it
// serves an empty page.
type scripted struct {
	mu    sync.Mutex
	steps []func(w http.ResponseWriter, r *http.Request)
	count int
}

func (s *scripted) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	n := s.count
	s.count++
	s.mu.Unlock()
	if n < len(s.steps) {
		s.steps[n](w, r)
		return
	}
	_, _ = w.Write([]byte("[]"))
}

func (s *scripted) requests() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.count
}

func status(code int) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(code) }
}

// dropConnection closes the connection without any response.
func dropConnection(w http.ResponseWriter, _ *http.Request) {
	conn, _, err := w.(http.Hijacker).Hijack()
	if err == nil {
		_ = conn.Close()
	}
}

func repeat(step func(http.ResponseWriter, *http.Request), n int) []func(http.ResponseWriter, *http.Request) {
	steps := make([]func(http.ResponseWriter, *http.Request), n)
	for i := range steps {
		steps[i] = step
	}
	return steps
}

func seconds(values ...int) []time.Duration {
	out := make([]time.Duration, 0, len(values))
	for _, v := range values {
		out = append(out, time.Duration(v)*time.Second)
	}
	return out
}

func TestRetry_ServerErrorsThenSuccess(t *testing.T) {
	server := &scripted{steps: repeat(status(http.StatusInternalServerError), 2)}
	ts := newTestServer(t, server)
	client, pauses := newTestClient(Config{BaseURL: ts.URL})

	if _, err := client.FetchPage(context.Background(), ts.URL+"/x"); err != nil {
		t.Fatalf("FetchPage: %v", err)
	}
	if got := pauses.recorded(); !reflect.DeepEqual(got, seconds(1, 2)) {
		t.Errorf("pauses = %v, want [1s 2s]", got)
	}
	if server.requests() != 3 {
		t.Errorf("%d requests, want 3", server.requests())
	}
}

func TestRetry_GivesUpAfterThreeRepeats(t *testing.T) {
	server := &scripted{steps: repeat(status(http.StatusBadGateway), 10)}
	ts := newTestServer(t, server)
	client, pauses := newTestClient(Config{BaseURL: ts.URL})

	_, err := client.FetchPage(context.Background(), ts.URL+"/x")
	if !errors.Is(err, ErrRequestFailed) {
		t.Fatalf("error = %v, want ErrRequestFailed", err)
	}
	if got := pauses.recorded(); !reflect.DeepEqual(got, seconds(1, 2, 4)) {
		t.Errorf("pauses = %v, want [1s 2s 4s]", got)
	}
	if server.requests() != 4 {
		t.Errorf("%d requests, want 4: one plus three repeats", server.requests())
	}
}

func TestRetry_DroppedConnections(t *testing.T) {
	server := &scripted{steps: repeat(dropConnection, 10)}
	ts := newTestServer(t, server)
	client, entries := tracedClient(t, Config{BaseURL: ts.URL})

	if _, err := client.FetchPage(context.Background(), ts.URL+"/x"); !errors.Is(err, ErrRequestFailed) {
		t.Fatalf("error = %v, want ErrRequestFailed", err)
	}
	got := entries()
	if len(got) != 4 {
		t.Fatalf("%d trace entries, want 4", len(got))
	}
	for i, entry := range got {
		if entry["status"] != float64(0) || entry["error"] != "network" || entry["attempt"] != float64(i+1) {
			t.Errorf("entry %d = %v, want status 0, error network, attempt %d", i, entry, i+1)
		}
	}
}

func TestRetry_Timeout(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	slow := func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}
	server := &scripted{steps: repeat(slow, 1)}
	ts := newTestServer(t, server)
	client, entries := tracedClient(t, Config{BaseURL: ts.URL})
	client.http.Timeout = 50 * time.Millisecond

	if _, err := client.FetchPage(context.Background(), ts.URL+"/x"); err != nil {
		t.Fatalf("FetchPage: %v", err)
	}
	got := entries()
	if len(got) != 2 || got[0]["error"] != "timeout" || got[0]["status"] != float64(0) ||
		got[1]["status"] != float64(200) || got[1]["attempt"] != float64(2) {
		t.Errorf("entries = %v, want a timeout then a 200 on attempt 2", got)
	}
}

func TestRetry_ClientErrorsAreNotRepeated(t *testing.T) {
	for _, code := range []int{http.StatusNotFound, http.StatusUnauthorized, http.StatusBadRequest} {
		server := &scripted{steps: repeat(status(code), 10)}
		ts := newTestServer(t, server)
		client, pauses := newTestClient(Config{BaseURL: ts.URL})

		if _, err := client.FetchPage(context.Background(), ts.URL+"/x"); err == nil {
			t.Errorf("status %d: no error", code)
		}
		if server.requests() != 1 || len(pauses.recorded()) != 0 {
			t.Errorf("status %d: %d requests, pauses %v; want one request and no pause",
				code, server.requests(), pauses.recorded())
		}
	}
}

func TestRetry_StopsWhenCancelled(t *testing.T) {
	server := &scripted{steps: repeat(status(http.StatusServiceUnavailable), 10)}
	ts := newTestServer(t, server)
	client, _ := newTestClient(Config{BaseURL: ts.URL})
	ctx, cancel := context.WithCancel(context.Background())
	client.sleep = func(context.Context, time.Duration) error {
		cancel() // Ctrl+C during the pause
		return context.Canceled
	}

	if _, err := client.FetchPage(ctx, ts.URL+"/x"); !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled", err)
	}
	if server.requests() != 1 {
		t.Errorf("%d requests after cancellation, want 1", server.requests())
	}
}
