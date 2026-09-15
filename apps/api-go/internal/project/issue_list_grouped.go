package project

import (
	"strings"
)

// issueGroupByAllowlist is ISSUE_GROUP_BY_ALLOWLIST. The name reaches F(), values(), order_by() and a window's partition_by, so anything outside it is refused before it gets near a query — that allowlist exists because it was not always there (GHSA-wwgj-929g-42cm).
var issueGroupByAllowlist = map[string]bool{
	"state_id": true, "state__group": true, "priority": true, "labels__id": true,
	"assignees__id": true, "issue_module__module_id": true, "cycle_id": true,
	"project_id": true, "created_by": true, "target_date": true, "start_date": true,
}

// issueGroupPartition maps a group-by name onto the expression the window partitions by. The three that reach through a join name the joined column, which is why grouping by one of them fans an issue into a row per value.
var issueGroupPartition = map[string]string{
	"state_id":     "i.state_id",
	"state__group": "(SELECT s.group FROM states s WHERE s.id = i.state_id)",
	"priority":     "i.priority",
	"cycle_id":     "(SELECT ci.cycle_id FROM cycle_issues ci WHERE ci.issue_id = i.id AND ci.deleted_at IS NULL LIMIT 1)",
	"project_id":   "i.project_id",
	"created_by":   "i.created_by_id",
	"target_date":  "i.target_date",
	"start_date":   "i.start_date",

	"labels__id":              "glab.label_id",
	"assignees__id":           "gass.assignee_id",
	"issue_module__module_id": "gmod.module_id",
}

// issueGroupJoin is the join a many-to-many group-by needs. It is a LEFT OUTER on purpose: an issue with no labels has to partition under the null group rather than vanish.
var issueGroupJoin = map[string]string{
	"labels__id":              "LEFT JOIN issue_labels glab ON glab.issue_id = i.id",
	"assignees__id":           "LEFT JOIN issue_assignees gass ON gass.issue_id = i.id",
	"issue_module__module_id": "LEFT JOIN module_issues gmod ON gmod.issue_id = i.id",
}

// issueWindowOrderClause is the ordering inside the window, which is not the ordering the flat list uses.
//
// Two differences. The paginator spells NULLS LAST explicitly, where the plain queryset leaves it to Postgres. And it orders by the string order_issue_queryset hands back rather than the one the caller sent — which for priority is the opposite direction, since that function returns the inverted name.
func issueWindowOrderClause(orderBy string) string {
	sanitized := sanitizeIssueOrderBy(orderBy)
	descending := strings.HasPrefix(sanitized, "-")
	bare := strings.TrimPrefix(sanitized, "-")

	switch bare {
	case "priority":
		// order_issue_queryset returns "priority_order" for a descending request and "-priority_order" for an ascending one, so the direction flips here.
		return caseOrder("i.priority", priorityOrder, "NULL") + nullsLastSuffix(!descending)
	case "state__group":
		values := stateOrder
		if descending {
			values = reversed(stateOrder)
		}
		return caseOrder("(SELECT s.group FROM states s WHERE s.id = i.state_id)", values, "5") + nullsLastSuffix(descending)
	}
	if expression, related := issueOrderRelatedMin[bare]; related {
		return expression + nullsLastSuffix(descending)
	}
	return issueOrderColumns[bare] + nullsLastSuffix(descending)
}

// nullsLastSuffix renders the direction the window orders in, followed by the created_at tiebreak the paginator always appends.
func nullsLastSuffix(descending bool) string {
	if descending {
		return " DESC NULLS LAST, i.created_at DESC"
	}
	return " ASC NULLS LAST, i.created_at DESC"
}

// issueGroupValuesQuery is issue_group_values: the list of groups the response pre-seeds. Four of them are fixed lists rather than queries, and three append the literal None so an issue in no group still has a bucket.
type issueGroupValuesQuery struct {
	// Fixed is the answer when the groups are a constant list.
	Fixed []string
	// Table and Column name the rows to read when they are not.
	Table  string
	Column string
	// ProjectScoped says whether the rows narrow to the project as well as the workspace.
	ProjectScoped bool
	// WithNone says whether the literal None is appended.
	WithNone bool
	// Extra is an additional predicate on the table.
	Extra string
	// FromFiltered says the values come from the filtered issues themselves rather than from a reference table.
	FromFiltered bool
}

// issueGroupValues describes where each group-by's known values come from.
var issueGroupValues = map[string]issueGroupValuesQuery{
	"priority":     {Fixed: []string{"low", "medium", "high", "urgent", "none"}},
	"state__group": {Fixed: []string{"backlog", "unstarted", "started", "completed", "cancelled"}},

	// A triage state is never a group, since a triage issue is not in the list to begin with.
	"state_id":                {Table: "states", Column: "id", ProjectScoped: true, Extra: "is_triage = FALSE"},
	"labels__id":              {Table: "labels", Column: "id", ProjectScoped: true, WithNone: true},
	"issue_module__module_id": {Table: "modules", Column: "id", ProjectScoped: true, WithNone: true},
	"cycle_id":                {Table: "cycles", Column: "id", ProjectScoped: true, WithNone: true},
	// The project list is workspace-wide even when a project is named, which is the one place the scoping is not applied.
	"project_id": {Table: "projects", Column: "id"},
	// Assignees come from the membership rather than from a reference table, and carry no None.
	"assignees__id": {Table: "project_members", Column: "member_id", ProjectScoped: true, Extra: "is_active = TRUE"},

	// These three read the distinct values out of the filtered issues themselves.
	"target_date": {FromFiltered: true, Column: "target_date"},
	"start_date":  {FromFiltered: true, Column: "start_date"},
	"created_by":  {FromFiltered: true, Column: "created_by_id"},
}

// issueGroupCountFilter is the count_filter the per-group totals carry: an issue that is still waiting in intake is not counted, and neither is an archived or draft one.
const issueGroupCountFilter = `(NOT EXISTS (SELECT 1 FROM intake_issues ii WHERE ii.issue_id = i.id AND ii.deleted_at IS NULL)
	OR EXISTS (SELECT 1 FROM intake_issues ii WHERE ii.issue_id = i.id AND ii.deleted_at IS NULL AND ii.status IN (1, -1, 2)))
	AND i.archived_at IS NULL AND i.is_draft = FALSE`
