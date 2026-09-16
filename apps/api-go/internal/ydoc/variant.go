package ydoc

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
)

// Variant is one of the editor's two schemas, with the parse rules that go with it.
//
// A page is written with the document one; everything else — a work item's description, a comment — with the rich text one, which is the same list without the work item embed. Which one a piece of HTML is read with decides whether an embed in it survives.
type Variant struct {
	Schema *Schema
	Rules  *ParseRules
	name   string
}

// Document and RichText are the two.
var (
	Document = Variant{Schema: DocumentSchema, Rules: DocumentParseRules, name: "document"}
	RichText = Variant{Schema: RichTextSchema, Rules: RichTextParseRules, name: "rich"}
)

// Name is the variant's name as the request that asks for it spells it.
func (v Variant) Name() string { return v.name }

// VariantFor returns the variant a request named, and refuses anything else.
func VariantFor(name string) (Variant, error) {
	switch name {
	case "document":
		return Document, nil
	case "rich":
		return RichText, nil
	default:
		return Variant{}, fmt.Errorf("ydoc: invalid variant provided: %s", name)
	}
}

// Formats is what a piece of content is stored as: the three columns a page and a work item description both keep.
type Formats struct {
	DescriptionJSON   json.RawMessage `json:"description_json"`
	DescriptionHTML   string          `json:"description_html"`
	DescriptionBinary string          `json:"description_binary"`
}

// ConvertHTML turns HTML into all three representations.
//
// It is not simply a parse and a render. The HTML is read into a document, the document is written out as a Yjs update, and the three formats are then derived from **that update** rather than from the document that was read — which is what the editor does, and is the reason the HTML that comes back is not always the HTML that went in.
func ConvertHTML(html string, variant Variant) (Formats, error) {
	document, err := ParseHTMLWithSchema(html, variant.Schema, variant.Rules)
	if err != nil {
		return Formats{}, err
	}
	update, err := ToUpdateWithSchema(document, nil, variant.Schema, 0)
	if err != nil {
		return Formats{}, err
	}
	return FormatsFor(update, variant)
}

// FormatsFor derives the three representations from an update.
func FormatsFor(update []byte, variant Variant) (Formats, error) {
	document, err := ParseWithSchema(update, variant.Schema)
	if err != nil {
		return Formats{}, err
	}
	html, err := HTMLWithSchema(document, variant.Schema)
	if err != nil {
		return Formats{}, err
	}
	encoded, err := document.JSON()
	if err != nil {
		return Formats{}, err
	}
	return Formats{
		DescriptionJSON:   json.RawMessage(encoded),
		DescriptionHTML:   html,
		DescriptionBinary: base64.StdEncoding.EncodeToString(update),
	}, nil
}
