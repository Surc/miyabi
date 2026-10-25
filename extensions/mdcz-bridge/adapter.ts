// This is the only bridge file importing MDCz runtime internals. Pin the
// upstream revision and verify these contracts before upgrading it.
import { createHash, randomUUID } from "node:crypto";
import { mkdir, readFile, writeFile, rename, mkdtemp, rm } from "node:fs/promises";
import { join, basename, extname } from "node:path";
import sharp from "sharp";
import { incompleteSources } from "./contract.mjs";
import { configurationSchema } from "@mdcz/shared/config";
import type { CrawlerData } from "@mdcz/shared/types";
import type { Website } from "@mdcz/shared/enums";
import type { PosterTagBadgePosition } from "@mdcz/shared/posterBadges";
import {
  buildComputedConfiguration,
  resolveSiteProxyUrl,
  siteNetworkKey,
} from "@mdcz/runtime/config/computed";
import { NetworkClient } from "@mdcz/runtime/network/NetworkClient";
import { runWithCrawlerSource } from "@mdcz/runtime/network/networkExecution";
import { FetchGateway } from "@mdcz/runtime/crawler/FetchGateway";
import { CrawlerProvider } from "@mdcz/runtime/crawler/CrawlerProvider";
import { listRegisteredCrawlerSites } from "@mdcz/runtime/crawler/registry";
import { AggregationService } from "@mdcz/runtime/scrape/aggregation";
import { TranslateService } from "@mdcz/runtime/scrape/TranslateService";
import { prepareOnlineMetadata } from "@mdcz/runtime/scrape/prepareOnlineMetadata";
import { PosterWatermarkService } from "@mdcz/runtime/scrape/PosterWatermarkService";
import { resolvePosterBadgeDefinitions } from "@mdcz/runtime/scrape/posterBadges";
import { parseFileInfo } from "@mdcz/runtime/scrape/utils/number";
import { classifyNumber } from "@mdcz/runtime/scrape/utils/movieClassification";

type ImageRef = { url: string; source?: Website };
const hash = (value: string) => createHash("sha256").update(value).digest("hex");
const imageURL = (value: string | undefined) => {
  if (!value) return false;
  try {
    const url = new URL(value);
    return ["http:", "https:"].includes(url.protocol) && !url.username && !url.password;
  } catch {
    return false;
  }
};

// Every mapping to an upstream configuration field is confined to this file.
const managedFields = {
  sites: ["scrape", "sites"],
  direct_sites: ["network", "directSites"],
  use_proxy: ["network", "useProxy"],
  timeout: ["network", "timeout"],
  retries: ["network", "retryCount"],
  javdb_url: ["network", "javdbUrl"],
  javbus_url: ["network", "javbusUrl"],
  javdb_cookie: ["network", "javdbCookie"],
  javbus_cookie: ["network", "javbusCookie"],
  fantia_cookie: ["network", "fantiaCookie"],
  translation_engine: ["translate", "engine"],
  target_language: ["translate", "targetLanguage"],
  deepl_key: ["translate", "deeplApiKey"],
  baidu_service: ["translate", "baiduService"],
  baidu_app_id: ["translate", "baiduAppId"],
  baidu_secret: ["translate", "baiduSecretKey"],
  baidu_key: ["translate", "baiduApiKey"],
  llm_base_url: ["translate", "llmBaseUrl"],
  llm_model: ["translate", "llmModelName"],
  llm_key: ["translate", "llmApiKey"],
  llm_api_format: ["translate", "llmApiFormat"],
  llm_service_type: ["translate", "llmServiceType"],
} as const;

export function configurationBoundary(directory: string, configuration: unknown) {
  const base = configurationSchema.parse(configuration);
  const defaults: Record<string, unknown> = {};
  for (const [key, [section, field]] of Object.entries(managedFields))
    defaults[key] = (base[section] as unknown as Record<string, unknown>)[field];
  const rawProxy = base.network.proxy.trim();
  // MDCz treats proxyType=none as disabled even when an old address remains.
  defaults.use_proxy = base.network.useProxy && base.network.proxyType !== "none" && !!rawProxy;
  defaults.proxy_url =
    base.network.proxyType === "none"
      ? ""
      : rawProxy && !rawProxy.includes("://")
        ? `${base.network.proxyType}://${rawProxy}`
        : rawProxy;
  return {
    defaults,
    sites: listRegisteredCrawlerSites(),
    baseDigest: hash(JSON.stringify(base)),
    create(values: Record<string, unknown>) {
      const next = structuredClone(base);
      for (const [key, [section, field]] of Object.entries(managedFields))
        (next[section] as unknown as Record<string, unknown>)[field] = values[key];
      next.network.proxy = String(values.proxy_url);
      next.network.proxyType = (
        next.network.proxy ? new URL(next.network.proxy).protocol.slice(0, -1) : "none"
      ) as typeof next.network.proxyType;
      return createAdapter(directory, configurationSchema.parse(next));
    },
  };
}

export async function createAdapter(directory: string, configuration: unknown) {
  const config = configurationSchema.parse(configuration);
  const computed = buildComputedConfiguration(config);
  const network = new NetworkClient({
    getProxyUrl: (site) => resolveSiteProxyUrl(computed, site),
    getTimeoutMs: () => computed.networkTimeoutMs,
    getRetryCount: () => computed.networkRetryCount,
  });
  const provider = new CrawlerProvider({
    fetchGateway: new FetchGateway(network),
    siteRequestConfigRegistrar: network,
    getSiteNetworkKey: (site) => siteNetworkKey(computed, site),
  });
  const translator = new TranslateService(network);
  const watermark = new PosterWatermarkService({ dataDir: directory });
  const assets = join(directory, "assets");
  await mkdir(assets, { recursive: true });
  sharp.cache({ memory: 16, files: 0, items: 20 });
  sharp.concurrency(1);

  async function asset(url: string, source?: Website) {
    const value: ImageRef = { url, source };
    const id = hash(JSON.stringify(value));
    const target = join(assets, `${id}.json`);
    const temp = `${target}.${randomUUID()}.tmp`;
    try {
      await writeFile(temp, JSON.stringify(value), { mode: 0o600 });
      await rename(temp, target);
    } finally {
      await rm(temp, { force: true });
    }
    return `mdcz:v1/${id}`;
  }

  return {
    async metadata(input: { code: string; translate: boolean }, signal: AbortSignal) {
      const current = structuredClone(config);
      current.translate.enableTranslation = input.translate;
      // Request-scoped aggregation avoids retaining a permanent stale cache
      // and propagates cancellation to every crawler owned by this request.
      const aggregation = new AggregationService(provider, { config: current, signal });
      const result = await prepareOnlineMetadata({
        number: input.code,
        configuration: current,
        aggregationService: aggregation,
        translateService: translator,
        keepEdits: false,
        signal,
      });
      if (result.translationError) throw new Error("Translation failed");
      const data = result.crawlerData;
      const sources = result.aggregation.sources;
      const images: Array<{ provider: string; role: string; url: string; layout?: string }> = [];
      for (const [role, url, source] of [
        ["poster", data.poster_url, sources.poster_url],
        ["cover", data.thumb_url ?? data.fanart_url, sources.thumb_url ?? sources.fanart_url],
      ] as const) {
        if (imageURL(url))
          images.push({
            provider: "mdcz",
            role,
            url: await asset(url!, source),
            layout: classifyNumber(data.number) === "censored" ? "jacket" : "single",
          });
      }
      for (const url of data.scene_images.slice(0, 30)) {
        if (imageURL(url))
          images.push({
            provider: "mdcz",
            role: "preview",
            url: await asset(url, sources.scene_images),
          });
      }
      const entity = (name?: string) =>
        name ? { provider: "mdcz", id: hash(name), name } : undefined;
      return {
        partial: incompleteSources(result.aggregation.stats.siteResults),
        detail: {
          code: data.number,
          title: data.title_zh || data.title,
          origin_title: data.original_title || data.title,
          plot: data.plot_zh || data.plot || "",
          zone: classifyNumber(data.number),
          release_date: /^\d{4}-\d{2}-\d{2}$/.test(data.release_date ?? "")
            ? data.release_date
            : "",
          duration: Math.max(0, Math.round((data.durationSeconds ?? 0) / 60)),
          // MDCz's aggregate rating does not carry a reliable denominator.
          // Leave it absent rather than relabel a source's scale.
          actors: data.actors.map((name) => ({
            ...entity(name),
            name_zht: "",
            gender: "unknown",
            avatar: "",
          })),
          tags: data.genres.map((name) => ({ ...entity(name), name_zht: "", category_id: "" })),
          maker: entity(data.studio),
          director: entity(data.director),
          series: entity(data.series),
          field_sources: Object.fromEntries(
            Object.entries(sources).map(([key, value]) => [key, `mdcz:${value}`]),
          ),
          preview_video: imageURL(data.trailer_url) ? data.trailer_url : "",
          preview_images: [],
          actor_movies: [],
          related_movies: [],
        },
        images,
      };
    },
    async image(id: string, signal: AbortSignal) {
      const ref = JSON.parse(await readFile(join(assets, `${id}.json`), "utf8")) as ImageRef;
      const get = () => network.getContent(ref.url, { signal, timeout: 30_000 });
      const body = ref.source ? await runWithCrawlerSource(ref.source, get) : await get();
      if (body.length > 20 * 1024 * 1024) throw new Error("Image too large");
      return sharp(body, { limitInputPixels: 40_000_000, animated: false })
        .rotate()
        .jpeg({ quality: 92 })
        .toBuffer();
    },
    async poster(
      input: {
        code: string;
        filenames: string[];
        poster: Buffer;
        position: PosterTagBadgePosition;
      },
      signal: AbortSignal,
    ) {
      const directoryName = await mkdtemp(join(directory, "poster-"));
      try {
        signal.throwIfAborted();
        // Deliberately omit web titles/genres: they describe a release, not the
        // user's owned file. For multi-part films require every part to match.
        const data: CrawlerData = {
          number: input.code,
          title: "",
          actors: [],
          genres: [],
          scene_images: [],
        };
        const matches = input.filenames.map((name) =>
          resolvePosterBadgeDefinitions(
            data,
            parseFileInfo(name, [], { [basename(name, extname(name))]: input.code }),
            undefined,
            // Do not infer a positive "censored" badge from missing evidence.
            ["subtitle", "umr", "leak", "uncensored", "fullHd", "fourK", "eightK"],
          ),
        );
        const badges = (matches[0] ?? []).filter((badge) =>
          matches.every((part) => part.some((other) => other.id === badge.id)),
        );
        if (!badges.length) return { poster: input.poster.toString("base64"), labels: [] };
        const path = join(directoryName, "poster.jpg");
        await sharp(input.poster, { limitInputPixels: 40_000_000, animated: false })
          .jpeg({ quality: 95 })
          .toFile(path);
        // Generated styles are fully identified by the pinned engine revision.
        // Mutable custom image files require a separate style-hash contract.
        await watermark.applyTagBadges(path, badges, input.position, { imageOverrides: false });
        signal.throwIfAborted();
        return {
          poster: (await readFile(path)).toString("base64"),
          labels: badges.map((badge) => badge.label),
        };
      } finally {
        await rm(directoryName, { recursive: true, force: true });
      }
    },
  };
}
