package ghapi

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kgugunava/vk-ims/service/internal/trace"
)

// tracedClient returns a client writing its trace to a temporary file, and
// a function that closes the trace and returns its entries.
func tracedClient(t *testing.T, cfg Config) (*Client, func() []map[string]any) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "trace.jsonl")
	writer, err := trace.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Trace = writer
	client := testClient(cfg)
	client.now = func() time.Time { return time.Date(2026, 10, 14, 12, 0, 0, 0, time.FixedZone("MSK", 3*3600)) }

	entries := func() []map[string]any {
		t.Helper()
		if err := writer.Close(); err != nil {
			t.Fatalf("close trace: %v", err)
		}
		file, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		var out []map[string]any
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			var entry map[string]any
			if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
				t.Fatalf("trace line %q: %v", scanner.Text(), err)
			}
			out = append(out, entry)
		}
		return out
	}
	return client, entries
}

func TestTrace_RecordsEveryPage(t *testing.T) {
	ts := newTestServer(t, threePages())
	client, entries := tracedClient(t, Config{BaseURL: ts.URL, Token: "secret-token"})

	if _, err := client.ListIssues(context.Background(), "o/r", 100, time.Time{}); err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	got := entries()
	if len(got) != 3 {
		t.Fatalf("%d trace entries, want 3: %v", len(got), got)
	}
	wantURLs := []string{
		ts.URL + "/repos/o/r/issues?state=open&sort=created&direction=asc&per_page=100",
		ts.URL + "/repos/o/r/issues?per_page=100&page=2",
		ts.URL + "/repos/o/r/issues?per_page=100&page=3",
	}
	for i, entry := range got {
		want := map[string]any{
			"ts": "2026-10-14T09:00:00Z", "method": "GET", "url": wantURLs[i],
			"status": float64(200), "attempt": float64(1), "from_cache": false, "error": nil,
		}
		if len(entry) != len(want) {
			t.Errorf("entry %d has fields %v, want exactly %d", i, entry, len(want))
		}
		for key, value := range want {
			if entry[key] != value {
				t.Errorf("entry %d: %s = %v, want %v", i, key, entry[key], value)
			}
		}
	}
}

func TestTrace_RecordsErrorStatuses(t *testing.T) {
	ts := newTestServer(t, &respond{status: http.StatusNotFound})
	client, entries := tracedClient(t, Config{BaseURL: ts.URL})

	if _, err := client.FetchPage(context.Background(), ts.URL+"/x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v", err)
	}
	got := entries()
	if len(got) != 1 || got[0]["status"] != float64(404) || got[0]["error"] != nil {
		t.Errorf("entries = %v, want one with status 404 and error null", got)
	}
}

func TestTrace_NoResponse(t *testing.T) {
	ts := newTestServer(t, &respond{status: http.StatusOK})
	url := ts.URL + "/x"
	ts.Close() // connection refused from now on
	client, entries := tracedClient(t, Config{BaseURL: ts.URL})

	if _, err := client.FetchPage(context.Background(), url); !errors.Is(err, ErrRequestFailed) {
		t.Fatalf("error = %v, want ErrRequestFailed", err)
	}
	got := entries()
	if len(got) == 0 || got[0]["status"] != float64(0) || got[0]["error"] != "network" {
		t.Errorf("entries = %v, want status 0 and error \"network\"", got)
	}
}

func TestTrace_NeverContainsToken(t *testing.T) {
	ts := newTestServer(t, threePages())
	path := filepath.Join(t.TempDir(), "trace.jsonl")
	writer, err := trace.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	client := testClient(Config{BaseURL: ts.URL, Token: "secret-token", Trace: writer})
	if _, err := client.ListIssues(context.Background(), "o/r", 100, time.Time{}); err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"secret-token", "Authorization", "Bearer"} {
		if strings.Contains(string(content), secret) {
			t.Errorf("trace contains %q:\n%s", secret, content)
		}
	}
}
