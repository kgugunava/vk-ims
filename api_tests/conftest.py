"""Shared setup: the environment, running ghreport, and fixtures.

Tests call run_ghreport() and report_of() from here and never start
processes themselves.
"""

from __future__ import annotations

import subprocess
import uuid
from pathlib import Path
from typing import Any

import pytest

from support import env as env_module
from support import reference as reference_module
from support import runner

ENV: env_module.Environment


def pytest_configure(config: pytest.Config) -> None:
    global ENV
    try:
        ENV = env_module.load()
    except RuntimeError as error:
        raise pytest.UsageError(str(error)) from error


def run_ghreport(
    repos: str,
    cache_dir: Path,
    *args: str,
    base: str | None = None,
    env: dict[str, str] | None = None,
) -> subprocess.CompletedProcess[str]:
    """Runs ghreport against GITHUB_API_BASE_URL, or against `base` (a proxy)."""
    return runner.run_ghreport(ENV.binary, base or ENV.base_url, repos, cache_dir, *args, env=env)


def report_of(proc: subprocess.CompletedProcess[str]) -> dict[str, Any]:
    return runner.report_of(proc)


@pytest.fixture
def cache_dir(tmp_path: Path) -> Path:
    """A cache directory of the test's own, so tests never share cached state."""
    return tmp_path / "cache"


@pytest.fixture
def trace_path(tmp_path: Path) -> Path:
    return tmp_path / "trace.jsonl"


@pytest.fixture(scope="session")
def repos() -> list[str]:
    return list(ENV.repos)


@pytest.fixture(scope="session")
def repo(repos: list[str]) -> str:
    """The main repository the tests read."""
    return repos[0]


@pytest.fixture(scope="session")
def reference():
    """reference(repo, per_page=100): the listing read directly from the
    server, by the suite's own client. Cached for the session: it is only
    read, and fewer requests matter against the real, rate-limited GitHub."""
    cache: dict[tuple[str, int], reference_module.Reference] = {}

    def fetch(repo: str, per_page: int = 100) -> reference_module.Reference:
        key = (repo, per_page)
        if key not in cache:
            cache[key] = reference_module.fetch_listing(ENV.base_url, repo, per_page, ENV.token)
        return cache[key]

    return fetch


@pytest.fixture(scope="session")
def server():
    """Where and how the suite reaches GITHUB_API_BASE_URL itself."""
    return ENV


@pytest.fixture
def missing_repo(repo: str) -> str:
    """A repository that cannot exist on the server, so it must be a 404."""
    owner = repo.split("/")[0]
    return f"{owner}/ghreport-missing-{uuid.uuid4().hex[:12]}"
