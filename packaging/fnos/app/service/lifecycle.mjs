import { access, writeFile } from "node:fs/promises";
import { constants } from "node:fs";
import { join } from "node:path";
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { createServer } from "node:net";
import {
  locations,
  initialize,
  load,
  patchConfiguration,
  writeJSON,
  backup,
  childEnvironment,
} from "./configuration.mjs";
import { start, stop, status, supervise } from "./processes.mjs";

const exec = promisify(execFile);
async function freePort(port, host) {
  await new Promise((resolve, reject) => {
    const server = createServer();
    server.once("error", () => reject(new Error(`端口 ${port} 已被占用或不可用`)));
    server.listen(port, host, () => server.close(resolve));
  });
}
const xml = (value) =>
  value.replaceAll("&", "&amp;").replaceAll("<", "&lt;").replaceAll('"', "&quot;");
async function preflight(paths, config) {
  await access(join(paths.app, "bin/miyabi"), constants.X_OK);
  await access(join(paths.app, "certs/ca-certificates.crt"));
  await access(join(paths.app, "fonts/NotoSansCJK-Regular.ttc"));
  await writeFile(
    join(paths.etc, "fonts.conf"),
    `<?xml version="1.0"?><!DOCTYPE fontconfig SYSTEM "fonts.dtd"><fontconfig><dir>${xml(join(paths.app, "fonts"))}</dir><cachedir>${xml(join(paths.data, "font-cache"))}</cachedir></fontconfig>`,
    { mode: 0o600 },
  );
  try {
    await exec(join(paths.app, "runtime/bin/node"), [join(paths.app, "service/native-probe.mjs")], {
      env: { ...childEnvironment(paths, config).bridge, TRIM_APPDEST: paths.app },
      timeout: 20_000,
      maxBuffer: 32_768,
    });
  } catch {
    throw new Error("内置 Node/图像/网络依赖不兼容，请使用已验证的 J4125 原生包");
  }
}
async function main(action, paths) {
  if (action === "supervise") return supervise(paths, await load(paths));
  if (action === "status") return (await status(paths, await load(paths))) ? 0 : 3;
  if (action === "stop") {
    await stop(paths);
    return;
  }
  if (action === "initialize" || action === "verify") {
    const config = await initialize(paths);
    await preflight(paths, config);
    return;
  }
  const config = await load(paths);
  if (action === "validate-configuration") {
    patchConfiguration(config, process.env);
    return;
  }
  if (action === "start") {
    if (await status(paths, config)) return;
    await stop(paths);
    await preflight(paths, config);
    await freePort(config.port, "0.0.0.0");
    await freePort(config.bridgePort, "127.0.0.1");
    return start(paths, config);
  }
  if (action === "configure") {
    const next = patchConfiguration(config, process.env);
    const wasRunning = await status(paths, config);
    await stop(paths);
    await writeJSON(paths.config, next);
    if (wasRunning) {
      try {
        await start(paths, next);
      } catch (error) {
        await stop(paths);
        await writeJSON(paths.config, config);
        await start(paths, config);
        throw error;
      }
    }
    return;
  }
  if (action === "upgrade-before" || action === "uninstall-before") {
    await stop(paths);
    // Full application-state backup includes original artwork; exported STRM
    // and media are outside these private directories and are never deleted.
    await backup(paths, config, action === "upgrade-before" ? "upgrade" : "uninstall");
    return;
  }
  throw new Error("未知的应用管理操作");
}
try {
  process.exitCode = (await main(process.argv[2], locations())) || 0;
} catch (error) {
  // Never print process environments, raw child stderr or configuration values.
  const message = error.code ? "应用目录或文件操作失败，请检查权限、空间和配置" : error.message;
  if (process.env.TRIM_TEMP_LOGFILE)
    await writeFile(process.env.TRIM_TEMP_LOGFILE, message).catch(() => {});
  console.error(message);
  process.exitCode = process.argv[2] === "status" ? 3 : 1;
}
