import { open, rename, stat } from "node:fs/promises";
import { join } from "node:path";
import { spawn } from "node:child_process";
import { setTimeout as delay } from "node:timers/promises";
import { readJSON, writeJSON, engineRevision, childEnvironment } from "./configuration.mjs";

import { identity, alive } from "./identity.mjs";
export { identity, alive } from "./identity.mjs";

export async function state(paths) {
  try {
    return await readJSON(join(paths.run, "processes.json"));
  } catch (error) {
    if (error.code === "ENOENT") return null;
    throw error;
  }
}
export async function terminate(records, grace = 10_000) {
  const signal = async (kind) => {
    for (const record of records)
      if (await alive(record)) {
        try {
          process.kill(record.pid, kind);
        } catch (error) {
          if (error.code !== "ESRCH") throw error;
        }
      }
  };
  await signal("SIGTERM");
  const deadline = Date.now() + grace;
  while (Date.now() < deadline && (await Promise.all(records.map(alive))).some(Boolean))
    await delay(100);
  await signal("SIGKILL");
  for (let attempt = 0; attempt < 20; attempt++) {
    if (!(await Promise.all(records.map(alive))).some(Boolean)) return;
    await delay(100);
  }
  throw new Error("进程尚未退出，暂不能升级或备份");
}
export async function stop(paths) {
  const saved = await state(paths);
  if (!saved) return;
  // Ask the supervisor to stop first, then recover any recorded orphan workers.
  await terminate([saved.supervisor], 12_000);
  await terminate(saved.children || []);
}
async function json(url, key) {
  const response = await fetch(url, {
    redirect: "error",
    signal: AbortSignal.timeout(1800),
    headers: key ? { Authorization: `Bearer ${key}` } : {},
  });
  if (!response.ok) throw new Error("服务健康检查失败");
  return response.json();
}
export async function healthy(config) {
  try {
    const [main, bridge] = await Promise.all([
      json(`http://127.0.0.1:${config.port}/api/auth/config`),
      json(`http://127.0.0.1:${config.bridgePort}/v1/health`, config.bridgeKey),
    ]);
    return (
      main.enabled === true &&
      bridge.protocol === 1 &&
      bridge.revision === engineRevision &&
      bridge.configuration_version === 1 &&
      !!bridge.configuration_id
    );
  } catch {
    return false;
  }
}
export async function status(paths, config) {
  const saved = await state(paths);
  return (
    !!saved &&
    saved.phase === "running" &&
    saved.children?.length === 2 &&
    (await Promise.all([saved.supervisor, ...saved.children].map(alive))).every(Boolean) &&
    (await healthy(config))
  );
}

async function logFile(paths, name) {
  const file = join(paths.data, "logs", `${name}.log`);
  try {
    if ((await stat(file)).size > 5 * 1024 * 1024) await rename(file, `${file}.1`);
  } catch (error) {
    if (error.code !== "ENOENT") throw error;
  }
  return open(file, "a", 0o600);
}

// The command wrapper holds flock for start/stop/configuration. A supervisor
// owns both children, and stops the pair on either exit instead of reporting a
// half-running application. No global killall or pidfile-only signals.
export async function supervise(paths, config) {
  const file = join(paths.run, "processes.json");
  const saved = { supervisor: await identity(process.pid), children: [], phase: "starting" };
  const exited = Promise.withResolvers();
  // Shutdown can fail before startup reaches the wait below; retain its error
  // without an early unhandled rejection interrupting worker cleanup.
  void exited.promise.catch(() => {});
  let stopping;
  let writes = Promise.resolve();
  const persist = () => {
    const copy = structuredClone(saved);
    writes = writes.then(() => writeJSON(file, copy));
    return writes;
  };
  const shutdown = () => {
    if (!stopping)
      stopping = (async () => {
        let failure;
        saved.phase = "stopping";
        try {
          await persist();
        } catch (error) {
          failure = error;
        }
        // Disk/permission errors must never bypass stopping the owned workers.
        try {
          await terminate(saved.children);
        } catch (error) {
          failure ||= error;
        }
        saved.phase = "stopped";
        try {
          await persist();
        } catch (error) {
          failure ||= error;
        }
        if (failure) throw failure;
        exited.resolve();
      })().catch((error) => exited.reject(error));
    return stopping;
  };
  process.on("SIGTERM", shutdown);
  process.on("SIGINT", shutdown);
  await persist();
  const env = childEnvironment(paths, config);
  try {
    for (const [name, program, args] of [
      ["bridge", join(paths.app, "runtime/bin/node"), [join(paths.app, "bridge/bridge.js")]],
      ["miyabi", join(paths.app, "bin/miyabi"), []],
    ]) {
      if (stopping) break;
      const output = await logFile(paths, name);
      const child = spawn(program, args, {
        cwd: paths.app,
        env: env[name],
        stdio: ["ignore", output.fd, output.fd],
      });
      const started = Promise.withResolvers();
      child.once("spawn", started.resolve);
      child.once("error", started.reject);
      child.once("exit", () => {
        void shutdown();
      });
      try {
        await started.promise;
        const record = await identity(child.pid);
        if (!record) throw new Error("子服务启动后立即退出");
        saved.children.push(record);
        await persist();
        // An earlier worker can exit while this spawn is completing.
        if (stopping) await terminate([record]);
      } finally {
        await output.close();
      }
    }
    const deadline = Date.now() + 25_000;
    while (!stopping && Date.now() < deadline) {
      if (saved.children.length === 2 && (await healthy(config))) {
        if (stopping) break;
        saved.phase = "running";
        await persist();
        return await exited.promise;
      }
      await delay(200);
    }
    await shutdown();
    await exited.promise;
    throw new Error("服务启动失败，请查看应用日志");
  } catch (error) {
    await shutdown();
    throw error;
  } finally {
    process.off("SIGTERM", shutdown);
    process.off("SIGINT", shutdown);
  }
}

export async function start(paths, config) {
  if (await status(paths, config)) return;
  await stop(paths);
  const log = await logFile(paths, "supervisor");
  const child = spawn(
    join(paths.app, "runtime/bin/node"),
    [join(paths.app, "service/lifecycle.mjs"), "supervise"],
    {
      cwd: paths.app,
      detached: true,
      stdio: ["ignore", log.fd, log.fd],
      env: {
        PATH: "/usr/bin:/bin",
        TRIM_APPDEST: paths.app,
        TRIM_PKGETC: paths.etc,
        TRIM_PKGVAR: paths.data,
      },
    },
  );
  const started = Promise.withResolvers();
  child.once("spawn", started.resolve);
  child.once("error", started.reject);
  try {
    await started.promise;
  } finally {
    await log.close();
  }
  const owner = await identity(child.pid);
  child.unref();
  const deadline = Date.now() + 30_000;
  while (Date.now() < deadline && (await alive(owner))) {
    if (await status(paths, config)) return;
    await delay(200);
  }
  await terminate([owner]);
  await stop(paths);
  throw new Error("启动失败，请检查端口、依赖和应用日志");
}
