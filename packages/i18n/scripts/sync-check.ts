/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

// Usage:
//   tsx packages/i18n/scripts/sync-check.ts          # Report only
//   tsx packages/i18n/scripts/sync-check.ts --ci     # Exit 1 if issues found

import fs from "node:fs";
import path from "node:path";
import type { LocaleData } from "./lib/locale-io.js";
import { LOCALES_DIR, listLocales, loadLocale } from "./lib/locale-io.js";
import { ADMIN_NAMESPACES } from "../src/constants/namespaces.js";

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/** Format a number with commas (e.g. 7712 -> "7,712"). */
function fmt(n: number): string {
  return n.toLocaleString("en-US");
}

// ---------------------------------------------------------------------------
// Checks
// ---------------------------------------------------------------------------

interface CollisionEntry {
  key: string;
  files: string[];
}

/** Cross-namespace collision check: same flattened key in multiple namespace files. */
function findCollisions(localeData: LocaleData): CollisionEntry[] {
  const keyToFiles = new Map<string, string[]>();
  for (const ns of localeData.namespaces) {
    for (const key of ns.keys) {
      const existing = keyToFiles.get(key);
      if (existing) {
        existing.push(`${ns.name}.json`);
      } else {
        keyToFiles.set(key, [`${ns.name}.json`]);
      }
    }
  }

  const collisions: CollisionEntry[] = [];
  for (const [key, files] of keyToFiles) {
    if (files.length > 1) {
      collisions.push({ key, files });
    }
  }
  return collisions.toSorted((a, b) => a.key.localeCompare(b.key));
}

interface PathConflict {
  leaf: string;
  branch: string;
}

/** Path conflict check: a key is both a leaf AND a prefix of another key. */
function findPathConflicts(localeData: LocaleData): PathConflict[] {
  const allKeysArray = [...localeData.allKeys].toSorted();
  const conflicts: PathConflict[] = [];

  // Build a set of all prefixes used in the keys
  const prefixes = new Set<string>();
  for (const key of allKeysArray) {
    const parts = key.split(".");
    for (let i = 1; i < parts.length; i++) {
      prefixes.add(parts.slice(0, i).join("."));
    }
  }

  // A conflict exists when a leaf key is also a prefix
  for (const key of allKeysArray) {
    if (prefixes.has(key)) {
      // Find one example of a key that extends this prefix
      const extending = allKeysArray.find((k) => k.startsWith(key + "."));
      if (extending) {
        conflicts.push({ leaf: key, branch: extending });
      }
    }
  }

  return conflicts;
}

interface LocaleComparison {
  locale: string;
  totalKeys: number;
  missingKeys: string[];
  staleKeys: string[];
  coverage: number; // 0-100
}

function compareToEnglish(enKeys: Set<string>, other: LocaleData): LocaleComparison {
  const missingKeys: string[] = [];
  const staleKeys: string[] = [];

  for (const key of enKeys) {
    if (!other.allKeys.has(key)) {
      missingKeys.push(key);
    }
  }

  for (const key of other.allKeys) {
    if (!enKeys.has(key)) {
      staleKeys.push(key);
    }
  }

  const coverage = enKeys.size > 0 ? ((enKeys.size - missingKeys.length) / enKeys.size) * 100 : 100;

  return {
    locale: other.locale,
    totalKeys: other.allKeys.size,
    missingKeys: missingKeys.toSorted(),
    staleKeys: staleKeys.toSorted(),
    coverage,
  };
}

// ---------------------------------------------------------------------------
// Eager-namespace reachability
// ---------------------------------------------------------------------------

interface EagerNamespaceScope {
  app: string;
  dir: string;
  namespaces: readonly string[];
}

// Apps that download fewer than all namespaces before their first render. Anything absent here loads every namespace and cannot go out of range.
const EAGER_NAMESPACE_SCOPES: EagerNamespaceScope[] = [
  { app: "admin", dir: path.resolve(LOCALES_DIR, "../../../../apps/admin"), namespaces: ADMIN_NAMESPACES },
];

interface OutOfScopeKey {
  key: string;
  namespaces: string[];
  file: string;
}

const SOURCE_EXTENSIONS = new Set([".ts", ".tsx"]);
const SKIPPED_DIRECTORIES = new Set(["node_modules", "build", "dist", ".react-router", ".turbo"]);

function collectSourceFiles(dir: string, out: string[] = []): string[] {
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    if (entry.isDirectory()) {
      if (SKIPPED_DIRECTORIES.has(entry.name)) continue;
      collectSourceFiles(path.join(dir, entry.name), out);
    } else if (SOURCE_EXTENSIONS.has(path.extname(entry.name))) {
      out.push(path.join(dir, entry.name));
    }
  }
  return out;
}

/**
 * Finds translation keys an app references but will not have loaded.
 *
 * Narrowing the eager namespace set is what keeps the admin console from downloading twenty-seven namespaces it has no screens for, but it removes the safety net that made any key resolve from anywhere: i18next answers an unloaded lookup with the key itself, silently, so the failure would be a raw `admin.page.titles.general` painted into the UI rather than an error. This turns that into a build failure.
 *
 * Every double-quoted string literal in the app is considered, not just the arguments of `t(...)`. Keys reach `t` indirectly often enough -- through a label map, a strength-message table, a `labelKey` prop -- that matching on the call site would miss exactly the cases hardest to spot by eye.
 */
function findOutOfScopeKeys(enData: LocaleData, scope: EagerNamespaceScope): OutOfScopeKey[] {
  const eager = new Set(scope.namespaces);
  const keyOwners = new Map<string, string[]>();
  for (const ns of enData.namespaces) {
    for (const key of ns.keys) {
      const owners = keyOwners.get(key);
      if (owners) owners.push(ns.name);
      else keyOwners.set(key, [ns.name]);
    }
  }

  const found = new Map<string, OutOfScopeKey>();
  for (const file of collectSourceFiles(scope.dir)) {
    const source = fs.readFileSync(file, "utf-8");
    for (const [, literal] of source.matchAll(/"([^"\\\n]+)"/g)) {
      const owners = keyOwners.get(literal);
      if (!owners || owners.some((ns) => eager.has(ns))) continue;
      if (!found.has(literal)) {
        found.set(literal, { key: literal, namespaces: owners, file: path.relative(scope.dir, file) });
      }
    }
  }
  return [...found.values()].toSorted((a, b) => a.key.localeCompare(b.key));
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

function main() {
  const ciMode = process.argv.includes("--ci");

  // Discover all locale directories
  const localeDirs = listLocales();

  if (!localeDirs.includes("en")) {
    console.error("ERROR: English locale (en) not found in", LOCALES_DIR);
    process.exit(1);
  }

  // Load all locales
  const localeDataMap = new Map<string, LocaleData>();
  for (const locale of localeDirs) {
    localeDataMap.set(locale, loadLocale(locale));
  }

  const enData = localeDataMap.get("en")!;

  // Run checks
  const collisions = findCollisions(enData);
  const pathConflicts = findPathConflicts(enData);

  const comparisons: LocaleComparison[] = [];
  for (const locale of localeDirs) {
    if (locale === "en") continue;
    comparisons.push(compareToEnglish(enData.allKeys, localeDataMap.get(locale)!));
  }

  // -------------------------------------------------------------------------
  // Print report
  // -------------------------------------------------------------------------

  let hasFailure = false;

  console.log("\n=== Sync Check Results ===\n");
  console.log(`  en:    ${fmt(enData.allKeys.size)} keys (source)\n`);

  for (const comp of comparisons) {
    const status = comp.missingKeys.length === 0 ? "✓" : "✗";
    const missingStr = comp.missingKeys.length > 0 ? ` — ${fmt(comp.missingKeys.length)} missing` : "";
    const staleStr = comp.staleKeys.length > 0 ? `, ${fmt(comp.staleKeys.length)} stale` : "";
    console.log(
      `  ${status} ${comp.locale.padEnd(10)} ${fmt(comp.totalKeys)} keys (${comp.coverage.toFixed(1)}%)${missingStr}${staleStr}`
    );
    if (comp.missingKeys.length > 0) {
      hasFailure = true;
    }
  }

  // Cross-namespace collisions
  if (collisions.length > 0) {
    hasFailure = true;
    console.log("\nCROSS-NAMESPACE COLLISIONS:");
    for (const c of collisions) {
      console.log(`  ✗ "${c.key}" exists in: ${c.files.join(", ")}`);
    }
  }

  // Keys an app references but does not eagerly load
  for (const scope of EAGER_NAMESPACE_SCOPES) {
    const outOfScope = findOutOfScopeKeys(enData, scope);
    if (outOfScope.length === 0) continue;
    hasFailure = true;
    console.log(`\nKEYS OUTSIDE ${scope.app.toUpperCase()}'S EAGER NAMESPACES (${scope.namespaces.join(", ")}):`);
    for (const entry of outOfScope) {
      console.log(`  ✗ "${entry.key}" lives in ${entry.namespaces.join(", ")} — referenced by ${entry.file}`);
    }
    console.log(
      `  Either move the key into one of ${scope.app}'s namespaces, or add its namespace to the ${scope.app.toUpperCase()}_NAMESPACES list in src/constants/namespaces.ts.`
    );
  }

  // Path conflicts
  if (pathConflicts.length > 0) {
    hasFailure = true;
    console.log("\nPATH CONFLICTS:");
    for (const pc of pathConflicts) {
      console.log(`  ✗ "${pc.leaf}" is a leaf but "${pc.branch}" extends it`);
    }
  }

  // Missing keys detail
  const withMissing = comparisons.filter((c) => c.missingKeys.length > 0);
  if (withMissing.length > 0) {
    console.log("\n--- Missing Keys Detail ---\n");
    for (const comp of withMissing) {
      console.log(`${comp.locale} (${fmt(comp.missingKeys.length)} missing):`);
      const show = comp.missingKeys.slice(0, 20);
      for (const key of show) {
        console.log(`  - ${key}`);
      }
      if (comp.missingKeys.length > 20) {
        console.log(`  ... and ${fmt(comp.missingKeys.length - 20)} more`);
      }
      console.log();
    }
  }

  // Stale keys detail
  const withStale = comparisons.filter((c) => c.staleKeys.length > 0);
  if (withStale.length > 0) {
    console.log("--- Stale Keys Detail ---\n");
    for (const comp of withStale) {
      console.log(`${comp.locale} (${fmt(comp.staleKeys.length)} stale):`);
      const show = comp.staleKeys.slice(0, 20);
      for (const key of show) {
        console.log(`  - ${key}`);
      }
      if (comp.staleKeys.length > 20) {
        console.log(`  ... and ${fmt(comp.staleKeys.length - 20)} more`);
      }
      console.log();
    }
  }

  // CI exit code
  if (ciMode && hasFailure) {
    console.log("CI mode: exiting with code 1 due to missing keys, collisions, or path conflicts.");
    process.exit(1);
  }

  if (!hasFailure) {
    console.log("\nAll locales are in sync with English. No issues found.");
  }
}

try {
  main();
} catch (err) {
  console.error("Sync check failed:", err);
  process.exit(1);
}
