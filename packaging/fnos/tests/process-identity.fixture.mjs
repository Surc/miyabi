// Only copied into temporary lifecycle test packages. This runner virtualizes
// process.kill/PIDs but exposes a different /proc namespace. Production always
// uses identity.mjs with boot ID/start ticks; those are tested independently.
export async function identity(pid) {
  if (!pid || pid < 2) return null;
  try {
    process.kill(pid, 0);
    return { pid, start: `fixture-${pid}`, boot: "fixture" };
  } catch (error) {
    if (error.code === "ESRCH") return null;
    throw error;
  }
}
export async function alive(record) {
  const current = record && (await identity(record.pid));
  return !!current && current.start === record.start && current.boot === record.boot;
}
