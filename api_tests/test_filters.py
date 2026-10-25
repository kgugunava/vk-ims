"""Coverage 2 (pull requests are excluded) and 3 (--since)."""

from __future__ import annotations

import copy
from datetime import date

from conftest import report_of, run_ghreport
from support.reference import expected_issues
from support.runner import parse_time

ALL = "100000"  # --limit that never cuts the listing


def report_numbers(report: dict) -> set[int]:
    return {issue["number"] for issue in report["repos"][0]["issues"]}


def fake_pull_request(template: dict, number: int, marker) -> dict:
    """An item shaped like an issue of the repository, marked as a PR."""
    item = copy.deepcopy(template)
    item["number"] = number
    item["pull_request"] = marker
    return item


def test_pull_requests_are_excluded(reference, repo, cache_dir):
    ref = reference(repo)
    report = report_of(run_ghreport(repo, cache_dir, "--limit", ALL))

    leaked = report_numbers(report) & {item["number"] for item in ref.pull_requests}
    assert not leaked, f"pull requests in the report: {sorted(leaked)}"
    assert all("/pull/" not in issue["url"] for issue in report["repos"][0]["issues"])
    assert report["repos"][0]["issues"] == expected_issues(ref, 100000)


def test_injected_pull_request_is_excluded(proxy, reference, repo, cache_dir):
    """Works whatever the data: a PR is added to the first page by the proxy."""
    ref = reference(repo)
    fake_number = max(item["number"] for item in ref.items) + 1000
    fake = fake_pull_request(ref.items[0], fake_number, {"url": "https://example.invalid/pull"})
    proxy.transform(lambda items: [fake] + items)

    report = report_of(run_ghreport(repo, cache_dir, "--limit", ALL, base=proxy.url))
    assert fake_number not in report_numbers(report), "the pull request is in the report"
    assert report["repos"][0]["issues"] == expected_issues(ref, 100000)


def test_pull_request_key_with_null_value_is_excluded(proxy, reference, repo, cache_dir):
    """The presence of the key marks a pull request, whatever its value."""
    ref = reference(repo)
    fake_number = max(item["number"] for item in ref.items) + 1000
    proxy.transform(lambda items: [fake_pull_request(ref.items[0], fake_number, None)] + items)

    report = report_of(run_ghreport(repo, cache_dir, "--limit", ALL, base=proxy.url))
    assert fake_number not in report_numbers(report), '"pull_request": null was not treated as a pull request'


def test_since_keeps_issues_created_on_or_after_date(reference, repo, cache_dir):
    ref = reference(repo)
    issues = ref.issues
    assert issues, f"{repo} has no open issues"
    # The day of the median issue splits the listing whatever the data.
    cutoff = parse_time(sorted(item["created_at"] for item in issues)[len(issues) // 2]).date()

    report = report_of(run_ghreport(repo, cache_dir, "--limit", ALL, "--since", cutoff.isoformat()))
    for issue in report["repos"][0]["issues"]:
        assert parse_time(issue["created_at"]).date() >= cutoff, (
            f"#{issue['number']} created {issue['created_at']} is before --since {cutoff}"
        )
    assert report["repos"][0]["issues"] == expected_issues(ref, 100000, cutoff)


def test_since_boundary_is_midnight_utc(proxy, reference, repo, cache_dir):
    """Three issues around midnight of a day far in the future, so no real
    issue gets in: one second before (out), exactly midnight (in), and 02:00
    in UTC+3, which is 23:00 UTC the day before (out)."""
    ref = reference(repo)
    base_number = max(item["number"] for item in ref.items) + 1000
    template = ref.issues[0] if ref.issues else ref.items[0]

    def issue(offset: int, created_at: str) -> dict:
        item = copy.deepcopy(template)
        item.pop("pull_request", None)
        item.update(number=base_number + offset, created_at=created_at)
        return item

    planted = [
        issue(1, "2098-12-31T23:59:59Z"),
        issue(2, "2099-01-01T00:00:00Z"),
        issue(3, "2099-01-01T02:00:00+03:00"),
    ]
    proxy.transform(lambda items: planted + items)

    report = report_of(run_ghreport(repo, cache_dir, "--limit", ALL, "--since", "2099-01-01", base=proxy.url))
    assert report_numbers(report) == {base_number + 2}, (
        f"--since 2099-01-01 kept {sorted(report_numbers(report))}, want only #{base_number + 2} "
        "created exactly at midnight UTC"
    )


def test_since_after_all_issues_returns_empty(reference, repo, cache_dir):
    proc = run_ghreport(repo, cache_dir, "--since", date(2099, 1, 1).isoformat())
    entry = report_of(proc)["repos"][0]
    assert entry == {"repo": repo, "error": None, "count": 0, "issues": []}
