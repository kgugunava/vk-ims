"""The suite's own client for the issues endpoint.

It reads the listing exactly as the contract describes, independently of
ghreport, and serves two purposes: checking the server against the contract,
and computing the report ghreport must produce for any data the server has.
"""

from __future__ import annotations

import json
import math
import re
import urllib.error
import urllib.request
from dataclasses import dataclass, field
from datetime import date, datetime, time, timezone
from email.message import Message
from typing import Any

from support.runner import parse_time

# Pages of the small-page walk used to cross-check pagination; keeps the
# number of requests bounded on large repositories.
CROSS_CHECK_PAGES = 10
MAX_PAGES = 2000

_LINK_PART = re.compile(r'<([^>]*)>\s*((?:;[^,<]*)*)')
_REL = re.compile(r';\s*rel\s*=\s*"?([^";]*)"?', re.IGNORECASE)


class ContractError(AssertionError):
    """The server broke the contract of the issues endpoint."""


@dataclass
class Response:
    url: str
    status: int
    headers: Message
    body: bytes

    def json(self) -> Any:
        try:
            return json.loads(self.body)
        except ValueError as error:
            raise ContractError(f"{self.url}: body is not valid JSON: {error}") from error


def get(url: str, headers: dict[str, str] | None = None, token: str | None = None) -> Response:
    """One GET with the headers GitHub expects; HTTP errors are returned, not raised."""
    request_headers = {
        "Accept": "application/vnd.github+json",
        "X-GitHub-Api-Version": "2026-03-10",
        "User-Agent": "ghreport-api-tests",
        **(headers or {}),
    }
    if token:
        request_headers["Authorization"] = f"Bearer {token}"
    request = urllib.request.Request(url, headers=request_headers)
    try:
        with urllib.request.urlopen(request, timeout=30) as response:
            return Response(url, response.status, response.headers, response.read())
    except urllib.error.HTTPError as error:
        return Response(url, error.code, error.headers, error.read())


def next_link(header: str | None) -> str | None:
    """The rel="next" target of a Link header."""
    for match in _LINK_PART.finditer(header or ""):
        target, params = match.groups()
        for rel in _REL.findall(params):
            if "next" in rel.lower().split():
                return target
    return None


def listing_url(base: str, repo: str, per_page: int, page: int | None = None) -> str:
    url = f"{base}/repos/{repo}/issues?state=open&sort=created&direction=asc&per_page={per_page}"
    return url if page is None else f"{url}&page={page}"


@dataclass
class Page:
    url: str
    etag: str | None
    next_url: str | None
    items: list[dict[str, Any]]


@dataclass
class Reference:
    repo: str
    per_page: int
    pages: list[Page] = field(default_factory=list)

    @property
    def raw_items(self) -> list[dict[str, Any]]:
        """All items in the order received, repeats included."""
        return [item for page in self.pages for item in page.items]

    @property
    def items(self) -> list[dict[str, Any]]:
        """Items in the order received, each number once."""
        seen: set[int] = set()
        out = []
        for item in self.raw_items:
            if item["number"] not in seen:
                seen.add(item["number"])
                out.append(item)
        return out

    @property
    def issues(self) -> list[dict[str, Any]]:
        return [item for item in self.items if "pull_request" not in item]

    @property
    def pull_requests(self) -> list[dict[str, Any]]:
        return [item for item in self.items if "pull_request" in item]


def fetch_listing(base: str, repo: str, per_page: int = 100, token: str | None = None) -> Reference:
    """Walks the listing by Link rel="next"; raises ContractError on a broken response."""
    reference = Reference(repo, per_page)
    url: str | None = listing_url(base, repo, per_page)
    visited: set[str] = set()
    while url is not None:
        if url in visited:
            raise ContractError(f"Link rel=next returns to {url}")
        if len(visited) >= MAX_PAGES:
            raise ContractError(f"more than {MAX_PAGES} pages")
        visited.add(url)

        response = get(url, token=token)
        if response.status != 200:
            raise ContractError(f"{url}: status {response.status}, want 200")
        items = response.json()
        if not isinstance(items, list) or not all(isinstance(item, dict) for item in items):
            raise ContractError(f"{url}: body is not a JSON array of objects")
        next_url = next_link(response.headers.get("Link"))
        reference.pages.append(Page(url, response.headers.get("ETag"), next_url, items))
        url = next_url
    return reference


def cross_check_page_size(item_count: int) -> int:
    """A page size other than 100 that splits the listing into about
    CROSS_CHECK_PAGES pages."""
    return min(max(2, math.ceil(item_count / CROSS_CHECK_PAGES)), 99)


def report_issue(item: dict[str, Any]) -> dict[str, Any]:
    """The report entry for an API item, per the field mapping of the task."""
    user = item.get("user") or {}
    return {
        "number": item["number"],
        "title": item["title"],
        "author": user.get("login", ""),
        "labels": [label["name"] for label in item.get("labels", []) if "name" in label],
        "created_at": item["created_at"],
        "url": item["html_url"],
    }


def expected_issues(reference: Reference, limit: int, since: date | None = None) -> list[dict[str, Any]]:
    """The issues ghreport must report: pull requests and issues created
    before `since` dropped, repeats dropped, the first `limit` in the order
    received, then sorted by created_at and number."""
    cutoff = datetime.combine(since, time(0), tzinfo=timezone.utc) if since else None
    chosen: list[dict[str, Any]] = []
    seen: set[int] = set()
    for item in reference.raw_items:
        if len(chosen) >= limit:
            break
        if "pull_request" in item or item["number"] in seen:
            continue
        if cutoff and parse_time(item["created_at"]) < cutoff:
            continue
        seen.add(item["number"])
        chosen.append(item)
    chosen.sort(key=lambda item: (parse_time(item["created_at"]), item["number"]))
    return [report_issue(item) for item in chosen]


def pages_needed(reference: Reference, limit: int) -> int:
    """How many pages ghreport must fetch to collect `limit` issues: it stops
    on the page where the limit is reached."""
    seen: set[int] = set()
    collected = 0
    for index, page in enumerate(reference.pages, start=1):
        for item in page.items:
            if "pull_request" not in item and item["number"] not in seen:
                seen.add(item["number"])
                collected += 1
                if collected >= limit:
                    return index
    return len(reference.pages)
