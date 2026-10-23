"""Artifact-format checks only: no compilation, staging, fnpack or downloads."""
import importlib.util
from pathlib import Path
import struct
import tempfile
import unittest
import zlib

spec = importlib.util.spec_from_file_location("fpk_prepare", Path(__file__).resolve().parents[1] / "prepare.py")
prepare = importlib.util.module_from_spec(spec)
spec.loader.exec_module(prepare)


class ArtifactChecks(unittest.TestCase):
    def test_rejects_wrong_architecture_or_non_elf(self):
        with tempfile.TemporaryDirectory() as folder:
            file = Path(folder) / "binary"
            for body in [b"", b"#!/bin/sh", b"\x7fELF\x01\x01" + bytes(14)]:
                file.write_bytes(body)
                with self.assertRaises(ValueError):
                    prepare.elf_x64(file)
            header = bytearray(b"\x7fELF\x02\x01" + bytes(14))
            struct.pack_into("<H", header, 18, 183)
            file.write_bytes(header)
            with self.assertRaises(ValueError):
                prepare.elf_x64(file)
            struct.pack_into("<H", header, 18, 62)
            file.write_bytes(header)
            prepare.elf_x64(file)

    def test_icons_are_complete_pngs_at_required_sizes(self):
        for size in [64, 256]:
            png = prepare.icon(size)
            self.assertEqual(png[:8], b"\x89PNG\r\n\x1a\n")
            self.assertEqual(struct.unpack_from(">II", png, 16), (size, size))
            pos, raw = 8, b""
            while pos < len(png):
                length = struct.unpack_from(">I", png, pos)[0]
                kind, payload = png[pos+4:pos+8], png[pos+8:pos+8+length]
                self.assertEqual(struct.unpack_from(">I", png, pos+8+length)[0], zlib.crc32(kind+payload) & 0xffffffff)
                if kind == b"IDAT":
                    raw += payload
                pos += 12 + length
            self.assertEqual(len(zlib.decompress(raw)), (size * 3 + 1) * size)

    def test_package_metadata_matches_native_entry_and_pinned_engine(self):
        import json
        root = Path(__file__).resolve().parents[3]
        package = root / "packaging/fnos"
        ui = json.loads((package / "app/ui/config").read_text())[".url"]
        manifest = (package / "manifest.in").read_text()
        self.assertIn("platform=x86\n", manifest)
        self.assertIn("desktop_applaunchname=miyabi-native.main\n", manifest)
        self.assertIn("miyabi-native.main", ui)
        self.assertEqual(ui["miyabi-native.main"]["port"], "${wizard_port}")
        self.assertNotIn("docker-project", (package / "config/resource").read_text())
        self.assertEqual(json.loads((package / "config/privilege").read_text())["defaults"]["run-as"], "package")
        import re
        sources = [package / "app/service/configuration.mjs", root / "extensions/mdcz-bridge/contract.mjs", root / "internal/mdcz/service.go", root / "extensions/mdcz-bridge/Dockerfile"]
        revisions = [set(re.findall(r"\b[a-f0-9]{40}\b", file.read_text())) for file in sources]
        self.assertEqual(len(set.intersection(*revisions)), 1)
        for file in (package / "wizard").iterdir():
            self.assertIsInstance(json.loads(file.read_text()), list)


if __name__ == "__main__":
    unittest.main()
