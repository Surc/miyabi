import { randomUUID } from "node:crypto";
import { link, mkdir, readdir, readFile, rm, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { buildServer } from "../server/src/app";
import { createServerTranslationMappingStore } from "../server/src/translationMappingStore";
import type { Configuration } from "@mdcz/shared/config";
import { serializeConfiguration } from "@mdcz/shared/configCodec";
import { readLegacyTranslation } from "./legacy-translation.mjs";
import { configurationBoundary, createAdapter } from "./adapter";
import { validateValues } from "./configuration.mjs";
import { createProfileAdapter } from "./profile.mjs";
import { createBridge } from "./server.mjs";

const directory = process.env.MIYABI_MDCZ_DATA!;
const key = process.env.MIYABI_MDCZ_KEY!;
const mappingStore = createServerTranslationMappingStore();
const webui = buildServer({
  backgroundAutomation: false,
  webStaticDir: join(import.meta.dirname, "web"),
  resources: { mappingStore },
});

const { configDir } = webui.services.config.runtimePaths;
await mkdir(configDir, { recursive: true, mode: 0o700 });
// An imported or previously edited official profile takes precedence. On
// first use, seed it from the legacy bundled bridge's saved configuration.
const entries = await readdir(configDir);
if (!entries.some((name) => name !== "auth-state.json" && /\.(toml|json)$/.test(name))) {
  const base = JSON.parse(await readFile(process.env.MIYABI_MDCZ_CONFIG!, "utf8"));
  const boundary = configurationBoundary(directory, base);
  const legacy = await readFile(join(directory, "configuration-v1.json"), "utf8").catch(
    (error: NodeJS.ErrnoException) => {
      if (error.code !== "ENOENT") throw error;
      return undefined;
    },
  );
  const values = legacy === undefined
    ? boundary.defaults
    : validateValues(JSON.parse(legacy).values, boundary.sites);
  const profile = boundary.profile(values);
  profile.translate.enableTranslation =
    await readLegacyTranslation(process.env.MIYABI_MDCZ_LEGACY_DATABASE!) ??
    profile.translate.enableTranslation;
  const temporary = join(configDir, `.miyabi-default-${randomUUID()}.tmp`);
  try {
    await writeFile(temporary, serializeConfiguration(profile), { mode: 0o600 });
    await link(temporary, join(configDir, "default.toml")).catch((error: NodeJS.ErrnoException) => {
      if (error.code !== "EEXIST") throw error;
    });
  } finally {
    await rm(temporary, { force: true });
  }
}

const adapter = createProfileAdapter(
  key,
  () => webui.services.config.get(),
  async (profile: Configuration) => {
    const { defaults, sites } = configurationBoundary(directory, profile);
    const runtime = await createAdapter(directory, profile, {
      useProfileTranslation: true,
      mappingStore,
    });
    return { defaults, sites, runtime };
  },
);
const bridge = createBridge(adapter, key);
bridge.requestTimeout = 155_000;
bridge.headersTimeout = 10_000;

try {
  await webui.fastify.listen({ host: "0.0.0.0", port: Number(process.env.MIYABI_MDCZ_WEB_PORT) });
  await new Promise<void>((resolve, reject) => {
    bridge.once("error", reject);
    bridge.listen(Number(process.env.PORT), "127.0.0.1", () => {
      bridge.off("error", reject);
      resolve();
    });
  });
} catch (error) {
  await webui.fastify.close();
  throw error;
}

for (const signal of ["SIGINT", "SIGTERM"] as const) {
  process.once(signal, () => {
    bridge.close();
    bridge.closeAllConnections();
    void webui.fastify.close().catch(() => {
      console.error("MDCz WebUI shutdown failed");
      process.exitCode = 1;
    });
  });
}
