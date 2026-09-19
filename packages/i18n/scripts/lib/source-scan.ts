/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

import fs from "node:fs";
import path from "node:path";
import { LOCALES_DIR } from "./locale-io.js";

/** Repo root, derived from the locales directory so the two move together if the package is ever relocated. */
export const REPO_ROOT = path.resolve(LOCALES_DIR, "../../../..");

// Only the extensions that can call `t()`. Widening this is tempting -- the Go API, the SQL migrations and the webmanifest all contain strings that happen to equal a key -- but every match measured outside .ts/.tsx was a database column (`avatar`, `workspaces`, `background_color`, `label_name`) rather than a translation lookup, so they would only launder dead keys into looking alive.
const SOURCE_EXTENSIONS = new Set([".ts", ".tsx"]);

const SKIPPED_DIRECTORIES = new Set([
  "node_modules",
  "build",
  "dist",
  ".react-router",
  ".turbo",
  ".next",
  ".vite",
  "coverage",
]);

// Written by `generate:types`. It spells every key in the catalogue as a string literal, so scanning it would report the entire catalogue as referenced. It is gitignored, but it exists on any machine that has run a build.
const SKIPPED_FILES = new Set(["keys.generated.ts"]);

/** Every .ts/.tsx file under `dir`, skipping build output and the generated key union. */
export function collectSourceFiles(dir: string, out: string[] = []): string[] {
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const entryPath = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      if (SKIPPED_DIRECTORIES.has(entry.name)) continue;
      // The locale JSON is the thing being checked, not a reference to it.
      if (entryPath === LOCALES_DIR) continue;
      collectSourceFiles(entryPath, out);
    } else if (SOURCE_EXTENSIONS.has(path.extname(entry.name)) && !SKIPPED_FILES.has(entry.name)) {
      out.push(entryPath);
    }
  }
  return out;
}

// Single-line string literals in all three quote styles, backticks only when the template has no interpolation. Anything containing a backslash is skipped rather than unescaped: no translation key contains one, so the only cost is ignoring literals that could never be keys.
const LITERAL_PATTERNS = [/"([^"\\\n]+)"/g, /'([^'\\\n]+)'/g, /`([^`\\\n$]+)`/g];

/**
 * Yields every string literal in a source file, not just the arguments of `t(...)`.
 *
 * Keys reach `t` indirectly often enough -- through a label map, a strength-message table, a `labelKey` prop -- that matching on the call site would miss exactly the cases hardest to spot by eye. The same resolution therefore backs both the eager-namespace check (which asks "is this key loaded?") and the unused-key report (which asks "does anyone name this key?"), so neither can be more naive than the other.
 *
 * Duplicates are yielded; callers are expected to be resolving against a key set anyway.
 */
export function* collectStringLiterals(source: string): Generator<string> {
  for (const pattern of LITERAL_PATTERNS) {
    for (const [, literal] of source.matchAll(pattern)) {
      yield literal;
    }
  }
}

// The literal head of an interpolated template, e.g. `admin.page.titles.${page}` -> "admin.page.titles.".
const TEMPLATE_PREFIX_PATTERN = /`([^`\\\n$]*)\$\{/g;

/**
 * Yields the static prefix of every interpolated template literal that could plausibly name a key subtree.
 *
 * A key built at runtime -- `translate(`admin.page.titles.${page}`)` in apps/admin/helpers/page-title.ts is the live example -- has no literal anywhere, so a literal scan alone would call the whole subtree dead. Requiring a dot in the prefix is what keeps this from matching every `${}` in the codebase; prefixes that name nothing are simply never consulted.
 */
export function* collectTemplatePrefixes(source: string): Generator<string> {
  for (const [, prefix] of source.matchAll(TEMPLATE_PREFIX_PATTERN)) {
    if (prefix.includes(".")) {
      yield prefix;
    }
  }
}
