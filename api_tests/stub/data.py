"""Deterministic repository contents served by the stub."""

from __future__ import annotations

from datetime import datetime, timedelta, timezone
from typing import Any

_START = datetime(2026, 1, 1, tzinfo=timezone.utc)

_TITLES = [
    "Crash on start",
    "Падает при запуске",
    "Emoji in title 💅",
    "Escape <html> & \"quotes\"",
    "Slow pagination",
]
_LABELS = [[], ["bug"], ["bug", "p1"], ["enhancement"], ["вопрос"]]


def _item(repo: str, number: int, created: datetime, is_pull: bool) -> dict[str, Any]:
    kind = "pull" if is_pull else "issues"
    item: dict[str, Any] = {
        "number": number,
        "title": f"{_TITLES[number % len(_TITLES)]} #{number}",
        # One item has no user, as GitHub returns for deleted accounts.
        "user": None if number == 7 else {"login": f"user{number % 6}"},
        "labels": [{"name": name, "color": "ededed"} for name in _LABELS[number % len(_LABELS)]],
        "state": "open",
        "created_at": created.strftime("%Y-%m-%dT%H:%M:%SZ"),
        "updated_at": (created + timedelta(days=30)).strftime("%Y-%m-%dT%H:%M:%SZ"),
        "html_url": f"https://github.com/{repo}/{kind}/{number}",
    }
    if is_pull:
        item["pull_request"] = {"url": f"https://api.github.com/repos/{repo}/pulls/{number}"}
    return item


def _alpha() -> list[dict[str, Any]]:
    repo = "stub/alpha"
    items = []
    for number in range(1, 251):
        # Seven hours apart, so the items span about ten weeks. Pairs of
        # numbers share a timestamp, which makes `number` the tie-breaker.
        step = number - 1 if number % 10 != 1 or number == 1 else number - 2
        created = _START + timedelta(hours=7 * step)
        items.append(_item(repo, number, created, is_pull=number % 4 == 0))
    return items


def _beta() -> list[dict[str, Any]]:
    repo = "stub/beta"
    return [_item(repo, n, _START + timedelta(days=n), is_pull=False) for n in (1, 2, 3)]


def repositories() -> dict[str, list[dict[str, Any]]]:
    """Repository name -> open issues and pull requests, sorted by
    (created_at, number) ascending."""
    repos = {"stub/alpha": _alpha(), "stub/beta": _beta(), "stub/empty": []}
    for items in repos.values():
        items.sort(key=lambda item: (item["created_at"], item["number"]))
    return repos
