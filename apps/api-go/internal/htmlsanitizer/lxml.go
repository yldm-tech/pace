package htmlsanitizer

import (
	"errors"
	"strings"

	"golang.org/x/net/html"
)

// ErrEmptyDocument is lxml's ParserError for markup that parses to nothing. The external API turns it into "Invalid HTML passed", so an empty description is refused rather than stored.
var ErrEmptyDocument = errors.New("htmlsanitizer: document is empty")

// RoundTrip is lxml.html.fromstring followed by tostring, which the external API's work item serializer runs over description_html before the sanitizer ever sees it. What gets stored is libxml2's reading of the markup rather than the markup the caller sent.
//
// fromstring does not return a fragment. It parses a whole document and then decides what to hand back: the single element the body holds, or the body itself retagged as a div or a span depending on whether a block-level tag is in there, or — when nothing reached the body at all — the document. Markup that parses to nothing is an error rather than an empty string, which is how an empty description comes to be refused.
func RoundTrip(content string) (string, error) {
	document, err := html.Parse(strings.NewReader(content))
	if err != nil {
		return "", err
	}
	root := documentElement(document)
	if root == nil {
		return "", ErrEmptyDocument
	}
	dropImpliedTableSections(root, content)
	head, body := headAndBody(root)
	// A caller who sends a whole document gets the whole document back. lxml decides that from the first fifty characters, before any parsing.
	if looksLikeADocument(content) {
		return renderDocument(root), nil
	}
	if body == nil || !holdsAnything(body) {
		if head == nil || !holdsAnything(head) {
			return "", ErrEmptyDocument
		}
		return renderDocument(root), nil
	}

	children := significantChildren(body)
	// One child and nothing loose around it means the caller sent one element, which is handed back on its own.
	if len(children) == 1 && isBlank(leadingText(body)) && isBlank(trailingText(body)) {
		return renderNodeWithTail(children[0]), nil
	}
	wrapper := "span"
	if holdsABlockTag(body) {
		wrapper = "div"
	}
	return "<" + wrapper + ">" + renderChildNodes(body) + "</" + wrapper + ">", nil
}

// dropImpliedTableSections unwraps the tbody the HTML5 parser puts around a table's rows. libxml2 implies no such element, so a table written without one keeps its rows directly under the table.
//
// Whether the element was implied cannot be read off the parsed tree, so it is read off the markup: a fragment that never says tbody cannot have one in its output. A fragment that says it once and leaves it out of a second table keeps both, which is the one case this does not follow lxml on — and is not markup any editor produces.
func dropImpliedTableSections(root *html.Node, content string) {
	if strings.Contains(strings.ToLower(content), "<tbody") {
		return
	}
	var walk func(node *html.Node)
	walk = func(node *html.Node) {
		child := node.FirstChild
		for child != nil {
			next := child.NextSibling
			walk(child)
			if child.Type == html.ElementNode && child.Data == "tbody" {
				unwrap(child)
			}
			child = next
		}
	}
	walk(root)
}

// unwrap lifts a node's children into its place and takes the node out.
func unwrap(node *html.Node) {
	parent := node.Parent
	for child := node.FirstChild; child != nil; {
		next := child.NextSibling
		node.RemoveChild(child)
		parent.InsertBefore(child, node)
		child = next
	}
	parent.RemoveChild(node)
}

// looksLikeADocument is fromstring's own shortcut: it reads the first fifty characters, trims the space off the front and lowercases them.
func looksLikeADocument(content string) bool {
	start := content
	if len(start) > 50 {
		start = start[:50]
	}
	start = strings.ToLower(strings.TrimLeft(start, " \t\r\n\f\v"))
	return strings.HasPrefix(start, "<html") || strings.HasPrefix(start, "<!doctype")
}

func documentElement(document *html.Node) *html.Node {
	for child := document.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.ElementNode && child.Data == "html" {
			return child
		}
	}
	return nil
}

func headAndBody(root *html.Node) (*html.Node, *html.Node) {
	var head, body *html.Node
	for child := root.FirstChild; child != nil; child = child.NextSibling {
		if child.Type != html.ElementNode {
			continue
		}
		switch child.Data {
		case "head":
			head = child
		case "body":
			body = child
		}
	}
	return head, body
}

// holdsAnything reports whether a node has content of its own. libxml2 makes no head or body element when there is nothing to put in one, and the Go parser makes both regardless, so an empty one has to be read as absent.
func holdsAnything(node *html.Node) bool {
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.TextNode {
			if !isBlank(child.Data) {
				return true
			}
			continue
		}
		return true
	}
	return false
}

// significantChildren counts what lxml counts as a child: an element or a comment, but not a run of text.
func significantChildren(node *html.Node) []*html.Node {
	children := []*html.Node{}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.ElementNode || child.Type == html.CommentNode {
			children = append(children, child)
		}
	}
	return children
}

// leadingText is the body's own text, which is everything before its first child.
func leadingText(node *html.Node) string {
	text := ""
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if child.Type != html.TextNode {
			break
		}
		text += child.Data
	}
	return text
}

// trailingText is the tail of the last child, which is what a single element keeps when it is handed back on its own. Whitespace there survives even though the whitespace in front of it does not.
func trailingText(node *html.Node) string {
	text := ""
	for child := node.LastChild; child != nil; child = child.PrevSibling {
		if child.Type != html.TextNode {
			break
		}
		text = child.Data + text
	}
	return text
}

// holdsABlockTag is _contains_block_level_tag, which decides between the two wrappers. It looks at every descendant rather than only the children.
func holdsABlockTag(node *html.Node) bool {
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if child.Type != html.ElementNode {
			continue
		}
		if blockTags[child.Data] || holdsABlockTag(child) {
			return true
		}
	}
	return false
}

// blockTags is lxml.html.defs.block_tags.
var blockTags = map[string]bool{
	"address": true, "blockquote": true, "caption": true, "center": true, "col": true,
	"colgroup": true, "dd": true, "del": true, "dir": true, "div": true, "dl": true,
	"dt": true, "fieldset": true, "form": true, "h1": true, "h2": true, "h3": true,
	"h4": true, "h5": true, "h6": true, "hr": true, "ins": true, "isindex": true,
	"legend": true, "li": true, "menu": true, "noscript": true, "ol": true,
	"optgroup": true, "option": true, "p": true, "pre": true, "table": true,
	"tbody": true, "td": true, "tfoot": true, "th": true, "thead": true, "tr": true,
	"ul": true,
}

// emptyTags is the set libxml2 writes without a closing tag.
var emptyTags = map[string]bool{
	"area": true, "base": true, "basefont": true, "br": true, "col": true, "embed": true,
	"frame": true, "hr": true, "img": true, "input": true, "isindex": true, "link": true,
	"meta": true, "param": true, "source": true, "track": true, "wbr": true,
}

// rawTextTags hold text that is written back as it stands, so a comparison inside a script is not turned into an entity.
var rawTextTags = map[string]bool{"script": true, "style": true}

// renderDocument writes the html element, dropping a head or a body that holds nothing — libxml2 never made one in the first place.
func renderDocument(root *html.Node) string {
	builder := &strings.Builder{}
	builder.WriteString("<html>")
	for child := root.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.ElementNode && (child.Data == "head" || child.Data == "body") && !holdsAnything(child) {
			continue
		}
		renderLxmlNode(builder, child, false)
	}
	builder.WriteString("</html>")
	return builder.String()
}

// renderNodeWithTail writes an element and the text that follows it, which is the tail tostring carries along.
func renderNodeWithTail(node *html.Node) string {
	builder := &strings.Builder{}
	renderLxmlNode(builder, node, false)
	for sibling := node.NextSibling; sibling != nil; sibling = sibling.NextSibling {
		if sibling.Type != html.TextNode {
			break
		}
		builder.WriteString(escapeLxmlText(sibling.Data, false))
	}
	return builder.String()
}

func renderChildNodes(node *html.Node) string {
	builder := &strings.Builder{}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		renderLxmlNode(builder, child, rawTextTags[node.Data])
	}
	return builder.String()
}

func renderLxmlNode(builder *strings.Builder, node *html.Node, raw bool) {
	switch node.Type {
	case html.TextNode:
		builder.WriteString(escapeLxmlText(node.Data, raw))
	case html.CommentNode:
		builder.WriteString("<!--" + node.Data + "-->")
	case html.ElementNode:
		builder.WriteString("<" + node.Data)
		for _, attribute := range node.Attr {
			builder.WriteString(" " + attribute.Key + "=" + quoteLxmlAttribute(attribute.Val))
		}
		builder.WriteString(">")
		if emptyTags[node.Data] {
			return
		}
		builder.WriteString(renderChildNodes(node))
		builder.WriteString("</" + node.Data + ">")
	}
}

// escapeLxmlText escapes the three characters libxml2 escapes in text. Inside a script or a style nothing is escaped, so a comparison written there survives.
func escapeLxmlText(text string, raw bool) string {
	if raw {
		return text
	}
	replaced := strings.ReplaceAll(text, "&", "&amp;")
	replaced = strings.ReplaceAll(replaced, "<", "&lt;")
	return strings.ReplaceAll(replaced, ">", "&gt;")
}

// quoteLxmlAttribute picks the quote libxml2 picks: a value holding a double quote and no single one is written in single quotes rather than escaped.
func quoteLxmlAttribute(value string) string {
	escaped := strings.ReplaceAll(value, "&", "&amp;")
	escaped = strings.ReplaceAll(escaped, "<", "&lt;")
	if strings.Contains(escaped, `"`) && !strings.Contains(escaped, "'") {
		return "'" + escaped + "'"
	}
	return `"` + strings.ReplaceAll(escaped, `"`, "&quot;") + `"`
}

func isBlank(value string) bool {
	return strings.TrimSpace(value) == ""
}
