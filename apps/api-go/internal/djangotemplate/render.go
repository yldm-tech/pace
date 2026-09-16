package djangotemplate

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

// Render writes the template out against a context.
//
// A lookup that finds nothing is empty rather than an error, which is what Django does and what the templates rely on — half their conditions are "is this here at all".
func (template *Template) Render(context map[string]any) (string, error) {
	var builder strings.Builder
	if err := renderNodes(template.nodes, context, &builder); err != nil {
		return "", err
	}
	return builder.String(), nil
}

func renderNodes(nodes []node, context map[string]any, builder *strings.Builder) error {
	for _, current := range nodes {
		switch typed := current.(type) {
		case textNode:
			builder.WriteString(string(typed))
		case variableNode:
			value, safe, err := evaluate(typed.expression, context)
			if err != nil {
				return err
			}
			// A key the context never had writes nothing; a key whose value is None writes the word. Django draws that line with string_if_invalid, which defaults to the empty string, and the templates rely on it — half their numbers are written by a context that does not always carry them.
			rendered := ""
			if _, absent := value.(missing); !absent {
				rendered = renderValue(value)
			}
			if !safe {
				rendered = escapeHTML(rendered)
			}
			builder.WriteString(rendered)
		case *ifNode:
			if err := renderIf(typed, context, builder); err != nil {
				return err
			}
		case *forNode:
			if err := renderFor(typed, context, builder); err != nil {
				return err
			}
		}
	}
	return nil
}

func renderIf(node *ifNode, context map[string]any, builder *strings.Builder) error {
	for _, arm := range node.branches {
		if arm.condition == nil {
			return renderNodes(arm.body, context, builder)
		}
		value, _, err := evaluate(*arm.condition, context)
		if err != nil {
			return err
		}
		if truthy(value) {
			return renderNodes(arm.body, context, builder)
		}
	}
	return nil
}

func renderFor(node *forNode, context map[string]any, builder *strings.Builder) error {
	value, _, err := evaluate(node.iterable, context)
	if err != nil {
		return err
	}
	items, err := iterate(value)
	if err != nil {
		return err
	}
	// The loop variable shadows whatever the context had and is put back afterwards, which is what a template scope comes to.
	previous, had := context[node.name]
	defer func() {
		if had {
			context[node.name] = previous
			return
		}
		delete(context, node.name)
	}()
	for _, item := range items {
		context[node.name] = item
		if err := renderNodes(node.body, context, builder); err != nil {
			return err
		}
	}
	return nil
}

// evaluate works out what an expression comes to, and whether the result may be written without escaping.
func evaluate(expr expression, context map[string]any) (any, bool, error) {
	if expr.operand != nil {
		return evaluateOperand(*expr.operand, context)
	}
	switch expr.operator {
	case "not":
		value, _, err := evaluate(*expr.left, context)
		return !truthy(value), false, err
	case "and":
		left, _, err := evaluate(*expr.left, context)
		if err != nil || !truthy(left) {
			return false, false, err
		}
		right, _, err := evaluate(*expr.right, context)
		return truthy(right), false, err
	case "or":
		left, _, err := evaluate(*expr.left, context)
		if err != nil {
			return nil, false, err
		}
		if truthy(left) {
			return true, false, nil
		}
		right, _, err := evaluate(*expr.right, context)
		return truthy(right), false, err
	}
	left, _, err := evaluate(*expr.left, context)
	if err != nil {
		return nil, false, err
	}
	right, _, err := evaluate(*expr.right, context)
	if err != nil {
		return nil, false, err
	}
	return compare(expr.operator, left, right)
}

func evaluateOperand(op operand, context map[string]any) (any, bool, error) {
	var value any
	if op.path == nil {
		value = op.literal
	} else {
		found, present := lookup(context, op.path)
		value = found
		if !present {
			value = missing{}
		}
	}
	safe := false
	for _, applied := range op.filters {
		switch applied.name {
		case "length":
			value = length(value)
		case "last":
			value = last(value)
		case "safe":
			safe = true
		case "add":
			addend, err := strconv.ParseFloat(applied.argument, 64)
			if err != nil {
				return nil, false, fmt.Errorf("djangotemplate: add takes a number, not %q", applied.argument)
			}
			value = toNumber(value) + addend
		case "slice":
			value = sliceValue(value, applied.argument)
		}
	}
	return value, safe, nil
}

// missing marks a lookup that found no key at all, which is not the same as one that found a key holding nothing.
type missing struct{}

// lookup walks a dotted path and says whether every step of it was there. A numeric step reads into a list.
func lookup(context map[string]any, path []string) (any, bool) {
	var current any = context
	for _, step := range path {
		if current == nil {
			return nil, false
		}
		switch typed := current.(type) {
		case map[string]any:
			value, present := typed[step]
			if !present {
				return nil, false
			}
			current = value
			continue
		case map[string]string:
			value, present := typed[step]
			if !present {
				return nil, false
			}
			current = value
			continue
		}
		reflected := reflect.ValueOf(current)
		// A numeric step into a string takes that character, which is how the templates write somebody's initial.
		if reflected.Kind() == reflect.String {
			index, err := strconv.Atoi(step)
			if err != nil || index < 0 || index >= reflected.Len() {
				return nil, false
			}
			current = string(reflected.String()[index])
			continue
		}
		if reflected.Kind() == reflect.Slice || reflected.Kind() == reflect.Array {
			index, err := strconv.Atoi(step)
			if err != nil || index < 0 || index >= reflected.Len() {
				return nil, false
			}
			current = reflected.Index(index).Interface()
			continue
		}
		if reflected.Kind() == reflect.Map {
			value := reflected.MapIndex(reflect.ValueOf(step))
			if !value.IsValid() {
				return nil, false
			}
			current = value.Interface()
			continue
		}
		return nil, false
	}
	return current, true
}

// compare is what the four operators the templates use come to. A comparison of two things that cannot be compared is false rather than an error, which is how Django's own comparison behaves once it has coerced.
func compare(operator string, left, right any) (any, bool, error) {
	switch operator {
	case "==":
		return equalValues(left, right), false, nil
	case "!=":
		return !equalValues(left, right), false, nil
	}
	leftNumber, rightNumber := toNumber(left), toNumber(right)
	switch operator {
	case ">":
		return leftNumber > rightNumber, false, nil
	case "<":
		return leftNumber < rightNumber, false, nil
	case ">=":
		return leftNumber >= rightNumber, false, nil
	case "<=":
		return leftNumber <= rightNumber, false, nil
	}
	return nil, false, fmt.Errorf("djangotemplate: %q is outside the supported subset", operator)
}

func equalValues(left, right any) bool {
	// A key that was never there compares as the empty string, which is what it renders as.
	if _, absent := left.(missing); absent {
		left = ""
	}
	if _, absent := right.(missing); absent {
		right = ""
	}
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	leftText, leftIsText := left.(string)
	rightText, rightIsText := right.(string)
	if leftIsText && rightIsText {
		return leftText == rightText
	}
	if leftIsText != rightIsText {
		// One side was written as a string and the other is a number, which Django compares by value once it has rendered.
		return renderValue(left) == renderValue(right)
	}
	return toNumber(left) == toNumber(right)
}

// truthy is Python's truthiness, which is what decides every condition in these templates.
func truthy(value any) bool {
	switch typed := value.(type) {
	case missing:
		return false
	case nil:
		return false
	case bool:
		return typed
	case string:
		return typed != ""
	case int:
		return typed != 0
	case int64:
		return typed != 0
	case float64:
		return typed != 0
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Slice, reflect.Array, reflect.Map:
		return reflected.Len() > 0
	case reflect.Ptr, reflect.Interface:
		return !reflected.IsNil()
	}
	return true
}

func length(value any) int {
	if value == nil {
		return 0
	}
	if _, absent := value.(missing); absent {
		return 0
	}
	if text, ok := value.(string); ok {
		return len(text)
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Slice, reflect.Array, reflect.Map:
		return reflected.Len()
	}
	return 0
}

func last(value any) any {
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Slice, reflect.Array:
		if reflected.Len() == 0 {
			return nil
		}
		return reflected.Index(reflected.Len() - 1).Interface()
	case reflect.String:
		text := reflected.String()
		if text == "" {
			return nil
		}
		return string(text[len(text)-1])
	}
	return nil
}

// sliceValue is the slice filter over the one shape the templates use, which is a count from the start.
func sliceValue(value any, argument string) any {
	before, after, found := strings.Cut(argument, ":")
	if !found {
		return value
	}
	reflected := reflect.ValueOf(value)
	if reflected.Kind() != reflect.Slice && reflected.Kind() != reflect.Array {
		return value
	}
	start := 0
	if before != "" {
		parsed, err := strconv.Atoi(before)
		if err != nil {
			return value
		}
		start = parsed
	}
	end := reflected.Len()
	if after != "" {
		parsed, err := strconv.Atoi(after)
		if err != nil {
			return value
		}
		end = parsed
	}
	if start < 0 {
		start = 0
	}
	if end > reflected.Len() {
		end = reflected.Len()
	}
	if start > end {
		return reflect.MakeSlice(reflect.SliceOf(reflected.Type().Elem()), 0, 0).Interface()
	}
	return reflected.Slice(start, end).Interface()
}

func toNumber(value any) float64 {
	switch typed := value.(type) {
	case missing:
		return 0
	case bool:
		if typed {
			return 1
		}
		return 0
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case float64:
		return typed
	case string:
		number, err := strconv.ParseFloat(typed, 64)
		if err != nil {
			return 0
		}
		return number
	}
	return 0
}

// renderValue writes a value the way Django writes it into html.
func renderValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return "None"
	case string:
		return typed
	case bool:
		if typed {
			return "True"
		}
		return "False"
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	case float64:
		if typed == float64(int64(typed)) {
			return strconv.FormatInt(int64(typed), 10)
		}
		return strconv.FormatFloat(typed, 'g', -1, 64)
	}
	return fmt.Sprint(value)
}

var htmlReplacer = strings.NewReplacer(
	"&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&#x27;",
)

// escapeHTML is Django's escape, which uses the hexadecimal entity for an apostrophe where Go's own would use a decimal one.
func escapeHTML(value string) string { return htmlReplacer.Replace(value) }

// iterate is what a for loop walks. Anything that is not a list is nothing to walk, which is what Django does with a value it cannot iterate rather than failing.
func iterate(value any) ([]any, error) {
	if value == nil {
		return nil, nil
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Slice, reflect.Array:
		items := make([]any, 0, reflected.Len())
		for index := 0; index < reflected.Len(); index++ {
			items = append(items, reflected.Index(index).Interface())
		}
		return items, nil
	}
	return nil, nil
}
