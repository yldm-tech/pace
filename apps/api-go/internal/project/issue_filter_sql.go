package project

import (
	"sort"
	"strings"
)

// unsupportedFilterFamily marks a lookup that issue_filters can produce but the ORM cannot resolve.
const unsupportedFilterFamily = "unsupported"

// issueFilterJoin describes one multi-valued relation the filters can reach through. Django gives each family its own alias and puts every condition that names it on that one join, so a value lookup and its soft-delete companion apply to the same row.
type issueFilterJoin struct {
	alias string
	table string
}

// issueFilterJoins maps each relation family to its join. The aliases are prefixed so they cannot collide with the ones the annotations already use.
var issueFilterJoins = map[string]issueFilterJoin{
	"labels":      {alias: "flab", table: "issue_labels"},
	"assignees":   {alias: "fass", table: "issue_assignees"},
	"modules":     {alias: "fmod", table: "module_issues"},
	"cycles":      {alias: "fcyc", table: "cycle_issues"},
	"subscribers": {alias: "fsub", table: "issue_subscribers"},
	"mentions":    {alias: "fmen", table: "issue_mentions"},
	"intake":      {alias: "fint", table: "intake_issues"},
}

// issueFilterLookups maps every lookup issue_filters can produce onto the family it belongs to and the SQL it becomes. A family of "" means the lookup is a plain column on the issue itself.
type issueFilterLookup struct {
	family string
	// expression is the left-hand side, with the alias already in place.
	expression string
	// operator says how the value is compared.
	operator string
}

// issueFilterSQLMap is the whole translation table. The expressions were taken from the SQL Django renders for each lookup rather than written from the field names, which is what settles the two things that are easy to get wrong: a many-to-many value lookup reaches only the through table, never the target, and a date lookup on a DateTimeField goes through a timezone cast that one on a DateField does not.
var issueFilterSQLMap = map[string]issueFilterLookup{
	// Plain columns on the issue.
	"state__in":          {expression: "i.state_id", operator: "IN"},
	"estimate_point__in": {expression: "i.estimate_point_id", operator: "IN"},
	"priority__in":       {expression: "i.priority", operator: "IN"},
	"parent__in":         {expression: "i.parent_id", operator: "IN"},
	"parent__isnull":     {expression: "i.parent_id", operator: "ISNULL"},
	"created_by__in":     {expression: "i.created_by_id", operator: "IN"},
	"created_by__isnull": {expression: "i.created_by_id", operator: "ISNULL"},
	"project__in":        {expression: "i.project_id", operator: "IN"},
	"name__icontains":    {expression: "i.name", operator: "ICONTAINS"},

	// state__group reaches the state table, which the annotations already join.
	"state__group__in": {expression: "(SELECT s.group FROM states s WHERE s.id = i.state_id)", operator: "IN"},

	// The two DateFields compare directly; the three DateTimeFields go through a cast to the project's timezone first, which for this deployment is UTC.
	"start_date":            {expression: "i.start_date", operator: "="},
	"start_date__gte":       {expression: "i.start_date", operator: ">="},
	"start_date__lte":       {expression: "i.start_date", operator: "<="},
	"start_date__contains":  {expression: "i.start_date::text", operator: "CONTAINS"},
	"start_date__isnull":    {expression: "i.start_date", operator: "ISNULL"},
	"target_date":           {expression: "i.target_date", operator: "="},
	"target_date__gte":      {expression: "i.target_date", operator: ">="},
	"target_date__lte":      {expression: "i.target_date", operator: "<="},
	"target_date__contains": {expression: "i.target_date::text", operator: "CONTAINS"},
	"target_date__isnull":   {expression: "i.target_date", operator: "ISNULL"},

	"created_at__date__gte":        {expression: "(i.created_at AT TIME ZONE 'UTC')::date", operator: ">="},
	"created_at__date__lte":        {expression: "(i.created_at AT TIME ZONE 'UTC')::date", operator: "<="},
	"created_at__date__contains":   {expression: "(i.created_at AT TIME ZONE 'UTC')::date::text", operator: "CONTAINS"},
	"updated_at__date__gte":        {expression: "(i.updated_at AT TIME ZONE 'UTC')::date", operator: ">="},
	"updated_at__date__lte":        {expression: "(i.updated_at AT TIME ZONE 'UTC')::date", operator: "<="},
	"updated_at__date__contains":   {expression: "(i.updated_at AT TIME ZONE 'UTC')::date::text", operator: "CONTAINS"},
	"completed_at__date__gte":      {expression: "(i.completed_at AT TIME ZONE 'UTC')::date", operator: ">="},
	"completed_at__date__lte":      {expression: "(i.completed_at AT TIME ZONE 'UTC')::date", operator: "<="},
	"completed_at__date__contains": {expression: "(i.completed_at AT TIME ZONE 'UTC')::date::text", operator: "CONTAINS"},

	// The multi-valued relations. Each family carries its own join, and the soft-delete companion sits on the same one.
	"labels__in":                      {family: "labels", expression: "flab.label_id", operator: "IN"},
	"labels__isnull":                  {family: "labels", expression: "flab.label_id", operator: "ISNULL"},
	"label_issue__deleted_at__isnull": {family: "labels", expression: "flab.deleted_at", operator: "ISNULL"},

	"assignees__in":                      {family: "assignees", expression: "fass.assignee_id", operator: "IN"},
	"assignees__isnull":                  {family: "assignees", expression: "fass.assignee_id", operator: "ISNULL"},
	"issue_assignee__deleted_at__isnull": {family: "assignees", expression: "fass.deleted_at", operator: "ISNULL"},

	"issue_module__module_id__in":      {family: "modules", expression: "fmod.module_id", operator: "IN"},
	"issue_module__module_id__isnull":  {family: "modules", expression: "fmod.module_id", operator: "ISNULL"},
	"issue_module__deleted_at__isnull": {family: "modules", expression: "fmod.deleted_at", operator: "ISNULL"},

	"issue_cycle__cycle_id__in":       {family: "cycles", expression: "fcyc.cycle_id", operator: "IN"},
	"issue_cycle__cycle_id__isnull":   {family: "cycles", expression: "fcyc.cycle_id", operator: "ISNULL"},
	"issue_cycle__deleted_at__isnull": {family: "cycles", expression: "fcyc.deleted_at", operator: "ISNULL"},

	"issue_subscribers__subscriber_id__in":  {family: "subscribers", expression: "fsub.subscriber_id", operator: "IN"},
	"issue_subscribers__deleted_at__isnull": {family: "subscribers", expression: "fsub.deleted_at", operator: "ISNULL"},

	"issue_mention__mention__id__in": {family: "mentions", expression: "fmen.mention_id", operator: "IN"},

	"issue_intake__status__in": {family: "intake", expression: "fint.status", operator: "IN"},

	// logged_by exists nowhere but in issue_filters itself — not on the model, not in any view — so Django cannot resolve it and raises FieldError, which answers 500. A caller passing ?logged_by= gets that today, and gets it here: the translation refuses rather than inventing a column.
	"logged_by__in":     {family: unsupportedFilterFamily},
	"logged_by__isnull": {family: unsupportedFilterFamily},
}

// issueFilterSQL turns the lookup dictionary into the joins and conditions a query needs.
//
// A family whose lookups include an isnull is joined with a LEFT OUTER rather than an INNER, because an inner join can never produce the null row that lookup is looking for. That is what Django does, and it is why asking for "issues with no label" works at all.
//
// A caller who asks for both — the literal None and a specific label — gets a condition that matches nothing, since both sit on the same join and a column cannot be null and in a list at once. Reproduced rather than corrected: it is what the endpoint does today.
func issueFilterSQL(filters map[string]filterValue) (joins []string, conditions []string, arguments []any, ok bool) {
	// Sorted so the SQL is stable, which matters for both testing and query plan caching.
	lookups := make([]string, 0, len(filters))
	for lookup := range filters {
		lookups = append(lookups, lookup)
	}
	sort.Strings(lookups)

	needsOuter := map[string]bool{}
	usedFamilies := map[string]bool{}
	for _, lookup := range lookups {
		mapped, known := issueFilterSQLMap[lookup]
		if !known || mapped.family == unsupportedFilterFamily {
			return nil, nil, nil, false
		}
		if mapped.family == "" {
			continue
		}
		usedFamilies[mapped.family] = true
		if mapped.operator == "ISNULL" && !strings.HasSuffix(mapped.expression, ".deleted_at") {
			// Only a null value lookup forces the outer join; the soft-delete companion is a condition on a row that has to exist anyway.
			needsOuter[mapped.family] = true
		}
	}

	families := make([]string, 0, len(usedFamilies))
	for family := range usedFamilies {
		families = append(families, family)
	}
	sort.Strings(families)
	for _, family := range families {
		join := issueFilterJoins[family]
		kind := "JOIN"
		if needsOuter[family] {
			kind = "LEFT JOIN"
		}
		joins = append(joins, kind+" "+join.table+" "+join.alias+" ON "+join.alias+".issue_id = i.id")
	}

	for _, lookup := range lookups {
		mapped := issueFilterSQLMap[lookup]
		condition, values := filterCondition(mapped, filters[lookup])
		if condition == "" {
			continue
		}
		conditions = append(conditions, condition)
		arguments = append(arguments, values...)
	}
	return joins, conditions, arguments, true
}

// filterCondition renders one lookup. A list value that arrived from a POST is a single string rather than a list, which Django passes to the ORM whole; here it becomes a one-element list, since that is what the __in lookup does with it.
func filterCondition(mapped issueFilterLookup, value filterValue) (string, []any) {
	switch mapped.operator {
	case "ISNULL":
		if value.flag {
			return mapped.expression + " IS NULL", nil
		}
		return mapped.expression + " IS NOT NULL", nil
	case "IN":
		values := filterListValues(value)
		if len(values) == 0 {
			// An empty IN matches nothing, which is what Django's empty list produces.
			return "FALSE", nil
		}
		placeholders := make([]string, len(values))
		arguments := make([]any, len(values))
		for index, item := range values {
			placeholders[index] = "?"
			arguments[index] = item
		}
		return mapped.expression + " IN (" + strings.Join(placeholders, ", ") + ")", arguments
	case "ICONTAINS":
		return "UPPER(" + mapped.expression + "::text) LIKE UPPER(?)", []any{"%" + escapeLikePattern(value.text) + "%"}
	case "CONTAINS":
		return mapped.expression + " LIKE ?", []any{"%" + escapeLikePattern(value.text) + "%"}
	case "=", ">=", "<=":
		return mapped.expression + " " + mapped.operator + " ?", []any{value.text}
	}
	return "", nil
}

func filterListValues(value filterValue) []string {
	if value.kind == 'l' {
		return value.list
	}
	if value.text == "" {
		return nil
	}
	return []string{value.text}
}

// escapeLikePattern is what Django does to a contains pattern before wrapping it: the wildcards a caller might have typed are neutralised so they match themselves.
func escapeLikePattern(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return replacer.Replace(value)
}
