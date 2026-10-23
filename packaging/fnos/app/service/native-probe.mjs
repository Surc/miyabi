// Executed with the bundled Node binary, before starting either service.
// Loading is insufficient for sharp: also exercise its native JPEG pipeline.
import { createRequire } from "node:module";
import { join } from "node:path";
const require = createRequire(join(process.env.TRIM_APPDEST, "bridge/package.json"));
if (
  process.platform !== "linux" ||
  process.arch !== "x64" ||
  process.versions.node.split(".")[0] !== "24"
)
  throw new Error("Expected linux/x64 Node 24");
const sharp = require("sharp");
await sharp({ create: { width: 2, height: 2, channels: 3, background: "#000" } })
  .jpeg()
  .toBuffer();
const { Impit } = require("impit");
new Impit();
const Database = require("better-sqlite3");
new Database(":memory:").close();
