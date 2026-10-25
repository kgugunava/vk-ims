"""The server itself against the contract of the issues endpoint.

Most contract violations a test suite must catch are the server's: a skipped
item, a pull request without its key, a 304 without If-None-Match, broken
JSON, the wrong sort order, a missing Link header. The suite reads the server
directly, independently of ghreport, so it fails on such a server even when
ghreport faithfully reports whatever it received.
"""

from __future__ import annotations

from support.reference import (
    cross_check_page_size,
    get,
    listing_url,
)
from support.schema import api_item_problems, sort_key


def test_listing_is_json_array_of_issue_objects(reference, repo):
    ref = reference(repo)
    problems = [
        f"{page.url} item {index}: {problem}"
        for page in ref.pages
        for index, item in enumerate(page.items)
        for problem in api_item_problems(item)
    ]
    assert not problems, "\n".join(problems[:20])


def test_listing_is_sorted_ascending(reference, repo):
    items = reference(repo).raw_items
    for earlier, later in zip(items, items[1:]):
        assert sort_key(earlier) <= sort_key(later), (
            f"#{earlier['number']} ({earlier['created_at']}) comes before "
            f"#{later['number']} ({later['created_at']}) with direction=asc"
        )


def test_pull_requests_carry_pull_request_key(reference, repo):
    unmarked = [
        item["number"] for item in reference(repo).items
        if "/pull/" in item.get("html_url", "") and "pull_request" not in item
    ]
    assert not unmarked, f"pull requests without the pull_request key: {unmarked}"


def test_link_next_present_while_pages_remain(reference, repo, server):
    """Walk with small pages; past the page without rel="next" there must be
    nothing. The extra page is requested by URL, which only the suite does."""
    page_size = cross_check_page_size(len(reference(repo).raw_items))
    ref = reference(repo, page_size)
    for page in ref.pages[:-1]:
        assert page.items, f"{page.url} is empty but links to a next page"

    beyond = listing_url(server.base_url, repo, page_size, page=len(ref.pages) + 1)
    response = get(beyond, token=server.token)
    leftover = response.json() if response.status == 200 else []
    assert leftover == [], (
        f"page {len(ref.pages)} has no Link rel=next, yet {beyond} returns "
        f"{len(leftover)} more item(s)"
    )


def test_pagination_is_complete(reference, repo):
    """Every page size must give the same issues: one skipped at a page
    boundary shows up as a difference. Repeats across pages are allowed."""
    full = reference(repo)
    numbers = {item["number"] for item in full.items}
    small = reference(repo, cross_check_page_size(len(full.raw_items)))
    small_numbers = {item["number"] for item in small.items}
    assert small_numbers == numbers, (
        f"per_page={small.per_page} misses {sorted(numbers - small_numbers)[:10]}, "
        f"per_page=100 misses {sorted(small_numbers - numbers)[:10]}"
    )


def test_unconditional_request_never_returns_304(repo, server):
    url = listing_url(server.base_url, repo, 100)
    for _ in range(2):
        response = get(url, token=server.token)
        assert response.status == 200, f"{url} without If-None-Match: status {response.status}"


def test_conditional_request_returns_304(repo, server):
    url = listing_url(server.base_url, repo, 100)
    first = get(url, token=server.token)
    etag = first.headers.get("ETag")
    assert etag, f"{url}: no ETag header, so conditional requests are impossible"

    again = get(url, headers={"If-None-Match": etag}, token=server.token)
    assert again.status == 304, f"If-None-Match: {etag}: status {again.status}, want 304"
    assert again.body == b"", "a 304 response has a body"

    other = get(url, headers={"If-None-Match": '"not-the-current-etag"'}, token=server.token)
    assert other.status == 200, f"If-None-Match with a stale ETag: status {other.status}, want 200"


def test_unknown_repository_returns_404(missing_repo, server):
    response = get(listing_url(server.base_url, missing_repo, 100), token=server.token)
    assert response.status == 404, f"{missing_repo}: status {response.status}, want 404"
