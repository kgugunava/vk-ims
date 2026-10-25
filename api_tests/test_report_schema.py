"""Coverage 7: the report schema and the order of issues."""

from __future__ import annotations

import json

from conftest import report_of, run_ghreport
from support.reference import expected_issues
from support.schema import report_problems, sort_key


def test_report_schema(tmp_path, repo):
    proc = run_ghreport(repo, tmp_path / "cache")
    report = report_of(proc)
    assert report["repos"][0]["repo"] == repo
    assert report["repos"][0]["error"] is None


def test_report_has_exact_fields_and_types(repo, missing_repo, cache_dir):
    proc = run_ghreport(f"{repo},{missing_repo}", cache_dir)
    problems = report_problems(json.loads(proc.stdout))
    assert not problems, "\n".join(problems)


def test_issues_sorted_by_created_at_then_number(repo, cache_dir):
    issues = report_of(run_ghreport(repo, cache_dir, "--limit", "100000"))["repos"][0]["issues"]
    for earlier, later in zip(issues, issues[1:]):
        assert sort_key(earlier) < sort_key(later), (
            f"#{earlier['number']} ({earlier['created_at']}) before #{later['number']} ({later['created_at']})"
        )


def test_issues_are_sorted_even_when_a_page_is_not(proxy, reference, repo, cache_dir):
    """A server that follows the contract already sends issues in order, so
    a service that never sorts would pass the test above by luck. Here the
    proxy reverses the first page; the report must still be sorted."""
    proxy.transform(lambda items: list(reversed(items)))
    report = report_of(run_ghreport(repo, cache_dir, "--limit", "100000", base=proxy.url))
    assert report["repos"][0]["issues"] == expected_issues(reference(repo), 100000)


def test_count_matches_issues_length(repo, missing_repo, cache_dir):
    report = json.loads(run_ghreport(f"{repo},{missing_repo}", cache_dir).stdout)
    for entry in report["repos"]:
        assert entry["count"] == len(entry["issues"]), entry["repo"]


def test_fields_match_api(reference, repo, cache_dir):
    """title, author (user.login), labels (names, in order), created_at (the
    API string unchanged) and url (html_url), issue by issue."""
    report = report_of(run_ghreport(repo, cache_dir))
    assert report["repos"][0]["issues"] == expected_issues(reference(repo), 100)


def test_repos_keep_argument_order_and_deduplicate(repo, missing_repo, cache_dir):
    proc = run_ghreport(f"{missing_repo},{repo},{missing_repo}", cache_dir)
    assert proc.returncode == 1
    assert [entry["repo"] for entry in json.loads(proc.stdout)["repos"]] == [missing_repo, repo]
