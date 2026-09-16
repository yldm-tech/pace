package ydoc

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// ContentMatch is one state of a node type's content expression.
//
// The expression is a grammar — "block+" for a document, "paragraph block*" for a list item, "(tableCell | tableHeader)*" for a table row — and it is compiled once into a finite automaton whose states these are. Reading a document walks the automaton: a child that has no edge out of the current state does not belong there, and the parser's answer to that is to wrap it in something that does.
//
// The order of the edges matters and is not an implementation detail. It is what decides which type gets invented when a gap has to be filled: a table row missing its cells is filled with the first type its expression offers.
type ContentMatch struct {
	// ValidEnd reports whether a node may end here.
	ValidEnd bool

	next      []contentEdge
	wrapCache map[*NodeType][]*NodeType
}

type contentEdge struct {
	Type *NodeType
	Next *ContentMatch
}

// EmptyContentMatch is the match for a type that holds nothing, which ends validly and goes nowhere.
var EmptyContentMatch = &ContentMatch{ValidEnd: true}

// MatchType follows the edge for a type, or returns nil when there is none.
func (m *ContentMatch) MatchType(nodeType *NodeType) *ContentMatch {
	for _, edge := range m.next {
		if edge.Type == nodeType {
			return edge.Next
		}
	}
	return nil
}

// MatchNodes follows the edges for a run of children, stopping at the first one that does not fit.
func (m *ContentMatch) MatchNodes(schema *Schema, nodes []Node) *ContentMatch {
	current := m
	for _, node := range nodes {
		if current == nil {
			return nil
		}
		current = current.MatchType(schema.NodeType(node.Type))
	}
	return current
}

// InlineContent reports whether what follows here is inline.
func (m *ContentMatch) InlineContent() bool {
	return len(m.next) > 0 && m.next[0].Type.IsInline
}

// DefaultType is the first type that can be invented here — the first edge whose type is neither text nor one that would need attributes nobody has supplied.
func (m *ContentMatch) DefaultType() *NodeType {
	for _, edge := range m.next {
		if !edge.Type.IsText && !edge.Type.hasRequiredAttrs() {
			return edge.Type
		}
	}
	return nil
}

// Compatible reports whether two states share any outgoing type.
func (m *ContentMatch) Compatible(other *ContentMatch) bool {
	for _, mine := range m.next {
		for _, theirs := range other.next {
			if mine.Type == theirs.Type {
				return true
			}
		}
	}
	return false
}

// EdgeCount is how many outgoing edges the state has.
func (m *ContentMatch) EdgeCount() int { return len(m.next) }

// Edge is the nth outgoing edge.
func (m *ContentMatch) Edge(n int) (*NodeType, *ContentMatch) {
	if n >= len(m.next) {
		return nil, nil
	}
	return m.next[n].Type, m.next[n].Next
}

// FindWrapping is the parser's answer to a child that does not fit: the run of types it would have to be wrapped in to belong here, empty when it already fits and nil when no wrapping exists.
//
// The search is breadth first, so the shallowest wrapping wins — a list item at the top of a document is wrapped in one list rather than in a list inside a blockquote.
func (m *ContentMatch) FindWrapping(target *NodeType) []*NodeType {
	if cached, ok := m.wrapCache[target]; ok {
		return cached
	}
	computed := m.computeWrapping(target)
	if m.wrapCache == nil {
		m.wrapCache = map[*NodeType][]*NodeType{}
	}
	m.wrapCache[target] = computed
	return computed
}

type wrappingStep struct {
	match    *ContentMatch
	nodeType *NodeType
	via      *wrappingStep
}

func (m *ContentMatch) computeWrapping(target *NodeType) []*NodeType {
	seen := map[string]bool{}
	active := []*wrappingStep{{match: m}}
	for len(active) > 0 {
		current := active[0]
		active = active[1:]
		if current.match.MatchType(target) != nil {
			var result []*NodeType
			for step := current; step.nodeType != nil; step = step.via {
				result = append(result, step.nodeType)
			}
			for left, right := 0, len(result)-1; left < right; left, right = left+1, right-1 {
				result[left], result[right] = result[right], result[left]
			}
			if result == nil {
				result = []*NodeType{}
			}
			return result
		}
		for _, edge := range current.match.next {
			nodeType := edge.Type
			if nodeType.IsLeaf || nodeType.hasRequiredAttrs() || seen[nodeType.Name] {
				continue
			}
			// A wrapper may only be entered from a state that could also have ended there, otherwise the wrapping would leave the node it was cut into incomplete.
			if current.nodeType != nil && !edge.Next.ValidEnd {
				continue
			}
			active = append(active, &wrappingStep{match: nodeType.ContentMatch, nodeType: nodeType, via: current})
			seen[nodeType.Name] = true
		}
	}
	return nil
}

// String dumps the whole automaton, one state per line, the way ProseMirror's own does. It exists because that dump is what the port is checked against.
func (m *ContentMatch) String() string {
	var seen []*ContentMatch
	indexOf := func(match *ContentMatch) int {
		for i, candidate := range seen {
			if candidate == match {
				return i
			}
		}
		return -1
	}
	var scan func(*ContentMatch)
	scan = func(match *ContentMatch) {
		seen = append(seen, match)
		for _, edge := range match.next {
			if indexOf(edge.Next) == -1 {
				scan(edge.Next)
			}
		}
	}
	scan(m)

	lines := make([]string, 0, len(seen))
	for i, match := range seen {
		end := " "
		if match.ValidEnd {
			end = "*"
		}
		line := fmt.Sprintf("%d%s ", i, end)
		for j, edge := range match.next {
			if j > 0 {
				line += ", "
			}
			line += fmt.Sprintf("%s->%d", edge.Type.Name, indexOf(edge.Next))
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// parseContentExpression compiles an expression into an automaton.
func parseContentExpression(expression string, schema *Schema) (*ContentMatch, error) {
	stream := newTokenStream(expression, schema)
	if stream.peek() == "" {
		return EmptyContentMatch, nil
	}
	parsed, err := parseExpr(stream)
	if err != nil {
		return nil, err
	}
	if stream.peek() != "" {
		return nil, stream.errorf("Unexpected trailing text")
	}
	match := buildDFA(buildNFA(parsed))
	if err := checkForDeadEnds(match, stream); err != nil {
		return nil, err
	}
	return match, nil
}

// tokenStream splits an expression the way ProseMirror does: on the boundary before a word, a non-word character or the end. The split leaves punctuation as its own token and drops the empty tokens at either end.
type tokenStream struct {
	expression string
	schema     *Schema
	tokens     []string
	pos        int
	inline     *bool
}

func newTokenStream(expression string, schema *Schema) *tokenStream {
	tokens := splitContentExpression(expression)
	return &tokenStream{expression: expression, schema: schema, tokens: tokens}
}

// splitContentExpression reproduces JavaScript's split on /\s*(?=\b|\W|$)/, which Go's regexp cannot express because it has no lookahead. The rule it works out to: a run of whitespace is dropped, a word character starts or continues a word token, and any other character is a token of its own.
func splitContentExpression(expression string) []string {
	var tokens []string
	var word strings.Builder
	flush := func() {
		if word.Len() > 0 {
			tokens = append(tokens, word.String())
			word.Reset()
		}
	}
	for _, r := range expression {
		switch {
		case r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\f' || r == '\v':
			flush()
		case isWordCharacter(r):
			word.WriteRune(r)
		default:
			flush()
			tokens = append(tokens, string(r))
		}
	}
	flush()
	return tokens
}

func isWordCharacter(r rune) bool {
	return r == '_' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

func (s *tokenStream) peek() string {
	if s.pos >= len(s.tokens) {
		return ""
	}
	return s.tokens[s.pos]
}

func (s *tokenStream) eat(token string) bool {
	if s.peek() != token {
		return false
	}
	s.pos++
	return true
}

func (s *tokenStream) errorf(format string, args ...any) error {
	return fmt.Errorf("%s (in content expression %q)", fmt.Sprintf(format, args...), s.expression)
}

// expr is one node of the parsed expression.
type expr struct {
	kind     string
	exprs    []*expr
	inner    *expr
	min, max int
	value    *NodeType
}

func parseExpr(stream *tokenStream) (*expr, error) {
	var exprs []*expr
	for {
		parsed, err := parseExprSeq(stream)
		if err != nil {
			return nil, err
		}
		exprs = append(exprs, parsed)
		if !stream.eat("|") {
			break
		}
	}
	if len(exprs) == 1 {
		return exprs[0], nil
	}
	return &expr{kind: "choice", exprs: exprs}, nil
}

func parseExprSeq(stream *tokenStream) (*expr, error) {
	var exprs []*expr
	for {
		parsed, err := parseExprSubscript(stream)
		if err != nil {
			return nil, err
		}
		exprs = append(exprs, parsed)
		if stream.peek() == "" || stream.peek() == ")" || stream.peek() == "|" {
			break
		}
	}
	if len(exprs) == 1 {
		return exprs[0], nil
	}
	return &expr{kind: "seq", exprs: exprs}, nil
}

func parseExprSubscript(stream *tokenStream) (*expr, error) {
	parsed, err := parseExprAtom(stream)
	if err != nil {
		return nil, err
	}
	for {
		switch {
		case stream.eat("+"):
			parsed = &expr{kind: "plus", inner: parsed}
		case stream.eat("*"):
			parsed = &expr{kind: "star", inner: parsed}
		case stream.eat("?"):
			parsed = &expr{kind: "opt", inner: parsed}
		case stream.eat("{"):
			parsed, err = parseExprRange(stream, parsed)
			if err != nil {
				return nil, err
			}
		default:
			return parsed, nil
		}
	}
}

func parseNum(stream *tokenStream) (int, error) {
	value, err := strconv.Atoi(stream.peek())
	if err != nil {
		return 0, stream.errorf("Expected number, got '%s'", stream.peek())
	}
	stream.pos++
	return value, nil
}

func parseExprRange(stream *tokenStream, inner *expr) (*expr, error) {
	min, err := parseNum(stream)
	if err != nil {
		return nil, err
	}
	max := min
	if stream.eat(",") {
		if stream.peek() != "}" {
			max, err = parseNum(stream)
			if err != nil {
				return nil, err
			}
		} else {
			max = -1
		}
	}
	if !stream.eat("}") {
		return nil, stream.errorf("Unclosed braced range")
	}
	return &expr{kind: "range", min: min, max: max, inner: inner}, nil
}

// resolveName turns a name into the types it stands for: one type when it names a type, and every member when it names a group.
func resolveName(stream *tokenStream, name string) ([]*NodeType, error) {
	if nodeType := stream.schema.NodeType(name); nodeType != nil {
		return []*NodeType{nodeType}, nil
	}
	var result []*NodeType
	for i := range stream.schema.Nodes {
		nodeType := &stream.schema.Nodes[i]
		if nodeType.inGroup(name) {
			result = append(result, nodeType)
		}
	}
	if len(result) == 0 {
		return nil, stream.errorf("No node type or group '%s' found", name)
	}
	return result, nil
}

func parseExprAtom(stream *tokenStream) (*expr, error) {
	if stream.eat("(") {
		inner, err := parseExpr(stream)
		if err != nil {
			return nil, err
		}
		if !stream.eat(")") {
			return nil, stream.errorf("Missing closing paren")
		}
		return inner, nil
	}
	token := stream.peek()
	if token == "" || !isWord(token) {
		return nil, stream.errorf("Unexpected token '%s'", token)
	}
	types, err := resolveName(stream, token)
	if err != nil {
		return nil, err
	}
	exprs := make([]*expr, 0, len(types))
	for _, nodeType := range types {
		if stream.inline == nil {
			inline := nodeType.IsInline
			stream.inline = &inline
		} else if *stream.inline != nodeType.IsInline {
			return nil, stream.errorf("Mixing inline and block content")
		}
		exprs = append(exprs, &expr{kind: "name", value: nodeType})
	}
	stream.pos++
	if len(exprs) == 1 {
		return exprs[0], nil
	}
	return &expr{kind: "choice", exprs: exprs}, nil
}

func isWord(token string) bool {
	for _, r := range token {
		if !isWordCharacter(r) {
			return false
		}
	}
	return len(token) > 0
}

// nfaEdge is one edge of the automaton before it is made deterministic. An edge with no term is a null edge; the order of the edges is significant, because it survives into the deterministic automaton and decides what gets invented to fill a gap.
type nfaEdge struct {
	term *NodeType
	to   int
}

type nfaBuilder struct {
	states [][]*nfaEdge
}

func buildNFA(parsed *expr) [][]*nfaEdge {
	builder := &nfaBuilder{states: [][]*nfaEdge{{}}}
	builder.connect(builder.compile(parsed, 0), builder.node())
	return builder.states
}

func (b *nfaBuilder) node() int {
	b.states = append(b.states, []*nfaEdge{})
	return len(b.states) - 1
}

func (b *nfaBuilder) edge(from, to int, term *NodeType) *nfaEdge {
	edge := &nfaEdge{term: term, to: to}
	b.states[from] = append(b.states[from], edge)
	return edge
}

func (b *nfaBuilder) connect(edges []*nfaEdge, to int) {
	for _, edge := range edges {
		edge.to = to
	}
}

func (b *nfaBuilder) compile(parsed *expr, from int) []*nfaEdge {
	switch parsed.kind {
	case "choice":
		var out []*nfaEdge
		for _, inner := range parsed.exprs {
			out = append(out, b.compile(inner, from)...)
		}
		return out
	case "seq":
		for i := 0; ; i++ {
			next := b.compile(parsed.exprs[i], from)
			if i == len(parsed.exprs)-1 {
				return next
			}
			from = b.node()
			b.connect(next, from)
		}
	case "star":
		loop := b.node()
		b.edge(from, loop, nil)
		b.connect(b.compile(parsed.inner, loop), loop)
		return []*nfaEdge{b.edge(loop, 0, nil)}
	case "plus":
		loop := b.node()
		b.connect(b.compile(parsed.inner, from), loop)
		b.connect(b.compile(parsed.inner, loop), loop)
		return []*nfaEdge{b.edge(loop, 0, nil)}
	case "opt":
		return append([]*nfaEdge{b.edge(from, 0, nil)}, b.compile(parsed.inner, from)...)
	case "range":
		current := from
		for i := 0; i < parsed.min; i++ {
			next := b.node()
			b.connect(b.compile(parsed.inner, current), next)
			current = next
		}
		if parsed.max == -1 {
			b.connect(b.compile(parsed.inner, current), current)
		} else {
			for i := parsed.min; i < parsed.max; i++ {
				next := b.node()
				b.edge(current, next, nil)
				b.connect(b.compile(parsed.inner, current), next)
				current = next
			}
		}
		return []*nfaEdge{b.edge(current, 0, nil)}
	case "name":
		return []*nfaEdge{b.edge(from, 0, parsed.value)}
	default:
		panic("ydoc: unknown content expression node " + parsed.kind)
	}
}

// nullFrom is the set of states reachable from one by null edges, skipping a state whose only way out is a single null edge so the deterministic automaton does not grow duplicates.
func nullFrom(states [][]*nfaEdge, node int) []int {
	var result []int
	var scan func(int)
	scan = func(node int) {
		edges := states[node]
		if len(edges) == 1 && edges[0].term == nil {
			scan(edges[0].to)
			return
		}
		result = append(result, node)
		for _, edge := range edges {
			if edge.term != nil {
				continue
			}
			if !containsInt(result, edge.to) {
				scan(edge.to)
			}
		}
	}
	scan(node)
	sort.Sort(sort.Reverse(sort.IntSlice(result)))
	return result
}

func containsInt(values []int, value int) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

// buildDFA makes the automaton deterministic, one ContentMatch per reachable set of states.
func buildDFA(states [][]*nfaEdge) *ContentMatch {
	labeled := map[string]*ContentMatch{}

	var explore func([]int) *ContentMatch
	explore = func(set []int) *ContentMatch {
		type grouped struct {
			term  *NodeType
			nodes []int
		}
		var out []*grouped
		for _, node := range set {
			for _, edge := range states[node] {
				if edge.term == nil {
					continue
				}
				var found *grouped
				for _, candidate := range out {
					if candidate.term == edge.term {
						found = candidate
						break
					}
				}
				for _, reached := range nullFrom(states, edge.to) {
					if found == nil {
						found = &grouped{term: edge.term}
						out = append(out, found)
					}
					if !containsInt(found.nodes, reached) {
						found.nodes = append(found.nodes, reached)
					}
				}
			}
		}

		match := &ContentMatch{ValidEnd: containsInt(set, len(states)-1)}
		labeled[joinInts(set)] = match
		for _, group := range out {
			sort.Sort(sort.Reverse(sort.IntSlice(group.nodes)))
			key := joinInts(group.nodes)
			next, ok := labeled[key]
			if !ok {
				next = explore(group.nodes)
			}
			match.next = append(match.next, contentEdge{Type: group.term, Next: next})
		}
		return match
	}

	return explore(nullFrom(states, 0))
}

func joinInts(values []int) string {
	parts := make([]string, len(values))
	for i, value := range values {
		parts[i] = strconv.Itoa(value)
	}
	return strings.Join(parts, ",")
}

// checkForDeadEnds refuses an expression with a state that cannot be left and is not an ending, because such a node could never be completed.
func checkForDeadEnds(match *ContentMatch, stream *tokenStream) error {
	work := []*ContentMatch{match}
	for i := 0; i < len(work); i++ {
		state := work[i]
		dead := !state.ValidEnd
		var nodes []string
		for _, edge := range state.next {
			nodes = append(nodes, edge.Type.Name)
			if dead && !(edge.Type.IsText || edge.Type.hasRequiredAttrs()) {
				dead = false
			}
			if !containsMatch(work, edge.Next) {
				work = append(work, edge.Next)
			}
		}
		if dead {
			return stream.errorf("Only non-generatable nodes (%s) in a required position", strings.Join(nodes, ", "))
		}
	}
	return nil
}

func containsMatch(matches []*ContentMatch, match *ContentMatch) bool {
	for _, candidate := range matches {
		if candidate == match {
			return true
		}
	}
	return false
}

// FillBefore is what makes an incomplete document whole: given the children that are meant to follow, it returns the nodes that would have to be inserted in front of them for the expression to be satisfied, empty when nothing is needed and nil when nothing would help.
//
// With toEnd set it insists that the result also finishes the expression, which is the question asked when a node is closed rather than when one is opened.
func (m *ContentMatch) FillBefore(schema *Schema, after []Node, toEnd bool) ([]Node, bool) {
	seen := []*ContentMatch{m}

	var search func(*ContentMatch, []*NodeType) ([]Node, bool)
	search = func(match *ContentMatch, types []*NodeType) ([]Node, bool) {
		finished := match.MatchNodes(schema, after)
		if finished != nil && (!toEnd || finished.ValidEnd) {
			filled := make([]Node, 0, len(types))
			for _, nodeType := range types {
				node, ok := nodeType.CreateAndFill(schema)
				if !ok {
					return nil, false
				}
				filled = append(filled, node)
			}
			return filled, true
		}
		for _, edge := range match.next {
			if edge.Type.IsText || edge.Type.hasRequiredAttrs() || containsMatch(seen, edge.Next) {
				continue
			}
			seen = append(seen, edge.Next)
			if found, ok := search(edge.Next, append(append([]*NodeType(nil), types...), edge.Type)); ok {
				return found, true
			}
		}
		return nil, false
	}

	return search(m, nil)
}

// CreateAndFill builds an empty node of the type, inventing whatever its expression insists on holding — which is how a table row asked for by a wrapping arrives with a cell already in it.
func (t *NodeType) CreateAndFill(schema *Schema) (Node, bool) {
	attrs, err := computeAttrs(t.Attrs, nil, t.Name, true)
	if err != nil {
		return Node{}, false
	}
	content, ok := t.ContentMatch.FillBefore(schema, nil, true)
	if !ok {
		return Node{}, false
	}
	return Node{Type: t.Name, Attrs: attrs, Content: content}, true
}
