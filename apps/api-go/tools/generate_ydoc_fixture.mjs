/**
 * Turn a set of documents through the real editor and print what it produces.
 *
 * The live service stores three things whenever somebody stops typing: the Yjs update bytes, the HTML they render to, and the ProseMirror JSON behind that HTML. The Go port has to produce the same three from the same bytes, so what is recorded here is exactly that.
 *
 * Run from the repository root:
 *
 *     pnpm install --filter @pace/editor...
 *     node apps/api-go/tools/generate_ydoc_fixture.mjs > apps/api-go/internal/ydoc/testdata/documents.json
 */

import { loadEditorModule } from "./ydoc_bundle.mjs";

// One bundle, so there is one copy of Yjs: two would work here, since the only thing crossing between them is a byte slice, but Yjs warns loudly about it and the warning is worth not having.
const { getAllDocumentFormatsFromDocumentEditorBinaryData, getBinaryDataFromDocumentEditorHTMLString, generateTitleProsemirrorJson, Y, yXmlFragmentToProseMirrorRootNode, prosemirrorJSONToYDoc, documentEditorSchema } = await loadEditorModule(process.cwd(), {
  source: `
    export { getAllDocumentFormatsFromDocumentEditorBinaryData, getBinaryDataFromDocumentEditorHTMLString, generateTitleProsemirrorJson } from "@/helpers/yjs-utils";
    export * as Y from "yjs";
    export { yXmlFragmentToProseMirrorRootNode, prosemirrorJSONToYDoc } from "y-prosemirror";
    import { getSchema } from "@tiptap/core";
    import { CoreEditorExtensionsWithoutProps, DocumentEditorExtensionsWithoutProps } from "@/extensions/core-without-props";
    export const documentEditorSchema = getSchema([...CoreEditorExtensionsWithoutProps, ...DocumentEditorExtensionsWithoutProps]);
  `,
  deterministic: true,
});

// One document per construct the editor can put in a page, plus the ones that break a serialiser.
const DOCUMENTS = [
  { name: "empty", title: "Untitled", html: "<p></p>" },
  { name: "paragraph", title: "A page", html: "<p>hello there</p>" },
  { name: "marks", title: "Marks", html: "<p><strong>bold</strong> <em>italic</em> <u>under</u> <s>struck</s> <code>code</code></p>" },
  { name: "nested marks", title: "Nested", html: "<p><strong>bold <em>and italic</em></strong></p>" },
  { name: "headings", title: "Headings", html: "<h1>One</h1><h2>Two</h2><h3>Three</h3><h4>Four</h4><h5>Five</h5><h6>Six</h6>" },
  { name: "bullet list", title: "Bullets", html: "<ul><li><p>one</p></li><li><p>two</p></li></ul>" },
  { name: "ordered list", title: "Numbers", html: "<ol><li><p>one</p></li><li><p>two</p></li></ol>" },
  { name: "nested list", title: "Nested list", html: "<ul><li><p>one</p><ul><li><p>deeper</p></li></ul></li></ul>" },
  { name: "task list", title: "Tasks", html: '<ul data-type="taskList"><li data-checked="true" data-type="taskItem"><label><input type="checkbox" checked="checked"><span></span></label><div><p>done</p></div></li><li data-checked="false" data-type="taskItem"><label><input type="checkbox"><span></span></label><div><p>not done</p></div></li></ul>' },
  { name: "quote", title: "Quote", html: "<blockquote><p>quoted</p></blockquote>" },
  { name: "code block", title: "Code", html: '<pre><code class="language-go">a := 1\nb := 2</code></pre>' },
  { name: "horizontal rule", title: "Rule", html: "<p>before</p><hr><p>after</p>" },
  { name: "link", title: "Link", html: '<p><a target="_blank" rel="noopener noreferrer nofollow" href="https://example.test/?a=1&amp;b=2">a link</a></p>' },
  { name: "hard break", title: "Break", html: "<p>line<br>break</p>" },
  { name: "text align", title: "Aligned", html: '<p style="text-align: center">centred</p><p style="text-align: right">right</p>' },
  { name: "colours", title: "Colours", html: '<p><span data-text-color="red">red text</span> <span data-background-color="peach">peach behind</span></p>' },
  { name: "table", title: "Table", html: "<table><tbody><tr><th><p>head</p></th><th><p>two</p></th></tr><tr><td><p>cell</p></td><td><p>other</p></td></tr></tbody></table>" },
  { name: "image", title: "Image", html: '<image-component src="11111111-1111-4111-8111-111111111111" width="35%" height="auto"></image-component>' },
  { name: "mention", title: "Mention", html: '<mention-component entity_identifier="22222222-2222-4222-8222-222222222222" entity_name="user_mention"></mention-component>' },
  { name: "callout", title: "Callout", html: '<div data-block-type="callout-component" data-logo-in-use="emoji" data-emoji-unicode="128161" data-emoji-url="https://example.test/bulb.png" data-background="#eef"><p>a callout</p></div>' },
  { name: "legacy image", title: "Legacy image", html: '<img src="https://example.test/picture.png" alt="a picture" title="hover text">' },
  {
    name: "emoji",
    title: "Emoji node",
    // Built from ProseMirror JSON rather than HTML: an emoji only ever enters a document through the ":shortcode:" input rule, so there is no HTML that parses back into one.
    json: {
      type: "doc",
      content: [{ type: "paragraph", attrs: { textAlign: null }, content: [{ type: "emoji", attrs: { name: "tada" } }] }],
    },
  },
  {
    name: "merged table cells",
    title: "Merged",
    // Also JSON, because colspan and rowspan only ever become non-default through the table toolbar.
    json: {
      type: "doc",
      content: [
        {
          type: "table",
          content: [
            {
              type: "tableRow",
              content: [
                { type: "tableHeader", attrs: { colspan: 2, rowspan: 1, colwidth: [200, 300], background: "none" }, content: [{ type: "paragraph", attrs: { textAlign: "center" }, content: [{ type: "text", text: "wide" }] }] },
              ],
            },
            {
              type: "tableRow",
              attrs: { background: "#fee", textColor: "#900" },
              content: [
                { type: "tableCell", attrs: { colspan: 1, rowspan: 2, colwidth: null, background: null }, content: [{ type: "paragraph", attrs: { textAlign: null }, content: [{ type: "text", text: "tall" }] }] },
                { type: "tableCell", attrs: { colspan: 1, rowspan: 1, colwidth: null, background: null }, content: [{ type: "paragraph", attrs: { textAlign: null }, content: [{ type: "text", text: "plain" }] }] },
              ],
            },
          ],
        },
      ],
    },
  },
  { name: "text style", title: "Styled", html: '<p><span style="font-size: 12px">styled</span></p>' },
  { name: "work item embed", title: "Embed", html: '<issue-embed-component entity_identifier="33333333-3333-4333-8333-333333333333" entity_name="issue_mention"></issue-embed-component>' },
  { name: "quoted title", title: `Quotes " and ' and & and \u00a0 and \u00ad`, html: "<p>body</p>" },
  { name: "padded title", title: "   leading and trailing   ", html: "<p>body</p>" },
  { name: "entity shaped title", title: "literal &amp; and &notanentity; and &#1234; and a bare &", html: "<p>body</p>" },
  { name: "entities", title: "Entities", html: '<p>non&nbsp;breaking and soft&shy;hyphen and an &amp; ampersand</p>' },
  { name: "escaping", title: "Escaping & <angles>", html: '<p>a &amp; b &lt; c &gt; d "quoted" \'single\'</p>' },
  { name: "unicode", title: "Ünicode ☃", html: "<p>Ünicode ☃ and an emoji 🎉</p>" },
  { name: "long paragraph", title: "Long", html: `<p>${"word ".repeat(200).trim()}</p>` },
  {
    name: "renumbered list",
    title: "Renumbered",
    // A list that does not start at one keeps its start attribute, which a list that does loses.
    json: { type: "doc", content: [{ type: "orderedList", attrs: { start: 7, type: null }, content: [{ type: "listItem", attrs: {}, content: [{ type: "paragraph", attrs: { textAlign: null }, content: [{ type: "text", text: "seventh" }] }] }] }] },
  },
  {
    name: "code block with a language",
    title: "Highlighted",
    json: { type: "doc", content: [{ type: "codeBlock", attrs: { language: "go" }, content: [{ type: "text", text: "package main" }] }] },
  },
  {
    name: "dangerous link",
    title: "Dangerous",
    // The href is emptied rather than the link dropped, and the check sees through a leading tab because a browser would.
    json: {
      type: "doc",
      content: [
        {
          type: "paragraph",
          attrs: { textAlign: null },
          content: [
            { type: "text", marks: [{ type: "link", attrs: { href: "javascript:alert(1)", target: "_blank", rel: "noopener noreferrer nofollow", class: null } }], text: "plain" },
            { type: "text", marks: [{ type: "link", attrs: { href: "\tJaVaScRiPt:alert(1)", target: "_blank", rel: "noopener noreferrer nofollow", class: null } }], text: "disguised" },
            { type: "text", marks: [{ type: "link", attrs: { href: "data:text/html,<script>", target: null, rel: null, class: null } }], text: "data" },
          ],
        },
      ],
    },
  },
  {
    name: "unpalettable colours",
    title: "Off palette",
    // A colour outside the editor's palette adds a style declaration, which this pipeline then drops.
    json: { type: "doc", content: [{ type: "paragraph", attrs: { textAlign: null }, content: [{ type: "text", marks: [{ type: "customColor", attrs: { color: "#ff0000", backgroundColor: "#00ff00" } }], text: "custom" }] }] },
  },
  {
    name: "unknown emoji",
    title: "Unknown",
    json: { type: "doc", content: [{ type: "paragraph", attrs: { textAlign: null }, content: [{ type: "emoji", attrs: { name: "not_an_emoji" } }] }] },
  },
  {
    name: "marks across text runs",
    title: "Runs",
    // One mark wrapping several runs opens once; the inner mark opens and closes inside it.
    json: {
      type: "doc",
      content: [
        {
          type: "paragraph",
          attrs: { textAlign: null },
          content: [
            { type: "text", marks: [{ type: "bold" }], text: "before " },
            { type: "text", marks: [{ type: "bold" }, { type: "italic" }], text: "middle" },
            { type: "text", marks: [{ type: "bold" }], text: " after" },
            { type: "text", text: " plain" },
          ],
        },
      ],
    },
  },
  {
    name: "marks in the other order",
    title: "Order",
    // The marks are listed italic first, and the output still nests bold outside, because the nesting follows the schema's registration order rather than the document's.
    json: { type: "doc", content: [{ type: "paragraph", attrs: { textAlign: null }, content: [{ type: "text", marks: [{ type: "italic" }, { type: "bold" }], text: "both" }] }] },
  },
  {
    name: "sized image",
    title: "Sized",
    json: { type: "doc", content: [{ type: "image", attrs: { src: "https://example.test/a.png", alt: null, title: null, width: "50%", height: "120px", aspectRatio: "1.5", alignment: "center" } }] },
  },
  {
    name: "coloured row",
    title: "Coloured",
    // A row with a background but no text colour still builds a style, and the style is dropped along with every other.
    json: {
      type: "doc",
      content: [
        {
          type: "table",
          content: [
            { type: "tableRow", attrs: { background: "#eee", textColor: null }, content: [{ type: "tableCell", attrs: { colspan: 1, rowspan: 1, colwidth: null, background: "#fff", textColor: "#111" }, content: [{ type: "paragraph", attrs: { textAlign: null }, content: [{ type: "text", text: "cell" }] }] }] },
          ],
        },
      ],
    },
  },
  {
    name: "identified mention",
    title: "Identified",
    json: { type: "doc", content: [{ type: "paragraph", attrs: { textAlign: null }, content: [{ type: "mention", attrs: { id: "44444444-4444-4444-8444-444444444444", entity_identifier: "55555555-5555-4555-8555-555555555555", entity_name: "user_mention" } }] }] },
  },
  { name: "mixed document", title: "Everything", html: "<h1>Title</h1><p>Some <strong>bold</strong> text.</p><ul><li><p>a point</p></li></ul><blockquote><p>a quote</p></blockquote><p>done</p>" },
];

// The title lives in its own fragment of the same update, as a whole document rather than as a string. `getAllDocumentFormats...` only hands back the flattened text, so the document itself is read out separately.
const titleDocument = (binary) => {
  const doc = new Y.Doc();
  Y.applyUpdate(doc, binary);
  return yXmlFragmentToProseMirrorRootNode(doc.getXmlFragment("title"), documentEditorSchema).toJSON();
};

// A case is written either as the HTML a user would paste, or — for node types no HTML parses back into — as the ProseMirror JSON the editor itself would hold.
const buildBinary = (document) => {
  if (document.html !== undefined) return getBinaryDataFromDocumentEditorHTMLString(document.html, document.title);
  const body = prosemirrorJSONToYDoc(documentEditorSchema, document.json, "default");
  const title = prosemirrorJSONToYDoc(documentEditorSchema, generateTitleProsemirrorJson(document.title), "title");
  Y.applyUpdate(body, Y.encodeStateAsUpdate(title));
  return Y.encodeStateAsUpdate(body);
};

const cases = DOCUMENTS.map((document) => {
  const binary = buildBinary(document);
  const formats = getAllDocumentFormatsFromDocumentEditorBinaryData(binary, true);
  return {
    name: document.name,
    source_html: document.html ?? null,
    source_json: document.json ?? null,
    source_title: document.title,
    binary: Buffer.from(binary).toString("base64"),
    content_html: formats.contentHTML,
    content_json: formats.contentJSON,
    title_json: titleDocument(binary),
    title_html: formats.titleHTML,
  };
});

process.stdout.write(`${JSON.stringify(cases, null, 2)}\n`);
