package externalapi

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// normalizeIssueOrderClause rewrites the SQL externalIssueOrderClause produces into the shape Django's own ORDER BY renders in. The rewrites are mechanical: strip the work item alias, collapse each correlated aggregate back to the joined one it stands for, and unquote the literals Django prints bare.
func normalizeIssueOrderClause(clause string) string {
	clause = strings.Join(strings.Fields(clause), " ")
	for pattern, replacement := range map[string]string{
		`(SELECT MAX(u.first_name) FROM issue_assignees ia JOIN users u ON u.id = ia.assignee_id WHERE ia.issue_id = i.id)`: `MAX(user.first_name)`,
		`(SELECT MAX(l.name) FROM issue_labels il JOIN labels l ON l.id = il.label_id WHERE il.issue_id = i.id)`:            `MAX(label.name)`,
	} {
		clause = strings.ReplaceAll(clause, pattern, replacement)
	}
	clause = strings.ReplaceAll(clause, "i.", "")
	clause = strings.ReplaceAll(clause, "s.group", "state.group")
	return regexp.MustCompile(`'([^']*)'`).ReplaceAllString(clause, "$1")
}

// TestExternalIssueOrderClauseMatchesDjango diffs every ordering the work item list accepts against the ORDER BY Django emits for it. The fixture is generated, which is what pins the three ways this ordering differs from the session API's: no secondary key, a reversed priority list, and a default on the state Case.
func TestExternalIssueOrderClauseMatchesDjango(t *testing.T) {
	fixture, err := os.ReadFile("testdata/issue_order_by.tsv")
	if err != nil {
		t.Fatal(err)
	}
	rows := 0
	for _, line := range strings.Split(string(fixture), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		orderBy, want, found := strings.Cut(line, "\t")
		if !found {
			t.Fatalf("malformed fixture row %q", line)
		}
		rows++
		got, err := externalIssueOrderClause(orderBy)
		if orderBy == "issue_module__module__name" || orderBy == "-issue_module__module__name" {
			// Django builds this clause and the database then refuses it, so there is nothing to compare.
			if err == nil {
				t.Errorf("order_by %q renders %s, but the query it belongs to cannot run", orderBy, got)
			}
			continue
		}
		if err != nil {
			t.Fatalf("order_by %q: %v", orderBy, err)
		}
		if normalized := normalizeIssueOrderClause(got); normalized != want {
			t.Errorf("order_by %q:\n got  %s\n want %s", orderBy, normalized, want)
		}
	}
	// Both directions of all fourteen allowlisted fields, plus four values that fall back.
	if rows != 32 {
		t.Fatalf("fixture has %d rows, want 32", rows)
	}
}

// The module ordering is the one allowlisted field the endpoint cannot serve: the column is not in the select list of a distinct query, so Postgres refuses it.
func TestOrderingByModuleNameFails(t *testing.T) {
	for _, value := range []string{"issue_module__module__name", "-issue_module__module__name"} {
		if _, err := externalIssueOrderClause(value); err == nil {
			t.Errorf("order_by %q is served, but Django's query for it does not run", value)
		}
	}
}

// Unlike the session API's list, this one really does reverse the priority ordering rather than ordering the same way twice.
func TestPriorityOrderingReversesHere(t *testing.T) {
	ascending, err := externalIssueOrderClause("priority")
	if err != nil {
		t.Fatal(err)
	}
	descending, err := externalIssueOrderClause("-priority")
	if err != nil {
		t.Fatal(err)
	}
	if ascending == descending {
		t.Fatal("the two directions render the same clause, which is the session API's behaviour rather than this one's")
	}
	if !strings.Contains(ascending, "'urgent' THEN 0") || !strings.Contains(descending, "'none' THEN 0") {
		t.Errorf("the lists are not reversed:\n %s\n %s", ascending, descending)
	}
	// Both are ascending; only the list turns around.
	if !strings.HasSuffix(ascending, " ASC") || !strings.HasSuffix(descending, " ASC") {
		t.Error("a direction other than ascending reached the clause")
	}
}

// The priority Case has no default and the state Case has one, which decides where an unknown value sorts.
func TestOnlyTheStateCaseHasADefault(t *testing.T) {
	priority, err := externalIssueOrderClause("priority")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(priority, "ELSE NULL END") {
		t.Errorf("the priority case has a default: %s", priority)
	}
	state, err := externalIssueOrderClause("state__group")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(state, "ELSE 5 END") {
		t.Errorf("the state case has no default: %s", state)
	}
	// Both state orderings are the same clause: the name is ordered by the group.
	byName, err := externalIssueOrderClause("state__name")
	if err != nil {
		t.Fatal(err)
	}
	if byName != state {
		t.Errorf("state__name and state__group render differently:\n %s\n %s", byName, state)
	}
}

// No ordering carries a secondary key, which is the other half of the disagreement with order_issue_queryset.
func TestNoOrderingCarriesASecondKey(t *testing.T) {
	for field := range issueOrderAllowlist {
		if field == "issue_module__module__name" {
			continue
		}
		clause, err := externalIssueOrderClause(field)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(clause, ",") {
			t.Errorf("order_by %q has more than one key: %s", field, clause)
		}
	}
}
