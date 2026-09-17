package ydoc

import (
	"fmt"
	"strings"

	"github.com/yldm-tech/pace/apps/api/internal/vdom"
)

// selector is a CSS selector a parse rule matches elements with.
//
// It covers the shapes the editor's rules actually use — a tag name, an attribute test, and a negation — and refuses anything else rather than quietly matching nothing. A rule added upstream with a selector this does not understand fails at load, where somebody will see it, rather than at parse time, where the element would simply never be recognised.
type selector struct {
	source string
	tag    string
	tests  []attributeTest
	nots   []selector
}

type attributeTest struct {
	name string
	// operator is empty for a presence test, "=" for equality and "^=" for a prefix.
	operator string
	value    string
}

func parseSelector(source string) (selector, error) {
	parsed := selector{source: source}
	rest := source

	// The tag name runs up to the first bracket or colon, and may be missing entirely.
	end := strings.IndexAny(rest, "[:")
	if end == -1 {
		end = len(rest)
	}
	parsed.tag = strings.ToLower(strings.TrimSpace(rest[:end]))
	rest = rest[end:]
	if !isSelectorName(parsed.tag) {
		return selector{}, fmt.Errorf("ydoc: selector %q has a tag this does not understand", source)
	}

	for rest != "" {
		switch {
		case strings.HasPrefix(rest, "["):
			test, remainder, err := parseAttributeTest(rest)
			if err != nil {
				return selector{}, fmt.Errorf("ydoc: selector %q: %w", source, err)
			}
			parsed.tests = append(parsed.tests, test)
			rest = remainder
		case strings.HasPrefix(rest, ":not("):
			depth := 0
			end := -1
			for i, r := range rest[4:] {
				if r == '(' {
					depth++
				}
				if r == ')' {
					depth--
					if depth == 0 {
						end = i + 4
						break
					}
				}
			}
			if end == -1 {
				return selector{}, fmt.Errorf("ydoc: selector %q has an unclosed :not()", source)
			}
			inner, err := parseSelector(rest[5:end])
			if err != nil {
				return selector{}, err
			}
			parsed.nots = append(parsed.nots, inner)
			rest = rest[end+1:]
		default:
			return selector{}, fmt.Errorf("ydoc: selector %q has a part this does not understand: %q", source, rest)
		}
	}
	return parsed, nil
}

func parseAttributeTest(source string) (attributeTest, string, error) {
	end := strings.Index(source, "]")
	if end == -1 {
		return attributeTest{}, "", fmt.Errorf("unclosed attribute test")
	}
	body := source[1:end]
	rest := source[end+1:]

	for _, operator := range []string{"^=", "$=", "*=", "~=", "|=", "="} {
		index := strings.Index(body, operator)
		if index == -1 {
			continue
		}
		if operator != "=" && operator != "^=" {
			return attributeTest{}, "", fmt.Errorf("attribute operator %q is not supported", operator)
		}
		name := strings.TrimSpace(body[:index])
		value := strings.TrimSpace(body[index+len(operator):])
		value = strings.Trim(value, `"'`)
		if !isSelectorName(name) {
			return attributeTest{}, "", fmt.Errorf("attribute name %q is not supported", name)
		}
		return attributeTest{name: name, operator: operator, value: value}, rest, nil
	}

	name := strings.TrimSpace(body)
	if !isSelectorName(name) || name == "" {
		return attributeTest{}, "", fmt.Errorf("attribute name %q is not supported", name)
	}
	return attributeTest{name: name}, rest, nil
}

// isSelectorName accepts what a tag or an attribute may be spelled with here: letters, digits, hyphens and underscores. An empty name is allowed for a tag, which is how a selector that is only an attribute test is written.
func isSelectorName(value string) bool {
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

// matches reports whether the element satisfies the selector.
func (s selector) matches(element *vdom.Node) bool {
	if element.Type != vdom.ElementNode || element.IsFragment() {
		return false
	}
	if s.tag != "" && element.Tag() != s.tag {
		return false
	}
	for _, test := range s.tests {
		value, present := element.Attr(test.name)
		if !present {
			return false
		}
		switch test.operator {
		case "":
		case "=":
			if value != test.value {
				return false
			}
		case "^=":
			if !strings.HasPrefix(value, test.value) {
				return false
			}
		}
	}
	for _, not := range s.nots {
		if not.matches(element) {
			return false
		}
	}
	return true
}
