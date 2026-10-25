package report

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"
)

// ErrInvalidItem means an element of an API page is not an issue object of
// the expected shape.
var ErrInvalidItem = errors.New("invalid issue item")

// apiIssue holds the fields of an API item that the report uses. Pointers
// tell a missing field from a zero value.
type apiIssue struct {
	Number *int    `json:"number"`
	Title  *string `json:"title"`
	User   *struct {
		Login string `json:"login"`
	} `json:"user"`
	Labels []struct {
		Name *string `json:"name"`
	} `json:"labels"`
	CreatedAt *string `json:"created_at"`
	HTMLURL   *string `json:"html_url"`
}

// Collector accumulates the issues of one repository page by page: it drops
// pull requests, issues created before --since and repeated numbers, and
// stops at the limit.
type Collector struct {
	limit  int
	since  time.Time
	seen   map[int]bool
	issues []Issue
}

// NewCollector keeps at most `limit` issues created at or after `since`
// (zero means no lower bound).
func NewCollector(limit int, since time.Time) *Collector {
	return &Collector{limit: limit, since: since, seen: make(map[int]bool)}
}

// Full reports whether the limit has been reached.
func (c *Collector) Full() bool { return len(c.issues) >= c.limit }

// Add takes the items of one page in page order, until the limit is
// reached. It fails with ErrInvalidItem if an item does not have the shape of
// an issue.
func (c *Collector) Add(items []json.RawMessage) error {
	for i, raw := range items {
		if c.Full() {
			return nil
		}
		issue, keep, err := c.convert(raw)
		if err != nil {
			return fmt.Errorf("item %d: %w", i, err)
		}
		if keep {
			c.seen[issue.Number] = true
			c.issues = append(c.issues, issue)
		}
	}
	return nil
}

// Issues returns the collected issues sorted by created_at, then by number.
func (c *Collector) Issues() []Issue {
	issues := slices.Clone(c.issues)
	slices.SortStableFunc(issues, func(a, b Issue) int {
		if order := a.created.Compare(b.created); order != 0 {
			return order
		}
		return cmp.Compare(a.Number, b.Number)
	})
	if issues == nil {
		issues = []Issue{}
	}
	return issues
}

func (c *Collector) convert(raw json.RawMessage) (issue Issue, keep bool, err error) {
	// A pull request is recognized by the presence of the key, whatever its
	// value: decoding into a struct field would not tell a missing key from
	// "pull_request": null.
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return Issue{}, false, fmt.Errorf("%w: not an object", ErrInvalidItem)
	}
	if _, isPull := fields["pull_request"]; isPull {
		return Issue{}, false, nil
	}

	var item apiIssue
	if err := json.Unmarshal(raw, &item); err != nil {
		return Issue{}, false, fmt.Errorf("%w: %v", ErrInvalidItem, err)
	}
	if item.Number == nil || item.Title == nil || item.CreatedAt == nil || item.HTMLURL == nil {
		return Issue{}, false, fmt.Errorf("%w: number, title, created_at or html_url missing", ErrInvalidItem)
	}
	created, err := time.Parse(time.RFC3339, *item.CreatedAt)
	if err != nil {
		return Issue{}, false, fmt.Errorf("%w: created_at %q", ErrInvalidItem, *item.CreatedAt)
	}

	// --since is applied here because the API's own `since` parameter filters
	// by updated_at, not created_at.
	if !c.since.IsZero() && created.Before(c.since) {
		return Issue{}, false, nil
	}
	if c.seen[*item.Number] {
		return Issue{}, false, nil
	}

	issue = Issue{
		Number:    *item.Number,
		Title:     *item.Title,
		Labels:    []string{},
		CreatedAt: *item.CreatedAt,
		URL:       *item.HTMLURL,
		created:   created,
	}
	// A deleted account comes back as "user": null; the author is left empty.
	if item.User != nil {
		issue.Author = item.User.Login
	}
	for _, label := range item.Labels {
		if label.Name != nil {
			issue.Labels = append(issue.Labels, *label.Name)
		}
	}
	return issue, true, nil
}
