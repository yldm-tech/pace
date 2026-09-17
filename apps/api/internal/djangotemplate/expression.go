package djangotemplate

import (
	"fmt"
	"strconv"
	"strings"
)

// expression is one thing a template asks for: a value, a comparison of two, or several joined by and and or.
type expression struct {
	// operand is set on a leaf: a literal or a variable path with its filters.
	operand *operand
	// operator is set on everything else.
	operator string
	left     *expression
	right    *expression
}

// operand is a literal or a variable, with whatever filters follow it.
type operand struct {
	// literal is set when the operand was written out rather than looked up.
	literal any
	// path is the dotted lookup, empty when the operand is a literal.
	path    []string
	filters []filter
}

type filter struct {
	name     string
	argument string
}

// parseExpression reads a condition or a variable. The grammar is or over and over not over a comparison, which is Django's own precedence.
func parseExpression(source string) (expression, error) {
	tokens := splitExpression(source)
	if len(tokens) == 0 {
		return expression{}, fmt.Errorf("djangotemplate: an empty expression")
	}
	parser := &expressionParser{tokens: tokens}
	parsed, err := parser.parseOr()
	if err != nil {
		return expression{}, err
	}
	if parser.position < len(parser.tokens) {
		return expression{}, fmt.Errorf("djangotemplate: %q is left over in %q", parser.tokens[parser.position], source)
	}
	return parsed, nil
}

// splitExpression breaks a condition into words, keeping a quoted string whole.
func splitExpression(source string) []string {
	tokens := []string{}
	current := strings.Builder{}
	quote := byte(0)
	for index := 0; index < len(source); index++ {
		character := source[index]
		switch {
		case quote != 0:
			current.WriteByte(character)
			if character == quote {
				quote = 0
			}
		case character == '\'' || character == '"':
			quote = character
			current.WriteByte(character)
		case character == ' ' || character == '\t' || character == '\n':
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
		default:
			current.WriteByte(character)
		}
	}
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}
	return tokens
}

type expressionParser struct {
	tokens   []string
	position int
}

func (p *expressionParser) parseOr() (expression, error) {
	left, err := p.parseAnd()
	if err != nil {
		return expression{}, err
	}
	for p.peek() == "or" {
		p.position++
		right, err := p.parseAnd()
		if err != nil {
			return expression{}, err
		}
		// The left side has to be copied before it is pointed at: assigning the joined node back into left would otherwise leave it pointing at itself.
		previous := left
		left = expression{operator: "or", left: &previous, right: &right}
	}
	return left, nil
}

func (p *expressionParser) parseAnd() (expression, error) {
	left, err := p.parseNot()
	if err != nil {
		return expression{}, err
	}
	for p.peek() == "and" {
		p.position++
		right, err := p.parseNot()
		if err != nil {
			return expression{}, err
		}
		previous := left
		left = expression{operator: "and", left: &previous, right: &right}
	}
	return left, nil
}

func (p *expressionParser) parseNot() (expression, error) {
	if p.peek() == "not" {
		p.position++
		inner, err := p.parseNot()
		if err != nil {
			return expression{}, err
		}
		return expression{operator: "not", left: &inner}, nil
	}
	return p.parseComparison()
}

func (p *expressionParser) parseComparison() (expression, error) {
	left, err := p.parseOperand()
	if err != nil {
		return expression{}, err
	}
	switch p.peek() {
	case "==", "!=", ">", "<", ">=", "<=":
		operator := p.tokens[p.position]
		p.position++
		right, err := p.parseOperand()
		if err != nil {
			return expression{}, err
		}
		return expression{operator: operator, left: &left, right: &right}, nil
	}
	return left, nil
}

func (p *expressionParser) parseOperand() (expression, error) {
	if p.position >= len(p.tokens) {
		return expression{}, fmt.Errorf("djangotemplate: an operand is missing")
	}
	text := p.tokens[p.position]
	p.position++
	parsed, err := parseOperand(text)
	if err != nil {
		return expression{}, err
	}
	return expression{operand: &parsed}, nil
}

func (p *expressionParser) peek() string {
	if p.position >= len(p.tokens) {
		return ""
	}
	return p.tokens[p.position]
}

// parseOperand reads one value and its filters.
func parseOperand(text string) (operand, error) {
	parts := splitFilters(text)
	head := parts[0]
	result := operand{}
	switch {
	case len(head) >= 2 && (head[0] == '\'' || head[0] == '"') && head[len(head)-1] == head[0]:
		result.literal = head[1 : len(head)-1]
	case isNumber(head):
		number, err := strconv.ParseFloat(head, 64)
		if err != nil {
			return operand{}, err
		}
		result.literal = number
	case head == "True":
		result.literal = true
	case head == "False":
		result.literal = false
	case head == "None":
		result.literal = nil
	default:
		result.path = strings.Split(head, ".")
	}
	for _, part := range parts[1:] {
		name, argument, _ := strings.Cut(part, ":")
		argument = strings.Trim(argument, `"'`)
		switch name {
		case "length", "add", "slice", "last", "safe":
			result.filters = append(result.filters, filter{name: name, argument: argument})
		default:
			return operand{}, fmt.Errorf("djangotemplate: the filter %q is outside the supported subset", name)
		}
	}
	return result, nil
}

// splitFilters breaks a value from its filters on the pipes that are not inside quotes.
func splitFilters(text string) []string {
	parts := []string{}
	current := strings.Builder{}
	quote := byte(0)
	for index := 0; index < len(text); index++ {
		character := text[index]
		switch {
		case quote != 0:
			current.WriteByte(character)
			if character == quote {
				quote = 0
			}
		case character == '\'' || character == '"':
			quote = character
			current.WriteByte(character)
		case character == '|':
			parts = append(parts, current.String())
			current.Reset()
		default:
			current.WriteByte(character)
		}
	}
	return append(parts, current.String())
}

func isNumber(text string) bool {
	if text == "" {
		return false
	}
	_, err := strconv.ParseFloat(text, 64)
	return err == nil
}
