#!/usr/bin/env python3
"""Checks that the test suite catches broken servers and broken services.

Server mutants: the stub server with one contract violation each
(stub/server.py MUTANTS). The suite must fail against every mutant that
breaks the contract and pass against the correct stub and the acceptable
variants.

Service mutants: ghreport built from its sources with one bug planted each.
The suite must fail against every one of them.

    python3 api_tests/tools/mutation_check.py [--only server|service] [--markdown FILE]

Needs Go to build the service mutants. Takes a few minutes: the whole suite
runs once per mutant.
"""

from __future__ import annotations

import argparse
import os
import shutil
import subprocess
import sys
import tempfile
import xml.etree.ElementTree as ElementTree
from dataclasses import dataclass
from pathlib import Path

API_TESTS = Path(__file__).resolve().parent.parent
SERVICE = API_TESTS.parent / "service"
sys.path.insert(0, str(API_TESTS))

from stub.server import MUTANTS  # noqa: E402 (needs the path above)


@dataclass(frozen=True)
class ServiceMutant:
    name: str
    description: str
    file: str  # relative to service/
    old: str
    new: str


# Each replaces one piece of code with a typical bug. The old text must occur
# exactly once, so a refactoring that moves it makes this tool fail loudly
# instead of silently testing the original.
SERVICE_MUTANTS = [
    ServiceMutant("keeps_pull_requests", "pull requests are not filtered out",
                  "internal/report/collect.go",
                  'if _, isPull := fields["pull_request"]; isPull {',
                  'if _, isPull := fields["pull_request"]; isPull && false {'),
    ServiceMutant("keeps_duplicates", "repeated numbers are not removed",
                  "internal/report/collect.go",
                  "if c.seen[*item.Number] {", "if false && c.seen[*item.Number] {"),
    ServiceMutant("ignores_since", "--since is not applied",
                  "internal/report/collect.go",
                  "if !c.since.IsZero() && created.Before(c.since) {", "if false {"),
    ServiceMutant("unsorted", "issues are not sorted",
                  "internal/report/collect.go",
                  "slices.SortStableFunc(issues, func", "slices.SortStableFunc(issues[:0], func"),
    ServiceMutant("limit_off_by_one", "--limit lets one issue too many through",
                  "internal/report/collect.go",
                  "return len(c.issues) >= c.limit", "return len(c.issues) > c.limit"),
    ServiceMutant("first_page_only", "Link rel=next is not followed",
                  "internal/ghapi/issues.go",
                  'if collector.Full() || page.NextURL == "" {', "if true {"),
    ServiceMutant("no_conditional_requests", "If-None-Match is never sent",
                  "internal/ghapi/client.go",
                  "etag = cached.ETag", 'etag = ""'),
    ServiceMutant("no_rate_limit_wait", "rate-limit pauses are skipped",
                  "internal/ghapi/client.go",
                  "if err := c.sleep(ctx, pause); err != nil {", "if err := c.sleep(ctx, 0); err != nil {"),
    ServiceMutant("no_5xx_retry", "5xx responses are not retried",
                  "internal/ghapi/client.go",
                  "if resp.status >= http.StatusInternalServerError {", "if false {"),
    ServiceMutant("attempt_always_1", "the trace numbers every attempt 1",
                  "internal/ghapi/client.go",
                  "c.record(target, resp.status, attempt, resp.status",
                  "c.record(target, resp.status, 1, resp.status"),
    ServiceMutant("token_in_trace", "the token ends up in the trace",
                  "internal/ghapi/client.go",
                  "URL:       target,", 'URL:       target + "#" + c.token,'),
    ServiceMutant("unauthorized_not_fatal", "401 is reported like any other failure",
                  "cmd/ghreport/main.go",
                  "case errors.Is(err, ghapi.ErrUnauthorized):", "case false:"),
    ServiceMutant("partial_failure_exits_0", "exit code 0 although a repository failed",
                  "cmd/ghreport/main.go",
                  "if failed > 0 {\n\t\treturn exitPartial", "if failed > 0 {\n\t\treturn exitOK"),
]


@dataclass
class Outcome:
    name: str
    description: str
    must_fail: bool
    failed_tests: list[str]
    errors: str = ""

    @property
    def ok(self) -> bool:
        return bool(self.failed_tests) == self.must_fail and not self.errors


def start_stub(mutant: str | None) -> tuple[subprocess.Popen[str], str]:
    cmd = [sys.executable, "-m", "stub.server", "--port", "0"] + (["--mutant", mutant] if mutant else [])
    stub = subprocess.Popen(cmd, cwd=API_TESTS, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, text=True)
    first_line = stub.stdout.readline().strip() if stub.stdout else ""
    if not first_line.startswith("listening on "):
        stub.kill()
        raise RuntimeError(f"stub did not start: {first_line!r}")
    return stub, first_line.removeprefix("listening on ")


def run_suite(binary: Path, base_url: str, workdir: Path) -> tuple[list[str], str]:
    """Runs the suite; returns the failed test ids and any error that kept
    pytest from running at all."""
    junit = workdir / "junit.xml"
    env = dict(os.environ, GHREPORT_BIN=str(binary), GITHUB_API_BASE_URL=base_url)
    env.pop("GITHUB_TOKEN", None)  # the stub needs none
    proc = subprocess.run(
        [sys.executable, "-m", "pytest", "-q", "-p", "no:cacheprovider", f"--junitxml={junit}", str(API_TESTS)],
        env=env, capture_output=True, text=True, timeout=1800,
    )
    if not junit.exists():
        return [], proc.stdout[-2000:] + proc.stderr[-2000:]
    failed = []
    for case in ElementTree.parse(junit).iter("testcase"):
        if case.find("failure") is not None or case.find("error") is not None:
            failed.append(f"{case.get('classname', '').split('.')[-1]}::{case.get('name')}")
    return failed, ""


def check_servers(binary: Path, workdir: Path) -> list[Outcome]:
    variants: list[tuple[str | None, str, bool]] = [(None, "the correct stub", False)]
    variants += [(name, description, not acceptable) for name, (description, acceptable) in MUTANTS.items()]
    outcomes = []
    for mutant, description, must_fail in variants:
        stub, base_url = start_stub(mutant)
        try:
            failed, errors = run_suite(binary, base_url, workdir)
        finally:
            stub.kill()
            stub.wait()
        outcome = Outcome(mutant or "correct", description, must_fail, failed, errors)
        outcomes.append(outcome)
        report_progress("server", outcome)
    return outcomes


def build_service(source: Path, output: Path) -> None:
    env = dict(os.environ, CGO_ENABLED="0")
    proc = subprocess.run(["go", "build", "-o", str(output), "./cmd/ghreport"], cwd=source, env=env,
                          capture_output=True, text=True)
    if proc.returncode != 0:
        raise RuntimeError(f"go build in {source} failed:\n{proc.stderr}")


def skip_build_outputs(directory: str, names: list[str]) -> set[str]:
    """Leaves out a built binary and cache at the top of service/ when copying
    it, but not cmd/ghreport, which has the same name."""
    if Path(directory) != SERVICE:
        return set()
    return {name for name in names if name in ("ghreport", ".ghreport-cache")}


def check_services(workdir: Path) -> list[Outcome]:
    stub, base_url = start_stub(None)
    outcomes = []
    try:
        for mutant in SERVICE_MUTANTS:
            source = workdir / "service"
            shutil.rmtree(source, ignore_errors=True)
            shutil.copytree(SERVICE, source, ignore=skip_build_outputs)
            target = source / mutant.file
            code = target.read_text()
            if code.count(mutant.old) != 1:
                raise RuntimeError(f"{mutant.name}: the code to replace occurs {code.count(mutant.old)} times in {mutant.file}")
            target.write_text(code.replace(mutant.old, mutant.new))
            binary = workdir / f"ghreport-{mutant.name}"
            build_service(source, binary)

            failed, errors = run_suite(binary, base_url, workdir)
            outcome = Outcome(mutant.name, mutant.description, True, failed, errors)
            outcomes.append(outcome)
            report_progress("service", outcome)
    finally:
        stub.kill()
        stub.wait()
    return outcomes


def report_progress(kind: str, outcome: Outcome) -> None:
    status = "ok " if outcome.ok else "BAD"
    print(f"[{status}] {kind} {outcome.name}: {len(outcome.failed_tests)} test(s) failed"
          + (f" — {outcome.errors.strip()[:200]}" if outcome.errors else ""), flush=True)


def markdown(server: list[Outcome], service: list[Outcome]) -> str:
    lines = []
    for title, outcomes in (("Broken servers", server), ("Broken services", service)):
        if not outcomes:
            continue
        lines += [f"### {title}", "", "| Variant | What is broken | Expected | Failed tests |", "|---|---|---|---|"]
        for o in outcomes:
            expected = "suite fails" if o.must_fail else "suite passes"
            shown = ", ".join(f"`{t}`" for t in o.failed_tests[:3])
            more = f" and {len(o.failed_tests) - 3} more" if len(o.failed_tests) > 3 else ""
            result = shown + more if o.failed_tests else "none"
            mark = "" if o.ok else " **(unexpected)**"
            lines.append(f"| `{o.name}` | {o.description} | {expected} | {result}{mark} |")
        lines.append("")
    return "\n".join(lines)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    parser.add_argument("--only", choices=["server", "service"])
    parser.add_argument("--markdown", type=Path, help="also write the result table here")
    args = parser.parse_args()

    with tempfile.TemporaryDirectory() as tmp:
        workdir = Path(tmp)
        binary = workdir / "ghreport"
        build_service(SERVICE, binary)
        server = check_servers(binary, workdir) if args.only != "service" else []
        service = check_services(workdir) if args.only != "server" else []

    table = markdown(server, service)
    print("\n" + table)
    if args.markdown:
        args.markdown.write_text(table)
    return 0 if all(o.ok for o in server + service) else 1


if __name__ == "__main__":
    sys.exit(main())
