import { test } from "node:test";
import assert from "node:assert/strict";
import { writeFile, readFile, stat, rm } from "node:fs/promises";
import { join } from "node:path";
import {
  initialize,
  load,
  patchConfiguration,
  childEnvironment,
  backup,
  bridgeConnection,
  validate,
} from "../app/service/configuration.mjs";

import { fixture } from "./fixture.mjs";

test("installation is idempotent and keeps keys, configuration and artwork", async (t) => {
  const { paths, env, config } = await fixture(t);
  await writeFile(join(paths.data, "miyabi/original-fixture.jpg"), "original");
  await writeFile(join(paths.etc, "mdcz.json"), '{"custom":true}');
  assert.deepEqual(
    await initialize(paths, { ...env, wizard_password: "different-password" }),
    config,
  );
  assert.equal((await stat(paths.config)).mode & 0o777, 0o600);
  assert.equal(await readFile(join(paths.etc, "mdcz.json"), "utf8"), '{"custom":true}');
  assert.equal(await readFile(join(paths.data, "miyabi/original-fixture.jpg"), "utf8"), "original");
  const connection = JSON.parse(await readFile(join(paths.etc, "mdcz-connection.json"), "utf8"));
  assert.equal(connection.api_key, config.bridgeKey);
  assert.equal(connection.server_url, "http://127.0.0.1:3840");
});

test("external MDCz provisioning persists across initialization without replacing app credentials", async (t) => {
  const {paths, env, config} = await fixture(t);
  const next={...config,externalMDCz:{serverURL:"http://nas.test:3841",apiKey:"a".repeat(64)}};
  validate(next);
  await writeFile(paths.config,JSON.stringify(next));
  assert.deepEqual(await initialize(paths,env),next);
  assert.deepEqual(JSON.parse(await readFile(join(paths.etc,"mdcz-connection.json"),"utf8")),bridgeConnection(next));
  assert.equal((await load(paths)).strmToken,config.strmToken);
  assert.throws(()=>validate({...next,externalMDCz:{...next.externalMDCz,serverURL:"http://user:secret@nas.test/"}}));
  assert.throws(()=>validate({...next,externalMDCz:{...next.externalMDCz,apiKey:"short"}}));
});

test("optional JavLibrary browser connection survives initialization and stays out of other processes", async (t) => {
  const {paths, env, config} = await fixture(t);
  assert.equal(childEnvironment(paths, config).miyabi.MIYABI_JAVLIBRARY_BROWSER_CONFIG, undefined);
  const next = {...config, javlibraryBrowser:{serverURL:"http://pc.test:18082",apiToken:"b".repeat(64)}};
  validate(next);
  await writeFile(paths.config,JSON.stringify(next));
  assert.deepEqual(await initialize(paths,env),next);
  const file = join(paths.etc,"javlibrary-browser.json");
  assert.deepEqual(JSON.parse(await readFile(file,"utf8")),{enabled:true,server_url:"http://pc.test:18082",api_token:"b".repeat(64)});
  assert.equal((await stat(file)).mode & 0o777,0o600);
  assert.equal(childEnvironment(paths,next).miyabi.MIYABI_JAVLIBRARY_BROWSER_CONFIG,file);
  assert.equal(childEnvironment(paths,next).bridge.MIYABI_JAVLIBRARY_BROWSER_CONFIG,undefined);
  assert.throws(()=>validate({...next,javlibraryBrowser:{...next.javlibraryBrowser,serverURL:"http://user:secret@pc.test/"}}));
  assert.equal((await load(paths)).strmToken,config.strmToken);

  const relay = {...config, javlibraryBrowser:{connectBack:true, apiToken:next.javlibraryBrowser.apiToken}};
  await writeFile(paths.config,JSON.stringify(relay));
  assert.deepEqual(await initialize(paths,env),relay);
  assert.deepEqual(JSON.parse(await readFile(file,"utf8")),{enabled:true, connect_back:true, api_token:next.javlibraryBrowser.apiToken});
  assert.throws(()=>validate({...relay,javlibraryBrowser:{...relay.javlibraryBrowser,serverURL:"http://pc.test:18082"}}));
});

test("malformed existing settings are not replaced and user settings are validated", async (t) => {
  const { paths, env, config } = await fixture(t);
  for (const value of ["{", "null", "{}"]) {
    await writeFile(paths.config, value);
    await assert.rejects(initialize(paths, env));
    assert.equal(await readFile(paths.config, "utf8"), value);
  }
  assert.throws(() => patchConfiguration(config, { wizard_password: "short" }));
  assert.throws(() =>
    patchConfiguration(config, { wizard_public_url: "https://user:private@host/" }),
  );
  const next = patchConfiguration(config, { wizard_password: "new-password" });
  assert.equal(next.password, "new-password");
  assert.equal(next.strmToken, config.strmToken);
  assert.equal(next.bridgeKey, config.bridgeKey);
  assert.notEqual(next.jwtSecret, config.jwtSecret);
  assert.deepEqual(
    patchConfiguration(config, { wizard_password: "", wizard_public_url: "" }),
    config,
  );
});

test("children receive separate credentials and no inherited proxy or wizard environment", async (t) => {
  const { paths, config } = await fixture(t);
  const env = childEnvironment(paths, config);
  assert.equal(env.bridge.HOST, "127.0.0.1");
  assert.equal(env.bridge.MIYABI_MDCZ_KEY, config.bridgeKey);
  assert.equal(env.miyabi.MIYABI_MDCZ_KEY, undefined);
  assert.equal(env.bridge.MIYABI_ACCESS_PASSWORD, undefined);
  assert.equal(env.miyabi.MIYABI_ACCESS_PASSWORD, config.password);
  for (const values of Object.values(env)) {
    assert.equal(values.wizard_password, undefined);
    assert.equal(values.HTTP_PROXY, undefined);
  }
});

test("backups preserve original artwork and completion is absent on copy failure", async (t) => {
  const { paths, config } = await fixture(t);
  await writeFile(join(paths.data, "miyabi/original.jpg"), "original");
  await writeFile(join(paths.data, "mdcz/configuration-v1.json"), "private fixture");
  await writeFile(join(config.exports, "movie.strm"), "exported");
  const target = await backup(paths, config, "upgrade");
  assert.equal(await readFile(join(target, "miyabi/original.jpg"), "utf8"), "original");
  assert.deepEqual(JSON.parse(await readFile(join(target, "etc/native-v1.json"), "utf8")), config);
  assert.equal(JSON.parse(await readFile(join(target, "complete.json"), "utf8")).reason, "upgrade");
  assert.equal(await readFile(join(config.exports, "movie.strm"), "utf8"), "exported");
  await rm(join(paths.data, "mdcz"), { recursive: true });
  await assert.rejects(backup(paths, config, "failure"));
  const { readdir } = await import("node:fs/promises");
  const failed = (await readdir(config.backups)).find((name) => name.startsWith("failure-"));
  await assert.rejects(stat(join(config.backups, failed, "complete.json")), { code: "ENOENT" });
  assert.equal((await load(paths)).strmToken, config.strmToken);
});
