package ydoc

import (
	"fmt"
	"html"
	"strings"
)

// HTML renders a document the way the editor does, which is what the API stores in description_html.
//
// This is ProseMirror's DOMSerializer walking the same schema, with zeed-dom's markup rules underneath it — the two the editor reaches through @tiptap/html.
func HTML(document Node) (string, error) {
	return HTMLWithSchema(document, DocumentSchema)
}

// HTMLWithSchema is HTML against a schema other than the document editor's.
func HTMLWithSchema(document Node, schema *Schema) (string, error) {
	return htmlWithSchema(document, schema)
}

// titleEscaper is what sanitize-html writes text back out with, which is a narrower set than the renderer used on the way in: a quote and an apostrophe survive as themselves, and so do the non-breaking space and the soft hyphen.
var titleEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

// TitleHTML reduces a page's title document to the string the API stores.
//
// The editor does this by rendering the title to HTML and then running the result through a sanitiser with no tag allowed, which is not the same as taking the document's text: the sanitiser decodes the entities the renderer wrote and escapes a narrower set on the way back out, and it trims. So a title holding an ampersand comes back holding an escaped one, and a title somebody typed "&amp;" into comes back doubly escaped.
func TitleHTML(title Node) (string, error) {
	rendered, err := htmlWithSchema(title, DocumentSchema)
	if err != nil {
		return "", err
	}
	// Every ampersand in the rendered HTML belongs to one of the seven escapes the renderer writes, so the decoder never meets a bare one.
	text := html.UnescapeString(stripTags(rendered))
	return strings.TrimFunc(titleEscaper.Replace(text), isJSWhitespace), nil
}

func htmlWithSchema(document Node, schema *Schema) (string, error) {
	var out strings.Builder
	if err := serializeFragment(&out, document.Content, schema); err != nil {
		return "", err
	}
	return out.String(), nil
}

// openMark is a mark whose element is currently open, with the text that closes it.
type openMark struct {
	mark  Mark
	close string
}

// serializeFragment walks a run of sibling nodes, opening and closing mark elements around them. A mark shared by several adjacent text nodes opens once and wraps them all, which is why the order of a text node's marks — the schema's registration order — decides the nesting.
func serializeFragment(out *strings.Builder, nodes []Node, schema *Schema) error {
	var active []openMark
	for _, node := range nodes {
		if len(active) > 0 || len(node.Marks) > 0 {
			keep, rendered := 0, 0
			for keep < len(active) && rendered < len(node.Marks) {
				next := node.Marks[rendered]
				markType := schema.MarkType(next.Type)
				if markType == nil {
					return fmt.Errorf("ydoc: schema has no mark type %q", next.Type)
				}
				if !markEqual(next, active[keep].mark) || !markType.Spanning {
					break
				}
				keep++
				rendered++
			}
			for len(active) > keep {
				out.WriteString(active[len(active)-1].close)
				active = active[:len(active)-1]
			}
			for rendered < len(node.Marks) {
				add := node.Marks[rendered]
				rendered++
				open, closing, err := markParts(add, schema)
				if err != nil {
					return err
				}
				active = append(active, openMark{mark: add, close: closing})
				out.WriteString(open)
			}
		}
		if err := serializeNode(out, node, schema); err != nil {
			return err
		}
	}
	for i := len(active) - 1; i >= 0; i-- {
		out.WriteString(active[i].close)
	}
	return nil
}

func serializeNode(out *strings.Builder, node Node, schema *Schema) error {
	nodeType := schema.NodeType(node.Type)
	if nodeType == nil {
		return fmt.Errorf("ydoc: schema has no node type %q", node.Type)
	}
	if nodeType.IsText {
		out.WriteString(escapeHTML(node.Text))
		return nil
	}
	render := nodeRenderers[node.Type]
	if render == nil {
		return fmt.Errorf("ydoc: no renderer for node type %q", node.Type)
	}
	rendered, holeAt, err := renderParts(render(node))
	if err != nil {
		return fmt.Errorf("ydoc: render %s: %w", node.Type, err)
	}
	// No hole means the spec has nowhere to put the node's children, and ProseMirror drops them rather than appending them somewhere.
	if holeAt < 0 {
		out.WriteString(rendered)
		return nil
	}
	if nodeType.IsLeaf {
		return fmt.Errorf("ydoc: content hole in the spec of leaf node %s", node.Type)
	}
	out.WriteString(rendered[:holeAt])
	if err := serializeFragment(out, node.Content, schema); err != nil {
		return err
	}
	out.WriteString(rendered[holeAt:])
	return nil
}

func markParts(mark Mark, schema *Schema) (open, closing string, err error) {
	render := markRenderers[mark.Type]
	if render == nil {
		return "", "", fmt.Errorf("ydoc: no renderer for mark type %q", mark.Type)
	}
	rendered, holeAt, err := renderMarkParts(render(mark))
	if err != nil {
		return "", "", fmt.Errorf("ydoc: render %s: %w", mark.Type, err)
	}
	if holeAt < 0 {
		return rendered, "", nil
	}
	return rendered[:holeAt], rendered[holeAt:], nil
}

func markEqual(a, b Mark) bool {
	if a.Type != b.Type || len(a.Attrs) != len(b.Attrs) {
		return false
	}
	for name, value := range a.Attrs {
		other, ok := b.Attrs[name]
		if !ok || !sameAttrValue(value, other) {
			return false
		}
	}
	return true
}

func sameAttrValue(a, b any) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	aList, aIsList := a.([]any)
	bList, bIsList := b.([]any)
	if aIsList || bIsList {
		if !aIsList || !bIsList || len(aList) != len(bList) {
			return false
		}
		for i := range aList {
			if !sameAttrValue(aList[i], bList[i]) {
				return false
			}
		}
		return true
	}
	return a == b
}

// stripTags takes the text out of rendered HTML without unescaping it, which is what the editor's own title helper does.
func stripTags(html string) string {
	var out strings.Builder
	depth := 0
	for _, r := range html {
		switch {
		case r == '<':
			depth++
		case r == '>' && depth > 0:
			depth--
		case depth == 0:
			out.WriteRune(r)
		}
	}
	return out.String()
}
