#!/usr/bin/env python3
"""Replay neutral fixtures through a real, isolated Javinizer API process.

This tests the engine contract, not live JavLibrary availability. A nonzero
exit identifies a failed safety or compatibility case. No existing media files
are mounted or organized. Fixture mode blocks image transfers; use the patched
rating-only mode for live probes. All engine state is temporary.
"""

import argparse
import hashlib
import http.cookiejar
import json
import os
from pathlib import Path
import secrets
import socket
import subprocess
import tempfile
import threading
import time
import urllib.error
import urllib.parse
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


OFFICIAL_SHA256 = "c0979f9c651b365001a00a4a109b5c04b2582094ff91645cc9ee873545392887"
CASES = {
    "ABP-101": ("direct_script_rating", True),
    "ABP-102": ("wrong_page_code", False),
    "ABP-103": ("missing_page_code", False),
    "ABP-104": ("unrated", False),
    "ABP-105": ("legacy_query_link", True),
    "ABP-106": ("modern_search_entry", True),
    "ABP-107": ("wrong_first_result_missing_page_code", False),
    "ABP-108": ("empty_search", False),
    "ABP-109": ("video_rating_span_num", True),
    "ABP-110": ("legacy_video_review_score", True),
    "ABP-111": ("html_search_link", True),
    "ABP-112": ("challenge_page", False),
    "ABP-113": ("legacy_score_missing_closing_parenthesis", False),
    "ABP-114": ("legacy_score_missing_opening_parenthesis", False),
}


def detail(code, score="8.4", layout="script"):
    identity = (
        f'<div id="video_id"><td class="text">{code}</td></div>' if code else ""
    )
    ratings = {
        "script": f'<script>var $rating = "{score}";</script>',
        "span": f'<div id="video_rating"><span class="num">{score}</span></div>',
        "legacy": f'<div id="video_review"><span class="score">({score})</span></div>',
    }
    rating = ratings[layout] if score else ""
    # This synthetic DMM-shaped URL exercises the engine's dimension probe.
    # The fixture proxy rejects CONNECT, so it cannot fetch external images.
    cover = (
        '<img id="video_jacket_img" '
        'src="https://pics.dmm.co.jp/digital/video/abp00101/abp00101pl.jpg">'
    )
    return (
        '<html><head><title>Neutral test record - JAVLibrary</title></head><body>'
        f'<div id="video_info">{identity}</div>{rating}{cover}</body></html>'
    )


class Fixtures(BaseHTTPRequestHandler):
    image_requests = 0
    site_requests = 0
    html_requests = 0

    def log_message(self, *_args):
        pass

    def do_CONNECT(self):
        type(self).image_requests += 1
        self.send_response(502)
        self.end_headers()

    def do_GET(self):
        parsed = urllib.parse.urlparse(self.path)
        if parsed.path == "/fixture-cover.jpg":
            type(self).image_requests += 1
            self.send_response(404)
            self.end_headers()
            return
        type(self).site_requests += 1
        query = urllib.parse.parse_qs(parsed.query)
        # Exercise the canonical .html page even when a scraper rewrites the
        # search entry into a legacy query URL before following its redirect.
        if query.get("v") == ["javhtml"]:
            self.send_response(302)
            self.send_header("Location", "/cn/javhtml.html")
            self.end_headers()
            return
        code = query.get("keyword", [""])[0]
        pages = {
            "ABP-101": detail("ABP-101"),
            "ABP-102": detail("ABP-999"),
            "ABP-103": detail(""),
            "ABP-104": detail("ABP-104", ""),
            "ABP-105": '<a href="?v=javold">ABP-105</a>',
            "ABP-106": (
                '<div class="video" id="vid_javnew">'
                '<div class="id">ABP-106</div></div>'
            ),
            "ABP-107": (
                '<div class="video" id="vid_javwrong">'
                '<div class="id">ABP-999</div></div>'
            ),
            "ABP-108": '<html><div class="videos"></div></html>',
            "ABP-109": detail("ABP-109", layout="span"),
            "ABP-110": detail("ABP-110", layout="legacy"),
            "ABP-111": (
                '<div class="video" id="vid_javhtml"><a href="javhtml.html">'
                '<div class="id">ABP-111</div></a></div>'
            ),
            "ABP-112": (
                '<html><title>Just a moment...</title>'
                '<div id="cf-chl-wrapper">Checking your browser</div></html>'
            ),
            "ABP-113": detail("ABP-113", layout="legacy").replace("(8.4)", "(8.4"),
            "ABP-114": detail("ABP-114", layout="legacy").replace("(8.4)", "8.4)"),
        }
        body = pages.get(code)
        if body is None:
            body = {
                "javold": detail("ABP-105"),
                "javnew": detail("ABP-106"),
                "javwrong": detail(""),
            }.get(query.get("v", [""])[0], "")
        if parsed.path.endswith("/javhtml.html"):
            type(self).html_requests += 1
            body = detail("ABP-111")
        self.send_response(200)
        self.send_header("Content-Type", "text/html; charset=utf-8")
        self.end_headers()
        self.wfile.write(body.encode())


def free_port():
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


def run(binary, live_code=None, require_identity=False):
    Fixtures.image_requests = 0
    Fixtures.site_requests = 0
    Fixtures.html_requests = 0
    fixture = ThreadingHTTPServer(("127.0.0.1", 0), Fixtures)
    thread = threading.Thread(target=fixture.serve_forever, daemon=True)
    thread.start()
    try:
        with tempfile.TemporaryDirectory(prefix="miyabi-javinizer-") as temporary:
            directory = Path(temporary)
            port = free_port()
            config = {
                "config_version": 3,
                "server": {"host": "127.0.0.1", "port": port},
                "database": {"type": "sqlite", "dsn": str(directory / "test.db")},
                "system": {"version_check_enabled": False},
                "scrapers": {
                    "priority": ["javlibrary"], "scrape_actress": False,
                    "timeout_seconds": 10, "request_timeout_seconds": 20,
                    "r18dev": {"enabled": False},
                    "proxy": {
                        "enabled": not live_code,
                        "default_profile": "fixture",
                        "profiles": {
                            "fixture": {"url": f"http://127.0.0.1:{fixture.server_port}"}
                        },
                    },
                    "javlibrary": {
                        "enabled": True, "language": "cn", "use_flaresolverr": False,
                        "proxy": {"enabled": not live_code},
                        "base_url": (
                            "https://www.javlibrary.com" if live_code
                            else "http://www.javlibrary.com"
                        ),
                    },
                },
                "metadata": {
                    "priority": {"rating": ["javlibrary"]},
                    "translation": {"enabled": False}, "required_fields": [],
                },
                "output": {
                    key: False for key in (
                        "download_cover", "download_poster", "download_extrafanart",
                        "download_trailer", "download_actress",
                    )
                },
                "logging": {"level": "info", "output": "stdout"},
                "performance": {"max_workers": 1},
            }
            path = directory / "config.yaml"
            path.write_text(json.dumps(config))
            env = os.environ.copy()
            # Never inherit settings or bootstrap secrets for an existing server.
            env = {k: v for k, v in env.items() if not k.startswith("JAVINIZER_")}
            env.pop("MIYABI_JAVLIBRARY_RATING_ONLY", None)
            if require_identity:
                env["MIYABI_JAVLIBRARY_RATING_ONLY"] = "1"
            opener = urllib.request.build_opener(
                urllib.request.ProxyHandler({}),
                urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()),
            )
            base = f"http://127.0.0.1:{port}/api/v1"

            def request(endpoint, body=None, token=None):
                headers = {"Content-Type": "application/json"}
                if token:
                    headers["Authorization"] = "Bearer " + token
                req = urllib.request.Request(
                    base + endpoint,
                    data=None if body is None else json.dumps(body).encode(),
                    headers=headers,
                )
                try:
                    with opener.open(req, timeout=25) as response:
                        marker = response.headers.get("X-Miyabi-JavLibrary-Identity")
                        return response.status, json.load(response), marker
                except urllib.error.HTTPError as error:
                    marker = error.headers.get("X-Miyabi-JavLibrary-Identity")
                    return error.code, json.load(error), marker

            with (directory / "engine.log").open("w+") as log:
                process = subprocess.Popen(
                    [str(binary), "--config", str(path), "web"],
                    cwd=directory, env=env, stdout=log, stderr=log,
                )
                try:
                    deadline = time.monotonic() + 45
                    while time.monotonic() < deadline:
                        if process.poll() is not None:
                            raise RuntimeError(
                                f"engine exited during startup: {process.returncode}"
                            )
                        try:
                            status, _, _ = request("/auth/status")
                            if status == 200:
                                break
                        except (urllib.error.URLError, TimeoutError):
                            time.sleep(0.2)
                    else:
                        raise RuntimeError("engine startup timed out")
                    credentials = {"username": "smoke", "password": secrets.token_urlsafe(24)}
                    status, _, _ = request("/auth/setup", credentials)
                    if status != 200:
                        raise RuntimeError(f"setup failed: HTTP {status}")
                    status, _, _ = request("/auth/login", credentials)
                    if status != 200:
                        raise RuntimeError(f"login failed: HTTP {status}")
                    status, issued, _ = request("/tokens", {"name": "temporary-smoke"})
                    if status != 201:
                        raise RuntimeError(f"token creation failed: HTTP {status}")
                    token = issued["token"]
                    # The scrape probe must authenticate using only the token.
                    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
                    codes = [live_code] if live_code else list(CASES)
                    failures = 0
                    accepted_ratings = 0
                    movies_returned = 0
                    for code in codes:
                        status, result, marker = request(
                            "/scrape",
                            {"id": code, "force": True, "selected_scrapers": ["javlibrary"]},
                            token,
                        )
                        movie = result.get("movie") or {}
                        identity = movie.get("code")
                        score = movie.get("rating_score")
                        label, expected = ("live_site", True) if live_code else CASES[code]
                        accepted = (
                            status == 200
                            and identity == code
                            and movie.get("id") == code
                            and movie.get("source_name") == "javlibrary"
                            and result.get("sources_used") == 1
                            and result.get("cached") is False
                            and isinstance(score, (float, int))
                            and 0 < score <= 10
                        )
                        if require_identity:
                            accepted = accepted and marker == "page-v1"
                        passed = accepted == expected
                        if expected and not live_code:
                            passed = passed and score == 8.4
                        observation = {
                            "case": code, "label": label, "status": status,
                            "code": identity, "rating_score": score,
                            "source": movie.get("source_name"),
                            "sources_used": result.get("sources_used"),
                            "cached": result.get("cached"),
                            "identity_header": marker, "accepted": accepted,
                            "passed": passed,
                        }
                        if expected and status == 200 and not score:
                            observation["limitation"] = "rating_field_not_extracted"
                        print(json.dumps(observation), flush=True)
                        failures += not passed
                        accepted_ratings += accepted
                        movies_returned += bool(movie)
                    no_images = Fixtures.image_requests == 0
                    print(json.dumps({
                        "cases": len(codes), "failed_cases": failures,
                        "movies_returned": movies_returned,
                        "accepted_ratings": accepted_ratings,
                        "site_requests": Fixtures.site_requests,
                        "html_detail_requests": Fixtures.html_requests,
                        "image_requests": Fixtures.image_requests,
                        "rating_only_passed": no_images if require_identity else None,
                    }), flush=True)
                    if require_identity and not no_images:
                        failures += 1
                    return failures
                finally:
                    process.terminate()
                    try:
                        process.wait(timeout=5)
                    except subprocess.TimeoutExpired:
                        process.kill()
                        process.wait()
    finally:
        fixture.shutdown()
        fixture.server_close()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("binary", type=Path)
    parser.add_argument(
        "--live", metavar="CODE",
        help="explicit real-site test; reads metadata into a temporary engine database",
    )
    parser.add_argument(
        "--require-identity-header", action="store_true",
        help="enable and verify the pinned Miyabi identity/rating-only patch",
    )
    arguments = parser.parse_args()
    binary = arguments.binary.resolve()
    digest = hashlib.sha256(binary.read_bytes()).hexdigest()
    print(json.dumps({"sha256": digest, "official_v1_6_5": digest == OFFICIAL_SHA256}), flush=True)
    return min(run(binary, arguments.live, arguments.require_identity_header), 1)


if __name__ == "__main__":
    raise SystemExit(main())
