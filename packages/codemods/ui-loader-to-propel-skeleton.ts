/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

import type {
  API,
  ASTPath,
  FileInfo,
  ImportDeclaration,
  ImportSpecifier,
} from "jscodeshift";

/**
 * `Loader` from `@pace/ui` -> `Skeleton` from `@pace/propel/skeleton`.
 *
 * The two are the same component under two names: both render `animate-pulse` + `role="status"` around their children and expose an `.Item` taking `height`/`width`/`className` with an `"auto"` default, and the class expressions are identical. Skeleton additionally sets `data-slot` and an `aria-label`, which is additive — no selector in the repo targets the old markup (`plane-ui-loader` appears only on the deleted component's own `displayName`).
 *
 * Everything keys off the import source, never off the name. `Loader` is a name several app files define for themselves, and one of them (`packages/editor/src/components/editors/document/loader.tsx`) both defines a `DocumentContentLoader` and imports the @pace/ui `Loader`. A transform that matched `<Loader>` in JSX would rewrite first-party components in the files that never imported anything. The headlessui-v2-default-tags transform makes the same guard for the same reason.
 *
 * The AST is used only to locate character offsets; the replacement is spliced into the original text and the source string is returned instead of `root.toSource()`. Reprinting a mutated AST through recast is not byte-preserving — on a 531-file sweep in this repo it rewrote unrelated JSX in 7 files, because recast reprints a whole subtree when it cannot reconcile comment attachment and several files carry `// oxlint-disable-next-line` comments inside their JSX. Splicing keeps every hunk in the 80 files to the import lines and the bare identifier renames.
 *
 * Deliberately not done:
 *
 * - No merging into an existing `@pace/propel/skeleton` import, and no reordering. A file keeping other @pace/ui symbols gains a second import statement above the one it had; that is what the icons sweep does too, and both oxlint and oxfmt accept it.
 * - An aliased import (`Loader as X`) keeps its local name — the new import becomes `Skeleton as X` and no reference is touched. There is none in the repo today, but renaming references would otherwise have to prove the alias is free at every use site.
 * - A reference whose scope resolves to something other than the module-level import is left alone, which is what makes a shadowing `const Loader` inside a function safe.
 */
const FROM_SOURCE = "@pace/ui";
const TO_SOURCE = "@pace/propel/skeleton";
const FROM_NAME = "Loader";
const TO_NAME = "Skeleton";

type Splice = { start: number; end: number; text: string };

/** Babel records these; ast-types does not declare them on the node types. */
type Ranged = { start?: number; end?: number };

const rangeOf = (node: unknown): [number, number] | undefined => {
  const { start, end } = (node ?? {}) as Ranged;
  return typeof start === "number" && typeof end === "number"
    ? [start, end]
    : undefined;
};

export default function transform(file: FileInfo, api: API) {
  const j = api.jscodeshift;
  const root = j(file.source);

  const splices: Splice[] = [];
  /** Local names bound to @pace/ui's `Loader` that are safe to rename at their use sites. */
  const renamedLocals = new Set<string>();

  root
    .find(j.ImportDeclaration, { source: { value: FROM_SOURCE } })
    .forEach((path: ASTPath<ImportDeclaration>) => {
      const specifiers = path.node.specifiers ?? [];
      const index = specifiers.findIndex(
        (specifier) =>
          specifier.type === "ImportSpecifier" &&
          // ast-types types both names as `string | IdentifierKind`; only the string form can be compared.
          typeof specifier.imported?.name === "string" &&
          specifier.imported.name === FROM_NAME
      );
      if (index === -1) return;

      const specifier = specifiers[index] as ImportSpecifier;
      const specifierRange = rangeOf(specifier);
      const declarationRange = rangeOf(path.node);
      if (!specifierRange || !declarationRange) return;

      const localName =
        typeof specifier.local?.name === "string"
          ? specifier.local.name
          : FROM_NAME;
      // An alias only has to survive when it differs from both names; `Loader as Skeleton` already binds the target name.
      const binding =
        localName === FROM_NAME || localName === TO_NAME
          ? TO_NAME
          : `${TO_NAME} as ${localName}`;
      // `import type { Loader }` binds no runtime value; the replacement has to keep the modifier or the emitted import stops type-checking.
      const keyword =
        path.node.importKind === "type" ? "import type" : "import";
      const declaration = `${keyword} { ${binding} } from "${TO_SOURCE}";`;

      if (specifiers.length === 1) {
        // Nothing else came from @pace/ui, so the statement itself moves and the file loses the dependency.
        splices.push({
          start: declarationRange[0],
          end: declarationRange[1],
          text: declaration,
        });
      } else {
        // Cutting to the next specifier's start (or back from the previous one's end when this is the last) takes the separating comma and whitespace with it, whatever the formatting.
        const neighbour =
          index + 1 < specifiers.length
            ? rangeOf(specifiers[index + 1])
            : rangeOf(specifiers[index - 1]);
        if (!neighbour) return;
        const [cutStart, cutEnd] =
          index + 1 < specifiers.length
            ? [specifierRange[0], neighbour[0]]
            : [neighbour[1], specifierRange[1]];
        splices.push({ start: cutStart, end: cutEnd, text: "" });
        // Inserted at the `import` keyword, so a leading `// ui` style comment stays above both statements.
        splices.push({
          start: declarationRange[0],
          end: declarationRange[0],
          text: `${declaration}\n`,
        });
      }

      if (localName === FROM_NAME) renamedLocals.add(localName);
    });

  if (splices.length === 0) return file.source;

  // The scope hangs off Program, not off the File node the root path points at — `root.get().scope` is null, and comparing against null would have matched every unresolved identifier instead of none.
  const moduleScope = root.find(j.Program).paths()[0]?.scope;

  if (renamedLocals.size > 0 && moduleScope) {
    const rename = (path: ASTPath<{ name?: unknown }>) => {
      const name = path.node.name;
      if (typeof name !== "string" || !renamedLocals.has(name)) return;
      // The import specifier's own identifiers sit inside a range already being replaced.
      if (path.parent?.node?.type === "ImportSpecifier") return;
      // A JSX member expression's property (`Loader.Item` -> the `Item`) must not be touched.
      if (
        path.parent?.node?.type === "JSXMemberExpression" &&
        path.parent.node.property === path.node
      )
        return;
      if (path.scope?.lookup(name) !== moduleScope) return;
      const range = rangeOf(path.node);
      if (range)
        splices.push({ start: range[0], end: range[1], text: TO_NAME });
    };

    // One pass, not two: ast-types declares JSXIdentifier as a subtype of Identifier, so this collection already contains the JSX element names. Searching both types would splice every one of them twice.
    root.find(j.Identifier).forEach(rename);
  }

  // Back to front, so an earlier splice never invalidates a later offset.
  let output = file.source;
  for (const splice of splices.toSorted((a, b) => b.start - a.start)) {
    output =
      output.slice(0, splice.start) + splice.text + output.slice(splice.end);
  }

  return output;
}
