import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtemp, readFile, writeFile, stat, rm, mkdir } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { once } from "node:events";
import { createManagedAdapter, secretFields } from "./configuration.mjs";
import { createBridge } from "./server.mjs";

const key = "fixture-bridge-key-at-least-24-characters";
function boundary(create) {
  return {
    sites: ["dmm", "javdb"],
    baseDigest: "fixture-base",
    defaults: {
      ...Object.fromEntries(secretFields.map((name) => [name, ""])),
      sites: ["dmm"],
      direct_sites: [],
      use_proxy: false,
      timeout: 15,
      retries: 1,
      javdb_url: "",
      javbus_url: "",
      translation_engine: "google",
      target_language: "zh-CN",
      baidu_service: "general",
      baidu_app_id: "",
      llm_base_url: "https://api.example.test/v1",
      llm_model: "fixture",
      llm_api_format: "responses",
      llm_service_type: "openai-compatible",
    },
    create: create ?? (async (values) => ({ metadata: async () => ({ timeout: values.timeout }) })),
  };
}
async function fixture(t, source = boundary()) {
  const dir = await mkdtemp(join(tmpdir(), "mdcz-configuration-test-"));
  t.after(() => rm(dir, { force: true, recursive: true }));
  return { dir, source, adapter: await createManagedAdapter(dir, source) };
}
const change = (adapter, values) => ({ ...adapter.information(), values });

test("configuration persists, masks credentials and supports explicit secret deletion", async (t) => {
  const { dir, source, adapter } = await fixture(t);
  const initial = adapter.information();
  await adapter.configure(
    change(adapter, {
      timeout: 30,
      proxy_url: "http://user:secret@proxy.test:7890",
      use_proxy: true,
      llm_key: "private-model-key",
    }),
  );
  const saved = adapter.configuration();
  assert.notEqual(saved.configuration_id, initial.configuration_id);
  assert.equal(saved.values.timeout, 30);
  assert.equal(saved.secrets.llm_key, true);
  assert.equal(saved.secrets.proxy_url, true);
  assert.equal(JSON.stringify(saved).includes("private-model-key"), false);
  assert.equal(JSON.stringify(saved).includes("user:secret"), false);
  assert.equal(source.defaults.llm_key, "");
  assert.equal((await stat(join(dir, "configuration-v1.json"))).mode & 0o777, 0o600);
  const restarted = await createManagedAdapter(dir, source);
  assert.deepEqual(restarted.configuration(), saved);
  await restarted.configure(change(restarted, { retries: 2 }));
  assert.equal(restarted.configuration().secrets.llm_key, true);
  await restarted.configure(change(restarted, { llm_key: "" }));
  assert.equal(restarted.configuration().secrets.llm_key, false);
  const metadata = await restarted.metadata({}, new AbortController().signal);
  assert.equal(metadata.configuration_id, restarted.information().configuration_id);
  assert.equal(metadata.timeout, 30);
});

test("invalid values and stale edits cannot replace a saved configuration", async (t) => {
  const { adapter, dir } = await fixture(t);
  const before = await readFile(join(dir, "configuration-v1.json"), "utf8");
  for (const values of [
    { sites: [] },
    { sites: ["unknown"] },
    { retries: 20 },
    { timeout: "30" },
    { proxy_url: "file:///etc/passwd" },
    { llm_base_url: "https://user:secret@example.test" },
    { paths: { media: "/owned" } },
    { javdb_cookie: "secret\nheader" },
  ]) {
    await assert.rejects(adapter.configure(change(adapter, values)), { statusCode: 400 });
    assert.equal(await readFile(join(dir, "configuration-v1.json"), "utf8"), before);
  }
  await assert.rejects(adapter.configure({ ...change(adapter, {}), configuration_id: "stale" }), {
    statusCode: 409,
  });
});

test("runtime construction or disk failure leaves the live profile unchanged", async (t) => {
  const source = boundary(async (values) => {
    if (values.timeout === 29) throw new Error("private upstream diagnostic");
    return { metadata: async () => ({ timeout: values.timeout }) };
  });
  const { adapter, dir } = await fixture(t, source);
  const before = adapter.configuration();
  await assert.rejects(adapter.configure(change(adapter, { timeout: 29 })), {
    statusCode: 400,
    message: "Invalid configuration",
  });
  assert.deepEqual(adapter.configuration(), before);
  const file = join(dir, "configuration-v1.json");
  await rm(file);
  await mkdir(file);
  await assert.rejects(adapter.configure(change(adapter, { timeout: 30 })));
  assert.deepEqual(adapter.configuration(), before);
  assert.equal((await adapter.metadata({}, new AbortController().signal)).timeout, 15);
});

test("base configuration changes invalidate cached introductions without discarding managed settings", async (t) => {
  const { adapter, dir, source } = await fixture(t);
  await adapter.configure(change(adapter, { timeout: 30 }));
  const restarted = await createManagedAdapter(dir, { ...source, baseDigest: "updated-base" });
  assert.notEqual(restarted.information().configuration_id, adapter.information().configuration_id);
  assert.equal(restarted.configuration().values.timeout, 30);
});

test("a corrupt saved profile is never silently replaced with defaults", async (t) => {
  const { dir, source } = await fixture(t);
  const file = join(dir, "configuration-v1.json");
  for (const value of [
    "{",
    "null",
    "false",
    JSON.stringify({ version: 1, id: "saved", base_digest: "fixture-base", values: null }),
  ]) {
    await writeFile(file, value);
    await assert.rejects(createManagedAdapter(dir, source));
    assert.equal(await readFile(file, "utf8"), value);
  }
});

test("configuration HTTP writes share the job lock; reads and health stay available", async (t) => {
  const started = Promise.withResolvers();
  const release = Promise.withResolvers();
  const { adapter } = await fixture(
    t,
    boundary(async () => ({
      metadata: async () => {
        started.resolve();
        await release.promise;
        return {};
      },
    })),
  );
  const server = createBridge(adapter, key);
  server.listen(0, "127.0.0.1");
  await once(server, "listening");
  t.after(() => {
    server.closeAllConnections();
    server.close();
  });
  const call = (path, method = "GET", input, auth = true) =>
    fetch(`http://127.0.0.1:${server.address().port}${path}`, {
      method,
      headers: {
        "content-type": "application/json",
        ...(auth ? { Authorization: `Bearer ${key}` } : {}),
      },
      ...(method === "GET" ? {} : { body: JSON.stringify(input) }),
    });
  assert.equal((await call("/v1/configuration", "GET", undefined, false)).status, 401);
  const old = await (await call("/v1/configuration")).json();
  const running = call("/v1/metadata", "POST", { code: "TEST-001", translate: false });
  await started.promise;
  assert.equal(
    (await call("/v1/configuration", "PUT", change(adapter, { timeout: 30 }))).status,
    503,
  );
  assert.equal((await (await call("/v1/health")).json()).configuration_id, old.configuration_id);
  assert.equal((await call("/v1/configuration")).status, 200);
  release.resolve();
  await running;
  assert.equal(
    (await call("/v1/configuration", "PUT", change(adapter, { timeout: 30 }))).status,
    200,
  );
  assert.equal(
    (await call("/v1/configuration", "PUT", { ...old, values: { timeout: 31 } })).status,
    409,
  );
  assert.equal(
    (await call("/v1/configuration", "PUT", change(adapter, { unexpected: "private" }))).status,
    400,
  );
});
