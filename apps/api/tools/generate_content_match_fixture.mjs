/**
 * Record what ProseMirror's content expressions compile to.
 *
 * A content expression is a grammar — "block+", "paragraph block*", "(tableCell | tableHeader)*" — and ProseMirror compiles each one into a finite automaton. The Go port has to build the same automaton, edge for edge and in the same order, because the order is what decides which node type gets invented when a gap has to be filled.
 *
 * ProseMirror's ContentMatch prints its whole automaton, so that dump is the truth table: one line per state, marked when a node may end there, listing every outgoing edge and the state it leads to. Two automatons that print alike are the same automaton.
 *
 * Beside the dumps are the answers to the three questions the parser actually asks — what a type may hold, what it would have to be wrapped in to fit somewhere, and what would have to be inserted to make a node whole.
 *
 * Run from the repository root:
 *
 *     pnpm install --filter @pace/editor...
 *     node apps/api/tools/generate_content_match_fixture.mjs > apps/api/internal/ydoc/testdata/content_match.json
 */

import { loadEditorModule } from "./ydoc_bundle.mjs";

const { schema, ContentMatch, Fragment } = await loadEditorModule(process.cwd(), {
  source: `
    import { getSchema } from "@tiptap/core";
    import { CoreEditorExtensionsWithoutProps, DocumentEditorExtensionsWithoutProps } from "@/extensions/core-without-props";
    export { ContentMatch, Fragment } from "@tiptap/pm/model";
    export const schema = getSchema([...CoreEditorExtensionsWithoutProps, ...DocumentEditorExtensionsWithoutProps]);
  `,
});

const typeNames = Object.keys(schema.nodes);

// The automaton of every node type, printed.
const automatons = typeNames.map((name) => ({
  type: name,
  expression: schema.nodes[name].spec.content ?? "",
  automaton: schema.nodes[name].contentMatch.toString(),
}));

// Whether one type may directly follow the start of another's content.
const matches = [];
for (const outer of typeNames) {
  for (const inner of typeNames) {
    matches.push({ in: outer, type: inner, matches: schema.nodes[outer].contentMatch.matchType(schema.nodes[inner]) !== null });
  }
}

// What a type would have to be wrapped in to be allowed at the start of another's content. Null means there is no such wrapping; an empty list means it already fits.
const wrappings = [];
for (const outer of typeNames) {
  for (const inner of typeNames) {
    const found = schema.nodes[outer].contentMatch.findWrapping(schema.nodes[inner]);
    wrappings.push({ in: outer, type: inner, wrapping: found === null ? null : found.map((wrapper) => wrapper.name) });
  }
}

// What would have to be inserted before nothing at all for a type's content to be complete, which is the question asked when an empty node is created.
const fills = typeNames.map((name) => {
  const filled = schema.nodes[name].contentMatch.fillBefore(Fragment.empty, true);
  return { type: name, fill: filled === null ? null : filled.content.map((node) => node.type.name) };
});

// The whole node each type builds when it is asked for empty, which is what a wrapping gets handed. It is recursive: a type whose content is required arrives with that content already in it.
const created = typeNames.map((name) => {
  const node = schema.nodes[name].createAndFill();
  return { type: name, node: node === null ? null : node.toJSON() };
});

// Which marks each node type allows. Null means every mark, and an empty list means none.
const markSets = typeNames.map((name) => ({
  type: name,
  marks: schema.nodes[name].markSet === null ? null : schema.nodes[name].markSet.map((mark) => mark.name),
}));

// Some expressions the schema does not itself contain, to exercise the parts of the grammar it never reaches: ranges, optionals, alternation and nesting.
const EXTRA_EXPRESSIONS = [
  "paragraph",
  "paragraph?",
  "paragraph*",
  "paragraph+",
  "paragraph{2}",
  "paragraph{1,3}",
  "paragraph{2,}",
  "paragraph heading",
  "paragraph | heading",
  "(paragraph | heading)+",
  "paragraph (heading | blockquote)* paragraph",
  "block",
  "text*",
  "inline*",
  "listItem+",
  "(paragraph heading){2}",
  "paragraph? heading? blockquote?",
];



const extras = EXTRA_EXPRESSIONS.map((expression) => ({
  expression,
  automaton: ContentMatch.parse(expression, schema.nodes).toString(),
}));

process.stdout.write(`${JSON.stringify({ automatons, matches, wrappings, fills, created, markSets, extras }, null, 2)}\n`);
