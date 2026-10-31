"""Rebuild the bundled JavDB rankings using only Python's standard library.

The pinned Gist defines the overall ranking. Its linked SQL dataset supplies
category/year rankings and card metadata. No movie details or magnets are fetched.
"""

import argparse
from contextlib import closing
import csv
import io
import json
from pathlib import Path
import sqlite3
from urllib.request import urlopen
import zipfile


GIST = "https://gist.github.com/jinjier/7a405fad753f996d85ed43073e3bf009/f690949b35b08309022b934accf6d041d075e149"
CSV_URL = "https://gist.githubusercontent.com/jinjier/7a405fad753f996d85ed43073e3bf009/raw/f690949b35b08309022b934accf6d041d075e149/top250-code.csv"
DATABASE_URL = "https://jinjier.art/20260112.gif"
BOARD_NAMES = {6: "总榜", 7: "有码", 8: "无码", 9: "欧美", 10: "FC2"}
BOARD_IDS = {6: "all", 7: "censored", 8: "uncensored", 9: "western", 10: "fc2"}


def read_source(path, url):
    if path:
        return Path(path).read_bytes()
    with urlopen(url, timeout=30) as response:
        return response.read()


def build(database, overall):
    movies = {}
    boards = []
    for kind in [6, 7, 8, 9, 10, *range(2025, 2007, -1)]:
        entries = []
        rows = database.execute(
            "SELECT number, name, date, icon_url FROM ranks WHERE kind = ? ORDER BY number, id",
            (kind,),
        ).fetchall()
        assert [row[0] for row in rows] == list(range(1, 251)), kind
        for _, name, date, image in rows:
            code, *title = name.strip().split(maxsplit=1)
            code = code.upper()
            entries.append(code)
            movies.setdefault(code, {
                "code": code,
                "title": title[0] if title else code,
                "release_date": date,
                "cover": image or "",
            })
        boards.append({
            "id": BOARD_IDS.get(kind, str(kind)),
            "name": f"JavDB {BOARD_NAMES[kind]} TOP250" if kind < 2008 else f"JavDB {kind}年度 TOP250",
            "codes": entries,
        })

    rows = list(csv.reader(io.StringIO(overall.decode("utf-8-sig"))))
    assert [int(row[0]) for row in rows] == list(range(1, 251))
    boards[0]["codes"] = []
    for _, code, date in rows:
        code = code.upper()
        boards[0]["codes"].append(code)
        movies.setdefault(code, {"code": code, "title": code, "release_date": date, "cover": ""})
    return {
        "updated_at": "2026-01-12",
        "source_url": GIST,
        "annual_source_url": "https://jinjier.art/sql",
        "database_url": DATABASE_URL,
        "boards": boards,
        "movies": list(movies.values()),
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--archive", help="Use a previously downloaded SQL dataset ZIP")
    parser.add_argument("--csv", help="Use a previously downloaded Gist CSV")
    args = parser.parse_args()
    archive = read_source(args.archive, DATABASE_URL)
    overall = read_source(args.csv, CSV_URL)
    with zipfile.ZipFile(io.BytesIO(archive)) as zipped, closing(sqlite3.connect(":memory:")) as database:
        database.deserialize(zipped.read("jinjier.sqlite3"))
        snapshot = build(database, overall)

    target = Path(__file__).resolve().parents[1] / "internal/top/data.json"
    # Keep each movie/board on its own line so source updates remain reviewable.
    lines = ["{"]
    for key, value in snapshot.items():
        prefix = f"  {json.dumps(key)}: "
        if isinstance(value, list):
            lines.append(prefix + "[")
            lines.extend("    " + json.dumps(item, ensure_ascii=False) + ("," if index < len(value) - 1 else "")
                         for index, item in enumerate(value))
            lines.append("  ]" + ("," if key != "movies" else ""))
        else:
            lines.append(prefix + json.dumps(value, ensure_ascii=False) + ",")
    lines.append("}")
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_text("\n".join(lines) + "\n", encoding="utf-8")
    print(f"Wrote {len(snapshot['boards'])} boards and {len(snapshot['movies'])} movies to {target}")


if __name__ == "__main__":
    main()
