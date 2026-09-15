package project

import (
	"strings"
	"testing"
	"time"

	"github.com/lib/pq"
)

// issue_on_results returns twenty-three required fields plus the three id arrays. The group-by field is named state__group rather than state_group, since that is the lookup path the projection asks for.
func TestIssueListProjectionShape(t *testing.T) {
	group := "started"
	row := issueListRow{
		Issue:      sampleIssueRow().Issue,
		StateGroup: &group,
		LabelIDs:   pq.StringArray{"label-a"},
	}
	data := issueListRowJSON(row)
	for _, field := range []string{
		"id", "name", "state_id", "sort_order", "completed_at", "estimate_point",
		"priority", "start_date", "target_date", "sequence_id", "project_id", "parent_id",
		"cycle_id", "sub_issues_count", "created_at", "updated_at", "created_by", "updated_by",
		"attachment_count", "link_count", "is_draft", "archived_at", "state__group",
		"assignee_ids", "label_ids", "module_ids",
	} {
		if _, present := data[field]; !present {
			t.Errorf("listed issue is missing %q", field)
		}
	}
	if len(data) != 26 {
		t.Fatalf("listed issue has %d fields, want 26", len(data))
	}
	// description_html is not in the projection, so the list never carries issue bodies.
	if _, present := data["description_html"]; present {
		t.Error("the list projection does not select description_html")
	}
	// The counts are null in the query and become zero here, the way an IntegerField renders a null annotation.
	if data["sub_issues_count"] != int64(0) || data["link_count"] != int64(0) {
		t.Fatalf("counts = %v %v, want zero", data["sub_issues_count"], data["link_count"])
	}
	if got, ok := data["module_ids"].([]string); !ok || len(got) != 0 {
		t.Fatalf("module_ids = %v, want an empty array", data["module_ids"])
	}
}

// The annotation SQL keeps the soft-delete and archive predicates the endpoint's own subqueries carry.
func TestIssueListAnnotations(t *testing.T) {
	annotations := issueListAnnotations()
	for _, fragment := range []string{
		"ci.deleted_at IS NULL", "il.deleted_at IS NULL", "fa.entity_type = 'ISSUE_ATTACHMENT'",
		"m.archived_at IS NULL", "AS state_group",
	} {
		if !strings.Contains(annotations, fragment) {
			t.Errorf("the select is missing %q", fragment)
		}
	}
	// The child count applies the whole issue_objects manager.
	if !strings.Contains(annotations, issueObjectsPredicate("sub")) {
		t.Error("sub_issues_count must apply the whole issue_objects predicate")
	}
	// Unlike the sub-issue read, these counts do not coalesce; the serializer turns the null into zero.
	if strings.Contains(annotations, "COALESCE((SELECT COUNT(") {
		t.Error("the counts must not coalesce here")
	}
}

// The argument list is handed out condition by condition, so each condition has to declare how many it takes.
func TestPlaceholderCounting(t *testing.T) {
	for condition, want := range map[string]int{
		"i.priority IN (?, ?)":              2,
		"i.parent_id IS NULL":               0,
		"i.target_date >= ?":                1,
		"UPPER(i.name::text) LIKE UPPER(?)": 1,
		"FALSE":                             0,
	} {
		if got := countPlaceholders(condition); got != want {
			t.Errorf("countPlaceholders(%q) = %d, want %d", condition, got, want)
		}
	}
}

// The filters and the conditions have to stay in step: every condition's placeholders are drawn from the flat argument list in order.
func TestConditionsAndArgumentsStayInStep(t *testing.T) {
	filters := issueFilters(map[string]string{
		"priority": "high,urgent",
		"name":     "bug",
		"labels":   "00000000-0000-0000-0000-000000000001",
	}, "GET", "", time.Now())
	_, conditions, arguments, ok := issueFilterSQL(filters)
	if !ok {
		t.Fatal("the filters should translate")
	}
	total := 0
	for _, condition := range conditions {
		total += countPlaceholders(condition)
	}
	if total != len(arguments) {
		t.Fatalf("conditions want %d arguments but %d were produced", total, len(arguments))
	}
}
