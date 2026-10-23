#!/usr/bin/env python3
"""Stage a native FPK from verified build outputs; never build/download them.

Run only after build authorization. fnpack is a separate, explicit final step.
The source checkout must be clean and match Miyabi's embedded VCS revision.
"""
import argparse
import hashlib
import json
from pathlib import Path
import re
import shutil
import struct
import subprocess
import zlib


def elf_x64(path):
    with open(path, "rb") as source:
        header = source.read(20)
    if len(header) != 20 or header[:6] != b"\x7fELF\x02\x01" or struct.unpack_from("<H", header, 18)[0] != 62:
        raise ValueError(f"Expected a Linux x86-64 ELF executable: {path.name}")


def version(value):
    if not re.fullmatch(r"\d+\.\d+\.\d+(?:[-.][A-Za-z0-9.-]+)?", value):
        raise argparse.ArgumentTypeError("Use a numeric three-part version")
    return value


def icon(size):
    """Small original geometric M icon; no image/font conversion dependencies."""
    pixels = bytearray()
    for y in range(size):
        pixels.append(0)
        for x in range(size):
            px, py = (x + .5) / size, (y + .5) / size
            mark = .25 < py < .75 and (
                .22 < px < .32 or .68 < px < .78 or
                (.30 < px < .50 and abs(py - (px + .03)) < .065) or
                (.50 <= px < .70 and abs(py - (1.03 - px)) < .065)
            )
            pixels.extend((250, 245, 252) if mark else (98, 64, 139))
    def chunk(kind, data):
        return struct.pack(">I", len(data)) + kind + data + struct.pack(">I", zlib.crc32(kind + data) & 0xffffffff)
    return b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack(">2I5B", size, size, 8, 2, 0, 0, 0)) + chunk(b"IDAT", zlib.compress(bytes(pixels))) + chunk(b"IEND", b"")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ["miyabi", "bridge", "node", "fonts", "certs", "output"]:
        parser.add_argument("--" + name, type=Path, required=True)
    parser.add_argument("--version", type=version, required=True)
    parser.add_argument("--os-min-version", type=version, required=True, help="Minimum fnOS version selected for actual NAS validation")
    parser.add_argument("--go", default="go", help="Go tool for reading build metadata only")
    args = parser.parse_args()
    root = Path(__file__).resolve().parents[2]
    template = root / "packaging/fnos"
    revision = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=root, text=True).strip()
    if subprocess.check_output(["git", "status", "--porcelain"], cwd=root, text=True).strip():
        raise ValueError("Prepare from a clean source checkout")
    engine = re.search(r'engineRevision\s*=\s*"([a-f0-9]{40})"', (template / "app/service/configuration.mjs").read_text()).group(1)
    if engine not in (args.bridge / "NOTICE").read_text():
        raise ValueError("Bridge artifact does not match the pinned engine")
    elf_x64(args.miyabi)
    elf_x64(args.node / "bin/node")
    build = subprocess.check_output([args.go, "version", "-m", str(args.miyabi)], text=True)
    for required in ["GOARCH=amd64", "GOAMD64=v1", "GOOS=linux", "CGO_ENABLED=0", "vcs.revision=" + revision, "vcs.modified=false"]:
        if required not in build:
            raise ValueError("Miyabi artifact provenance mismatch: " + required)
    node_version = subprocess.check_output([str(args.node / "bin/node"), "--version"], text=True).strip()
    if not node_version.startswith("v24."):
        raise ValueError("The bridge requires the pinned Node 24 runtime")
    for required in [args.bridge / "bridge.js", args.bridge / "node_modules", args.bridge / "MDCz-LICENSE", args.node / "LICENSE", args.fonts / "NotoSansCJK-Regular.ttc", args.fonts / "LICENSE", args.certs]:
        if not required.exists():
            raise ValueError("Missing packaged dependency: " + str(required))
    if args.output.exists():
        raise ValueError("Output directory must not already exist")
    args.output.mkdir(parents=True)
    for folder in ["app", "cmd", "config", "wizard"]:
        shutil.copytree(template / folder, args.output / folder)
    app = args.output / "app"
    for folder in ["bin", "runtime/bin", "defaults", "fonts", "certs", "licenses", "ui/images"]:
        (app / folder).mkdir(parents=True, exist_ok=True)
    # Keep pnpm's relative symlinks inside the copied tree.
    shutil.copytree(args.bridge, app / "bridge", symlinks=True)
    shutil.copy2(args.miyabi, app / "bin/miyabi")
    shutil.copy2(args.node / "bin/node", app / "runtime/bin/node")
    for source, target in [
        (root / "extensions/mdcz-bridge/mdcz.example.json", app / "defaults/mdcz.json"),
        (args.fonts / "NotoSansCJK-Regular.ttc", app / "fonts/NotoSansCJK-Regular.ttc"),
        (args.fonts / "LICENSE", app / "licenses/Noto-LICENSE"),
        (args.node / "LICENSE", app / "licenses/Node-LICENSE"),
        (root / "LICENSE", app / "licenses/Miyabi-LICENSE"),
        (args.certs, app / "certs/ca-certificates.crt"),
    ]:
        shutil.copy2(source, target)
    for size, name in [(64, "ICON.PNG"), (256, "ICON_256.PNG")]:
        data = icon(size)
        (args.output / name).write_bytes(data)
        (app / "ui/images" / f"icon_{size}.png").write_bytes(data)
    manifest = (template / "manifest.in").read_text().replace("@VERSION@", args.version).replace("@MIN_OS@", args.os_min_version)
    (args.output / "manifest").write_text(manifest)
    # A prepared directory is not a tested/installable FPK. Keep build identity
    # and checksums with it so the release operator can trace each dependency.
    checksums = {}
    destination = args.output.resolve()
    for file in sorted(args.output.rglob("*")):
        if file.is_symlink() and not file.resolve().is_relative_to(destination):
            raise ValueError("Dependency symlink escapes the package: " + str(file))
        if file.is_file():
            with file.open("rb") as source:
                checksums[str(file.relative_to(args.output))] = hashlib.file_digest(source, "sha256").hexdigest()
    (app / "bundle.json").write_text(json.dumps({"version": args.version, "source_revision": revision, "mdcz_revision": engine, "node": node_version, "platform": "linux/amd64", "goamd64": "v1", "sha256": checksums}, indent=2) + "\n")
    for file in [* (args.output / "cmd").iterdir(), app / "service/control", app / "runtime/bin/node", app / "bin/miyabi"]:
        file.chmod(0o755)
    print(f"Staged {args.output}; no FPK built. Validate on fnOS before release.")


if __name__ == "__main__":
    main()
