import { readFile } from "node:fs/promises";
import { configurationBoundary } from "./adapter";
import { createBridge } from "./server.mjs";
import { createManagedAdapter } from "./configuration.mjs";

const directory = process.env.MIYABI_MDCZ_DATA || "/data";
// Configuration is mounted read-only. Never rewrite an upstream MDCz profile.
const configuration = JSON.parse(
  await readFile(process.env.MIYABI_MDCZ_CONFIG || "/config/mdcz.json", "utf8"),
);
const adapter = await createManagedAdapter(
  directory,
  configurationBoundary(directory, configuration),
);
const server = createBridge(adapter, process.env.MIYABI_MDCZ_KEY);
server.requestTimeout = 155_000;
server.headersTimeout = 10_000;
server.listen(Number(process.env.PORT || 3840), process.env.HOST || "0.0.0.0");
for (const event of ["SIGTERM", "SIGINT"] as const)
  process.on(event, () => {
    server.close();
    server.closeAllConnections();
  });
