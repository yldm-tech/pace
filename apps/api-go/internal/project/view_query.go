package project

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// errQueryHoldsADate is the TypeError Django raises while saving a view whose filters carry a relative date term.
//
// A term like "2_weeks;after;fromnow" resolves to a datetime.date, the query column is a plain JSONField, and json.dumps refuses a date. So the view cannot be saved at all and the request answers 500. Nothing rewrites the value on the way in, and nothing catches the error on the way out.
var errQueryHoldsADate = errors.New("view query: a relative date term resolves to a date, which the query column cannot hold")

// viewQueryFromFilters is issue_filters(filters, "POST") over a JSON body.
//
// It is a second shape rather than a second implementation of the same thing. The GET path parses a query string, where every value is text; this path is handed decoded JSON, where a value is usually a list and occasionally a string, and the POST branch stores it **as it arrived**. The difference is visible: a string date is iterated one character at a time and a list of the same dates is iterated one clause at a time, both upstream, both reachable from this endpoint.
func viewQueryFromFilters(filters map[string]any, today time.Time) (map[string]any, error) {
	query := map[string]any{}

	for _, name := range issueFilterOrder {
		value, present := filters[name]
		if !present {
			continue
		}
		if err := applyJSONFilter(name, value, filters, query, today); err != nil {
			return nil, err
		}
	}
	return query, nil
}

// copyThroughLookups are the filters whose POST branch stores the value it was given, under the lookup named here.
var copyThroughLookups = map[string]string{
	"state":          "state__in",
	"state_group":    "state__group__in",
	"estimate_point": "estimate_point__in",
	"priority":       "priority__in",
	"parent":         "parent__in",
	"labels":         "labels__in",
	"assignees":      "assignees__in",
	"mentions":       "issue_mention__mention__id__in",
	"created_by":     "created_by__in",
	"logged_by":      "logged_by__in",
	"project":        "project__in",
	"cycle":          "issue_cycle__cycle_id__in",
	"module":         "issue_module__module_id__in",
	"inbox_status":   "issue_intake__status__in",
	"subscriber":     "issue_subscribers__subscriber_id__in",
}

// unconditionalLookups are written whenever their filter is named at all, whatever its value — which is why asking about labels with an empty list still excludes the soft-deleted links.
var unconditionalLookups = map[string]string{
	"labels":     "label_issue__deleted_at__isnull",
	"assignees":  "issue_assignee__deleted_at__isnull",
	"cycle":      "issue_cycle__deleted_at__isnull",
	"module":     "issue_module__deleted_at__isnull",
	"subscriber": "issue_subscribers__deleted_at__isnull",
}

// dateLookups are the three filters that run the date grammar rather than storing what they were given.
var dateLookups = map[string]string{
	"created_at":   "created_at__date",
	"updated_at":   "updated_at__date",
	"completed_at": "completed_at__date",
}

func applyJSONFilter(name string, value any, filters, query map[string]any, today time.Time) error {
	if lookup, ok := unconditionalLookups[name]; ok {
		query[lookup] = true
	}
	if lookup, ok := copyThroughLookups[name]; ok {
		if truthyWithLength(value) && !isNullString(value) {
			query[lookup] = value
		}
		return nil
	}
	if term, ok := dateLookups[name]; ok {
		if !truthyWithLength(value) {
			return nil
		}
		return applyJSONDateQueries(query, term, value, today)
	}

	switch name {
	case "name":
		// The one filter with no method branch at all: any non-empty name is a contains lookup.
		if text, ok := value.(string); ok && text != "" {
			query["name__icontains"] = text
		}
	case "start_date", "target_date":
		// On POST these skip the grammar and store the raw value under the bare column.
		if truthyWithLength(value) {
			query[name] = value
		}
	case "type":
		query["state__group__in"] = stateGroupsForType(value)
	case "intake_status":
		// Upstream slip, reproduced: the guard reads intake_status and the value stored is inbox_status, which is absent far more often than not.
		if truthyWithLength(value) && !isNullString(value) {
			query["issue_intake__status__in"] = filters["inbox_status"]
		}
	case "sub_issue":
		// Only the exact string switches it on. The default the Python reads when the key is absent is that same string, but the key is never absent here — the dispatch only runs for a filter that was named — so anything else, a list included, writes nothing.
		if text, ok := value.(string); ok && text == "false" {
			query["parent__isnull"] = true
		}
	case "start_target_date":
		if text, ok := value.(string); ok && text == "true" {
			query["target_date__isnull"] = false
			query["start_date__isnull"] = false
		}
	}
	return nil
}

// applyJSONDateQueries runs the date grammar over whatever shape the value arrived in, then copies the result across — refusing the one case the column cannot hold.
func applyJSONDateQueries(query map[string]any, term string, value any, today time.Time) error {
	queries, err := dateQueriesOf(value)
	if err != nil {
		return err
	}
	parsed := map[string]filterValue{}
	applyDateQueries(parsed, term, queries, today)
	for lookup, result := range parsed {
		switch result.kind {
		case 'b':
			query[lookup] = result.flag
		case 'l':
			query[lookup] = result.list
		default:
			query[lookup] = result.text
		}
	}
	return nil
}

// dateQueriesOf is what Python's loop iterates: the items of a list, or the characters of a string.
func dateQueriesOf(value any) ([]string, error) {
	switch typed := value.(type) {
	case string:
		return splitToCharacters(typed), holdsRelativeTerm(splitToCharacters(typed))
	case []any:
		queries := make([]string, 0, len(typed))
		for _, item := range typed {
			text, ok := item.(string)
			if !ok {
				// Python splits whatever it is given on a semicolon, and a non-string has no split.
				return nil, errQueryHoldsADate
			}
			queries = append(queries, text)
		}
		return queries, holdsRelativeTerm(queries)
	}
	return nil, errQueryHoldsADate
}

// holdsRelativeTerm reports the clause that resolves to a date rather than to text, which is the one the column cannot hold.
func holdsRelativeTerm(queries []string) error {
	for _, query := range queries {
		parts := strings.Split(query, ";")
		if len(parts) == 3 && relativeTermPattern.MatchString(parts[0]) {
			return errQueryHoldsADate
		}
	}
	return nil
}

// stateGroupsForType is the type filter, which has no method branch and always writes a group list.
func stateGroupsForType(value any) []string {
	text, _ := value.(string)
	switch text {
	case "backlog":
		return []string{"backlog"}
	case "active":
		return []string{"unstarted", "started"}
	}
	return []string{"backlog", "unstarted", "started", "completed", "cancelled"}
}

// truthyWithLength is Python's `value and len(value)`: a null, an empty string, an empty list and an empty object are all false.
func truthyWithLength(value any) bool {
	switch typed := value.(type) {
	case nil:
		return false
	case string:
		return typed != ""
	case []any:
		return len(typed) > 0
	case map[string]any:
		return len(typed) > 0
	case bool:
		// len() of a bool raises, so Python would answer 500 here rather than a filter.
		return typed
	}
	return true
}

// isNullString is the `!= "null"` guard, which only ever catches the literal text.
func isNullString(value any) bool {
	text, ok := value.(string)
	return ok && text == "null"
}

// viewQueryJSON renders the query column, or the empty object when there are no filters to derive it from.
func viewQueryJSON(filters []byte, today time.Time) ([]byte, error) {
	decoded, ok := decodeJSON(filters).(map[string]any)
	if !ok || len(decoded) == 0 {
		return []byte("{}"), nil
	}
	query, err := viewQueryFromFilters(decoded, today)
	if err != nil {
		return nil, err
	}
	return json.Marshal(query)
}
