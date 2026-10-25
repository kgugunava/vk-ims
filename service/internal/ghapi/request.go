package ghapi

import (
	"context"
	"net/http"
	"net/url"
)

// UserAgent identifies ghreport to GitHub, which rejects requests without one.
const UserAgent = "ghreport/1.0 (+https://github.com/kgugunava/vk-ims)"

// APIVersion is sent as X-GitHub-Api-Version.
const APIVersion = "2026-03-10"

// FirstPageURL is the only URL ghreport builds itself; every following page
// comes from the Link header. The parameter order matches the task's example.
func FirstPageURL(baseURL, repo string) string {
	return baseURL + "/repos/" + repo + "/issues?state=open&sort=created&direction=asc&per_page=100"
}

// newRequest builds a GET with the headers GitHub expects. The token is sent
// only to the API host itself, never to a host reached through a redirect or
// a Link header.
func (c *Client) newRequest(ctx context.Context, target, etag string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("X-GitHub-Api-Version", APIVersion)
	if c.token != "" && sameHost(target, c.baseURL) {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	return req, nil
}

func sameHost(a, b string) bool {
	ua, errA := url.Parse(a)
	ub, errB := url.Parse(b)
	return errA == nil && errB == nil && ua.Scheme == ub.Scheme && ua.Host == ub.Host
}
