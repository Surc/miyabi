// Backup completeness, not recovery arbitration: SQL receipts and live export
// hashes remain the Go publication manager's responsibility after restoration.
import { createHash } from "node:crypto";
import { createReadStream } from "node:fs";
import { lstat, readFile, readdir } from "node:fs/promises";
import { join } from "node:path";

const failure = () => new Error("发布恢复资料未完整包含在备份中；请保留原目录和未完成备份，不要卸载或删除数据");
const digest = (value) => createHash("sha256").update(value).digest("hex");
const hex = (value, length) => typeof value === "string" && new RegExp(`^[a-f0-9]{${length}}$`).test(value);
async function regularTree(path) {
  const info = await lstat(path);
  if (info.isDirectory()) {
    for (const name of await readdir(path)) await regularTree(join(path, name));
  } else if (!info.isFile()) {
    throw failure();
  }
}
async function json(path) {
  const info = await lstat(path);
  if (!info.isFile() || info.size > 64 * 1024 * 1024) throw failure();
  return JSON.parse(await readFile(path, "utf8"));
}
async function verifyBlob(path, expected) {
  if (!hex(expected, 64)) throw failure();
  const hash = createHash("sha256");
  for await (const chunk of createReadStream(path)) hash.update(chunk);
  if (hash.digest("hex") !== expected) throw failure();
}

export async function verifyPublicationBackup(target, sourceData) {
  // childEnvironment sets MIYABI_DATA_DIR to data/miyabi; app assembly installs
  // the recovery store at DataDir/export-publications. Keep the paths aligned.
  const app = join(target, "miyabi");
  const store = join(app, "export-publications");
  const originalStore = join(sourceData, "miyabi", "export-publications");
  try {
    if (!(await lstat(app)).isDirectory()) throw failure();
    try {
      if (!(await lstat(store)).isDirectory()) throw failure();
    } catch (error) {
      if (error.code === "ENOENT") return;
      throw error;
    }
    // cp deliberately preserves links. A link to a source stage is not a copy
    // of its recovery evidence, even if that target is still readable today.
    await regularTree(store);
    for (const name of await readdir(store)) {
      if (!name.endsWith(".json")) continue;
      const journal = await json(join(store, name));
      const id = journal?.operation_id;
      if (journal?.version !== 2 || !hex(id, 32) || name !== `${id}.json` ||
          journal.stage !== join(originalStore, "stages", id) ||
          typeof journal.root !== "string" || !hex(journal.root_id, 32) ||
          (journal.entries !== null && !Array.isArray(journal.entries))) throw failure();
      const stage = join(store, "stages", id);
      if (!(await lstat(stage)).isDirectory()) throw failure();
      const binding = await json(join(store, "roots", `${digest(journal.root)}.json`));
      if (binding.root !== journal.root || binding.identity !== journal.root_id) throw failure();
      for (const [index, entry] of (journal.entries || []).entries()) {
        for (const [field, directory] of [["before", "old"], ["after", "new"]]) {
          if (entry[field]) await verifyBlob(join(stage, directory, String(index).padStart(6, "0")), entry[field]);
        }
      }
    }
  } catch {
    // Neither journal paths nor file contents belong in management logs.
    throw failure();
  }
}
