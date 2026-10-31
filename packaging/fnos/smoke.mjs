// Exercise the actual packaged executables without touching a NAS or user data.
import assert from "node:assert/strict";
import { spawn, execFileSync } from "node:child_process";
import { randomBytes } from "node:crypto";
import { once } from "node:events";
import { mkdtemp, readFile, readdir, rm } from "node:fs/promises";
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

  // Check the delivered frontend and backend together, rather than only health.
  const assets = new URL("../../web/dist/assets/", import.meta.url);
  const settingsAsset = (await readdir(assets)).find((name) => /^settings-.*\.js$/.test(name));
  assert.ok(settingsAsset, "Build the settings frontend before packaging");
  const expectedAsset = await readFile(new URL(settingsAsset, assets));
  assert.match(expectedAsset.toString(), /字幕下载/);
  const deliveredAsset = await fetch(`${webBase}/assets/${settingsAsset}`);
  assert.equal(deliveredAsset.status, 200);
  assert.deepEqual(Buffer.from(await deliveredAsset.arrayBuffer()), expectedAsset);

  const login = await fetch(webBase + "/api/auth/login", {
    method: "POST",
    headers: { "Content-Type": "application/json", Origin: webBase },
    body: JSON.stringify({ password: key }),
  });
  assert.equal(login.status, 200);
  const cookie = login.headers.getSetCookie().map((value) => value.split(";")[0]).join("; ");
  assert.ok(cookie);
  const request = (path, method = "GET", value) => fetch(webBase + path, {
    method,
    headers: { "Content-Type": "application/json", Cookie: cookie, Origin: webBase },
    ...(value === undefined ? {} : { body: JSON.stringify(value) }),
  });
  const settingsPath = "/api/settings/subtitles";
  const defaults = await request(settingsPath);
  assert.equal(defaults.status, 200);
  assert.equal(defaults.headers.get("Cache-Control"), "no-store");
  assert.deepEqual(await defaults.json(), {
    enabled: true,
    sources: { xunlei: true, subtitlecat: true, avsubtitles: true },
    language: "zh-CN",
  });
  const saved = {
    enabled: false,
    sources: { xunlei: false, subtitlecat: false, avsubtitles: true },
    language: "zh-TW",
  };
  const update = await request(settingsPath, "PUT", saved);
  assert.equal(update.status, 200);
  assert.deepEqual(await update.json(), saved);
  assert.equal((await request(settingsPath, "PUT", null)).status, 400);
  assert.equal((await request(settingsPath + "/test", "POST", { code: "hello" })).status, 400);
  assert.equal((await request(settingsPath + "/test", "POST", {
    code: "SSIS-589",
    config: { ...saved, sources: { xunlei: false, subtitlecat: false, avsubtitles: false } },
  })).status, 400);
  assert.deepEqual(await (await request(settingsPath)).json(), saved);
  console.log(
    "Packaged Miyabi, current settings UI, subtitle configuration/test API, bridge auth and native dependencies passed.",
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
