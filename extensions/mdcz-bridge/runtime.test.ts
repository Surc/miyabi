import { afterEach, beforeAll, beforeEach, expect, test, vi } from "vitest";
import { execFileSync } from "node:child_process";
import { mkdtemp, readFile, readdir, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { createServer } from "node:http";
import { once } from "node:events";
import sharp from "sharp";
import { CrawlerProvider } from "@mdcz/runtime/crawler/CrawlerProvider";
import { Website } from "@mdcz/shared/enums";
import { configurationBoundary, createAdapter, parseBridgeConfiguration } from "./adapter";
import { parseFileInfo } from "@mdcz/runtime/scrape/utils/number";
import { createManagedAdapter } from "./configuration.mjs";
import { revision } from "./contract.mjs";

let directory: string;
const signal = () => new AbortController().signal;
const configuration = JSON.parse(
  await readFile(new URL("./mdcz.example.json", import.meta.url), "utf8"),
);
const poster = () =>
  sharp({ create: { width: 400, height: 600, channels: 3, background: "#345678" } })
    .jpeg()
    .toBuffer();

beforeAll(() => {
  expect(
    execFileSync("git", ["rev-parse", "HEAD"], {
      cwd: resolve(import.meta.dirname, "../.."),
      encoding: "utf8",
    }).trim(),
  ).toBe(revision);
});
beforeEach(async () => {
  directory = await mkdtemp(join(tmpdir(), "miyabi-mdcz-runtime-"));
});
afterEach(async () => {
  await rm(directory, { recursive: true, force: true });
});

test("real engine defaults and managed settings initialize, persist and reload", async () => {
  const legacy = {
    ...configuration,
    personSync: { actorAliases: { "Canonical Actor": ["Old Alias", "第二艺名"] } },
    network: { ...configuration.network, javdbCookie: "legacy-cookie", proxy: "http://127.0.0.1:7890", proxyType: "http", useProxy: true },
  };
  const original = structuredClone(legacy);
  const parsed = parseBridgeConfiguration(legacy);
  expect(parsed.personSync.actorAliases).toEqual([{ name: "Canonical Actor", aliases: ["Old Alias", "第二艺名"] }]);
  expect(parsed.network.javdbCookie).toBe("legacy-cookie");
  expect(parsed.network.proxy).toBe("http://127.0.0.1:7890");
  expect(legacy).toEqual(original);
  expect(() => parseBridgeConfiguration({ ...legacy, personSync: { actorAliases: { One: ["Alias"], Two: ["Alias"] } } })).toThrow();
  const boundary = configurationBoundary(directory, configuration);
  expect(boundary.sites).toContain(Website.JAVDB);
  expect(boundary.defaults.use_proxy).toBe(false);
  const adapter = await createManagedAdapter(directory, boundary);
  await adapter.configure({
    ...adapter.information(),
    values: {
      sites: [Website.JAVDB],
      timeout: 25,
      translation_engine: "baidu",
      baidu_app_id: "fixture-app",
      baidu_secret: "fixture-secret",
    },
  });
  const saved = adapter.configuration();
  expect(saved.values.timeout).toBe(25);
  expect(saved.secrets.baidu_secret).toBe(true);
  expect(JSON.stringify(saved)).not.toContain("fixture-secret");
  const reloaded = await createManagedAdapter(directory, boundary);
  expect(reloaded.configuration()).toEqual(saved);
});

test("real filename parsing and sharp produce common badges and clean cancelled work", async () => {
  expect(parseFileInfo("unrecognizable-name-C-CD2.mp4", { numberMappings: { "unrecognizable-name": "TEST-001" } })).toMatchObject({ number: "TEST-001", isSubtitled: true, part: { number: 2 } });
  const adapter = await createAdapter(directory, configuration);
  const original = await poster();
  const input = { code: "TEST-001", poster: original, position: "bottomLeft" as const };
  const decorated = await adapter.poster(
    { ...input, filenames: ["TEST-001-C-破解-1080p.mp4"] },
    signal(),
  );
  expect(decorated.labels).toEqual(expect.arrayContaining(["中字", "破解", "1080P"]));
  const bytes = Buffer.from(decorated.poster, "base64");
  expect(bytes.equals(original)).toBe(false);
  expect(await sharp(bytes).metadata()).toMatchObject({ format: "jpeg", width: 400, height: 600 });
  const basePixels = await sharp(original).raw().toBuffer();
  for (const position of ["topLeft", "topRight", "bottomLeft", "bottomRight"] as const) {
    const result = await adapter.poster({ ...input, position, filenames: ["TEST-001-C.mp4"] }, signal());
    const pixels = await sharp(Buffer.from(result.poster, "base64")).raw().toBuffer();
    const changes = [0, 0, 0, 0];
    for (let y = 0; y < 600; y++) for (let x = 0; x < 400; x++) {
      const i = (y * 400 + x) * 3;
      if (Math.max(...[0, 1, 2].map((c) => Math.abs(pixels[i + c] - basePixels[i + c]))) > 40)
        changes[(y >= 300 ? 2 : 0) + (x >= 200 ? 1 : 0)]++;
    }
    const corner = ["topLeft", "topRight", "bottomLeft", "bottomRight"].indexOf(position);
    expect(changes[corner]).toBeGreaterThan(100);
    expect(changes.filter((_, index) => index !== corner).every((value) => value < 20)).toBe(true);
    const repeated = await adapter.poster({ ...input, position, filenames: ["TEST-001-C.mp4"] }, signal());
    expect(repeated.poster).toEqual(result.poster);
  }
  const mixed = await adapter.poster(
    { ...input, filenames: ["TEST-001-C-破解-1080p-CD1.mp4", "TEST-001-CD2.mp4"] },
    signal(),
  );
  expect(mixed.labels).toEqual([]);
  expect(Buffer.from(mixed.poster, "base64")).toEqual(original);
  await expect(
    adapter.poster({ ...input, filenames: ["TEST-001-C.mp4"] }, AbortSignal.abort()),
  ).rejects.toThrow();
  expect((await readdir(directory)).filter((name) => name.startsWith("poster-"))).toEqual([]);
});

test("real impit fetches an issued image reference and sharp returns JPEG", async () => {
  const image = await poster();
  const server = createServer((_request, response) => {
    response.setHeader("content-type", "image/jpeg");
    response.end(image);
  });
  server.listen(0, "127.0.0.1");
  await once(server, "listening");
  try {
    const port = (server.address() as { port: number }).port;
    const adapter = await createAdapter(directory, configuration);
    const id = "a".repeat(64);
    await writeFile(
      join(directory, "assets", `${id}.json`),
      JSON.stringify({ url: `http://127.0.0.1:${port}/fixture.jpg` }),
    );
    const bytes = await adapter.image(id, signal());
    expect(await sharp(bytes).metadata()).toMatchObject({
      format: "jpeg",
      width: 400,
      height: 600,
    });
  } finally {
    server.closeAllConnections();
    await new Promise<void>((resolve, reject) =>
      server.close((error) => (error ? reject(error) : resolve())),
    );
  }
});

test("real aggregation maps controlled crawler results without inventing a rating scale", async () => {
  // Keep only the external source response synthetic: aggregation, publication,
  // identity and asset-reference mapping use the pinned engine implementation.
  vi.spyOn(CrawlerProvider.prototype, "crawl").mockImplementation(async (input) => ({
    input,
    elapsedMs: 0,
    result: {
      success: true,
      data: {
        number: input.number,
        title: "Fixture title",
        plot: "Fixture synopsis",
        actors: ["Fixture actor"],
        genres: ["Fixture genre"],
        scene_images: [],
        poster_url: "https://images.example.test/poster.jpg",
        release_date: "2026-01-02",
        durationSeconds: 7200,
      },
    },
  }));
  const adapter = await createAdapter(directory, {
    ...configuration,
    scrape: { ...configuration.scrape, sites: [Website.JAVDB] },
    personSync: { actorAliases: { "Canonical actor": ["Fixture actor"] } },
  });
  const result = await adapter.metadata({ code: "TEST-001", translate: false }, signal());
  expect(result.partial).toBe(false);
  expect(result.detail).toMatchObject({
    code: "TEST-001",
    title: "Fixture title",
    plot: "Fixture synopsis",
    duration: 120,
  });
  expect(result.detail.actors.map((actor) => actor.name)).toEqual(["Canonical actor"]);
  expect(result.detail).not.toHaveProperty("rating");
  expect(result.images[0].url).toMatch(/^mdcz:v1\/[a-f0-9]{64}$/);
  expect(await readdir(join(directory, "assets"))).toHaveLength(1);
});
