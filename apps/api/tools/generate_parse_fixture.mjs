/**
 * Record the documents the editor reads HTML into.
 *
 * This is the other direction from the renderer: markup in, document out. The inputs are the ones the parser actually meets — the renderer's own output, which is what a page's stored HTML is — plus the shapes that make the parser work: content in the wrong place, elements the schema has no rule for, whitespace, and markup nobody would write on purpose.
 *
 * Run from the repository root:
 *
 *     pnpm install --filter @pace/editor...
 *     node apps/api/tools/generate_parse_fixture.mjs > apps/api/internal/ydoc/testdata/parsed.json
 */

import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { loadEditorModule } from "./ydoc_bundle.mjs";

const { generateJSON, generateHTML, extensions } = await loadEditorModule(process.cwd(), {
  source: `
    export { generateJSON, generateHTML } from "@tiptap/html";
    import { CoreEditorExtensionsWithoutProps, DocumentEditorExtensionsWithoutProps } from "@/extensions/core-without-props";
    export const extensions = [...CoreEditorExtensionsWithoutProps, ...DocumentEditorExtensionsWithoutProps];
  `,
});

const DOCUMENTS = [
  { name: "empty", html: "" },
  { name: "bare text", html: "hello" },
  { name: "paragraph", html: "<p>hello</p>" },
  { name: "two paragraphs", html: "<p>one</p><p>two</p>" },
  { name: "headings", html: "<h1>One</h1><h2>Two</h2><h3>Three</h3><h4>Four</h4><h5>Five</h5><h6>Six</h6>" },
  { name: "marks", html: "<p><strong>bold</strong> <em>italic</em> <u>under</u> <s>struck</s> <code>code</code></p>" },
  { name: "legacy mark tags", html: "<p><b>bold</b> <i>italic</i> <del>gone</del> <strike>gone</strike></p>" },
  // The style rules, which only run when the element carries a style attribute of its own.
  { name: "bold by style", html: `<p><span style="font-weight: bold">bold</span> <span style="font-weight: 700">also</span> <span style="font-weight: 300">not</span></p>` },
  { name: "bold cancelled by style", html: `<p><strong><span style="font-weight: 400">not bold</span></strong></p>` },
  { name: "italic by style", html: `<p><span style="font-style: italic">italic</span></p>` },
  { name: "decoration styles", html: `<p><span style="text-decoration: line-through">struck</span> <span style="text-decoration: underline">under</span></p>` },
  { name: "bold element saying otherwise", html: `<p><b style="font-weight: normal">not bold</b></p>` },
  { name: "colours", html: `<p><span data-text-color="red">red</span> <span data-background-color="peach">peach</span></p>` },
  { name: "nested marks", html: "<p><strong>bold <em>and italic</em></strong></p>" },
  { name: "link", html: `<p><a href="https://example.test/">a link</a></p>` },
  { name: "dangerous link", html: `<p><a href="javascript:alert(1)">still text</a></p>` },
  { name: "lists", html: "<ul><li><p>one</p></li><li><p>two</p></li></ul><ol><li><p>first</p></li></ol>" },
  { name: "list with bare text", html: "<ul><li>bare</li></ul>" },
  // A list written straight inside another is moved into the item above it.
  { name: "directly nested list", html: "<ul><li><p>one</p></li><ul><li><p>deeper</p></li></ul></ul>" },
  { name: "renumbered list", html: `<ol start="7"><li><p>seventh</p></li></ol>` },
  { name: "task list", html: `<ul data-type="taskList"><li data-checked="true" data-type="taskItem"><div><p>done</p></div></li><li data-type="taskItem"><div><p>not done</p></div></li></ul>` },
  { name: "quote", html: "<blockquote><p>quoted</p></blockquote>" },
  { name: "code block", html: `<pre><code class="language-go">a := 1\nb := 2</code></pre>` },
  { name: "code block without a language", html: "<pre><code>plain</code></pre>" },
  { name: "horizontal rule", html: "<p>before</p><hr><p>after</p>" },
  { name: "break", html: "<p>line<br>break</p>" },
  { name: "alignment", html: `<p style="text-align: center">centred</p><p style="text-align: justify">not offered</p>` },
  // No tbody is invented, so the rules have to cope with a row straight inside a table.
  { name: "table without a tbody", html: "<table><tr><th>head</th></tr><tr><td>cell</td></tr></table>" },
  { name: "table with a tbody", html: "<table><tbody><tr><td><p>cell</p></td></tr></tbody></table>" },
  { name: "merged cells", html: `<table><tr><th colspan="2" rowspan="1" colwidth="200" background="none"><p>wide</p></th></tr></table>` },
  { name: "components", html: `<image-component src="abc" width="35%"></image-component><mention-component entity_identifier="x" entity_name="user_mention"></mention-component><issue-embed-component entity_identifier="y"></issue-embed-component>` },
  { name: "callout", html: `<div data-block-type="callout-component" data-logo-in-use="emoji"><p>a callout</p></div>` },
  { name: "emoji", html: `<p><span data-type="emoji" data-name="tada"></span></p>` },
  { name: "legacy image", html: `<img src="https://example.test/a.png" alt="alt">` },
  { name: "data url image", html: `<img src="data:image/png;base64,AAAA">` },
  // Content in the wrong place, which is what makes the parser wrap and fill.
  { name: "stray list item", html: "<li><p>on its own</p></li>" },
  { name: "stray table cell", html: "<td>on its own</td>" },
  { name: "stray table row", html: "<tr><td>on its own</td></tr>" },
  { name: "block inside inline", html: "<p>before<div>a block</div>after</p>" },
  { name: "text beside a block", html: "<div>loose text<p>a paragraph</p>more loose text</div>" },
  { name: "unknown element", html: "<section><p>inside</p></section>" },
  { name: "unknown inline element", html: "<p>a <cite>cited</cite> thing</p>" },
  { name: "empty unknown element", html: "<p>a<wbr>b</p>" },
  { name: "ignored element", html: "<p>before</p><script>notdocument()</script><p>after</p>" },
  // Whitespace: collapsed outside a preformatted node, kept inside one.
  { name: "collapsed whitespace", html: "<p>  lots   of    space  </p>" },
  { name: "whitespace between blocks", html: "<p>one</p>\n\n  <p>two</p>" },
  { name: "whitespace only", html: "<p>   </p>" },
  { name: "whitespace after a break", html: "<p>a<br>  b</p>" },
  { name: "preformatted whitespace", html: "<pre><code>  kept  \n  as written  </code></pre>" },
  { name: "entities", html: "<p>a &amp; b &lt; c &nbsp; d</p>" },
  { name: "deeply nested", html: "<blockquote><ul><li><blockquote><p>deep</p></blockquote></li></ul></blockquote>" },
  { name: "overlapping tags", html: "<p><strong>bold <em>both</strong> italic</em></p>" },
  { name: "unclosed paragraph", html: "<p>never closed" },
];

// Every document the renderer produces, read back in. A page's stored HTML is the renderer's own output, and this is the path it takes the first time somebody opens the page in the collaborative editor.
const renderedPath = path.join(path.dirname(fileURLToPath(import.meta.url)), "../internal/ydoc/testdata/documents.json");
for (const document of JSON.parse(readFileSync(renderedPath, "utf8"))) {
  DOCUMENTS.push({ name: `rendered: ${document.name}`, html: document.content_html });
}

// The loop the live service runs: read the page's HTML, render it back, read that. The two renderings are recorded separately because they are not always the same — a span carrying a style is read into marks that render as elements, and those elements are then read into a different nesting. The editor is not idempotent there, and the Go port has to be un-idempotent in the same way.
const cases = DOCUMENTS.map((document) => {
  const parsed = generateJSON(document.html, extensions);
  const rendered = generateHTML(parsed, extensions);
  const reparsed = generateJSON(rendered, extensions);
  return {
    name: document.name,
    html: document.html,
    document: parsed,
    rendered,
    reparsed,
    rerendered: generateHTML(reparsed, extensions),
  };
});

process.stdout.write(`${JSON.stringify(cases, null, 2)}\n`);
