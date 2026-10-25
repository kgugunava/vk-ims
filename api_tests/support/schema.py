"""Shape checks for API items, the report and the trace."""

from __future__ import annotations

from typing import Any

from support.runner import parse_time

REPORT_KEYS = {"generated_at", "repos"}
REPO_KEYS = {"repo", "error", "count", "issues"}
ISSUE_KEYS = {"number", "title", "author", "labels", "created_at", "url"}
TRACE_KEYS = {"ts", "method", "url", "status", "attempt", "from_cache", "error"}
ERROR_CODES = {"not_found", "rate_limited", "request_failed", "invalid_response"}


def _is_int(value: Any) -> bool:
    # bool is a subclass of int in Python; JSON true is not a number.
    return isinstance(value, int) and not isinstance(value, bool)


def _is_time(value: Any) -> bool:
    try:
        parse_time(value)
    except (TypeError, ValueError):
        return False
    return True


def api_item_problems(item: Any) -> list[str]:
    """What is wrong with one element of an issues page."""
    if not isinstance(item, dict):
        return ["not an object"]
    problems = []
    if not _is_int(item.get("number")):
        problems.append(f"number {item.get('number')!r} is not an integer")
    if not isinstance(item.get("title"), str):
        problems.append("title is not a string")
    user = item.get("user")
    if user is not None and not (isinstance(user, dict) and isinstance(user.get("login"), str)):
        problems.append(f"user {user!r} has no login")
    labels = item.get("labels", [])
    if not isinstance(labels, list) or not all(isinstance(label, dict) for label in labels):
        problems.append("labels is not a list of objects")
    if not _is_time(item.get("created_at")):
        problems.append(f"created_at {item.get('created_at')!r} is not RFC 3339")
    if not isinstance(item.get("html_url"), str):
        problems.append("html_url is not a string")
    return problems


def report_problems(report: Any) -> list[str]:
    """What is wrong with the shape of a ghreport report."""
    if not isinstance(report, dict) or set(report) != REPORT_KEYS:
        return [f"top-level keys {sorted(report) if isinstance(report, dict) else report!r}, want {sorted(REPORT_KEYS)}"]
    problems = []
    generated_at = report["generated_at"]
    if not isinstance(generated_at, str) or not generated_at.endswith("Z") or not _is_time(generated_at):
        problems.append(f"generated_at {generated_at!r} is not RFC 3339 UTC")
    if not isinstance(report["repos"], list):
        return problems + ["repos is not a list"]
    for repo in report["repos"]:
        name = repo.get("repo") if isinstance(repo, dict) else repo
        if not isinstance(repo, dict) or set(repo) != REPO_KEYS:
            problems.append(f"repo entry {name!r} has keys {sorted(repo) if isinstance(repo, dict) else '?'}")
            continue
        if not isinstance(repo["repo"], str):
            problems.append(f"repo {name!r}: repo is not a string")
        if repo["error"] is not None and repo["error"] not in ERROR_CODES:
            problems.append(f"repo {name!r}: error {repo['error']!r}")
        if not _is_int(repo["count"]) or not isinstance(repo["issues"], list):
            problems.append(f"repo {name!r}: count or issues of the wrong type")
            continue
        if repo["count"] != len(repo["issues"]):
            problems.append(f"repo {name!r}: count {repo['count']} != {len(repo['issues'])} issues")
        for issue in repo["issues"]:
            problems.extend(f"repo {name!r}: {problem}" for problem in issue_problems(issue))
    return problems


def issue_problems(issue: Any) -> list[str]:
    if not isinstance(issue, dict) or set(issue) != ISSUE_KEYS:
        return [f"issue keys {sorted(issue) if isinstance(issue, dict) else issue!r}, want {sorted(ISSUE_KEYS)}"]
    problems = []
    number = issue["number"]
    if not _is_int(number):
        problems.append(f"issue number {number!r} is not an integer")
    for key in ("title", "author", "url"):
        if not isinstance(issue[key], str):
            problems.append(f"issue {number}: {key} is not a string")
    if not isinstance(issue["labels"], list) or not all(isinstance(label, str) for label in issue["labels"]):
        problems.append(f"issue {number}: labels is not a list of strings")
    if not _is_time(issue["created_at"]):
        problems.append(f"issue {number}: created_at {issue['created_at']!r} is not RFC 3339")
    return problems


def trace_problems(entry: Any) -> list[str]:
    """What is wrong with one --trace record."""
    if not isinstance(entry, dict) or set(entry) != TRACE_KEYS:
        return [f"trace keys {sorted(entry) if isinstance(entry, dict) else entry!r}, want {sorted(TRACE_KEYS)}"]
    problems = []
    if not isinstance(entry["ts"], str) or not entry["ts"].endswith("Z") or not _is_time(entry["ts"]):
        problems.append(f"ts {entry['ts']!r} is not RFC 3339 UTC")
    if entry["method"] != "GET":
        problems.append(f"method {entry['method']!r}")
    if not isinstance(entry["url"], str) or not entry["url"].startswith(("http://", "https://")):
        problems.append(f"url {entry['url']!r} is not a full URL")
    if not _is_int(entry["status"]) or entry["status"] < 0:
        problems.append(f"status {entry['status']!r}")
    if not _is_int(entry["attempt"]) or entry["attempt"] < 1:
        problems.append(f"attempt {entry['attempt']!r}")
    if not isinstance(entry["from_cache"], bool):
        problems.append(f"from_cache {entry['from_cache']!r} is not a boolean")
    if entry["error"] is not None and not isinstance(entry["error"], str):
        problems.append(f"error {entry['error']!r} is neither null nor a string")
    return problems


def sort_key(issue: dict[str, Any]) -> tuple[Any, int]:
    return parse_time(issue["created_at"]), issue["number"]
