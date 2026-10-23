// Own configuration contract. MDCz field names and defaults stay in adapter.ts.
import { randomUUID } from "node:crypto";
import { mkdir, readFile, writeFile, rename, rm } from "node:fs/promises";
import { join } from "node:path";

export const configurationVersion = 1;
export const secretFields = [
  "proxy_url",
  "javdb_cookie",
  "javbus_cookie",
  "fantia_cookie",
  "deepl_key",
  "baidu_secret",
  "baidu_key",
  "llm_key",
];
const stringFields = ["javdb_url", "javbus_url", "baidu_app_id", "llm_base_url", "llm_model"];
const enums = {
  translation_engine: ["google", "deepl", "baidu", "openai"],
  target_language: ["zh-CN", "zh-TW"],
  baidu_service: ["general", "llm"],
  llm_api_format: ["responses", "chat-completions"],
  llm_service_type: ["openai-compatible", "google", "deepseek"],
};
const invalid = () => Object.assign(new Error("Invalid configuration"), { statusCode: 400 });
const object = (value) => value !== null && typeof value === "object" && !Array.isArray(value);

function validURL(value, proxy = false) {
  if (!value) return true;
  try {
    const url = new URL(value);
    return (
      (proxy ? ["http:", "https:", "socks5:"] : ["http:", "https:"]).includes(url.protocol) &&
      !!url.hostname &&
      !url.hash &&
      !url.search &&
      (proxy ? url.pathname === "" || url.pathname === "/" : !url.username && !url.password)
    );
  } catch {
    return false;
  }
}

export function validateValues(values, availableSites) {
  if (!object(values)) throw invalid();
  const allowed = new Set([
    ...secretFields,
    ...stringFields,
    ...Object.keys(enums),
    "sites",
    "direct_sites",
    "use_proxy",
    "timeout",
    "retries",
  ]);
  if (Object.keys(values).some((key) => !allowed.has(key))) throw invalid();
  for (const name of [...secretFields, ...stringFields]) {
    if (
      typeof values[name] !== "string" ||
      values[name].length > 16384 ||
      /[\r\n]/.test(values[name])
    )
      throw invalid();
  }
  for (const [key, choices] of Object.entries(enums))
    if (!choices.includes(values[key])) throw invalid();
  for (const key of ["sites", "direct_sites"]) {
    const items = values[key];
    if (
      !Array.isArray(items) ||
      items.length > availableSites.length ||
      new Set(items).size !== items.length ||
      items.some((item) => !availableSites.includes(item))
    )
      throw invalid();
  }
  if (
    !values.sites.length ||
    typeof values.use_proxy !== "boolean" ||
    !Number.isInteger(values.timeout) ||
    values.timeout < 1 ||
    values.timeout > 300 ||
    !Number.isInteger(values.retries) ||
    values.retries < 0 ||
    values.retries > 10
  )
    throw invalid();
  if (!validURL(values.proxy_url, true) || (values.use_proxy && !values.proxy_url)) throw invalid();
  for (const name of ["javdb_url", "javbus_url", "llm_base_url"])
    if (!validURL(values[name])) throw invalid();
  return values;
}

// The HTTP server serializes mutations with jobs. Build and persist a complete
// candidate before swapping it; a failed save leaves the live runtime intact.
export async function createManagedAdapter(directory, boundary) {
  await mkdir(directory, { recursive: true });
  const file = join(directory, "configuration-v1.json");
  let state;
  try {
    state = JSON.parse(await readFile(file, "utf8"));
  } catch (error) {
    if (error.code !== "ENOENT") throw error;
  }
  if (
    state !== undefined &&
    (!object(state) ||
      state.version !== configurationVersion ||
      typeof state.id !== "string" ||
      !state.id ||
      typeof state.base_digest !== "string")
  )
    throw invalid();
  const values = validateValues(
    state === undefined ? boundary.defaults : state.values,
    boundary.sites,
  );
  let runtime = await boundary.create(values);
  const persist = async (next) => {
    const temp = `${file}.${randomUUID()}.tmp`;
    try {
      await writeFile(temp, JSON.stringify(next), { mode: 0o600 });
      await rename(temp, file);
    } finally {
      await rm(temp, { force: true });
    }
  };
  if (!state || state.base_digest !== boundary.baseDigest) {
    state = {
      version: configurationVersion,
      id: randomUUID(),
      base_digest: boundary.baseDigest,
      values,
    };
    await persist(state);
  }
  const information = () => ({
    configuration_version: configurationVersion,
    configuration_id: state.id,
  });
  const configuration = () => {
    const visible = structuredClone(state.values);
    const secrets = Object.fromEntries(secretFields.map((key) => [key, !!visible[key]]));
    for (const key of secretFields) delete visible[key];
    return { ...information(), values: visible, secrets, available_sites: boundary.sites };
  };
  return {
    information,
    configuration,
    async configure(input) {
      if (
        !object(input) ||
        !object(input.values) ||
        input.configuration_version !== configurationVersion
      )
        throw invalid();
      if (input.configuration_id !== state.id)
        throw Object.assign(new Error("Configuration changed"), { statusCode: 409 });
      const values = validateValues({ ...state.values, ...input.values }, boundary.sites);
      let candidate;
      try {
        candidate = await boundary.create(values);
      } catch {
        throw invalid();
      }
      const next = { ...state, id: randomUUID(), values };
      await persist(next);
      state = next;
      runtime = candidate;
      return configuration();
    },
    async metadata(input, signal) {
      const id = state.id;
      return { ...(await runtime.metadata(input, signal)), configuration_id: id };
    },
    image: (id, signal) => runtime.image(id, signal),
    poster: (input, signal) => runtime.poster(input, signal),
  };
}
