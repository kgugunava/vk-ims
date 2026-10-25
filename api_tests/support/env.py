"""Environment variables the suite runs with."""

from __future__ import annotations

import os
from dataclasses import dataclass

DEFAULT_TEST_REPOS = "stub/alpha"


@dataclass(frozen=True)
class Environment:
    binary: str  # GHREPORT_BIN: absolute path to the built service
    base_url: str  # GITHUB_API_BASE_URL: server that follows the contract
    token: str | None  # GITHUB_TOKEN: optional, also used for the suite's own requests
    repos: list[str]  # GHREPORT_TEST_REPOS: repositories with open issues on that server


def load() -> Environment:
    """Reads and checks the environment; raises RuntimeError with a hint."""
    missing = [name for name in ("GHREPORT_BIN", "GITHUB_API_BASE_URL") if not os.environ.get(name)]
    if missing:
        raise RuntimeError(
            f"set {' and '.join(missing)}, e.g. GHREPORT_BIN=$PWD/service/ghreport "
            "GITHUB_API_BASE_URL=http://127.0.0.1:8080"
        )
    binary = os.environ["GHREPORT_BIN"]
    if not os.path.isfile(binary) or not os.access(binary, os.X_OK):
        raise RuntimeError(f"GHREPORT_BIN={binary!r} is not an executable file")

    repos = [repo.strip() for repo in os.environ.get("GHREPORT_TEST_REPOS", DEFAULT_TEST_REPOS).split(",")]
    return Environment(
        binary=binary,
        base_url=os.environ["GITHUB_API_BASE_URL"].rstrip("/"),
        token=os.environ.get("GITHUB_TOKEN") or None,
        repos=[repo for repo in repos if repo],
    )
