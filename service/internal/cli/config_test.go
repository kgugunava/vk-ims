package cli

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func env(values map[string]string) func(string) string {
	return func(name string) string { return values[name] }
}

func TestParse_Defaults(t *testing.T) {
	cfg, err := Parse([]string{"--repos", "cli/cli"}, env(nil))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := Config{
		Repos:    []string{"cli/cli"},
		Limit:    100,
		Out:      "-",
		CacheDir: "./.ghreport-cache",
		BaseURL:  "https://api.github.com",
	}
	if !reflect.DeepEqual(cfg, want) {
		t.Errorf("Parse defaults:\n got  %+v\n want %+v", cfg, want)
	}
}

func TestParse_Values(t *testing.T) {
	args := []string{
		"--repos", " a/b , c.d/e-f_g ,a/b", "--limit=5", "-since", "2026-01-05",
		"--out", "report.json", "--trace", "t.jsonl", "--cache-dir", "/tmp/c",
	}
	cfg, err := Parse(args, env(map[string]string{
		"GITHUB_TOKEN":        "  secret  ",
		"GITHUB_API_BASE_URL": "http://h:1/api/v3/",
	}))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := Config{
		Repos:    []string{"a/b", "c.d/e-f_g"},
		Limit:    5,
		Since:    time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC),
		Out:      "report.json",
		Trace:    "t.jsonl",
		CacheDir: "/tmp/c",
		BaseURL:  "http://h:1/api/v3",
		Token:    "secret",
	}
	if !reflect.DeepEqual(cfg, want) {
		t.Errorf("Parse:\n got  %+v\n want %+v", cfg, want)
	}
}

func TestParse_KeepsDifferentlyCasedRepos(t *testing.T) {
	cfg, err := Parse([]string{"--repos", "a/b,A/B"}, env(nil))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if want := []string{"a/b", "A/B"}; !reflect.DeepEqual(cfg.Repos, want) {
		t.Errorf("Repos = %q, want %q", cfg.Repos, want)
	}
}

func TestParse_UsageErrors(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		env         map[string]string
		wantMessage string // substring of the error message
	}{
		{"no repos", []string{"--limit", "1"}, nil, "--repos обязателен"},
		{"empty repos", []string{"--repos", ""}, nil, "owner/repo"},
		{"only a comma", []string{"--repos", ","}, nil, "owner/repo"},
		{"empty item", []string{"--repos", "a/b,,c/d"}, nil, "owner/repo"},
		{"no slash", []string{"--repos", "a"}, nil, "owner/repo"},
		{"two slashes", []string{"--repos", "a/b/c"}, nil, "owner/repo"},
		{"no owner", []string{"--repos", "/b"}, nil, "owner/repo"},
		{"no name", []string{"--repos", "a/"}, nil, "owner/repo"},
		{"space inside", []string{"--repos", "a b/c"}, nil, "owner/repo"},
		{"limit zero", []string{"--repos", "a/b", "--limit", "0"}, nil, limitMessage},
		{"limit negative", []string{"--repos", "a/b", "--limit", "-1"}, nil, limitMessage},
		{"limit not a number", []string{"--repos", "a/b", "--limit", "abc"}, nil, limitMessage},
		{"limit fraction", []string{"--repos", "a/b", "--limit", "1.5"}, nil, limitMessage},
		{"limit empty", []string{"--repos", "a/b", "--limit="}, nil, limitMessage},
		{"since not a date", []string{"--repos", "a/b", "--since", "2026-02-30"}, nil, "--since"},
		{"since no padding", []string{"--repos", "a/b", "--since", "2026-1-5"}, nil, "--since"},
		{"since other format", []string{"--repos", "a/b", "--since", "05.01.2026"}, nil, "--since"},
		{"empty out", []string{"--repos", "a/b", "--out="}, nil, "--out"},
		{"empty cache dir", []string{"--repos", "a/b", "--cache-dir", ""}, nil, "--cache-dir"},
		{"unknown flag", []string{"--repos", "a/b", "--verbose", "x"}, nil, "неизвестный флаг"},
		{"positional argument", []string{"--repos", "a/b", "extra"}, nil, "лишний аргумент"},
		{"repeated flag", []string{"--repos", "a/b", "--limit", "1", "--limit", "2"}, nil, "повторно"},
		{"missing value", []string{"--repos"}, nil, "нужно значение"},
		{"base url without scheme", []string{"--repos", "a/b"}, map[string]string{"GITHUB_API_BASE_URL": "h:1"}, "GITHUB_API_BASE_URL"},
		{"base url ftp", []string{"--repos", "a/b"}, map[string]string{"GITHUB_API_BASE_URL": "ftp://h"}, "GITHUB_API_BASE_URL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(tt.args, env(tt.env))
			var usageErr *UsageError
			if !errors.As(err, &usageErr) {
				t.Fatalf("Parse(%q) error = %v, want *UsageError", tt.args, err)
			}
			if !strings.Contains(usageErr.Message, tt.wantMessage) {
				t.Errorf("Parse(%q) message = %q, want it to contain %q", tt.args, usageErr.Message, tt.wantMessage)
			}
		})
	}
}

// The task gives this exact message as an example.
const limitMessage = "--limit должен быть целым числом, не меньшим единицы"

func TestParse_Help(t *testing.T) {
	for _, arg := range []string{"-h", "--help"} {
		if _, err := Parse([]string{arg}, env(nil)); !errors.Is(err, ErrHelp) {
			t.Errorf("Parse(%q) error = %v, want ErrHelp", arg, err)
		}
	}
}
