package ghapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	// Pause when the response says nothing about when to retry.
	fallbackRateLimitPause = 60 * time.Second
	// A reset time already in the past still gets a short pause, so a server
	// that keeps answering this way is not hammered.
	minResetPause = 1 * time.Second
	// Longer pauses are not waited out: the repository is reported as
	// rate_limited instead. Primary limits reset within an hour, which is too
	// long for a run that may be waiting on the user.
	maxRateLimitPause = 5 * time.Minute
	// Rate-limit pauses per request before giving up.
	maxRateLimitWaits = 3
)

// IsRateLimited reports whether a response means the rate limit is used up:
// status 403 or 429, together with any of a body message about the rate
// limit, a Retry-After header or X-RateLimit-Remaining equal to 0. Other 403s
// (no access to the repository) are not rate limiting.
func IsRateLimited(status int, header http.Header, body []byte) bool {
	if status != http.StatusForbidden && status != http.StatusTooManyRequests {
		return false
	}
	if header.Get("Retry-After") != "" || header.Get("X-RateLimit-Remaining") == "0" {
		return true
	}
	// GitHub puts the reason in "message": "API rate limit exceeded ..." for
	// the primary limit, "You have exceeded a secondary rate limit" otherwise.
	message := string(body)
	var payload struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &payload) == nil && payload.Message != "" {
		message = payload.Message
	}
	return strings.Contains(strings.ToLower(message), "rate limit")
}

// RateLimitPause picks the wait before retrying a rate-limited request:
// Retry-After (seconds, or an HTTP date), else until X-RateLimit-Reset (Unix
// seconds), else a minute.
func RateLimitPause(header http.Header, now time.Time) time.Duration {
	if value := strings.TrimSpace(header.Get("Retry-After")); value != "" {
		if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
			return time.Duration(seconds) * time.Second
		}
		if at, err := http.ParseTime(value); err == nil {
			return max(at.Sub(now), 0)
		}
	}
	if value := strings.TrimSpace(header.Get("X-RateLimit-Reset")); value != "" {
		if epoch, err := strconv.ParseInt(value, 10, 64); err == nil {
			return max(time.Unix(epoch, 0).Sub(now), minResetPause)
		}
	}
	return fallbackRateLimitPause
}
