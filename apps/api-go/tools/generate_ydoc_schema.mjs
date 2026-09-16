/**
 * Dump the document editor's ProseMirror schema as the Go port needs to see it.
 *
 * Rebuilding a document from Yjs is `schema.node(nodeName, attrs, children)` for every element and `schema.text(text, marks)` for every run of text, and both of those need the schema: which attributes a node declares, what each one defaults to when the Yjs element does not carry it, and the order the mark types were registered in, because that order is the rank ProseMirror sorts a text node's marks by.
 *
 * None of that is behaviour, it is data, so it is generated rather than transcribed. Regenerate whenever an extension is added, removed or reordered:
 *
 *     pnpm install --filter @plane/editor...
 *     node apps/api-go/tools/generate_ydoc_schema.mjs > apps/api-go/internal/ydoc/schema.json
 */

import { loadEditorModule } from "./ydoc_bundle.mjs";

// The same extension list `getAllDocumentFormatsFromDocumentEditorBinaryData` builds its schema from.
const { schema } = await loadEditorModule(process.cwd(), {
  source: `
    import { getSchema } from "@tiptap/core";
    import { CoreEditorExtensionsWithoutProps, DocumentEditorExtensionsWithoutProps } from "@/extensions/core-without-props";
    export const schema = getSchema([...CoreEditorExtensionsWithoutProps, ...DocumentEditorExtensionsWithoutProps]);
  `,
});

// Three cases, not two. `hasDefault` is what ProseMirror itself branches on: an attribute with no default must be supplied by the document, and a node missing one is a broken document rather than a node with a null attribute. But several extensions declare `default: undefined`, which is a default — so the node is valid without it — whose value then disappears from the node's JSON, because JSON.stringify drops undefined. That is different from `default: null`, which survives as a null, so the two are recorded apart.
const attributes = (attrs) =>
  Object.fromEntries(
    Object.entries(attrs).map(([name, attr]) => {
      if (!attr.hasDefault) return [name, { has_default: false }];
      if (attr.default === undefined) return [name, { has_default: true, default_is_undefined: true }];
      return [name, { has_default: true, default: attr.default }];
    })
  );

const nodes = Object.values(schema.nodes).map((type) => ({
  name: type.name,
  is_text: type.isText,
  attrs: attributes(type.attrs),
}));

// Emitted in registration order, which is the rank ProseMirror assigns each mark type.
const marks = Object.values(schema.marks).map((type) => ({
  name: type.name,
  attrs: attributes(type.attrs),
}));

process.stdout.write(`${JSON.stringify({ top_node: schema.topNodeType.name, nodes, marks }, null, 2)}\n`);
