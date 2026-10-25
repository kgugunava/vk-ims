package ghapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kgugunava/vk-ims/service/internal/report"
)

// pagedServer serves numbered pages; page N links to `next[N]` if present.
type pagedServer struct {
	mu       sync.Mutex
	pages    map[string]string // page number -> JSON body
	next     map[string]string // page number -> next page URL (path and query)
	status   map[string]int    // page number -> status, 200 if absent
	requests []string          // request URIs in order
}

func (s *pagedServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.requests = append(s.requests, r.URL.RequestURI())
	s.mu.Unlock()

	page := r.URL.Query().Get("page")
	if page == "" {
		page = "1"
	}
	if next, ok := s.next[page]; ok {
		w.Header().Set("Link", fmt.Sprintf(`<%s>; rel="next"`, next))
	}
	if status, ok := s.status[page]; ok {
		w.WriteHeader(status)
		return
	}
	_, _ = w.Write([]byte(s.pages[page]))
}

func issuesJSON(numbers ...int) string {
	parts := make([]string, 0, len(numbers))
	for _, n := range numbers {
		parts = append(parts, fmt.Sprintf(
			`{"number":%d,"title":"t%d","user":{"login":"u"},"labels":[],"created_at":"2026-01-%02dT00:00:00Z","html_url":"https://github.com/o/r/issues/%d"}`,
			n, n, n, n))
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func issueNumbers(issues []report.Issue) []int {
	out := make([]int, 0, len(issues))
	for _, issue := range issues {
		out = append(out, issue.Number)
	}
	return out
}

func threePages() *pagedServer {
	const path = "/repos/o/r/issues?per_page=100&page="
	return &pagedServer{
		pages: map[string]string{"1": issuesJSON(1, 2), "2": issuesJSON(3, 4), "3": issuesJSON(5)},
		next:  map[string]string{"1": path + "2", "2": path + "3"},
	}
}

func TestListIssues_FollowsAllPages(t *testing.T) {
	server := threePages()
	ts := newTestServer(t, server)

	issues, err := testClient(Config{BaseURL: ts.URL}).ListIssues(context.Background(), "o/r", 100, time.Time{})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if got := issueNumbers(issues); !reflect.DeepEqual(got, []int{1, 2, 3, 4, 5}) {
		t.Errorf("numbers = %v", got)
	}
	want := []string{
		"/repos/o/r/issues?state=open&sort=created&direction=asc&per_page=100",
		"/repos/o/r/issues?per_page=100&page=2",
		"/repos/o/r/issues?per_page=100&page=3",
	}
	if !reflect.DeepEqual(server.requests, want) {
		t.Errorf("requests:\n got  %q\n want %q", server.requests, want)
	}
}

func TestListIssues_StopsAtLimit(t *testing.T) {
	server := threePages()
	ts := newTestServer(t, server)

	issues, err := testClient(Config{BaseURL: ts.URL}).ListIssues(context.Background(), "o/r", 3, time.Time{})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if got := issueNumbers(issues); !reflect.DeepEqual(got, []int{1, 2, 3}) {
		t.Errorf("numbers = %v, want [1 2 3]", got)
	}
	if len(server.requests) != 2 {
		t.Errorf("%d requests, want 2: the third page must not be fetched", len(server.requests))
	}
}

func TestListIssues_StopsWithoutNextLink(t *testing.T) {
	server := &pagedServer{pages: map[string]string{"1": issuesJSON(1)}}
	ts := newTestServer(t, server)

	if _, err := testClient(Config{BaseURL: ts.URL}).ListIssues(context.Background(), "o/r", 100, time.Time{}); err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if len(server.requests) != 1 {
		t.Errorf("%d requests, want 1", len(server.requests))
	}
}

func TestListIssues_RemovesDuplicatesAcrossPages(t *testing.T) {
	server := threePages()
	server.pages["2"] = issuesJSON(2, 3, 4) // issue 2 shifted onto page 2
	ts := newTestServer(t, server)

	issues, err := testClient(Config{BaseURL: ts.URL}).ListIssues(context.Background(), "o/r", 100, time.Time{})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if got := issueNumbers(issues); !reflect.DeepEqual(got, []int{1, 2, 3, 4, 5}) {
		t.Errorf("numbers = %v", got)
	}
}

func TestListIssues_LoopingNextLink(t *testing.T) {
	server := threePages()
	server.next["2"] = "/repos/o/r/issues?per_page=100&page=2" // points at itself
	ts := newTestServer(t, server)

	_, err := testClient(Config{BaseURL: ts.URL}).ListIssues(context.Background(), "o/r", 100, time.Time{})
	if !errors.Is(err, ErrInvalidResponse) {
		t.Errorf("error = %v, want ErrInvalidResponse", err)
	}
}

func TestListIssues_ErrorOnLaterPage(t *testing.T) {
	server := threePages()
	server.status = map[string]int{"2": http.StatusNotFound}
	ts := newTestServer(t, server)

	issues, err := testClient(Config{BaseURL: ts.URL}).ListIssues(context.Background(), "o/r", 100, time.Time{})
	if !errors.Is(err, ErrNotFound) || issues != nil {
		t.Errorf("ListIssues = %v, %v; want no issues and ErrNotFound", issues, err)
	}
}

func TestListIssues_InvalidItem(t *testing.T) {
	server := &pagedServer{pages: map[string]string{"1": `[{"number":"one"}]`}}
	ts := newTestServer(t, server)

	_, err := testClient(Config{BaseURL: ts.URL}).ListIssues(context.Background(), "o/r", 100, time.Time{})
	if !errors.Is(err, ErrInvalidResponse) || CodeOf(err) != report.InvalidResponse {
		t.Errorf("error = %v, want ErrInvalidResponse", err)
	}
}
