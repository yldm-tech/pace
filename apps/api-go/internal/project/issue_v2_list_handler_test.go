package project

import (
	"strings"
	"testing"
	"time"
)

// The sync projection is twenty-six fields, and description_html is added only when the caller asks for it.
func TestIssueSyncProjection(t *testing.T) {
	row := issueListRow{Issue: sampleIssueRow().Issue}
	data := issueV2JSON(row, time.UTC, false)
	for _, field := range []string{
		"id", "name", "state_id", "state__group", "sort_order", "completed_at",
		"estimate_point", "priority", "start_date", "target_date", "sequence_id",
		"project_id", "parent_id", "cycle_id", "created_at", "updated_at",
		"created_by", "updated_by", "is_draft", "archived_at",
		"module_ids", "label_ids", "assignee_ids",
		"link_count", "attachment_count", "sub_issues_count",
	} {
		if _, present := data[field]; !present {
			t.Errorf("the sync projection is missing %q", field)
		}
	}
	if len(data) != 26 {
		t.Fatalf("the sync projection has %d fields, want 26", len(data))
	}
	if _, present := data["description_html"]; present {
		t.Error("description_html is added only when asked for")
	}
	if len(issueV2JSON(row, time.UTC, true)) != 27 {
		t.Fatal("asking for the description adds exactly one field")
	}
}

// Its counts are raw rather than coalesced, the same as the detail list's.
func TestTheSyncProjectionLeavesNullCountsAlone(t *testing.T) {
	data := issueV2JSON(issueListRow{Issue: sampleIssueRow().Issue}, time.UTC, false)
	if data["link_count"] != (*int64)(nil) {
		t.Fatalf("link_count = %v, want the raw null", data["link_count"])
	}
}

// This route's id arrays carry no soft-delete filter on the through table, which every other issue list does.
func TestTheSyncAnnotationsOmitTheSoftDeleteFilters(t *testing.T) {
	annotations := issueV2Annotations()
	for _, absent := range []string{"il2.deleted_at", "ia.deleted_at", "mi.deleted_at"} {
		if strings.Contains(annotations, absent) {
			t.Errorf("the sync select filters %s, which Django's does not", absent)
		}
	}
	// What it does filter, it keeps.
	for _, present := range []string{"pm2.is_active = TRUE", "m.archived_at IS NULL", "ci.deleted_at IS NULL"} {
		if !strings.Contains(annotations, present) {
			t.Errorf("the sync select is missing %q", present)
		}
	}
	// The paginated list does filter them, so the two genuinely differ.
	if !strings.Contains(issueListAnnotations(), "il2.deleted_at IS NULL") {
		t.Error("the paginated list filters soft-deleted label links")
	}
}

// Only created_at and updated_at move into the caller's timezone.
func TestTheSyncProjectionConvertsOnlyTheAuditTimestamps(t *testing.T) {
	shanghai, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	data := issueV2JSON(issueListRow{Issue: sampleIssueRow().Issue}, shanghai, false)
	if name, _ := data["created_at"].(time.Time).Zone(); name != "CST" {
		t.Fatalf("created_at is in %s", name)
	}
	if data["start_date"] != "2026-09-15" {
		t.Fatalf("start_date = %v, want a bare date", data["start_date"])
	}
}
