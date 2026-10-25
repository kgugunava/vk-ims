package ghapi

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strconv"
	"testing"
	"time"
)

var testNow = time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)

func TestIsRateLimited(t *testing.T) {
	tests := []struct {
		name   string
		status int
		header http.Header
		body   string
		want   bool
	}{
		{"429 with retry-after", 429, http.Header{"Retry-After": {"3"}}, "", true},
		{"403 with remaining 0", 403, http.Header{"X-Ratelimit-Remaining": {"0"}}, "", true},
		{"403 primary limit message", 403, nil, `{"message":"API rate limit exceeded for 1.2.3.4."}`, true},
		{"403 secondary limit message", 403, nil, `{"message":"You have exceeded a secondary rate limit."}`, true},
		{"plain text body", 429, nil, "Rate Limit exceeded", true},
		{"403 forbidden for another reason", 403, nil, `{"message":"Resource not accessible"}`, false},
		{"403 with remaining left", 403, http.Header{"X-Ratelimit-Remaining": {"10"}}, "", false},
		{"429 without any sign", 429, nil, `{"message":"slow down"}`, false},
		{"500 with retry-after", 500, http.Header{"Retry-After": {"3"}}, "", false},
		{"200 with remaining 0", 200, http.Header{"X-Ratelimit-Remaining": {"0"}}, "[]", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsRateLimited(tt.status, tt.header, []byte(tt.body)); got != tt.want {
				t.Errorf("IsRateLimited = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRateLimitPause(t *testing.T) {
	epoch := func(d time.Duration) string { return strconv.FormatInt(testNow.Add(d).Unix(), 10) }
	tests := []struct {
		name   string
		header http.Header
		want   time.Duration
	}{
		{"retry-after seconds", http.Header{"Retry-After": {"3"}}, 3 * time.Second},
		{"retry-after zero", http.Header{"Retry-After": {"0"}}, 0},
		{"retry-after wins over reset", http.Header{"Retry-After": {"2"}, "X-Ratelimit-Reset": {epoch(time.Hour)}}, 2 * time.Second},
		{"retry-after as a date", http.Header{"Retry-After": {testNow.Add(7 * time.Second).Format(http.TimeFormat)}}, 7 * time.Second},
		{"unparsable retry-after falls back to reset", http.Header{"Retry-After": {"soon"}, "X-Ratelimit-Reset": {epoch(10 * time.Second)}}, 10 * time.Second},
		{"until reset", http.Header{"X-Ratelimit-Reset": {epoch(10 * time.Second)}}, 10 * time.Second},
		{"reset in the past", http.Header{"X-Ratelimit-Reset": {epoch(-time.Minute)}}, time.Second},
		{"nothing to go by", http.Header{}, time.Minute},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RateLimitPause(tt.header, testNow); got != tt.want {
				t.Errorf("RateLimitPause = %v, want %v", got, tt.want)
			}
		})
	}
}

func headers(pairs ...string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) {
		for i := 0; i+2 < len(pairs); i += 2 {
			w.Header().Set(pairs[i], pairs[i+1])
		}
		code, _ := strconv.Atoi(pairs[len(pairs)-1])
		w.WriteHeader(code)
	}
}

func rateLimitClient(t *testing.T, steps ...func(http.ResponseWriter, *http.Request)) (*Client, *fakeSleep, *scripted, string) {
	t.Helper()
	server := &scripted{steps: steps}
	ts := newTestServer(t, server)
	client, pauses := newTestClient(Config{BaseURL: ts.URL})
	client.now = func() time.Time { return testNow }
	return client, pauses, server, ts.URL + "/x"
}

func TestRateLimit_WaitsRetryAfterThenSucceeds(t *testing.T) {
	client, pauses, server, url := rateLimitClient(t, headers("Retry-After", "3", "429"))

	if _, err := client.FetchPage(context.Background(), url); err != nil {
		t.Fatalf("FetchPage: %v", err)
	}
	if got := pauses.recorded(); !reflect.DeepEqual(got, seconds(3)) {
		t.Errorf("pauses = %v, want [3s]", got)
	}
	if server.requests() != 2 {
		t.Errorf("%d requests, want 2", server.requests())
	}
}

func TestRateLimit_WaitsUntilReset(t *testing.T) {
	reset := strconv.FormatInt(testNow.Add(10*time.Second).Unix(), 10)
	client, pauses, _, url := rateLimitClient(t, headers("X-RateLimit-Remaining", "0", "X-RateLimit-Reset", reset, "403"))

	if _, err := client.FetchPage(context.Background(), url); err != nil {
		t.Fatalf("FetchPage: %v", err)
	}
	if got := pauses.recorded(); !reflect.DeepEqual(got, seconds(10)) {
		t.Errorf("pauses = %v, want [10s]", got)
	}
}

func TestRateLimit_MessageOnlyWaitsAMinute(t *testing.T) {
	body := func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"API rate limit exceeded"}`))
	}
	client, pauses, _, url := rateLimitClient(t, body)

	if _, err := client.FetchPage(context.Background(), url); err != nil {
		t.Fatalf("FetchPage: %v", err)
	}
	if got := pauses.recorded(); !reflect.DeepEqual(got, seconds(60)) {
		t.Errorf("pauses = %v, want [1m]", got)
	}
}

func TestRateLimit_GivesUpAfterThreeWaits(t *testing.T) {
	limited := headers("Retry-After", "1", "429")
	client, pauses, server, url := rateLimitClient(t, limited, limited, limited, limited, limited)

	if _, err := client.FetchPage(context.Background(), url); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("error = %v, want ErrRateLimited", err)
	}
	if got := pauses.recorded(); !reflect.DeepEqual(got, seconds(1, 1, 1)) {
		t.Errorf("pauses = %v, want three of 1s", got)
	}
	if server.requests() != 4 {
		t.Errorf("%d requests, want 4", server.requests())
	}
}

func TestRateLimit_LongPauseIsNotWaited(t *testing.T) {
	client, pauses, server, url := rateLimitClient(t, headers("Retry-After", "3600", "429"))

	if _, err := client.FetchPage(context.Background(), url); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("error = %v, want ErrRateLimited", err)
	}
	if len(pauses.recorded()) != 0 || server.requests() != 1 {
		t.Errorf("pauses %v, %d requests; want no pause and one request", pauses.recorded(), server.requests())
	}
}

func TestRateLimit_429WithoutSignsIsAFailure(t *testing.T) {
	client, pauses, _, url := rateLimitClient(t, status(http.StatusTooManyRequests))

	if _, err := client.FetchPage(context.Background(), url); !errors.Is(err, ErrRequestFailed) {
		t.Fatalf("error = %v, want ErrRequestFailed", err)
	}
	if len(pauses.recorded()) != 0 {
		t.Errorf("pauses = %v, want none", pauses.recorded())
	}
}

func TestRateLimit_TraceShowsEachAttempt(t *testing.T) {
	server := &scripted{steps: repeat(headers("Retry-After", "1", "429"), 1)}
	ts := newTestServer(t, server)
	client, entries := tracedClient(t, Config{BaseURL: ts.URL})

	if _, err := client.FetchPage(context.Background(), ts.URL+"/x"); err != nil {
		t.Fatalf("FetchPage: %v", err)
	}
	got := entries()
	if len(got) != 2 || got[0]["status"] != float64(429) || got[0]["attempt"] != float64(1) ||
		got[1]["status"] != float64(200) || got[1]["attempt"] != float64(2) {
		t.Errorf("entries = %v, want 429 on attempt 1 then 200 on attempt 2", got)
	}
}
