import { access, writeFile } from "node:fs/promises";
import { constants } from "node:fs";
import { join } from "node:path";
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { createServer } from "node:net";
import { childEnvironment, mdczWebPort } from "./configuration.mjs";

const exec = promisify(execFile);
export async function freePort(port, host) {
  await new Promise((resolve, reject) => {
    const server = createServer();
    server.once("error", () => reject(new Error(`端口 ${port} 已被占用或不可用`)));
    server.listen(port, host, () => server.close(resolve));
  });
}

export async function mainPreflight(paths) {
  await access(join(paths.app, "bin/miyabi"), constants.X_OK);
  await access(join(paths.app, "certs/ca-certificates.crt"));
}

const xml = (value) =>
  value.replaceAll("&", "&amp;").replaceAll("<", "&lt;").replaceAll('"', "&quot;");

// A missing font/native bridge dependency must not take browsing, playback or
// settings offline. Run this probe in the separately supervised bridge branch.
export async function bridgePreflight(paths, config, signal) {
  await freePort(config.bridgePort, "127.0.0.1");
  const webPort = mdczWebPort(config);
  if (webPort) await freePort(webPort, "0.0.0.0");
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
      signal,
    });
  } catch (error) {
    if (signal.aborted) throw error;
    throw new Error("内置图像/网络依赖不兼容，MDCz 暂不可用");
  }
}
