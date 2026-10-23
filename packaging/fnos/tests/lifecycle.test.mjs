import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdir, cp, symlink, writeFile, readFile, readdir, rename, rm } from "node:fs/promises";
import { join } from "node:path";
import { createServer } from "node:net";
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { setTimeout as delay } from "node:timers/promises";
import { fixture } from "./fixture.mjs";
import { writeJSON, load } from "../app/service/configuration.mjs";
import { state } from "../app/service/processes.mjs";
const nativeProcfs =
  Number((await readFile("/proc/self/stat", "utf8")).split(" ")[0]) === process.pid;
const { identity, alive } = await import(
  nativeProcfs ? "../app/service/identity.mjs" : "./process-identity.fixture.mjs"
);
const exec = promisify(execFile);
const service = new URL("../app/service/", import.meta.url);
async function port() {
  const server = createServer();
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
  const value = server.address().port;
  await new Promise((resolve) => server.close(resolve));
  return value;
}
async function installed(t) {
  const data = await fixture(t);
  const { paths, env, config } = data;
  config.port = await port();
  do {
    config.bridgePort = await port();
  } while (config.port === config.bridgePort);
  config.publicURL = `http://127.0.0.1:${config.port}`;
  await writeJSON(paths.config, config);
  await cp(service, join(paths.app, "service"), { recursive: true });
  if (!nativeProcfs)
    await cp(
      new URL("./process-identity.fixture.mjs", import.meta.url),
      join(paths.app, "service/identity.mjs"),
    );
  for (const folder of ["runtime/bin", "bin", "bridge/node_modules", "fonts", "certs"])
    await mkdir(join(paths.app, folder), { recursive: true });
  await symlink(process.execPath, join(paths.app, "runtime/bin/node"));
  await writeFile(join(paths.app, "fonts/NotoSansCJK-Regular.ttc"), "fixture only");
  await cp("/etc/ssl/certs/ca-certificates.crt", join(paths.app, "certs/ca-certificates.crt"));
  await writeFile(join(paths.app, "bridge/package.json"), '{"type":"module"}');
  // Test doubles exercise lifecycle control, never claim native ABI validation.
  for (const [name, source] of Object.entries({
    sharp: "module.exports=()=>({jpeg(){return this},async toBuffer(){return Buffer.alloc(1)}})",
    impit: "exports.Impit=class {}",
    "better-sqlite3": "module.exports=class { close(){} }",
  })) {
    await mkdir(join(paths.app, "bridge/node_modules", name));
    await writeFile(join(paths.app, "bridge/node_modules", name, "index.js"), source);
  }
  await writeFile(
    join(paths.app, "bridge/bridge.js"),
    `
import { createServer } from 'node:http';
import { engineRevision } from '../service/configuration.mjs';
const server=createServer((req,res)=>{
 if(req.headers.authorization!=='Bearer '+process.env.MIYABI_MDCZ_KEY){res.writeHead(401);res.end();return;}
 res.setHeader('content-type','application/json');res.end(JSON.stringify({protocol:1,revision:engineRevision,configuration_version:1,configuration_id:'fixture'}));
});
server.listen(Number(process.env.PORT),process.env.HOST);
process.on('SIGTERM',()=>server.close());
`,
  );
  await writeFile(
    join(paths.app, "bin/miyabi"),
    `#!${process.execPath}
const {createServer}=require('node:http');
const {existsSync}=require('node:fs');
if(existsSync('fail-main') || process.env.MIYABI_ACCESS_PASSWORD==='failure-password')process.exit(1);
const server=createServer((req,res)=>{res.setHeader('content-type','application/json');res.end(JSON.stringify({enabled:!!process.env.MIYABI_ACCESS_PASSWORD}));});
server.listen(Number(process.env.MIYABI_LISTEN.slice(1)),'127.0.0.1');
process.on('SIGTERM',()=>server.close());
`,
    { mode: 0o755 },
  );
  const call = async (action, extra = {}) => {
    try {
      const result = await exec("bash", [join(paths.app, "service/control"), action], {
        env: { ...process.env, ...env, ...extra },
        timeout: 45_000,
      });
      return { code: 0, ...result };
    } catch (error) {
      return { code: error.code, stdout: error.stdout, stderr: error.stderr };
    }
  };
  assert.equal((await call("initialize")).code, 0);
  return { ...data, call };
}
async function eventually(check, timeout = 5000) {
  const deadline = Date.now() + timeout;
  while (Date.now() < deadline) {
    if (await check()) return;
    await delay(50);
  }
  assert.fail("condition did not settle");
}

test("concurrent starts produce one service pair; stop is idempotent and preserves data", async (t) => {
  const { paths, call } = await installed(t);
  const calls = await Promise.all([call("start"), call("start")]);
  assert.deepEqual(
    calls.map((x) => x.code),
    [0, 0],
    JSON.stringify(calls),
  );
  const before = await state(paths);
  assert.equal((await call("status")).code, 0);
  assert.equal((await call("start")).code, 0);
  assert.deepEqual((await state(paths)).children, before.children);
  await writeFile(join(paths.data, "miyabi/saved.db"), "data");
  assert.equal((await call("stop")).code, 0);
  assert.equal((await call("stop")).code, 0);
  assert.equal((await call("status")).code, 3);
  assert.equal(await readFile(join(paths.data, "miyabi/saved.db"), "utf8"), "data");
  for (const record of [before.supervisor, ...before.children])
    assert.equal(await alive(record), false);
});

test("worker failure stops its sibling and an immediate startup failure leaves no orphan", async (t) => {
  const { paths, call } = await installed(t);
  assert.equal((await call("start")).code, 0);
  const before = await state(paths);
  process.kill(before.children[0].pid, "SIGTERM");
  await eventually(async () => !(await alive(before.supervisor)));
  assert.equal(await alive(before.children[1]), false);
  await writeFile(join(paths.app, "fail-main"), "fixture");
  const result = await call("start");
  assert.equal(result.code, 1);
  const after = await state(paths);
  for (const record of [after.supervisor, ...after.children])
    assert.equal(await alive(record), false);
});

test("state write failure cannot interrupt worker cleanup", async (t) => {
  const { paths, call } = await installed(t);
  assert.equal((await call("start")).code, 0);
  const before = await state(paths);
  const file = join(paths.run, "processes.json");
  await rename(file, `${file}.saved`);
  await mkdir(file);
  try {
    process.kill(before.children[0].pid, "SIGTERM");
    await eventually(async () => !(await alive(before.supervisor)));
    assert.equal(await alive(before.children[1]), false);
  } finally {
    await rm(file, { recursive: true });
    await rename(`${file}.saved`, file);
  }
});

test("port conflict is rejected before spawning and reused PID records never signal another process", async (t) => {
  const { paths, config, call } = await installed(t);
  const occupied = createServer();
  await new Promise((resolve) => occupied.listen(config.port, "127.0.0.1", resolve));
  t.after(() => new Promise((resolve) => occupied.close(resolve)));
  const result = await call("start");
  assert.equal(result.code, 1);
  assert.match(result.stderr, /端口/);
  assert.equal(await state(paths), null);
  const own = await identity(process.pid);
  const copied = await import("file://" + join(paths.app, "service/processes.mjs"));
  await copied.terminate([{ ...own, start: "wrong-start-time" }], 1);
  assert.equal(await alive(own), true);
});

test("upgrade and uninstall stop both services and keep complete backups and exports", async (t) => {
  const { paths, config, call } = await installed(t);
  await writeFile(join(config.exports, "fixture.strm"), "export");
  await writeFile(join(paths.data, "miyabi/original.jpg"), "original");
  assert.equal((await call("start")).code, 0);
  const key = (await load(paths)).bridgeKey;
  assert.equal((await call("upgrade-before")).code, 0);
  assert.equal((await call("verify")).code, 0);
  assert.equal((await load(paths)).bridgeKey, key);
  assert.equal((await call("start")).code, 0);
  assert.equal((await call("uninstall-before")).code, 0);
  assert.equal((await call("status")).code, 3);
  const snapshots = await readdir(config.backups);
  assert.equal(snapshots.length, 2);
  for (const snapshot of snapshots) {
    assert.equal(
      JSON.parse(await readFile(join(config.backups, snapshot, "complete.json"), "utf8")).version,
      1,
    );
    assert.equal(
      await readFile(join(config.backups, snapshot, "miyabi/original.jpg"), "utf8"),
      "original",
    );
  }
  assert.equal(await readFile(join(config.exports, "fixture.strm"), "utf8"), "export");
});

test("access configuration restarts the pair, preserves playback tokens and rolls back failed startup", async (t) => {
  const { paths, config, call } = await installed(t);
  assert.equal((await call("validate-configuration", { wizard_password: "short" })).code, 1);
  assert.equal((await call("validate-configuration", { wizard_password: "new-password" })).code, 0);
  assert.deepEqual(await load(paths), config);
  assert.equal((await call("start")).code, 0);
  assert.equal((await call("configure", { wizard_password: "new-password" })).code, 0);
  const changed = await load(paths);
  assert.equal(changed.password, "new-password");
  assert.equal(changed.strmToken, config.strmToken);
  assert.notEqual(changed.jwtSecret, config.jwtSecret);
  const failed = await call("configure", { wizard_password: "failure-password" });
  assert.equal(failed.code, 1);
  assert.deepEqual(await load(paths), changed);
  assert.equal((await call("status")).code, 0);
  assert.equal(failed.stderr.includes("failure-password"), false);
});
