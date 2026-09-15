package project

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// normalizeOrderClause rewrites the SQL issueOrderClause produces into the shape Django's own ORDER BY renders in, so the two can be compared literally. The rewrites are mechanical: strip the issue alias, collapse each correlated subquery back to the joined column or aggregate it stands for, and unquote the string literals Django prints bare.
func normalizeOrderClause(clause string) string {
	clause = strings.Join(strings.Fields(clause), " ")
	for pattern, replacement := range map[string]string{
		`(SELECT s.name FROM states s WHERE s.id = i.state_id)`:                                                             `state.name`,
		`(SELECT s.group FROM states s WHERE s.id = i.state_id)`:                                                            `state.group`,
		`(SELECT MIN(u.first_name) FROM issue_assignees ia JOIN users u ON u.id = ia.assignee_id WHERE ia.issue_id = i.id)`: `MIN(user.first_name)`,
		`(SELECT MIN(l.name) FROM issue_labels il JOIN labels l ON l.id = il.label_id WHERE il.issue_id = i.id)`:            `MIN(label.name)`,
		`(SELECT MIN(m.name) FROM module_issues mi JOIN modules m ON m.id = mi.module_id WHERE mi.issue_id = i.id)`:         `MIN(module.name)`,
	} {
		clause = strings.ReplaceAll(clause, pattern, replacement)
	}
	clause = strings.ReplaceAll(clause, "i.", "")
	return regexp.MustCompile(`'([^']*)'`).ReplaceAllString(clause, "$1")
}

// TestIssueOrderClauseMatchesDjango diffs every allowlisted ordering against the ORDER BY Django emits for it. The fixture is generated, not written by hand, which is what catches the divergences reasoning alone missed: Django spells no NULLS position anywhere, the priority Case has no default so an unknown priority sorts as NULL, and the Min() joins carry no soft-delete predicate.
func TestIssueOrderClauseMatchesDjango(t *testing.T) {
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
		if got := normalizeOrderClause(issueOrderClause(orderBy)); got != want {
			t.Errorf("order_by %q:\n got  %s\n want %s", orderBy, got, want)
		}
	}
	// Both directions of all fourteen allowlisted fields.
	if rows != 28 {
		t.Fatalf("fixture has %d rows, want 28", rows)
	}
}

func TestOrderByIsSanitizedAgainstTheAllowlist(t *testing.T) {
	for _, test := range []struct {
		in   string
		want string
	}{
		{in: "", want: "-created_at"},
		{in: "created_at", want: "created_at"},
		{in: "-updated_at", want: "-updated_at"},
		{in: "priority", want: "priority"},
		{in: "state__group", want: "state__group"},
		// Not on the allowlist.
		{in: "id", want: "-created_at"},
		{in: "password", want: "-created_at"},
		// Two dashes are rejected rather than stripped, which is what stops an allowlist bypass.
		{in: "--created_at", want: "-created_at"},
	} {
		if got := sanitizeIssueOrderBy(test.in); got != test.want {
			t.Errorf("sanitizeIssueOrderBy(%q) = %q, want %q", test.in, got, test.want)
		}
	}
}

// An unknown order_by must never reach the query, since the parameter comes straight from the caller.
func TestUnknownOrderByCannotReachTheQuery(t *testing.T) {
	for _, value := range []string{"i.id; DROP TABLE issues", "--created_at", "id", ""} {
		if clause := issueOrderClause(value); clause != "i.created_at DESC" {
			t.Errorf("order_by %q produced %s, want the safe default", value, clause)
		}
	}
}

// Django builds the priority Case and then orders by it ascending in both directions; only the string handed to the paginator flips. So priority and -priority give the same order, which the fixture pins and this names.
func TestPriorityOrderIsTheSameInBothDirections(t *testing.T) {
	if issueOrderClause("priority") != issueOrderClause("-priority") {
		t.Fatal("priority orders differ by direction, but Django's do not")
	}
}

// No clause may pin a NULLS position: Django emits a bare ASC/DESC and leaves Postgres to decide, so an explicit NULLS LAST would move null target dates on a descending sort.
func TestNoClausePinsANullsPosition(t *testing.T) {
	for field := range issueOrderAllowlist {
		for _, value := range []string{field, "-" + field} {
			if strings.Contains(issueOrderClause(value), "NULLS") {
				t.Errorf("order_by %q pins a NULLS position", value)
			}
		}
	}
}
