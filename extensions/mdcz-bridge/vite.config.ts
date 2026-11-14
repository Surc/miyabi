import { resolve } from "node:path";
import { defineConfig } from "vite";

const root = resolve(import.meta.dirname, "../..");
const entry = process.env.MIYABI_MDCZ_FNOS === "1" ? "fnos" : "bridge";
const external = [
  "@trpc/server",
  "@trpc/server/adapters/fastify",
  "better-sqlite3",
  "drizzle-orm",
  "drizzle-orm/better-sqlite3",
  "drizzle-orm/better-sqlite3/migrator",
  "drizzle-orm/sqlite-core",
  "fastify",
  "impit",
  "mediainfo.js",
  "sharp",
];
export default defineConfig({
  build: {
    lib: {
      entry: resolve(import.meta.dirname, entry === "fnos" ? "fnos.ts" : "main.ts"),
      formats: ["es"],
      fileName: () => `${entry}.js`,
    },
    outDir: resolve(import.meta.dirname, "dist"),
    emptyOutDir: entry === "bridge",
    target: "node24",
    ssr: true,
    // SSR builds use Rollup's entry naming, not build.lib.fileName.
    // Keep stable entry names for the standalone bridge and the native WebUI.
    rollupOptions: {
      external: [/^node:/, ...external],
      output: { entryFileNames: `${entry}.js` },
    },
  },
  resolve: {
    alias: [
      {
        find: /^@mdcz\/persistence$/,
        replacement: resolve(root, "packages/persistence/src/index.ts"),
      },
      { find: /^@mdcz\/runtime$/, replacement: resolve(root, "packages/runtime/src/index.ts") },
      { find: /^@mdcz\/runtime\/(.+)$/, replacement: resolve(root, "packages/runtime/src/$1") },
      { find: /^@mdcz\/shared\/(.+)$/, replacement: resolve(root, "packages/shared/$1") },
      { find: /^@mdcz\/shared$/, replacement: resolve(root, "packages/shared/index.ts") },
      {
        find: /^@mdcz\/media-store$/,
        replacement: resolve(root, "packages/media-store/src/index.ts"),
      },
    ],
  },
  ssr: { external, noExternal: true },
});
