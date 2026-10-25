"""Coverage 7: the report schema and the order of issues."""

from __future__ import annotations

from conftest import report_of, run_ghreport


def test_report_schema(tmp_path, repo):
    proc = run_ghreport(repo, tmp_path / "cache")
    report = report_of(proc)
    assert report["repos"][0]["repo"] == repo
    assert report["repos"][0]["error"] is None
