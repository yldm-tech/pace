import { defineConfig } from "tsdown";

export default defineConfig({
  entry: ["src/index.ts", "src/constants/namespaces.ts"],
  format: ["esm"],
  dts: true,
  platform: "neutral",
  exports: true,
});
