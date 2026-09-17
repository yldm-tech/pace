package ydoc

import (
	"strings"
	"testing"

	"github.com/reearth/ygo/crdt"
)

// buildUpdate assembles a document by hand, for the shapes the editor cannot be made to produce.
func buildUpdate(t *testing.T, build func(*crdt.Transaction, *crdt.YXmlFragment)) []byte {
	t.Helper()
	doc := crdt.New()
	fragment := doc.GetXmlFragment(BodyFragment)
	doc.Transact(func(txn *crdt.Transaction) { build(txn, fragment) })
	return doc.EncodeStateAsUpdate()
}

func TestParseRejectsUnknownNodeType(t *testing.T) {
	update := buildUpdate(t, func(txn *crdt.Transaction, fragment *crdt.YXmlFragment) {
		fragment.InsertElement(txn, 0, crdt.NewYXmlElement("somethingElse"))
	})
	_, err := Parse(update)
	if err == nil || !strings.Contains(err.Error(), "somethingElse") {
		t.Fatalf("err = %v, want one naming the unknown node type", err)
	}
}

func TestParseRejectsUnknownMarkType(t *testing.T) {
	update := buildUpdate(t, func(txn *crdt.Transaction, fragment *crdt.YXmlFragment) {
		paragraph := crdt.NewYXmlElement("paragraph")
		fragment.InsertElement(txn, 0, paragraph)
		text := crdt.NewYXmlText()
		paragraph.InsertText(txn, 0, text)
		text.Insert(txn, 0, "marked", crdt.Attributes{"sparkle": map[string]any{}})
	})
	_, err := Parse(update)
	if err == nil || !strings.Contains(err.Error(), "sparkle") {
		t.Fatalf("err = %v, want one naming the unknown mark type", err)
	}
}

// TestParseRejectsMissingRequiredAttribute covers the third state an attribute can be in: declared with no default, so the document has to carry it. No extension in the document editor declares one, hence the hand-built schema.
func TestParseRejectsMissingRequiredAttribute(t *testing.T) {
	schema := &Schema{
		TopNode: "doc",
		Nodes: []NodeType{
			{Name: "doc"},
			{Name: "paragraph", Attrs: map[string]Attribute{"lang": {HasDefault: false}}},
		},
	}
	schema.nodesByName = map[string]*NodeType{"doc": &schema.Nodes[0], "paragraph": &schema.Nodes[1]}
	schema.marksByName = map[string]*MarkType{}
	schema.markRank = map[string]int{}

	update := buildUpdate(t, func(txn *crdt.Transaction, fragment *crdt.YXmlFragment) {
		fragment.InsertElement(txn, 0, crdt.NewYXmlElement("paragraph"))
	})
	_, err := ParseWithSchema(update, schema)
	if err == nil || !strings.Contains(err.Error(), "lang") {
		t.Fatalf("err = %v, want one naming the missing attribute", err)
	}
}

func TestParseRejectsMalformedUpdate(t *testing.T) {
	if _, err := Parse([]byte{0xff, 0xff, 0xff, 0xff}); err == nil {
		t.Fatal("a malformed update parsed without error")
	}
}

// TestAttrsDistinguishNoneFromEmpty pins the two ways a node can end up without attributes, because they serialise differently.
func TestAttrsDistinguishNoneFromEmpty(t *testing.T) {
	declaresNone, err := Node{Type: "doc"}.JSON()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if got, want := string(declaresNone), `{"type":"doc"}`; got != want {
		t.Errorf("node with no declared attributes = %s, want %s", got, want)
	}

	allUndefined, err := Node{Type: "issue-embed-component", Attrs: map[string]any{}}.JSON()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if got, want := string(allUndefined), `{"attrs":{},"type":"issue-embed-component"}`; got != want {
		t.Errorf("node whose attributes are all undefined = %s, want %s", got, want)
	}
}
