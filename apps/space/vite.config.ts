import path from "node:path";
import * as dotenv from "dotenv";
import { reactRouter } from "@react-router/dev/vite";
import { defineConfig } from "vite";
import tsconfigPaths from "vite-tsconfig-paths";
import { joinUrlPath } from "@pace/utils";
import { NAMESPACES } from "@pace/i18n/constants/namespaces";

dotenv.config({ path: path.resolve(__dirname, ".env") });

// Expose only vars starting with VITE_
const viteEnv = Object.keys(process.env)
  .filter((k) => k.startsWith("VITE_"))
  .reduce<Record<string, string>>((a, k) => {
    a[k] = process.env[k] ?? "";
    return a;
  }, {});

const basePath = joinUrlPath(process.env.VITE_SPACE_BASE_PATH ?? "", "/") ?? "/";

export default defineConfig(() => ({
  base: basePath,
  define: {
    "process.env": JSON.stringify(viteEnv),
    // Stated rather than inherited: @pace/i18n falls back to every namespace when this is absent, but each app declaring its own eager set is what keeps the admin console's narrower one from looking like an accident.
    __PACE_I18N_EAGER_NAMESPACES__: JSON.stringify(NAMESPACES),
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
}));
