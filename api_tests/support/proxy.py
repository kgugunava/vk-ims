"""A local proxy between ghreport and GITHUB_API_BASE_URL that injects faults.

401, 429, 5xx or broken JSON cannot be had on demand from an arbitrary server
that follows the contract, so tests point ghreport at this proxy instead and
tell it what to break. It only ever forwards to GITHUB_API_BASE_URL, so the
suite still talks to no other host.

What it can do:
- answer with a given status, headers and body instead of forwarding;
- forward, then truncate the body or edit the JSON array of issues;
- rewrite per_page, so any repository spans several pages, and rewrite the
  Link and Location headers to point back at the proxy;
- record the requests ghreport sent, with their headers;
- serve a real repository under a made-up name, which gives a test a second
  working repository whatever data the server has.

It replaces ghreport's Authorization header with GITHUB_TOKEN, or drops it,
so a token made up by a test never reaches the real server.
"""

from __future__ import annotations

import json
import threading
import urllib.error
import urllib.request
from dataclasses import dataclass, field
from email.message import Message
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from typing import Any, Callable
from urllib.parse import parse_qsl, urlencode, urlsplit

from support.reference import next_link

# Headers passed on in each direction; everything else is left out.
_REQUEST_HEADERS = ("Accept", "User-Agent", "X-GitHub-Api-Version", "If-None-Match")
_RESPONSE_HEADERS = (
    "Content-Type", "ETag", "Link", "Location", "Retry-After",
    "X-RateLimit-Limit", "X-RateLimit-Remaining", "X-RateLimit-Reset",
)

Items = list[dict[str, Any]]


@dataclass
class Recorded:
    """A request ghreport sent to the proxy, and what it got back."""
    path: str
    # As sent by ghreport, Authorization included. A Message, so lookups
    # ignore case: Go sends X-GitHub-Api-Version as X-Github-Api-Version.
    headers: Message
    status: int = 0
    served_next: str | None = None  # the Link rel="next" the proxy answered with
    served_etag: str | None = None  # the ETag the proxy answered with


@dataclass
class _Rule:
    times: int | None  # None: every matching request
    path_contains: str | None
    status: int = 0
    headers: dict[str, str] = field(default_factory=dict)
    body: bytes = b""
    transform: Callable[[Items], Items] | None = None
    corrupt: bool = False

    def matches(self, path: str) -> bool:
        if self.times == 0:
            return False
        return self.path_contains is None or self.path_contains in path

    def use(self) -> None:
        if self.times is not None:
            self.times -= 1


class _NoRedirect(urllib.request.HTTPRedirectHandler):
    """Redirects go back to ghreport untouched: following them is its job."""

    def redirect_request(self, *args: Any, **kwargs: Any) -> None:
        return None


class Proxy:
    def __init__(self, upstream: str, token: str | None) -> None:
        self.upstream = upstream.rstrip("/")
        self._token = token
        self._lock = threading.Lock()
        self._faults: list[_Rule] = []
        self._edits: list[_Rule] = []
        self._page_size: int | None = None
        self._aliases: dict[str, str] = {}
        self._opener = urllib.request.build_opener(_NoRedirect)
        self.requests: list[Recorded] = []

        handler = type("Handler", (_Handler,), {"proxy": self})
        self._server = ThreadingHTTPServer(("127.0.0.1", 0), handler)
        self.url = f"http://127.0.0.1:{self._server.server_address[1]}"

    def start(self) -> None:
        threading.Thread(target=self._server.serve_forever, daemon=True).start()

    def stop(self) -> None:
        self._server.shutdown()
        self._server.server_close()

    # Rules. `times=None` applies to every matching request; `path_contains`
    # limits a rule to paths with that substring, e.g. a repository name.

    def fail(self, status: int, headers: dict[str, str] | None = None, body: bytes = b"",
             times: int | None = None, path_contains: str | None = None) -> None:
        """Answer with this response instead of forwarding."""
        with self._lock:
            self._faults.append(_Rule(times, path_contains, status, headers or {}, body))

    def corrupt(self, times: int | None = 1, path_contains: str | None = None) -> None:
        """Forward, then cut the end off a 200 body so it is not valid JSON."""
        with self._lock:
            self._edits.append(_Rule(times, path_contains, corrupt=True))

    def transform(self, edit: Callable[[Items], Items], times: int | None = 1,
                  path_contains: str | None = None) -> None:
        """Forward, then replace the JSON array of a 200 response with edit(items)."""
        with self._lock:
            self._edits.append(_Rule(times, path_contains, transform=edit))

    def page_size(self, size: int) -> None:
        """Forward per_page=size instead of what ghreport asked for."""
        self._page_size = size

    def alias(self, name: str, repo: str) -> None:
        """Serve `repo` under the repository name `name` (owner/repo)."""
        self._aliases[name] = repo

    # Request handling.

    def _take(self, rules: list[_Rule], path: str) -> _Rule | None:
        with self._lock:
            for rule in rules:
                if rule.matches(path):
                    rule.use()
                    return rule
        return None

    def _upstream_url(self, path: str) -> str:
        parts = urlsplit(path)
        upstream_path = parts.path
        for name, repo in self._aliases.items():
            upstream_path = upstream_path.replace(f"/repos/{name}/", f"/repos/{repo}/")
        query = parse_qsl(parts.query, keep_blank_values=True)
        if self._page_size is not None:
            query = [(key, str(self._page_size) if key == "per_page" else value) for key, value in query]
        return self.upstream + upstream_path + ("?" + urlencode(query) if query else "")

    def _to_proxy(self, value: str, host: str | None) -> str:
        """Point URLs of the upstream back at the proxy, under the host name
        the client used: a ghreport in a container reaches the proxy as
        host.docker.internal, where 127.0.0.1 would be the container itself."""
        return value.replace(self.upstream, f"http://{host}" if host else self.url)

    def forward(self, path: str, headers: Message) -> tuple[int, dict[str, str], bytes]:
        request_headers = {name: headers[name] for name in _REQUEST_HEADERS if headers.get(name) is not None}
        if self._token:
            request_headers["Authorization"] = f"Bearer {self._token}"
        request = urllib.request.Request(self._upstream_url(path), headers=request_headers)
        try:
            with self._opener.open(request, timeout=30) as response:
                status, response_headers, body = response.status, response.headers, response.read()
        except urllib.error.HTTPError as error:
            status, response_headers, body = error.code, error.headers, error.read()
        out_headers = {
            name: self._to_proxy(response_headers[name], headers.get("Host"))
            for name in _RESPONSE_HEADERS if response_headers.get(name) is not None
        }
        return status, out_headers, body

    def edit(self, path: str, status: int, body: bytes) -> bytes:
        if status != 200:
            return body
        rule = self._take(self._edits, path)
        if rule is None:
            return body
        if rule.corrupt:
            return body[: max(len(body) - 3, 1)]
        items = json.loads(body)
        return json.dumps(rule.transform(items), ensure_ascii=False).encode()


class _Handler(BaseHTTPRequestHandler):
    proxy: Proxy

    def do_GET(self) -> None:  # noqa: N802 (name fixed by BaseHTTPRequestHandler)
        proxy = self.proxy
        record = Recorded(self.path, self.headers)
        with proxy._lock:
            proxy.requests.append(record)

        fault = proxy._take(proxy._faults, self.path)
        if fault is not None:
            status, headers, body = fault.status, dict(fault.headers), fault.body
        else:
            status, headers, body = proxy.forward(self.path, record.headers)
            body = proxy.edit(self.path, status, body)

        record.status = status
        record.served_next = next_link(headers.get("Link"))
        record.served_etag = headers.get("ETag")
        self.send_response(status)
        for name, value in headers.items():
            self.send_header(name, value)
        if status != 304:
            self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        if body and status != 304:
            self.wfile.write(body)

    def log_message(self, format: str, *args: Any) -> None:  # noqa: A002 (base signature)
        pass
