import { access } from "node:fs/promises";
import { DatabaseSync } from "node:sqlite";

// The WebUI takes over this switch along with the translation engine settings.
export async function readLegacyTranslation(file) {
  try {
    await access(file);
  } catch (error) {
    if (error.code === "ENOENT") return undefined;
    throw error;
  }
  const db = new DatabaseSync(file, { readOnly: true });
  try {
    if (!db.prepare("SELECT 1 FROM sqlite_schema WHERE type = 'table' AND name = 'settings'").get())
      return undefined;
    const row = db.prepare("SELECT value FROM settings WHERE key = ?").get("extensions.mdcz.v1");
    const value = row?.value;
    return value === undefined ? undefined : JSON.parse(Buffer.from(value).toString("utf8")).translate;
  } finally {
    db.close();
  }
}
