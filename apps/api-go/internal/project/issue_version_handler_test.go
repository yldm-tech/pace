package project

import (
	"testing"
	"time"

	"github.com/lib/pq"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
)

// IssueVersionDetailSerializer names "name" twice in its field list and DRF silently dedupes, so the response carries thirty keys. properties and activity are on the model but not in the list.
func TestIssueVersionDetailShape(t *testing.T) {
	start := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)
	version := IssueVersion{
		ID: "version-id", WorkspaceID: "workspace-id", ProjectID: "project-id", IssueID: "issue-id",
		Name: "Snapshot", Priority: "high", SequenceID: 7, SortOrder: 65535,
		OwnedByID: "user-id", StartDate: &start, LastSavedAt: start,
		Properties: auth.JSONValue(`{"a":1}`), Meta: auth.JSONValue(`{"b":2}`),
	}
	data := issueVersionJSON(version)
	for _, field := range []string{
		"id", "workspace", "project", "issue", "parent", "state", "estimate_point",
		"name", "priority", "start_date", "target_date", "assignees", "sequence_id",
		"labels", "sort_order", "completed_at", "archived_at", "is_draft",
		"external_source", "external_id", "type", "cycle", "modules", "meta",
		"last_saved_at", "owned_by", "created_at", "updated_at", "created_by", "updated_by",
	} {
		if _, present := data[field]; !present {
			t.Errorf("serialized version is missing %q", field)
		}
	}
	for _, absent := range []string{"properties", "activity"} {
		if _, present := data[absent]; present {
			t.Errorf("%q is on the model but not in the serializer's field list", absent)
		}
	}
	if len(data) != 30 {
		t.Fatalf("serialized version has %d fields, want 30", len(data))
	}
	// The dates are DateFields, so they render without a time part.
	if data["start_date"] != "2026-09-15" {
		t.Fatalf("start_date = %v, want a bare date", data["start_date"])
	}
	// The array columns default to empty rather than null.
	if got, ok := data["assignees"].([]string); !ok || len(got) != 0 {
		t.Fatalf("assignees = %v, want an empty array", data["assignees"])
	}
}

// Only created_at and updated_at move into the caller's timezone; last_saved_at is not in the converter's list.
func TestVersionListConvertsOnlyTheAuditTimestamps(t *testing.T) {
	shanghai, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	moment := time.Date(2026, 9, 15, 4, 0, 0, 0, time.UTC)
	data := versionListJSON("id", "workspace", "project", "issue", "owner", moment, moment, moment, nil, nil, shanghai)
	created := data["created_at"].(time.Time)
	if name, _ := created.Zone(); name != "CST" {
		t.Fatalf("created_at is in %s, want the caller's zone", name)
	}
	saved := data["last_saved_at"].(time.Time)
	if name, _ := saved.Zone(); name != "UTC" {
		t.Fatalf("last_saved_at is in %s, want it left alone", name)
	}
	if len(data) != 10 {
		t.Fatalf("listed version has %d fields, want 10", len(data))
	}
}

// DRF renders a BinaryField as base64, and as null when the column is.
func TestDescriptionBinaryRendering(t *testing.T) {
	if base64OrNil(nil) != nil {
		t.Fatal("a null binary column must render as null")
	}
	if got := base64OrNil([]byte("hi")); got != "aGk=" {
		t.Fatalf("base64 = %v", got)
	}
	// An empty but present value is an empty string, not null.
	if got := base64OrNil([]byte{}); got != "" {
		t.Fatalf("empty binary = %v", got)
	}
}

func TestVersionArrayColumnsStayArrays(t *testing.T) {
	version := IssueVersion{Assignees: pq.StringArray{"a"}, Labels: pq.StringArray{}, Modules: nil}
	data := issueVersionJSON(version)
	if got := data["assignees"].([]string); len(got) != 1 {
		t.Fatalf("assignees = %v", got)
	}
	for _, field := range []string{"labels", "modules"} {
		if got, ok := data[field].([]string); !ok || len(got) != 0 {
			t.Errorf("%s = %v, want an empty array", field, data[field])
		}
	}
}
