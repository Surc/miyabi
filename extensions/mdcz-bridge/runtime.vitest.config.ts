// Run this suite from apps/miyabi-bridge in the pinned MDCz checkout, using
// its frozen dependencies. The production alias map remains the single source.
import { defineConfig } from "vitest/config";
import bridge from "./vite.config";

export default defineConfig({
  root: import.meta.dirname,
  resolve: bridge.resolve,
  test: {
    environment: "node",
    include: ["runtime.test.ts"],
    restoreMocks: true,
    fileParallelism: false,
  },
});
