import { resolve } from "node:path";
import { defineConfig } from "vite";

const root = resolve(import.meta.dirname, "../..");
const external = [
  "@trpc/server",
  "better-sqlite3",
  "drizzle-orm",
  "fastify",
  "impit",
  "mediainfo.js",
  "sharp",
];
export default defineConfig({
  build: {
    lib: {
      entry: resolve(import.meta.dirname, "main.ts"),
      formats: ["es"],
      fileName: () => "bridge.js",
    },
    outDir: resolve(import.meta.dirname, "dist"),
    emptyOutDir: true,
    target: "node24",
    ssr: true,
    // SSR builds use Rollup's entry naming, not build.lib.fileName.
    // Docker and the native package share this stable launch path.
    rollupOptions: {
      external: [/^node:/, ...external],
      output: { entryFileNames: "bridge.js" },
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
