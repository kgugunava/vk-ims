"""Coverage 5: rate limits. The proxy answers with a rate-limit response once;
ghreport must wait as told and then finish successfully.

Only lower bounds on time are checked: an upper bound would make the tests
flaky on a slow machine, and a hung run is stopped by the run timeout.
"""

from __future__ import annotations

import json
import time

from conftest import report_of, run_ghreport
from support.runner import read_trace, without_generated_at

RATE_LIMIT_BODY = json.dumps({"message": "API rate limit exceeded"}).encode()


def test_waits_retry_after_on_429_and_succeeds(proxy, repo, tmp_path, trace_path):
    direct = report_of(run_ghreport(repo, tmp_path / "direct"))
    proxy.fail(429, headers={"Retry-After": "1"}, body=RATE_LIMIT_BODY, times=1)

    started = time.monotonic()
    proc = run_ghreport(repo, tmp_path / "cache", "--trace", str(trace_path), base=proxy.url)
    elapsed = time.monotonic() - started

    report = report_of(proc)
    assert elapsed >= 1.0, f"finished in {elapsed:.2f}s, before Retry-After: 1 allowed a retry"
    entries = read_trace(trace_path)
    assert entries[0]["status"] == 429 and entries[0]["attempt"] == 1, entries[0]
    assert entries[1]["url"] == entries[0]["url"], "the request after 429 is not a retry of the same URL"
    assert entries[1]["status"] == 200 and entries[1]["attempt"] == 2, entries[1]
    assert without_generated_at(report) == without_generated_at(direct)


def test_waits_until_reset_on_403_with_zero_remaining(proxy, repo, cache_dir):
    reset = int(time.time()) + 2
    proxy.fail(403, headers={"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": str(reset)},
               body=RATE_LIMIT_BODY, times=1)

    started = time.monotonic()
    proc = run_ghreport(repo, cache_dir, base=proxy.url)
    elapsed = time.monotonic() - started

    report_of(proc)
    assert elapsed >= 1.0, f"finished in {elapsed:.2f}s without waiting for X-RateLimit-Reset"


def test_rate_limit_on_a_later_page(proxy, reference, repo, cache_dir):
    issues = reference(repo).issues
    assert len(issues) >= 2, f"{repo} needs at least 2 open issues"
    limit = min(len(issues), 3)
    proxy.page_size(1)
    proxy.fail(429, headers={"Retry-After": "1"}, body=RATE_LIMIT_BODY, times=1, path_contains="page=2")

    report = report_of(run_ghreport(repo, cache_dir, "--limit", str(limit), base=proxy.url))
    assert report["repos"][0]["count"] == limit, "issues after the rate-limited page are missing"
