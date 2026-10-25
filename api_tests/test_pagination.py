"""Coverage 1: pagination and --limit.

The proxy forwards per_page=1, so the repository spans several pages whatever
its size; the expected report comes from the suite's own reading of the
listing, not from fixed data.
"""

from __future__ import annotations

import pytest

from conftest import report_of, run_ghreport
from support.reference import Reference, expected_issues
from support.runner import read_trace, without_generated_at

# Fewer issues than this make the multi-page checks meaningless; the test
# fails rather than skips, so a server returning empty lists cannot pass.
MIN_ISSUES = 2


def numbers(report: dict) -> list[int]:
    return [issue["number"] for issue in report["repos"][0]["issues"]]


def requests_for_limit_with_one_item_per_page(ref: Reference, limit: int) -> int:
    """With per_page=1 ghreport stops on the page that holds the limit-th issue."""
    issues_seen = 0
    for position, item in enumerate(ref.items, start=1):
        if "pull_request" not in item:
            issues_seen += 1
            if issues_seen == limit:
                return position
    return len(ref.items)


@pytest.fixture
def small_limit(reference, repo) -> int:
    issues = reference(repo).issues
    assert len(issues) >= MIN_ISSUES, (
        f"{repo} has {len(issues)} open issue(s); the pagination tests need at least {MIN_ISSUES}"
    )
    return min(len(issues), 3)


def test_proxy_without_rules_changes_nothing(proxy, repo, tmp_path):
    """The proxy is the base of many tests; through it, untouched, the report
    must equal the one made directly."""
    direct = report_of(run_ghreport(repo, tmp_path / "direct"))
    proxied = report_of(run_ghreport(repo, tmp_path / "proxied", base=proxy.url))
    assert without_generated_at(proxied) == without_generated_at(direct)
    assert proxy.requests, "ghreport did not go through the proxy"


def test_limit_spans_multiple_pages(proxy, reference, repo, cache_dir, small_limit):
    proxy.page_size(1)
    report = report_of(run_ghreport(repo, cache_dir, "--limit", str(small_limit), base=proxy.url))

    assert len(proxy.requests) >= 2, "the listing did not span several pages"
    got = numbers(report)
    assert len(got) == small_limit, f"{len(got)} issues, want exactly --limit {small_limit}"
    assert len(set(got)) == len(got), f"repeated numbers: {got}"
    assert report["repos"][0]["issues"] == expected_issues(reference(repo), small_limit)


def test_follows_link_next_exactly(proxy, repo, cache_dir, trace_path, small_limit):
    proxy.page_size(1)
    report_of(run_ghreport(repo, cache_dir, "--limit", str(small_limit), "--trace", str(trace_path), base=proxy.url))

    urls = [entry["url"] for entry in read_trace(trace_path)]
    for index, request in enumerate(proxy.requests[:-1]):
        assert request.served_next is not None, f"request {index} had no next page, yet ghreport went on"
        assert urls[index + 1] == request.served_next, (
            f"after {urls[index]} ghreport requested {urls[index + 1]}, "
            f"but Link rel=next was {request.served_next}"
        )


def test_stops_requesting_after_limit(proxy, reference, repo, cache_dir, small_limit):
    proxy.page_size(1)
    report_of(run_ghreport(repo, cache_dir, "--limit", str(small_limit), base=proxy.url))

    want = requests_for_limit_with_one_item_per_page(reference(repo), small_limit)
    assert len(proxy.requests) == want, (
        f"{len(proxy.requests)} pages requested, want {want}: "
        "no page may be fetched once --limit issues are collected"
    )


def test_limit_larger_than_listing_returns_all(reference, repo, cache_dir):
    ref = reference(repo)
    report = report_of(run_ghreport(repo, cache_dir, "--limit", "100000"))
    assert report["repos"][0]["issues"] == expected_issues(ref, 100000)


def test_default_limit_is_100(reference, repo, cache_dir):
    ref = reference(repo)
    report = report_of(run_ghreport(repo, cache_dir))
    assert report["repos"][0]["count"] == min(100, len(ref.issues))
    assert report["repos"][0]["issues"] == expected_issues(ref, 100)


def test_duplicates_across_pages_are_removed(proxy, reference, repo, cache_dir, small_limit):
    """A listing that shifts while being read repeats an item on the next
    page; the report must have it once."""
    first_item = reference(repo).items[0]
    proxy.page_size(1)
    proxy.transform(lambda items: [first_item] + items, path_contains="page=2")

    report = report_of(run_ghreport(repo, cache_dir, "--limit", str(small_limit), base=proxy.url))
    got = numbers(report)
    assert len(set(got)) == len(got), f"repeated numbers: {got}"
    assert report["repos"][0]["issues"] == expected_issues(reference(repo), small_limit)
