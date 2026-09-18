package project

import (
	"strconv"
	"strings"
)

// issueOrderAllowlist is ISSUE_ORDER_BY_ALLOWLIST. Anything outside it is replaced with the default rather than reaching the query, which is what sanitize_order_by does to keep an order_by parameter from being injected.
var issueOrderAllowlist = map[string]string{
	"created_at": "created_at", "updated_at": "updated_at", "sequence_id": "sequence_id", "sort_order": "sort_order",
	"target_date": "target_date", "start_date": "start_date", "completed_at": "completed_at", "archived_at": "archived_at",
	"priority": "priority", "state__name": "state__name", "state__group": "state__group",
	"assignees__first_name": "assignees__first_name", "labels__name": "labels__name", "issue_module__module__name": "issue_module__module__name",
}

// issueOrderColumns maps the allowlisted names onto the expressions the Go queries order by. Django reaches state__name through a LEFT OUTER JOIN, which a scalar subquery reproduces: both yield NULL for an issue with no state.
var issueOrderColumns = map[string]string{
	"created_at": "i.created_at", "updated_at": "i.updated_at", "sequence_id": "i.sequence_id",
	"sort_order": "i.sort_order", "target_date": "i.target_date", "start_date": "i.start_date",
	"completed_at": "i.completed_at", "archived_at": "i.archived_at",
	"state__name": "(SELECT s.name FROM states s WHERE s.id = i.state_id)",
}

// priorityOrder and stateOrder are PRIORITY_ORDER and STATE_ORDER.
var priorityOrder = []string{"urgent", "high", "medium", "low", "none"}
var stateOrder = []string{"backlog", "unstarted", "started", "completed", "cancelled"}

// issueOrderRelatedMin holds the three fields Django orders by Min() over a joined table. Django's join carries no soft-delete predicate — a model's default manager only filters that model's own queryset, never a join it is traversed through — so a soft-deleted label link still contributes its name to the minimum. That is reproduced rather than corrected; adding the deleted_at filters here would reorder issues relative to Django.
var issueOrderRelatedMin = map[string]string{
	"assignees__first_name":      `(SELECT MIN(u.first_name) FROM issue_assignees ia JOIN users u ON u.id = ia.assignee_id WHERE ia.issue_id = i.id)`,
	"labels__name":               `(SELECT MIN(l.name) FROM issue_labels il JOIN labels l ON l.id = il.label_id WHERE il.issue_id = i.id)`,
	"issue_module__module__name": `(SELECT MIN(m.name) FROM module_issues mi JOIN modules m ON m.id = mi.module_id WHERE mi.issue_id = i.id)`,
}

// sanitizeIssueOrderBy is sanitize_order_by: at most one leading dash, the bare name must be allowlisted, and anything else falls back to the default.
func sanitizeIssueOrderBy(value string) string {
	const fallback = "-created_at"
	if value == "" {
		return fallback
	}
	descending := strings.HasPrefix(value, "-")
	bare := value
	if descending {
		bare = value[1:]
	}
	column, permitted := issueOrderAllowlist[bare]
	if strings.HasPrefix(bare, "-") || !permitted {
		return fallback
	}
	// The column comes out of the allowlist rather than out of the parameter, so nothing derived from the request reaches the clause built from this.
	if descending {
		return "-" + column
	}
	return column
}

// issueOrderClause turns an order_by parameter into SQL, reproducing order_issue_queryset.
//
// No clause carries an explicit NULLS position: Django emits a bare ASC/DESC and leaves Postgres to apply its defaults (NULLS LAST ascending, NULLS FIRST descending), so spelling one out here would move null target dates on a descending sort.
func issueOrderClause(orderBy string) string {
	orderBy = sanitizeIssueOrderBy(orderBy)
	descending := strings.HasPrefix(orderBy, "-")
	bare := strings.TrimPrefix(orderBy, "-")

	switch bare {
	case "priority":
		// The Case has no default, so a priority outside the list sorts as NULL, and Django orders by it ascending in both directions — only the order_by string it hands back to the paginator flips. So `priority` and `-priority` return the same order, which is reproduced rather than corrected.
		return caseOrder("i.priority", priorityOrder, "NULL") + ", i.created_at DESC"
	case "state__group":
		// Descending reverses the list the Case is built from instead of reversing the sort, so this direction really does change the order.
		values := stateOrder
		if descending {
			values = reversed(stateOrder)
		}
		return caseOrder("(SELECT s.group FROM states s WHERE s.id = i.state_id)", values, strconv.Itoa(len(stateOrder))) + ", i.created_at DESC"
	}

	if expression, related := issueOrderRelatedMin[bare]; related {
		return expression + " " + direction(descending) + ", i.created_at DESC"
	}

	column := issueOrderColumns[bare]
	// Django appends a created_at tiebreak unless the ordering already is one.
	if bare == "created_at" {
		return column + " " + direction(descending)
	}
	return column + " " + direction(descending) + ", i.created_at DESC"
}

// caseOrder is caseOrderExpression sorted ascending, which is the direction the flat list always uses.
func caseOrder(expression string, values []string, fallback string) string {
	return caseOrderExpression(expression, values, fallback) + " ASC"
}

// caseOrderExpression builds the annotation Django orders by, with no direction of its own. Django declares the Case as a CharField, so the positions compare as text, but every position is a single digit and text and numeric order agree.
//
// The direction is separate because the grouped list supplies its own -- it orders by the string order_issue_queryset hands back rather than the one the caller sent, which for priority is the opposite direction. Folding ASC into the expression, as this did, produced `END ASC DESC NULLS LAST` there and a Postgres syntax error.
func caseOrderExpression(expression string, values []string, fallback string) string {
	var builder strings.Builder
	builder.WriteString("CASE ")
	for index, value := range values {
		builder.WriteString("WHEN ")
		builder.WriteString(expression)
		builder.WriteString(" = '")
		builder.WriteString(value)
		builder.WriteString("' THEN ")
		builder.WriteString(strconv.Itoa(index))
		builder.WriteString(" ")
	}
	builder.WriteString("ELSE ")
	builder.WriteString(fallback)
	builder.WriteString(" END")
	return builder.String()
}

func direction(descending bool) string {
	if descending {
		return "DESC"
	}
	return "ASC"
}

func reversed(values []string) []string {
	result := make([]string, len(values))
	for index, value := range values {
		result[len(values)-1-index] = value
	}
	return result
}
