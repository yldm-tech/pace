package complexfilters

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// maxDepth is ComplexFilterBackend.default_max_depth, and no view raises it.
const maxDepth = 5

// Refusal is the DRF ValidationError the backend raises, which answers 400 with the message and the code beside it.
type Refusal struct {
	Message string
	Code    string
}

func (refusal *Refusal) Error() string { return refusal.Code + ": " + refusal.Message }

// Body is what the client receives.
func (refusal *Refusal) Body() map[string]any {
	return map[string]any{"message": refusal.Message, "code": refusal.Code}
}

func refuse(code, message string) *Refusal { return &Refusal{Message: message, Code: code} }

// Parse reads the filters parameter and answers with the Q tree it stands for.
//
// A nil node and a nil refusal together mean the caller asked for nothing, which leaves the list unfiltered.
func Parse(raw string) (*Node, *Refusal) {
	if raw == "" {
		return nil, nil
	}
	var tree map[string]any
	decoder := json.NewDecoder(strings.NewReader(raw))
	// Numbers stay as they were written, since what reaches the form is str() of whatever came out of the json.
	decoder.UseNumber()
	if err := decoder.Decode(&tree); err != nil {
		return nil, refuse("invalid_json", "Invalid JSON for 'filter'. Expected a valid JSON object.")
	}
	return ParseTree(tree)
}

// ParseTree is the same work over an already decoded object, which is the shape the space app hands in.
func ParseTree(tree map[string]any) (*Node, *Refusal) {
	if len(tree) == 0 {
		return nil, nil
	}
	if refusal := validateStructure(tree, 1); refusal != nil {
		return nil, refusal
	}
	if refusal := validateFields(tree); refusal != nil {
		return nil, refusal
	}
	return evaluate(tree)
}

// validateStructure is _validate_structure: one operator to an object, no mixing it with fields, and no more than five levels of nesting.
func validateStructure(node map[string]any, depth int) *Refusal {
	if depth > maxDepth {
		return refuse("max_depth_exceeded",
			fmt.Sprintf("Filter nesting is too deep (max %d); found depth %d", maxDepth, depth))
	}
	if len(node) == 0 {
		return refuse("empty_filter_object", "Filter objects must not be empty")
	}

	operators := []string{}
	for key := range node {
		if isOperator(key) {
			operators = append(operators, key)
		}
	}
	sort.Strings(operators)
	if len(operators) > 1 {
		return refuse("multiple_logical_operators",
			"A filter object cannot contain multiple logical operators at the same level")
	}
	if len(operators) == 1 {
		key := operators[0]
		if len(node) != 1 {
			return refuse("mixed_operator_and_fields",
				fmt.Sprintf("Cannot mix logical operator '%s' with field keys at the same level", key))
		}
		operator := strings.ToLower(key)
		value := node[key]
		if operator == "not" {
			child, ok := value.(map[string]any)
			if !ok {
				return refuse("invalid_not_child", "'not' must be a single JSON object")
			}
			return validateStructure(child, depth+1)
		}
		children, ok := value.([]any)
		if !ok || len(children) == 0 {
			return refuse("invalid_operator_children",
				fmt.Sprintf("'%s' must be a non-empty list of filter objects", operator))
		}
		for _, entry := range children {
			child, ok := entry.(map[string]any)
			if !ok {
				return refuse("invalid_operator_child_type",
					fmt.Sprintf("All children of '%s' must be JSON objects", operator))
			}
			if refusal := validateStructure(child, depth+1); refusal != nil {
				return refusal
			}
		}
		return nil
	}
	return validateLeaf(node)
}

// validateLeaf is _validate_leaf: no operators here, and every value a scalar, null, or a non-empty list of scalars.
func validateLeaf(leaf map[string]any) *Refusal {
	keys := sortedKeys(leaf)
	for _, key := range keys {
		if isOperator(key) {
			return refuse("operator_in_leaf", "Logical operators cannot appear in a leaf filter object")
		}
		value := leaf[key]
		if list, ok := value.([]any); ok {
			if len(list) == 0 {
				return refuse("empty_list_value", fmt.Sprintf("List value for '%s' must not be empty", key))
			}
			for _, item := range list {
				if !isScalar(item) {
					return refuse("non_scalar_list_item",
						fmt.Sprintf("List value for '%s' must contain only scalar items", key))
				}
			}
			continue
		}
		if !isScalar(value) {
			return refuse("invalid_value_type",
				fmt.Sprintf("Value for '%s' must be a scalar, null, or list/tuple of scalars", key))
		}
	}
	return nil
}

// validateFields is _validate_fields: every field named anywhere in the tree has to be one the set declares.
func validateFields(node map[string]any) *Refusal {
	for _, field := range fieldNames(node) {
		if _, declared := issueFilters[field]; !declared {
			return refuse("invalid_filter_field", fmt.Sprintf("Filtering on field '%s' is not allowed", field))
		}
	}
	return nil
}

func fieldNames(node map[string]any) []string {
	names := []string{}
	for _, key := range sortedKeys(node) {
		value := node[key]
		if isOperator(key) {
			if strings.ToLower(key) == "not" {
				if child, ok := value.(map[string]any); ok {
					names = append(names, fieldNames(child)...)
				}
				continue
			}
			if children, ok := value.([]any); ok {
				for _, entry := range children {
					if child, ok := entry.(map[string]any); ok {
						names = append(names, fieldNames(child)...)
					}
				}
			}
			continue
		}
		names = append(names, key)
	}
	return names
}

// evaluate is _evaluate_node. The operator lookup is case sensitive here while the validation above was not, which is the one place the two disagree — an object keyed "OR" passes every check and is then read as a leaf, where no field matches and nothing is filtered.
func evaluate(node map[string]any) (*Node, *Refusal) {
	if children, present := node["or"]; present {
		return evaluateConnector(children, Or)
	}
	if children, present := node["and"]; present {
		return evaluateConnector(children, And)
	}
	if child, present := node["not"]; present {
		branch, ok := child.(map[string]any)
		if !ok {
			return nil, nil
		}
		inner, refusal := evaluate(branch)
		if refusal != nil {
			return nil, refusal
		}
		if inner == nil {
			return nil, nil
		}
		return Invert(inner), nil
	}
	return buildLeaf(node)
}

func evaluateConnector(children any, connector Connector) (*Node, *Refusal) {
	entries, ok := children.([]any)
	if !ok || len(entries) == 0 {
		return nil, nil
	}
	combined := EmptyNode()
	for _, entry := range entries {
		branch, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		child, refusal := evaluate(branch)
		if refusal != nil {
			return nil, refusal
		}
		if child == nil {
			continue
		}
		combined = Combine(combined, child, connector)
	}
	return combined, nil
}

// buildLeaf is _build_leaf_q: the leaf becomes a QueryDict, the set cleans it, and what is left is ANDed together.
//
// The QueryDict step is where a list loses everything but its last item. Django writes the list with setlist and then the form reads it with get, which takes the last value — so `{"priority__in": ["high", "urgent"]}` filters on urgent alone. Sending the same thing as `"high,urgent"` works. Reproduced rather than corrected.
func buildLeaf(leaf map[string]any) (*Node, *Refusal) {
	if len(leaf) == 0 {
		return EmptyNode(), nil
	}
	combined := EmptyNode()
	for _, key := range sortedKeys(leaf) {
		spec, declared := issueFilters[key]
		if !declared {
			// An undeclared key never reaches here, since the field check above refuses it first. The one exception is a capitalised operator, which is simply not a filter and contributes nothing.
			continue
		}
		value, err := spec.clean(queryDictValue(leaf[key]))
		if err != nil {
			return nil, refuse("invalid_filterset", "Invalid filter parameters")
		}
		combined = Combine(combined, NewNode(spec.conditions(value)...), And)
	}
	return combined, nil
}

// queryDictValue is what a json value becomes on its way into the QueryDict: str() of it, with null becoming the empty string and a list keeping only its last item.
func queryDictValue(value any) string {
	if list, ok := value.([]any); ok {
		if len(list) == 0 {
			return ""
		}
		return queryDictValue(list[len(list)-1])
	}
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case bool:
		// Python's str() of a boolean is capitalised, which is exactly what the boolean widget looks for.
		if typed {
			return "True"
		}
		return "False"
	case json.Number:
		return typed.String()
	case float64:
		return strconv.FormatFloat(typed, 'g', -1, 64)
	}
	return fmt.Sprint(value)
}

func isOperator(key string) bool {
	switch strings.ToLower(key) {
	case "or", "and", "not":
		return true
	}
	return false
}

func isScalar(value any) bool {
	switch value.(type) {
	case nil, string, bool, json.Number, float64, int:
		return true
	}
	return false
}

func sortedKeys(node map[string]any) []string {
	keys := make([]string, 0, len(node))
	for key := range node {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
