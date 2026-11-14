import { open, rename, stat } from "node:fs/promises";
import { join } from "node:path";
import { spawn } from "node:child_process";
import { setTimeout as delay } from "node:timers/promises";
import { readJSON, writeJSON, childEnvironment, bridgeConnection, mdczWebPort, syncBridgeConnection } from "./configuration.mjs";

import { bridgePreflight } from "./preflight.mjs";
import { engineRevision, protocol, configurationVersion, bridgeVersion, requiredCapabilities } from "./compatibility.mjs";
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
  while (
    Date.now() < deadline &&
    (await Promise.all(records.map((record) => alive(record)))).some(Boolean)
  )
    await delay(100);
  await signal("SIGKILL");
  for (let attempt = 0; attempt < 20; attempt++) {
    if (!(await Promise.all(records.map((record) => alive(record)))).some(Boolean)) return;
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
// Main availability and optional scraping capability are deliberately separate.
export async function healthy(config) {
  try {
    return (await json(`http://127.0.0.1:${config.port}/api/auth/config`)).enabled === true;
  } catch {
    return false;
  }
}
export async function bridgeHealthy(config) {
  try {
    const connection = bridgeConnection(config);
    const health = await json(`${connection.server_url}/v1/health`, connection.api_key);
    return health.protocol === protocol && health.revision === engineRevision &&
      health.configuration_version === configurationVersion &&
      typeof health.configuration_id === "string" && health.configuration_id.length > 0 &&
      health.bridge_version === bridgeVersion && Array.isArray(health.capabilities) &&
      requiredCapabilities.every((name) => health.capabilities?.includes(name));
  } catch {
    return false;
  }
}
export async function status(paths, config) {
  const saved = await state(paths);
  // Old state files have unnamed records in bridge/main order. Do not signal
  // processes based on a role alone: alive() still verifies boot/start identity.
  const named = saved?.children?.some((record) => record.name);
  const main = named ? saved.children.find((record) => record.name === "miyabi") :
    saved?.children?.[config.externalMDCz ? 0 : 1];
  return !!saved && saved.phase === "running" && !!main &&
    (await alive(saved.supervisor)) && (await alive(main)) && (await healthy(config));
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

// The command wrapper holds flock for start/stop/configuration. Only the main
// process defines application availability. The optional bridge has a bounded
// restart budget; neither its crash nor failed native probe terminates Miyabi.
export const restartPolicy = Object.freeze({
  attempts: 5,
  firstDelay: 1000,
  maximumDelay: 8000,
  stablePeriod: 60_000,
});
export function restartDelay(failures) {
  return failures >= restartPolicy.attempts ? null :
    Math.min(restartPolicy.firstDelay * 2 ** (failures - 1), restartPolicy.maximumDelay);
}

export async function supervise(paths, config) {
  const file = join(paths.run, "processes.json");
  const saved = {
    supervisor: await identity(process.pid), children: [], phase: "starting",
    bridge: { phase: config.externalMDCz ? "external" : "starting", attempts: 0 },
  };
  const exited = Promise.withResolvers();
  void exited.promise.catch(() => {});
  const controller = new AbortController();
  let stopping;
  let bridgeWork;
  let writes = Promise.resolve();
  const spawning = new Set();
  const persist = () => {
    const copy = structuredClone(saved);
    writes = writes.then(() => writeJSON(file, copy));
    return writes;
  };
  const shutdown = () => {
    if (!stopping) {
      controller.abort(); // Cancels backoff and an in-progress native probe.
      stopping = (async () => {
        let failure;
        saved.phase = "stopping";
        try { await persist(); } catch (error) { failure = error; }
        // A signal may arrive between spawn and recording the child's identity.
        // Join those launches before terminating; no new launch starts on abort.
        await Promise.allSettled([...spawning]);
        try { await terminate(saved.children); } catch (error) { failure ||= error; }
        await bridgeWork?.catch((error) => { failure ||= error; });
        saved.phase = "stopped";
        saved.bridge.phase = "stopped";
        try { await persist(); } catch (error) { failure ||= error; }
        if (failure) throw failure;
        exited.resolve();
      })().catch((error) => exited.reject(error));
    }
    return stopping;
  };
  process.on("SIGTERM", shutdown);
  process.on("SIGINT", shutdown);
  const env = childEnvironment(paths, config);
  const launch = (name, program, args) => {
    const work = (async () => {
      if (controller.signal.aborted) return null;
      const output = await logFile(paths, name);
      try {
        if (controller.signal.aborted) return null;
        const child = spawn(program, args, {
          cwd: name === "bridge" ? join(paths.app, "bridge") : paths.app,
          env: env[name], stdio: ["ignore", output.fd, output.fd],
        });
        const started = Promise.withResolvers();
        const ended = Promise.withResolvers();
        child.once("spawn", started.resolve);
        child.once("error", (error) => { started.reject(error); ended.resolve(); });
        child.once("exit", ended.resolve);
        if (name === "miyabi") child.once("exit", () => { void shutdown(); });
        let record;
        try {
          await started.promise;
          const processIdentity = await identity(child.pid);
          if (!processIdentity) throw new Error("子服务启动后立即退出");
          record = { ...processIdentity, name };
          saved.children.push(record);
          await persist();
          return { record, ended: ended.promise };
        } catch (error) {
          if (record) {
            await terminate([record]);
          } else if (child.pid && child.exitCode === null && child.signalCode === null) {
            // Identity acquisition itself can fail. This is the owned spawn
            // handle, never a PID from disk; dispose it before retry/rollback.
            child.kill("SIGTERM");
            await Promise.race([ended.promise, delay(1000)]);
            if (child.exitCode === null && child.signalCode === null) {
              child.kill("SIGKILL");
              await ended.promise;
            }
          }
          throw error;
        }
      } finally {
        await output.close();
      }
    })();
    spawning.add(work);
    void work.finally(() => spawning.delete(work)).catch(() => {});
    return work;
  };
  const bridgeLoop = async () => {
    let failures = 0;
    while (!controller.signal.aborted) {
      let worker;
      let healthySince;
      let reason = "preflight";
      try {
        saved.bridge = { phase: "starting", attempts: failures + 1 };
        await persist();
        await bridgePreflight(paths, config, controller.signal);
        if (controller.signal.aborted) return;
        const entry = mdczWebPort(config) ? "bridge/fnos.js" : "bridge/bridge.js";
        worker = await launch("bridge", join(paths.app, "runtime/bin/node"), [join(paths.app, entry)]);
        if (!worker || controller.signal.aborted) return;
        reason = "health";
        const deadline = Date.now() + 25_000;
        while (!controller.signal.aborted && Date.now() < deadline && await alive(worker.record)) {
          if (await bridgeHealthy(config)) {
            healthySince = Date.now();
            saved.bridge.phase = "running";
            await persist();
            break;
          }
          await delay(200, undefined, { signal: controller.signal });
        }
        if (!healthySince && !controller.signal.aborted) {
          await terminate([worker.record]);
          throw new Error("MDCz 健康检查失败");
        }
        reason = "process-exited";
        await worker.ended;
        if (await alive(worker.record)) await terminate([worker.record]);
        if (healthySince && Date.now() - healthySince >= restartPolicy.stablePeriod) failures = 0;
      } catch (error) {
        if (controller.signal.aborted) return;
        if (worker) await terminate([worker.record]);
        // Do not log raw spawn/probe errors or inherited process environments.
      } finally {
        if (worker && !(await alive(worker.record)))
          saved.children = saved.children.filter((record) => record !== worker.record);
      }
      if (controller.signal.aborted) return;
      failures++;
      const wait = restartDelay(failures);
      saved.bridge = { phase: wait === null ? "unavailable" : "backoff", attempts: failures, reason };
      await persist();
      if (wait === null) return;
      await delay(wait, undefined, { signal: controller.signal }).catch((error) => {
        if (!controller.signal.aborted) throw error;
      });
    }
  };
  try {
    await persist();
    const main = await launch("miyabi", join(paths.app, "bin/miyabi"), []);
    if (!config.externalMDCz && !controller.signal.aborted) {
      bridgeWork = bridgeLoop();
      // A lifecycle/state error is not an ordinary bridge outage. Stop safely if
      // identities cannot be persisted; the optional service must not be orphaned.
      void bridgeWork.catch(() => { void shutdown(); });
    }
    const deadline = Date.now() + 25_000;
    while (!controller.signal.aborted && Date.now() < deadline) {
      if (main && await alive(main.record) && await healthy(config)) {
        if (controller.signal.aborted) break;
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
  await syncBridgeConnection(paths, config);
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
