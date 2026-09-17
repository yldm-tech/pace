package externalapi

import "strings"

// plainIssueOrder is a work item ordering written as a bare order_by rather than through the Case machinery the project's own list uses.
//
// The same parameter means something different here. `priority` sorts the **words** — high, low, medium, none, urgent — rather than the severities, because nothing maps them onto an order. And the three fields that reach through a multi-valued relation add joins that **repeat** a work item once per related row, since the queryset carries no distinct: a work item with three labels is three rows of a list ordered by label name. Both are upstream's.
type plainIssueOrder struct {
	Clause string
	Joins  []string
}

// plainIssueOrderColumns maps the fields that are columns of the work item itself.
var plainIssueOrderColumns = map[string]string{
	"created_at": "i.created_at", "updated_at": "i.updated_at", "sequence_id": "i.sequence_id",
	"sort_order": "i.sort_order", "target_date": "i.target_date", "start_date": "i.start_date",
	"completed_at": "i.completed_at", "archived_at": "i.archived_at", "priority": "i.priority",
	"state__name": "s.name", "state__group": "s.group",
}

// plainIssueOrderJoins holds the three fields Django reaches through a join. Neither join filters deleted_at, because a model's default manager never reaches into a join traversed through it — a soft-deleted label link still places its work item.
var plainIssueOrderJoins = map[string]plainIssueOrder{
	"assignees__first_name": {
		Clause: "au.first_name",
		Joins: []string{
			"LEFT JOIN issue_assignees oia ON oia.issue_id = i.id",
			"LEFT JOIN users au ON au.id = oia.assignee_id",
		},
	},
	"labels__name": {
		Clause: "ol.name",
		Joins: []string{
			"LEFT JOIN issue_labels oil ON oil.issue_id = i.id",
			"LEFT JOIN labels ol ON ol.id = oil.label_id",
		},
	},
	"issue_module__module__name": {
		Clause: "om.name",
		Joins: []string{
			"LEFT JOIN module_issues omi ON omi.issue_id = i.id",
			"LEFT JOIN modules om ON om.id = omi.module_id",
		},
	},
}

// plainIssueOrderBy turns an order_by parameter into the clause and the joins it needs. Anything the allowlist does not name falls back, and the fallback here is ascending by creation rather than the descending one the project list uses.
func plainIssueOrderBy(value string) plainIssueOrder {
	value = sanitizeOrderBy(value, issueOrderAllowlist, "created_at")
	descending := strings.HasPrefix(value, "-")
	bare := strings.TrimPrefix(value, "-")
	direction := " ASC"
	if descending {
		direction = " DESC"
	}
	if joined, present := plainIssueOrderJoins[bare]; present {
		return plainIssueOrder{Clause: joined.Clause + direction, Joins: joined.Joins}
	}
	return plainIssueOrder{Clause: plainIssueOrderColumns[bare] + direction}
}
