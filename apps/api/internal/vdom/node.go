// Package vdom is the document object model the editor parses HTML into.
//
// It is deliberately not an HTML5 tree builder. The editor runs on zeed-dom, whose parser is a scanner over the markup with a stack of open elements and none of HTML5's repair rules: no implied `tbody`, no foster parenting, no reopening of formatting elements. A closing tag pops whatever is open, whether or not it is the tag being closed.
//
// Reproducing that is the point. A document read by a parser that repairs more than zeed-dom does is a different document, and the difference lands in what a page's HTML converts back into.
package vdom

import (
	"regexp"
	"strings"
)

// NodeType says which of the two kinds of node this is.
type NodeType int

// The two kinds of node that survive parsing. Comments are dropped as they are read.
const (
	ElementNode NodeType = iota
	TextNode
)

// Attribute is one attribute, keeping the order it was written in because that is the order it is read back in.
type Attribute struct {
	Name string
	// Value is the attribute's value. Bare is true for an attribute written with no value at all, which the parser records as present rather than empty.
	Value string
	Bare  bool
}

// Node is an element or a run of text.
type Node struct {
	Type NodeType

	// TagName is the element's name as it was written, with its case intact.
	TagName string
	Attrs   []Attribute

	// Text is the content of a text node.
	Text string

	Parent   *Node
	Children []*Node
}

// NewFragment is the root a parsed document hangs from. It is not an element, so it has no tag of its own.
func NewFragment() *Node {
	return &Node{Type: ElementNode}
}

// IsFragment reports whether the node is the parse root rather than an element.
func (n *Node) IsFragment() bool { return n.Type == ElementNode && n.TagName == "" }

// Tag is the element's name in lower case, which is what a parse rule matches against.
func (n *Node) Tag() string { return strings.ToLower(n.TagName) }

// Attr returns an attribute's value and whether it is present at all. A bare attribute is present with an empty value.
func (n *Node) Attr(name string) (string, bool) {
	for _, attr := range n.Attrs {
		if strings.EqualFold(attr.Name, name) {
			return attr.Value, true
		}
	}
	return "", false
}

// HasAttr reports whether the attribute is present, however it was written.
func (n *Node) HasAttr(name string) bool {
	_, ok := n.Attr(name)
	return ok
}

// AttrOr returns an attribute's value, or the fallback when it is absent.
func (n *Node) AttrOr(name, fallback string) string {
	if value, ok := n.Attr(name); ok {
		return value
	}
	return fallback
}

// Append adds a child and records who its parent is.
func (n *Node) Append(child *Node) {
	child.Parent = n
	n.Children = append(n.Children, child)
}

// LastChild is the last child, or nil when there is none.
func (n *Node) LastChild() *Node {
	if len(n.Children) == 0 {
		return nil
	}
	return n.Children[len(n.Children)-1]
}

// TextContent is every bit of text under the node, concatenated.
func (n *Node) TextContent() string {
	if n.Type == TextNode {
		return n.Text
	}
	var out strings.Builder
	for _, child := range n.Children {
		out.WriteString(child.TextContent())
	}
	return out.String()
}

// Classes is the element's class attribute split on whitespace.
func (n *Node) Classes() []string {
	value, _ := n.Attr("class")
	return strings.Fields(value)
}

// HasClass reports whether one of the element's classes is the named one.
func (n *Node) HasClass(name string) bool {
	for _, class := range n.Classes() {
		if class == name {
			return true
		}
	}
	return false
}

// styleDeclaration matches one declaration inside a style attribute, the way zeed-dom's own style property reads it: a name of word characters and hyphens, a colon, and then either a url() run or everything up to the next semicolon.
var styleDeclaration = regexp.MustCompile(`(?i)\s*([\w-]+)\s*:\s*(url\(.*?\)[^;]*|[^;]+)`)

// tagStyleDefaults is the presentational styling zeed-dom hands an element whether or not it carries a style attribute. It is what makes a bare <b> answer "bold" when asked for its font weight.
//
// Two of these are not CSS at all — the decoration property is spelled in the plural — and that is reproduced rather than corrected, because a rule matching on the real spelling would find nothing here either.
var tagStyleDefaults = map[string]map[string]string{
	"b":      {"fontWeight": "bold"},
	"strong": {"fontWeight": "bold"},
	"em":     {"fontStyle": "italic"},
	"i":      {"fontStyle": "italic"},
	"mark":   {"backgroundColor": "rgb(255, 250, 165)"},
	"ins":    {"backgroundColor": "rgb(255, 250, 165)"},
	"u":      {"textDecorations": "underline"},
	"a":      {"textDecorations": "underline"},
	"s":      {"textDecorations": "line-through"},
	"del":    {"textDecorations": "line-through"},
	"strike": {"textDecorations": "line-through"},
}

// styleDeclarations parses the style attribute into a map keyed both by the name as written and by its camel case spelling, which is how zeed-dom stores them.
func (n *Node) styleDeclarations() (map[string]string, int) {
	declarations := map[string]string{}
	attribute, ok := n.Attr("style")
	if !ok {
		return declarations, 0
	}
	count := 0
	for _, match := range styleDeclaration.FindAllStringSubmatch(attribute, -1) {
		count++
		name, value := match[1], strings.TrimSpace(match[2])
		declarations[name] = value
		declarations[toCamelCase(name)] = value
	}
	return declarations, count
}

// StyleCount is how many declarations the style attribute holds. It is what the parser checks before it reads any of them, and it deliberately does not count the defaults a tag carries.
func (n *Node) StyleCount() int {
	_, count := n.styleDeclarations()
	return count
}

// StyleProperty is the value of one declaration in the style attribute, empty when there is none. The tag defaults are not consulted, matching the accessor the parser reads styles through.
func (n *Node) StyleProperty(name string) string {
	declarations, _ := n.styleDeclarations()
	return declarations[name]
}

// StyleValue is the value a rule sees when it reads a style off the element directly: the declaration when there is one, and the tag's default otherwise.
func (n *Node) StyleValue(name string) string {
	declarations, _ := n.styleDeclarations()
	if value, ok := declarations[name]; ok {
		return value
	}
	return tagStyleDefaults[n.Tag()][name]
}

// toCamelCase is zeed-dom's: lower case throughout, and every run of characters that are neither letters nor digits is dropped with the character after it raised.
func toCamelCase(value string) string {
	lowered := strings.ToLower(value)
	var out strings.Builder
	upperNext := false
	for i, r := range lowered {
		isWord := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if !isWord {
			// A trailing run with nothing after it is dropped entirely, which is what a replacement needing a following character does.
			if i < len(lowered) {
				upperNext = true
			}
			continue
		}
		if upperNext {
			out.WriteRune(r - 32)
			upperNext = false
			continue
		}
		out.WriteRune(r)
	}
	return out.String()
}
