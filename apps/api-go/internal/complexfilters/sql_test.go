package complexfilters

import (
	"strings"
	"testing"
)

func build(t *testing.T, raw string) Query {
	t.Helper()
	node, refusal := Parse(raw)
	if refusal != nil {
		t.Fatalf("%s was refused: %v", raw, refusal)
	}
	query, err := SQL(node, "i")
	if err != nil {
		t.Fatalf("%s did not translate: %v", raw, err)
	}
	return query
}

// A relation named in a plain AND is an inner join, which is what makes the condition narrow the list.
func TestAPositiveRelationIsAnInnerJoin(t *testing.T) {
	query := build(t, `{"assignee_id": "11111111-1111-1111-1111-111111111111"}`)
	if len(query.Joins) != 1 || query.Joins[0].Outer {
		t.Fatalf("joins = %#v", query.Joins)
	}
	if got := query.Joins[0].Clause("i"); got != "JOIN issue_assignees cfass ON cfass.issue_id = i.id" {
		t.Errorf("the join is %q", got)
	}
	for _, fragment := range []string{"cfass.assignee_id = ?", "cfass.deleted_at IS NULL"} {
		if !strings.Contains(query.Condition, fragment) {
			t.Errorf("the condition %q is missing %q", query.Condition, fragment)
		}
	}
	if len(query.Arguments) != 1 {
		t.Errorf("arguments = %#v", query.Arguments)
	}
}

// The same relation under an `or` becomes an outer join, since an inner one would drop the rows the other branch is there to find.
func TestARelationUnderAnOrIsAnOuterJoin(t *testing.T) {
	query := build(t, `{"or": [{"assignee_id": "11111111-1111-1111-1111-111111111111"}, {"priority": "high"}]}`)
	if len(query.Joins) != 1 || !query.Joins[0].Outer {
		t.Fatalf("joins = %#v", query.Joins)
	}
	if !strings.Contains(query.Condition, " OR ") {
		t.Errorf("the condition %q does not read as an or", query.Condition)
	}
}

// Two relations in one leaf are two joins, and each carries its own soft-delete check.
func TestTwoRelationsAreTwoJoins(t *testing.T) {
	query := build(t, `{"assignee_id": "11111111-1111-1111-1111-111111111111", "label_id": "22222222-2222-2222-2222-222222222222"}`)
	if len(query.Joins) != 2 {
		t.Fatalf("joins = %#v", query.Joins)
	}
	tables := []string{query.Joins[0].Table, query.Joins[1].Table}
	if tables[0] != "issue_assignees" || tables[1] != "issue_labels" {
		t.Errorf("tables = %#v", tables)
	}
}

// The same relation twice is still one join, which is why asking for two assignees at once matches nothing.
func TestTheSameRelationTwiceIsOneJoin(t *testing.T) {
	query := build(t, `{"and": [{"assignee_id": "11111111-1111-1111-1111-111111111111"}, {"assignee_id": "22222222-2222-2222-2222-222222222222"}]}`)
	if len(query.Joins) != 1 {
		t.Fatalf("joins = %#v", query.Joins)
	}
	if strings.Count(query.Condition, "cfass.assignee_id = ?") != 2 {
		t.Errorf("the condition is %q", query.Condition)
	}
}

// A negated relation becomes a correlated subquery rather than a join, so it does not drag the join's rows into the negation.
func TestANegatedRelationIsASubquery(t *testing.T) {
	query := build(t, `{"not": {"assignee_id": "11111111-1111-1111-1111-111111111111"}}`)
	if len(query.Joins) != 0 {
		t.Fatalf("a negated relation produced joins: %#v", query.Joins)
	}
	if !strings.HasPrefix(query.Condition, "NOT ") {
		t.Errorf("the condition %q is not negated", query.Condition)
	}
	if strings.Count(query.Condition, "EXISTS (") != 2 {
		t.Errorf("the condition %q does not carry both subqueries", query.Condition)
	}
	if !strings.Contains(query.Condition, "LEFT JOIN issue_assignees cfu1") {
		t.Error("the soft-delete companion is not written as a left join inside its subquery")
	}
}

// The dates each compare what they should: a plain column directly, a timestamp through its date.
func TestTheDateComparisons(t *testing.T) {
	for raw, want := range map[string]string{
		`{"start_date": "2026-01-02"}`:                   "i.start_date = ?",
		`{"start_date__range": "2026-01-02,2026-02-03"}`: "i.start_date BETWEEN ? AND ?",
		`{"created_at": "2026-01-02"}`:                   "(i.created_at AT TIME ZONE 'UTC')::date = ?",
		`{"created_at__range": "2026-01-02,2026-02-03"}`: "(i.created_at AT TIME ZONE 'UTC')::date BETWEEN ? AND ?",
	} {
		query := build(t, raw)
		if !strings.Contains(query.Condition, want) {
			t.Errorf("%s became %q, want it to contain %q", raw, query.Condition, want)
		}
	}
}

// A range with a number of ends other than two is a ValueError inside the ORM, which answers 500 rather than 400.
func TestARangeThatIsNotAPairIsUntranslatable(t *testing.T) {
	node, refusal := Parse(`{"created_at__range": "2026-01-02"}`)
	if refusal != nil {
		t.Fatalf("the tree was refused at validation: %v", refusal)
	}
	if _, err := SQL(node, "i"); err != ErrUntranslatable {
		t.Errorf("a one-ended range translated to %v", err)
	}
}

// An empty list is an empty result rather than a syntax error, and a value that cleaned to nothing is no condition at all.
func TestTheTwoEmptyValues(t *testing.T) {
	query := build(t, `{"state_id__in": ""}`)
	if !strings.Contains(query.Condition, "FALSE") {
		t.Errorf("an empty list became %q", query.Condition)
	}
	query = build(t, `{"assignee_id": null}`)
	if !strings.Contains(query.Condition, "TRUE") || len(query.Joins) != 0 {
		t.Errorf("an empty relation value became %q with joins %#v", query.Condition, query.Joins)
	}
}

// The archived shorthand reads the timestamp rather than a column of its own.
func TestTheArchivedShorthand(t *testing.T) {
	if got := build(t, `{"is_archived": true}`).Condition; !strings.Contains(got, "i.archived_at IS NOT NULL") {
		t.Errorf("archived true became %q", got)
	}
	if got := build(t, `{"is_archived": false}`).Condition; !strings.Contains(got, "i.archived_at IS NULL") {
		t.Errorf("archived false became %q", got)
	}
}

// The state group is read for itself rather than through a join the caller has to have made.
func TestTheStateGroupReadsItsOwnState(t *testing.T) {
	query := build(t, `{"state_group": "started"}`)
	if !strings.Contains(query.Condition, `SELECT cfs."group" FROM states cfs WHERE cfs.id = i.state_id`) {
		t.Errorf("the condition is %q", query.Condition)
	}
	if len(query.Joins) != 0 {
		t.Errorf("reading the state group added joins: %#v", query.Joins)
	}
}

// Nothing at all is nothing at all, not a condition that happens to be true.
func TestAnAbsentTreeIsAnEmptyQuery(t *testing.T) {
	query, err := SQL(nil, "i")
	if err != nil {
		t.Fatal(err)
	}
	if query.Condition != "" || len(query.Joins) != 0 || len(query.Arguments) != 0 {
		t.Errorf("query = %#v", query)
	}
}
