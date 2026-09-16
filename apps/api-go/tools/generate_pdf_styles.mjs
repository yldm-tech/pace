/**
 * Dump the styles the PDF exporter lays a page out with.
 *
 * Every number and every colour in the export is a constant in the service's own stylesheet — the page padding, the space above a heading, the width of a blockquote's rule, the grey a table's border is. None of it is behaviour, so none of it is transcribed: it is read out of the stylesheet itself and the Go renderer works from the result.
 *
 * Run from the repository root:
 *
 *     pnpm install --filter live...
 *     node apps/api-go/tools/generate_pdf_styles.mjs > apps/api-go/internal/pdfdoc/styles.json
 */

import { createRequire } from "node:module";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

const root = process.cwd();
const require = createRequire(path.join(root, "package.json"));

const esbuildEntry = () => {
  try {
    return require.resolve("esbuild");
  } catch {
    const store = path.join(root, "node_modules/.pnpm");
    const hit = fs.readdirSync(store).find((name) => name.startsWith("esbuild@"));
    if (!hit) throw new Error("esbuild is not installed");
    return path.join(store, hit, "node_modules/esbuild/lib/main.js");
  }
};
const esbuild = require(esbuildEntry());

const liveSrc = path.join(root, "apps/live/src");
const outfile = path.join(fs.mkdtempSync(path.join(os.tmpdir(), "pdfstyles-")), "styles.cjs");

await esbuild.build({
  stdin: {
    contents: `
      export { pdfStyles } from "@/lib/pdf/styles";
      export * as colors from "@/lib/pdf/colors";
    `,
    resolveDir: liveSrc,
    sourcefile: "styles-dump.ts",
    loader: "ts",
  },
  bundle: true,
  format: "cjs",
  platform: "node",
  outfile,
  logLevel: "error",
  plugins: [
    {
      name: "resolve-live-alias",
      setup(build) {
        build.onResolve({ filter: /^@\// }, (args) => {
          const base = path.join(liveSrc, args.path.slice(2));
          for (const candidate of [base, `${base}.ts`, `${base}.tsx`, path.join(base, "index.ts")]) {
            if (fs.existsSync(candidate) && fs.statSync(candidate).isFile()) return { path: candidate };
          }
          return null;
        });
        // The stylesheet is built through react-pdf's StyleSheet.create, which is the identity on a plain object. The renderer itself is never loaded.
        build.onResolve({ filter: /^@react-pdf\/renderer$/ }, () => ({ path: "stub", namespace: "react-pdf" }));
        build.onLoad({ filter: /.*/, namespace: "react-pdf" }, () => ({
          contents: "module.exports = { StyleSheet: { create: (styles) => styles } };",
          loader: "js",
        }));
      },
    },
  ],
});

const { pdfStyles, colors } = require(outfile);

process.stdout.write(`${JSON.stringify({ styles: pdfStyles, colors: { ...colors } }, null, 2)}\n`);
