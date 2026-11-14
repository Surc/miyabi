import { test } from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import { syncBuiltinESMExports } from "node:module";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { engineRevision, protocol, configurationVersion, bridgeVersion, requiredCapabilities } from "../app/service/compatibility.mjs";
import { status, terminate, bridgeHealthy, restartDelay, restartPolicy } from "../app/service/processes.mjs";

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
  const saved = { supervisor: records[0], children: [{ ...records[1], name: "bridge" }, { ...records[2], name: "miyabi" }], phase: "running" };
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

test("status requires main and supervisor identities but not the optional bridge", async (t) => {
  const { paths, running, records, saved } = procfs(t);
  const config = { port: 18080, bridgePort: 3840, bridgeKey: "fixture-key" };
  const requests = t.mock.method(globalThis, "fetch", async (url) => {
    assert.ok(url.endsWith("/api/auth/config"));
    return Response.json({ enabled: true });
  });
  assert.equal(await status(paths, config), true);
  assert.equal(requests.mock.callCount(), 1);
  saved.children[0] = { ...records[1], name: "bridge", start: "reused-pid" };
  assert.equal(await status(paths, config), true);
  running.delete(records[2].pid);
  assert.equal(await status(paths, config), false);
  assert.equal(requests.mock.callCount(), 2);
  running.add(records[2].pid);
  saved.supervisor = { ...saved.supervisor, start: "reused-supervisor" };
  assert.equal(await status(paths, config), false);
});

test("external mode checks only the Miyabi process and its health endpoint", async (t) => {
  const { paths, saved } = procfs(t);
  saved.children = [saved.children[1]];
  const config = {
    port: 18080,
    externalMDCz: { serverURL: "http://bridge.test:3841", apiKey: "a".repeat(64) },
  };
  let enabled = true;
  const requests = t.mock.method(globalThis, "fetch", async (url) => {
    assert.equal(url, "http://127.0.0.1:18080/api/auth/config");
    return Response.json({ enabled });
  });
  assert.equal(await status(paths, config), true);
  assert.equal(requests.mock.callCount(), 1);
  enabled = false;
  assert.equal(await status(paths, config), false);
});

test("legacy unnamed records remain readable but named bridge is never main", async (t) => {
  const { paths, records, saved } = procfs(t);
  t.mock.method(globalThis, "fetch", async () => Response.json({ enabled: true }));
  saved.children = records.slice(1);
  assert.equal(await status(paths, { port: 18080 }), true);
  saved.children = [{ ...records[1], name: "bridge" }];
  assert.equal(await status(paths, { port: 18080, externalMDCz: {} }), false);
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


test("bridge health requires the exact independently versioned compatibility tuple", async (t) => {
  const config = { port: 18080, bridgePort: 3840, bridgeKey: "fixture-key" };
  const valid = { protocol, revision: engineRevision, bridge_version: bridgeVersion,
    configuration_version: configurationVersion, configuration_id: "fixture", capabilities: requiredCapabilities };
  let health = valid;
  t.mock.method(globalThis, "fetch", async (url) => {
    assert.ok(url.endsWith("/v1/health"));
    return Response.json(health);
  });
  assert.equal(await bridgeHealthy(config), true);
  for (const patch of [{ revision: "unknown" }, { bridge_version: "unknown" },
    { protocol: 999 }, { configuration_version: 999 }, { configuration_id: "" }, { capabilities: [] }, { capabilities: requiredCapabilities.join(",") }]) {
    health = { ...valid, ...patch };
    assert.equal(await bridgeHealthy(config), false);
  }
});

test("unstable bridge retry budget is finite and exponentially backed off", () => {
  assert.deepEqual(Array.from({ length: restartPolicy.attempts }, (_, i) => restartDelay(i + 1)),
    [1000, 2000, 4000, 8000, null]);
});
