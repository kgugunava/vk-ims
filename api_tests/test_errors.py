"""Coverage 6: error handling and exit codes."""

from __future__ import annotations

import json

import pytest

from conftest import report_of, run_ghreport, run_ghreport_args
from support.runner import read_trace


def entry_for(proc, repo: str) -> dict:
    report = json.loads(proc.stdout)
    return next(entry for entry in report["repos"] if entry["repo"] == repo)


def test_401_is_fatal_with_exit_code_3(proxy, repo, cache_dir):
    proxy.fail(401, body=json.dumps({"message": "Bad credentials"}).encode())
    proc = run_ghreport(repo, cache_dir, base=proxy.url)

    assert proc.returncode == 3, f"exit code {proc.returncode}\nstderr:\n{proc.stderr}"
    assert json.loads(proc.stdout) == {"error": "unauthorized"}


def test_401_on_a_later_repository_discards_the_report(proxy, repo, alias_repo, cache_dir):
    proxy.fail(401, path_contains=alias_repo)
    proc = run_ghreport(f"{repo},{alias_repo}", cache_dir, base=proxy.url)

    assert proc.returncode == 3
    assert json.loads(proc.stdout) == {"error": "unauthorized"}


def test_404_marks_repo_and_exit_code_1(repo, missing_repo, cache_dir):
    proc = run_ghreport(f"{repo},{missing_repo}", cache_dir)

    assert proc.returncode == 1, f"exit code {proc.returncode}\nstderr:\n{proc.stderr}"
    assert entry_for(proc, repo)["error"] is None
    assert entry_for(proc, missing_repo) == {"repo": missing_repo, "error": "not_found", "count": 0, "issues": []}


def test_all_repositories_failing_is_fatal(missing_repo, cache_dir):
    proc = run_ghreport(missing_repo, cache_dir)

    assert proc.returncode == 3
    assert json.loads(proc.stdout) == {"error": "all_failed"}


def test_invalid_json_marks_repo(proxy, repo, alias_repo, cache_dir):
    proxy.corrupt(path_contains=alias_repo)
    proc = run_ghreport(f"{repo},{alias_repo}", cache_dir, base=proxy.url)

    assert proc.returncode == 1, f"exit code {proc.returncode}\nstderr:\n{proc.stderr}"
    assert entry_for(proc, repo)["error"] is None
    assert entry_for(proc, alias_repo) == {"repo": alias_repo, "error": "invalid_response", "count": 0, "issues": []}


def test_server_error_is_retried(proxy, repo, cache_dir, trace_path):
    proxy.fail(500, times=1)
    report_of(run_ghreport(repo, cache_dir, "--trace", str(trace_path), base=proxy.url))

    first, second = read_trace(trace_path)[:2]
    assert (first["status"], first["attempt"]) == (500, 1)
    assert (second["url"], second["status"], second["attempt"]) == (first["url"], 200, 2)


def test_persistent_server_error_fails_repo(proxy, repo, alias_repo, cache_dir, trace_path):
    proxy.fail(503, path_contains=alias_repo)
    proc = run_ghreport(f"{repo},{alias_repo}", cache_dir, "--trace", str(trace_path), base=proxy.url)

    assert proc.returncode == 1
    assert entry_for(proc, alias_repo)["error"] == "request_failed"
    attempts = [entry["attempt"] for entry in read_trace(trace_path) if alias_repo in entry["url"]]
    assert attempts == [1, 2, 3, 4], f"attempts {attempts}, want the request and three retries"


@pytest.mark.parametrize(
    "args",
    [
        pytest.param([], id="no-repos"),
        pytest.param(["--repos", "o/r", "--limit", "0"], id="limit-zero"),
        pytest.param(["--repos", "o/r", "--limit", "x"], id="limit-not-a-number"),
        pytest.param(["--repos", "o/r", "--since", "2026-13-01"], id="since-not-a-date"),
        pytest.param(["--repos", "not-a-repo"], id="repo-without-owner"),
        pytest.param(["--repos", "o/r", "--verbose"], id="unknown-flag"),
        pytest.param(["--repos", "o/r", "extra"], id="positional-argument"),
    ],
)
def test_invalid_arguments_exit_2_with_empty_stdout(args):
    proc = run_ghreport_args(*args)
    assert proc.returncode == 2, f"{args}: exit code {proc.returncode}"
    assert proc.stdout == "", f"{args}: stdout {proc.stdout!r}, want empty"
    assert proc.stderr.strip(), f"{args}: no message on stderr"
