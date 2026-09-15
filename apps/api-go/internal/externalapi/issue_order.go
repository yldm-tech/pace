package externalapi

import (
	"strconv"
	"strings"
)

// issueOrderAllowlist is ISSUE_ORDER_BY_ALLOWLIST, the same fourteen fields the session API allows.
var issueOrderAllowlist = map[string]bool{
	"created_at": true, "updated_at": true, "sequence_id": true, "sort_order": true,
	"target_date": true, "start_date": true, "completed_at": true, "archived_at": true,
	"priority": true, "state__name": true, "state__group": true,
	"assignees__first_name": true, "labels__name": true, "issue_module__module__name": true,
}

// issueOrderColumns maps the plain fields onto their columns.
var issueOrderColumns = map[string]string{
	"created_at": "i.created_at", "updated_at": "i.updated_at", "sequence_id": "i.sequence_id",
	"sort_order": "i.sort_order", "target_date": "i.target_date", "start_date": "i.start_date",
	"completed_at": "i.completed_at", "archived_at": "i.archived_at",
}

// issuePriorityOrder and issueStateOrder are the two lists the view builds its Case expressions from.
var issuePriorityOrder = []string{"urgent", "high", "medium", "low", "none"}
var issueStateOrder = []string{"backlog", "unstarted", "started", "completed", "cancelled"}

// externalIssueOrderClause is the ordering branch of the work item list, which is written inline in the view rather than shared with order_issue_queryset — and does not agree with it.
//
// Three differences fall out of the SQL the two render. This one carries **no secondary key**, so two work items created in the same millisecond come back in whatever order the database likes. It **reverses the priority list** for a descending sort rather than ordering the same way twice, so unlike the session API's list `-priority` really is the reverse of `priority`. And its state Case has a **default**, so a work item whose state falls outside the five groups sorts last rather than as a null.
//
// A relation is reached through a correlated aggregate rather than a join, which is what keeps one row per work item without a GROUP BY. Django's join carries no soft-delete predicate, so a soft-deleted label link still contributes its name to the maximum; the subquery leaves that alone rather than correcting it.
func externalIssueOrderClause(orderBy string) (string, error) {
	orderBy = sanitizeOrderBy(orderBy, issueOrderAllowlist, "-created_at")
	descending := strings.HasPrefix(orderBy, "-")
	bare := strings.TrimPrefix(orderBy, "-")
	direction := " ASC"
	if descending {
		direction = " DESC"
	}

	switch bare {
	case "priority":
		return issueCaseOrder("i.priority", orderedList(issuePriorityOrder, descending), "NULL") + " ASC", nil
	case "state__name", "state__group":
		return issueCaseOrder("s.group", orderedList(issueStateOrder, descending), "5") + " ASC", nil
	case "assignees__first_name":
		return `(SELECT MAX(u.first_name) FROM issue_assignees ia JOIN users u ON u.id = ia.assignee_id WHERE ia.issue_id = i.id)` + direction, nil
	case "labels__name":
		return `(SELECT MAX(l.name) FROM issue_labels il JOIN labels l ON l.id = il.label_id WHERE il.issue_id = i.id)` + direction, nil
	case "issue_module__module__name":
		// Django reaches the module name through a join and leaves the column out of the select list, which a SELECT DISTINCT refuses. The request fails in the database rather than returning work items in some other order, so it fails here too.
		return "", errOrderByNeedsTheSelectList
	}
	return issueOrderColumns[bare] + direction, nil
}

// errOrderByNeedsTheSelectList names the database's refusal of the one allowlisted ordering that cannot be rendered.
var errOrderByNeedsTheSelectList = &orderError{"ordering by the module name needs the column in the select list of a distinct query"}

// orderedList reverses the list for a descending sort, which is how both Case expressions change direction. The ordering itself is always ascending.
func orderedList(values []string, descending bool) []string {
	ordered := make([]string, len(values))
	copy(ordered, values)
	if descending {
		for left, right := 0, len(ordered)-1; left < right; left, right = left+1, right-1 {
			ordered[left], ordered[right] = ordered[right], ordered[left]
		}
	}
	return ordered
}

// issueCaseOrder builds the Case the view annotates, numbering each value by its place in the list.
func issueCaseOrder(column string, values []string, fallback string) string {
	clause := "CASE"
	for index, value := range values {
		clause += " WHEN " + column + " = '" + value + "' THEN " + strconv.Itoa(index)
	}
	return clause + " ELSE " + fallback + " END"
}
