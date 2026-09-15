package project

import (
	"testing"
	"time"

	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
)

// ProjectUserPropertySerializer is fields = "__all__" with the three relations read-only, which renders fifteen keys.
func TestProjectUserPropertyShape(t *testing.T) {
	property := ProjectUserProperty{
		ID: "property-id", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		ProjectID: "project-id", WorkspaceID: "workspace-id", UserID: "user-id",
		Filters: defaultFiltersJSON(), DisplayFilters: defaultDisplayFiltersJSON(),
		DisplayProperties: defaultDisplayPropertiesJSON(), RichFilters: emptyJSON(),
		Preferences: defaultPreferencesJSON(), SortOrder: 65535,
	}
	data := projectUserPropertyJSON(property)
	for _, field := range []string{
		"id", "created_at", "updated_at", "created_by", "updated_by", "deleted_at",
		"filters", "display_filters", "display_properties", "rich_filters", "preferences",
		"sort_order", "project", "workspace", "user",
	} {
		if _, present := data[field]; !present {
			t.Errorf("serialized property is missing %q", field)
		}
	}
	if len(data) != 15 {
		t.Fatalf("serialized property has %d fields, want 15", len(data))
	}
	// The json columns come back decoded rather than as raw bytes.
	if _, raw := data["filters"].(auth.JSONValue); raw {
		t.Fatal("filters should be decoded, not handed back as bytes")
	}
	if data["sort_order"] != float64(65535) {
		t.Fatalf("sort_order = %v", data["sort_order"])
	}
}

// A fresh row takes every default from the model, which is what get_or_create with no defaults does.
func TestNewPropertyRowTakesTheModelDefaults(t *testing.T) {
	if string(emptyJSON()) != "{}" {
		t.Fatalf("rich_filters default = %s, want an empty object", emptyJSON())
	}
	for name, value := range map[string]auth.JSONValue{
		"filters":            defaultFiltersJSON(),
		"display_filters":    defaultDisplayFiltersJSON(),
		"display_properties": defaultDisplayPropertiesJSON(),
		"preferences":        defaultPreferencesJSON(),
	} {
		if len(value) == 0 || string(value) == "null" {
			t.Errorf("%s default is empty", name)
		}
	}
}

// The deleted-issues filter accepts the same datetime shapes the history filter does.
func TestDeletedIssuesFilterParsing(t *testing.T) {
	if _, ok := parseDjangoDateTime("2026-09-15T04:00:00Z"); !ok {
		t.Fatal("an ISO instant should be accepted")
	}
	if _, ok := parseDjangoDateTime("last tuesday"); ok {
		t.Fatal("nonsense should be refused")
	}
}

// bulk delete counts the issues the filter matched, not the ids the caller sent, so a request naming an id in another project reports fewer.
func TestBulkDeleteMessageCountsMatchedIssues(t *testing.T) {
	for matched, want := range map[int]string{0: "0 issues were deleted", 1: "1 issues were deleted", 12: "12 issues were deleted"} {
		got := bulkDeleteMessage(matched)
		if got != want {
			t.Errorf("bulkDeleteMessage(%d) = %q, want %q", matched, got, want)
		}
	}
}
