import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtemp, mkdir, readFile, writeFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { generate } from "./generate-compatibility.mjs";
import { health } from "./contract.mjs";

const root = resolve(import.meta.dirname, "../..");
test("compatibility consumers are generated from one exact source lock", async () => {
  const lock = await generate(root, true);
  assert.equal(health.revision, lock.engine_revision);
  assert.equal(health.bridge_version, lock.bridge_version);
  assert.deepEqual(health.capabilities, lock.capabilities);
});

test("source lock changes must regenerate every consumer; check mode never rewrites", async (t) => {
  const temp = await mkdtemp(join(tmpdir(), "miyabi-compatibility-"));
  t.after(() => rm(temp, { recursive: true, force: true }));
  for (const directory of ["extensions/mdcz-bridge", "packaging/fnos/app/service", "internal/mdcz"])
    await mkdir(join(temp, directory), { recursive: true });
  const path = join(temp, "extensions/mdcz-bridge/source-lock.json");
  const lock = JSON.parse(await readFile(join(root, "extensions/mdcz-bridge/source-lock.json"), "utf8"));
  await writeFile(path, JSON.stringify(lock));
  await generate(temp);
  await generate(temp, true);
  lock.bridge_version = "1.2.0";
  await writeFile(path, JSON.stringify(lock));
  await assert.rejects(generate(temp, true), /Stale compatibility/);
  assert.match(await readFile(join(temp, "internal/mdcz/version_generated.go"), "utf8"), /1\.1\.0/);
  await generate(temp);
  await generate(temp, true);
  lock.engine_revision = "main";
  await writeFile(path, JSON.stringify(lock));
  await assert.rejects(generate(temp), /Invalid MDCz source lock/);
});
