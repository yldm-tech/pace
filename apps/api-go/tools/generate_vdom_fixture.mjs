/**
 * Record the trees the editor's HTML parser builds.
 *
 * The editor parses HTML with zeed-dom, which is a scanner rather than an HTML5 tree builder: a closing tag pops whatever is open rather than the tag it names, nothing is implied, and markup it cannot make sense of becomes text. Reproducing that exactly is what this corpus is for — a parser that repairs more than zeed-dom does reads a page into a different document.
 *
 * Run from the repository root:
 *
 *     pnpm install --filter @plane/editor...
 *     node apps/api-go/tools/generate_vdom_fixture.mjs > apps/api-go/internal/vdom/testdata/trees.json
 */

import { readFileSync } from "node:fs";
import { createRequire } from "node:module";
import path from "node:path";
import { fileURLToPath } from "node:url";

// zeed-dom is not a direct dependency of any workspace package — the editor reaches it through @tiptap/html — so it is resolved from there rather than from the workspace root.
const editorRequire = createRequire(path.join(process.cwd(), "packages/editor/package.json"));
const htmlRequire = createRequire(editorRequire.resolve("@tiptap/html"));
const { parseHTML, VNode } = htmlRequire("zeed-dom");

const DOCUMENTS = [
  { name: "plain text", html: "just some words" },
  { name: "one element", html: "<p>hello</p>" },
  { name: "nested elements", html: "<div><p>a<strong>b</strong>c</p></div>" },
  { name: "attributes", html: `<a href="https://example.test/?a=1&amp;b=2" target='_blank' download data-x=bare>link</a>` },
  { name: "empty attribute values", html: `<p title="" data-a=''>x</p>` },
  { name: "repeated attribute", html: `<p class="one" class="two">x</p>` },
  { name: "uppercase tags", html: "<DIV><P>Mixed</P></DIV>" },
  { name: "void element", html: "<p>before<br>after</p>" },
  { name: "self closing syntax", html: "<p>a<span />b</p>" },
  { name: "image", html: `<img src="a.png" alt="an image">` },
  // A closing tag pops whatever is open, so these two nest by position rather than by name.
  { name: "overlapping tags", html: "<b><i>both</b>italic</i>" },
  { name: "stray closing tag", html: "<p>text</p></div>more" },
  { name: "unclosed elements", html: "<div><p>never closed" },
  { name: "comment", html: "<p>a<!-- hidden -->b</p>" },
  { name: "unterminated comment", html: "<p>a<!-- never ends" },
  { name: "doctype", html: "<!DOCTYPE html><p>after</p>" },
  { name: "entities in text", html: "<p>a &amp; b &lt; c &gt; d &quot;e&quot; &apos;f&apos; &nbsp; &shy; &#65; &#x42;</p>" },
  { name: "entity without a semicolon", html: "<p>a &amp b &notanentity; c</p>" },
  { name: "lone angle bracket", html: "<p>a < b</p>" },
  { name: "unmatched angle bracket at the start", html: "< not a tag <p>real</p>" },
  { name: "script body", html: "<script>if (a < b) { x(); }</script><p>after</p>" },
  { name: "style body", html: "<style>p > a { color: red }</style><p>after</p>" },
  { name: "unterminated script", html: "<script>if (a < b) {" },
  // No tbody is invented, which is the difference that matters most for a table.
  { name: "table without a tbody", html: "<table><tr><td>cell</td></tr></table>" },
  { name: "table with a tbody", html: "<table><tbody><tr><th>head</th></tr></tbody></table>" },
  { name: "adjacent text runs", html: "<p>a<!--x-->b</p>" },
  { name: "whitespace", html: "<p>  spaced  out  </p>\n<p>\tnext\n</p>" },
  { name: "style attribute", html: `<p style="text-align: center; color: red">x</p>` },
  { name: "style attribute with a url", html: `<div style="background: url(a.png?a=1;b=2) no-repeat; color: blue">x</div>` },
  { name: "custom element", html: `<image-component src="abc" width="35%"></image-component>` },
  { name: "unicode", html: "<p>Ünicode ☃ 🎉</p>" },
  { name: "empty string", html: "" },
];

const serialize = (node) => {
  if (node.nodeType === VNode.TEXT_NODE) {
    return { type: "text", text: node.textContent };
  }
  const attrs = Object.entries(node._attributes ?? {}).map(([name, value]) => ({
    name,
    value: value === true ? "" : String(value),
    bare: value === true,
  }));
  return {
    type: "element",
    tag: node._originalTagName ?? node.tagName,
    attrs,
    children: (node._childNodes ?? []).map(serialize),
  };
};

// Every document the editor renders, fed back in. This is the input shape that matters most: a page's stored HTML is the renderer's own output, and it is what gets parsed back when the page is opened with no Yjs document yet.
// Resolved from this file rather than from the working directory: the editor's dependencies come from the checkout the tool is run in, but this fixture belongs to the tool.
const renderedPath = path.join(path.dirname(fileURLToPath(import.meta.url)), "../internal/ydoc/testdata/documents.json");
const rendered = JSON.parse(readFileSync(renderedPath, "utf8"));
for (const document of rendered) {
  DOCUMENTS.push({ name: `rendered: ${document.name}`, html: document.content_html });
}

const cases = DOCUMENTS.map((document) => ({
  name: document.name,
  html: document.html,
  tree: (parseHTML(document.html)._childNodes ?? []).map(serialize),
}));

process.stdout.write(`${JSON.stringify(cases, null, 2)}\n`);
