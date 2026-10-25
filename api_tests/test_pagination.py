"""Coverage 1: pagination and --limit."""

from __future__ import annotations

from conftest import report_of, run_ghreport
from support.runner import without_generated_at


def test_proxy_without_rules_changes_nothing(proxy, repo, tmp_path):
    """The proxy is the base of many tests; through it, untouched, the report
    must equal the one made directly."""
    direct = report_of(run_ghreport(repo, tmp_path / "direct"))
    proxied = report_of(run_ghreport(repo, tmp_path / "proxied", base=proxy.url))
    assert without_generated_at(proxied) == without_generated_at(direct)
    assert proxy.requests, "ghreport did not go through the proxy"
