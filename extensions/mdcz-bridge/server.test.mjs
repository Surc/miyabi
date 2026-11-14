import { test } from "node:test";
import assert from "node:assert/strict";
import { once } from "node:events";
import { createBridge } from "./server.mjs";
import { health, incompleteSources } from "./contract.mjs";

const key = "fixture-key-with-at-least-24-characters";
test("absent releases are complete answers; unavailable or budget-limited sources are partial", () => {
  assert.equal(
    incompleteSources([
      { status: "failed", reason: "not_found" },
      { status: "skipped", skipReason: "content_type" },
      { status: "success" },
    ]),
    false,
  );
  for (const result of [
    { status: "failed", reason: "network_error" },
    { status: "skipped", skipReason: "global_timeout" },
    { status: "skipped", skipReason: "unavailable" },
  ])
    assert.equal(incompleteSources([result]), true);
});
async function fixture(t, adapter) {
  const server = createBridge(adapter, key);
  server.listen(0, "127.0.0.1");
  await once(server, "listening");
  t.after(() => {
    server.closeAllConnections();
    server.close();
  });
  const base = `http://127.0.0.1:${server.address().port}`;
  return (path, body, auth = true, signal, headers = {}) =>
    fetch(base + path, {
      signal,
      method: body === undefined ? "GET" : "POST",
      headers: {
        ...(auth ? { Authorization: `Bearer ${key}` } : {}),
        "content-type": "application/json",
        ...headers,
      },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
}

test("requires a header key and isolates invalid requests before invoking the runtime", async (t) => {
  let called = 0;
  const call = await fixture(t, {
    metadata: async () => {
      called++;
      return { detail: { code: "TEST-001" }, images: [] };
    },
  });
  assert.equal((await call("/v1/health", undefined, false)).status, 401);
  assert.deepEqual(await (await call("/v1/health")).json(), health);
  assert.equal((await call("/v1/metadata", { code: "", translate: false })).status, 400);
  assert.equal(
    (
      await call("/v1/poster", {
        code: "TEST-001",
        filenames: ["../movie.mp4"],
        position: "bottomLeft",
        poster: "aGVsbG8=",
      })
    ).status,
    400,
  );
  assert.equal(called, 0);
  const result = await call("/v1/metadata", { code: "TEST-001", translate: false });
  assert.equal(result.status, 200);
  assert.equal((await result.json()).protocol, 1);
  assert.equal(called, 1);
});

test("disconnect cancels the runtime request", { timeout: 5000 }, async (t) => {
  const started = Promise.withResolvers();
  const canceled = Promise.withResolvers();
  const call = await fixture(t, {
    metadata: async (_input, signal) => {
      started.resolve();
      await new Promise((resolve) => signal.addEventListener("abort", resolve, { once: true }));
      canceled.resolve();
      throw new Error("canceled");
    },
  });
  const abort = new AbortController();
  const request = call("/v1/metadata", { code: "TEST-001", translate: false }, true, abort.signal);
  const rejection = assert.rejects(request);
  await started.promise;
  abort.abort();
  await rejection;
  await canceled.promise;
});

test("bounds concurrency, keeps health available and redacts runtime failures", async (t) => {
  const wait = Promise.withResolvers();
  const started = Promise.withResolvers();
  const call = await fixture(t, {
    metadata: async () => {
      started.resolve();
      await wait.promise;
      throw new Error("proxy-user:secret");
    },
  });
  const first = call("/v1/metadata", { code: "TEST-001", translate: false });
  await started.promise;
  assert.equal((await call("/v1/metadata", { code: "TEST-002", translate: false })).status, 503);
  assert.equal((await call("/v1/health")).status, 200);
  wait.resolve();
  const response = await first;
  assert.equal(response.status, 502);
  assert.equal((await response.text()).includes("secret"), false);
});

test("returns image bytes only for an opaque asset id", async (t) => {
  const call = await fixture(t, {
    image: async (id) => {
      assert.equal(id, "a".repeat(64));
      return Buffer.from("fixture-image");
    },
  });
  assert.equal((await call("/v1/images/http://example.com")).status, 404);
  const response = await call("/v1/images/" + "a".repeat(64));
  assert.equal(response.status, 200);
  assert.equal(response.headers.get("content-type"), "image/jpeg");
  assert.equal(await response.text(), "fixture-image");
});


test("snapshot guard rejects changed config and engine for metadata, images and poster", async (t) => {
  let invoked = 0;
  let configurationID = "profile-one";
  const call = await fixture(t, {
    information: () => ({ configuration_version: 1, configuration_id: configurationID }),
    metadata: async () => { invoked++; return { detail: { code: "TEST-001" } }; },
    image: async () => { invoked++; return Buffer.from("image"); },
    poster: async () => { invoked++; return { poster: "aGVsbG8=" }; },
  });
  const headers = {
    "X-Miyabi-MDCz-Configuration-ID": "profile-one",
    "X-Miyabi-MDCz-Engine-Revision": health.revision,
  };
  const metadata = { code: "TEST-001", translate: false };
  const poster = { code: "TEST-001", filenames: ["TEST-001.mp4"], position: "bottomLeft", poster: "aGVsbG8=" };
  assert.equal((await call("/v1/metadata", metadata, true, undefined, headers)).status, 200);
  assert.equal(invoked, 1);
  configurationID = "profile-two";
  for (const [path, body] of [["/v1/metadata", metadata], ["/v1/poster", poster], ["/v1/images/" + "a".repeat(64), undefined]])
    assert.equal((await call(path, body, true, undefined, headers)).status, 409);
  assert.equal(invoked, 1);
  headers["X-Miyabi-MDCz-Configuration-ID"] = configurationID;
  headers["X-Miyabi-MDCz-Engine-Revision"] = "unknown-engine";
  assert.equal((await call("/v1/metadata", metadata, true, undefined, headers)).status, 409);
  delete headers["X-Miyabi-MDCz-Engine-Revision"];
  assert.equal((await call("/v1/metadata", metadata, true, undefined, headers)).status, 409);
  assert.equal(invoked, 1);
  // Old clients remain accepted; the versioned health lets new Go require the guard.
  assert.equal((await call("/v1/metadata", metadata)).status, 200);
  assert.equal(invoked, 2);
});

test("health advertises the snapshot capability and exact bridge version", async (t) => {
  const call = await fixture(t, {});
  const response = await (await call("/v1/health")).json();
  assert.equal(response.bridge_version, "1.1.0");
  assert.ok(response.capabilities.includes("attempt-snapshot"));
});
