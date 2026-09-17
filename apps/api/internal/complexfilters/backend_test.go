package complexfilters

import (
	"os"
	"strings"
	"testing"
)

// TestMatchesDjango walks the truth table the real backend generated. Every line is a filter a client could send and what Django makes of it, and any difference here is a difference the endpoint would show.
func TestMatchesDjango(t *testing.T) {
	contents, err := os.ReadFile("testdata/filters.tsv")
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, line := range strings.Split(string(contents), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 2 {
			t.Fatalf("malformed fixture line: %q", line)
		}
		input, want := fields[0], fields[1]
		checked++

		node, refusal := Parse(input)
		if want == "ERR" {
			if len(fields) != 4 {
				t.Fatalf("malformed refusal line: %q", line)
			}
			if refusal == nil {
				t.Errorf("%s was accepted as %s, want the refusal %s", input, render(node), fields[2])
				continue
			}
			if refusal.Code != fields[2] || refusal.Message != fields[3] {
				t.Errorf("%s was refused as %s/%q, want %s/%q", input, refusal.Code, refusal.Message, fields[2], fields[3])
			}
			continue
		}
		if refusal != nil {
			t.Errorf("%s was refused as %s/%q, want %s", input, refusal.Code, refusal.Message, want)
			continue
		}
		if got := render(node); got != want {
			t.Errorf("%s became\n  %s\nwant\n  %s", input, got, want)
		}
	}
	if checked < 100 {
		t.Fatalf("the fixture only carried %d cases, which is too few to be the generated one", checked)
	}
}

func render(node *Node) string {
	if node == nil {
		return "NONE"
	}
	return node.Render()
}

// An empty Q combines away rather than wrapping what it is combined with, which is why a one-branch `or` comes back as an AND.
func TestAnEmptyNodeCombinesAway(t *testing.T) {
	leaf := NewNode(Condition{Lookup: "priority__exact", Value: StringValue("high")})
	if got := Combine(EmptyNode(), leaf, Or).Render(); got != `AND(priority__exact="high")` {
		t.Errorf("an empty node ORed with a leaf is %s", got)
	}
	if got := Combine(leaf, EmptyNode(), Or).Render(); got != `AND(priority__exact="high")` {
		t.Errorf("a leaf ORed with an empty node is %s", got)
	}
}

// Two negations cancel rather than nesting, because inverting flips a flag.
func TestTwoNegationsCancel(t *testing.T) {
	leaf := NewNode(Condition{Lookup: "priority__exact", Value: StringValue("high")})
	if got := Invert(Invert(leaf)).Render(); got != leaf.Render() {
		t.Errorf("a doubly negated node is %s, want %s", got, leaf.Render())
	}
	if got := Invert(leaf).Render(); got != `NOT(AND(priority__exact="high"))` {
		t.Errorf("a negated node is %s", got)
	}
}

// A child is folded into its parent when it joins the same way, and nested when it does not — which is what keeps an OR inside an AND visible in the tree.
func TestFoldingFollowsTheConnector(t *testing.T) {
	first := NewNode(Condition{Lookup: "a", Value: StringValue("1")})
	second := NewNode(Condition{Lookup: "b", Value: StringValue("2")})
	third := NewNode(Condition{Lookup: "c", Value: StringValue("3")})
	either := Combine(Combine(EmptyNode(), first, Or), second, Or)
	if got := either.Render(); got != `OR(a="1",b="2")` {
		t.Errorf("two ORed leaves are %s", got)
	}
	both := Combine(either, third, And)
	if got := both.Render(); got != `AND(OR(a="1",b="2"),c="3")` {
		t.Errorf("an OR ANDed with a leaf is %s", got)
	}
}

// A refusal carries the same two keys Django's does, since the client reads the code rather than the sentence.
func TestARefusalIsTheDRFShape(t *testing.T) {
	_, refusal := Parse(`{"nonexistent": 1}`)
	if refusal == nil {
		t.Fatal("an undeclared field was accepted")
	}
	body := refusal.Body()
	if len(body) != 2 || body["code"] != "invalid_filter_field" {
		t.Errorf("the body is %#v", body)
	}
}

// Text that is not JSON at all is its own refusal rather than an empty filter.
func TestTextThatIsNotJSON(t *testing.T) {
	_, refusal := Parse("not json")
	if refusal == nil || refusal.Code != "invalid_json" {
		t.Fatalf("refusal = %#v", refusal)
	}
	// An absent parameter is not a refusal; it is simply no filter.
	if node, refusal := Parse(""); node != nil || refusal != nil {
		t.Errorf("an absent parameter gave %v and %v", node, refusal)
	}
}

// Forty-nine filters, which is what the set declares once its exact twins are added.
func TestTheDeclaredFilters(t *testing.T) {
	if len(issueFilters) != 49 {
		t.Fatalf("there are %d filters, want 49", len(issueFilters))
	}
	// Every filter whose lookup is an exact one has a twin under the suffixed name.
	for name, spec := range issueFilters {
		if strings.HasSuffix(name, "__in") || strings.HasSuffix(name, "__range") || strings.HasSuffix(name, "__exact") {
			continue
		}
		if _, twinned := issueFilters[name+"__exact"]; !twinned {
			t.Errorf("%q has no __exact twin", name)
		}
		_ = spec
	}
}
