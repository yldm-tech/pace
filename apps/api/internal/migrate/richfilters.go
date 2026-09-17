package migrate

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

// The legacy-to-rich filter conversion db.0107 runs, ported from plane/utils/filters/converters.py.
//
// It is here rather than in a package of its own because nothing else calls it. The API reads rich_filters and does not convert; this exists to fill the column in once, for the rows that were written before it existed.

// legacyFieldMappings is the converter's DEFAULT_FIELD_MAPPINGS. A legacy key that is not in it is skipped.
var legacyFieldMappings = map[string]string{
	"state":       "state_id",
	"labels":      "label_id",
	"cycle":       "cycle_id",
	"module":      "module_id",
	"assignees":   "assignee_id",
	"mentions":    "mention_id",
	"created_by":  "created_by_id",
	"state_group": "state_group",
	"priority":    "priority",
	"project":     "project_id",
	"start_date":  "start_date",
	"target_date": "target_date",
}

// legacyUUIDFields are the rich field names whose values have to parse as uuids.
var legacyUUIDFields = map[string]bool{
	"state_id": true, "label_id": true, "cycle_id": true, "module_id": true,
	"assignee_id": true, "mention_id": true, "created_by_id": true, "project_id": true,
}

// legacyValidChoices are the rich field names whose values have to be one of a list.
var legacyValidChoices = map[string][]string{
	"state_group": {"backlog", "unstarted", "started", "completed", "cancelled"},
	"priority":    {"urgent", "high", "medium", "low", "none"},
}

var legacyDateFields = map[string]bool{"start_date": true, "target_date": true}

// legacyRelativeDate is the converter's DATE_PATTERN: a count and a unit, which marks a value the conversion gives up on.
var legacyRelativeDate = regexp.MustCompile(`^(\d+)_(weeks|months)$`)

// convertLegacyFilters is the converter's convert(legacy_filters, strict=False).
//
// Only the non-strict path is ported. The migration passes strict=False and nothing else calls this, so the error collecting and the exception the strict path raises would be unreachable code.
//
// The key order of the result matters, because a multi-filter result becomes a list. Python iterates the legacy object in insertion order, which for a value read back out of a jsonb column is the order Postgres stores it in — shortest key first, then bytewise. That order is reproduced here rather than the map's.
func convertLegacyFilters(legacy map[string]json.RawMessage, keyOrder []string) (any, error) {
	converted := map[string]any{}
	var order []string

	for _, legacyKey := range keyOrder {
		raw, present := legacy[legacyKey]
		if !present {
			continue
		}
		richField, mapped := legacyFieldMappings[legacyKey]
		if !mapped {
			continue
		}

		var asList []any
		if err := json.Unmarshal(raw, &asList); err == nil {
			// A list value. An empty one is skipped, as a null is.
			if len(asList) == 0 {
				continue
			}
			if legacyDateFields[richField] {
				for key, value := range convertLegacyDate(richField, asList) {
					if _, seen := converted[key]; !seen {
						order = append(order, key)
					}
					converted[key] = value
				}
				continue
			}
			var valid []any
			for _, value := range asList {
				if validLegacyValue(richField, value) {
					valid = append(valid, value)
				}
			}
			if len(valid) == 0 {
				continue
			}
			key, value := richFilter(richField, "in", valid)
			if _, seen := converted[key]; !seen {
				order = append(order, key)
			}
			converted[key] = value
			continue
		}

		var single any
		if err := json.Unmarshal(raw, &single); err != nil {
			return nil, fmt.Errorf("read %s: %w", legacyKey, err)
		}
		if single == nil {
			continue
		}
		if legacyDateFields[richField] {
			for key, value := range convertLegacyDate(richField, []any{single}) {
				if _, seen := converted[key]; !seen {
					order = append(order, key)
				}
				converted[key] = value
			}
			continue
		}
		if !validLegacyValue(richField, single) {
			continue
		}
		key, value := richFilter(richField, "exact", single)
		if _, seen := converted[key]; !seen {
			order = append(order, key)
		}
		converted[key] = value
	}

	return formatAsRichFilter(converted, order), nil
}

// richFilter is _add_rich_filter: the key is the field and the operator joined by two underscores, and a list value for in or range becomes a comma-separated string.
func richFilter(field, operator string, value any) (string, any) {
	if operator == "in" || operator == "range" {
		if list, isList := value.([]any); isList {
			parts := make([]string, len(list))
			for index, element := range list {
				parts[index] = pythonString(element)
			}
			return field + "__" + operator, strings.Join(parts, ",")
		}
	}
	return field + "__" + operator, value
}

// pythonString is str() of a value that came out of JSON, which is what the join in _add_rich_filter applies.
//
// Only strings reach it in practice — every list a filter carries holds uuids or choice words — but numbers and booleans are written out the way Python would write them rather than the way Go would.
func pythonString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case bool:
		if typed {
			return "True"
		}
		return "False"
	case float64:
		if typed == float64(int64(typed)) {
			return fmt.Sprintf("%d", int64(typed))
		}
		return fmt.Sprintf("%v", typed)
	case nil:
		return "None"
	default:
		return fmt.Sprintf("%v", typed)
	}
}

// validLegacyValue is _validate_value: uuid fields parse, choice fields are in their list, date fields parse as a date, and anything else passes.
func validLegacyValue(richField string, value any) bool {
	text := pythonString(value)
	if legacyUUIDFields[richField] {
		_, err := uuid.Parse(text)
		return err == nil
	}
	if choices, isChoice := legacyValidChoices[richField]; isChoice {
		for _, choice := range choices {
			if text == choice {
				return true
			}
		}
		return false
	}
	if legacyDateFields[richField] {
		return validLegacyDate(text)
	}
	return true
}

// legacyDateLayouts are the shapes a legacy date value actually takes.
//
// Upstream validates with dateutil.parser.parse, which accepts far more than this — it is a general-purpose parser that will take "next tuesday"'s cousins and a dozen orderings of the same three numbers. Reproducing it exactly is not in scope and would not be worth it: the values here are written by the web app's date picker, which emits ISO dates, and the conversion's own DATE_PATTERN has already taken the relative ones out of the running.
//
// A value this rejects and dateutil would have accepted is skipped rather than converted, which leaves the row's rich_filters without that one field. It is the one place in this package where the port is narrower than the Python rather than identical to it, and it is written down here rather than discovered later.
var legacyDateLayouts = []string{
	"2006-01-02",
	"2006-01-02T15:04:05",
	"2006-01-02T15:04:05Z07:00",
	"2006-01-02 15:04:05",
	"2006/01/02",
	"01/02/2006",
	time.RFC3339,
}

func validLegacyDate(value string) bool {
	for _, layout := range legacyDateLayouts {
		if _, err := time.Parse(layout, value); err == nil {
			return true
		}
	}
	return false
}

// convertLegacyDate is _convert_date_value: a single date becomes an exact match, an after and a before become a range, and everything else is dropped.
func convertLegacyDate(field string, values []any) map[string]any {
	texts := make([]string, len(values))
	for index, value := range values {
		texts[index] = pythonString(value)
	}
	// A relative pattern anywhere gives up on the whole field.
	for _, text := range texts {
		if strings.Contains(text, ";") {
			parts := strings.Split(text, ";")
			if len(parts) > 0 && legacyRelativeDate.MatchString(parts[0]) {
				return nil
			}
		}
	}
	// More than two values is a condition this does not try to express.
	if len(texts) > 2 {
		return nil
	}

	var exact, after, before []string
	for _, text := range texts {
		if !strings.Contains(text, ";") {
			if validLegacyDate(text) {
				exact = append(exact, text)
			}
			continue
		}
		parts := strings.Split(text, ";")
		if len(parts) < 2 {
			continue
		}
		if !validLegacyDate(parts[0]) {
			continue
		}
		switch parts[1] {
		case "after":
			after = append(after, parts[0])
		case "before":
			before = append(before, parts[0])
		}
	}

	result := map[string]any{}
	switch {
	case len(after) == 1 && len(before) == 1 && len(exact) == 0:
		// min and max of the two, compared as strings, which is what Python does with them.
		low, high := after[0], before[0]
		if high < low {
			low, high = high, low
		}
		key, value := richFilter(field, "range", []any{low, high})
		result[key] = value
	case len(exact) == 1 && len(after) == 0 && len(before) == 0:
		key, value := richFilter(field, "exact", exact[0])
		result[key] = value
	}
	return result
}

// formatAsRichFilter is _format_as_rich_filter: nothing becomes an empty object, one filter stands on its own, and several are wrapped in an and.
func formatAsRichFilter(flat map[string]any, order []string) any {
	if len(flat) == 0 {
		return map[string]any{}
	}
	if len(flat) == 1 {
		return flat
	}
	conditions := make([]any, 0, len(order))
	for _, key := range order {
		conditions = append(conditions, map[string]any{key: flat[key]})
	}
	return map[string]any{"and": conditions}
}

// jsonbKeyOrder is the order Postgres hands an object's keys back in, which is the order Python then iterates.
//
// jsonb does not keep the order the keys were written in. It stores them shortest first and, within a length, bytewise — so that is the order a dict built from one iterates in, and the order the "and" list comes out in.
func jsonbKeyOrder(object map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if len(keys[i]) != len(keys[j]) {
			return len(keys[i]) < len(keys[j])
		}
		return keys[i] < keys[j]
	})
	return keys
}
