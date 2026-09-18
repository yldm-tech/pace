/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

// Usage:
//   tsx packages/i18n/scripts/unused-check.ts              # Report every unused key, always exit 0
//   tsx packages/i18n/scripts/unused-check.ts --summary     # Counts and per-namespace totals only
//   tsx packages/i18n/scripts/unused-check.ts --max=2400    # Exit 1 when the count climbs past a ratchet
//   tsx packages/i18n/scripts/unused-check.ts --strict      # Exit 1 if any key is unused (same as --max=0)
//
// Advisory by design. Unlike sync-check's other checks -- which compare one machine-readable set against another and are right or wrong -- this one infers intent from string literals, so it is a heuristic and can be wrong in both directions. Failing CI on it by default would break every PR that adds a label map on the day it is added, which is why the only exit-1 paths are opt-in.

import fs from "node:fs";
import path from "node:path";
import { loadLocale } from "./lib/locale-io.js";
import { REPO_ROOT, collectSourceFiles, collectStringLiterals, collectTemplatePrefixes } from "./lib/source-scan.js";

// Everything that can call `t()`. The Go API in apps/api cannot -- it has no access to the catalogue -- so it is covered only incidentally by living under apps/.
const SCAN_ROOTS = ["apps", "packages"];

/** Format a number with commas (e.g. 7712 -> "7,712"). */
function fmt(n: number): string {
  return n.toLocaleString("en-US");
}

interface ScanResult {
  /** Keys named by a string literal somewhere in the scanned tree. */
  referenced: Set<string>;
  /** Keys with no literal, but under a prefix some interpolated template builds at runtime. */
  dynamic: Map<string, string>;
  /** Keys with neither signal. */
  unused: string[];
  /** Template prefixes that actually cover at least one key, for the reader to audit. */
  usedPrefixes: Map<string, number>;
  filesScanned: number;
}

function scan(enKeys: Set<string>): ScanResult {
  const files: string[] = [];
  for (const root of SCAN_ROOTS) {
    collectSourceFiles(path.resolve(REPO_ROOT, root), files);
  }

  const referenced = new Set<string>();
  const prefixes = new Set<string>();

  for (const file of files) {
    const source = fs.readFileSync(file, "utf-8");
    for (const literal of collectStringLiterals(source)) {
      if (enKeys.has(literal)) referenced.add(literal);
    }
    for (const prefix of collectTemplatePrefixes(source)) {
      prefixes.add(prefix);
    }
  }

  // Two signals, deliberately ordered: a literal is direct evidence, a prefix only says "something in this subtree is assembled at runtime". Keeping them apart is what lets a reviewer treat the prefix-covered keys as off-limits without treating them as proven live.
  const dynamic = new Map<string, string>();
  const unused: string[] = [];
  const usedPrefixes = new Map<string, number>();

  const prefixList = [...prefixes];
  for (const key of [...enKeys].toSorted()) {
    if (referenced.has(key)) continue;
    const prefix = prefixList.find((p) => key.startsWith(p));
    if (prefix) {
      dynamic.set(key, prefix);
      usedPrefixes.set(prefix, (usedPrefixes.get(prefix) ?? 0) + 1);
    } else {
      unused.push(key);
    }
  }

  return { referenced, dynamic, unused, usedPrefixes, filesScanned: files.length };
}

/** Group keys by the namespace file they live in, so a whole dead namespace is visible as one line. */
function groupByNamespace(keys: string[], owners: Map<string, string[]>): Map<string, number> {
  const counts = new Map<string, number>();
  for (const key of keys) {
    for (const ns of owners.get(key) ?? []) {
      counts.set(ns, (counts.get(ns) ?? 0) + 1);
    }
  }
  return counts;
}

/** Group keys by their first dot-segment, which is how a dead feature (`sso.`, `automations.`) shows up. */
function groupByTopLevel(keys: string[]): Map<string, number> {
  const counts = new Map<string, number>();
  for (const key of keys) {
    const top = key.split(".")[0]!;
    counts.set(top, (counts.get(top) ?? 0) + 1);
  }
  return counts;
}

function parseMax(argv: string[]): number | undefined {
  if (argv.includes("--strict")) return 0;
  const flag = argv.find((a) => a.startsWith("--max="));
  if (!flag) return undefined;
  const value = Number.parseInt(flag.slice("--max=".length), 10);
  if (!Number.isInteger(value) || value < 0) {
    console.error(`ERROR: --max expects a non-negative integer, got "${flag.slice("--max=".length)}"`);
    process.exit(1);
  }
  return value;
}

function main() {
  const summaryOnly = process.argv.includes("--summary");
  const max = parseMax(process.argv);

  const enData = loadLocale("en");

  const keyOwners = new Map<string, string[]>();
  const namespaceTotals = new Map<string, number>();
  for (const ns of enData.namespaces) {
    namespaceTotals.set(ns.name, ns.keys.size);
    for (const key of ns.keys) {
      const owners = keyOwners.get(key);
      if (owners) owners.push(ns.name);
      else keyOwners.set(key, [ns.name]);
    }
  }

  const result = scan(enData.allKeys);
  const total = enData.allKeys.size;
  const share = total > 0 ? (result.unused.length / total) * 100 : 0;

  console.log("\n=== Unused Key Report ===\n");
  console.log(
    `  scanned      ${fmt(result.filesScanned)} source files under ${SCAN_ROOTS.map((r) => `${r}/`).join(", ")}`
  );
  console.log(`  en           ${fmt(total)} keys`);
  console.log(`  referenced   ${fmt(result.referenced.size)} named by a string literal`);
  console.log(`  dynamic      ${fmt(result.dynamic.size)} reachable only through a template-literal prefix`);
  console.log(`  UNUSED       ${fmt(result.unused.length)} (${share.toFixed(1)}% of en) — no literal, no prefix\n`);

  if (result.usedPrefixes.size > 0) {
    console.log("KEYS BUILT AT RUNTIME (counted as used — do not delete these without reading the call site):");
    for (const [prefix, count] of [...result.usedPrefixes].toSorted((a, b) => b[1] - a[1])) {
      console.log(`  ${String(count).padStart(5)}  ${prefix}\${...}`);
    }
    console.log();
  }

  const byNamespace = groupByNamespace(result.unused, keyOwners);
  if (byNamespace.size > 0) {
    console.log("UNUSED BY NAMESPACE:");
    for (const [ns, count] of [...byNamespace].toSorted((a, b) => b[1] - a[1])) {
      const nsTotal = namespaceTotals.get(ns) ?? 0;
      const pct = nsTotal > 0 ? ((count / nsTotal) * 100).toFixed(0) : "0";
      console.log(`  ${String(count).padStart(5)} of ${String(nsTotal).padEnd(5)} (${pct.padStart(3)}%)  ${ns}.json`);
    }
    console.log();
  }

  const byTopLevel = groupByTopLevel(result.unused);
  if (byTopLevel.size > 0) {
    console.log("UNUSED BY TOP-LEVEL PREFIX (>= 10 keys):");
    for (const [top, count] of [...byTopLevel].toSorted((a, b) => b[1] - a[1])) {
      if (count < 10) continue;
      console.log(`  ${String(count).padStart(5)}  ${top}`);
    }
    console.log();
  }

  if (!summaryOnly && result.unused.length > 0) {
    console.log("UNUSED KEYS:");
    for (const key of result.unused) {
      console.log(`  - ${key} (${(keyOwners.get(key) ?? []).join(", ")})`);
    }
    console.log();
  }

  console.log(
    "This is a heuristic, not a fact: a key is called unused when no string literal and no interpolated template prefix in apps/ or packages/ names it. It can be wrong in both directions — a key assembled from parts the prefix scan does not model reads as unused, and a key that happens to equal an unrelated literal reads as used. Before deleting anything, prefer whole feature subtrees whose feature does not exist in this repo over scattered individual keys, confirm the prefix appears nowhere in apps/, and remember that deleting from `en` means deleting the same key from all twenty locales under the rules in the translate skill."
  );

  if (max !== undefined && result.unused.length > max) {
    console.log(`\nFAIL: ${fmt(result.unused.length)} unused keys exceeds the --max=${fmt(max)} ratchet.`);
    process.exit(1);
  }
}

try {
  main();
} catch (err) {
  console.error("Unused check failed:", err);
  process.exit(1);
}
