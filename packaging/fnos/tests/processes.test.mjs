import { test } from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import { syncBuiltinESMExports } from "node:module";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { engineRevision } from "../app/service/configuration.mjs";
import { status, terminate } from "../app/service/processes.mjs";

// Keep the production identity reader in these checks. The lifecycle fixture's
// one-argument alive() used to hide Array.map passing its index as a reader.
function procfs(t) {
  const records = [51001, 51002, 51003].map((pid) => ({
    pid,
    start: String(pid * 10),
    boot: "fixture-boot",
  }));
  const running = new Set(records.map((record) => record.pid));
  const paths = { run: join(tmpdir(), "miyabi-process-checks") };
  const saved = { supervisor: records[0], children: records.slice(1), phase: "running" };
  t.mock.method(fs.promises, "readFile", async (file) => {
    if (file === join(paths.run, "processes.json")) return JSON.stringify(saved);
    if (file === "/proc/sys/kernel/random/boot_id") return "fixture-boot\n";
    const record = records.find((item) => file === `/proc/${item.pid}/stat`);
    if (!record || !running.has(record.pid))
      throw Object.assign(new Error("Process has exited"), { code: "ENOENT" });
    return `${record.pid} (worker name) S ${Array(18).fill("0").join(" ")} ${record.start} 0`;
  });
  syncBuiltinESMExports();
  t.after(() => {
    t.mock.restoreAll();
    syncBuiltinESMExports();
  });
  return { records, running, paths, saved };
}

test("status checks all production process identities before reporting healthy", async (t) => {
  const { paths, running, records, saved } = procfs(t);
  const config = { port: 18080, bridgePort: 3840, bridgeKey: "fixture-key" };
  const requests = t.mock.method(globalThis, "fetch", async (url) =>
    Response.json(
      url.endsWith("/api/auth/config")
        ? { enabled: true }
        : {
            protocol: 1,
            revision: engineRevision,
            configuration_version: 1,
            configuration_id: "fixture",
          },
    ),
  );
  assert.equal(await status(paths, config), true);
  assert.equal(requests.mock.callCount(), 2);
  saved.children[0] = { ...records[1], start: "reused-pid" };
  assert.equal(await status(paths, config), false);
  saved.children[0] = records[1];
  running.delete(records[2].pid);
  assert.equal(await status(paths, config), false);
  assert.equal(requests.mock.callCount(), 2);
});

for (const grace of [1000, 0])
  test(`termination checks exited processes with a ${grace} ms grace period`, async (t) => {
    const { records, running } = procfs(t);
    const signals = [];
    t.mock.method(process, "kill", (pid, signal) => {
      signals.push([pid, signal]);
      running.delete(pid);
      return true;
    });
    await terminate(records, grace);
    assert.deepEqual(
      signals,
      records.map((record) => [record.pid, "SIGTERM"]),
    );
    assert.equal(running.size, 0);
  });
