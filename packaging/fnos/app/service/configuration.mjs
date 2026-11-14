// The FPK owns installation settings; application settings stay in Miyabi/MDCz.
import { randomBytes, randomUUID, createHash } from "node:crypto";
import { mkdir, readFile, writeFile, rename, rm, cp, access, chmod } from "node:fs/promises";
import { join, isAbsolute } from "node:path";
import { verifyPublicationBackup } from "./publication-backup.mjs";

import { engineRevision } from "./compatibility.mjs";
export { engineRevision } from "./compatibility.mjs";
export function locations(env = process.env) {
  const result = { app: env.TRIM_APPDEST, etc: env.TRIM_PKGETC, data: env.TRIM_PKGVAR };
  for (const value of Object.values(result))
    if (!value || !isAbsolute(value) || /[\r\n]/.test(value))
      throw new Error("缺少有效的飞牛应用目录");
  return { ...result, run: join(result.data, "run"), config: join(result.etc, "native-v1.json") };
}
export async function readJSON(file) {
  const body = await readFile(file, "utf8");
  try {
    return JSON.parse(body);
  } catch {
    throw new Error("应用 JSON 配置损坏，请从备份恢复");
  }
}
export async function writeJSON(file, value) {
  const temporary = `${file}.${randomUUID()}.tmp`;
  try {
    await writeFile(temporary, JSON.stringify(value, null, 2) + "\n", { mode: 0o600 });
    await rename(temporary, file);
  } finally {
    await rm(temporary, { force: true });
  }
}
export function validate(value) {
  if (!value || value.version !== 1) throw new Error("安装配置版本无效，请从备份恢复");
  if (value.mdczConnectionMode !== undefined && !["managed", "legacy"].includes(value.mdczConnectionMode))
    throw new Error("MDCz 连接管理模式无效");
  for (const key of ["port", "bridgePort"])
    if (!Number.isInteger(value[key]) || value[key] < 1024 || value[key] > 65535)
      throw new Error("端口必须为 1024–65535 的整数");
  if (value.port === value.bridgePort) throw new Error("网页端口与内部刮削端口不能相同");
  if (value.mdczWebPort !== undefined &&
      (!Number.isInteger(value.mdczWebPort) || value.mdczWebPort < 1024 || value.mdczWebPort > 65535 ||
        value.mdczWebPort === value.port || value.mdczWebPort === value.bridgePort))
    throw new Error("MDCz 网页端口必须为 1024–65535，并与 Miyabi 和内部桥接端口不同");
  if (value.externalMDCz) {
    let endpoint;
    try { endpoint = new URL(value.externalMDCz.serverURL); } catch { throw new Error("外部 MDCz 地址无效"); }
    if (!["http:", "https:"].includes(endpoint.protocol) || endpoint.username || endpoint.password ||
        endpoint.search || endpoint.hash || endpoint.pathname !== "/" ||
        !/^[a-f0-9]{64}$/.test(value.externalMDCz.apiKey))
      throw new Error("外部 MDCz 连接配置无效");
  }
  if (value.javlibraryBrowser) {
    const { serverURL, connectBack, apiToken } = value.javlibraryBrowser;
    if (!/^[a-f0-9]{64}$/.test(apiToken) || (connectBack !== undefined && typeof connectBack !== "boolean"))
      throw new Error("JavLibrary 浏览器连接配置无效");
    if (connectBack) {
      if (serverURL !== undefined) throw new Error("JavLibrary 反向连接无需填写浏览器地址");
    } else {
      let endpoint;
      try { endpoint = new URL(serverURL); } catch { throw new Error("JavLibrary 浏览器地址无效"); }
      if (!["http:", "https:"].includes(endpoint.protocol) || endpoint.username || endpoint.password ||
          endpoint.search || endpoint.hash || endpoint.pathname !== "/")
        throw new Error("JavLibrary 浏览器连接配置无效");
    }
  }
  if (
    typeof value.password !== "string" ||
    value.password.length < 8 ||
    value.password.length > 256 ||
    /[\r\n]/.test(value.password)
  )
    throw new Error("访问密码需为 8–256 个字符，不能包含换行");
  for (const key of ["bridgeKey", "jwtSecret"])
    if (typeof value[key] !== "string" || !/^[a-f0-9]{64}$/.test(value[key]))
      throw new Error("安装密钥缺失，请从备份恢复");
  // Existing Docker exports may use a user-supplied token; permit preserving
  // it during an explicit offline migration instead of breaking every STRM.
  if (
    typeof value.strmToken !== "string" ||
    !value.strmToken ||
    value.strmToken.length > 4096 ||
    /[\r\n]/.test(value.strmToken)
  )
    throw new Error("播放令牌无效，请从备份恢复");
  let url;
  try {
    url = new URL(value.publicURL);
  } catch {
    throw new Error("请填写 Emby 能访问的 Miyabi HTTP(S) 地址");
  }
  if (
    !["http:", "https:"].includes(url.protocol) ||
    !url.hostname ||
    url.username ||
    url.password ||
    url.search ||
    url.hash ||
    url.pathname !== "/"
  )
    throw new Error("访问地址需为 HTTP(S) 根地址，不能含账号或参数");
  for (const key of ["exports", "backups"])
    if (typeof value[key] !== "string" || !isAbsolute(value[key]) || /[\r\n]/.test(value[key]))
      throw new Error("缺少授权的导出或备份目录");
  return value;
}
export async function load(paths) {
  return validate(await readJSON(paths.config));
}

export async function initialize(paths, env = process.env) {
  for (const folder of [paths.etc, paths.data, paths.run])
    await mkdir(folder, { recursive: true, mode: 0o700 });
  let current;
  try {
    current = await load(paths);
  } catch (error) {
    if (error.code !== "ENOENT") throw error;
  }
  if (!current) {
    const shares = (env.TRIM_DATA_SHARE_PATHS || "").split(":");
    const share = (name) => shares.find((path) => path.endsWith(`/miyabi-native/${name}`));
    const port = Number(env.wizard_port || 18080);
    const mdczPort = env.wizard_mdcz_web_port || (port === 3838 ? 3839 : 3838);
    current = validate({
      version: 1,
      mdczConnectionMode: "managed",
      port,
      mdczWebPort: Number(mdczPort),
      bridgePort: 3840,
      publicURL: (env.wizard_public_url || "").trim().replace(/\/$/, ""),
      password: env.wizard_password || "",
      bridgeKey: randomBytes(32).toString("hex"),
      jwtSecret: randomBytes(32).toString("hex"),
      strmToken: randomBytes(32).toString("hex"),
      exports: share("emby"),
      backups: share("backups"),
    });
    await writeJSON(paths.config, current);
  }
  for (const folder of ["miyabi", "mdcz", "logs"])
    await mkdir(join(paths.data, folder), { recursive: true, mode: 0o700 });
  await access(current.exports);
  await access(current.backups);
  const base = join(paths.etc, "mdcz.json");
  try {
    await access(base);
  } catch (error) {
    if (error.code !== "ENOENT") throw error;
    await cp(join(paths.app, "defaults/mdcz.json"), base, { force: false, errorOnExist: true });
    await chmod(base, 0o600);
  }
  await syncBridgeConnection(paths, current);
  return current;
}

export function bridgeConnection(config) {
  const webPort = mdczWebPort(config);
  return config.externalMDCz
    ? { server_url: config.externalMDCz.serverURL.replace(/\/$/, ""), api_key: config.externalMDCz.apiKey }
    : {
        server_url: `http://127.0.0.1:${config.bridgePort}`,
        api_key: config.bridgeKey,
        ...(webPort ? { web_port: webPort } : {}),
      };
}

export function mdczWebPort(config) {
  return config.mdczConnectionMode === "managed" && !config.externalMDCz
    ? config.mdczWebPort
    : undefined;
}

export async function syncBridgeConnection(paths, config) {
  const connection = bridgeConnection(config);
  await writeJSON(join(paths.etc, "mdcz-connection.json"), {
    ...connection,
    generation: createHash("sha256").update(connection.server_url).digest("hex"),
  });
  if (config.javlibraryBrowser)
    await writeJSON(join(paths.etc, "javlibrary-browser.json"), {
      enabled: true,
      ...(config.javlibraryBrowser.connectBack
        ? { connect_back: true }
        : { server_url: config.javlibraryBrowser.serverURL.replace(/\/$/, "") }),
      api_token: config.javlibraryBrowser.apiToken,
    });
}

export function patchConfiguration(current, env) {
  const next = {
    ...current,
    ...(env.wizard_password
      ? {
          password: env.wizard_password,
          jwtSecret:
            env.wizard_password === current.password
              ? current.jwtSecret
              : randomBytes(32).toString("hex"),
        }
      : {}),
    ...(env.wizard_public_url
      ? { publicURL: env.wizard_public_url.trim().replace(/\/$/, "") }
      : {}),
  };
  if (env.wizard_mdcz_web_port) {
    next.mdczConnectionMode = "managed";
    next.mdczWebPort = Number(env.wizard_mdcz_web_port);
    delete next.externalMDCz;
  }
  return validate(next);
}

// Copy only while both processes are stopped. The manifest is written last;
// interrupted snapshots remain visibly incomplete and cannot look successful.
export async function backup(paths, config, reason) {
  const stamp = `${new Date().toISOString().replace(/[:.]/g, "-")}-${randomUUID()}`;
  const target = join(config.backups, `${reason}-${stamp}`);
  await mkdir(target, { mode: 0o700 });
  await cp(paths.etc, join(target, "etc"), { recursive: true, dereference: false });
  for (const name of ["miyabi", "mdcz"])
    await cp(join(paths.data, name), join(target, name), { recursive: true, dereference: false });
  await verifyPublicationBackup(target, paths.data);
  await writeJSON(join(target, "complete.json"), {
    version: 1,
    reason,
    engineRevision,
    created: new Date().toISOString(),
  });
  return target;
}

export function childEnvironment(paths, config) {
  const webPort = mdczWebPort(config);
  // Do not pass wizard secrets, inherited proxies or unrelated MIYABI_* values
  // to child processes. Credentials never appear in command-line arguments.
  const common = {
    PATH: `${join(paths.app, "runtime/bin")}:/usr/bin:/bin`,
    HOME: paths.data,
    LANG: "C.UTF-8",
    TZ: process.env.TZ || "Asia/Shanghai",
    NODE_ENV: "production",
    FONTCONFIG_FILE: join(paths.etc, "fonts.conf"),
    SSL_CERT_FILE: join(paths.app, "certs/ca-certificates.crt"),
    NODE_EXTRA_CA_CERTS: join(paths.app, "certs/ca-certificates.crt"),
  };
  return {
    miyabi: {
      ...common,
      MIYABI_LISTEN: `:${config.port}`,
      MIYABI_DATA_DIR: join(paths.data, "miyabi"),
      MIYABI_EMBY_DIR: config.exports,
      MIYABI_PUBLIC_URL: config.publicURL,
      MIYABI_ACCESS_PASSWORD: config.password,
      MIYABI_JWT_SECRET: config.jwtSecret,
      MIYABI_STRM_TOKEN: config.strmToken,
      ...(config.mdczConnectionMode === "managed"
        ? { MIYABI_MDCZ_MANAGED: join(paths.etc, "mdcz-connection.json") }
        : { MIYABI_MDCZ_BOOTSTRAP: join(paths.etc, "mdcz-connection.json") }),
      ...(config.javlibraryBrowser
        ? { MIYABI_JAVLIBRARY_BROWSER_CONFIG: join(paths.etc, "javlibrary-browser.json") }
        : {}),
    },
    bridge: {
      ...common,
      HOST: "127.0.0.1",
      PORT: String(config.bridgePort),
      MIYABI_MDCZ_DATA: join(paths.data, "mdcz"),
      MIYABI_MDCZ_CONFIG: join(paths.etc, "mdcz.json"),
      MIYABI_MDCZ_KEY: config.bridgeKey,
      ...(webPort ? {
        MIYABI_MDCZ_WEB_PORT: String(webPort),
        MIYABI_MDCZ_LEGACY_DATABASE: join(paths.data, "miyabi/miyabi.db"),
        MDCZ_HOME: join(paths.data, "mdcz/webui"),
        MDCZ_ADMIN_PASSWORD: config.password,
      } : {}),
    },
  };
}
