package ydoc

import "encoding/json"

// Node is one ProseMirror node.
//
// Attrs distinguishes nil from empty, because ProseMirror's own toJSON does: a type that declares no attributes produces a node with no attrs key at all, while a type that declares attributes always produces the key even when every value is dropped, leaving an empty object. Content and Marks are plain slices, absent when empty.
type Node struct {
	Type    string
	Attrs   map[string]any
	Content []Node
	Marks   []Mark
	Text    string
}

// Mark is one ProseMirror mark, following the same attrs rule as Node.
type Mark struct {
	Type  string
	Attrs map[string]any
}

// MarshalJSON writes the node the way ProseMirror's Node.toJSON does, which is the shape the API stores in description_json.
func (n Node) MarshalJSON() ([]byte, error) {
	out := make(map[string]any, 5)
	out["type"] = n.Type
	if n.Attrs != nil {
		out["attrs"] = n.Attrs
	}
	if len(n.Content) > 0 {
		out["content"] = n.Content
	}
	if len(n.Marks) > 0 {
		out["marks"] = n.Marks
	}
	if n.Text != "" {
		out["text"] = n.Text
	}
	return json.Marshal(out)
}

// MarshalJSON writes the mark the way ProseMirror's Mark.toJSON does.
func (m Mark) MarshalJSON() ([]byte, error) {
	out := make(map[string]any, 2)
	out["type"] = m.Type
	if m.Attrs != nil {
		out["attrs"] = m.Attrs
	}
	return json.Marshal(out)
}

// JSON renders the node the way the API stores it.
func (n Node) JSON() ([]byte, error) {
	return json.Marshal(n)
}

// TextContent is the node's text with every bit of structure dropped. It is not how a page's title becomes a string — see TitleHTML, which goes through the rendered HTML because the editor does.
func (n Node) TextContent() string {
	if n.Text != "" {
		return n.Text
	}
	out := ""
	for _, child := range n.Content {
		out += child.TextContent()
	}
	return out
}
