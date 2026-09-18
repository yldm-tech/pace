import path from "node:path";
import * as dotenv from "dotenv";
import { reactRouter } from "@react-router/dev/vite";
import { defineConfig } from "vite";
import tsconfigPaths from "vite-tsconfig-paths";
import { joinUrlPath } from "@pace/utils";
import { ADMIN_NAMESPACES } from "@pace/i18n/constants/namespaces";

dotenv.config({ path: path.resolve(__dirname, ".env") });

// Expose only vars starting with VITE_
const viteEnv = Object.keys(process.env)
  .filter((k) => k.startsWith("VITE_"))
  .reduce<Record<string, string>>((a, k) => {
    a[k] = process.env[k] ?? "";
    return a;
  }, {});

const basePath = joinUrlPath(process.env.VITE_ADMIN_BASE_PATH ?? "", "/") ?? "/";

export default defineConfig(() => ({
  base: basePath,
  define: {
    "process.env": JSON.stringify(viteEnv),
    // The console's screens only ever look up keys in the `admin` namespace, so it has no reason to download the other twenty-seven before its first paint. @pace/i18n reads this at init and falls back to every namespace when it is absent, which is what web and space rely on.
    __PACE_I18N_EAGER_NAMESPACES__: JSON.stringify(ADMIN_NAMESPACES),
  },
  build: {
    assetsInlineLimit: 0,
  },
  plugins: [reactRouter(), tsconfigPaths({ projects: [path.resolve(__dirname, "tsconfig.json")] })],
  resolve: {
    alias: {},
    dedupe: ["react", "react-dom"],
  },
  server: {
    host: "127.0.0.1",
  },
  // No SSR-specific overrides needed; alias resolves to ESM build
}));
