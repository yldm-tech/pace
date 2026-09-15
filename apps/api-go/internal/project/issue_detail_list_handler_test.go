package project

import (
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// IssueListDetailSerializer is a plain twenty-three field dictionary, plus the two relation lists when the caller asks for them.
func TestIssueDetailListProjection(t *testing.T) {
	row := issueListRow{Issue: sampleIssueRow().Issue}
	data := issueListDetailJSON(row, nil)
	for _, field := range []string{
		"id", "name", "state_id", "sort_order", "completed_at", "estimate_point",
		"priority", "start_date", "target_date", "sequence_id", "project_id", "parent_id",
		"created_at", "updated_at", "created_by", "updated_by", "is_draft", "archived_at",
		"cycle_id", "module_ids", "label_ids", "assignee_ids",
		"sub_issues_count", "attachment_count", "link_count",
	} {
		if _, present := data[field]; !present {
			t.Errorf("the detail list is missing %q", field)
		}
	}
	if len(data) != 25 {
		t.Fatalf("the detail list has %d fields, want 25", len(data))
	}
	// Unlike the other projections this one carries no state__group and no deleted_at.
	for _, absent := range []string{"state__group", "deleted_at", "description_html"} {
		if _, present := data[absent]; present {
			t.Errorf("the detail list should not carry %q", absent)
		}
	}
}

// The counts here are the raw annotations rather than coalesced, so a null stays null where the other projections turn it into zero.
func TestTheDetailListLeavesNullCountsAlone(t *testing.T) {
	row := issueListRow{Issue: sampleIssueRow().Issue}
	detail := issueListDetailJSON(row, nil)
	if detail["sub_issues_count"] != (*int64)(nil) {
		t.Fatalf("sub_issues_count = %v, want the raw null", detail["sub_issues_count"])
	}
	// The paginated list's own projection does coalesce.
	if issueListRowJSON(row)["sub_issues_count"] != int64(0) {
		t.Fatal("the paginated list coalesces its counts")
	}
}

// The expansions are present as empty lists when asked for, so a client can map over them either way.
func TestRelationExpansionsAreListsWhenAskedFor(t *testing.T) {
	row := issueListRow{Issue: sampleIssueRow().Issue}
	data := issueListDetailJSON(row, map[string][]gin.H{"issue_relation": {}, "issue_related": {}})
	for _, name := range []string{"issue_relation", "issue_related"} {
		list, ok := data[name].([]gin.H)
		if !ok || len(list) != 0 {
			t.Errorf("%s = %v, want an empty list", name, data[name])
		}
	}
	// And absent entirely when not.
	bare := issueListDetailJSON(row, nil)
	for _, name := range []string{"issue_relation", "issue_related"} {
		if _, present := bare[name]; present {
			t.Errorf("%s should be absent when it was not asked for", name)
		}
	}
}

// The visibility subquery is what narrows a guest's view on this route, rather than the separate check the paginated list uses.
func TestTheVisibilityPredicateCoversTheThreeBranches(t *testing.T) {
	for _, fragment := range []string{
		"pv.role > 5",
		"pv.role = 5 AND pvp.guest_view_all_features = TRUE",
		"pv.role = 5 AND pvp.guest_view_all_features = FALSE AND i.created_by_id = ?",
		"pv.is_active = TRUE",
	} {
		if !strings.Contains(issueVisibilityPredicate, fragment) {
			t.Errorf("the visibility predicate is missing %q", fragment)
		}
	}
	// It takes the caller's id four times, once per branch plus the created_by comparison.
	if strings.Count(issueVisibilityPredicate, "?") != 4 {
		t.Fatalf("the predicate takes %d arguments, want 4", strings.Count(issueVisibilityPredicate, "?"))
	}
}
