package ghapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kgugunava/vk-ims/service/internal/report"
)

// respond serves the same response to every request and records the
// requests it received.
type respond struct {
	status   int
	header   http.Header
	body     string
	requests []*http.Request
}

func (r *respond) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.requests = append(r.requests, req.Clone(context.Background()))
	for name, values := range r.header {
		w.Header()[name] = values
	}
	w.WriteHeader(r.status)
	_, _ = w.Write([]byte(r.body))
}

func newTestServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}

func TestFirstPageURL(t *testing.T) {
	got := FirstPageURL("http://h:1/api/v3", "cli/cli")
	want := "http://h:1/api/v3/repos/cli/cli/issues?state=open&sort=created&direction=asc&per_page=100"
	if got != want {
		t.Errorf("FirstPageURL = %q, want %q", got, want)
	}
}

func TestFetchPage_SendsHeaders(t *testing.T) {
	handler := &respond{status: http.StatusOK, body: "[]"}
	server := newTestServer(t, handler)

	for _, token := range []string{"secret", ""} {
		handler.requests = nil
		client := testClient(Config{BaseURL: server.URL, Token: token})
		if _, err := client.FetchPage(context.Background(), FirstPageURL(server.URL, "o/r")); err != nil {
			t.Fatalf("FetchPage: %v", err)
		}
		req := handler.requests[0]
		checks := map[string]string{
			"Accept":               "application/vnd.github+json",
			"User-Agent":           UserAgent,
			"X-GitHub-Api-Version": "2026-03-10",
		}
		for name, want := range checks {
			if got := req.Header.Get(name); got != want {
				t.Errorf("token %q: header %s = %q, want %q", token, name, got, want)
			}
		}
		wantAuth := ""
		if token != "" {
			wantAuth = "Bearer " + token
		}
		if got := req.Header.Get("Authorization"); got != wantAuth {
			t.Errorf("token %q: Authorization = %q, want %q", token, got, wantAuth)
		}
		if req.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", req.Method)
		}
	}
}

func TestFetchPage_TokenOnlyForTheAPIHost(t *testing.T) {
	other := &respond{status: http.StatusOK, body: "[]"}
	otherServer := newTestServer(t, other)
	apiServer := newTestServer(t, &respond{status: http.StatusOK, body: "[]"})

	client := testClient(Config{BaseURL: apiServer.URL, Token: "secret"})
	if _, err := client.FetchPage(context.Background(), otherServer.URL+"/repos/o/r/issues?page=2"); err != nil {
		t.Fatalf("FetchPage: %v", err)
	}
	if got := other.requests[0].Header.Get("Authorization"); got != "" {
		t.Errorf("Authorization sent to another host: %q", got)
	}
}

func TestFetchPage_ReturnsItemsAndNext(t *testing.T) {
	server := newTestServer(t, &respond{
		status: http.StatusOK,
		header: http.Header{"Link": {`</repos/o/r/issues?page=2>; rel="next"`}},
		body:   `[{"number":1},{"number":2}]`,
	})
	page, err := testClient(Config{BaseURL: server.URL}).FetchPage(context.Background(), server.URL+"/repos/o/r/issues")
	if err != nil {
		t.Fatalf("FetchPage: %v", err)
	}
	if len(page.Items) != 2 || string(page.Items[1]) != `{"number":2}` {
		t.Errorf("items = %s", page.Items)
	}
	if want := server.URL + "/repos/o/r/issues?page=2"; page.NextURL != want {
		t.Errorf("NextURL = %q, want %q", page.NextURL, want)
	}
}

func TestFetchPage_StatusAndBodyErrors(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		body     string
		wantErr  error
		wantCode report.ErrorCode
	}{
		{"unauthorized", http.StatusUnauthorized, `{"message":"Bad credentials"}`, ErrUnauthorized, report.RequestFailed},
		{"not found", http.StatusNotFound, `{"message":"Not Found"}`, ErrNotFound, report.NotFound},
		{"bad request", http.StatusBadRequest, "", ErrRequestFailed, report.RequestFailed},
		{"gone", http.StatusGone, "", ErrRequestFailed, report.RequestFailed},
		{"unprocessable", http.StatusUnprocessableEntity, "", ErrRequestFailed, report.RequestFailed},
		{"forbidden without rate limit signs", http.StatusForbidden, `{"message":"Forbidden"}`, ErrRequestFailed, report.RequestFailed},
		{"object instead of array", http.StatusOK, `{"items":[]}`, ErrInvalidResponse, report.InvalidResponse},
		{"null", http.StatusOK, `null`, ErrInvalidResponse, report.InvalidResponse},
		{"truncated JSON", http.StatusOK, `[{"number":1}`, ErrInvalidResponse, report.InvalidResponse},
		{"empty body", http.StatusOK, ``, ErrInvalidResponse, report.InvalidResponse},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newTestServer(t, &respond{status: tt.status, body: tt.body})
			_, err := testClient(Config{BaseURL: server.URL}).FetchPage(context.Background(), server.URL+"/x")
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != ErrUnauthorized && CodeOf(err) != tt.wantCode {
				t.Errorf("CodeOf = %q, want %q", CodeOf(err), tt.wantCode)
			}
		})
	}
}

func TestFetchPage_ErrorsDoNotContainToken(t *testing.T) {
	server := newTestServer(t, &respond{status: http.StatusInternalServerError})
	_, err := testClient(Config{BaseURL: server.URL, Token: "secret-token"}).FetchPage(context.Background(), server.URL+"/x")
	if err == nil || strings.Contains(err.Error(), "secret-token") {
		t.Errorf("error = %v; want an error without the token", err)
	}
}
