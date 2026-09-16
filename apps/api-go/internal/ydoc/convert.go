package ydoc

import (
	"fmt"
	"sort"

	"github.com/reearth/ygo/crdt"
)

// BodyFragment is the name of the XML fragment holding a page's body, and TitleFragment the one holding its title. Both names come from the editor's collaboration extension.
const (
	BodyFragment  = "default"
	TitleFragment = "title"
)

// Parse rebuilds the document in a page's Yjs update. The update is the V1 encoding the editor and the live service exchange — the same bytes the API keeps in description_binary.
func Parse(update []byte) (Node, error) {
	return ParseWithSchema(update, DocumentSchema)
}

// ParseWithSchema is Parse against a schema other than the document editor's.
func ParseWithSchema(update []byte, schema *Schema) (Node, error) {
	doc := crdt.New()
	if err := crdt.ApplyUpdateV1(doc, update, nil); err != nil {
		return Node{}, fmt.Errorf("ydoc: apply update: %w", err)
	}
	return fragmentToNode(doc.GetXmlFragment(BodyFragment), schema)
}

// ParseTitle rebuilds the document in a page's title fragment. The title is not stored as a string: it is a whole document of its own, a level one heading holding the text, living in the same update beside the body.
func ParseTitle(update []byte) (Node, error) {
	return ParseTitleWithSchema(update, DocumentSchema)
}

// ParseTitleWithSchema is ParseTitle against a schema other than the document editor's.
func ParseTitleWithSchema(update []byte, schema *Schema) (Node, error) {
	doc := crdt.New()
	if err := crdt.ApplyUpdateV1(doc, update, nil); err != nil {
		return Node{}, fmt.Errorf("ydoc: apply update: %w", err)
	}
	return fragmentToNode(doc.GetXmlFragment(TitleFragment), schema)
}

// fragmentToNode is y-prosemirror's yXmlFragmentToProseMirrorRootNode: every child of the fragment becomes a child of the top node.
func fragmentToNode(fragment *crdt.YXmlFragment, schema *Schema) (Node, error) {
	content, err := childNodes(fragment.Children(), schema)
	if err != nil {
		return Node{}, err
	}
	top := schema.NodeType(schema.TopNode)
	attrs, err := computeAttrs(top.Attrs, nil, top.Name, true)
	if err != nil {
		return Node{}, err
	}
	return Node{Type: top.Name, Attrs: attrs, Content: content}, nil
}

// childNodes converts a run of Yjs children. Elements become nodes and text becomes one node per formatting run, matching how y-prosemirror walks the same list.
//
// The type parameter is there because ygo's Children returns a slice of an unexported interface, which has no name to write down here.
func childNodes[T any](children []T, schema *Schema) ([]Node, error) {
	var nodes []Node
	for _, child := range children {
		switch typed := any(child).(type) {
		case *crdt.YXmlElement:
			node, err := elementToNode(typed, schema)
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, node)
		case *crdt.YXmlText:
			texts, err := textToNodes(typed, schema)
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, texts...)
		default:
			return nil, fmt.Errorf("ydoc: unexpected %T in an xml fragment", child)
		}
	}
	return nodes, nil
}

func elementToNode(element *crdt.YXmlElement, schema *Schema) (Node, error) {
	nodeType := schema.NodeType(element.NodeName)
	if nodeType == nil {
		return Node{}, fmt.Errorf("ydoc: schema has no node type %q", element.NodeName)
	}
	content, err := childNodes(element.Children(), schema)
	if err != nil {
		return Node{}, err
	}
	attrs, err := computeAttrs(nodeType.Attrs, element.GetAttributeValues(), nodeType.Name, true)
	if err != nil {
		return Node{}, err
	}
	return Node{Type: nodeType.Name, Attrs: attrs, Content: content}, nil
}

// textToNodes turns one Yjs text node into the run of ProseMirror text nodes it represents: the delta breaks the text at every change of formatting, and each piece carries the marks that were in force over it.
func textToNodes(text *crdt.YXmlText, schema *Schema) ([]Node, error) {
	var nodes []Node
	for _, op := range text.ToDelta() {
		if op.Op != crdt.DeltaOpInsert {
			continue
		}
		body, ok := op.Insert.(string)
		if !ok {
			return nil, fmt.Errorf("ydoc: embedded %T in text is not supported", op.Insert)
		}
		marks, err := marksFrom(op.Attributes, schema)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, Node{Type: "text", Marks: marks, Text: body})
	}
	return nodes, nil
}

// marksFrom builds a text node's mark set. ProseMirror keeps a mark set sorted by the rank of each mark type, which is the order the types were registered in, so the order of the Yjs attributes is irrelevant.
func marksFrom(attributes crdt.Attributes, schema *Schema) ([]Mark, error) {
	if len(attributes) == 0 {
		return nil, nil
	}
	marks := make([]Mark, 0, len(attributes))
	for name, value := range attributes {
		markType := schema.MarkType(name)
		if markType == nil {
			return nil, fmt.Errorf("ydoc: schema has no mark type %q", name)
		}
		given, _ := value.(map[string]any)
		attrs, err := computeAttrs(markType.Attrs, given, markType.Name, false)
		if err != nil {
			return nil, err
		}
		marks = append(marks, Mark{Type: markType.Name, Attrs: attrs})
	}
	sort.Slice(marks, func(i, j int) bool {
		return schema.markRank[marks[i].Type] < schema.markRank[marks[j].Type]
	})
	return marks, nil
}
