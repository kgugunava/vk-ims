// Package ghapi talks to the GitHub issues endpoint: requests, pagination,
// conditional requests, retries and rate limits.
package ghapi

import (
	"errors"

	"github.com/kgugunava/vk-ims/service/internal/report"
)

// Errors returned for a repository. ErrUnauthorized aborts the whole run;
// the others become the repository's error code in the report.
var (
	ErrUnauthorized    = errors.New("unauthorized")
	ErrNotFound        = errors.New("not found")
	ErrRateLimited     = errors.New("rate limited")
	ErrRequestFailed   = errors.New("request failed")
	ErrInvalidResponse = errors.New("invalid response")
)

// CodeOf maps an error from ListIssues to the report's error code. Errors
// that are not one of the above count as request failures.
func CodeOf(err error) report.ErrorCode {
	switch {
	case errors.Is(err, ErrNotFound):
		return report.NotFound
	case errors.Is(err, ErrRateLimited):
		return report.RateLimited
	case errors.Is(err, ErrInvalidResponse):
		return report.InvalidResponse
	default:
		return report.RequestFailed
	}
}
