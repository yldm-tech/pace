package ydoc

import (
	"regexp"
	"strings"

	"github.com/yldm-tech/pace/apps/api-go/internal/vdom"
)

// ParseHTML reads HTML into a document, the way the editor does.
//
// This is ProseMirror's DOMParser over the schema's own parse rules, with the editor's HTML scanner underneath it. What comes out is what the editor would have made of the same markup, which matters because a page's stored HTML is converted back through this path the first time somebody opens it in the collaborative editor.
func ParseHTML(source string) (Node, error) {
	return ParseHTMLWithSchema(source, DocumentSchema, DocumentParseRules)
}

// ParseHTMLWithSchema is ParseHTML against a schema and rules other than the document editor's.
func ParseHTMLWithSchema(source string, schema *Schema, rules *ParseRules) (Node, error) {
	context := newParseContext(schema, rules)
	context.addAll(vdom.Parse(source), nil)
	return context.finish(), nil
}

// blockTags are the elements that end an inline run. An element in this list with no rule of its own still closes whatever inline content was open.
var blockTags = map[string]bool{
	"address": true, "article": true, "aside": true, "blockquote": true, "canvas": true,
	"dd": true, "div": true, "dl": true, "fieldset": true, "figcaption": true, "figure": true,
	"footer": true, "form": true, "h1": true, "h2": true, "h3": true, "h4": true, "h5": true,
	"h6": true, "header": true, "hgroup": true, "hr": true, "li": true, "noscript": true, "ol": true,
	"output": true, "p": true, "pre": true, "section": true, "table": true, "tfoot": true, "ul": true,
}

// ignoreTags are the elements whose content never becomes document content, whatever is inside them.
var ignoreTags = map[string]bool{
	"head": true, "noscript": true, "object": true, "script": true, "style": true, "title": true,
}

var listTags = map[string]bool{"ol": true, "ul": true}

// The three flags a node context carries about whitespace and about being open on the left.
const (
	optPreserveWS     = 1
	optPreserveWSFull = 2
	optOpenLeft       = 4
)

// wsOptionsFor works out the whitespace handling for a node being opened: what the rule says if it says anything, what the type says if it is a preformatted one, and otherwise what the node above it had.
func wsOptionsFor(nodeType *NodeType, ruleSet, ruleOn, ruleFull bool, base int) int {
	if ruleSet {
		options := 0
		if ruleOn {
			options |= optPreserveWS
		}
		if ruleFull {
			options |= optPreserveWSFull
		}
		return options
	}
	if nodeType != nil && nodeType.Whitespace == "pre" {
		return optPreserveWS | optPreserveWSFull
	}
	return base &^ optOpenLeft
}

// nodeContext is one node being built, and the stack of them is how far into the document the parse has got.
type nodeContext struct {
	nodeType *NodeType
	attrs    map[string]any
	marks    []Mark
	// solid marks a node the parser will not leave without being asked to, which is every node a rule opened.
	solid   bool
	options int
	content []Node
	match   *ContentMatch
}

func newNodeContext(schema *Schema, nodeType *NodeType, attrs map[string]any, marks []Mark, solid bool, match *ContentMatch, options int) *nodeContext {
	context := &nodeContext{nodeType: nodeType, attrs: attrs, marks: marks, solid: solid, options: options, match: match}
	if match == nil && options&optOpenLeft == 0 && nodeType != nil {
		context.match = nodeType.ContentMatch
	}
	return context
}

// findWrapping answers what a node would have to be wrapped in to go here, and adjusts the match when the node could only fit after something else is inserted first.
func (c *nodeContext) findWrapping(schema *Schema, node Node) ([]*NodeType, bool) {
	if c.match == nil {
		if c.nodeType == nil {
			return []*NodeType{}, true
		}
		if fill, ok := c.nodeType.ContentMatch.FillBefore(schema, []Node{node}, false); ok {
			c.match = c.nodeType.ContentMatch.MatchNodes(schema, fill)
		} else {
			start := c.nodeType.ContentMatch
			if wrap := start.FindWrapping(schema.NodeType(node.Type)); wrap != nil {
				c.match = start
				return wrap, true
			}
			return nil, false
		}
	}
	wrap := c.match.FindWrapping(schema.NodeType(node.Type))
	return wrap, wrap != nil
}

var trailingWhitespace = regexp.MustCompile(`[ \t\r\n\f]+$`)

// finish closes the node: trailing whitespace goes unless it is being preserved, and whatever the content expression still insists on is filled in unless the node is being left open.
func (c *nodeContext) finish(schema *Schema, openEnd bool) Node {
	if c.options&optPreserveWS == 0 && len(c.content) > 0 {
		last := c.content[len(c.content)-1]
		if last.Type == "text" {
			if match := trailingWhitespace.FindString(last.Text); match != "" {
				if len(last.Text) == len(match) {
					c.content = c.content[:len(c.content)-1]
				} else {
					last.Text = last.Text[:len(last.Text)-len(match)]
					c.content[len(c.content)-1] = last
				}
			}
		}
	}
	content := mergeAdjacentText(c.content)
	if !openEnd && c.match != nil {
		if fill, ok := c.match.FillBefore(schema, nil, true); ok {
			content = append(content, fill...)
		}
	}
	if c.nodeType == nil {
		return Node{Content: content}
	}
	attrs, err := computeAttrs(c.nodeType.Attrs, c.attrs, c.nodeType.Name, true)
	if err != nil {
		attrs = nil
	}
	return Node{Type: c.nodeType.Name, Attrs: attrs, Content: content, Marks: c.marks}
}

// mergeAdjacentText joins neighbouring pieces of text that carry the same marks, which is what building a fragment out of them does. Without it a word split across two elements nobody had a rule for stays split.
func mergeAdjacentText(content []Node) []Node {
	out := make([]Node, 0, len(content))
	for _, node := range content {
		if len(out) > 0 {
			previous := &out[len(out)-1]
			if node.Type == "text" && previous.Type == "text" && sameMarks(previous.Marks, node.Marks) {
				previous.Text += node.Text
				continue
			}
		}
		out = append(out, node)
	}
	return out
}

func sameMarks(a, b []Mark) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !markEqual(a[i], b[i]) {
			return false
		}
	}
	return true
}

// inlineContext reports whether what is being read here is inline, which decides whether whitespace on its own is worth keeping.
func (c *nodeContext) inlineContext(schema *Schema, element *vdom.Node) bool {
	if c.nodeType != nil {
		return c.nodeType.ContentMatch.InlineContent()
	}
	if len(c.content) > 0 {
		first := schema.NodeType(c.content[0].Type)
		return first != nil && first.IsInline
	}
	return element.Parent != nil && !element.Parent.IsFragment() && !blockTags[element.Parent.Tag()]
}

// parseContext is the whole parse in progress.
type parseContext struct {
	schema *Schema
	rules  *ParseRules

	nodes            []*nodeContext
	open             int
	needsBlock       bool
	localPreserveWS  bool
	normalizeApplied map[*vdom.Node]bool
}

func newParseContext(schema *Schema, rules *ParseRules) *parseContext {
	top := newNodeContext(schema, schema.NodeType(schema.TopNode), nil, nil, true, nil, 0)
	return &parseContext{schema: schema, rules: rules, nodes: []*nodeContext{top}, normalizeApplied: map[*vdom.Node]bool{}}
}

func (p *parseContext) top() *nodeContext { return p.nodes[p.open] }

func (p *parseContext) addDOM(dom *vdom.Node, marks []Mark) {
	switch dom.Type {
	case vdom.TextNode:
		p.addTextNode(dom, marks)
	case vdom.ElementNode:
		p.addElement(dom, marks, -1)
	}
}

var whitespaceRun = regexp.MustCompile(`[ \t\r\n\f]+`)
var anyNonWhitespace = regexp.MustCompile(`[^ \t\r\n\f]`)
var leadingWhitespace = regexp.MustCompile(`^[ \t\r\n\f]`)
var newlines = regexp.MustCompile(`\r?\n|\r`)
var carriageReturns = regexp.MustCompile(`\r\n?`)

// addTextNode is where whitespace is decided. Outside a preformatted node every run of whitespace collapses to one space, and a leading space is dropped when there is nothing before it that would have made it meaningful.
func (p *parseContext) addTextNode(dom *vdom.Node, marks []Mark) {
	value := dom.Text
	top := p.top()
	preserveFull := top.options&optPreserveWSFull != 0
	preserve := preserveFull || p.localPreserveWS || top.options&optPreserveWS != 0

	if !preserveFull && !top.inlineContext(p.schema, dom) && !anyNonWhitespace.MatchString(value) {
		return
	}

	switch {
	case !preserve:
		value = whitespaceRun.ReplaceAllString(value, " ")
		if leadingWhitespace.MatchString(value) && p.open == len(p.nodes)-1 {
			var before *Node
			if len(top.content) > 0 {
				before = &top.content[len(top.content)-1]
			}
			previous := previousSibling(dom)
			if before == nil ||
				(previous != nil && previous.Type == vdom.ElementNode && previous.Tag() == "br") ||
				(before.Type == "text" && trailingWhitespace.MatchString(before.Text)) {
				value = value[1:]
			}
		}
	case !preserveFull:
		value = newlines.ReplaceAllString(value, " ")
	default:
		value = carriageReturns.ReplaceAllString(value, "\n")
	}

	if value != "" {
		p.insertNode(Node{Type: "text", Text: value}, marks, !anyNonWhitespace.MatchString(value))
	}
}

func previousSibling(dom *vdom.Node) *vdom.Node {
	if dom.Parent == nil {
		return nil
	}
	for i, child := range dom.Parent.Children {
		if child == dom {
			if i == 0 {
				return nil
			}
			return dom.Parent.Children[i-1]
		}
	}
	return nil
}

// addElement reads one element: the first rule that matches decides what it becomes, and an element nothing matches contributes its children directly.
func (p *parseContext) addElement(dom *vdom.Node, marks []Mark, matchAfter int) {
	outerWS := p.localPreserveWS
	defer func() { p.localPreserveWS = outerWS }()

	top := p.top()
	if dom.Tag() == "pre" || strings.Contains(dom.StyleValue("whiteSpace"), "pre") {
		p.localPreserveWS = true
	}
	name := dom.Tag()
	if listTags[name] && p.rules.NormalizeLists && !p.normalizeApplied[dom] {
		p.normalizeApplied[dom] = true
		normalizeList(dom)
	}

	ruleIndex, rule, ruleAttributes := p.matchTag(dom, matchAfter)

	switch {
	case (rule != nil && rule.Ignore) || (rule == nil && ignoreTags[name]):
		p.ignoreFallback(dom, marks)

	case rule == nil || rule.Skip || rule.CloseParent:
		if rule != nil && rule.CloseParent {
			p.open = max(0, p.open-1)
		}
		var sync bool
		oldNeedsBlock := p.needsBlock
		if blockTags[name] {
			if len(top.content) > 0 && p.open > 0 {
				first := p.schema.NodeType(top.content[0].Type)
				if first != nil && first.IsInline {
					p.open--
					top = p.top()
				}
			}
			sync = true
			if top.nodeType == nil {
				p.needsBlock = true
			}
		} else if len(dom.Children) == 0 {
			p.leafFallback(dom, marks)
			return
		}
		innerMarks := marks
		if rule == nil || !rule.Skip {
			var ok bool
			innerMarks, ok = p.readStyles(dom, marks)
			if !ok {
				p.needsBlock = oldNeedsBlock
				return
			}
		}
		p.addAll(dom, innerMarks)
		if sync {
			p.sync(top)
		}
		p.needsBlock = oldNeedsBlock

	default:
		innerMarks, ok := p.readStyles(dom, marks)
		if !ok {
			return
		}
		continueAfter := -1
		if rule.Consuming != nil && !*rule.Consuming {
			continueAfter = ruleIndex
		}
		p.addElementByRule(dom, rule, ruleAttributes, innerMarks, continueAfter)
	}
}

// leafFallback is what becomes of an element with nothing in it that no rule wanted: a break inside inline content is a newline, and everything else is nothing.
func (p *parseContext) leafFallback(dom *vdom.Node, marks []Mark) {
	if dom.Tag() == "br" && p.top().nodeType != nil && p.top().nodeType.ContentMatch.InlineContent() {
		p.addTextNode(&vdom.Node{Type: vdom.TextNode, Text: "\n", Parent: dom.Parent}, marks)
	}
}

// ignoreFallback keeps a break from being lost entirely when it falls outside inline content: it opens one, which is what the placeholder is for.
func (p *parseContext) ignoreFallback(dom *vdom.Node, marks []Mark) {
	if dom.Tag() == "br" && (p.top().nodeType == nil || !p.top().nodeType.ContentMatch.InlineContent()) {
		p.findPlace(Node{Type: "text", Text: "-"}, marks, true)
	}
}

// readStyles runs the style rules over an element's own declarations, returning the marks that result. A rule that says to ignore the element reports so.
func (p *parseContext) readStyles(dom *vdom.Node, marks []Mark) ([]Mark, bool) {
	if dom.StyleCount() == 0 {
		return marks, true
	}
	for _, property := range p.rules.MatchedStyles {
		value := dom.StyleProperty(property)
		if value == "" {
			continue
		}
		after := -1
		for {
			index, rule, attrs := p.matchStyle(property, value, after)
			if rule == nil {
				break
			}
			switch {
			case rule.Ignore:
				return nil, false
			case rule.HasClearMark:
				marks = clearMarksOfType(marks, rule.Owner)
			default:
				marks = append(append([]Mark(nil), marks...), Mark{Type: rule.Mark, Attrs: p.markAttrs(rule.Mark, attrs)})
			}
			if rule.Consuming != nil && !*rule.Consuming {
				after = index
				continue
			}
			break
		}
	}
	return marks, true
}

// clearMarksOfType drops every mark of one type, which is what a rule saying a style cancels a mark does.
func clearMarksOfType(marks []Mark, markType string) []Mark {
	out := make([]Mark, 0, len(marks))
	for _, mark := range marks {
		if mark.Type != markType {
			out = append(out, mark)
		}
	}
	return out
}

func (p *parseContext) matchTag(dom *vdom.Node, after int) (int, *ParseRule, map[string]any) {
	start := 0
	for i, index := range p.rules.tagRules {
		if index == after {
			start = i + 1
			break
		}
	}
	if after == -1 {
		start = 0
	}
	for i := start; i < len(p.rules.tagRules); i++ {
		index := p.rules.tagRules[i]
		rule := &p.rules.Rules[index]
		if !rule.compiled.matches(dom) {
			continue
		}
		if rule.Context != "" && !p.matchesContext(rule.Context) {
			continue
		}
		attrs, ok := ruleAttrs(rule, dom)
		if !ok {
			continue
		}
		return index, rule, attrs
	}
	return -1, nil, nil
}

func (p *parseContext) matchStyle(property, value string, after int) (int, *ParseRule, map[string]any) {
	start := 0
	if after != -1 {
		for i, index := range p.rules.styleRules {
			if index == after {
				start = i + 1
				break
			}
		}
	}
	for i := start; i < len(p.rules.styleRules); i++ {
		index := p.rules.styleRules[i]
		rule := &p.rules.Rules[index]
		if !strings.HasPrefix(rule.Style, property) {
			continue
		}
		if rule.Context != "" && !p.matchesContext(rule.Context) {
			continue
		}
		// A rule may name a value as well as a property, and then only that value matches.
		if len(rule.Style) > len(property) && (!rule.hasStyleValue || rule.styleValue != value) {
			continue
		}
		attrs, ok := styleAttrs(rule, value)
		if !ok {
			continue
		}
		return index, rule, attrs
	}
	return -1, nil, nil
}

// addElementByRule applies the rule that matched: a node rule opens a node or inserts a leaf, a mark rule adds a mark to everything inside.
func (p *parseContext) addElementByRule(dom *vdom.Node, rule *ParseRule, attrs map[string]any, marks []Mark, continueAfter int) {
	var sync bool
	var nodeType *NodeType

	if rule.Node != "" {
		nodeType = p.schema.NodeType(rule.Node)
		if nodeType == nil {
			return
		}
		if !nodeType.IsLeaf {
			ruleSet, ruleOn, ruleFull := rule.preserveWhitespaceSetting()
			if inner, ok := p.enter(nodeType, attrs, marks, ruleSet, ruleOn, ruleFull); ok {
				sync = true
				marks = inner
			}
		} else {
			computed, err := computeAttrs(nodeType.Attrs, attrs, nodeType.Name, true)
			if err != nil {
				return
			}
			if !p.insertNode(Node{Type: nodeType.Name, Attrs: computed}, marks, dom.Tag() == "br") {
				p.leafFallback(dom, marks)
			}
		}
	} else {
		marks = append(append([]Mark(nil), marks...), Mark{Type: rule.Mark, Attrs: p.markAttrs(rule.Mark, attrs)})
	}

	startIn := p.top()
	switch {
	case nodeType != nil && nodeType.IsLeaf:
	case continueAfter != -1:
		p.addElement(dom, marks, continueAfter)
	default:
		p.addAll(dom, marks)
	}
	if sync && p.sync(startIn) {
		p.open--
	}
}

// markAttrs fills a mark's declared attributes from what the rule produced.
func (p *parseContext) markAttrs(markName string, attrs map[string]any) map[string]any {
	markType := p.schema.MarkType(markName)
	if markType == nil {
		return nil
	}
	computed, err := computeAttrs(markType.Attrs, attrs, markType.Name, true)
	if err != nil {
		return nil
	}
	return computed
}

func (p *parseContext) addAll(parent *vdom.Node, marks []Mark) {
	for _, child := range parent.Children {
		p.addDOM(child, marks)
	}
}

// findPlace looks for somewhere the node can go, closing nodes that are not solid and inserting wrappers where they are needed. The shallowest place wins, with a penalty for each solid node it would have to leave.
func (p *parseContext) findPlace(node Node, marks []Mark, cautious bool) ([]Mark, bool) {
	var route []*NodeType
	var sync *nodeContext
	found := false

	penalty := 0
	for depth := p.open; depth >= 0; depth-- {
		context := p.nodes[depth]
		wrapping, ok := context.findWrapping(p.schema, node)
		if ok && (!found || len(route) > len(wrapping)+penalty) {
			route = wrapping
			sync = context
			found = true
			if len(wrapping) == 0 {
				break
			}
		}
		if context.solid {
			if cautious {
				break
			}
			penalty += 2
		}
	}
	if !found {
		return nil, false
	}
	p.sync(sync)
	for _, wrapper := range route {
		marks = p.enterInner(wrapper, nil, marks, false, false, false, false)
	}
	return marks, true
}

// insertNode puts a node into the document, opening whatever has to be opened around it first.
func (p *parseContext) insertNode(node Node, marks []Mark, cautious bool) bool {
	nodeType := p.schema.NodeType(node.Type)
	if nodeType != nil && nodeType.IsInline && p.needsBlock && p.top().nodeType == nil {
		if block := p.textblockFromContext(); block != nil {
			marks = p.enterInner(block, nil, marks, false, false, false, false)
		}
	}
	innerMarks, ok := p.findPlace(node, marks, cautious)
	if !ok {
		return false
	}
	p.closeExtra(false)
	top := p.top()
	if top.match != nil {
		top.match = top.match.MatchType(nodeType)
	}
	var nodeMarks []Mark
	for _, mark := range append(append([]Mark(nil), innerMarks...), node.Marks...) {
		if p.marksAllowed(top, mark, nodeType) {
			nodeMarks = addMarkToSet(p.schema, nodeMarks, mark)
		}
	}
	node.Marks = nodeMarks
	top.content = append(top.content, node)
	return true
}

// marksAllowed asks whether a mark may sit on a node here: the node above decides when there is one, and otherwise the question is whether the schema allows the mark anywhere the node could appear.
func (p *parseContext) marksAllowed(top *nodeContext, mark Mark, nodeType *NodeType) bool {
	if top.nodeType != nil {
		return allowsMarkType(top.nodeType, mark.Type)
	}
	return markMayApply(p.schema, mark.Type, nodeType)
}

func allowsMarkType(nodeType *NodeType, markName string) bool {
	if nodeType.MarkSet == nil {
		return true
	}
	for _, allowed := range nodeType.MarkSet {
		if allowed.Name == markName {
			return true
		}
	}
	return false
}

// markMayApply asks whether it would be reasonable to put a mark on a node, by looking for any type in the schema that both allows the mark and can hold the node.
func markMayApply(schema *Schema, markName string, nodeType *NodeType) bool {
	if nodeType == nil {
		return false
	}
	for i := range schema.Nodes {
		parent := &schema.Nodes[i]
		if !allowsMarkType(parent, markName) {
			continue
		}
		var seen []*ContentMatch
		var scan func(*ContentMatch) bool
		scan = func(match *ContentMatch) bool {
			seen = append(seen, match)
			for edge := 0; edge < match.EdgeCount(); edge++ {
				edgeType, next := match.Edge(edge)
				if edgeType == nodeType {
					return true
				}
				if !containsMatch(seen, next) && scan(next) {
					return true
				}
			}
			return false
		}
		if scan(parent.ContentMatch) {
			return true
		}
	}
	return false
}

// addMarkToSet keeps a mark set sorted by the schema's order, the same order the renderer nests them in.
func addMarkToSet(schema *Schema, set []Mark, mark Mark) []Mark {
	for _, existing := range set {
		if existing.Type == mark.Type {
			return set
		}
	}
	out := append(append([]Mark(nil), set...), mark)
	for i := len(out) - 1; i > 0; i-- {
		if schema.markRank[out[i].Type] < schema.markRank[out[i-1].Type] {
			out[i], out[i-1] = out[i-1], out[i]
			continue
		}
		break
	}
	return out
}

// enter opens a node, first finding somewhere it can go.
func (p *parseContext) enter(nodeType *NodeType, attrs map[string]any, marks []Mark, wsSet, wsOn, wsFull bool) ([]Mark, bool) {
	probe, ok := nodeType.CreateAndFill(p.schema)
	if !ok {
		probe = Node{Type: nodeType.Name}
	}
	if _, ok := p.findPlace(probe, marks, false); !ok {
		return nil, false
	}
	return p.enterInner(nodeType, attrs, marks, true, wsSet, wsOn, wsFull), true
}

// enterInner opens a node without looking for a place first, which is what a wrapper does.
func (p *parseContext) enterInner(nodeType *NodeType, attrs map[string]any, marks []Mark, solid, wsSet, wsOn, wsFull bool) []Mark {
	p.closeExtra(false)
	top := p.top()
	if top.match != nil {
		top.match = top.match.MatchType(nodeType)
	}
	options := wsOptionsFor(nodeType, wsSet, wsOn, wsFull, top.options)
	if top.options&optOpenLeft != 0 && len(top.content) == 0 {
		options |= optOpenLeft
	}

	// A mark the node can carry itself moves onto the node; the rest stay with the content.
	var applied []Mark
	var remaining []Mark
	for _, mark := range marks {
		if p.marksAllowed(top, mark, nodeType) {
			applied = addMarkToSet(p.schema, applied, mark)
			continue
		}
		remaining = append(remaining, mark)
	}

	p.nodes = append(p.nodes[:p.open+1], newNodeContext(p.schema, nodeType, attrs, applied, solid, nil, options))
	p.open++
	return remaining
}

// closeExtra finishes every node above the open one and hands it to its parent.
func (p *parseContext) closeExtra(openEnd bool) {
	i := len(p.nodes) - 1
	if i <= p.open {
		return
	}
	for ; i > p.open; i-- {
		finished := p.nodes[i].finish(p.schema, openEnd)
		p.nodes[i-1].content = append(p.nodes[i-1].content, finished)
	}
	p.nodes = p.nodes[:p.open+1]
}

func (p *parseContext) finish() Node {
	p.open = 0
	p.closeExtra(false)
	return p.nodes[0].finish(p.schema, false)
}

// sync steps back out to a node that is still open, and reports whether it found it.
func (p *parseContext) sync(to *nodeContext) bool {
	for i := p.open; i >= 0; i-- {
		if p.nodes[i] == to {
			p.open = i
			return true
		}
		if p.localPreserveWS {
			p.nodes[i].options |= optPreserveWS
		}
	}
	return false
}

// matchesContext answers a rule that only applies inside something. The parts are read from the innermost outwards, an empty part standing for any number of levels.
func (p *parseContext) matchesContext(context string) bool {
	if strings.Contains(context, "|") {
		for _, option := range strings.Split(context, "|") {
			if p.matchesContext(strings.TrimSpace(option)) {
				return true
			}
		}
		return false
	}
	parts := strings.Split(context, "/")

	var match func(int, int) bool
	match = func(i, depth int) bool {
		for ; i >= 0; i-- {
			part := parts[i]
			if part == "" {
				if i == len(parts)-1 || i == 0 {
					continue
				}
				for ; depth >= 1; depth-- {
					if match(i-1, depth) {
						return true
					}
				}
				return false
			}
			if depth < 0 {
				return false
			}
			next := p.nodes[depth].nodeType
			if next == nil || (next.Name != part && !next.inGroup(part)) {
				return false
			}
			depth--
		}
		return true
	}
	return match(len(parts)-1, p.open)
}

// textblockFromContext is the block a stray piece of inline text is put into when there is nothing else to hold it.
func (p *parseContext) textblockFromContext() *NodeType {
	for i := range p.schema.Nodes {
		nodeType := &p.schema.Nodes[i]
		if nodeType.ContentMatch.InlineContent() && !nodeType.IsInline && !nodeType.hasRequiredAttrs() {
			return nodeType
		}
	}
	return nil
}

// normalizeList moves a list written straight inside another list into the item above it, which is how browsers read it and how some editors write it.
func normalizeList(dom *vdom.Node) {
	var previousItem *vdom.Node
	remaining := make([]*vdom.Node, 0, len(dom.Children))
	for _, child := range dom.Children {
		name := ""
		if child.Type == vdom.ElementNode {
			name = child.Tag()
		}
		switch {
		case name != "" && listTags[name] && previousItem != nil:
			previousItem.Append(child)
		case name == "li":
			previousItem = child
			remaining = append(remaining, child)
		case name != "":
			previousItem = nil
			remaining = append(remaining, child)
		default:
			remaining = append(remaining, child)
		}
	}
	dom.Children = remaining
}
