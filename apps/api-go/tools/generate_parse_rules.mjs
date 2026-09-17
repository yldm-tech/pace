/**
 * Dump the parse rules the document editor's schema carries.
 *
 * A parse rule says which element or style produces which node or mark, and ProseMirror sorts them by priority into one list that it tries in order. Everything about a rule except its functions is data, so it is generated: the selector, the priority, what it produces, and the handful of flags that change how its content is read.
 *
 * The functions are not data. A rule carrying getAttrs, contentElement or getContent is flagged here and ported by hand on the Go side, and the flag is what stops one being added upstream without anybody noticing.
 *
 * Run from the repository root:
 *
 *     pnpm install --filter @pace/editor...
 *     node apps/api-go/tools/generate_parse_rules.mjs > apps/api-go/internal/ydoc/parse_rules.json
 *     node apps/api-go/tools/generate_parse_rules.mjs --variant=rich > apps/api-go/internal/ydoc/parse_rules_rich.json
 */

import { loadEditorModule } from "./ydoc_bundle.mjs";

// The editor has two schemas, and so two sets of rules. Which is dumped is chosen with --variant.
const variant = process.argv.includes("--variant=rich") ? "rich" : "document";

const { schema, DOMParser, extensionAttributes, baseRules } = await loadEditorModule(process.cwd(), {
  source: `
    import { getSchema, getAttributesFromExtensions } from "@tiptap/core";
    import { CoreEditorExtensionsWithoutProps, DocumentEditorExtensionsWithoutProps } from "@/extensions/core-without-props";
    export { DOMParser } from "@tiptap/pm/model";
    import { getExtensionField } from "@tiptap/core";
    const extensions = ${variant === "rich" ? "[...CoreEditorExtensionsWithoutProps]" : "[...CoreEditorExtensionsWithoutProps, ...DocumentEditorExtensionsWithoutProps]"};
    export const schema = getSchema(extensions);
    // Flattened here rather than through the manager's own resolve, which is not exported: a kit contributes its extensions through addExtensions, and only the leaves declare attributes.
    const flatten = (list) => list.flatMap((extension) => {
      const nested = extension.config.addExtensions ? extension.config.addExtensions.call({ name: extension.name, options: extension.options, storage: extension.storage }) : null;
      return nested ? flatten(nested) : [extension];
    });
    const leaves = flatten(extensions);
    export const extensionAttributes = getAttributesFromExtensions(leaves);
    // The rules as each extension declares them, before Tiptap wraps every one of them in an attribute reader. The wrapper is the same for all of them; what differs, and what has to be ported by hand, is the getAttrs underneath it.
    export const baseRules = leaves.flatMap((extension) => {
      const context = { name: extension.name, options: extension.options, storage: extension.storage, editor: undefined, type: null };
      const parseHTML = getExtensionField(extension, "parseHTML", context);
      if (!parseHTML) return [];
      return (parseHTML() ?? []).map((rule, index) => ({
        owner: extension.name,
        index,
        tag: rule.tag ?? null,
        style: rule.style ?? null,
        attrs: rule.attrs ?? null,
        get_attrs: rule.getAttrs ? String(rule.getAttrs) : null,
        clear_mark: rule.clearMark ? String(rule.clearMark) : null,
      }));
    });
  `,
});

// The attributes each type reads off an element, in the order Tiptap reads them. An attribute with its own parseHTML is code and is ported by hand; every other one is the element's attribute of the same name, with a numeric string turned into a number and "true" and "false" into booleans.
const attributesFor = (typeName) =>
  extensionAttributes
    .filter((item) => item.type === typeName)
    .map((item) => ({
      name: item.name,
      has_parse_html: typeof item.attribute.parseHTML === "function",
      parse_html: item.attribute.parseHTML ? String(item.attribute.parseHTML) : null,
    }));

// Each rule is marked with the type that declared it and its position among that type's rules before the sorting, because sorting mixes the types together and a hand-ported function has to be matched back to the rule it came from. The marker survives because the sort copies each rule's own properties.
for (const collection of [schema.marks, schema.nodes]) {
  for (const [name, type] of Object.entries(collection)) {
    (type.spec.parseDOM ?? []).forEach((rule, index) => {
      rule.__owner = name;
      rule.__index = index;
    });
  }
}

// schemaRules is what the parser is built from: every rule in the schema, sorted by priority, with the node or mark it produces filled in.
const rules = DOMParser.schemaRules(schema).map((rule) => ({
  owner: rule.__owner,
  owner_index: rule.__index,
  tag: rule.tag ?? null,
  style: rule.style ?? null,
  node: rule.node ?? null,
  mark: rule.mark ?? null,
  reads: rule.style ? [] : attributesFor(rule.node ?? rule.mark ?? ""),
  priority: rule.priority ?? null,
  context: rule.context ?? null,
  ignore: rule.ignore ?? false,
  skip: rule.skip ?? false,
  close_parent: rule.closeParent ?? false,
  consuming: rule.consuming ?? null,
  preserve_whitespace: rule.preserveWhitespace ?? null,
  attrs: rule.attrs ?? null,
  has_get_attrs: typeof rule.getAttrs === "function",
  has_clear_mark: typeof rule.clearMark === "function",
  has_content_element: rule.contentElement !== undefined,
  has_get_content: typeof rule.getContent === "function",
}));

// The style properties the parser will look up on an element, taken from the style rules. It reads only these rather than walking every declaration.
const matchedStyles = [];
for (const rule of rules) {
  if (rule.style === null) continue;
  const property = /[^=]*/.exec(rule.style)[0];
  if (!matchedStyles.includes(property)) matchedStyles.push(property);
}

// Whether a list type in this schema can hold itself directly. When none can, the parser first moves a list nested straight inside another into the list item above it.
const normalizeLists = !rules.some((rule) => {
  if (rule.tag === null || !/^(ul|ol)\b/.test(rule.tag) || !rule.node) return false;
  const node = schema.nodes[rule.node];
  return node.contentMatch.matchType(node) !== null;
});

process.stdout.write(`${JSON.stringify({ rules, base_rules: baseRules, matched_styles: matchedStyles, normalize_lists: normalizeLists }, null, 2)}\n`);
