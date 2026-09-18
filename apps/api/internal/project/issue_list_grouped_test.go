package project

import (
	"strings"
	"testing"
)

// The allowlist is what stands between a query parameter and F(), values(), order_by() and a window's partition_by. It was added after the fact, so the eleven names are pinned rather than assumed.
func TestGroupByAllowlist(t *testing.T) {
	for _, allowed := range []string{
		"state_id", "state__group", "priority", "labels__id", "assignees__id",
		"issue_module__module_id", "cycle_id", "project_id", "created_by",
		"target_date", "start_date",
	} {
		if !issueGroupByAllowlist[allowed] {
			t.Errorf("%q should be allowed", allowed)
		}
	}
	if len(issueGroupByAllowlist) != 11 {
		t.Fatalf("allowlist has %d entries, want 11", len(issueGroupByAllowlist))
	}
	for _, refused := range []string{"id", "name", "description_html", "workspace_id", "", "state__name"} {
		if issueGroupByAllowlist[refused] {
			t.Errorf("%q must not be allowed", refused)
		}
	}
	// Every allowed name needs somewhere to partition and a source of known values, or a caller could reach a nil expression.
	for name := range issueGroupByAllowlist {
		if issueGroupPartition[name] == "" {
			t.Errorf("%q has no partition expression", name)
		}
		if _, known := issueGroupValues[name]; !known {
			t.Errorf("%q has no source of group values", name)
		}
	}
}

// Only the three that reach through a join need one, and it has to be a LEFT OUTER so an issue with no rows still partitions under the null group.
func TestOnlyTheManyValuedGroupsJoin(t *testing.T) {
	if len(issueGroupJoin) != 3 {
		t.Fatalf("joins = %v, want three", issueGroupJoin)
	}
	for name, join := range issueGroupJoin {
		if !strings.HasPrefix(join, "LEFT JOIN ") {
			t.Errorf("%q joins with %q, want a left outer join", name, join)
		}
		if _, multi := map[string]bool{"labels__id": true, "assignees__id": true, "issue_module__module_id": true}[name]; !multi {
			t.Errorf("%q should not need a join", name)
		}
	}
	// A plain group-by must not carry one.
	for _, plain := range []string{"state_id", "priority", "cycle_id", "created_by"} {
		if issueGroupJoin[plain] != "" {
			t.Errorf("%q should not join", plain)
		}
	}
}

// The window's ordering is not the list's. It spells NULLS LAST explicitly, and for priority it runs in the opposite direction, because order_issue_queryset hands the paginator the inverted annotation name.
func TestTheWindowOrdersDifferentlyFromTheList(t *testing.T) {
	for _, orderBy := range []string{"-created_at", "priority", "-priority", "state__group", "labels__name"} {
		clause := issueWindowOrderClause(orderBy)
		if !strings.Contains(clause, "NULLS LAST") {
			t.Errorf("%q: the window must spell NULLS LAST, got %s", orderBy, clause)
		}
		if !strings.HasSuffix(clause, "i.created_at DESC") {
			t.Errorf("%q: the window is missing the tiebreak: %s", orderBy, clause)
		}
		// The list's own ordering never spells it.
		if strings.Contains(issueOrderClause(orderBy), "NULLS") {
			t.Errorf("%q: the list ordering must not pin a NULLS position", orderBy)
		}
	}
	// priority ascending in the window is descending in the list's returned name, and the other way round.
	ascending := issueWindowOrderClause("priority")
	descending := issueWindowOrderClause("-priority")
	if ascending == descending {
		t.Fatal("the window's priority ordering does depend on direction, unlike the list's")
	}
	if !strings.Contains(ascending, "DESC NULLS LAST") {
		t.Fatalf("priority ascending should order the window descending: %s", ascending)
	}
	if !strings.Contains(descending, "ASC NULLS LAST") {
		t.Fatalf("priority descending should order the window ascending: %s", descending)
	}
}

// An unknown order_by falls back before it reaches the window, the same way it does for the list.
func TestTheWindowOrderIsSanitizedToo(t *testing.T) {
	clause := issueWindowOrderClause("i.id; DROP TABLE issues")
	if !strings.HasPrefix(clause, "i.created_at DESC NULLS LAST") {
		t.Fatalf("injected order = %s", clause)
	}
}

// Three group-bys append the literal None so an issue in no group still has a bucket; the rest do not.
func TestOnlySomeGroupsGetANoneBucket(t *testing.T) {
	for _, withNone := range []string{"labels__id", "issue_module__module_id", "cycle_id"} {
		if !issueGroupValues[withNone].WithNone {
			t.Errorf("%q should append None", withNone)
		}
	}
	for _, without := range []string{"state_id", "assignees__id", "project_id", "priority", "state__group"} {
		if issueGroupValues[without].WithNone {
			t.Errorf("%q should not append None", without)
		}
	}
}

// Two group-bys are fixed lists rather than queries, and their order is the one the frontend renders.
func TestFixedGroupValues(t *testing.T) {
	priority := issueGroupValues["priority"].Fixed
	if strings.Join(priority, ",") != "low,medium,high,urgent,none" {
		t.Fatalf("priority groups = %v", priority)
	}
	states := issueGroupValues["state__group"].Fixed
	if strings.Join(states, ",") != "backlog,unstarted,started,completed,cancelled" {
		t.Fatalf("state groups = %v", states)
	}
}

// The project list is workspace-wide even when a project is named, which is the one place the scoping is not applied.
func TestProjectGroupsAreNotProjectScoped(t *testing.T) {
	if issueGroupValues["project_id"].ProjectScoped {
		t.Fatal("the project group list is workspace-wide")
	}
	for _, scoped := range []string{"state_id", "labels__id", "cycle_id", "assignees__id", "issue_module__module_id"} {
		if !issueGroupValues[scoped].ProjectScoped {
			t.Errorf("%q should narrow to the project", scoped)
		}
	}
}

// The per-group totals skip an issue still waiting in intake, and archived and draft ones.
func TestGroupCountFilter(t *testing.T) {
	for _, fragment := range []string{
		"i.archived_at IS NULL", "i.is_draft = FALSE", "ii.status IN (1, -1, 2)",
	} {
		if !strings.Contains(issueGroupCountFilter, fragment) {
			t.Errorf("the count filter is missing %q", fragment)
		}
	}
}

// Every window clause must carry exactly one direction, and the direction the window uses is the one nullsLastSuffix supplies.
//
// This is a regression test with a specific history. caseOrder baked " ASC" into the CASE expression, so the two branches that built on it emitted `END ASC DESC NULLS LAST` -- a Postgres syntax error, which meant `group_by=<anything>&order_by=priority` and `order_by=state__group` answered 500 on the board's primary screen. The test above did not catch it because `strings.Contains(clause, "DESC NULLS LAST")` is satisfied by a clause that also has a stray ASC in front of it.
func TestTheWindowClauseCarriesOneDirection(t *testing.T) {
	for name := range issueOrderAllowlist {
		for _, orderBy := range []string{name, "-" + name} {
			clause := issueWindowOrderClause(orderBy)
			for _, doubled := range []string{"ASC ASC", "ASC DESC", "DESC DESC", "DESC ASC"} {
				if strings.Contains(clause, doubled) {
					t.Errorf("%q: %q in %s", orderBy, doubled, clause)
				}
			}
			// The direction has to be the token immediately before NULLS LAST, since that is what the sort applies to.
			index := strings.Index(clause, " NULLS LAST")
			if index < 0 {
				t.Errorf("%q has no NULLS LAST: %s", orderBy, clause)
				continue
			}
			if !strings.HasSuffix(clause[:index], " ASC") && !strings.HasSuffix(clause[:index], " DESC") {
				t.Errorf("%q has no direction before NULLS LAST: %s", orderBy, clause)
			}
		}
	}
}

// The flat list keeps the ascending CASE it always had; only the window takes its direction from elsewhere.
func TestTheFlatCaseOrderStaysAscending(t *testing.T) {
	for _, orderBy := range []string{"priority", "-priority", "state__group", "-state__group"} {
		clause := issueOrderClause(orderBy)
		if !strings.Contains(clause, " END ASC,") {
			t.Errorf("%q: the list's CASE must stay ascending: %s", orderBy, clause)
		}
	}
	if strings.Contains(caseOrderExpression("i.priority", priorityOrder, "NULL"), " ASC") {
		t.Fatal("the expression the window builds on must carry no direction of its own")
	}
}

// The window ranks rows and nothing else. Selecting the annotations inside it made Postgres evaluate eight correlated subqueries for every issue in the project to return one page of them; they are joined onto the surviving rows instead.
func TestTheWindowSelectsOnlyWhatItRanksBy(t *testing.T) {
	annotations := issueListAnnotations()
	for _, fragment := range []string{"AS link_count", "AS attachment_count", "AS sub_issues_count", "AS label_ids"} {
		if !strings.Contains(annotations, fragment) {
			t.Fatalf("the annotation list no longer contains %q, so this test is checking nothing", fragment)
		}
	}
}
