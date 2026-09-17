package ydoc

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

type contentMatchCorpus struct {
	Automatons []struct {
		Type       string `json:"type"`
		Expression string `json:"expression"`
		Automaton  string `json:"automaton"`
	} `json:"automatons"`
	Matches []struct {
		In      string `json:"in"`
		Type    string `json:"type"`
		Matches bool   `json:"matches"`
	} `json:"matches"`
	Wrappings []struct {
		In       string   `json:"in"`
		Type     string   `json:"type"`
		Wrapping []string `json:"wrapping"`
	} `json:"wrappings"`
	Fills []struct {
		Type string   `json:"type"`
		Fill []string `json:"fill"`
	} `json:"fills"`
	Created []struct {
		Type string          `json:"type"`
		Node json.RawMessage `json:"node"`
	} `json:"created"`
	MarkSets []struct {
		Type  string   `json:"type"`
		Marks []string `json:"marks"`
	} `json:"markSets"`
	Extras []struct {
		Expression string `json:"expression"`
		Automaton  string `json:"automaton"`
	} `json:"extras"`
}

func loadContentMatchCorpus(t *testing.T) contentMatchCorpus {
	t.Helper()
	raw, err := os.ReadFile("testdata/content_match.json")
	if err != nil {
		t.Fatalf("read corpus: %v", err)
	}
	var corpus contentMatchCorpus
	if err := json.Unmarshal(raw, &corpus); err != nil {
		t.Fatalf("parse corpus: %v", err)
	}
	return corpus
}

// TestAutomatonsMatchProseMirror is the strongest check in the package: two automatons that print alike are the same automaton, state for state and edge for edge, in the same order.
func TestAutomatonsMatchProseMirror(t *testing.T) {
	corpus := loadContentMatchCorpus(t)
	if len(corpus.Automatons) != len(DocumentSchema.Nodes) {
		t.Fatalf("the corpus holds %d automatons and the schema has %d node types", len(corpus.Automatons), len(DocumentSchema.Nodes))
	}
	for _, expected := range corpus.Automatons {
		t.Run(expected.Type, func(t *testing.T) {
			nodeType := DocumentSchema.NodeType(expected.Type)
			if nodeType == nil {
				t.Fatalf("the schema has no type %q", expected.Type)
			}
			if nodeType.Content != expected.Expression {
				t.Errorf("expression = %q, want %q", nodeType.Content, expected.Expression)
			}
			if got := nodeType.ContentMatch.String(); got != expected.Automaton {
				t.Errorf("automaton differs\n go:\n%s\nwant:\n%s", got, expected.Automaton)
			}
		})
	}
}

// TestExtraExpressionsMatchProseMirror covers the parts of the grammar the schema itself never reaches: ranges, optionals, alternation and nesting.
func TestExtraExpressionsMatchProseMirror(t *testing.T) {
	for _, expected := range loadContentMatchCorpus(t).Extras {
		t.Run(expected.Expression, func(t *testing.T) {
			match, err := parseContentExpression(expected.Expression, DocumentSchema)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if got := match.String(); got != expected.Automaton {
				t.Errorf("automaton differs\n go:\n%s\nwant:\n%s", got, expected.Automaton)
			}
		})
	}
}

func TestMatchTypeMatchesProseMirror(t *testing.T) {
	for _, expected := range loadContentMatchCorpus(t).Matches {
		outer := DocumentSchema.NodeType(expected.In)
		inner := DocumentSchema.NodeType(expected.Type)
		if got := outer.ContentMatch.MatchType(inner) != nil; got != expected.Matches {
			t.Errorf("%s may start %s content = %v, want %v", expected.Type, expected.In, got, expected.Matches)
		}
	}
}

// TestFindWrappingMatchesProseMirror covers every pair of types in the schema, which is what tells a list item at the top of a document that it belongs in a list.
func TestFindWrappingMatchesProseMirror(t *testing.T) {
	for _, expected := range loadContentMatchCorpus(t).Wrappings {
		outer := DocumentSchema.NodeType(expected.In)
		inner := DocumentSchema.NodeType(expected.Type)
		wrapping := outer.ContentMatch.FindWrapping(inner)
		var names []string
		if wrapping != nil {
			names = make([]string, 0, len(wrapping))
			for _, wrapper := range wrapping {
				names = append(names, wrapper.Name)
			}
		}
		if !sameNames(names, expected.Wrapping) {
			t.Errorf("wrapping %s into %s = %v, want %v", expected.Type, expected.In, names, expected.Wrapping)
		}
	}
}

func TestFillBeforeMatchesProseMirror(t *testing.T) {
	for _, expected := range loadContentMatchCorpus(t).Fills {
		nodeType := DocumentSchema.NodeType(expected.Type)
		filled, ok := nodeType.ContentMatch.FillBefore(DocumentSchema, nil, true)
		var names []string
		if ok {
			names = make([]string, 0, len(filled))
			for _, node := range filled {
				names = append(names, node.Type)
			}
		}
		if !sameNames(names, expected.Fill) {
			t.Errorf("filling %s = %v, want %v", expected.Type, names, expected.Fill)
		}
	}
}

// TestCreateAndFillMatchesProseMirror covers the recursive part: a type asked for empty arrives with whatever its expression insists on, and that content with its own.
func TestCreateAndFillMatchesProseMirror(t *testing.T) {
	for _, expected := range loadContentMatchCorpus(t).Created {
		t.Run(expected.Type, func(t *testing.T) {
			node, ok := DocumentSchema.NodeType(expected.Type).CreateAndFill(DocumentSchema)
			if !ok {
				if string(expected.Node) != "null" {
					t.Errorf("nothing was built, want %s", expected.Node)
				}
				return
			}
			got, err := node.JSON()
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if !reflect.DeepEqual(decodeJSON(t, got), decodeJSON(t, expected.Node)) {
				t.Errorf("node = %s\nwant %s", got, expected.Node)
			}
		})
	}
}

// TestMarkSetsMatchProseMirror covers the other half of a type's grammar: which marks its content may carry.
func TestMarkSetsMatchProseMirror(t *testing.T) {
	for _, expected := range loadContentMatchCorpus(t).MarkSets {
		nodeType := DocumentSchema.NodeType(expected.Type)
		var names []string
		if nodeType.MarkSet != nil {
			names = make([]string, 0, len(nodeType.MarkSet))
			for _, markType := range nodeType.MarkSet {
				names = append(names, markType.Name)
			}
		}
		if !sameNames(names, expected.Marks) {
			t.Errorf("marks of %s = %v, want %v", expected.Type, names, expected.Marks)
		}
	}
}

func TestSchemaCompilesEveryExpression(t *testing.T) {
	for i := range DocumentSchema.Nodes {
		nodeType := &DocumentSchema.Nodes[i]
		if nodeType.ContentMatch == nil {
			t.Errorf("%s has no compiled content", nodeType.Name)
		}
	}
}

// TestBrokenExpressionsAreRefused pins the refusals, because a schema that will not compile is a broken schema rather than a broken document.
func TestBrokenExpressionsAreRefused(t *testing.T) {
	for _, expression := range []string{
		"nosuchtype",
		"paragraph)",
		"(paragraph",
		"paragraph{",
		"paragraph{a}",
		"paragraph text",
	} {
		if _, err := parseContentExpression(expression, DocumentSchema); err == nil {
			t.Errorf("%q compiled without error", expression)
		}
	}
}

func sameNames(got, want []string) bool {
	if len(got) == 0 && len(want) == 0 {
		// The corpus writes null for no wrapping and an empty list for an empty one, and the two have to stay apart.
		return (got == nil) == (want == nil)
	}
	return reflect.DeepEqual(got, want)
}
