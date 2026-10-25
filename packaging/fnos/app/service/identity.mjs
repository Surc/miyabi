import { readFile } from "node:fs/promises";

// A PID alone is not ownership: Linux may reuse it after a crash or reboot.
// Keep procfs access separate from lifecycle control for isolated test runners.
export async function identity(pid, read = readFile) {
  try {
    const [raw, boot] = await Promise.all([
      read(`/proc/${pid}/stat`, "utf8"),
      read("/proc/sys/kernel/random/boot_id", "utf8"),
    ]);
    const fields = raw.slice(raw.lastIndexOf(")") + 2).split(" ");
    if (["Z", "X"].includes(fields[0])) return null;
    if (!/^\d+$/.test(fields[19]) || !boot.trim()) throw new Error("无法确认 Linux 进程身份");
    return { pid, start: fields[19], boot: boot.trim() };
  } catch (error) {
    if (["ENOENT", "ESRCH"].includes(error.code)) return null;
    throw error;
  }
}
export async function alive(record, read = readFile) {
  if (!record || !Number.isInteger(record.pid) || record.pid < 2) return false;
  const current = await identity(record.pid, read);
  return !!current && record.start === current.start && record.boot === current.boot;
}
