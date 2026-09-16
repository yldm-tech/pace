package ydoc

import (
	"bufio"
	"bytes"
	_ "embed"
	"fmt"
	"strings"
)

// nodeRenderers is each node type's renderHTML, keyed the way the schema names it.
//
// The order attributes are added in is the order they come out in, and it is the order Tiptap builds them in: the extension's own options first, then the node's attributes in the order the extension declares them, then whatever the renderHTML adds last.
var nodeRenderers = map[string]func(Node) spec{
	"paragraph":  renderParagraph,
	"heading":    renderHeading,
	"blockquote": func(Node) spec { return element("blockquote", newAttrs(), hole()) },
	"bulletList": func(Node) spec {
		return element("ul", newAttrs("class", "list-disc pl-7 space-y-(--list-spacing-y)"), hole())
	},
	"orderedList":           renderOrderedList,
	"listItem":              func(Node) spec { return element("li", newAttrs("class", "not-prose space-y-2"), hole()) },
	"hardBreak":             func(Node) spec { return element("br", newAttrs()) },
	"horizontalRule":        renderHorizontalRule,
	"codeBlock":             renderCodeBlock,
	"image":                 renderImage,
	"imageComponent":        renderImageComponent,
	"mention":               renderMention,
	"emoji":                 renderEmoji,
	"taskList":              renderTaskList,
	"taskItem":              renderTaskItem,
	"table":                 renderTable,
	"tableRow":              renderTableRow,
	"tableHeader":           renderTableHeader,
	"tableCell":             renderTableCell,
	"calloutComponent":      renderCallout,
	"issue-embed-component": renderWorkItemEmbed,
}

// attrsInOrder turns a node's attributes into HTML attributes, taking them in the order the extension declares them and passing each value through untouched. It is Tiptap's getRenderedAttributes for the attributes that declare no rendering of their own, which is most of them.
func attrsInOrder(attrs map[string]any, names ...string) *attrList {
	out := newAttrs()
	for _, name := range names {
		out.set(name, attrValue(attrs[name]))
	}
	return out
}

// attrValue renders one attribute value the way JavaScript would hand it to setAttribute. A list becomes its elements joined by commas, which is what a column width array turns into.
func attrValue(value any) any {
	switch typed := value.(type) {
	case nil:
		return nil
	case bool:
		return typed
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			parts = append(parts, asString(attrValue(item)))
		}
		return strings.Join(parts, ",")
	case float64:
		return formatNumber(typed)
	case int64:
		return fmt.Sprint(typed)
	case int:
		return fmt.Sprint(typed)
	default:
		return typed
	}
}

// formatNumber prints a number the way JavaScript's String() does, which drops the fractional part of a whole number.
func formatNumber(value float64) string {
	if value == float64(int64(value)) {
		return fmt.Sprint(int64(value))
	}
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%f", value), "0"), ".")
}

// textAlignStyle is the text alignment extension's contribution to a paragraph or a heading. It is a style declaration, and this pipeline drops every style, so it never reaches the stored HTML — it is here because the extension writes it.
func textAlignStyle(attrs map[string]any) *attrList {
	out := newAttrs()
	if alignment := asString(attrs["textAlign"]); alignment != "" {
		out.set("style", "text-align: "+alignment)
	}
	return out
}

func renderParagraph(node Node) spec {
	options := newAttrs("class", "editor-paragraph-block")
	return element("p", mergeAttributes(options, textAlignStyle(node.Attrs)), hole())
}

// renderHeading picks the tag from the level. A level the extension does not offer falls back to the first one it does, which is why a heading can never render as anything but h1 to h6.
func renderHeading(node Node) spec {
	level := 1
	switch value := node.Attrs["level"].(type) {
	case float64:
		level = int(value)
	case int64:
		level = int(value)
	case int:
		level = value
	}
	if level < 1 || level > 6 {
		level = 1
	}
	options := newAttrs("class", "editor-heading-block")
	return element(fmt.Sprintf("h%d", level), mergeAttributes(options, textAlignStyle(node.Attrs)), hole())
}

// renderOrderedList leaves the start attribute out when the list starts at one, which is every list nobody has renumbered.
func renderOrderedList(node Node) spec {
	options := newAttrs("class", "list-decimal pl-7 space-y-(--list-spacing-y)")
	attrs := attrsInOrder(node.Attrs, "start", "type")
	if attrs.get("start") == "1" {
		attrs = attrsInOrder(node.Attrs, "type")
	}
	return element("ol", mergeAttributes(options, attrs), hole())
}

// renderHorizontalRule has no content hole, so a rule never carries children even though its spec has an inner element.
func renderHorizontalRule(Node) spec {
	options := newAttrs("class", "py-4 border-strong-1")
	attrs := mergeAttributes(options, newAttrs("data-type", "horizontalRule"))
	return element("div", attrs, element("div", newAttrs()))
}

// renderCodeBlock puts the language on the inner code element rather than on the pre, and the language attribute itself is never written out as an attribute.
func renderCodeBlock(node Node) spec {
	var class any
	if language := asString(node.Attrs["language"]); language != "" {
		class = "language-" + language
	}
	return element("pre", newAttrs(), element("code", newAttrs("class", class), hole()))
}

func renderImage(node Node) spec {
	return element("img", mergeAttributes(attrsInOrder(node.Attrs, "src", "alt", "title", "width", "height", "aspectRatio", "alignment")))
}

func renderImageComponent(node Node) spec {
	return element("image-component", mergeAttributes(attrsInOrder(node.Attrs, "src", "alt", "title", "id", "width", "height", "aspectRatio", "alignment", "status")))
}

func renderMention(node Node) spec {
	return element("mention-component", mergeAttributes(attrsInOrder(node.Attrs, "id", "entity_identifier", "entity_name")))
}

func renderWorkItemEmbed(node Node) spec {
	return element("issue-embed-component", mergeAttributes(attrsInOrder(node.Attrs, "entity_identifier", "project_identifier", "workspace_identifier", "id", "entity_name")))
}

func renderCallout(node Node) spec {
	names := []string{"id", "data-icon-color", "data-icon-name", "data-emoji-unicode", "data-emoji-url", "data-logo-in-use", "data-background", "data-block-type"}
	return element("div", mergeAttributes(attrsInOrder(node.Attrs, names...)), hole())
}

func renderTaskList(Node) spec {
	options := newAttrs("class", "not-prose pl-2 space-y-2")
	return element("ul", mergeAttributes(options, newAttrs("data-type", "taskList")), hole())
}

// renderTaskItem writes data-checked as a bare attribute when the item is ticked and leaves it out entirely when it is not, because that is what a boolean attribute value turns into.
func renderTaskItem(node Node) spec {
	checked, _ := node.Attrs["checked"].(bool)
	options := newAttrs("class", "flex")
	attrs := mergeAttributes(options, newAttrs("data-checked", checked), newAttrs("data-type", "taskItem"))
	var checkedValue any
	if checked {
		checkedValue = "checked"
	}
	label := element("label", nil,
		element("input", newAttrs("type", "checkbox", "checked", checkedValue)),
		element("span", nil),
	)
	return element("li", attrs, label, element("div", nil, hole()))
}

func renderTable(Node) spec {
	return element("table", newAttrs(), element("tbody", nil, hole()))
}

// renderTableRow, renderTableHeader and renderTableCell each build a style declaration out of the colour attributes. Every one of them is dropped, so a coloured row or cell keeps its colour in the document and loses it in the stored HTML.
func renderTableRow(node Node) spec {
	attrs := attrsInOrder(node.Attrs, "background", "textColor")
	style := ""
	if background := attrs.get("background"); background != nil && background != "" {
		style = fmt.Sprintf("background-color: %s; color: %s", asString(background), asString(attrs.get("textColor")))
	}
	return element("tr", mergeAttributes(attrs, newAttrs("style", style)), hole())
}

func renderTableHeader(node Node) spec {
	attrs := attrsInOrder(node.Attrs, "colspan", "rowspan", "colwidth", "background")
	style := fmt.Sprintf("background-color: %s;", asString(attrValue(node.Attrs["background"])))
	return element("th", mergeAttributes(attrs, newAttrs("style", style)), hole())
}

func renderTableCell(node Node) spec {
	attrs := attrsInOrder(node.Attrs, "colspan", "rowspan", "colwidth", "background", "textColor")
	style := fmt.Sprintf("background-color: %s; color: %s;", asString(attrValue(node.Attrs["background"])), asString(attrValue(node.Attrs["textColor"])))
	return element("td", mergeAttributes(attrs, newAttrs("style", style)), hole())
}

// emojiTable maps a shortcode to its character, generated by tools/generate_ydoc_emoji.mjs from the emoji list the editor is configured with.
//
//go:embed emoji.tsv
var emojiTSV []byte

var emojiByShortcode = loadEmojiTable()

func loadEmojiTable() map[string]string {
	table := map[string]string{}
	scanner := bufio.NewScanner(bytes.NewReader(emojiTSV))
	for scanner.Scan() {
		key, emoji, found := strings.Cut(scanner.Text(), "\t")
		if !found {
			continue
		}
		table[key] = emoji
	}
	return table
}

// renderEmoji writes the character when the shortcode is one the editor knows and the shortcode back between colons when it is not.
func renderEmoji(node Node) spec {
	name := asString(node.Attrs["name"])
	attrs := mergeAttributes(newAttrs("data-name", attrValue(node.Attrs["name"])), newAttrs("data-type", "emoji"))
	emoji, known := emojiByShortcode[name]
	if !known {
		return element("span", attrs, literal(":"+name+":"))
	}
	return element("span", attrs, literal(emoji))
}
