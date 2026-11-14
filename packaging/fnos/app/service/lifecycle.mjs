import { writeFile } from "node:fs/promises";
import {
  locations,
  initialize,
  load,
  mdczWebPort,
  patchConfiguration,
  writeJSON,
  backup,
} from "./configuration.mjs";
import { mainPreflight, freePort } from "./preflight.mjs";
import { start, stop, status, supervise } from "./processes.mjs";

async function main(action, paths) {
  if (action === "supervise") return supervise(paths, await load(paths));
  if (action === "status") return (await status(paths, await load(paths))) ? 0 : 3;
  if (action === "stop") {
    await stop(paths);
    return;
  }
  if (action === "initialize" || action === "verify") {
    await initialize(paths);
    await mainPreflight(paths);
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
    await mainPreflight(paths);
    await freePort(config.port, "0.0.0.0");
    return start(paths, config);
  }
  if (action === "configure") {
    const next = patchConfiguration(config, process.env);
    const nextWebPort = mdczWebPort(next);
    if (nextWebPort && nextWebPort !== mdczWebPort(config))
      await freePort(nextWebPort, "0.0.0.0");
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
  if (action === "upgrade-before" || action === "uninstall-before" || action === "backup") {
    await stop(paths);
    // fnOS may call uninstall hooks while upgrading. Reuse the existing data
    // for both hooks; full snapshots are manual or precede an actual uninstall.
    const upgrading =
      process.env.TRIM_APP_STATUS === "UPGRADE" || !!process.env.TRIM_TEMP_UPGRADE_FOLDER;
    if (action === "backup" || (action === "uninstall-before" && !upgrading))
      await backup(paths, config, action === "backup" ? "manual" : "uninstall");
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
