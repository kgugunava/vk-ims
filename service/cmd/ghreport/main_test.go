package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// fakeAPI serves the issues of a few repositories; any other repository is
// 404. A repository named "o/unauthorized" answers 401, "o/limited" answers
// 429 with Retry-After: 0 once before its issues.
type fakeAPI struct {
	limitedOnce bool
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/repos/o/a/issues", "/repos/o/b/issues":
		w.Header().Set("ETag", `"v1"`) // so the run writes cache files
		fmt.Fprint(w, `[{"number":2,"title":"t2","user":{"login":"u"},"labels":[],"created_at":"2026-01-02T00:00:00Z","html_url":"h2"},`+
			`{"number":1,"title":"t1","user":{"login":"u"},"labels":[{"name":"bug"}],"created_at":"2026-01-01T00:00:00Z","html_url":"h1"}]`)
	case "/repos/o/limited/issues":
		if !f.limitedOnce {
			f.limitedOnce = true
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		fmt.Fprint(w, `[]`)
	case "/repos/o/unauthorized/issues":
		w.WriteHeader(http.StatusUnauthorized)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

type result struct {
	code           int
	stdout, stderr string
	dir            string // temporary directory holding cache and trace
}

func runGhreport(t *testing.T, args ...string) result {
	t.Helper()
	ts := httptest.NewServer(&fakeAPI{})
	t.Cleanup(ts.Close)
	dir := t.TempDir()
	env := map[string]string{"GITHUB_API_BASE_URL": ts.URL + "/", "GITHUB_TOKEN": "secret-token"}
	args = append([]string{"--cache-dir", filepath.Join(dir, "cache"), "--trace", filepath.Join(dir, "trace.jsonl")}, args...)

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), args, &stdout, &stderr, func(name string) string { return env[name] })
	return result{code: code, stdout: stdout.String(), stderr: stderr.String(), dir: dir}
}

type reportJSON struct {
	GeneratedAt string `json:"generated_at"`
	Repos       []struct {
		Repo   string  `json:"repo"`
		Error  *string `json:"error"`
		Count  int     `json:"count"`
		Issues []struct {
			Number int `json:"number"`
		} `json:"issues"`
	} `json:"repos"`
}

func decodeReport(t *testing.T, data string) reportJSON {
	t.Helper()
	var r reportJSON
	if err := json.Unmarshal([]byte(data), &r); err != nil {
		t.Fatalf("report is not JSON: %v\n%s", err, data)
	}
	return r
}

func TestRun_UsageErrorKeepsStdoutEmpty(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"--repos", "a/b", "--limit", "0"}, &stdout, &stderr, func(string) string { return "" })

	if code != exitUsage {
		t.Errorf("exit code = %d, want %d", code, exitUsage)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty", stdout.String())
	}
	want := "ghreport: --limit должен быть целым числом, не меньшим единицы\n"
	if stderr.String() != want {
		t.Errorf("stderr = %q, want %q", stderr.String(), want)
	}
}

func TestRun_HelpGoesToStderr(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"--help"}, &stdout, &stderr, func(string) string { return "" })
	if code != exitOK || stdout.Len() != 0 || !strings.Contains(stderr.String(), "usage: ghreport") {
		t.Errorf("code %d, stdout %q, stderr %q", code, stdout.String(), stderr.String())
	}
}

func TestRun_AllRepositoriesSucceed(t *testing.T) {
	res := runGhreport(t, "--repos", "o/b,o/a,o/b")
	if res.code != exitOK {
		t.Fatalf("exit code = %d, stderr:\n%s", res.code, res.stderr)
	}
	r := decodeReport(t, res.stdout)
	if len(r.Repos) != 2 || r.Repos[0].Repo != "o/b" || r.Repos[1].Repo != "o/a" {
		t.Fatalf("repos = %+v, want o/b then o/a once each", r.Repos)
	}
	for _, repo := range r.Repos {
		if repo.Error != nil || repo.Count != 2 || repo.Issues[0].Number != 1 || repo.Issues[1].Number != 2 {
			t.Errorf("repo %+v, want two issues sorted by created_at", repo)
		}
	}
}

func TestRun_OneRepositoryMissing(t *testing.T) {
	res := runGhreport(t, "--repos", "o/a,o/missing")
	if res.code != exitPartial {
		t.Fatalf("exit code = %d, want %d", res.code, exitPartial)
	}
	r := decodeReport(t, res.stdout)
	missing := r.Repos[1]
	if r.Repos[0].Error != nil || missing.Error == nil || *missing.Error != "not_found" ||
		missing.Count != 0 || missing.Issues == nil || len(missing.Issues) != 0 {
		t.Errorf("repos = %+v; want o/missing with not_found, count 0 and issues []", r.Repos)
	}
}

func TestRun_AllRepositoriesFail(t *testing.T) {
	res := runGhreport(t, "--repos", "o/missing,o/gone")
	if res.code != exitFatal || res.stdout != `{"error":"all_failed"}`+"\n" {
		t.Errorf("code %d, stdout %q; want 3 and all_failed", res.code, res.stdout)
	}
}

func TestRun_UnauthorizedStopsTheRun(t *testing.T) {
	res := runGhreport(t, "--repos", "o/a,o/unauthorized,o/b")
	if res.code != exitFatal || res.stdout != `{"error":"unauthorized"}`+"\n" {
		t.Errorf("code %d, stdout %q; want 3 and unauthorized", res.code, res.stdout)
	}
}

func TestRun_RateLimitedThenSucceeds(t *testing.T) {
	res := runGhreport(t, "--repos", "o/limited")
	if res.code != exitOK {
		t.Errorf("exit code = %d, stderr:\n%s", res.code, res.stderr)
	}
}

func TestRun_WritesReportFile(t *testing.T) {
	out := filepath.Join(t.TempDir(), "report.json")
	res := runGhreport(t, "--repos", "o/a", "--out", out)
	if res.code != exitOK || res.stdout != "" {
		t.Fatalf("code %d, stdout %q; want 0 and nothing on stdout", res.code, res.stdout)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if r := decodeReport(t, string(data)); len(r.Repos) != 1 || r.Repos[0].Count != 2 {
		t.Errorf("report file = %s", data)
	}
	if leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(out), ".ghreport-*")); len(leftovers) != 0 {
		t.Errorf("temporary files left: %v", leftovers)
	}
}

func TestRun_UnwritableReportFile(t *testing.T) {
	out := filepath.Join(t.TempDir(), "missing", "report.json")
	res := runGhreport(t, "--repos", "o/a", "--out", out)
	if res.code != exitPartial || res.stdout != "" || !strings.Contains(res.stderr, "write report") {
		t.Errorf("code %d, stdout %q, stderr %q; want 1, nothing on stdout, an error on stderr",
			res.code, res.stdout, res.stderr)
	}
}

func TestRun_FatalErrorIgnoresOut(t *testing.T) {
	out := filepath.Join(t.TempDir(), "report.json")
	res := runGhreport(t, "--repos", "o/missing", "--out", out)
	if res.stdout != `{"error":"all_failed"}`+"\n" {
		t.Errorf("stdout = %q, want the fatal object", res.stdout)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Errorf("report file exists after a fatal error: %v", err)
	}
}

func TestRun_GeneratedAtIsUTC(t *testing.T) {
	r := decodeReport(t, runGhreport(t, "--repos", "o/a").stdout)
	if !strings.HasSuffix(r.GeneratedAt, "Z") {
		t.Errorf("generated_at = %q, want UTC", r.GeneratedAt)
	}
}

func TestRun_TokenNeverLeaks(t *testing.T) {
	res := runGhreport(t, "--repos", "o/a,o/missing,o/limited")
	outputs := map[string]string{"stdout": res.stdout, "stderr": res.stderr}
	err := filepath.Walk(res.dir, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			outputs[path] = string(data)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(outputs) < 4 {
		t.Fatalf("expected trace and cache files, got %v", reflect.ValueOf(outputs).MapKeys())
	}
	for name, content := range outputs {
		for _, secret := range []string{"secret-token", "Bearer", "Authorization"} {
			if strings.Contains(content, secret) {
				t.Errorf("%s contains %q", name, secret)
			}
		}
	}
}
