"""Coverage 8: the --trace log, and that the token never leaks anywhere."""

from __future__ import annotations

import uuid
from urllib.parse import parse_qsl, urlsplit

from conftest import report_of, run_ghreport
from support.runner import read_trace
from support.schema import trace_problems


def test_trace_records_have_exact_fields(repo, missing_repo, cache_dir, trace_path):
    run_ghreport(f"{repo},{missing_repo}", cache_dir, "--trace", str(trace_path))
    entries = read_trace(trace_path)
    assert entries, "the trace is empty"
    problems = [f"line {index + 1}: {problem}" for index, entry in enumerate(entries) for problem in trace_problems(entry)]
    assert not problems, "\n".join(problems)


def test_trace_has_one_record_per_request(proxy, repo, cache_dir, trace_path):
    proxy.page_size(1)
    proxy.fail(500, times=1)  # a retry is an attempt of its own
    report_of(run_ghreport(repo, cache_dir, "--limit", "2", "--trace", str(trace_path), base=proxy.url))

    traced = [entry["url"] for entry in read_trace(trace_path)]
    sent = [proxy.url + request.path for request in proxy.requests]
    assert traced == sent, f"trace URLs:\n{traced}\nrequests the server saw:\n{sent}"


def test_first_request_matches_contract(repo, cache_dir, trace_path, server):
    report_of(run_ghreport(repo, cache_dir, "--trace", str(trace_path)))
    url = urlsplit(read_trace(trace_path)[0]["url"])

    assert f"{url.scheme}://{url.netloc}{url.path}" == f"{server.base_url}/repos/{repo}/issues"
    assert dict(parse_qsl(url.query)) == {
        "state": "open", "sort": "created", "direction": "asc", "per_page": "100",
    }


def test_request_headers(proxy, repo, tmp_path):
    token = f"ghreport-test-{uuid.uuid4().hex}"
    report_of(run_ghreport(repo, tmp_path / "with", base=proxy.url, env={"GITHUB_TOKEN": token}))
    headers = proxy.requests[0].headers
    assert headers.get("Accept") == "application/vnd.github+json"
    assert headers.get("X-GitHub-Api-Version") == "2026-03-10"
    assert headers.get("User-Agent"), "no User-Agent"
    assert headers.get("Authorization") == f"Bearer {token}"

    without = len(proxy.requests)
    report_of(run_ghreport(repo, tmp_path / "without", base=proxy.url, env={"GITHUB_TOKEN": ""}))
    assert "Authorization" not in proxy.requests[without].headers, "Authorization sent without a token"


def test_token_never_leaks(proxy, repo, missing_repo, tmp_path):
    """A made-up token, a failing repository and a retry, so that error paths
    write to stderr and the trace too."""
    token = f"ghreport-test-{uuid.uuid4().hex}"
    proxy.fail(500, times=1)
    cache_dir, trace_path = tmp_path / "cache", tmp_path / "trace.jsonl"
    proc = run_ghreport(f"{repo},{missing_repo}", cache_dir, "--trace", str(trace_path),
                        base=proxy.url, env={"GITHUB_TOKEN": token})

    assert proxy.requests[0].headers.get("Authorization") == f"Bearer {token}", (
        "ghreport did not send the token, so this test proves nothing"
    )
    outputs = {"stdout": proc.stdout, "stderr": proc.stderr, "trace": trace_path.read_text()}
    outputs.update({str(path): path.read_text(errors="replace") for path in cache_dir.rglob("*") if path.is_file()})
    for name, content in outputs.items():
        assert token not in content, f"{name} contains the token"
    trace = outputs["trace"]
    assert "Authorization" not in trace and "Bearer" not in trace, "the trace contains the Authorization header"
