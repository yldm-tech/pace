package externalapi

import (
	"os"
	"strconv"
	"strings"
	"testing"
)

// TestPlainIssueOrderMatchesDjango diffs the ordering the cycle's work item list builds against the ORDER BY Django renders, and against the number of joins it adds. The join count is the point: an ordering that reaches through a multi-valued relation repeats a work item once per related row, because the queryset carries no distinct.
func TestPlainIssueOrderMatchesDjango(t *testing.T) {
	fixture, err := os.ReadFile("testdata/plain_issue_order_by.tsv")
	if err != nil {
		t.Fatal(err)
	}
	rows := 0
	for _, line := range strings.Split(string(fixture), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) != 3 {
			t.Fatalf("malformed fixture row %q", line)
		}
		rows++
		joins, err := strconv.Atoi(parts[2])
		if err != nil {
			t.Fatal(err)
		}
		got := plainIssueOrderBy(parts[0])
		if normalized := normalizePlainOrder(got.Clause); normalized != parts[1] {
			t.Errorf("order_by %q renders %s, want %s", parts[0], normalized, parts[1])
		}
		if len(got.Joins) != joins {
			t.Errorf("order_by %q adds %d joins, want %d", parts[0], len(got.Joins), joins)
		}
	}
	if rows != 31 {
		t.Fatalf("the fixture has %d rows, want 31", rows)
	}
}

// normalizePlainOrder rewrites the aliases into the names Django prints.
func normalizePlainOrder(clause string) string {
	for alias, name := range map[string]string{
		"i.": "", "s.": "state.", "au.": "user.", "ol.": "label.", "om.": "module.",
	} {
		clause = strings.ReplaceAll(clause, alias, name)
	}
	return clause
}

// priority is ordered as a word here rather than as a severity, which is the difference between this list and the project's own.
func TestPriorityIsOrderedAlphabetically(t *testing.T) {
	got := plainIssueOrderBy("priority")
	if strings.Contains(got.Clause, "CASE") {
		t.Errorf("the priority ordering is %s, and this list has no case expression", got.Clause)
	}
	if got.Clause != "i.priority ASC" {
		t.Errorf("the priority ordering is %s", got.Clause)
	}
}

// The fallback is ascending by creation, not the descending one the project's list falls back to.
func TestThePlainFallbackIsAscending(t *testing.T) {
	for _, value := range []string{"", "--created_at", "project__name", "nonsense"} {
		if got := plainIssueOrderBy(value).Clause; got != "i.created_at ASC" {
			t.Errorf("order_by %q falls back to %s", value, got)
		}
	}
}
