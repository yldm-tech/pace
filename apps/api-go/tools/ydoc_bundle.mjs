/**
 * Load a module from `packages/editor/src` in a plain Node process.
 *
 * The requested entry point is bundled from source with the workspace UI packages replaced by a proxy
 * that answers any named import. Nothing replaced is ever called: building a schema reads each
 * extension's name, attributes and renderers, and never renders a node view, so pulling in a component
 * library to get at a schema would cost a build and buy nothing.
 *
 * This used to say that @plane/propel could not compile at all, which was true when it was written --
 * its icon picker imported a directory that existed in no commit. That was fixed; the proxy stays
 * because the reason above outlived the reason it was born with.
 */

import { createRequire } from "node:module";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

export const editorSource = (root) => path.join(root, "packages/editor/src");

// Yjs picks a random client id for every document it creates, and lib0 draws it from the platform's CSPRNG, so the same input produces different update bytes on every run. That would make a generated fixture unreviewable and undiffable in CI, so the randomness is replaced by a counter. It only ever feeds client ids and generated element ids — never the conversion the fixture exists to pin down.
const DETERMINISTIC_RANDOM = `
  let counter = 0;
  export const getRandomValues = (array) => { for (let i = 0; i < array.length; i++) array[i] = ++counter; return array; };
  export const subtle = undefined;
`;

const stubWorkspaceUI = (editorSrc, deterministic) => ({
  name: "stub-workspace-ui",
  setup(build) {
    if (deterministic) {
      build.onResolve({ filter: /^lib0\/webcrypto$/ }, () => ({ path: "random", namespace: "deterministic" }));
      build.onLoad({ filter: /.*/, namespace: "deterministic" }, () => ({ contents: DETERMINISTIC_RANDOM, loader: "js" }));
    }
    build.onResolve({ filter: /^@plane\/(propel|hooks)(\/.*)?$/ }, () => ({ path: "stub", namespace: "stub" }));
    build.onResolve({ filter: /^@\// }, (args) => {
      const base = path.join(editorSrc, args.path.slice(2));
      const candidates = [base, `${base}.ts`, `${base}.tsx`, path.join(base, "index.ts"), path.join(base, "index.tsx")];
      const hit = candidates.find((candidate) => fs.existsSync(candidate) && fs.statSync(candidate).isFile());
      return hit ? { path: hit } : null;
    });
    build.onLoad({ filter: /.*/, namespace: "stub" }, () => ({
      // CommonJS, so esbuild's interop lets any named import resolve rather than failing the build.
      contents:
        "const handler={get:()=>new Proxy(function(){},handler),apply:()=>null,construct:()=>({})};" +
        "module.exports = new Proxy(function(){},handler);",
      loader: "js",
    }));
  },
});

/**
 * Bundle and load a module. Pass `{ entry }` for a path under `packages/editor/src`, or `{ source }` for a snippet that imports from there — the snippet resolves `@/` and relative paths as if it sat in that directory. Pass `{ deterministic: true }` to make the loaded module's randomness repeatable.
 */
export const loadEditorModule = async (root, { entry, source, deterministic = false }) => {
  const editorSrc = editorSource(root);
  if (!fs.existsSync(editorSrc)) throw new Error("run this from the repository root");

  // Resolved against the repository, not against this file: these tools are usually run from a worktree with no node_modules of its own.
  const require = createRequire(path.join(root, "package.json"));
  let esbuildEntry;
  try {
    esbuildEntry = require.resolve("esbuild");
  } catch {
    const store = path.join(root, "node_modules/.pnpm");
    const hit = fs.existsSync(store) ? fs.readdirSync(store).find((name) => name.startsWith("esbuild@")) : undefined;
    if (!hit) throw new Error("esbuild is not installed; run pnpm install --filter @plane/editor...");
    esbuildEntry = path.join(store, hit, "node_modules/esbuild/lib/main.js");
  }
  const esbuild = require(esbuildEntry);

  const outfile = path.join(fs.mkdtempSync(path.join(os.tmpdir(), "ydoc-")), "bundle.cjs");
  await esbuild.build({
    ...(entry
      ? { entryPoints: [path.join(editorSrc, entry)] }
      : { stdin: { contents: source, resolveDir: editorSrc, sourcefile: "schema-dump.ts", loader: "ts" } }),
    bundle: true,
    format: "cjs",
    platform: "node",
    outfile,
    plugins: [stubWorkspaceUI(editorSrc, deterministic)],
    external: ["sharp"],
    logLevel: "error",
    jsx: "automatic",
  });

  return require(outfile);
};
