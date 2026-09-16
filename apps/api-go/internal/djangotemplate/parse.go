// Package djangotemplate renders the subset of Django's template language the notification emails are written in.
//
// The templates themselves are copied from apps/api unchanged. Translating them into Go's own template syntax was the other option, and it was rejected for a reason worth stating: a translated template drifts. Somebody changes a line of the html on the Python side, the two copies stop agreeing, and nobody finds out until a customer reads an email that is missing a row. Keeping the file byte for byte means an upstream change is a copy rather than a re-translation, and a diff tells you whether the two are the same.
//
// The subset is what those templates actually use: variables with dotted paths and numeric indices, if/elif/else, for, the operators and, or, not, ==, != and >, and the filters length, add, slice, last and safe. Anything outside it is refused at parse time rather than rendered wrongly.
package djangotemplate

import (
	"fmt"
	"strings"
)

// node is one piece of a parsed template.
type node interface{}

// textNode is the html between the tags.
type textNode string

// variableNode is a {{ ... }}, with whatever filters were applied to it.
type variableNode struct {
	expression expression
}

// branch is one arm of an if: its condition and what to render when it holds. The else arm has no condition.
type branch struct {
	condition *expression
	body      []node
}

type ifNode struct {
	branches []branch
}

type forNode struct {
	name     string
	iterable expression
	body     []node
}

// Template is a parsed template, ready to render as many times as you like.
type Template struct {
	nodes []node
}

// Parse reads a template. A construct outside the supported subset is an error here rather than a surprise at render time.
func Parse(source string) (*Template, error) {
	tokens, err := tokenize(source)
	if err != nil {
		return nil, err
	}
	parser := &parser{tokens: tokens}
	nodes, err := parser.parseUntil()
	if err != nil {
		return nil, err
	}
	if parser.position < len(parser.tokens) {
		return nil, fmt.Errorf("djangotemplate: unexpected %q", parser.tokens[parser.position].text)
	}
	return &Template{nodes: nodes}, nil
}

// tokenKind says which of the three things a token is.
type tokenKind int

const (
	tokenText tokenKind = iota
	tokenVariable
	tokenBlock
)

type token struct {
	kind tokenKind
	text string
}

// tokenize splits the source into text, {{ variables }} and {% blocks %}. A comment is dropped where it stands.
func tokenize(source string) ([]token, error) {
	tokens := []token{}
	for len(source) > 0 {
		start := strings.IndexAny(source, "{")
		opening := ""
		for offset := start; offset >= 0 && offset < len(source)-1; {
			switch source[offset : offset+2] {
			case "{{", "{%", "{#":
				opening = source[offset : offset+2]
				start = offset
			}
			if opening != "" {
				break
			}
			next := strings.IndexAny(source[offset+1:], "{")
			if next < 0 {
				offset = -1
				start = -1
				break
			}
			offset += next + 1
			start = offset
		}
		if opening == "" {
			tokens = append(tokens, token{kind: tokenText, text: source})
			break
		}
		if start > 0 {
			tokens = append(tokens, token{kind: tokenText, text: source[:start]})
		}
		closing := map[string]string{"{{": "}}", "{%": "%}", "{#": "#}"}[opening]
		end := strings.Index(source[start:], closing)
		if end < 0 {
			return nil, fmt.Errorf("djangotemplate: %s is never closed", opening)
		}
		inner := strings.TrimSpace(source[start+2 : start+end])
		switch opening {
		case "{{":
			tokens = append(tokens, token{kind: tokenVariable, text: inner})
		case "{%":
			tokens = append(tokens, token{kind: tokenBlock, text: inner})
		}
		source = source[start+end+len(closing):]
	}
	return tokens, nil
}

type parser struct {
	tokens   []token
	position int
}

// parseUntil reads nodes until it meets one of the given block tags or runs out, leaving that tag unconsumed.
func (p *parser) parseUntil(stops ...string) ([]node, error) {
	nodes := []node{}
	for p.position < len(p.tokens) {
		current := p.tokens[p.position]
		if current.kind == tokenBlock {
			name := blockName(current.text)
			for _, stop := range stops {
				if name == stop {
					return nodes, nil
				}
			}
		}
		p.position++
		switch current.kind {
		case tokenText:
			nodes = append(nodes, textNode(current.text))
		case tokenVariable:
			parsed, err := parseExpression(current.text)
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, variableNode{expression: parsed})
		case tokenBlock:
			parsed, err := p.parseBlock(current.text)
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, parsed)
		}
	}
	return nodes, nil
}

func (p *parser) parseBlock(text string) (node, error) {
	name := blockName(text)
	switch name {
	case "if":
		return p.parseIf(strings.TrimSpace(text[len("if"):]))
	case "for":
		return p.parseFor(strings.TrimSpace(text[len("for"):]))
	}
	return nil, fmt.Errorf("djangotemplate: %q is outside the supported subset", name)
}

func (p *parser) parseIf(condition string) (node, error) {
	parsed, err := parseExpression(condition)
	if err != nil {
		return nil, err
	}
	result := &ifNode{}
	for {
		body, err := p.parseUntil("elif", "else", "endif")
		if err != nil {
			return nil, err
		}
		// The condition is copied before it is pointed at, since the next elif reassigns the variable and every branch would otherwise end up holding the last one's test.
		condition := parsed
		result.branches = append(result.branches, branch{condition: &condition, body: body})
		if p.position >= len(p.tokens) {
			return nil, fmt.Errorf("djangotemplate: an if is never closed")
		}
		current := p.tokens[p.position]
		p.position++
		switch blockName(current.text) {
		case "endif":
			return result, nil
		case "else":
			body, err := p.parseUntil("endif")
			if err != nil {
				return nil, err
			}
			result.branches = append(result.branches, branch{body: body})
			if p.position >= len(p.tokens) {
				return nil, fmt.Errorf("djangotemplate: an if is never closed")
			}
			p.position++
			return result, nil
		case "elif":
			parsed, err = parseExpression(strings.TrimSpace(current.text[len("elif"):]))
			if err != nil {
				return nil, err
			}
		}
	}
}

func (p *parser) parseFor(header string) (node, error) {
	parts := strings.Fields(header)
	if len(parts) != 3 || parts[1] != "in" {
		return nil, fmt.Errorf("djangotemplate: %q is not a for this understands", header)
	}
	iterable, err := parseExpression(parts[2])
	if err != nil {
		return nil, err
	}
	body, err := p.parseUntil("endfor")
	if err != nil {
		return nil, err
	}
	if p.position >= len(p.tokens) {
		return nil, fmt.Errorf("djangotemplate: a for is never closed")
	}
	p.position++
	return &forNode{name: parts[0], iterable: iterable, body: body}, nil
}

func blockName(text string) string {
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}
