package ydoc

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/yldm-tech/pace/apps/api/internal/vdom"
)

type parsedCase struct {
	Name       string          `json:"name"`
	HTML       string          `json:"html"`
	Document   json.RawMessage `json:"document"`
	Rendered   string          `json:"rendered"`
	Reparsed   json.RawMessage `json:"reparsed"`
	Rerendered string          `json:"rerendered"`
}

func loadParsedCorpus(t *testing.T) []parsedCase {
	t.Helper()
	raw, err := os.ReadFile("testdata/parsed.json")
	if err != nil {
		t.Fatalf("read corpus: %v", err)
	}
	var cases []parsedCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatalf("parse corpus: %v", err)
	}
	return cases
}

// TestParseHTMLMatchesEditor is the whole point of the parser: the same markup has to become the same document here as it does in the editor.
func TestParseHTMLMatchesEditor(t *testing.T) {
	for _, testCase := range loadParsedCorpus(t) {
		t.Run(testCase.Name, func(t *testing.T) {
			document, err := ParseHTML(testCase.HTML)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			got, err := document.JSON()
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if !reflect.DeepEqual(decodeJSON(t, got), decodeJSON(t, testCase.Document)) {
				t.Errorf("document differs\n go: %s\nwant: %s", got, testCase.Document)
			}
		})
	}
}

// TestEveryRuleIsUnderstood keeps the table honest: a rule added upstream with a shape this does not implement should fail here rather than quietly never match.
func TestEveryRuleIsUnderstood(t *testing.T) {
	for i := range DocumentParseRules.Rules {
		rule := &DocumentParseRules.Rules[i]
		if rule.HasContentElement {
			t.Errorf("%s rule %d names a content element, which is not implemented", rule.Owner, rule.OwnerIndex)
		}
		if rule.HasGetContent {
			t.Errorf("%s rule %d builds its own content, which is not implemented", rule.Owner, rule.OwnerIndex)
		}
		if rule.Tag == "" && rule.Style == "" {
			t.Errorf("%s rule %d matches neither a tag nor a style", rule.Owner, rule.OwnerIndex)
		}
	}
}

// TestEveryHandWrittenFunctionHasARule is the other direction: a function registered for a rule that no longer exists is a port of something that has gone.
func TestEveryHandWrittenFunctionHasARule(t *testing.T) {
	exists := map[string]map[int]bool{}
	for i := range DocumentParseRules.Rules {
		rule := &DocumentParseRules.Rules[i]
		if exists[rule.Owner] == nil {
			exists[rule.Owner] = map[int]bool{}
		}
		exists[rule.Owner][rule.OwnerIndex] = true
	}
	for owner, byIndex := range tagRuleAttrs {
		for index := range byIndex {
			if !exists[owner][index] {
				t.Errorf("a function is registered for %s rule %d, which the schema no longer has", owner, index)
			}
		}
	}
	for owner, byIndex := range styleRuleAttrs {
		for index := range byIndex {
			if !exists[owner][index] {
				t.Errorf("a function is registered for %s style rule %d, which the schema no longer has", owner, index)
			}
		}
	}
}

// TestEveryCustomAttributeReaderIsImplemented pins the other half of the hand-written code: an attribute whose reader is code in the editor has to have one here too.
func TestEveryCustomAttributeReaderIsImplemented(t *testing.T) {
	for i := range DocumentParseRules.Rules {
		rule := &DocumentParseRules.Rules[i]
		for _, reader := range rule.Reads {
			if !reader.HasParseHTML {
				continue
			}
			if _, ok := attributeParsers[rule.Owner][reader.Name]; !ok {
				t.Errorf("%s.%s is read by code in the editor and has no reader here", rule.Owner, reader.Name)
			}
		}
	}
}

func TestSelectorsAllParse(t *testing.T) {
	for i := range DocumentParseRules.Rules {
		rule := &DocumentParseRules.Rules[i]
		if rule.Tag == "" {
			continue
		}
		if _, err := parseSelector(rule.Tag); err != nil {
			t.Errorf("%s: %v", rule.Tag, err)
		}
	}
}

func TestSelectorMatching(t *testing.T) {
	for _, testCase := range []struct {
		selector string
		html     string
		want     bool
	}{
		{selector: "p", html: "<p>x</p>", want: true},
		{selector: "p", html: "<div>x</div>", want: false},
		{selector: "p", html: "<P>x</P>", want: true},
		{selector: "a[href]", html: `<a href="x">y</a>`, want: true},
		{selector: "a[href]", html: "<a>y</a>", want: false},
		{selector: `li[data-type="taskItem"]`, html: `<li data-type="taskItem">x</li>`, want: true},
		{selector: `li[data-type="taskItem"]`, html: `<li data-type="other">x</li>`, want: false},
		{selector: `img[src]:not([src^="data:"])`, html: `<img src="a.png">`, want: true},
		{selector: `img[src]:not([src^="data:"])`, html: `<img src="data:image/png;base64,AA">`, want: false},
		{selector: `img[src]:not([src^="data:"])`, html: "<img>", want: false},
	} {
		t.Run(testCase.selector+" "+testCase.html, func(t *testing.T) {
			compiled, err := parseSelector(testCase.selector)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			element := parseOneElement(t, testCase.html)
			if got := compiled.matches(element); got != testCase.want {
				t.Errorf("matches = %v, want %v", got, testCase.want)
			}
		})
	}
}

func TestSelectorsThatAreNotUnderstoodAreRefused(t *testing.T) {
	for _, selector := range []string{
		"p.MsoNormal",
		"p > a",
		"#id",
		"p[class~=x]",
		"p:first-child",
		"a[href",
	} {
		if _, err := parseSelector(selector); err == nil {
			t.Errorf("%q was accepted, want it refused", selector)
		}
	}
}

func TestFromString(t *testing.T) {
	for input, want := range map[string]any{
		"1":     float64(1),
		"-2":    float64(-2),
		"1.5":   1.5,
		"true":  true,
		"false": false,
		"x":     "x",
		"":      "",
		"1px":   "1px",
	} {
		if got := fromString(input); got != want {
			t.Errorf("fromString(%q) = %#v, want %#v", input, got, want)
		}
	}
}

func parseOneElement(t *testing.T, source string) *vdom.Node {
	t.Helper()
	fragment := vdom.Parse(source)
	if len(fragment.Children) != 1 {
		t.Fatalf("%q parsed into %d nodes", source, len(fragment.Children))
	}
	return fragment.Children[0]
}

// TestRoundTripMatchesEditor walks the loop the live service actually runs: a page's stored HTML is read into a document, rendered back, and read again. The editor is not idempotent over that loop — a span carrying a style becomes marks, those marks render as elements, and the elements read back into a different nesting — so what is checked is that the port is un-idempotent in exactly the same way.
func TestRoundTripMatchesEditor(t *testing.T) {
	for _, testCase := range loadParsedCorpus(t) {
		t.Run(testCase.Name, func(t *testing.T) {
			first, err := ParseHTML(testCase.HTML)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			rendered, err := HTML(first)
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			if rendered != testCase.Rendered {
				t.Fatalf("rendering differs\n go: %s\nwant: %s", rendered, testCase.Rendered)
			}
			second, err := ParseHTML(rendered)
			if err != nil {
				t.Fatalf("reparse: %v", err)
			}
			got, err := second.JSON()
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if !reflect.DeepEqual(decodeJSON(t, got), decodeJSON(t, testCase.Reparsed)) {
				t.Errorf("the second reading differs\n go: %s\nwant: %s", got, testCase.Reparsed)
			}
			again, err := HTML(second)
			if err != nil {
				t.Fatalf("rerender: %v", err)
			}
			if again != testCase.Rerendered {
				t.Errorf("the second rendering differs\n go: %s\nwant: %s", again, testCase.Rerendered)
			}
		})
	}
}
