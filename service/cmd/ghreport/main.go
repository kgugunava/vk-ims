// Command ghreport writes a JSON summary of the open issues of GitHub
// repositories.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/kgugunava/vk-ims/service/internal/cache"
	"github.com/kgugunava/vk-ims/service/internal/cli"
	"github.com/kgugunava/vk-ims/service/internal/ghapi"
	"github.com/kgugunava/vk-ims/service/internal/report"
	"github.com/kgugunava/vk-ims/service/internal/trace"
)

// Exit codes are part of the CLI contract.
const (
	exitOK      = 0 // every repository succeeded
	exitPartial = 1 // some repositories failed; also when the report cannot be written
	exitUsage   = 2 // invalid arguments; stdout stays empty
	exitFatal   = 3 // 401, or every repository failed; stdout holds {"error": ...}
)

func main() {
	// Ctrl+C or SIGTERM cancels the context, which ends a pending pause or
	// request instead of killing the process mid-write.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr, os.Getenv)
	stop()
	os.Exit(code)
}

// run is main without the process around it, so tests can call it directly.
// stdout gets only the report or a fatal error object; diagnostics go to
// stderr.
func run(ctx context.Context, args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	cfg, err := cli.Parse(args, getenv)
	if errors.Is(err, cli.ErrHelp) {
		fmt.Fprint(stderr, cli.Usage)
		return exitOK
	}
	var usageErr *cli.UsageError
	if errors.As(err, &usageErr) {
		fmt.Fprintf(stderr, "ghreport: %s\n", usageErr.Message)
		return exitUsage
	}

	logger := slog.New(slog.NewTextHandler(stderr, nil))
	generatedAt := time.Now()

	var traceWriter *trace.Writer
	if cfg.Trace != "" {
		if traceWriter, err = trace.Create(cfg.Trace); err != nil {
			fmt.Fprintf(stderr, "ghreport: %v\n", err)
			return exitPartial
		}
		defer func() {
			if err := traceWriter.Close(); err != nil {
				logger.Warn("trace incomplete", "error", err)
			}
		}()
	}

	// Without a cache the run still works, only without conditional requests.
	store, err := cache.Open(cfg.CacheDir)
	if err != nil {
		logger.Warn("cache disabled", "error", err)
	}

	client := ghapi.New(ghapi.Config{
		BaseURL: cfg.BaseURL,
		Token:   cfg.Token,
		Cache:   store,
		Trace:   traceWriter,
		Logger:  logger,
	})

	result := report.New(generatedAt)
	failed := 0
	for _, repo := range cfg.Repos {
		issues, err := client.ListIssues(ctx, repo, cfg.Limit, cfg.Since)
		switch {
		case err == nil:
			result.Repos = append(result.Repos, report.Succeeded(repo, issues))
		case errors.Is(err, ghapi.ErrUnauthorized):
			// A bad token fails every repository the same way: stop at once.
			logger.Error("unauthorized", "repo", repo, "error", err)
			return writeFatal(stdout, stderr, report.FatalUnauthorized)
		case ctx.Err() != nil:
			fmt.Fprintf(stderr, "ghreport: interrupted\n")
			return exitPartial
		default:
			failed++
			logger.Warn("repository failed", "repo", repo, "error", err)
			result.Repos = append(result.Repos, report.Failed(repo, ghapi.CodeOf(err)))
		}
	}

	if failed == len(cfg.Repos) {
		return writeFatal(stdout, stderr, report.FatalAllFailed)
	}
	if err := writeReport(cfg.Out, stdout, result); err != nil {
		fmt.Fprintf(stderr, "ghreport: %v\n", err)
		return exitPartial
	}
	if failed > 0 {
		return exitPartial
	}
	return exitOK
}

// writeFatal prints the error object that replaces the report. It always
// goes to stdout, even with --out, and no report file is written.
func writeFatal(stdout, stderr io.Writer, code string) int {
	if err := report.EncodeFatal(stdout, code); err != nil {
		fmt.Fprintf(stderr, "ghreport: write stdout: %v\n", err)
	}
	return exitFatal
}

// writeReport writes to stdout for "-", otherwise to a file, replacing it
// atomically so that a failed run never leaves a truncated report behind.
func writeReport(out string, stdout io.Writer, r report.Report) error {
	if out == cli.DefaultOut {
		if err := report.Encode(stdout, r); err != nil {
			return fmt.Errorf("write stdout: %w", err)
		}
		return nil
	}

	tmp, err := os.CreateTemp(filepath.Dir(out), ".ghreport-*.tmp")
	if err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	defer os.Remove(tmp.Name()) // no-op once renamed

	if err := report.Encode(tmp, r); err != nil {
		_ = tmp.Close() // the encoding error is the one to report
		return fmt.Errorf("write report: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	if err := os.Rename(tmp.Name(), out); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	return nil
}
