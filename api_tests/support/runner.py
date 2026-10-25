"""Running ghreport as a separate process and reading what it produced."""

from __future__ import annotations

import json
import os
import subprocess
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

# Upper bound for one run. A run that pauses for rate limits takes seconds;
# this only stops a hung process from hanging the suite.
RUN_TIMEOUT_SECONDS = 120


def run_ghreport(
    binary: str,
    base_url: str,
    repos: str,
    cache_dir: Path,
    *args: str,
    env: dict[str, str] | None = None,
) -> subprocess.CompletedProcess[str]:
    """Runs ghreport with stdout as the report; `env` overrides variables."""
    cmd = [binary, "--repos", repos, "--out", "-", "--cache-dir", str(cache_dir), *args]
    process_env = dict(os.environ, GITHUB_API_BASE_URL=base_url)
    process_env.update(env or {})
    # A variable set to "" in `env` is removed, e.g. to run without a token.
    process_env = {name: value for name, value in process_env.items() if value != ""}
    return subprocess.run(cmd, capture_output=True, text=True, env=process_env, timeout=RUN_TIMEOUT_SECONDS)


def report_of(proc: subprocess.CompletedProcess[str]) -> dict[str, Any]:
    """The report of a successful run."""
    assert proc.returncode == 0, f"exit code {proc.returncode}\nstdout:\n{proc.stdout}\nstderr:\n{proc.stderr}"
    return json.loads(proc.stdout)


def read_trace(path: Path) -> list[dict[str, Any]]:
    """The --trace file: one JSON object per line."""
    lines = path.read_text(encoding="utf-8").splitlines()
    return [json.loads(line) for line in lines if line.strip()]


def parse_time(value: str) -> datetime:
    """RFC 3339 timestamp. Python 3.10's fromisoformat does not accept "Z"."""
    parsed = datetime.fromisoformat(value.replace("Z", "+00:00"))
    if parsed.tzinfo is None:
        raise ValueError(f"timestamp without a time zone: {value!r}")
    return parsed.astimezone(timezone.utc)


def without_generated_at(report: dict[str, Any]) -> dict[str, Any]:
    """Two runs give the same report except for the time it was made."""
    return {key: value for key, value in report.items() if key != "generated_at"}
