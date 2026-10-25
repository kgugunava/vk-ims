// Package cli turns ghreport's command line and environment into a Config.
package cli

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Defaults from the task's flag table.
const (
	DefaultLimit    = 100
	DefaultOut      = "-"
	DefaultCacheDir = "./.ghreport-cache"
	DefaultBaseURL  = "https://api.github.com"
)

// Usage is printed for --help.
const Usage = `usage: ghreport --repos <owner/repo[,owner/repo...]>
                [--limit <N>] [--since <YYYY-MM-DD>]
                [--out <file|->] [--trace <file>] [--cache-dir <dir>]

environment:
  GITHUB_TOKEN          optional token, sent as "Authorization: Bearer"
  GITHUB_API_BASE_URL   API base URL, default https://api.github.com
`

// Config is the validated configuration of one run.
type Config struct {
	Repos    []string  // owner/repo, duplicates removed, in argument order
	Limit    int       // maximum issues per repository, at least 1
	Since    time.Time // start of a UTC day; zero if --since is not given
	Out      string    // report file; "-" means stdout
	Trace    string    // JSON Lines trace file; empty means no trace
	CacheDir string
	BaseURL  string // absolute http(s) URL without a trailing slash
	Token    string // empty means unauthenticated requests
}

// UsageError is an invalid argument or environment value. ghreport prints it
// to stderr and exits with code 2.
type UsageError struct {
	Message string
}

func (e *UsageError) Error() string { return e.Message }

// ErrHelp is returned for -h and --help.
var ErrHelp = errors.New("help requested")

var repoPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$`)

var knownFlags = map[string]bool{
	"repos": true, "limit": true, "since": true, "out": true, "trace": true, "cache-dir": true,
}

// Parse validates args (without the program name) and the environment read
// through getenv. It returns ErrHelp or a *UsageError on failure.
func Parse(args []string, getenv func(string) string) (Config, error) {
	values, err := parseFlags(args)
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		Limit:    DefaultLimit,
		Out:      DefaultOut,
		CacheDir: DefaultCacheDir,
		Trace:    values["trace"],
		Token:    strings.TrimSpace(getenv("GITHUB_TOKEN")),
	}

	repos, ok := values["repos"]
	if !ok {
		return Config{}, usage("--repos обязателен")
	}
	if cfg.Repos, err = parseRepos(repos); err != nil {
		return Config{}, err
	}

	if limit, ok := values["limit"]; ok {
		n, err := strconv.Atoi(limit)
		if err != nil || n < 1 {
			return Config{}, usage("--limit должен быть целым числом, не меньшим единицы")
		}
		cfg.Limit = n
	}

	if since, ok := values["since"]; ok {
		day, err := time.ParseInLocation("2006-01-02", since, time.UTC)
		if err != nil {
			return Config{}, usage("--since должен быть датой в формате YYYY-MM-DD, получено %q", since)
		}
		cfg.Since = day
	}

	for _, name := range []string{"out", "trace", "cache-dir"} {
		if value, ok := values[name]; ok && value == "" {
			return Config{}, usage("--%s не может быть пустым", name)
		}
	}
	if out, ok := values["out"]; ok {
		cfg.Out = out
	}
	if dir, ok := values["cache-dir"]; ok {
		cfg.CacheDir = dir
	}

	if cfg.BaseURL, err = parseBaseURL(getenv("GITHUB_API_BASE_URL")); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// parseFlags accepts "--name value", "--name=value" and the single-dash forms.
// It is hand-written rather than built on the flag package because that
// package keeps the last of repeated flags silently and words its errors in
// English, while the task's example error is in Russian.
func parseFlags(args []string) (map[string]string, error) {
	values := make(map[string]string, len(args)/2)
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "-h" || arg == "--help" || arg == "-help" {
			return nil, ErrHelp
		}
		if !strings.HasPrefix(arg, "-") {
			return nil, usage("лишний аргумент %q", arg)
		}

		name, value, hasValue := strings.Cut(strings.TrimLeft(arg, "-"), "=")
		if !knownFlags[name] {
			return nil, usage("неизвестный флаг %q", arg)
		}
		if _, repeated := values[name]; repeated {
			return nil, usage("флаг --%s указан повторно", name)
		}
		if !hasValue {
			if i+1 >= len(args) {
				return nil, usage("флагу --%s нужно значение", name)
			}
			i++
			value = args[i]
		}
		values[name] = value
	}
	return values, nil
}

func parseRepos(list string) ([]string, error) {
	var repos []string
	seen := make(map[string]bool)
	for _, part := range strings.Split(list, ",") {
		repo := strings.TrimSpace(part)
		if !repoPattern.MatchString(repo) {
			return nil, usage("--repos: %q не в формате owner/repo", repo)
		}
		// Exact comparison: the report shows names as given, so differently
		// cased spellings of one repository are kept apart.
		if !seen[repo] {
			seen[repo] = true
			repos = append(repos, repo)
		}
	}
	return repos, nil
}

func parseBaseURL(raw string) (string, error) {
	if raw == "" {
		return DefaultBaseURL, nil
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", usage("GITHUB_API_BASE_URL должен быть абсолютным адресом http или https, получено %q", raw)
	}
	// A trailing slash is allowed; the path prefix, if any, is kept.
	return strings.TrimRight(raw, "/"), nil
}

func usage(format string, args ...any) *UsageError {
	return &UsageError{Message: fmt.Sprintf(format, args...)}
}
