import { defineConfig } from "tsdown";

export default defineConfig({
  entry: ["src/index.ts", "src/html.ts", "src/markdown.ts", "src/theme/index.ts"],
  format: ["esm"],
  dts: true,
  platform: "neutral",
  exports: true,
});
