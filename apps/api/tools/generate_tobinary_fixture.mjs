/**
 * Record the Yjs updates the editor writes documents out as.
 *
 * This is the direction a page takes when it has HTML but no Yjs document yet: the HTML is read, the document is written out as an update, and that update is what every client afterwards synchronises against. The Go port has to write the same bytes, so the same bytes are what is recorded.
 *
 * Both sides fix the document's client id rather than drawing one, because the id is encoded into every item and a fresh one on every run would make this file undiffable. A running service leaves it random.
 *
 * Run from the repository root:
 *
 *     pnpm install --filter @pace/editor...
 *     node apps/api/tools/generate_tobinary_fixture.mjs > apps/api/internal/ydoc/testdata/updates.json
 */

import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { loadEditorModule } from "./ydoc_bundle.mjs";

const CLIENT_ID = 4242;

const { generateJSON, extensions, schema, Y, prosemirrorToYXmlFragment, Node } = await loadEditorModule(process.cwd(), {
  source: `
    export { generateJSON } from "@tiptap/html";
    export * as Y from "yjs";
    export { prosemirrorToYXmlFragment } from "y-prosemirror";
    export { Node } from "@tiptap/pm/model";
    import { getSchema } from "@tiptap/core";
    import { CoreEditorExtensionsWithoutProps, DocumentEditorExtensionsWithoutProps } from "@/extensions/core-without-props";
    export const extensions = [...CoreEditorExtensionsWithoutProps, ...DocumentEditorExtensionsWithoutProps];
    export const schema = getSchema(extensions);
  `,
});

// The fragments are written into a document built here rather than through prosemirrorJSONToYDoc, which makes its own and gives no way to fix the client id. What it does with that document is what happens below.
const buildUpdate = (documentJSON, titleJSON) => {
  const doc = new Y.Doc();
  doc.clientID = CLIENT_ID;
  prosemirrorToYXmlFragment(Node.fromJSON(schema, documentJSON), doc.getXmlFragment("default"));
  if (titleJSON) {
    prosemirrorToYXmlFragment(Node.fromJSON(schema, titleJSON), doc.getXmlFragment("title"));
  }
  return Buffer.from(Y.encodeStateAsUpdate(doc)).toString("base64");
};

const titleDocument = (title) => ({
  type: "doc",
  content: [{ type: "heading", attrs: { level: 1 }, ...(title ? { content: [{ type: "text", text: title }] } : {}) }],
});

// The same documents the parser is checked against, so every shape that reaches the reader also reaches the writer.
const parsedPath = path.join(path.dirname(fileURLToPath(import.meta.url)), "../internal/ydoc/testdata/parsed.json");
const cases = JSON.parse(readFileSync(parsedPath, "utf8")).map((parsed) => ({
  name: parsed.name,
  html: parsed.html,
  update: buildUpdate(parsed.document),
  // With a title beside the body, which is what a page that is being built from its HTML is written with.
  update_with_title: buildUpdate(parsed.document, titleDocument(`Title of ${parsed.name}`)),
}));

// A title on its own, including the empty one, whose heading has no content at all.
cases.push({
  name: "empty title",
  html: null,
  update: buildUpdate(generateJSON("<p></p>", extensions)),
  update_with_title: buildUpdate(generateJSON("<p></p>", extensions), titleDocument("")),
});

process.stdout.write(`${JSON.stringify(cases, null, 2)}\n`);
