package project

import (
	"strings"
	"testing"
)

// TestOrderClauseProducesSQL covers the gap between what sanitizeOrderBy returns and what a database accepts.
//
// sanitizeOrderBy speaks Django's ordering syntax, where a leading minus means descending. Concatenating that straight after a table alias produced `p.-created_at`, which Postgres rejects with `syntax error at or near "-"`, so the project pages list and the workspace views list both answered 500 on every request that did not name an order.
func TestOrderClauseProducesSQL(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		alias string
		order string
		want  string
	}{
		{name: "the default the pages list falls back to", alias: "p.", order: "-created_at", want: "p.created_at DESC"},
		{name: "ascending carries the direction too", alias: "p.", order: "created_at", want: "p.created_at ASC"},
		{name: "another alias", alias: "v.", order: "-updated_at", want: "v.updated_at DESC"},
		{name: "no alias at all", alias: "", order: "name", want: "name ASC"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			got := orderClause(testCase.alias, testCase.order)
			if got != testCase.want {
				t.Errorf("orderClause(%q, %q) = %q, want %q", testCase.alias, testCase.order, got, testCase.want)
			}
			if strings.Contains(got, ".-") || strings.HasPrefix(got, "-") {
				t.Errorf("orderClause(%q, %q) = %q, which is Django's syntax and not SQL", testCase.alias, testCase.order, got)
			}
		})
	}
}

// The two clauses that were wrong, built the way their handlers build them.
func TestTheListOrderingsAreValidSQL(t *testing.T) {
	pages := "is_favorite DESC, " + orderClause("p.", sanitizeOrderBy("", pageOrderByAllowlist, "-created_at")) + ", p.id"
	if want := "is_favorite DESC, p.created_at DESC, p.id"; pages != want {
		t.Errorf("the pages ordering is %q, want %q", pages, want)
	}
	views := orderClause("v.", sanitizeOrderBy("", viewOrderByAllowlist, "-created_at"))
	if want := "v.created_at DESC"; views != want {
		t.Errorf("the views ordering is %q, want %q", views, want)
	}
}
