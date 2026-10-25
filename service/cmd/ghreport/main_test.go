package main

import (
	"bytes"
	"strings"
	"testing"
)

func noEnv(string) string { return "" }

func TestRun_UsageErrorKeepsStdoutEmpty(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--repos", "a/b", "--limit", "0"}, &stdout, &stderr, noEnv)

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
	code := run([]string{"--help"}, &stdout, &stderr, noEnv)

	if code != exitOK {
		t.Errorf("exit code = %d, want %d", code, exitOK)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "usage: ghreport") {
		t.Errorf("stderr = %q, want usage", stderr.String())
	}
}
