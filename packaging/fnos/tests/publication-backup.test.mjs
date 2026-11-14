import { test } from "node:test";
import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { mkdir, writeFile, readFile, readdir, rm, stat, symlink } from "node:fs/promises";
import { join } from "node:path";
import { backup, childEnvironment } from "../app/service/configuration.mjs";
import { fixture } from "./fixture.mjs";

const digest = (value) => createHash("sha256").update(value).digest("hex");
async function pendingPublication(t) {
  const setup = await fixture(t);
  const { paths, config } = setup;
  const store = join(paths.data, "miyabi", "export-publications");
  const id = "a".repeat(32);
  const rootID = "b".repeat(32);
  const stage = join(store, "stages", id);
  for (const path of [join(store, "roots"), ...["old", "new", "working"].map((name) => join(stage, name))])
    await mkdir(path, { recursive: true, mode: 0o700 });
  const journal = {
    version: 2, operation_id: id, root: config.exports, root_id: rootID, stage,
    entries: [{ path: "TEST-001/poster.jpg", before: digest("old image"), after: digest("new image") }],
  };
  const catalog = join(store, `${id}.json`);
  await writeFile(catalog, JSON.stringify(journal));
  await writeFile(join(store, "roots", `${digest(config.exports)}.json`), JSON.stringify({ root: config.exports, identity: rootID }));
  await writeFile(join(stage, "old", "000000"), "old image");
  await writeFile(join(stage, "new", "000000"), "new image");
  await writeFile(join(stage, "working", "candidate.nfo"), "private candidate");
  return { ...setup, store, stage, id, catalog, journal };
}

test("production publication storage is inside the app data copied by FPK backups", async (t) => {
  const { paths, config } = await fixture(t);
  const env = childEnvironment(paths, config);
  assert.equal(env.miyabi.MIYABI_DATA_DIR, join(paths.data, "miyabi"));
  const assembly = await readFile(new URL("../../../internal/app/app.go", import.meta.url), "utf8");
  assert.match(assembly, /EnablePublicationRecovery\(filepath\.Join\(cfg\.DataDir, "export-publications"\)/);
});

test("a complete backup contains publication catalogs, root bindings, immutable blobs and working files", async (t) => {
  const { paths, config, id, journal } = await pendingPublication(t);
  await writeFile(join(paths.data, "miyabi", "miyabi.db"), "matching receipt fixture");
  const target = await backup(paths, config, "publication");
  const copied = join(target, "miyabi", "export-publications");
  assert.deepEqual(JSON.parse(await readFile(join(copied, `${id}.json`), "utf8")), journal);
  assert.equal(await readFile(join(copied, "stages", id, "old", "000000"), "utf8"), "old image");
  assert.equal(await readFile(join(copied, "stages", id, "new", "000000"), "utf8"), "new image");
  assert.equal(await readFile(join(copied, "stages", id, "working", "candidate.nfo"), "utf8"), "private candidate");
  assert.equal(JSON.parse(await readFile(join(copied, "roots", `${digest(config.exports)}.json`), "utf8")).identity, journal.root_id);
  assert.equal(await readFile(join(target, "miyabi", "miyabi.db"), "utf8"), "matching receipt fixture");
  assert.equal(JSON.parse(await readFile(join(target, "complete.json"), "utf8")).reason, "publication");
});

for (const kind of ["legacy-external", "v2-external", "missing-blob", "changed-blob", "stage-symlink"]) {
  test(`backup refuses a completion marker for ${kind} recovery evidence`, async (t) => {
    const { paths, config, catalog, journal, stage } = await pendingPublication(t);
    if (kind.endsWith("external")) {
      const external = join(config.exports, "old-private-stage");
      await mkdir(external);
      await writeFile(join(external, "preserve"), "original recovery data");
      journal.version = kind === "legacy-external" ? 1 : 2;
      journal.stage = external;
      await writeFile(catalog, JSON.stringify(journal));
    } else if (kind === "missing-blob") {
      await rm(join(stage, "old", "000000"));
    } else if (kind === "changed-blob") {
      await writeFile(join(stage, "new", "000000"), "changed after publication");
    } else {
      await rm(stage, { recursive: true });
      await symlink(config.exports, stage, "dir");
    }
    await assert.rejects(backup(paths, config, kind), /发布恢复资料未完整包含/);
    const failed = (await readdir(config.backups)).find((name) => name.startsWith(`${kind}-`));
    assert.ok(failed);
    await assert.rejects(stat(join(config.backups, failed, "complete.json")), { code: "ENOENT" });
    assert.deepEqual(JSON.parse(await readFile(catalog, "utf8")), journal);
    assert.equal((await stat(paths.config)).isFile(), true);
    if (kind.endsWith("external"))
      assert.equal(await readFile(join(journal.stage, "preserve"), "utf8"), "original recovery data");
  });
}
