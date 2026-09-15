package complexfilters

import (
	"errors"
	"regexp"
	"strings"
	"time"
)

// cleanerKind is the form field a declared filter cleans its value through.
type cleanerKind int

const (
	cleanUUID cleanerKind = iota
	cleanUUIDList
	cleanChar
	cleanCharList
	cleanChoice
	cleanBool
	cleanDate
	// cleanDateList is the comma-separated range that takes however many dates it is given.
	cleanDateList
	// cleanDatePair is the range that insists on exactly two.
	cleanDatePair
)

// filterSpec is one of the forty-nine filters IssueFilterSet declares.
//
// A filter either writes a lookup of its own or runs a method. The methods all do the same thing — pair the value with the soft-delete check on the relation it reaches through — which is why they are a list of lookups here rather than Go functions.
type filterSpec struct {
	cleaner cleanerKind
	// lookup is what a plain filter writes, already carrying the field name and the lookup expression.
	lookup string
	// relation, when set, marks a method filter: the value goes on the first lookup and a soft-delete check on the second.
	relation string
	// archived marks the one method filter that is not a relation at all.
	archived bool
}

// issueFilters is IssueFilterSet.base_filters. The `__exact` twin of every filter whose lookup expression is `exact` is added by BaseFilterSet.get_filters, which is why the map carries both names for most of them.
var issueFilters = map[string]filterSpec{
	"assignee_id":          {cleaner: cleanUUID, relation: "issue_assignee__assignee_id"},
	"assignee_id__exact":   {cleaner: cleanUUID, relation: "issue_assignee__assignee_id"},
	"assignee_id__in":      {cleaner: cleanUUIDList, relation: "issue_assignee__assignee_id__in"},
	"cycle_id":             {cleaner: cleanUUID, relation: "issue_cycle__cycle_id"},
	"cycle_id__exact":      {cleaner: cleanUUID, relation: "issue_cycle__cycle_id"},
	"cycle_id__in":         {cleaner: cleanUUIDList, relation: "issue_cycle__cycle_id__in"},
	"module_id":            {cleaner: cleanUUID, relation: "issue_module__module_id"},
	"module_id__exact":     {cleaner: cleanUUID, relation: "issue_module__module_id"},
	"module_id__in":        {cleaner: cleanUUIDList, relation: "issue_module__module_id__in"},
	"mention_id":           {cleaner: cleanUUID, relation: "issue_mention__mention_id"},
	"mention_id__exact":    {cleaner: cleanUUID, relation: "issue_mention__mention_id"},
	"mention_id__in":       {cleaner: cleanUUIDList, relation: "issue_mention__mention_id__in"},
	"label_id":             {cleaner: cleanUUID, relation: "label_issue__label_id"},
	"label_id__exact":      {cleaner: cleanUUID, relation: "label_issue__label_id"},
	"label_id__in":         {cleaner: cleanUUIDList, relation: "label_issue__label_id__in"},
	"subscriber_id":        {cleaner: cleanUUID, relation: "issue_subscribers__subscriber_id"},
	"subscriber_id__exact": {cleaner: cleanUUID, relation: "issue_subscribers__subscriber_id"},
	"subscriber_id__in":    {cleaner: cleanUUIDList, relation: "issue_subscribers__subscriber_id__in"},

	"is_archived":        {cleaner: cleanBool, archived: true},
	"is_archived__exact": {cleaner: cleanBool, archived: true},

	"created_by_id":        {cleaner: cleanUUID, lookup: "created_by_id__exact"},
	"created_by_id__exact": {cleaner: cleanUUID, lookup: "created_by_id__exact"},
	"created_by_id__in":    {cleaner: cleanUUIDList, lookup: "created_by_id__in"},
	"state_id":             {cleaner: cleanUUID, lookup: "state_id__exact"},
	"state_id__exact":      {cleaner: cleanUUID, lookup: "state_id__exact"},
	"state_id__in":         {cleaner: cleanUUIDList, lookup: "state_id__in"},
	"project_id":           {cleaner: cleanUUID, lookup: "project_id__exact"},
	"project_id__exact":    {cleaner: cleanUUID, lookup: "project_id__exact"},
	"project_id__in":       {cleaner: cleanUUIDList, lookup: "project_id__in"},

	"state_group":        {cleaner: cleanChar, lookup: "state__group__exact"},
	"state_group__exact": {cleaner: cleanChar, lookup: "state__group__exact"},
	"state_group__in":    {cleaner: cleanCharList, lookup: "state__group__in"},

	"priority":        {cleaner: cleanChoice, lookup: "priority__exact"},
	"priority__exact": {cleaner: cleanChoice, lookup: "priority__exact"},
	"priority__in":    {cleaner: cleanCharList, lookup: "priority__in"},

	"is_draft":        {cleaner: cleanBool, lookup: "is_draft__exact"},
	"is_draft__exact": {cleaner: cleanBool, lookup: "is_draft__exact"},

	// The two DateFields compare directly. The two DateTimeFields compare their date component, because the client sends a bare calendar day and an exact match against a timestamp would only ever find midnight.
	"start_date":         {cleaner: cleanDate, lookup: "start_date__exact"},
	"start_date__exact":  {cleaner: cleanDate, lookup: "start_date__exact"},
	"start_date__range":  {cleaner: cleanDatePair, lookup: "start_date__range"},
	"target_date":        {cleaner: cleanDate, lookup: "target_date__exact"},
	"target_date__exact": {cleaner: cleanDate, lookup: "target_date__exact"},
	"target_date__range": {cleaner: cleanDatePair, lookup: "target_date__range"},
	"created_at":         {cleaner: cleanDate, lookup: "created_at__date"},
	"created_at__exact":  {cleaner: cleanDate, lookup: "created_at__date"},
	"created_at__range":  {cleaner: cleanDateList, lookup: "created_at__date__range"},
	"updated_at":         {cleaner: cleanDate, lookup: "updated_at__date"},
	"updated_at__exact":  {cleaner: cleanDate, lookup: "updated_at__date"},
	"updated_at__range":  {cleaner: cleanDateList, lookup: "updated_at__date__range"},
}

// issuePriorities are the five choices the priority field accepts, and the match is case sensitive.
var issuePriorities = map[string]bool{"urgent": true, "high": true, "medium": true, "low": true, "none": true}

// errInvalidField is what a cleaner reports when the form would refuse the value. Every one of them comes back to the caller as the same invalid_filterset refusal, which is what Django answers with.
var errInvalidField = errors.New("invalid filter value")

// clean turns the string a value became in the QueryDict into what the form makes of it.
func (spec filterSpec) clean(raw string) (Value, error) {
	switch spec.cleaner {
	case cleanUUID:
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			return NullValue(), nil
		}
		canonical, ok := canonicalUUID(trimmed)
		if !ok {
			return Value{}, errInvalidField
		}
		return StringValue(canonical), nil
	case cleanUUIDList:
		items, err := splitCSV(raw, func(part string) (Value, error) {
			trimmed := strings.TrimSpace(part)
			if trimmed == "" {
				return NullValue(), nil
			}
			canonical, ok := canonicalUUID(trimmed)
			if !ok {
				return Value{}, errInvalidField
			}
			return StringValue(canonical), nil
		})
		if err != nil {
			return Value{}, err
		}
		return ListValue(items), nil
	case cleanChar:
		return StringValue(raw), nil
	case cleanCharList:
		items, err := splitCSV(raw, func(part string) (Value, error) { return StringValue(part), nil })
		if err != nil {
			return Value{}, err
		}
		return ListValue(items), nil
	case cleanChoice:
		if raw == "" {
			return StringValue(""), nil
		}
		if !issuePriorities[raw] {
			return Value{}, errInvalidField
		}
		return StringValue(raw), nil
	case cleanBool:
		// NullBooleanSelect's table, which is why "1" and "0" are not booleans here and "2" and "3" are.
		switch raw {
		case "True", "true", "2":
			return BoolValue(true), nil
		case "False", "false", "3":
			return BoolValue(false), nil
		}
		return NullValue(), nil
	case cleanDate:
		if strings.TrimSpace(raw) == "" {
			return NullValue(), nil
		}
		day, ok := parseFormDate(raw)
		if !ok {
			return Value{}, errInvalidField
		}
		return StringValue(day), nil
	case cleanDateList, cleanDatePair:
		items, err := splitCSV(raw, func(part string) (Value, error) {
			if strings.TrimSpace(part) == "" {
				return NullValue(), nil
			}
			day, ok := parseFormDate(part)
			if !ok {
				return Value{}, errInvalidField
			}
			return StringValue(day), nil
		})
		if err != nil {
			return Value{}, err
		}
		// The pair filter is a range filter rather than a plain csv one, so it insists on two ends — but only when it was given anything at all.
		if spec.cleaner == cleanDatePair && len(items) != 0 && len(items) != 2 {
			return Value{}, errInvalidField
		}
		return ListValue(items), nil
	}
	return Value{}, errInvalidField
}

// splitCSV is the csv widget: nothing at all is an empty list, and everything else splits on commas without dropping the empty pieces.
func splitCSV(raw string, clean func(string) (Value, error)) ([]Value, error) {
	if raw == "" {
		return []Value{}, nil
	}
	parts := strings.Split(raw, ",")
	items := make([]Value, 0, len(parts))
	for _, part := range parts {
		value, err := clean(part)
		if err != nil {
			return nil, err
		}
		items = append(items, value)
	}
	return items, nil
}

// isEmpty is Django's EMPTY_VALUES, which is what decides whether a method filter runs at all.
func (value Value) isEmpty() bool {
	switch value.Kind {
	case KindNull:
		return true
	case KindString:
		return value.Text == ""
	case KindList:
		return len(value.List) == 0
	}
	return false
}

// conditions is what one provided filter contributes to the leaf's Q.
func (spec filterSpec) conditions(value Value) []Condition {
	if spec.archived {
		// The method short-circuits on an empty value, and what it returns then is the queryset rather than a Q.
		if value.isEmpty() {
			return []Condition{{Lookup: "pk__in", Value: everyRowValue()}}
		}
		return []Condition{{Lookup: "archived_at__isnull", Value: BoolValue(!value.Bool)}}
	}
	if spec.relation != "" {
		if value.isEmpty() {
			return []Condition{{Lookup: "pk__in", Value: everyRowValue()}}
		}
		// Every relation method pairs its value with the soft-delete check on the same join.
		relation := spec.relation[:strings.Index(spec.relation, "__")]
		return []Condition{
			{Lookup: spec.relation, Value: value},
			{Lookup: relation + "__deleted_at__isnull", Value: BoolValue(true)},
		}
	}
	return []Condition{{Lookup: spec.lookup, Value: value}}
}

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-?[0-9a-fA-F]{4}-?[0-9a-fA-F]{4}-?[0-9a-fA-F]{4}-?[0-9a-fA-F]{12}$`)

// canonicalUUID is forms.UUIDField, which takes a uuid with or without its dashes and always gives one back with them.
func canonicalUUID(value string) (string, bool) {
	if !uuidPattern.MatchString(value) {
		return "", false
	}
	bare := strings.ToLower(strings.ReplaceAll(value, "-", ""))
	return bare[0:8] + "-" + bare[8:12] + "-" + bare[12:16] + "-" + bare[16:20] + "-" + bare[20:32], true
}

// formDateLayouts are Django's DATE_INPUT_FORMATS. The first is what the client sends; the rest are what the form would also have taken.
var formDateLayouts = []string{
	"2006-1-2", "1/2/2006", "1/2/06",
	"Jan 2 2006", "Jan 2, 2006", "2 Jan 2006", "2 Jan, 2006",
	"January 2 2006", "January 2, 2006", "2 January 2006", "2 January, 2006",
}

// parseFormDate accepts what Django's date field accepts and answers with the one shape everything is rendered in.
//
// The layouts are the unpadded ones on purpose: strptime takes "2026-1-2" as readily as "2026-01-02", and Go's padded layout would refuse the first.
func parseFormDate(raw string) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	for _, layout := range formDateLayouts {
		if day, err := time.Parse(layout, trimmed); err == nil {
			return day.Format("2006-01-02"), true
		}
	}
	return "", false
}
