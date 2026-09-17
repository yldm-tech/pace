package complexfilters

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ErrUntranslatable marks a filter tree the ORM would refuse at compile time rather than at validation — a range with a number of ends other than two. Django raises there, which answers 500.
var ErrUntranslatable = errors.New("complex filters: the tree names a lookup the ORM cannot compile")

// Join is one through table a filter reaches across.
//
// Whether it is an inner or an outer join is not decoration: an inner one drops work items with no row on the far side, and that is what makes `assignee_id` narrow the list while the same condition under an `or` does not.
type Join struct {
	Alias string
	Table string
	Outer bool
}

// Clause renders the join the way a query builder wants it.
func (join Join) Clause(issueAlias string) string {
	kind := "JOIN"
	if join.Outer {
		kind = "LEFT JOIN"
	}
	return kind + " " + join.Table + " " + join.Alias + " ON " + join.Alias + ".issue_id = " + issueAlias + ".id"
}

// Query is what a filter tree becomes.
type Query struct {
	Joins     []Join
	Condition string
	Arguments []any
}

// relation describes one of the six through tables a filter can reach across.
type relation struct {
	table string
	alias string
}

// relations maps each related name the filter set produces onto the table it stands for. The aliases are prefixed so they cannot collide with the ones a list query already uses.
var relations = map[string]relation{
	"issue_assignee":    {table: "issue_assignees", alias: "cfass"},
	"issue_cycle":       {table: "cycle_issues", alias: "cfcyc"},
	"issue_module":      {table: "module_issues", alias: "cfmod"},
	"issue_mention":     {table: "issue_mentions", alias: "cfmen"},
	"label_issue":       {table: "issue_labels", alias: "cflab"},
	"issue_subscribers": {table: "issue_subscribers", alias: "cfsub"},
}

// SQL turns a filter tree into the joins and the condition a list query needs.
//
// A relation is joined once however many conditions name it, which is what Django does within a single filter() call — and it is why `{"and": [{"assignee_id": a}, {"assignee_id": b}]}` asks for one row to be two people at once and therefore matches nothing. Reproduced rather than corrected.
func SQL(node *Node, issueAlias string) (Query, error) {
	if node == nil {
		return Query{}, nil
	}
	builder := &sqlBuilder{issueAlias: issueAlias, outer: map[string]bool{}, used: map[string]bool{}}
	// The first pass only decides which joins have to be outer ones, since a relation named anywhere under an `or` cannot drop the rows that do not match it.
	builder.markOuter(node, false)
	condition, arguments, err := builder.render(node, false)
	if err != nil {
		return Query{}, err
	}

	names := make([]string, 0, len(builder.used))
	for name := range builder.used {
		names = append(names, name)
	}
	sort.Strings(names)
	joins := make([]Join, 0, len(names))
	for _, name := range names {
		joins = append(joins, Join{
			Alias: relations[name].alias, Table: relations[name].table, Outer: builder.outer[name],
		})
	}
	return Query{Joins: joins, Condition: condition, Arguments: arguments}, nil
}

type sqlBuilder struct {
	issueAlias string
	outer      map[string]bool
	used       map[string]bool
}

// markOuter walks the tree once to find the relations that sit under an `or`. A negated branch is skipped, because everything inside one becomes a subquery rather than a join.
func (builder *sqlBuilder) markOuter(node *Node, underOr bool) {
	if node.Negated {
		return
	}
	under := underOr || node.Connector == Or
	for _, condition := range node.Conditions {
		if name, _, ok := relationOf(condition.Lookup); ok && under {
			builder.outer[name] = true
		}
	}
	for _, child := range node.Children {
		builder.markOuter(child, under)
	}
}

func (builder *sqlBuilder) render(node *Node, negated bool) (string, []any, error) {
	inside := negated || node.Negated
	parts := []string{}
	arguments := []any{}
	for _, condition := range node.Conditions {
		text, values, err := builder.condition(condition, inside)
		if err != nil {
			return "", nil, err
		}
		parts = append(parts, text)
		arguments = append(arguments, values...)
	}
	for _, child := range node.Children {
		text, values, err := builder.render(child, inside)
		if err != nil {
			return "", nil, err
		}
		parts = append(parts, text)
		arguments = append(arguments, values...)
	}
	if len(parts) == 0 {
		return "TRUE", nil, nil
	}
	joiner := " AND "
	if node.Connector == Or {
		joiner = " OR "
	}
	body := "(" + strings.Join(parts, joiner) + ")"
	if node.Negated {
		body = "NOT " + body
	}
	return body, arguments, nil
}

// condition renders one leaf. Inside a negated branch a relation becomes a correlated EXISTS rather than a join, which is what Django writes there and what keeps "not assigned to this person" from meaning "has some other assignee".
func (builder *sqlBuilder) condition(condition Condition, negated bool) (string, []any, error) {
	if name, column, ok := relationOf(condition.Lookup); ok {
		return builder.relationCondition(name, column, condition, negated)
	}
	return builder.plainCondition(condition)
}

func (builder *sqlBuilder) relationCondition(name, column string, condition Condition, negated bool) (string, []any, error) {
	table := relations[name]
	if !negated {
		builder.used[name] = true
		text, arguments, err := comparison(table.alias+"."+column, condition, table.alias)
		return text, arguments, err
	}
	// The soft-delete companion is written as a left join inside its own subquery, so it is true for a work item with no rows on the far side at all. That is Django's shape, oddity included.
	if strings.HasSuffix(condition.Lookup, "__deleted_at__isnull") {
		return "EXISTS (SELECT 1 FROM issues cfu0 LEFT JOIN " + table.table + " cfu1 ON cfu1.issue_id = cfu0.id" +
			" WHERE cfu1.deleted_at IS NULL AND cfu0.id = " + builder.issueAlias + ".id)", nil, nil
	}
	text, arguments, err := comparison("cfu1."+column, condition, "cfu1")
	if err != nil {
		return "", nil, err
	}
	return "EXISTS (SELECT 1 FROM " + table.table + " cfu1 WHERE " + text +
		" AND cfu1.issue_id = " + builder.issueAlias + ".id)", arguments, nil
}

// plainColumns maps every lookup the filter set can write on the work item itself onto the expression it compares.
var plainColumns = map[string]string{
	"priority__exact":         "%s.priority",
	"priority__in":            "%s.priority",
	"state_id__exact":         "%s.state_id",
	"state_id__in":            "%s.state_id",
	"project_id__exact":       "%s.project_id",
	"project_id__in":          "%s.project_id",
	"created_by_id__exact":    "%s.created_by_id",
	"created_by_id__in":       "%s.created_by_id",
	"is_draft__exact":         "%s.is_draft",
	"archived_at__isnull":     "%s.archived_at",
	"start_date__exact":       "%s.start_date",
	"start_date__range":       "%s.start_date",
	"target_date__exact":      "%s.target_date",
	"target_date__range":      "%s.target_date",
	"created_at__date":        "(%s.created_at AT TIME ZONE 'UTC')::date",
	"created_at__date__range": "(%s.created_at AT TIME ZONE 'UTC')::date",
	"updated_at__date":        "(%s.updated_at AT TIME ZONE 'UTC')::date",
	"updated_at__date__range": "(%s.updated_at AT TIME ZONE 'UTC')::date",
	// state__group reaches the state table, which this reads for itself rather than relying on the caller's join.
	"state__group__exact": "(SELECT cfs.\"group\" FROM states cfs WHERE cfs.id = %s.state_id)",
	"state__group__in":    "(SELECT cfs.\"group\" FROM states cfs WHERE cfs.id = %s.state_id)",
}

func (builder *sqlBuilder) plainCondition(condition Condition) (string, []any, error) {
	if condition.Lookup == "pk__in" && condition.Value.Kind == KindEveryRow {
		// A filter whose value cleaned to nothing asks for every row, which is no condition at all.
		return "TRUE", nil, nil
	}
	pattern, known := plainColumns[condition.Lookup]
	if !known {
		return "", nil, ErrUntranslatable
	}
	return comparison(fmt.Sprintf(pattern, builder.issueAlias), condition, builder.issueAlias)
}

// comparison writes one expression against one cleaned value, choosing the operator from the lookup's suffix.
func comparison(expression string, condition Condition, _ string) (string, []any, error) {
	switch {
	case strings.HasSuffix(condition.Lookup, "__isnull"):
		if condition.Value.Kind == KindBool && !condition.Value.Bool {
			return expression + " IS NOT NULL", nil, nil
		}
		return expression + " IS NULL", nil, nil
	case strings.HasSuffix(condition.Lookup, "__range"):
		if condition.Value.Kind != KindList || len(condition.Value.List) != 2 {
			// A range with any other number of ends is a ValueError inside the ORM, which answers 500.
			return "", nil, ErrUntranslatable
		}
		return expression + " BETWEEN ? AND ?",
			[]any{literal(condition.Value.List[0]), literal(condition.Value.List[1])}, nil
	case strings.HasSuffix(condition.Lookup, "__in"):
		if condition.Value.Kind != KindList {
			return "", nil, ErrUntranslatable
		}
		if len(condition.Value.List) == 0 {
			// An empty IN is an empty result set rather than a syntax error, which is what Django turns it into too.
			return "FALSE", nil, nil
		}
		values := make([]any, 0, len(condition.Value.List))
		for _, item := range condition.Value.List {
			values = append(values, literal(item))
		}
		return expression + " IN ?", []any{values}, nil
	}
	if condition.Value.Kind == KindNull {
		return expression + " IS NULL", nil, nil
	}
	return expression + " = ?", []any{literal(condition.Value)}, nil
}

// literal is the Go value a cleaned filter value stands for.
func literal(value Value) any {
	switch value.Kind {
	case KindBool:
		return value.Bool
	case KindNull:
		return nil
	default:
		return value.Text
	}
}

// relationOf reads the related name and the column off a lookup, and reports whether the lookup reaches across a relation at all.
//
// The column is whatever is left once the relation's name comes off the front and the lookup expression comes off the back, so both the value column and its soft-delete companion land on the same join.
func relationOf(lookup string) (string, string, bool) {
	separator := strings.Index(lookup, "__")
	if separator < 0 {
		return "", "", false
	}
	name := lookup[:separator]
	if _, known := relations[name]; !known {
		return "", "", false
	}
	column := lookup[separator+2:]
	for _, expression := range []string{"__in", "__isnull", "__range"} {
		column = strings.TrimSuffix(column, expression)
	}
	return name, column, true
}
