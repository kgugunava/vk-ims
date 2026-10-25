package ghapi

import (
	"context"
	"fmt"
	"time"

	"github.com/kgugunava/vk-ims/service/internal/report"
)

// ListIssues collects up to `limit` open issues of repo created at or after
// `since`, following Link rel="next" page by page and stopping as soon as the
// limit is reached. The result is sorted by created_at, then by number.
func (c *Client) ListIssues(ctx context.Context, repo string, limit int, since time.Time) ([]report.Issue, error) {
	collector := report.NewCollector(limit, since)
	visited := make(map[string]bool)
	target := FirstPageURL(c.baseURL, repo)
	for {
		// A next link that points back to a page already seen would loop
		// forever; it is a broken response, not a reason to hang.
		if visited[target] {
			return nil, fmt.Errorf("%w: Link rel=next returns to %s", ErrInvalidResponse, target)
		}
		visited[target] = true

		page, err := c.FetchPage(ctx, target)
		if err != nil {
			return nil, err
		}
		if err := collector.Add(page.Items); err != nil {
			return nil, fmt.Errorf("%s: %w: %w", target, ErrInvalidResponse, err)
		}
		if collector.Full() || page.NextURL == "" {
			return collector.Issues(), nil
		}
		target = page.NextURL
	}
}
