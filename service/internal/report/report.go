// Package report builds ghreport's output: issues taken from API pages and
// the JSON report or fatal error written at the end.
package report

import (
	"encoding/json"
	"io"
	"time"
)

// ErrorCode is the per-repository error written to the report.
type ErrorCode string

// The only error codes the report may contain.
const (
	NotFound        ErrorCode = "not_found"
	RateLimited     ErrorCode = "rate_limited"
	RequestFailed   ErrorCode = "request_failed"
	InvalidResponse ErrorCode = "invalid_response"
)

// Fatal errors replace the whole report.
const (
	FatalUnauthorized = "unauthorized"
	FatalAllFailed    = "all_failed"
)

// Issue is one entry of RepoResult.Issues.
type Issue struct {
	Number    int      `json:"number"`
	Title     string   `json:"title"`
	Author    string   `json:"author"`
	Labels    []string `json:"labels"`
	CreatedAt string   `json:"created_at"` // exactly as in the API response
	URL       string   `json:"url"`

	created time.Time // CreatedAt parsed, for --since and sorting
}

// RepoResult is the outcome for one repository.
type RepoResult struct {
	Repo   string     `json:"repo"`
	Error  *ErrorCode `json:"error"` // null on success
	Count  int        `json:"count"`
	Issues []Issue    `json:"issues"`
}

// Report is the document written to --out.
type Report struct {
	GeneratedAt string       `json:"generated_at"`
	Repos       []RepoResult `json:"repos"`
}

// Succeeded is the result of a repository whose issues were fetched.
func Succeeded(repo string, issues []Issue) RepoResult {
	if issues == nil {
		issues = []Issue{} // "issues": [] rather than null
	}
	return RepoResult{Repo: repo, Count: len(issues), Issues: issues}
}

// Failed is the result of a repository that could not be fetched.
func Failed(repo string, code ErrorCode) RepoResult {
	return RepoResult{Repo: repo, Error: &code, Issues: []Issue{}}
}

// New starts a report generated at `now`.
func New(now time.Time) Report {
	return Report{GeneratedAt: now.UTC().Format(time.RFC3339), Repos: []RepoResult{}}
}

// Encode writes the report as indented JSON with a trailing newline. HTML
// escaping is off: titles keep <, > and & as GitHub returns them.
func Encode(w io.Writer, r Report) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// EncodeFatal writes {"error": code} on one line, which replaces the report
// when the run fails as a whole.
func EncodeFatal(w io.Writer, code string) error {
	return json.NewEncoder(w).Encode(map[string]string{"error": code})
}
