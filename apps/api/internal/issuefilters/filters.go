// Package issuefilters is issue_filters and the SQL it becomes.
//
// Every application reads the same filter parameters off a query string — the session API's lists and analytics, the space app's published board — so the parsing and the SQL live here rather than in whichever of them was written first.
package issuefilters

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Value is one entry of the dictionary issue_filters builds. Django puts three different kinds of thing in that dictionary — a list of uuids, a raw string, a boolean — and which kind a lookup gets depends on the request method, so the kind is carried rather than flattened.
// Value is one parsed filter: a list, a piece of text or a flag.
type Value struct {
	kind byte // 'l' list, 't' text, 'b' bool
	list []string
	text string
	flag bool
}

// ListValue, TextValue and BoolValue build the three shapes a filter can take.
func ListValue(values []string) Value { return Value{kind: 'l', list: values} }
func TextValue(value string) Value    { return Value{kind: 't', text: value} }
func BoolValue(value bool) Value      { return Value{kind: 'b', flag: value} }

// String renders a value the way the fixture does, so the two sides compare literally.
// Kind reports which of the three shapes the value carries: 'l' for a list, 'b' for a flag, 't' for text.
func (value Value) Kind() byte { return value.kind }

// List, Flag and Text are what the value holds, and only one of them is meaningful.
func (value Value) List() []string { return value.list }
func (value Value) Flag() bool     { return value.flag }
func (value Value) Text() string   { return value.text }

func (value Value) String() string {
	switch value.kind {
	case 'l':
		return "[" + strings.Join(value.list, ",") + "]"
	case 'b':
		if value.flag {
			return "true"
		}
		return "false"
	}
	return value.text
}

// Order is the order the dispatch table iterates. It matters: the filters share one dictionary, so a later one can overwrite a lookup an earlier one wrote.
// Order is the order Django reads the parameters in, which is the order the lookups come out in.
var Order = []string{
	"state", "state_group", "estimate_point", "priority", "parent", "labels",
	"assignees", "mentions", "created_by", "logged_by", "name", "created_at",
	"updated_at", "start_date", "target_date", "completed_at", "type", "project",
	"cycle", "module", "intake_status", "inbox_status", "sub_issue", "subscriber",
	"start_target_date",
}

// issueFilters is plane.utils.issue_filters.issue_filters. A filter runs when its parameter is present at all, even with an empty value, and the today argument is what the relative date terms are measured from.
// Parse is issue_filters: the query parameters a list route accepts, read in the order Django reads them.
func Parse(params map[string]string, method, prefix string, today time.Time) map[string]Value {
	result := map[string]Value{}
	for _, name := range Order {
		if _, present := params[name]; !present {
			continue
		}
		applyIssueFilter(name, params, result, method, prefix, today)
	}
	return result
}

func applyIssueFilter(name string, params map[string]string, result map[string]Value, method, prefix string, today time.Time) {
	switch name {
	case "state":
		uuidListFilter(params, result, method, prefix, "state", "state__in", false)
	case "state_group":
		// The only list filter that keeps whatever it was given rather than dropping non-uuids.
		plainListFilter(params, result, method, prefix, "state_group", "state__group__in")
	case "estimate_point":
		plainListFilter(params, result, method, prefix, "estimate_point", "estimate_point__in")
	case "priority":
		plainListFilter(params, result, method, prefix, "priority", "priority__in")
	case "parent":
		uuidListFilter(params, result, method, prefix, "parent", "parent__in", true)
	case "labels":
		uuidListFilter(params, result, method, prefix, "labels", "labels__in", true)
		// Written unconditionally, so asking about labels at all excludes the soft-deleted links.
		result[prefix+"label_issue__deleted_at__isnull"] = BoolValue(true)
	case "assignees":
		uuidListFilter(params, result, method, prefix, "assignees", "assignees__in", true)
		result[prefix+"issue_assignee__deleted_at__isnull"] = BoolValue(true)
	case "mentions":
		uuidListFilter(params, result, method, prefix, "mentions", "issue_mention__mention__id__in", false)
	case "created_by":
		uuidListFilter(params, result, method, prefix, "created_by", "created_by__in", true)
	case "logged_by":
		uuidListFilter(params, result, method, prefix, "logged_by", "logged_by__in", true)
	case "name":
		// The one filter that ignores the method entirely.
		if value := params["name"]; value != "" {
			result[prefix+"name__icontains"] = TextValue(value)
		}
	case "created_at":
		dateFieldFilter(params, result, method, prefix, "created_at", prefix+"created_at__date", today)
	case "updated_at":
		dateFieldFilter(params, result, method, prefix, "updated_at", prefix+"updated_at__date", today)
	case "completed_at":
		dateFieldFilter(params, result, method, prefix, "completed_at", prefix+"completed_at__date", today)
	case "start_date":
		// start_date and target_date part company with the rest on POST: rather than parsing, they store the raw value under the bare column.
		bareDateFieldFilter(params, result, method, prefix, "start_date", today)
	case "target_date":
		bareDateFieldFilter(params, result, method, prefix, "target_date", today)
	case "type":
		group := []string{"backlog", "unstarted", "started", "completed", "cancelled"}
		switch params["type"] {
		case "backlog":
			group = []string{"backlog"}
		case "active":
			group = []string{"unstarted", "started"}
		}
		result[prefix+"state__group__in"] = ListValue(group)
	case "project":
		uuidListFilter(params, result, method, prefix, "project", "project__in", false)
	case "cycle":
		uuidListFilter(params, result, method, prefix, "cycle", "issue_cycle__cycle_id__in", true)
		result[prefix+"issue_cycle__deleted_at__isnull"] = BoolValue(true)
	case "module":
		uuidListFilter(params, result, method, prefix, "module", "issue_module__module_id__in", true)
		result[prefix+"issue_module__deleted_at__isnull"] = BoolValue(true)
	case "intake_status":
		if method == "GET" {
			plainListFilter(params, result, method, prefix, "intake_status", "issue_intake__status__in")
			return
		}
		// Upstream slip, reproduced: the POST branch guards on intake_status but stores the value of inbox_status, which is the Python None when that parameter is absent.
		if value := params["intake_status"]; value != "" && value != "null" {
			other, present := params["inbox_status"]
			if !present {
				other = "None"
			}
			result[prefix+"issue_intake__status__in"] = TextValue(other)
		}
	case "inbox_status":
		plainListFilter(params, result, method, prefix, "inbox_status", "issue_intake__status__in")
	case "sub_issue":
		// Both branches are the same, and an absent value defaults to false, which is what hides sub-issues.
		if value, present := params["sub_issue"]; !present || value == "false" {
			result[prefix+"parent__isnull"] = BoolValue(true)
		}
	case "subscriber":
		uuidListFilter(params, result, method, prefix, "subscriber", "issue_subscribers__subscriber_id__in", false)
		result[prefix+"issue_subscribers__deleted_at__isnull"] = BoolValue(true)
	case "start_target_date":
		if params["start_target_date"] == "true" {
			result[prefix+"target_date__isnull"] = BoolValue(false)
			result[prefix+"start_date__isnull"] = BoolValue(false)
		}
	}
}

// uuidListFilter is the shape most list filters take: on GET the value is split on commas, the literal null dropped, a literal None turned into an isnull lookup when the filter supports one, and everything that is not a uuid discarded. On POST the value goes in whole, with no parsing at all.
func uuidListFilter(params map[string]string, result map[string]Value, method, prefix, name, lookup string, withIsNull bool) {
	if method != "GET" {
		if value := params[name]; value != "" && value != "null" {
			result[prefix+lookup] = TextValue(value)
		}
		return
	}
	items := splitDroppingNull(params[name])
	if withIsNull && ContainsString(items, "None") {
		// The isnull lookup is named after the column, which is the lookup without its __in.
		result[prefix+strings.TrimSuffix(lookup, "__in")+"__isnull"] = BoolValue(true)
	}
	valid := validUUIDs(items)
	if len(valid) > 0 && !ContainsString(valid, "") {
		result[prefix+lookup] = ListValue(valid)
	}
}

// plainListFilter is the same on POST but keeps whatever it was given on GET, since these values are not uuids.
func plainListFilter(params map[string]string, result map[string]Value, method, prefix, name, lookup string) {
	if method != "GET" {
		if value := params[name]; value != "" && value != "null" {
			result[prefix+lookup] = TextValue(value)
		}
		return
	}
	items := splitDroppingNull(params[name])
	if len(items) > 0 && !ContainsString(items, "") {
		result[prefix+lookup] = ListValue(items)
	}
}

// dateFieldFilter runs the date grammar over the value.
//
// The POST branch is broken upstream and reproduced as such: it hands date_filter the raw string rather than a list, and the loop then iterates it one character at a time. A value like "2026-01-02;before" therefore writes a __lte of the empty string, from the lone semicolon, and a __contains of the last character.
func dateFieldFilter(params map[string]string, result map[string]Value, method, prefix, name, term string, today time.Time) {
	if method == "GET" {
		queries := strings.Split(params[name], ",")
		if len(queries) > 0 && !ContainsString(queries, "") {
			ApplyDateQueries(result, term, queries, today)
		}
		return
	}
	value := params[name]
	if value == "" {
		return
	}
	ApplyDateQueries(result, term, SplitToCharacters(value), today)
}

// bareDateFieldFilter is start_date and target_date, which on POST skip the grammar entirely and store the raw value under the bare column.
func bareDateFieldFilter(params map[string]string, result map[string]Value, method, prefix, name string, today time.Time) {
	if method == "GET" {
		queries := strings.Split(params[name], ",")
		if len(queries) > 0 && !ContainsString(queries, "") {
			ApplyDateQueries(result, prefix+name, queries, today)
		}
		return
	}
	if value := params[name]; value != "" {
		result[prefix+name] = TextValue(value)
	}
}

// RelativeTermPattern is the "2_weeks" / "3_months" form, anchored at the front the way re.match is.
// RelativeTermPattern matches a relative date term like 2_weeks.
var RelativeTermPattern = regexp.MustCompile(`^\d+_(weeks|months)$`)

// applyDateQueries is date_filter. Each query is a semicolon-separated clause: either a relative term with a direction and an offset, or a literal date with a direction, or a bare value that becomes a contains lookup.
// ApplyDateQueries runs the date grammar over a term, which the saved-view query needs on its own.
func ApplyDateQueries(result map[string]Value, term string, queries []string, today time.Time) {
	for _, query := range queries {
		parts := strings.Split(query, ";")
		if len(parts) < 2 {
			result[term+"__contains"] = TextValue(parts[0])
			continue
		}
		if RelativeTermPattern.MatchString(parts[0]) {
			// A relative term needs all three parts; with only two it is silently dropped.
			if len(parts) == 3 {
				applyRelativeDate(result, term, parts[0], parts[1], parts[2], today)
			}
			continue
		}
		// "after" has to be one of the clause's own parts, not merely a substring of the value.
		if ContainsString(parts, "after") {
			result[term+"__gte"] = TextValue(parts[0])
		} else {
			result[term+"__lte"] = TextValue(parts[0])
		}
	}
}

// applyRelativeDate is string_date_filter. A month is thirty days flat, and an offset of anything other than "fromnow" counts backwards.
func applyRelativeDate(result map[string]Value, term, head, subsequent, offset string, today time.Time) {
	digits, unit, found := strings.Cut(head, "_")
	if !found {
		return
	}
	duration, err := strconv.Atoi(digits)
	if err != nil {
		return
	}
	days := duration * 7
	if unit == "months" {
		days = duration * 30
	} else if unit != "weeks" {
		return
	}
	if offset != "fromnow" {
		days = -days
	}
	moment := today.AddDate(0, 0, days).Format("2006-01-02")
	if subsequent == "after" {
		result[term+"__gte"] = TextValue(moment)
	} else {
		result[term+"__lte"] = TextValue(moment)
	}
}

func splitDroppingNull(value string) []string {
	items := []string{}
	for _, item := range strings.Split(value, ",") {
		if item != "null" {
			items = append(items, item)
		}
	}
	return items
}

// splitToCharacters is what iterating a Python string yields, which is what the broken POST date branch does.
// SplitToCharacters is how a string of single-character values is read, which the saved-view query needs too.
func SplitToCharacters(value string) []string {
	characters := make([]string, 0, len(value))
	for _, character := range value {
		characters = append(characters, string(character))
	}
	return characters
}

// filterUUIDPattern is what uuid.UUID() accepts of the forms this code passes it: the plain hyphenated form. It is deliberately looser than the project package's uuidPattern, which additionally pins the version and variant nibbles; Python's constructor does not.
var filterUUIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func validUUIDs(items []string) []string {
	valid := []string{}
	for _, item := range items {
		if filterUUIDPattern.MatchString(item) {
			// Python stores the parsed UUID, whose str() is the lower-case hyphenated form.
			valid = append(valid, strings.ToLower(item))
		}
	}
	return valid
}

// ContainsString reports whether a list holds a value.
func ContainsString(items []string, needle string) bool {
	for _, item := range items {
		if item == needle {
			return true
		}
	}
	return false
}
