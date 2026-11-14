// Exercise the actual packaged executables without touching a NAS or user data.
import assert from "node:assert/strict";
import { spawn, execFileSync } from "node:child_process";
import { createHash, randomBytes } from "node:crypto";
import { once } from "node:events";
import { mkdir, mkdtemp, readFile, readdir, rm, writeFile } from "node:fs/promises";
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
function start(file, args, env, cwd) {
  const child = spawn(file, args, {
    env: { ...process.env, ...env },
    cwd,
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
  let mdczPort;
  do {
    mdczPort = await port();
  } while (mdczPort === bridgePort);
  const legacyDatabase = join(directory, "legacy-miyabi.db");
  execFileSync(node, [
    "--input-type=module",
    "--eval",
    `import { DatabaseSync } from "node:sqlite";
     const db = new DatabaseSync(process.argv[1]);
     db.exec("CREATE TABLE settings (key TEXT PRIMARY KEY, value BLOB)");
     db.prepare("INSERT INTO settings VALUES (?, ?)").run("extensions.mdcz.v1", Buffer.from('{"translate":true}'));
     db.close();`,
    legacyDatabase,
  ]);
  const bridge = start(node, [join(app, "bridge/fnos.js")], {
    PORT: String(bridgePort),
    MIYABI_MDCZ_KEY: key,
    MIYABI_MDCZ_DATA: join(directory, "mdcz"),
    MIYABI_MDCZ_CONFIG: join(app, "defaults/mdcz.json"),
    MIYABI_MDCZ_WEB_PORT: String(mdczPort),
    MIYABI_MDCZ_LEGACY_DATABASE: legacyDatabase,
    MDCZ_HOME: join(directory, "mdcz/webui"),
    MDCZ_ADMIN_PASSWORD: key,
  }, join(app, "bridge"));
  const base = `http://127.0.0.1:${bridgePort}`;
  const headers = { Authorization: `Bearer ${key}` };
  const health = await (await ready(bridge, base + "/v1/health", headers)).json();
  assert.equal(health.revision, revision);
  assert.equal(health.protocol, 1);
  assert.equal((await fetch(base + "/v1/health")).status, 401);
  assert.equal((await fetch(base + "/v1/configuration", { headers })).status, 200);

  const mdczBase = `http://127.0.0.1:${mdczPort}`;
  const officialSettings = await fetch(mdczBase + "/settings");
  assert.equal(officialSettings.status, 200);
  const officialAsset = (await officialSettings.text()).match(/<script type="module"[^>]+src="(\/assets\/[^"]+)"/)?.[1];
  assert.ok(officialAsset);
  const script = await fetch(mdczBase + officialAsset);
  assert.equal(script.status, 200);
  assert.match(script.headers.get("Content-Type"), /javascript/);
  const mdczAPI = (procedure, input, token) => fetch(`${mdczBase}/trpc/${procedure}`, {
    method: input === undefined ? "GET" : "POST",
    headers: {
      "Content-Type": "application/json",
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
    },
    ...(input === undefined ? {} : { body: JSON.stringify(input) }),
  });
  assert.equal((await mdczAPI("config.read")).status, 401);
  const mdczLogin = await mdczAPI("auth.login", { password: key });
  assert.equal(mdczLogin.status, 200);
  const mdczToken = (await mdczLogin.json()).result.data.token;
  assert.ok(mdczToken);
  const initialProfile = await (await mdczAPI("config.read", undefined, mdczToken)).json();
  assert.equal(initialProfile.result.data.translate.enableTranslation, true);

  const webPort = await port();
  const webBase = `http://127.0.0.1:${webPort}`;
  const managedConnection = join(directory, "mdcz-connection.json");
  await writeFile(managedConnection, JSON.stringify({
    server_url: base,
    api_key: key,
    generation: createHash("sha256").update(base).digest("hex"),
    web_port: mdczPort,
  }), { mode: 0o600 });
  await mkdir(join(directory, "emby"));
  const miyabi = start(join(app, "bin/miyabi"), [], {
    MIYABI_LISTEN: `127.0.0.1:${webPort}`,
    MIYABI_DATA_DIR: join(directory, "miyabi"),
    MIYABI_EMBY_DIR: join(directory, "emby"),
    MIYABI_PUBLIC_URL: webBase,
    MIYABI_ACCESS_PASSWORD: key,
    MIYABI_JWT_SECRET: key,
    MIYABI_EMBY_ENABLED: "false",
    MIYABI_MDCZ_MANAGED: managedConnection,
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
  const mdczPath = "/api/extensions/mdcz";
  const mdczSettings = await (await request(mdczPath + "/settings")).json();
  assert.equal(mdczSettings.web_port, mdczPort);
  const newTimeout = 47;
  const edit = await mdczAPI("config.update", { network: { timeout: newTimeout } }, mdczToken);
  assert.equal(edit.status, 200);
  const fromMiyabi = await (await request(mdczPath + "/configuration")).json();
  assert.equal(fromMiyabi.values.timeout, newTimeout);
  assert.notEqual(fromMiyabi.configuration_id, health.configuration_id);
  const stale = await fetch(base + "/v1/metadata", {
    method: "POST",
    headers: {
      ...headers,
      "Content-Type": "application/json",
      "X-Miyabi-MDCz-Configuration-ID": health.configuration_id,
      "X-Miyabi-MDCz-Engine-Revision": revision,
    },
    body: JSON.stringify({ code: "TEST-001", translate: false }),
  });
  assert.equal(stale.status, 409);
  const profileImport = await mdczAPI("config.profiles.import", {
    name: "imported",
    fileName: "imported.toml",
    content: "[network]\ntimeout = 61\n",
  }, mdczToken);
  assert.equal(profileImport.status, 200);
  assert.equal((await mdczAPI("config.profiles.switch", { name: "imported" }, mdczToken)).status, 200);
  const imported = await (await request(mdczPath + "/configuration")).json();
  assert.equal(imported.values.timeout, 61);
  assert.notEqual(imported.configuration_id, fromMiyabi.configuration_id);
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
    "Packaged Miyabi, MDCz WebUI login and settings handoff, bridge auth, subtitle API and native dependencies passed.",
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
