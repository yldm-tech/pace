/**
 * Dump the document editor's ProseMirror schema as the Go port needs to see it.
 *
 * Rebuilding a document from Yjs is `schema.node(nodeName, attrs, children)` for every element and `schema.text(text, marks)` for every run of text, and both of those need the schema: which attributes a node declares, what each one defaults to when the Yjs element does not carry it, and the order the mark types were registered in, because that order is the rank ProseMirror sorts a text node's marks by.
 *
 * None of that is behaviour, it is data, so it is generated rather than transcribed. Regenerate whenever an extension is added, removed or reordered:
 *
 *     pnpm install --filter @plane/editor...
 *     node apps/api-go/tools/generate_ydoc_schema.mjs > apps/api-go/internal/ydoc/schema.json
 *     node apps/api-go/tools/generate_ydoc_schema.mjs --variant=rich > apps/api-go/internal/ydoc/schema_rich.json
 */

import { loadEditorModule } from "./ydoc_bundle.mjs";

// The editor has two schemas: the document one a page is written with, and the rich text one everything else uses, which is the same list without the work item embed. Which is dumped is chosen with --variant.
const variant = process.argv.includes("--variant=rich") ? "rich" : "document";

const { schema } = await loadEditorModule(process.cwd(), {
  source: `
    import { getSchema } from "@tiptap/core";
    import { CoreEditorExtensionsWithoutProps, DocumentEditorExtensionsWithoutProps } from "@/extensions/core-without-props";
    const extensions = ${variant === "rich" ? "[...CoreEditorExtensionsWithoutProps]" : "[...CoreEditorExtensionsWithoutProps, ...DocumentEditorExtensionsWithoutProps]"};
    export const schema = getSchema(extensions);
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

// is_leaf and is_inline are the two facts the serializer branches on: a leaf node may not have a content hole in its spec, and a mark wrapping inline content is told so. The content and marks expressions are what the parser matches against — they are the grammar that says a list item holds blocks and a table row holds cells, and nothing else.
const nodes = Object.values(schema.nodes).map((type) => ({
  name: type.name,
  is_text: type.isText,
  is_leaf: type.isLeaf,
  is_inline: type.isInline,
  groups: type.groups,
  content: type.spec.content ?? "",
  whitespace: type.spec.whitespace ?? "",
  marks: type.spec.marks ?? null,
  attr_names: Object.keys(type.attrs),
  attrs: attributes(type.attrs),
}));

// Emitted in registration order, which is the rank ProseMirror assigns each mark type. A mark declared non-spanning is never reused across adjacent text nodes, so it opens a fresh element for each.
const marks = Object.values(schema.marks).map((type) => ({
  name: type.name,
  spanning: type.spec.spanning !== false,
  groups: type.spec.group ? type.spec.group.split(" ") : [],
  excludes: type.spec.excludes ?? null,
  attr_names: Object.keys(type.attrs),
  attrs: attributes(type.attrs),
}));

process.stdout.write(`${JSON.stringify({ top_node: schema.topNodeType.name, nodes, marks }, null, 2)}\n`);
