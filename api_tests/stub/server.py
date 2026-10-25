"""Stub of GET /repos/{owner}/{repo}/issues, following the GitHub contract.

`--mutant NAME` breaks one part of the contract, so that the test suite can be
checked for catching it. Mutants marked acceptable stay within the contract
and the suite must still pass against them.

    python -m stub.server --port 8080 [--mutant skip_item]

With --port 0 a free port is chosen; the first line on stdout is always
"listening on http://127.0.0.1:PORT".
"""

from __future__ import annotations

import argparse
import hashlib
import json
import re
import sys
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from typing import Any
from urllib.parse import parse_qs, urlencode, urlsplit

from stub.data import repositories

# Name -> (description, whether the suite must still pass against it).
MUTANTS: dict[str, tuple[str, bool]] = {
    "skip_item": ("the first item of every page after the first is missing", False),
    "pr_without_key": ("one pull request has no pull_request key", False),
    "304_without_inm": ("304 on the first page even without If-None-Match", False),
    "corrupt_json": ("the first page body is truncated JSON", False),
    "desc_sort": ("items come newest first even with direction=asc", False),
    "missing_link": ("no Link header, even when more pages follow", False),
    "no_etag": ("no ETag header and no conditional requests", False),
    "duplicate_item": ("every page after the first repeats the last item of the previous one", True),
    "link_omitted_on_304": ("304 responses carry no Link header", True),
}

_ISSUES_PATH = re.compile(r"^/repos/(?P<owner>[^/]+)/(?P<repo>[^/]+)/issues$")
_DEFAULT_PER_PAGE = 30
_MAX_PER_PAGE = 100


class StubHandler(BaseHTTPRequestHandler):
    # Set on the server class by make_server().
    mutant: str | None = None
    repos: dict[str, list[dict[str, Any]]] = {}

    def do_GET(self) -> None:  # noqa: N802 (name fixed by BaseHTTPRequestHandler)
        url = urlsplit(self.path)
        match = _ISSUES_PATH.match(url.path)
        repo = f"{match['owner']}/{match['repo']}" if match else None
        if repo not in self.repos:
            self._send_json(404, {"message": "Not Found"})
            return

        query = {key: values[-1] for key, values in parse_qs(url.query).items()}
        try:
            per_page = min(max(int(query.get("per_page", _DEFAULT_PER_PAGE)), 1), _MAX_PER_PAGE)
            page = max(int(query.get("page", 1)), 1)
        except ValueError:
            self._send_json(422, {"message": "Validation Failed"})
            return

        items = list(self.repos[repo])
        pr_without_key = self.mutant == "pr_without_key"
        if pr_without_key:
            items = [dict(item) for item in items]
            first_pr = next(item for item in items if "pull_request" in item)
            del first_pr["pull_request"]
        # GitHub's default direction is desc.
        descending = query.get("direction", "desc") == "desc" or self.mutant == "desc_sort"
        if descending:
            items.reverse()

        last_page = max((len(items) + per_page - 1) // per_page, 1)
        start = (page - 1) * per_page
        page_items = items[start:start + per_page]
        if page > 1 and page_items:
            if self.mutant == "skip_item":
                page_items = page_items[1:]
            elif self.mutant == "duplicate_item":
                page_items = [items[start - 1]] + page_items

        body = json.dumps(page_items, ensure_ascii=False).encode()
        if self.mutant == "corrupt_json" and page == 1:
            body = body[:-3]

        headers = {"Content-Type": "application/json; charset=utf-8"}
        etag = '"' + hashlib.sha256(body).hexdigest()[:20] + '"'
        if self.mutant != "no_etag":
            headers["ETag"] = etag
        link = self._link_header(query, page, last_page)
        if link and self.mutant != "missing_link":
            headers["Link"] = link

        if_none_match = self.headers.get("If-None-Match")
        not_modified = self.mutant != "no_etag" and if_none_match == etag
        if self.mutant == "304_without_inm" and page == 1 and if_none_match is None:
            not_modified = True
        if not_modified:
            if self.mutant == "link_omitted_on_304":
                headers.pop("Link", None)
            self._send(304, headers, b"")
            return
        self._send(200, headers, body)

    def _link_header(self, query: dict[str, str], page: int, last_page: int) -> str:
        """A GitHub-style Link header with absolute URLs to this server."""
        base = f"http://{self.headers.get('Host', 'localhost')}" + urlsplit(self.path).path

        def page_url(number: int) -> str:
            return base + "?" + urlencode({**query, "page": str(number)})

        parts = []
        if page < last_page:
            parts.append(f'<{page_url(page + 1)}>; rel="next"')
            parts.append(f'<{page_url(last_page)}>; rel="last"')
        if page > 1:
            parts.append(f'<{page_url(1)}>; rel="first"')
            parts.append(f'<{page_url(page - 1)}>; rel="prev"')
        return ", ".join(parts)

    def _send_json(self, status: int, payload: Any) -> None:
        self._send(status, {"Content-Type": "application/json"}, json.dumps(payload).encode())

    def _send(self, status: int, headers: dict[str, str], body: bytes) -> None:
        self.send_response(status)
        for name, value in headers.items():
            self.send_header(name, value)
        if status != 304:
            self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        if body:
            self.wfile.write(body)

    def log_message(self, format: str, *args: Any) -> None:  # noqa: A002 (base signature)
        sys.stderr.write("stub: " + format % args + "\n")


def make_server(port: int = 0, mutant: str | None = None) -> ThreadingHTTPServer:
    """A stub server bound to 127.0.0.1:port (0 picks a free port), not yet serving."""
    if mutant is not None and mutant not in MUTANTS:
        raise ValueError(f"unknown mutant {mutant!r}; known: {', '.join(MUTANTS)}")
    handler = type("Handler", (StubHandler,), {"mutant": mutant, "repos": repositories()})
    return ThreadingHTTPServer(("127.0.0.1", port), handler)


def serve_in_thread(port: int = 0, mutant: str | None = None) -> tuple[ThreadingHTTPServer, str]:
    """Starts a stub server in a daemon thread; returns it and its base URL."""
    server = make_server(port, mutant)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    return server, f"http://127.0.0.1:{server.server_address[1]}"


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    parser.add_argument("--port", type=int, default=8080)
    parser.add_argument("--mutant", choices=sorted(MUTANTS))
    args = parser.parse_args()
    server = make_server(args.port, args.mutant)
    print(f"listening on http://127.0.0.1:{server.server_address[1]}", flush=True)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass


if __name__ == "__main__":
    main()
