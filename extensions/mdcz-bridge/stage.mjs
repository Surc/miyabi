// Preserve exact upstream native dependency resolutions in the runtime image.
import { readFile, writeFile, mkdir, cp } from "node:fs/promises";
import { resolve } from "node:path";
import { parse, stringify } from "yaml";
import { revision } from "./contract.mjs";
import { execFileSync } from "node:child_process";

const root = resolve(import.meta.dirname, "../..");
if (execFileSync("git", ["rev-parse", "HEAD"], { cwd: root, encoding: "utf8" }).trim() !== revision)
  throw new Error("MDCz checkout does not match the pinned bridge revision");
const staging = resolve(root, "bridge-staging");
const source = JSON.parse(await readFile(resolve(root, "apps/server/package.json"), "utf8"));
const lock = parse(await readFile(resolve(root, "pnpm-lock.yaml"), "utf8"));
const dependencies = Object.fromEntries(
  Object.entries(source.dependencies).filter(([, version]) => !version.startsWith("workspace:")),
);
const locked = Object.fromEntries(
  Object.keys(dependencies).map((name) => {
    const item = lock.importers["apps/server"].dependencies[name];
    if (!item || item.specifier !== dependencies[name])
      throw new Error(`Unlocked dependency: ${name}`);
    return [name, item];
  }),
);
lock.importers = { ".": { dependencies: locked } };
delete lock.catalogs;
await mkdir(staging, { recursive: true });
await cp(resolve(import.meta.dirname, "dist"), staging, { recursive: true });
await cp(resolve(root, "LICENSE"), resolve(staging, "MDCz-LICENSE"));
await writeFile(
  resolve(staging, "NOTICE"),
  `Miyabi MDCz bridge; MDCz revision ${revision}. Source: https://github.com/ShotHeadman/mdcz\nBridge source: https://github.com/Surc/miyabi/tree/feat/mdcz-extension/extensions/mdcz-bridge\n`,
);
await writeFile(
  resolve(staging, "package.json"),
  JSON.stringify(
    {
      name: "miyabi-mdcz-bridge",
      private: true,
      type: "module",
      dependencies,
      pnpm: {
        onlyBuiltDependencies: ["better-sqlite3", "impit"],
        ignoredBuiltDependencies: ["sharp"],
      },
    },
    null,
    2,
  ),
);
await writeFile(resolve(staging, "pnpm-lock.yaml"), stringify(lock));
