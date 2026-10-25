"""Coverage 4: caching with ETag and If-None-Match."""

from __future__ import annotations

import uuid

from conftest import report_of, run_ghreport
from support.runner import read_trace, without_generated_at


def test_second_run_uses_cache(repo, cache_dir, tmp_path):
    first_trace, second_trace = tmp_path / "first.jsonl", tmp_path / "second.jsonl"
    first = report_of(run_ghreport(repo, cache_dir, "--trace", str(first_trace)))
    second = report_of(run_ghreport(repo, cache_dir, "--trace", str(second_trace)))

    for entry in read_trace(first_trace):
        assert entry["status"] == 200 and entry["from_cache"] is False, (
            f"first run with an empty cache: {entry}"
        )
    entries = read_trace(second_trace)
    assert entries, "the second run made no requests"
    for entry in entries:
        assert entry["status"] == 304 and entry["from_cache"] is True, (
            f"second run with the same cache: {entry}, want 304 from cache"
        )
    assert without_generated_at(second) == without_generated_at(first)


def test_second_run_sends_the_stored_etag(proxy, repo, cache_dir):
    report_of(run_ghreport(repo, cache_dir, base=proxy.url))
    first = list(proxy.requests)
    report_of(run_ghreport(repo, cache_dir, base=proxy.url))
    second = proxy.requests[len(first):]

    for request in first:
        assert "If-None-Match" not in request.headers, f"first run sent If-None-Match for {request.path}"
    served = {request.path: request.served_etag for request in first}
    for request in second:
        assert request.headers.get("If-None-Match") == served.get(request.path), (
            f"{request.path}: sent If-None-Match {request.headers.get('If-None-Match')!r}, "
            f"the server's ETag was {served.get(request.path)!r}"
        )


def test_fresh_cache_dir_does_not_hit_cache(repo, tmp_path):
    report_of(run_ghreport(repo, tmp_path / "one"))
    trace = tmp_path / "trace.jsonl"
    report_of(run_ghreport(repo, tmp_path / "two", "--trace", str(trace)))
    for entry in read_trace(trace):
        assert entry["status"] == 200 and entry["from_cache"] is False, (
            f"a run with its own empty cache: {entry}"
        )


def test_cache_dir_is_created(repo, tmp_path):
    cache_dir = tmp_path / "nested" / "cache"
    report_of(run_ghreport(repo, cache_dir))
    assert cache_dir.is_dir() and any(cache_dir.iterdir()), "the cache directory is missing or empty"


def test_cache_does_not_contain_token(proxy, repo, cache_dir):
    token = f"ghreport-test-{uuid.uuid4().hex}"
    report_of(run_ghreport(repo, cache_dir, base=proxy.url, env={"GITHUB_TOKEN": token}))

    assert proxy.requests[0].headers.get("Authorization") == f"Bearer {token}", (
        "ghreport did not send the token, so this test proves nothing"
    )
    files = [path for path in cache_dir.rglob("*") if path.is_file()]
    assert files, "nothing was cached"
    for path in files:
        content = path.read_bytes()
        assert token.encode() not in content, f"{path} contains the token"
