// Package soup reads and writes html the way BeautifulSoup's html.parser does, which is what the asset copier stores back into a description.
//
// It is not the same as the html round trip in internal/htmlsanitizer. That one is libxml2's, reached through lxml, and it corrects the markup as a browser would: a paragraph is closed before a block element opens, a table's rows are moved into the table. Python's own html.parser corrects almost nothing — it keeps a stack, pushes on a start tag and pops to the matching name on an end tag — so `<p>a<div>b</div></p>` keeps the div inside the paragraph and `<p>a<p>b</p>` nests one paragraph inside the other. A description that goes through the asset copier comes out of this reading, so it has to be this reading.
package soup

import (
	"sort"
	"strings"

	"golang.org/x/net/html"
)

// NodeType says what one node in the tree is.
type NodeType int

const (
	// ElementNode is a tag.
	ElementNode NodeType = iota
	// TextNode is the text between tags.
	TextNode
	// CommentNode is an html comment.
	CommentNode
	// DoctypeNode is a doctype declaration, which bs4 writes back on a line of its own.
	DoctypeNode
)

// Attribute is one attribute, kept in the order it was written so a lookup reads the first one. The output sorts them by name instead, which is what bs4's formatter does.
type Attribute struct {
	Key   string
	Value string
}

// Node is one node in the parsed tree.
type Node struct {
	Type     NodeType
	Name     string
	Text     string
	Attr     []Attribute
	Children []*Node
}

// voidTags are HTMLTreeBuilder.empty_element_tags: the tags that never hold anything and are written self-closed. It is bs4's list rather than the html5 one, so `image`, `isindex`, `nextid` and `spacer` are on it.
var voidTags = map[string]bool{
	"area": true, "base": true, "basefont": true, "bgsound": true, "br": true, "col": true,
	"command": true, "embed": true, "frame": true, "hr": true, "image": true, "img": true,
	"input": true, "isindex": true, "keygen": true, "link": true, "menuitem": true, "meta": true,
	"nextid": true, "param": true, "source": true, "spacer": true, "track": true, "wbr": true,
}

// rawTextTags hold text bs4 writes back exactly as it read it, because their contents are not markup.
var rawTextTags = map[string]bool{"script": true, "style": true}

// listAttributes are DEFAULT_CDATA_LIST_ATTRIBUTES: attributes bs4 splits on whitespace and joins back with one space, so a value written with two spaces in it comes back with one.
var listAttributes = map[string]map[string]bool{
	"*":      {"accesskey": true, "class": true, "dropzone": true},
	"a":      {"rel": true, "rev": true},
	"link":   {"rel": true, "rev": true},
	"td":     {"headers": true},
	"th":     {"headers": true},
	"form":   {"accept-charset": true},
	"object": {"archive": true},
	"area":   {"rel": true},
	"icon":   {"sizes": true},
	"iframe": {"sandbox": true},
	"output": {"for": true},
}

// Parse reads markup into a tree the way BeautifulSoup(content, "html.parser") does.
//
// The tokens come from x/net/html's tokenizer, which agrees with python's on everything a description carries; the tree above them is bs4's, which is where the two really part company. An end tag with nothing open to match it is dropped, an end tag that matches something further down the stack closes everything above it too, and whatever is still open when the markup runs out is simply closed.
func Parse(content string) *Node {
	root := &Node{Type: ElementNode, Name: ""}
	stack := []*Node{root}
	tokenizer := html.NewTokenizer(strings.NewReader(content))

	for {
		switch tokenizer.Next() {
		case html.ErrorToken:
			return root
		case html.TextToken:
			appendChild(stack, &Node{Type: TextNode, Text: string(tokenizer.Text())})
		case html.CommentToken:
			appendChild(stack, &Node{Type: CommentNode, Text: string(tokenizer.Text())})
		case html.DoctypeToken:
			appendChild(stack, &Node{Type: DoctypeNode, Text: string(tokenizer.Text())})
		case html.StartTagToken:
			node := elementFrom(tokenizer)
			appendChild(stack, node)
			if !voidTags[node.Name] {
				stack = append(stack, node)
			}
		case html.SelfClosingTagToken:
			// handle_startendtag opens and closes in one step, so a custom tag written `<x/>` is written back as `<x></x>`.
			appendChild(stack, elementFrom(tokenizer))
		case html.EndTagToken:
			name, _ := tokenizer.TagName()
			stack = popToTag(stack, string(name))
		}
	}
}

// elementFrom reads one start tag's name and attributes.
func elementFrom(tokenizer *html.Tokenizer) *Node {
	name, hasAttributes := tokenizer.TagName()
	node := &Node{Type: ElementNode, Name: string(name)}
	for hasAttributes {
		var key, value []byte
		key, value, hasAttributes = tokenizer.TagAttr()
		attribute := Attribute{Key: string(key), Value: string(value)}
		if isListAttribute(node.Name, attribute.Key) {
			attribute.Value = strings.Join(strings.Fields(attribute.Value), " ")
		}
		// A repeated attribute keeps the first one, which is what a dict built by assignment in order would not do — but html.parser hands bs4 a list and bs4 builds the dict from it, keeping the last. The tokenizer here has already dropped the duplicates.
		node.Attr = append(node.Attr, attribute)
	}
	return node
}

func isListAttribute(tag, key string) bool {
	if listAttributes["*"][key] {
		return true
	}
	return listAttributes[tag][key]
}

func appendChild(stack []*Node, node *Node) {
	parent := stack[len(stack)-1]
	parent.Children = append(parent.Children, node)
}

// popToTag is bs4's _popToTag: close the most recent element with this name, and everything opened inside it. An end tag naming nothing on the stack is dropped.
func popToTag(stack []*Node, name string) []*Node {
	for index := len(stack) - 1; index > 0; index-- {
		if stack[index].Name == name {
			return stack[:index]
		}
	}
	return stack
}

// Render writes the tree back out the way str(soup) does.
func (node *Node) Render() string {
	var builder strings.Builder
	node.renderChildren(&builder, false)
	return builder.String()
}

func (node *Node) renderChildren(builder *strings.Builder, raw bool) {
	for _, child := range node.Children {
		child.render(builder, raw)
	}
}

func (node *Node) render(builder *strings.Builder, raw bool) {
	switch node.Type {
	case TextNode:
		if raw {
			builder.WriteString(node.Text)
			return
		}
		builder.WriteString(escapeText(node.Text))
	case CommentNode:
		builder.WriteString("<!--" + node.Text + "-->")
	case DoctypeNode:
		// bs4 writes a doctype on a line of its own.
		builder.WriteString("<!DOCTYPE " + node.Text + ">\n")
	case ElementNode:
		builder.WriteString("<" + node.Name)
		for _, attribute := range sortedAttributes(node.Attr) {
			builder.WriteString(" " + attribute.Key + "=" + quoteAttribute(attribute.Value))
		}
		if voidTags[node.Name] {
			builder.WriteString("/>")
			return
		}
		builder.WriteString(">")
		node.renderChildren(builder, rawTextTags[node.Name])
		builder.WriteString("</" + node.Name + ">")
	}
}

// sortedAttributes is what bs4's formatter does before writing a tag: the attributes come out in name order rather than the order they were written.
func sortedAttributes(attributes []Attribute) []Attribute {
	sorted := make([]Attribute, len(attributes))
	copy(sorted, attributes)
	sort.SliceStable(sorted, func(first, second int) bool {
		return sorted[first].Key < sorted[second].Key
	})
	return sorted
}

// escapeText is substitute_xml: the three characters that would otherwise be markup, and nothing else. A quote in text is left alone.
func escapeText(text string) string {
	replaced := strings.ReplaceAll(text, "&", "&amp;")
	replaced = strings.ReplaceAll(replaced, "<", "&lt;")
	return strings.ReplaceAll(replaced, ">", "&gt;")
}

// quoteAttribute is quoted_attribute_value: a value carrying a double quote and no single one is written in single quotes rather than escaped.
func quoteAttribute(value string) string {
	escaped := escapeText(value)
	if strings.Contains(escaped, `"`) {
		if strings.Contains(escaped, "'") {
			return `"` + strings.ReplaceAll(escaped, `"`, "&quot;") + `"`
		}
		return "'" + escaped + "'"
	}
	return `"` + escaped + `"`
}

// FindAll is soup.find_all(name): every element of that name, in the order they appear.
func (node *Node) FindAll(name string) []*Node {
	var found []*Node
	for _, child := range node.Children {
		if child.Type != ElementNode {
			continue
		}
		if child.Name == name {
			found = append(found, child)
		}
		found = append(found, child.FindAll(name)...)
	}
	return found
}

// Get reads an attribute, and reports whether the element carries it at all.
func (node *Node) Get(key string) (string, bool) {
	for _, attribute := range node.Attr {
		if attribute.Key == key {
			return attribute.Value, true
		}
	}
	return "", false
}

// Set writes an attribute, leaving it where it already was and appending it when it was not there.
func (node *Node) Set(key, value string) {
	for index, attribute := range node.Attr {
		if attribute.Key == key {
			node.Attr[index].Value = value
			return
		}
	}
	node.Attr = append(node.Attr, Attribute{Key: key, Value: value})
}
