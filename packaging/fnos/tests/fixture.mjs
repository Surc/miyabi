import { mkdtemp, mkdir, writeFile, rm } from "node:fs/promises";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { locations, initialize } from "../app/service/configuration.mjs";
import { stop, state } from "../app/service/processes.mjs";

export async function fixture(t) {
  const root = await mkdtemp(join(tmpdir(), "miyabi-fpk-"));
  const env = {
    TRIM_APPDEST: join(root, "app"),
    TRIM_PKGETC: join(root, "etc"),
    TRIM_PKGVAR: join(root, "var"),
    TRIM_DATA_SHARE_PATHS: ["emby", "backups"].map((x) => join(root, "miyabi-native", x)).join(":"),
    wizard_port: "18080",
    wizard_public_url: "http://nas.test:18080",
    wizard_password: "test-password",
  };
  await mkdir(join(env.TRIM_APPDEST, "defaults"), { recursive: true });
  await writeFile(join(env.TRIM_APPDEST, "defaults/mdcz.json"), "{}");
  for (const path of env.TRIM_DATA_SHARE_PATHS.split(":")) await mkdir(path, { recursive: true });
  const paths = locations(env);
  t.after(async () => {
    if (await state(paths)) {
      const copied = await import("file://" + join(paths.app, "service/processes.mjs"));
      await copied.stop(paths);
    } else await stop(paths);
    await rm(root, { recursive: true, force: true });
  });
  return { root, paths, env, config: await initialize(paths, env) };
}
