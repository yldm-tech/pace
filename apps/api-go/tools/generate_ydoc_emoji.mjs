/**
 * Dump the shortcode to emoji table the editor renders emoji nodes with.
 *
 * An emoji node stores only a shortcode; the character comes from a lookup against the extension's emoji list, which Plane configures to GitHub's set with the entries that have no character removed. So the character has to be looked up on the Go side too, and the table is data rather than behaviour.
 *
 * The lookup tries a shortcode against each entry's name and then its aliases, first match winning, so the table is flattened in that order and a key already claimed is left alone. Regenerate from the repository root:
 *
 *     pnpm install --filter @pace/editor...
 *     node apps/api-go/tools/generate_ydoc_emoji.mjs > apps/api-go/internal/ydoc/emoji.tsv
 */

import { loadEditorModule } from "./ydoc_bundle.mjs";

const { gitHubEmojis } = await loadEditorModule(process.cwd(), {
  source: `export { gitHubEmojis } from "@tiptap/extension-emoji";`,
});

const table = new Map();
for (const entry of gitHubEmojis) {
  if (!entry.emoji) continue;
  for (const key of [entry.name, ...(entry.shortcodes ?? [])]) {
    if (key && !table.has(key)) table.set(key, entry.emoji);
  }
}

const rows = [...table].map(([key, emoji]) => `${key}\t${emoji}`);
process.stdout.write(`${rows.join("\n")}\n`);
