package externalapi

import (
	"strings"
	"testing"
)

// The archived switch reads "true" or "1" after lowercasing, and nothing else.
func TestTheArchivedSwitchReadsTwoWords(t *testing.T) {
	included := func(raw string) bool {
		value := strings.ToLower(raw)
		return value == "true" || value == "1"
	}
	for raw, want := range map[string]bool{
		"true": true, "TRUE": true, "True": true, "1": true,
		"false": false, "0": false, "yes": false, "": false, "2": false,
	} {
		if got := included(raw); got != want {
			t.Errorf("%q reads as %v, want %v", raw, got, want)
		}
	}
}

// The lite serializer is nine fields and nothing else: no network, no lead, no counts.
func TestProjectLiteJSONShape(t *testing.T) {
	data := projectLiteJSON(Project{ID: "project-id", Network: 2})
	for _, field := range []string{
		"id", "identifier", "name", "cover_image", "icon_prop", "emoji",
		"description", "cover_image_url", "archived_at",
	} {
		if _, present := data[field]; !present {
			t.Errorf("the lite project is missing %q", field)
		}
	}
	if len(data) != 9 {
		t.Fatalf("the lite project has %d fields, want 9", len(data))
	}
	for _, absent := range []string{"network", "project_lead", "total_members", "workspace"} {
		if _, present := data[absent]; present {
			t.Errorf("the lite project carries %q, which its serializer does not name", absent)
		}
	}
}

// Every summary field has a subquery, and asking for nothing valid asks for all eight.
func TestTheSummaryFields(t *testing.T) {
	if len(projectSummaryFields) != 8 {
		t.Fatalf("there are %d summary fields, want 8", len(projectSummaryFields))
	}
	for _, field := range projectSummaryFields {
		if projectSummaryColumn(field) == "0" {
			t.Errorf("%q counts nothing", field)
		}
	}
	if projectSummaryColumn("nonsense") != "0" {
		t.Error("an unknown field counts nothing")
	}
}

// Only the issue count excludes anything, and it excludes triage alone, not archived issues and not drafts.
func TestOnlyTheIssueCountExcludesAnything(t *testing.T) {
	issues := projectSummaryColumn("issues")
	if !strings.Contains(issues, "'triage'") {
		t.Error("the issue count excludes the triage state")
	}
	for _, absent := range []string{"archived_at IS NULL AND", "is_draft"} {
		if strings.Contains(issues, absent) {
			t.Errorf("the issue count must not exclude %q: it is not the issue_objects manager", absent)
		}
	}
	for _, field := range []string{"members", "states", "labels", "cycles", "modules", "intakes", "pages"} {
		column := projectSummaryColumn(field)
		if strings.Contains(column, "deleted_at") {
			t.Errorf("%q counts rows of its own table, soft-deleted ones included", field)
		}
	}
}

// The order allowlist is five fields, and a doubled dash is rejected rather than reaching the ORM.
func TestTheProjectOrderAllowlist(t *testing.T) {
	if len(projectOrderByAllowlist) != 5 {
		t.Fatalf("there are %d orderable fields, want 5", len(projectOrderByAllowlist))
	}
	for value, want := range map[string]string{
		"":           "-created_at",
		"name":       "name",
		"-name":      "-name",
		"--name":     "-created_at",
		"identifier": "-created_at",
		"name; DROP": "-created_at",
		"sort_order": "sort_order",
	} {
		if got := sanitizeOrderBy(value, projectOrderByAllowlist, "-created_at"); got != want {
			t.Errorf("sanitizeOrderBy(%q) = %q, want %q", value, got, want)
		}
	}
}
