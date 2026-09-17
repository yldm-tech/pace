package ydoc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/reearth/ygo/crdt"
)

// ToUpdate turns a document into the Yjs update a page is stored as.
//
// It is the inverse of Parse, and it is how a page that has never been opened in the collaborative editor gets a document at all: the page's HTML is read, the result is written out as an update, and every client that connects afterwards synchronises against it.
func ToUpdate(document Node, title *Node) ([]byte, error) {
	return ToUpdateWithOptions(document, title, 0)
}

// ToUpdateWithOptions is ToUpdate with the document's client id fixed rather than drawn at random. A fixed id makes the bytes repeatable, which is what the corpus needs; zero leaves the id random, which is what a running service wants so that two servers writing the same page do not claim the same identity.
func ToUpdateWithOptions(document Node, title *Node, clientID uint64) ([]byte, error) {
	return ToUpdateWithSchema(document, title, DocumentSchema, clientID)
}

// ToUpdateWithSchema is ToUpdateWithOptions against a schema other than the document editor's. The schema decides the order a node's attributes go in, which is part of the bytes.
func ToUpdateWithSchema(document Node, title *Node, schema *Schema, clientID uint64) ([]byte, error) {
	var options []crdt.DocOption
	if clientID != 0 {
		options = append(options, crdt.WithClientID(crdt.ClientID(clientID)))
	}
	doc := crdt.New(options...)

	if err := writeFragment(doc, BodyFragment, document, schema); err != nil {
		return nil, err
	}
	if title != nil {
		if err := writeFragment(doc, TitleFragment, *title, schema); err != nil {
			return nil, err
		}
	}
	return doc.EncodeStateAsUpdate(), nil
}

// TitleDocument is the document a page's title is stored as: a level one heading holding the text, and nothing at all inside it when the title is empty.
func TitleDocument(title string) Node {
	heading := Node{Type: "heading", Attrs: map[string]any{"level": float64(1)}}

	if title != "" {
		heading.Content = []Node{{Type: "text", Text: title}}
	}
	return Node{Type: "doc", Content: []Node{heading}}
}

func writeFragment(doc *crdt.Doc, name string, document Node, schema *Schema) error {
	fragment := doc.GetXmlFragment(name)
	var err error
	doc.Transact(func(txn *crdt.Transaction) {
		err = insertChildren(txn, fragment, nil, document.Content, schema)
	})
	return err
}

// insertChildren writes a node's children. Runs of text are gathered into one text node the way the editor's own writer does, because a text node with several runs of formatting is one Yjs text with several formatting ranges rather than several texts.
func insertChildren(txn *crdt.Transaction, fragment *crdt.YXmlFragment, element *crdt.YXmlElement, children []Node, schema *Schema) error {
	index := 0
	for start := 0; start < len(children); {
		if children[start].Type != "text" {
			child, err := buildElement(txn, children[start], schema)
			if err != nil {
				return err
			}
			if fragment != nil {
				fragment.InsertElement(txn, index, child)
			} else {
				element.InsertElement(txn, index, child)
			}
			index++
			start++
			continue
		}

		end := start
		for end < len(children) && children[end].Type == "text" {
			end++
		}
		text := crdt.NewYXmlText()
		if fragment != nil {
			fragment.InsertText(txn, index, text)
		} else {
			element.InsertText(txn, index, text)
		}
		// Written as one delta rather than as a sequence of inserts. The two produce the same text and different bytes: a delta walks a cursor to the end and appends, where an insert at a position has to point at what follows it, and the items then carry a right neighbour the editor's never do.
		delta := make([]crdt.Delta, 0, end-start)
		for _, run := range children[start:end] {
			delta = append(delta, crdt.Delta{Op: crdt.DeltaOpInsert, Insert: run.Text, Attributes: marksToAttributes(run.Marks, schema)})
		}
		text.ApplyDelta(txn, delta)
		index++
		start = end
	}
	return nil
}

// buildElement writes one node. An attribute that is null is left out entirely, which is what makes a null in the document indistinguishable from an absent attribute when it is read back.
func buildElement(txn *crdt.Transaction, node Node, schema *Schema) (*crdt.YXmlElement, error) {
	if node.Type == "" {
		return nil, fmt.Errorf("ydoc: a node with no type cannot be written")
	}
	element := crdt.NewYXmlElement(node.Type)
	for _, name := range attributeOrder(node, schema) {
		value := node.Attrs[name]
		if value == nil || name == "ychange" {
			continue
		}
		element.SetAttributeValue(txn, name, anyValue(value))
	}
	if err := insertChildren(txn, nil, element, node.Content, schema); err != nil {
		return nil, err
	}
	return element, nil
}

// maxSafeInteger is the largest whole number JavaScript counts exactly, and the boundary Yjs uses to decide whether a number is written as an integer.
const maxSafeInteger = float64(1<<53 - 1)

// anyValue maps a value into the Go type that encodes the way JavaScript's would.
//
// JavaScript has one number type and Yjs writes a whole one as an integer and a fractional one as a float. Go has both types, and a number that arrived through JSON is a float whatever it holds — so a whole one is handed over as an integer here, where the two languages meet, rather than being written out as a float that the editor would never have written.
func anyValue(value any) any {
	switch typed := value.(type) {
	case float64:
		if typed == float64(int64(typed)) && typed <= maxSafeInteger && typed >= -maxSafeInteger {
			return int64(typed)
		}
		return typed
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = anyValue(item)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(typed))
		for name, item := range typed {
			out[name] = anyValue(item)
		}
		return out
	default:
		return value
	}
}

// attributeOrder is the order a node's attributes are written in: the order the schema declares them, since that is the order the editor's own writer walks. An attribute the schema does not declare is written after those, in name order, so that a document from somewhere else is still written the same way twice.
func attributeOrder(node Node, schema *Schema) []string {
	nodeType := schema.NodeType(node.Type)
	var order []string
	seen := map[string]bool{}
	if nodeType != nil {
		for _, name := range nodeType.AttrNames {
			if _, ok := node.Attrs[name]; ok {
				order = append(order, name)
				seen[name] = true
			}
		}
	}
	var extra []string
	for name := range node.Attrs {
		if !seen[name] {
			extra = append(extra, name)
		}
	}
	sort.Strings(extra)
	return append(order, extra...)
}

// marksToAttributes turns a text node's marks into the formatting the Yjs text carries. The whole of each mark's attributes goes over as one value, nulls and all, which is why a mark's null survives a round trip where a node's does not.
func marksToAttributes(marks []Mark, schema *Schema) crdt.Attributes {
	if len(marks) == 0 {
		return nil
	}
	attributes := make(crdt.Attributes, len(marks))
	for _, mark := range marks {
		if mark.Type == "ychange" {
			continue
		}
		attributes[mark.Type] = orderedJSON(mark, schema)
	}
	return attributes
}

// rawJSON is a value that is already JSON and is written out as it stands.
//
// A mark's attributes travel as a JSON string, and the order of its keys is part of the bytes. Go's encoder sorts a map's keys where JavaScript keeps the order the object was built in, which is the order the schema declares the attributes — so the JSON is built here, in that order, and handed over ready-made.
//
// It is a string rather than a byte slice because the value is compared as it is carried, and a slice cannot be compared at all.
type rawJSON string

// MarshalJSON writes the value through unchanged.
func (r rawJSON) MarshalJSON() ([]byte, error) { return []byte(r), nil }

// marshalJS encodes a value the way JavaScript's JSON.stringify does. Go's encoder escapes an ampersand and the angle brackets so that its output is safe to drop into a page; JavaScript's leaves them, and a link whose address carries a query string would otherwise be written differently here.
func marshalJS(value any) ([]byte, error) {
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	// Encode writes a trailing newline that Marshal does not.
	return bytes.TrimRight(out.Bytes(), "\n"), nil
}

// orderedJSON renders a mark's attributes as JSON with the keys in the order its type declares them.
func orderedJSON(mark Mark, schema *Schema) rawJSON {
	markType := schema.MarkType(mark.Type)
	if markType == nil || len(markType.AttrNames) == 0 {
		return "{}"
	}
	var out []byte
	out = append(out, '{')
	for i, name := range markType.AttrNames {
		value, ok := mark.Attrs[name]
		if !ok {
			continue
		}
		if i > 0 && len(out) > 1 {
			out = append(out, ',')
		}
		key, err := marshalJS(name)
		if err != nil {
			return "{}"
		}
		encoded, err := marshalJS(anyValue(value))
		if err != nil {
			return "{}"
		}
		out = append(out, key...)
		out = append(out, ':')
		out = append(out, encoded...)
	}
	out = append(out, '}')
	return rawJSON(out)
}

// EncodesLikeTheEditor reports whether writing the document out would give the same bytes the editor would.
//
// It is false for three reasons. All three are in the Yjs port underneath rather than in anything above it, none of them changes the document that comes back, and each is named here so that the difference is a fact somebody chose rather than a surprise.
//
//  1. An attribute whose value is JavaScript's undefined. The editor writes one for every attribute a type declares with an undefined default and the document does not carry, because its check lets undefined through where it stops null. The port has null and nothing else, so those attributes are left out. Reading back maps both to nothing and the schema's default applies, which is undefined again.
//  2. A run of text whose marks are not in alphabetical order. The editor opens the formatting markers in the order the marks are in, which is the schema's; the port sorts them by name, deliberately, because the order they are linked in is observable. The same markers, in a different order.
//  3. A mark attribute holding an ampersand or an angle bracket. A mark's attributes travel as JSON, and the port writes that JSON with Go's encoder, which escapes those three characters so its output is safe to drop into a page. JavaScript's leaves them. A link whose address carries a query string is the case this shows up in.
func EncodesLikeTheEditor(document Node) bool {
	if !marksAreInNameOrder(document.Marks) {
		return false
	}
	for _, mark := range document.Marks {
		if markAttributesNeedEscaping(mark) {
			return false
		}
	}
	nodeType := DocumentSchema.NodeType(document.Type)
	if nodeType != nil {
		for name, attr := range nodeType.Attrs {
			if !attr.HasDefault || !attr.DefaultUndefined {
				continue
			}
			if _, ok := document.Attrs[name]; !ok {
				return false
			}
		}
	}
	for _, child := range document.Content {
		if !EncodesLikeTheEditor(child) {
			return false
		}
	}
	return true
}

func marksAreInNameOrder(marks []Mark) bool {
	for i := 1; i < len(marks); i++ {
		if marks[i-1].Type > marks[i].Type {
			return false
		}
	}
	return true
}

func markAttributesNeedEscaping(mark Mark) bool {
	return strings.ContainsAny(string(orderedJSON(mark, DocumentSchema)), "&<>")
}

// utf16Length is how far a run of text advances the cursor, because Yjs counts positions in UTF-16 code units rather than in bytes or runes.
func utf16Length(text string) int {
	length := 0
	for _, r := range text {
		if r > 0xffff {
			length += 2
			continue
		}
		length++
	}
	return length
}
