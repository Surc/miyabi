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
import { configurationBoundary, createAdapter } from "./adapter";
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
  });
  const result = await adapter.metadata({ code: "TEST-001", translate: false }, signal());
  expect(result.partial).toBe(false);
  expect(result.detail).toMatchObject({
    code: "TEST-001",
    title: "Fixture title",
    plot: "Fixture synopsis",
    duration: 120,
  });
  expect(result.detail).not.toHaveProperty("rating");
  expect(result.images[0].url).toMatch(/^mdcz:v1\/[a-f0-9]{64}$/);
  expect(await readdir(join(directory, "assets"))).toHaveLength(1);
});
