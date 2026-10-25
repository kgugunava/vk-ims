package ghapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kgugunava/vk-ims/service/internal/cache"
)

// conditional serves pages with ETags and answers If-None-Match with 304.
type conditional struct {
	mu          sync.Mutex
	pages       map[string]string // page -> body
	etags       map[string]string // page -> ETag; none if empty
	next        map[string]string // page -> next page path
	linkOn304   bool              // repeat the Link header in 304 responses
	always304   bool              // 304 even without If-None-Match: breaks the contract
	ifNoneMatch []string          // the header of each request, "" if absent
}

func (s *conditional) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	page := r.URL.Query().Get("page")
	if page == "" {
		page = "1"
	}
	inm := r.Header.Get("If-None-Match")
	s.mu.Lock()
	s.ifNoneMatch = append(s.ifNoneMatch, inm)
	etag := s.etags[page]
	s.mu.Unlock()

	if next, ok := s.next[page]; ok {
		w.Header().Set("Link", fmt.Sprintf(`<%s>; rel="next"`, next))
	}
	if etag != "" {
		w.Header().Set("ETag", etag)
	}
	if s.always304 || (etag != "" && inm == etag) {
		if !s.linkOn304 {
			w.Header().Del("Link")
		}
		w.WriteHeader(http.StatusNotModified)
		return
	}
	_, _ = w.Write([]byte(s.pages[page]))
}

func (s *conditional) sent() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.ifNoneMatch
	s.ifNoneMatch = nil
	return out
}

func twoPagesWithETags() *conditional {
	return &conditional{
		pages: map[string]string{"1": issuesJSON(1, 2), "2": issuesJSON(3)},
		etags: map[string]string{"1": `"e1"`, "2": `W/"e2"`},
		next:  map[string]string{"1": "/repos/o/r/issues?per_page=100&page=2"},
	}
}

func openCache(t *testing.T) *cache.Store {
	t.Helper()
	store, err := cache.Open(filepath.Join(t.TempDir(), "cache"))
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func TestCache_SecondRunUsesConditionalRequests(t *testing.T) {
	server := twoPagesWithETags()
	ts := newTestServer(t, server)
	store := openCache(t)

	first, firstTrace := tracedClient(t, Config{BaseURL: ts.URL, Cache: store})
	want, err := first.ListIssues(context.Background(), "o/r", 100, time.Time{})
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	if got := server.sent(); !reflect.DeepEqual(got, []string{"", ""}) {
		t.Errorf("first run sent If-None-Match %q, want none", got)
	}
	for _, entry := range firstTrace() {
		if entry["status"] != float64(200) || entry["from_cache"] != false {
			t.Errorf("first run trace entry %v, want 200 not from cache", entry)
		}
	}

	second, secondTrace := tracedClient(t, Config{BaseURL: ts.URL, Cache: store})
	got, err := second.ListIssues(context.Background(), "o/r", 100, time.Time{})
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("second run issues %v, want %v", got, want)
	}
	// The weak validator goes back exactly as received.
	if sent := server.sent(); !reflect.DeepEqual(sent, []string{`"e1"`, `W/"e2"`}) {
		t.Errorf("second run sent If-None-Match %q", sent)
	}
	entries := secondTrace()
	if len(entries) != 2 {
		t.Fatalf("second run: %d trace entries, want 2", len(entries))
	}
	for _, entry := range entries {
		if entry["status"] != float64(304) || entry["from_cache"] != true {
			t.Errorf("second run trace entry %v, want 304 from cache", entry)
		}
	}
}

func TestCache_LinkComesFromCacheWhen304OmitsIt(t *testing.T) {
	server := twoPagesWithETags() // linkOn304 is false
	ts := newTestServer(t, server)
	store := openCache(t)

	if _, err := testClient(Config{BaseURL: ts.URL, Cache: store}).ListIssues(context.Background(), "o/r", 100, time.Time{}); err != nil {
		t.Fatal(err)
	}
	issues, err := testClient(Config{BaseURL: ts.URL, Cache: store}).ListIssues(context.Background(), "o/r", 100, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if got := issueNumbers(issues); !reflect.DeepEqual(got, []int{1, 2, 3}) {
		t.Errorf("numbers = %v: pagination stopped at the 304 without a Link header", got)
	}
}

func TestCache_NoETagNoConditionalRequest(t *testing.T) {
	server := twoPagesWithETags()
	server.etags = nil
	ts := newTestServer(t, server)
	store := openCache(t)

	for run := 0; run < 2; run++ {
		if _, err := testClient(Config{BaseURL: ts.URL, Cache: store}).ListIssues(context.Background(), "o/r", 100, time.Time{}); err != nil {
			t.Fatal(err)
		}
	}
	for _, inm := range server.sent() {
		if inm != "" {
			t.Errorf("If-None-Match %q sent although the server gave no ETag", inm)
		}
	}
}

func TestCache_ChangedETagReplacesEntry(t *testing.T) {
	server := twoPagesWithETags()
	server.next = nil
	ts := newTestServer(t, server)
	store := openCache(t)
	run := func() {
		t.Helper()
		if _, err := testClient(Config{BaseURL: ts.URL, Cache: store}).ListIssues(context.Background(), "o/r", 100, time.Time{}); err != nil {
			t.Fatal(err)
		}
	}

	run()
	server.mu.Lock()
	server.etags["1"] = `"changed"`
	server.pages["1"] = issuesJSON(4)
	server.mu.Unlock()
	run() // sends "e1", gets 200 with the new ETag
	run() // must now send the new one
	if got := server.sent(); !reflect.DeepEqual(got, []string{"", `"e1"`, `"changed"`}) {
		t.Errorf("If-None-Match per run = %q", got)
	}
}

func TestCache_DamagedEntryIsAMiss(t *testing.T) {
	server := twoPagesWithETags()
	server.next = nil
	ts := newTestServer(t, server)
	dir := filepath.Join(t.TempDir(), "cache")
	store, err := cache.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	client := testClient(Config{BaseURL: ts.URL, Cache: store})
	if _, err := client.ListIssues(context.Background(), "o/r", 100, time.Time{}); err != nil {
		t.Fatal(err)
	}

	files, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	for _, file := range files {
		if err := os.WriteFile(file, []byte(`{"url":`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := client.ListIssues(context.Background(), "o/r", 100, time.Time{}); err != nil {
		t.Fatalf("run with a damaged cache: %v", err)
	}
	if _, err := client.ListIssues(context.Background(), "o/r", 100, time.Time{}); err != nil {
		t.Fatal(err)
	}
	// Damaged entry: unconditional request; it is rewritten and used again.
	if got := server.sent(); !reflect.DeepEqual(got, []string{"", "", `"e1"`}) {
		t.Errorf("If-None-Match per run = %q", got)
	}
}

func TestCache_304WithoutConditionalRequest(t *testing.T) {
	server := twoPagesWithETags()
	server.always304 = true
	ts := newTestServer(t, server)

	_, err := testClient(Config{BaseURL: ts.URL, Cache: openCache(t)}).ListIssues(context.Background(), "o/r", 100, time.Time{})
	if !errors.Is(err, ErrInvalidResponse) {
		t.Errorf("error = %v, want ErrInvalidResponse", err)
	}
}

func TestCache_InvalidBodyIsNotStored(t *testing.T) {
	server := twoPagesWithETags()
	server.pages["1"] = `[{"number":1}`
	server.next = nil
	ts := newTestServer(t, server)
	store := openCache(t)

	_, err := testClient(Config{BaseURL: ts.URL, Cache: store}).ListIssues(context.Background(), "o/r", 100, time.Time{})
	if !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("error = %v, want ErrInvalidResponse", err)
	}
	if _, ok := store.Get(FirstPageURL(ts.URL, "o/r")); ok {
		t.Error("an invalid body was cached")
	}
}

func TestCache_FilesHoldNoToken(t *testing.T) {
	ts := newTestServer(t, twoPagesWithETags())
	dir := filepath.Join(t.TempDir(), "cache")
	store, err := cache.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	client := testClient(Config{BaseURL: ts.URL, Token: "secret-token", Cache: store})
	if _, err := client.ListIssues(context.Background(), "o/r", 100, time.Time{}); err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*"))
	if len(files) != 2 {
		t.Fatalf("cache holds %d files, want 2", len(files))
	}
	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, secret := range []string{"secret-token", "Authorization", "Bearer"} {
			if strings.Contains(string(content), secret) {
				t.Errorf("%s contains %q", file, secret)
			}
		}
	}
}
