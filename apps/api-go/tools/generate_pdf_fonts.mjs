/**
 * Unpack the fonts the PDF exporter draws with into a form a PDF writer can embed.
 *
 * The export registers six faces of Inter — regular, semibold and bold, each upright and italic — and the package ships them as WOFF. A WOFF file is an ordinary TrueType file with each table deflated and a different header in front, so unpacking one is reading the table directory, inflating each table and writing a TrueType header back.
 *
 * The same faces matter, not merely a similar font: how a line breaks depends on the width of every character, so a PDF laid out with different metrics is a differently paginated document.
 *
 * Run from the repository root:
 *
 *     pnpm install --filter live...
 *     node apps/api-go/tools/generate_pdf_fonts.mjs
 */

import { createRequire } from "node:module";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import zlib from "node:zlib";

const root = process.cwd();
const require = createRequire(path.join(root, "apps/live/package.json"));
const fontDir = path.join(path.dirname(require.resolve("@fontsource/inter/package.json")), "files");

// The six faces the exporter registers, and nothing else.
const FACES = [
  { file: "inter-latin-400-normal.woff", out: "inter-regular.ttf" },
  { file: "inter-latin-400-italic.woff", out: "inter-italic.ttf" },
  { file: "inter-latin-600-normal.woff", out: "inter-semibold.ttf" },
  { file: "inter-latin-600-italic.woff", out: "inter-semibold-italic.ttf" },
  { file: "inter-latin-700-normal.woff", out: "inter-bold.ttf" },
  { file: "inter-latin-700-italic.woff", out: "inter-bold-italic.ttf" },
];

// woffToTrueType rebuilds a TrueType file out of a WOFF one. The header is fixed width, then one directory entry per table; a table whose compressed length equals its real length was stored rather than deflated.
const woffToTrueType = (woff) => {
  if (woff.readUInt32BE(0) !== 0x774f4646) throw new Error("not a WOFF file");
  const flavour = woff.readUInt32BE(4);
  const tableCount = woff.readUInt16BE(12);

  const tables = [];
  for (let i = 0; i < tableCount; i++) {
    const entry = 44 + i * 20;
    const tag = woff.subarray(entry, entry + 4);
    const offset = woff.readUInt32BE(entry + 4);
    const compressedLength = woff.readUInt32BE(entry + 8);
    const originalLength = woff.readUInt32BE(entry + 12);
    const checksum = woff.readUInt32BE(entry + 16);
    const stored = woff.subarray(offset, offset + compressedLength);
    const data = compressedLength === originalLength ? stored : zlib.inflateSync(stored);
    if (data.length !== originalLength) throw new Error(`table ${tag} unpacked to the wrong length`);
    tables.push({ tag, data, checksum });
  }
  // A TrueType directory is ordered by tag.
  tables.sort((a, b) => Buffer.compare(a.tag, b.tag));

  const searchRange = 2 ** Math.floor(Math.log2(tableCount)) * 16;
  const header = Buffer.alloc(12);
  header.writeUInt32BE(flavour, 0);
  header.writeUInt16BE(tableCount, 4);
  header.writeUInt16BE(searchRange, 6);
  header.writeUInt16BE(Math.floor(Math.log2(tableCount)), 8);
  header.writeUInt16BE(tableCount * 16 - searchRange, 10);

  const directory = Buffer.alloc(tableCount * 16);
  const bodies = [];
  let offset = 12 + tableCount * 16;
  tables.forEach((table, i) => {
    table.tag.copy(directory, i * 16);
    directory.writeUInt32BE(table.checksum, i * 16 + 4);
    directory.writeUInt32BE(offset, i * 16 + 8);
    directory.writeUInt32BE(table.data.length, i * 16 + 12);
    bodies.push(table.data);
    // Every table starts on a four byte boundary.
    const padding = (4 - (table.data.length % 4)) % 4;
    if (padding) bodies.push(Buffer.alloc(padding));
    offset += table.data.length + padding;
  });

  return Buffer.concat([header, directory, ...bodies]);
};

const outDir = path.join(path.dirname(fileURLToPath(import.meta.url)), "../internal/pdfdoc/fonts");
fs.mkdirSync(outDir, { recursive: true });

for (const face of FACES) {
  const converted = woffToTrueType(fs.readFileSync(path.join(fontDir, face.file)));
  fs.writeFileSync(path.join(outDir, face.out), converted);
  process.stderr.write(`${face.out} ${converted.length} bytes\n`);
}

// The licence travels with the fonts.
fs.copyFileSync(path.join(path.dirname(fontDir), "LICENSE"), path.join(outDir, "LICENSE"));
