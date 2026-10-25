package ghapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// mover redirects /old to `location` with `code` and serves a page at /new.
type mover struct {
	mu       sync.Mutex
	code     int
	location string
	auth     map[string]string // path -> Authorization header received
}

func (m *mover) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	if m.auth == nil {
		m.auth = make(map[string]string)
	}
	m.auth[r.URL.Path] = r.Header.Get("Authorization")
	m.mu.Unlock()

	switch r.URL.Path {
	case "/old":
		w.Header().Set("Location", m.location)
		w.WriteHeader(m.code)
	case "/loop":
		w.Header().Set("Location", "/loop")
		w.WriteHeader(http.StatusFound)
	default:
		_, _ = w.Write([]byte(issuesJSON(1)))
	}
}

func TestRedirect_FollowedAndTraced(t *testing.T) {
	codes := []int{
		http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther,
		http.StatusTemporaryRedirect, http.StatusPermanentRedirect,
	}
	for _, code := range codes {
		t.Run(http.StatusText(code), func(t *testing.T) {
			ts := newTestServer(t, &mover{code: code, location: "/new?page=1"})
			client, entries := tracedClient(t, Config{BaseURL: ts.URL})

			page, err := client.FetchPage(context.Background(), ts.URL+"/old")
			if err != nil {
				t.Fatalf("FetchPage: %v", err)
			}
			if len(page.Items) != 1 {
				t.Errorf("items = %s, want the page at the new location", page.Items)
			}
			got := entries()
			if len(got) != 2 ||
				got[0]["url"] != ts.URL+"/old" || got[0]["status"] != float64(code) ||
				got[1]["url"] != ts.URL+"/new?page=1" || got[1]["status"] != float64(200) ||
				got[1]["attempt"] != float64(1) {
				t.Errorf("trace = %v, want the redirect and then the new URL at attempt 1", got)
			}
		})
	}
}

func TestRedirect_GivesUpOnLoops(t *testing.T) {
	server := &mover{}
	ts := newTestServer(t, server)
	client, entries := tracedClient(t, Config{BaseURL: ts.URL})

	if _, err := client.FetchPage(context.Background(), ts.URL+"/loop"); !errors.Is(err, ErrRequestFailed) {
		t.Fatalf("error = %v, want ErrRequestFailed", err)
	}
	if got := len(entries()); got != maxRedirects+1 {
		t.Errorf("%d attempts, want %d", got, maxRedirects+1)
	}
}

func TestRedirect_WithoutLocation(t *testing.T) {
	ts := newTestServer(t, &respond{status: http.StatusMovedPermanently})
	if _, err := testClient(Config{BaseURL: ts.URL}).FetchPage(context.Background(), ts.URL+"/x"); !errors.Is(err, ErrRequestFailed) {
		t.Errorf("error = %v, want ErrRequestFailed", err)
	}
}

func TestRedirect_TokenStaysOnTheAPIHost(t *testing.T) {
	elsewhere := &mover{}
	other := httptest.NewServer(elsewhere)
	t.Cleanup(other.Close)

	api := &mover{code: http.StatusFound, location: other.URL + "/new"}
	apiServer := newTestServer(t, api)
	client := testClient(Config{BaseURL: apiServer.URL, Token: "secret"})

	if _, err := client.FetchPage(context.Background(), apiServer.URL+"/old"); err != nil {
		t.Fatalf("FetchPage: %v", err)
	}
	if got := api.auth["/old"]; got != "Bearer secret" {
		t.Errorf("Authorization to the API host = %q, want the token", got)
	}
	if got := elsewhere.auth["/new"]; got != "" {
		t.Errorf("Authorization sent to the redirect target on another host: %q", got)
	}

	// A redirect within the API host keeps the token.
	api.location = "/new"
	if _, err := client.FetchPage(context.Background(), apiServer.URL+"/old"); err != nil {
		t.Fatalf("FetchPage: %v", err)
	}
	if got := api.auth["/new"]; got != "Bearer secret" {
		t.Errorf("Authorization after a same-host redirect = %q, want the token", got)
	}
}
