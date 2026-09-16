package vdom

import (
	"encoding/json"
	"os"
	"testing"
)

// serialised is the shape the corpus records a tree in.
type serialised struct {
	Type     string       `json:"type"`
	Tag      string       `json:"tag,omitempty"`
	Attrs    []serialAttr `json:"attrs,omitempty"`
	Text     string       `json:"text,omitempty"`
	Children []serialised `json:"children,omitempty"`
}

type serialAttr struct {
	Name  string `json:"name"`
	Value string `json:"value"`
	Bare  bool   `json:"bare"`
}

type treeCase struct {
	Name string       `json:"name"`
	HTML string       `json:"html"`
	Tree []serialised `json:"tree"`
}

func serialise(node *Node) serialised {
	if node.Type == TextNode {
		return serialised{Type: "text", Text: node.Text}
	}
	out := serialised{Type: "element", Tag: node.TagName}
	for _, attr := range node.Attrs {
		out.Attrs = append(out.Attrs, serialAttr{Name: attr.Name, Value: attr.Value, Bare: attr.Bare})
	}
	for _, child := range node.Children {
		out.Children = append(out.Children, serialise(child))
	}
	return out
}

func loadTrees(t *testing.T) []treeCase {
	t.Helper()
	raw, err := os.ReadFile("testdata/trees.json")
	if err != nil {
		t.Fatalf("read corpus: %v", err)
	}
	var cases []treeCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatalf("parse corpus: %v", err)
	}
	return cases
}

// TestParseMatchesZeedDom is the whole point of the package: the same markup has to come apart into the same tree here as it does in the editor.
func TestParseMatchesZeedDom(t *testing.T) {
	for _, testCase := range loadTrees(t) {
		t.Run(testCase.Name, func(t *testing.T) {
			fragment := Parse(testCase.HTML)
			got := make([]serialised, 0, len(fragment.Children))
			for _, child := range fragment.Children {
				got = append(got, serialise(child))
			}
			want := testCase.Tree
			if want == nil {
				want = []serialised{}
			}
			// Compared as encodings rather than as values, because the two sides differ in whether an absent list is nil or empty and that difference is not a difference in the tree.
			gotJSON, _ := json.Marshal(got)
			wantJSON, _ := json.Marshal(want)
			if string(gotJSON) != string(wantJSON) {
				t.Errorf("tree differs\n go: %s\nwant: %s", gotJSON, wantJSON)
			}
		})
	}
}

// TestParentsAreLinked matters because every parse rule that looks at an element's context walks upwards.
func TestParentsAreLinked(t *testing.T) {
	fragment := Parse("<div><p><strong>x</strong></p></div>")
	div := fragment.Children[0]
	paragraph := div.Children[0]
	strong := paragraph.Children[0]
	if strong.Parent != paragraph || paragraph.Parent != div || div.Parent != fragment {
		t.Fatal("the tree is not linked upwards")
	}
	if !fragment.IsFragment() || div.IsFragment() {
		t.Error("the fragment is not the only fragment")
	}
}

func TestTagIsLowerCased(t *testing.T) {
	element := Parse("<DiV>x</DiV>").Children[0]
	if element.TagName != "DiV" {
		t.Errorf("the tag name lost its case: %q", element.TagName)
	}
	if element.Tag() != "div" {
		t.Errorf("tag = %q, want div", element.Tag())
	}
}

func TestAttributeHelpers(t *testing.T) {
	element := Parse(`<p class="one two" data-flag title="">x</p>`).Children[0]
	if value, ok := element.Attr("class"); !ok || value != "one two" {
		t.Errorf("class = %q, %v", value, ok)
	}
	if !element.HasAttr("data-flag") {
		t.Error("a bare attribute is not reported as present")
	}
	if value, ok := element.Attr("title"); !ok || value != "" {
		t.Errorf("an empty attribute should be present and empty, got %q %v", value, ok)
	}
	if element.HasAttr("nothing") {
		t.Error("an absent attribute is reported as present")
	}
	if element.AttrOr("nothing", "fallback") != "fallback" {
		t.Error("the fallback was not used")
	}
	if !element.HasClass("two") || element.HasClass("three") {
		t.Errorf("classes = %v", element.Classes())
	}
}

// TestStyleReading covers the three ways a style is asked for, because a parse rule uses all three: how many declarations there are, one declaration by name, and the value an element answers with when it has no declaration at all.
func TestStyleReading(t *testing.T) {
	styled := Parse(`<p style="text-align: center; color: red">x</p>`).Children[0]
	if got := styled.StyleCount(); got != 2 {
		t.Errorf("count = %d, want 2", got)
	}
	if got := styled.StyleProperty("text-align"); got != "center" {
		t.Errorf("text-align = %q", got)
	}
	if got := styled.StyleProperty("textAlign"); got != "center" {
		t.Errorf("textAlign = %q, want the camel case spelling to work too", got)
	}
	if got := styled.StyleProperty("font-weight"); got != "" {
		t.Errorf("font-weight = %q, want nothing", got)
	}

	// A url may hold a semicolon, and the declaration still runs to the end of it.
	withURL := Parse(`<div style="background: url(a.png?a=1;b=2) no-repeat; color: blue">x</div>`).Children[0]
	if got := withURL.StyleProperty("background"); got != "url(a.png?a=1;b=2) no-repeat" {
		t.Errorf("background = %q", got)
	}
	if got := withURL.StyleProperty("color"); got != "blue" {
		t.Errorf("color = %q", got)
	}

	// A bold element answers "bold" for its weight with no style attribute at all, and still reports no declarations.
	bold := Parse("<b>x</b>").Children[0]
	if got := bold.StyleValue("fontWeight"); got != "bold" {
		t.Errorf("fontWeight of a bare <b> = %q, want bold", got)
	}
	if got := bold.StyleCount(); got != 0 {
		t.Errorf("a bare <b> reports %d declarations, want none", got)
	}
	if got := bold.StyleProperty("fontWeight"); got != "" {
		t.Errorf("the declaration reader should not see a tag default, got %q", got)
	}
}

func TestToCamelCase(t *testing.T) {
	for input, want := range map[string]string{
		"text-align":       "textAlign",
		"font-weight":      "fontWeight",
		"color":            "color",
		"BACKGROUND-COLOR": "backgroundColor",
		"a-b-c":            "aBC",
		"trailing-":        "trailing",
	} {
		if got := toCamelCase(input); got != want {
			t.Errorf("toCamelCase(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestTextContent(t *testing.T) {
	fragment := Parse("<div>a<p>b<strong>c</strong></p>d</div>")
	if got := fragment.Children[0].TextContent(); got != "abcd" {
		t.Errorf("text = %q", got)
	}
}
