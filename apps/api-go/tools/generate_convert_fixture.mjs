/**
 * Record what the conversion route answers.
 *
 * The API calls it when it copies a page or a work item description: the assets in the HTML have been swapped for their copies, and the Yjs document and the JSON beside it have to be rebuilt from the result. Both of the editor's schemas are exercised, because which one a piece of HTML is read with decides whether a work item embed in it survives.
 *
 * The client id is fixed on both sides, because it is encoded into every item and a fresh one on every run would make this file undiffable.
 *
 * Run from the repository root:
 *
 *     pnpm install --filter @plane/editor...
 *     node apps/api-go/tools/generate_convert_fixture.mjs > apps/api-go/internal/live/testdata/converted.json
 */

import { loadEditorModule } from "./ydoc_bundle.mjs";

const CLIENT_ID = 4242;

const { generateJSON, generateHTML, Y, Node, prosemirrorToYXmlFragment, yXmlFragmentToProseMirrorRootNode, documentSchema, richSchema, documentExtensions, richExtensions } =
  await loadEditorModule(process.cwd(), {
    source: `
    export { generateJSON, generateHTML } from "@tiptap/html";
    export * as Y from "yjs";
    export { Node } from "@tiptap/pm/model";
    export { prosemirrorToYXmlFragment, yXmlFragmentToProseMirrorRootNode } from "y-prosemirror";
    import { getSchema } from "@tiptap/core";
    import { CoreEditorExtensionsWithoutProps, DocumentEditorExtensionsWithoutProps } from "@/extensions/core-without-props";
    export const richExtensions = [...CoreEditorExtensionsWithoutProps];
    export const documentExtensions = [...CoreEditorExtensionsWithoutProps, ...DocumentEditorExtensionsWithoutProps];
    export const richSchema = getSchema(richExtensions);
    export const documentSchema = getSchema(documentExtensions);
  `,
  });

const variants = {
  document: { schema: documentSchema, extensions: documentExtensions },
  rich: { schema: richSchema, extensions: richExtensions },
};

// convert is the route's own body: read the html, write it out as a Yjs update, and derive the three formats from that update rather than from what was read. The difference matters — a document read out of an update is not always the document that went into it.
const convert = (html, variantName) => {
  const { schema, extensions } = variants[variantName];

  const doc = new Y.Doc();
  doc.clientID = CLIENT_ID;
  prosemirrorToYXmlFragment(Node.fromJSON(schema, generateJSON(html, extensions)), doc.getXmlFragment("default"));
  const update = Y.encodeStateAsUpdate(doc);

  const back = new Y.Doc();
  Y.applyUpdate(back, update);
  const json = yXmlFragmentToProseMirrorRootNode(back.getXmlFragment("default"), schema).toJSON();

  return {
    description_binary: Buffer.from(update).toString("base64"),
    description_json: json,
    description_html: generateHTML(json, extensions),
  };
};

const DOCUMENTS = [
  { name: "paragraph", html: "<p>hello</p>" },
  { name: "marks", html: "<p><strong>bold</strong> <em>italic</em></p>" },
  { name: "list", html: "<ul><li><p>one</p></li></ul>" },
  { name: "table", html: "<table><tr><td><p>cell</p></td></tr></table>" },
  { name: "image", html: `<image-component src="abc" width="35%"></image-component>` },
  { name: "mention", html: `<p><mention-component entity_identifier="x" entity_name="user_mention"></mention-component></p>` },
  // The one thing the two schemas disagree about: a work item embed is a node in one and nothing at all in the other.
  { name: "work item embed", html: `<issue-embed-component entity_identifier="y" entity_name="issue_mention"></issue-embed-component>` },
  { name: "entities", html: "<p>a &amp; b &lt; c</p>" },
  { name: "whitespace", html: "<p>  spaced   out  </p>" },
  { name: "unknown element", html: "<section><p>inside</p></section>" },
];

const cases = [];
for (const document of DOCUMENTS) {
  for (const variantName of ["document", "rich"]) {
    cases.push({
      name: `${variantName}: ${document.name}`,
      variant: variantName,
      html: document.html,
      ...convert(document.html, variantName),
    });
  }
}

process.stdout.write(`${JSON.stringify(cases, null, 2)}\n`);
