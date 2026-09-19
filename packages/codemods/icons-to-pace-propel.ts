/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

import type { API, FileInfo } from "jscodeshift";

/**
 * `@makeplane/propel/icons` -> `@pace/propel/icons`.
 *
 * `@pace/propel/icons` re-exports the whole external icon set, so the two paths now expose the same 900 bindings and this rewrite is an identity at runtime. The two name sets are disjoint (170 local `*Icon` exports against 737 external `*Outline`/`*Filled` ones, intersection empty), which is what makes renaming the source safe without touching a single specifier.
 *
 * The AST is used only to locate the module specifier; the replacement is then spliced into the original text and the source string is returned instead of `root.toSource()`. That is deliberate. Reprinting mutated ASTs through recast is not byte-preserving: on a first pass over this repo's 531 files it rewrote unrelated JSX in 7 of them, wrapping elements in parentheses, folding `<span />` onto the following text node and stripping blank lines between siblings — recast falls back to printing a whole subtree whenever it cannot reconcile comment attachment, and several of these files carry `// oxlint-disable-next-line` comments inside their JSX. Splicing guarantees the property this sweep needs: every hunk in 516 files is the single `from` line, so the diff is reviewable by reading the transform rather than the files.
 *
 * Deliberately not done:
 *
 * - No specifier is renamed, added or removed, and imports are not merged or reordered. A file that already imports from `@pace/propel/icons` simply ends up with two import statements from that path — which the codebase already does elsewhere (`issue-layouts/utils.tsx` splits its type and value icon imports exactly that way) and which oxlint and oxfmt both accept. Merging would reflow the specifier lists of 52 files for no behavioural gain.
 * - Three scopes keep the vendor path and are left out of the `icons-to-pace-propel` script's target list rather than filtered here, so running the transform over a single file still does the obvious thing: `packages/propel` is where the re-export lives and must not import its own public subpath, while `apps/admin` and `packages/utils` declare no `@pace/propel` dependency at all (and `@pace/propel` depends on `@pace/utils`, so giving utils the reverse edge would close a package cycle).
 */
const FROM_SOURCE = "@makeplane/propel/icons";
const TO_SOURCE = "@pace/propel/icons";

export default function transform(file: FileInfo, api: API) {
  const j = api.jscodeshift;
  const root = j(file.source);

  const ranges: [start: number, end: number][] = [];

  // Character offsets come from the Babel parse. ast-types does not declare them on the literal node, but they are what makes a textual splice safe: they delimit exactly the quoted specifier and nothing else.
  const collect = (node: { source?: unknown } | null) => {
    const source = node?.source as
      | { start?: number; end?: number }
      | null
      | undefined;
    if (typeof source?.start === "number" && typeof source.end === "number") {
      ranges.push([source.start, source.end]);
    }
  };

  const matchesSource = { source: { value: FROM_SOURCE } };

  root
    .find(j.ImportDeclaration, matchesSource)
    .forEach((path) => collect(path.node));
  // `export { X } from` and `export * from` carry a source in the same position as an import and would otherwise leave the old path behind in a barrel file. Dynamic `import()` is not handled: every one of the 574 references in the repo is a static `from "..."` clause, so a CallExpression branch would be untested code.
  root
    .find(j.ExportNamedDeclaration, matchesSource)
    .forEach((path) => collect(path.node));
  root
    .find(j.ExportAllDeclaration, matchesSource)
    .forEach((path) => collect(path.node));

  if (ranges.length === 0) return file.source;

  // Back to front, so an earlier splice never invalidates a later offset.
  let output = file.source;
  for (const [start, end] of ranges.toSorted((a, b) => b[0] - a[0])) {
    // The offsets include the quotes; reusing the opening one keeps a single-quoted specifier single-quoted.
    const quote = output[start];
    output =
      output.slice(0, start) + quote + TO_SOURCE + quote + output.slice(end);
  }

  return output;
}
