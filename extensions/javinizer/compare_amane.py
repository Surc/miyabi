"""Run the unchanged Amane v0.23.0 JavLibrary crawler against synthetic HTML.

These neutral contract inputs are NOT captured site pages or Amane's official
fixtures. The real crawler, parsel parser, HttpClient and curl_cffi WebClient run
against a loopback HTTP server. A separate --live option makes bounded requests
to the public site without a browser, configured proxy, or saved credentials.
"""

from __future__ import annotations

import argparse
import asyncio
import contextlib
import importlib.metadata
import json
import logging
import os
import subprocess
import sys
import threading
import time
from dataclasses import dataclass
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from typing import Iterator
from urllib.parse import urlencode

import structlog

import amane.crawlers.sites.javlibrary as javlibrary_module
from amane.config import SiteConfig
from amane.crawlers.http import HttpClient
from amane.crawlers.models import MediaMetadata, SearchQuery
from amane.crawlers.sites.javlibrary import JavLibraryCrawler
from amane.net.errors import RequestError, SourceError
from amane.net.http import RateLimiters, WebClient


@dataclass(frozen=True)
class Case:
    name: str
    number: str
    operation: str
    routes: dict[str, str]
    target: str
    expected: dict[str, object]
    purpose: str


def detail(number: str | None, rating: str = "legacy", value: str = "8.4") -> str:
    identity = (
        f'<div id="video_id"><table><tr><td class="text">{number}</td></tr></table></div>'
        if number is not None else ""
    )
    fields = {
        "legacy": f'<div id="video_review"><span class="score">({value})</span></div>',
        "script": f'<script>var $rating = "{value}";</script>',
        "num": f'<div id="video_rating"><span class="num">{value}</span></div>',
        "none": "",
    }
    return (
        '<!doctype html><html><head><title>Neutral metadata fixture</title></head><body>'
        '<h3 class="post-title"><a>Neutral metadata fixture</a></h3>'
        f'<div id="video_info">{identity}</div>{fields[rating]}'
        '<img id="video_jacket_img" src="/fixture-cover.jpg"></body></html>'
    )


def listing(entries: list[tuple[str, str]]) -> str:
    return "<html><body>" + "".join(
        f'<div class="video"><a href="{href}" title="{number} Neutral fixture">'
        f'<div class="id">{number}</div></a></div>' for href, number in entries
    ) + "</body></html>"


def metadata(number: str, score: float | None = 8.4) -> dict[str, object]:
    return {"outcome": "metadata", "number": number, "score": score}


def cases() -> list[Case]:
    result: list[Case] = []
    for index, (rating, label) in enumerate([
        ("legacy", "legacy_parenthesized_score"),
        ("script", "script_rating"),
        ("num", "video_rating_num"),
    ], start=101):
        number = f"ABP-{index}"
        result.append(Case(label, number, "scrape", {"/cn/detail.html": detail(number, rating)},
                           "/cn/detail.html", metadata(number), "Return the source's 8.4 rating."))

    def add_fetch(name: str, number: str, search: str, routes: dict[str, str],
                  expected: dict[str, object], purpose: str) -> None:
        path = "/cn/vl_searchbyid.php?" + urlencode({"keyword": number})
        result.append(Case(name, number, "fetch", {path: search, **routes}, path, expected, purpose))

    add_fetch("direct_detail_search", "ABP-104", detail("ABP-104"), {}, metadata("ABP-104"),
              "A search endpoint that returns the detail document should preserve its rating.")
    add_fetch("root_relative_detail_link", "ABP-105", listing([("/cn/detail.html", "ABP-105")]),
              {"/cn/detail.html": detail("ABP-105")}, metadata("ABP-105"), "Positive list-to-detail control.")
    add_fetch("relative_query_detail_link", "ABP-106", listing([("./?v=fixture", "ABP-106")]),
              {"/cn/?v=fixture": detail("ABP-106")}, metadata("ABP-106"),
              "Resolve ./?v relative to the search page's /cn/ directory.")
    add_fetch("relative_html_detail_link", "ABP-107", listing([("fixture.html", "ABP-107")]),
              {"/cn/fixture.html": detail("ABP-107")}, metadata("ABP-107"),
              "Resolve .html relative to the search page's /cn/ directory.")
    add_fetch("wrong_prefix_before_exact", "ABP-108",
              listing([("/cn/wrong.html", "ABP-1080"), ("/cn/exact.html", "ABP-108")]),
              {"/cn/wrong.html": detail("ABP-1080"), "/cn/exact.html": detail("ABP-108")},
              metadata("ABP-108"), "Reject a longer number sharing the requested prefix.")
    add_fetch("unrelated_first_fallback", "ABP-109", listing([("/cn/wrong.html", "ABP-999")]),
              {"/cn/wrong.html": detail("ABP-999")}, {"outcome": "none"},
              "Do not return another number's rating when no exact result exists.")
    result.append(Case("missing_actual_number", "ABP-110", "scrape",
                       {"/cn/detail.html": detail(None)}, "/cn/detail.html", {"outcome": "none"},
                       "Require an actual page number before accepting a rating."))
    result.append(Case("unrated_detail", "ABP-111", "scrape",
                       {"/cn/detail.html": detail("ABP-111", "none")}, "/cn/detail.html",
                       metadata("ABP-111", None), "Do not invent a score on an unrated detail."))
    add_fetch("empty_results_document", "ABP-112", "<html><body>No results</body></html>", {},
              {"outcome": "none"}, "No source score when the result list is empty.")
    add_fetch("empty_response", "ABP-113", "", {},
              {"outcome": "source_error", "reason": "empty_response", "http_status": None},
              "Classify an empty transport response explicitly.")
    challenge = "<html><title>Just a moment</title><script>window._cf_chl_opt={};</script>Cloudflare</html>"
    add_fetch("search_challenge", "ABP-114", challenge, {},
              {"outcome": "source_error", "reason": "cloudflare_challenge", "http_status": None},
              "Classify a challenge without claiming a successful source read.")
    result.append(Case("detail_challenge", "ABP-115", "scrape", {"/cn/detail.html": challenge},
                       "/cn/detail.html", {"outcome": "source_error", "reason": "cloudflare_challenge", "http_status": None},
                       "Apply the same classification at the detail stage."))
    result.append(Case("out_of_range_rating", "ABP-116", "scrape",
                       {"/cn/detail.html": detail("ABP-116", value="99")}, "/cn/detail.html",
                       metadata("ABP-116", None), "Never expose an invalid rating outside 0 to 10."))
    add_fetch("title_attribute_alignment", "ABP-117",
              '<html><div class="video"><a href="/cn/wrong.html">Missing title</a></div>'
              '<div class="video"><a href="/cn/exact.html" title="ABP-117 Neutral fixture">Exact</a></div></html>',
              {"/cn/wrong.html": detail("ABP-999"), "/cn/exact.html": detail("ABP-117")},
              metadata("ABP-117"), "Keep each result's title and href paired if an attribute is absent.")
    return result


class FixtureServer(ThreadingHTTPServer):
    def __init__(self) -> None:
        self.routes: dict[str, str] = {}
        self.requests: list[str] = []
        super().__init__(("127.0.0.1", 0), FixtureHandler)


class FixtureHandler(BaseHTTPRequestHandler):
    def do_GET(self) -> None:
        server: FixtureServer = self.server  # type: ignore[assignment]
        server.requests.append(self.path)
        exists = self.path in server.routes
        body = server.routes.get(self.path, "Missing fixture route").encode()
        self.send_response(200 if exists else 404)
        self.send_header("Content-Type", "text/html; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, format: str, *args: object) -> None:
        pass


@contextlib.contextmanager
def direct_network() -> Iterator[None]:
    """Match the comparison engine's disabled application proxy in this process."""
    keys = ("HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy")
    previous = {key: os.environ.pop(key) for key in keys if key in os.environ}
    try:
        yield
    finally:
        os.environ.update(previous)


def summarize(result: MediaMetadata | None) -> dict[str, object]:
    return {"outcome": "none"} if result is None else metadata(result.number, result.score)


async def observe(crawler: JavLibraryCrawler, operation: str, number: str, url: str,
                  *, include_diagnostics: bool = False) -> dict[str, object]:
    try:
        result = await (crawler.fetch(SearchQuery(number=number)) if operation == "fetch"
                        else crawler._scrape(url))
        return summarize(result)
    except SourceError as exc:
        error: dict[str, object] = {"outcome": "source_error", "reason": str(exc.reason), "http_status": exc.http_status}
        if include_diagnostics and isinstance(exc, RequestError) and exc.failure is not None:
            error["transport_kind"] = str(exc.failure.kind)
        return error
    except Exception as exc:
        return {"outcome": "exception", "type": type(exc).__name__}


async def run_fixtures() -> dict[str, object]:
    server = FixtureServer()
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    base_url = f"http://127.0.0.1:{server.server_port}"
    web = WebClient(limiters=RateLimiters(), timeout=3, max_retries=0)
    crawler = JavLibraryCrawler(HttpClient(web), SiteConfig(base_url=base_url))
    rows: list[dict[str, object]] = []
    try:
        for case in cases():
            server.routes = case.routes
            server.requests = []
            observed = await observe(crawler, case.operation, case.number, base_url + case.target)
            rows.append({"name": case.name, "purpose": case.purpose, "expected": case.expected,
                         "observed": observed, "contract_pass": observed == case.expected,
                         "http_requests": list(server.requests),
                         "image_requests": sum("fixture-cover" in path for path in server.requests)})
    finally:
        await web.close()
        server.shutdown()
        server.server_close()
        thread.join(timeout=2)
    return {"kind": "synthetic_fixture_contract", "real_components": ["JavLibraryCrawler", "parsel", "HttpClient", "WebClient", "curl_cffi"],
            "passed": sum(row["contract_pass"] is True for row in rows), "total": len(rows), "cases": rows}


async def run_live(number: str) -> dict[str, object]:
    web = WebClient(limiters=RateLimiters(), timeout=10, max_retries=0)
    crawler = JavLibraryCrawler(HttpClient(web))
    started = time.monotonic()
    try:
        async with asyncio.timeout(20):
            observed = await observe(crawler, "fetch", number, "", include_diagnostics=True)
    except TimeoutError:
        observed = {"outcome": "timeout", "reason": "20_second_total_deadline"}
    finally:
        await web.close()
    return {"kind": "live_public_site", "base_url": crawler.base_url, "number": number,
            "proxy": "disabled", "browser": "disabled", "credentials": "none",
            "duration_seconds": round(time.monotonic() - started, 2), "observed": observed,
            "scope_limit": "This runtime's outbound network; not the user's NAS/browser/session."}


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source", required=True, type=Path, help="Clean Amane v0.23.0 source checkout")
    parser.add_argument("--live", action="store_true")
    parser.add_argument("--number", default="IPX-535")
    parser.add_argument("--output", required=True, type=Path, help="JSON report path outside the repository")
    args = parser.parse_args()
    structlog.configure(wrapper_class=structlog.make_filtering_bound_logger(logging.CRITICAL))
    source = args.source.resolve()
    expected_commit = "4230de458c3197e1e17b6c5b42cc385fc6961d15"
    try:
        commit = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=source, text=True).strip()
        dirty = subprocess.check_output(
            ["git", "status", "--porcelain", "--untracked-files=no"], cwd=source, text=True
        ).strip()
    except (OSError, subprocess.CalledProcessError):
        parser.error("--source must point to an existing Amane Git checkout")
    if commit != expected_commit:
        parser.error(f"expected Amane v0.23.0 commit {expected_commit}; review and update the pin before testing another revision")
    if dirty:
        parser.error("--source must have no tracked modifications")
    expected_module = source / "src/amane/crawlers/sites/javlibrary.py"
    if Path(javlibrary_module.__file__).resolve() != expected_module:
        parser.error("PYTHONPATH must use the --source checkout's src directory")
    report: dict[str, object] = {
        "source_repository": "https://github.com/sqzw-x/amane", "source_tag": "v0.23.0",
        "source_commit": commit,
        "python": sys.version.split()[0],
        "dependencies": {name: importlib.metadata.version(name) for name in ("parsel", "curl-cffi", "pydantic")},
        "fixture_policy": "Synthetic neutral boundary inputs, not captured pages or official Amane fixtures.",
    }
    with direct_network():
        report["fixtures"] = asyncio.run(run_fixtures())
        if args.live:
            report["live"] = asyncio.run(run_live(args.number))
    args.output.write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n")
    print(json.dumps(report, ensure_ascii=False, indent=2))
    fixtures = report["fixtures"]
    assert isinstance(fixtures, dict)
    return 0 if fixtures["passed"] == fixtures["total"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
