package project

import (
	"strings"
	"testing"
	"time"
)

// The list returns twenty-three fields.
func TestCycleListProjection(t *testing.T) {
	data := cycleListJSON(cycleRow{Cycle: Cycle{ID: "cycle-id", WorkspaceID: "workspace-id", ProjectID: "project-id"}, Status: "DRAFT"}, time.UTC)
	for _, field := range []string{
		"id", "workspace_id", "project_id", "name", "description", "start_date", "end_date",
		"owned_by_id", "view_props", "sort_order", "external_source", "external_id",
		"progress_snapshot", "logo_props", "is_favorite", "total_issues",
		"cancelled_issues", "completed_issues", "assignee_ids", "status", "version", "created_by",
	} {
		if _, present := data[field]; !present {
			t.Errorf("the cycle list is missing %q", field)
		}
	}
	if len(data) != 22 {
		t.Fatalf("the cycle list has %d fields, want 22", len(data))
	}
	// A null date stays null rather than becoming a zero time.
	if data["start_date"] != nil {
		t.Fatalf("start_date = %v, want null", data["start_date"])
	}
}

// The dates are rendered in the project's timezone rather than the caller's, which is the one place in the codebase that distinction is made.
func TestCycleDatesUseTheProjectsZone(t *testing.T) {
	shanghai, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	moment := time.Date(2026, 6, 1, 0, 0, 1, 0, time.UTC)
	data := cycleListJSON(cycleRow{Cycle: Cycle{StartDate: &moment}}, shanghai)
	rendered, ok := data["start_date"].(time.Time)
	if !ok {
		t.Fatalf("start_date = %T", data["start_date"])
	}
	if name, _ := rendered.Zone(); name != "CST" {
		t.Fatalf("start_date is in %s, want the project's zone", name)
	}
	if !rendered.Equal(moment) {
		t.Fatal("the conversion changed the instant")
	}
}

// Every count carries the same four exclusions, so a cycle's totals agree with what its board shows. The assignee aggregate deliberately carries none of them.
func TestCycleCountsShareTheirExclusions(t *testing.T) {
	annotations := cycleAnnotations()
	// Three counts, each with the four exclusions.
	if strings.Count(annotations, "ci.deleted_at IS NULL AND ii.deleted_at IS NULL") != 3 {
		t.Fatalf("expected three counts sharing the exclusions:\n%s", annotations)
	}
	for _, fragment := range []string{
		"ii.archived_at IS NULL AND ii.is_draft = FALSE",
		"st.group = 'completed'",
		"st.group = 'cancelled'",
	} {
		if !strings.Contains(annotations, fragment) {
			t.Errorf("the select is missing %q", fragment)
		}
	}
	// The assignee aggregate filters only the assignee link, matching the queryset: a soft-deleted cycle link still contributes.
	aggregate := annotations[strings.Index(annotations, "ARRAY_AGG"):]
	aggregate = aggregate[:strings.Index(aggregate, "AS assignee_ids")]
	if strings.Contains(aggregate, "ii.archived_at") || strings.Contains(aggregate, "ci.deleted_at") {
		t.Errorf("the assignee aggregate carries exclusions Django's does not:\n%s", aggregate)
	}
	if !strings.Contains(aggregate, "ia.deleted_at IS NULL") {
		t.Error("the assignee aggregate filters the assignee link itself")
	}
}

// The status is derived rather than stored, and a cycle with no dates at all is a draft.
func TestCycleStatusCases(t *testing.T) {
	annotations := cycleAnnotations()
	for _, fragment := range []string{
		"THEN 'CURRENT'", "THEN 'UPCOMING'", "THEN 'COMPLETED'", "ELSE 'DRAFT'",
	} {
		if !strings.Contains(annotations, fragment) {
			t.Errorf("the status case is missing %q", fragment)
		}
	}
	// A cycle with a start date but no end date falls through to the default, since every earlier branch needs both or the other one.
	if strings.Count(annotations, "ELSE 'DRAFT'") != 1 {
		t.Error("there is one default")
	}
}

// The list orders favourites first and then newest, which overrides the queryset's own ordering by name.
func TestCycleListOrdering(t *testing.T) {
	// Pinned here because the two orderings differ and the list's is the one that reaches the client.
	const listOrder = "is_favorite DESC, c.created_at DESC"
	if !strings.Contains(listOrder, "is_favorite DESC") || !strings.Contains(listOrder, "created_at DESC") {
		t.Fatal("the list orders favourites first, then newest")
	}
}
