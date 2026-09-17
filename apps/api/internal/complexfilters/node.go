// Package complexfilters is the JSON filter tree the two cross-project work item lists accept.
//
// A client sends `?filters={...}`, a nested structure of `and`, `or` and `not` around leaf objects of field lookups. Django turns that into a Q object and hands it to the queryset in one call. Porting it means porting two things that are easy to separate and easy to get wrong together: what each field's value cleans to, and how the resulting pieces combine — because Django's Q flattens some combinations and not others, and the flattening is visible in the SQL.
//
// The truth table in testdata/filters.tsv is generated from the real backend, so both halves are checked against what the endpoint actually does rather than against a reading of it.
package complexfilters

import (
	"sort"
	"strconv"
	"strings"
)

// ValueKind is what a cleaned filter value turned out to be.
type ValueKind int

const (
	// KindString covers everything Django renders as a string: text, a uuid and a date alike.
	KindString ValueKind = iota
	KindBool
	KindNull
	KindList
	// KindEveryRow is the queryset a filter returns when its value cleaned to nothing. build_combined_q wraps it as a subquery over every row, which is no filter at all written the long way round.
	KindEveryRow
)

type Value struct {
	Kind ValueKind
	Text string
	Bool bool
	List []Value
}

func StringValue(text string) Value { return Value{Kind: KindString, Text: text} }
func BoolValue(value bool) Value    { return Value{Kind: KindBool, Bool: value} }
func NullValue() Value              { return Value{Kind: KindNull} }
func ListValue(items []Value) Value { return Value{Kind: KindList, List: items} }
func everyRowValue() Value          { return Value{Kind: KindEveryRow} }

// Render writes a value the way the fixture does, which is JSON for everything that has a JSON shape.
func (value Value) Render() string {
	switch value.Kind {
	case KindBool:
		return strconv.FormatBool(value.Bool)
	case KindNull:
		return "null"
	case KindEveryRow:
		return "<every-row>"
	case KindList:
		parts := make([]string, 0, len(value.List))
		for _, item := range value.List {
			parts = append(parts, item.Render())
		}
		return "[" + strings.Join(parts, ",") + "]"
	default:
		return quoteJSON(value.Text)
	}
}

func quoteJSON(text string) string {
	var builder strings.Builder
	builder.WriteByte('"')
	for _, character := range text {
		switch character {
		case '"':
			builder.WriteString(`\"`)
		case '\\':
			builder.WriteString(`\\`)
		case '\n':
			builder.WriteString(`\n`)
		case '\r':
			builder.WriteString(`\r`)
		case '\t':
			builder.WriteString(`\t`)
		default:
			builder.WriteRune(character)
		}
	}
	builder.WriteByte('"')
	return builder.String()
}

// Condition is one leaf of a Q tree: the lookup Django would pass as a keyword and the value it cleaned to.
type Condition struct {
	Lookup string
	Value  Value
}

// Connector is how a node joins its children.
type Connector string

const (
	And Connector = "AND"
	Or  Connector = "OR"
)

// Node is Django's Q. A child is either another node or a condition, never both.
type Node struct {
	Connector  Connector
	Negated    bool
	Conditions []Condition
	Children   []*Node
	// order keeps the children in the sequence they were added, since a node's children are a single list in Django and the two kinds interleave.
	order []nodeChild
}

type nodeChild struct {
	condition *Condition
	node      *Node
}

// NewNode is Q(**kwargs): an unnegated AND over the conditions, with the keywords sorted the way Q sorts them.
func NewNode(conditions ...Condition) *Node {
	sorted := append([]Condition(nil), conditions...)
	sort.Slice(sorted, func(first, second int) bool { return sorted[first].Lookup < sorted[second].Lookup })
	node := &Node{Connector: And}
	for index := range sorted {
		node.addCondition(sorted[index])
	}
	return node
}

// EmptyNode is Q(), which combines away rather than adding anything.
func EmptyNode() *Node { return &Node{Connector: And} }

func (node *Node) addCondition(condition Condition) {
	node.Conditions = append(node.Conditions, condition)
	node.order = append(node.order, nodeChild{condition: &node.Conditions[len(node.Conditions)-1]})
}

func (node *Node) addNode(child *Node) {
	node.Children = append(node.Children, child)
	node.order = append(node.order, nodeChild{node: child})
}

// Len is how many children a node has, counting conditions and sub-nodes alike.
func (node *Node) Len() int { return len(node.order) }

func (node *Node) copy() *Node {
	clone := &Node{Connector: node.Connector, Negated: node.Negated}
	for _, child := range node.order {
		if child.condition != nil {
			clone.addCondition(*child.condition)
			continue
		}
		clone.addNode(child.node.copy())
	}
	return clone
}

// Combine is Q._combine, and the two short-circuits at the top are what make `Q() | x` come back as x rather than as an OR of one thing.
func Combine(left, right *Node, connector Connector) *Node {
	if left.Len() == 0 {
		return right.copy()
	}
	if right.Len() == 0 {
		return left.copy()
	}
	combined := &Node{Connector: connector}
	combined.add(left, connector)
	combined.add(right, connector)
	return combined
}

// add is tree.Node.add. A child is folded into the parent when it is not negated and either joins the same way or holds a single thing; otherwise it is nested. That rule is why `{"or": [a, b]}` comes back flat and `{"not": {...}}` does not.
func (node *Node) add(child *Node, connector Connector) {
	if node.Connector != connector {
		inner := node.copy()
		node.Connector = connector
		node.Conditions = nil
		node.Children = nil
		node.order = nil
		node.addNode(inner)
		node.addChildOrFold(child, connector)
		return
	}
	node.addChildOrFold(child, connector)
}

func (node *Node) addChildOrFold(child *Node, connector Connector) {
	if !child.Negated && (child.Connector == connector || child.Len() == 1) {
		for _, grandchild := range child.order {
			if grandchild.condition != nil {
				node.addCondition(*grandchild.condition)
				continue
			}
			node.addNode(grandchild.node)
		}
		return
	}
	node.addNode(child)
}

// Invert is ~Q, which flips the flag rather than wrapping — so a double negation comes back as the original.
func Invert(node *Node) *Node {
	inverted := node.copy()
	inverted.Negated = !inverted.Negated
	return inverted
}

// Render writes the canonical form the fixture compares against: the connector, then the children sorted by their own rendering so neither side has to agree on the order Django happened to build them in.
func (node *Node) Render() string {
	parts := make([]string, 0, node.Len())
	for _, child := range node.order {
		if child.condition != nil {
			parts = append(parts, child.condition.Lookup+"="+child.condition.Value.Render())
			continue
		}
		parts = append(parts, child.node.Render())
	}
	sort.Strings(parts)
	body := string(node.Connector) + "(" + strings.Join(parts, ",") + ")"
	if node.Negated {
		return "NOT(" + body + ")"
	}
	return body
}
