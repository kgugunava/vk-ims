package report

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"
)

// item builds an API issue object; extra fields are merged in, and a nil
// value deletes a field.
func item(number int, createdAt string, extra map[string]any) json.RawMessage {
	fields := map[string]any{
		"number":     number,
		"title":      fmt.Sprintf("issue %d", number),
		"user":       map[string]any{"login": "octocat"},
		"labels":     []any{map[string]any{"name": "bug"}, map[string]any{"name": "p1"}},
		"created_at": createdAt,
		"html_url":   fmt.Sprintf("https://github.com/o/r/issues/%d", number),
	}
	for key, value := range extra {
		if value == nil {
			delete(fields, key)
		} else {
			fields[key] = value
		}
	}
	raw, err := json.Marshal(fields)
	if err != nil {
		panic(err)
	}
	return raw
}

func numbers(issues []Issue) []int {
	out := make([]int, 0, len(issues))
	for _, issue := range issues {
		out = append(out, issue.Number)
	}
	return out
}

func collect(t *testing.T, limit int, since time.Time, pages ...[]json.RawMessage) []Issue {
	t.Helper()
	c := NewCollector(limit, since)
	for _, page := range pages {
		if err := c.Add(page); err != nil {
			t.Fatalf("Add: %v", err)
		}
	}
	return c.Issues()
}

func TestCollector_MapsFields(t *testing.T) {
	got := collect(t, 10, time.Time{}, []json.RawMessage{item(7, "2026-01-05T10:00:00Z", nil)})
	want := []Issue{{
		Number:    7,
		Title:     "issue 7",
		Author:    "octocat",
		Labels:    []string{"bug", "p1"},
		CreatedAt: "2026-01-05T10:00:00Z",
		URL:       "https://github.com/o/r/issues/7",
		created:   time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC),
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("issues:\n got  %+v\n want %+v", got, want)
	}
}

func TestCollector_KeepsCreatedAtAsGiven(t *testing.T) {
	got := collect(t, 10, time.Time{}, []json.RawMessage{item(1, "2026-01-05T13:00:00+03:00", nil)})
	if got[0].CreatedAt != "2026-01-05T13:00:00+03:00" {
		t.Errorf("CreatedAt = %q, want the API string unchanged", got[0].CreatedAt)
	}
}

func TestCollector_ExcludesPullRequests(t *testing.T) {
	page := []json.RawMessage{
		item(1, "2026-01-01T00:00:00Z", map[string]any{"pull_request": map[string]any{"url": "u"}}),
		item(2, "2026-01-02T00:00:00Z", nil),
	}
	// The key alone marks a pull request, even with a null value.
	withNull := json.RawMessage(`{"number":3,"title":"t","created_at":"2026-01-03T00:00:00Z",` +
		`"html_url":"u","pull_request":null}`)
	page = append(page, withNull)

	if got := numbers(collect(t, 10, time.Time{}, page)); !reflect.DeepEqual(got, []int{2}) {
		t.Errorf("numbers = %v, want [2]", got)
	}
}

func TestCollector_Since(t *testing.T) {
	since := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	page := []json.RawMessage{
		item(1, "2026-01-04T23:59:59Z", nil),      // one second before: excluded
		item(2, "2026-01-05T00:00:00Z", nil),      // exactly midnight: included
		item(3, "2026-01-05T02:00:00+03:00", nil), // 2026-01-04T23:00Z: excluded
		item(4, "2026-01-06T00:00:00Z", nil),
	}
	if got := numbers(collect(t, 10, since, page)); !reflect.DeepEqual(got, []int{2, 4}) {
		t.Errorf("numbers = %v, want [2 4]", got)
	}
}

func TestCollector_RemovesRepeatedNumbers(t *testing.T) {
	first := []json.RawMessage{item(1, "2026-01-01T00:00:00Z", nil), item(2, "2026-01-02T00:00:00Z", nil)}
	// The second page repeats issue 2, as happens when the list shifts.
	second := []json.RawMessage{
		item(2, "2026-01-02T00:00:00Z", map[string]any{"title": "repeated"}),
		item(3, "2026-01-03T00:00:00Z", nil),
	}
	got := collect(t, 10, time.Time{}, first, second)
	if !reflect.DeepEqual(numbers(got), []int{1, 2, 3}) {
		t.Fatalf("numbers = %v, want [1 2 3]", numbers(got))
	}
	if got[1].Title != "issue 2" {
		t.Errorf("issue 2 title = %q, want the first occurrence", got[1].Title)
	}
}

func TestCollector_StopsAtLimitMidPage(t *testing.T) {
	c := NewCollector(2, time.Time{})
	page := []json.RawMessage{
		item(1, "2026-01-01T00:00:00Z", map[string]any{"pull_request": map[string]any{}}),
		item(2, "2026-01-02T00:00:00Z", nil),
		item(3, "2026-01-03T00:00:00Z", nil),
		item(4, "2026-01-04T00:00:00Z", nil),
	}
	if err := c.Add(page); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if !c.Full() {
		t.Error("Full() = false after reaching the limit")
	}
	if got := numbers(c.Issues()); !reflect.DeepEqual(got, []int{2, 3}) {
		t.Errorf("numbers = %v, want [2 3]", got)
	}
}

func TestCollector_SortsByCreatedThenNumber(t *testing.T) {
	page := []json.RawMessage{
		item(5, "2026-01-03T00:00:00Z", nil),
		item(4, "2026-01-02T00:00:00Z", nil),
		item(9, "2026-01-01T00:00:00Z", nil),
		item(2, "2026-01-02T00:00:00Z", nil),
		// Same instant written with an offset still sorts by time.
		item(1, "2026-01-02T03:00:00+03:00", nil),
	}
	if got := numbers(collect(t, 10, time.Time{}, page)); !reflect.DeepEqual(got, []int{9, 1, 2, 4, 5}) {
		t.Errorf("numbers = %v, want [9 1 2 4 5]", got)
	}
}

func TestCollector_UserAndLabels(t *testing.T) {
	page := []json.RawMessage{
		item(1, "2026-01-01T00:00:00Z", map[string]any{"user": nil}),
		json.RawMessage(`{"number":2,"title":"t","user":null,"labels":[{"name":"a"},{"color":"x"},{"name":"b"}],` +
			`"created_at":"2026-01-02T00:00:00Z","html_url":"u"}`),
	}
	got := collect(t, 10, time.Time{}, page)
	if got[0].Author != "" || got[1].Author != "" {
		t.Errorf("authors = %q, %q; want empty for a missing or null user", got[0].Author, got[1].Author)
	}
	if !reflect.DeepEqual(got[0].Labels, []string{"bug", "p1"}) {
		t.Errorf("labels = %q", got[0].Labels)
	}
	if !reflect.DeepEqual(got[1].Labels, []string{"a", "b"}) {
		t.Errorf("labels = %q, want a label without a name skipped", got[1].Labels)
	}
}

func TestCollector_InvalidItems(t *testing.T) {
	tests := []struct {
		name string
		raw  json.RawMessage
	}{
		{"not an object", json.RawMessage(`[1]`)},
		{"number is a string", item(1, "2026-01-01T00:00:00Z", map[string]any{"number": "1"})},
		{"number is a fraction", json.RawMessage(`{"number":1.5,"title":"t","created_at":"2026-01-01T00:00:00Z","html_url":"u"}`)},
		{"no number", item(1, "2026-01-01T00:00:00Z", map[string]any{"number": nil})},
		{"no title", item(1, "2026-01-01T00:00:00Z", map[string]any{"title": nil})},
		{"created_at not a time", item(1, "yesterday", nil)},
		{"no html_url", item(1, "2026-01-01T00:00:00Z", map[string]any{"html_url": nil})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NewCollector(10, time.Time{}).Add([]json.RawMessage{tt.raw})
			if !errors.Is(err, ErrInvalidItem) {
				t.Errorf("Add(%s) error = %v, want ErrInvalidItem", tt.raw, err)
			}
		})
	}
}

func TestCollector_EmptyIsNotNil(t *testing.T) {
	if got := NewCollector(10, time.Time{}).Issues(); got == nil {
		t.Error("Issues() = nil, want an empty slice so the report shows []")
	}
}
