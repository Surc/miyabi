// Exercise the actual packaged executables without touching a NAS or user data.
import assert from "node:assert/strict";
import { spawn, execFileSync } from "node:child_process";
import { randomBytes } from "node:crypto";
import { once } from "node:events";
import { mkdtemp, rm } from "node:fs/promises";
import { createServer } from "node:net";
import { tmpdir } from "node:os";
import { resolve, join } from "node:path";
import { setTimeout as delay } from "node:timers/promises";
import { revision } from "../../extensions/mdcz-bridge/contract.mjs";

const app = resolve(process.argv[2]);
const directory = await mkdtemp(join(tmpdir(), "miyabi-artifact-smoke-"));
const children = [];
const key = randomBytes(32).toString("hex");
async function port() {
  const server = createServer().listen(0, "127.0.0.1");
  await once(server, "listening");
  const number = server.address().port;
  await new Promise((resolve) => server.close(resolve));
  return number;
}
function start(file, args, env) {
  const child = spawn(file, args, {
    env: { ...process.env, ...env },
    stdio: ["ignore", "pipe", "pipe"],
  });
  let output = "";
  child.stdout.on("data", (data) => {
    output = (output + data).slice(-4000);
  });
  child.stderr.on("data", (data) => {
    output = (output + data).slice(-4000);
  });
  child.on("error", (error) => {
    output += error.message;
  });
  children.push(child);
  return { child, output: () => output };
}
async function ready(process, url, headers = {}) {
  for (let attempt = 0; attempt < 120; attempt++) {
    if (process.child.exitCode !== null || process.child.signalCode !== null)
      throw new Error("Packaged service exited: " + process.output());
    try {
      const response = await fetch(url, { headers, signal: AbortSignal.timeout(1000) });
      if (response.ok) return response;
    } catch {
      /* Service is still starting. */
    }
    await delay(250);
  }
  throw new Error("Packaged service did not become ready: " + process.output());
}
try {
  const node = join(app, "runtime/bin/node");
  execFileSync(node, [join(app, "service/native-probe.mjs")], {
    env: { ...process.env, TRIM_APPDEST: app },
  });
  const bridgePort = await port();
  const bridge = start(node, [join(app, "bridge/bridge.js")], {
    HOST: "127.0.0.1",
    PORT: String(bridgePort),
    MIYABI_MDCZ_KEY: key,
    MIYABI_MDCZ_DATA: join(directory, "mdcz"),
    MIYABI_MDCZ_CONFIG: join(app, "defaults/mdcz.json"),
  });
  const base = `http://127.0.0.1:${bridgePort}`;
  const headers = { Authorization: `Bearer ${key}` };
  const health = await (await ready(bridge, base + "/v1/health", headers)).json();
  assert.equal(health.revision, revision);
  assert.equal(health.protocol, 1);
  assert.equal((await fetch(base + "/v1/health")).status, 401);
  assert.equal((await fetch(base + "/v1/configuration", { headers })).status, 200);

  const webPort = await port();
  const webBase = `http://127.0.0.1:${webPort}`;
  const miyabi = start(join(app, "bin/miyabi"), [], {
    MIYABI_LISTEN: `127.0.0.1:${webPort}`,
    MIYABI_DATA_DIR: join(directory, "miyabi"),
    MIYABI_EMBY_DIR: join(directory, "emby"),
    MIYABI_PUBLIC_URL: webBase,
    MIYABI_ACCESS_PASSWORD: key,
    MIYABI_JWT_SECRET: key,
    MIYABI_EMBY_ENABLED: "false",
  });
  const auth = await ready(miyabi, webBase + "/api/auth/config");
  assert.equal((await auth.json()).enabled, true);
  assert.equal((await fetch(webBase + "/api/library/movies")).status, 401);
  assert.match(await (await fetch(webBase + "/")).text(), /<html/);
  console.log(
    "Packaged Miyabi, embedded UI, bridge configuration/auth and native dependencies passed.",
  );
} finally {
  await Promise.all(
    children.map(async (child) => {
      if (child.exitCode !== null || child.signalCode !== null) return;
      const exited = once(child, "exit");
      child.kill("SIGTERM");
      const timer = setTimeout(() => child.kill("SIGKILL"), 5000);
      await exited;
      clearTimeout(timer);
    }),
  );
  await rm(directory, { recursive: true, force: true });
}
