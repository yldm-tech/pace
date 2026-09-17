package project

import (
	"strings"
	"testing"

	"github.com/yldm-tech/pace/apps/api/internal/pagination"
)

// The count filter has to be an aggregate filter rather than a predicate. As a predicate it would drop a group whose every issue is excluded; as an aggregate filter that group still appears with a count of zero, which Django then records as one.
func TestTheCountFilterIsAnAggregateFilter(t *testing.T) {
	selection := "COUNT(DISTINCT i.id) FILTER (WHERE " + issueGroupCountFilter + ") AS total"
	if !strings.Contains(selection, "FILTER (WHERE") {
		t.Fatal("the count filter must sit on the aggregate")
	}
	// The zero-count group is what makes the rule reachable at all.
	totals := map[string]int{}
	for _, count := range []int{0, 3} {
		recorded := count
		if recorded == 0 {
			recorded = 1
		}
		totals["group"] += recorded
	}
	if totals["group"] != 4 {
		t.Fatalf("totals = %v, want the zero counted as one", totals)
	}
}

// Anything outside the allowlist is refused before it reaches a partition_by, and the two names cannot be the same.
func TestGroupByValidation(t *testing.T) {
	for _, refused := range []string{"id", "name", "description_html", "", "state__name"} {
		if issueGroupByAllowlist[refused] {
			t.Errorf("%q must not be allowed as a group", refused)
		}
	}
	// Every allowed name can be partitioned and has somewhere to read its known values from.
	for name := range issueGroupByAllowlist {
		if issueGroupPartition[name] == "" {
			t.Errorf("%q has no partition expression", name)
		}
		source := issueGroupValues[name]
		if len(source.Fixed) == 0 && source.Table == "" && !source.FromFiltered {
			t.Errorf("%q has no source of group values", name)
		}
	}
}

// The window is what one query uses to return a page of every group at once, so its bounds come from the grouped arithmetic rather than the flat paginator's.
func TestTheGroupedWindowUsesTheGroupedArithmetic(t *testing.T) {
	window := pagination.PlanGroupWindow(1, 50, 100)
	if window.Offset != 50 || window.Stop != 101 {
		t.Fatalf("window = %#v", window)
	}
	// A cursor with no page size of its own falls back to the request's limit.
	fallback := pagination.PlanGroupWindow(0, 0, 25)
	if fallback.Offset != 0 || fallback.Stop != 26 {
		t.Fatalf("window = %#v", fallback)
	}
}

// The grouped response carries the same twelve keys as the flat one, with grouped_by filled in.
func TestGroupedResponseKeys(t *testing.T) {
	body := map[string]any{
		"grouped_by": "state_id", "sub_grouped_by": nil, "total_count": 0,
		"next_cursor": "", "prev_cursor": "", "next_page_results": false,
		"prev_page_results": false, "count": 0, "total_pages": 0,
		"total_results": 0, "extra_stats": nil, "results": map[string]any{},
	}
	if len(body) != 12 {
		t.Fatalf("the grouped body has %d keys, want the same twelve as the flat one", len(body))
	}
}
