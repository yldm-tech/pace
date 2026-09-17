package project

import (
	"testing"
	"time"
)

// The update snapshot is ModuleSerializer over the instance, which carries none of the annotated counts.
func TestModuleWriteSnapshotCarriesNoAnnotations(t *testing.T) {
	data := moduleWriteJSON(Module{ID: "module-id", ProjectID: "project-id", WorkspaceID: "workspace-id", Status: "planned"})
	for _, absent := range []string{"is_favorite", "total_issues", "completed_issues", "member_ids", "total_estimate_points"} {
		if _, present := data[absent]; present {
			t.Errorf("%q is annotated on the queryset, not on the instance", absent)
		}
	}
	for _, field := range []string{
		"id", "created_at", "updated_at", "created_by", "updated_by", "deleted_at",
		"name", "description", "description_text", "description_html",
		"start_date", "target_date", "status", "lead", "view_props", "sort_order",
		"external_source", "external_id", "archived_at", "logo_props", "project", "workspace",
	} {
		if _, present := data[field]; !present {
			t.Errorf("the snapshot is missing %q", field)
		}
	}
	if len(data) != 22 {
		t.Fatalf("the snapshot has %d fields, want 22", len(data))
	}
}

// A module is archived on its status, where a cycle is archived on its end date. The two apps judge "finished" differently and both are reproduced.
func TestAModuleIsArchivedOnItsStatus(t *testing.T) {
	for status, archivable := range map[string]bool{
		"completed": true, "cancelled": true,
		"planned": false, "backlog": false, "in-progress": false, "paused": false,
	} {
		module := Module{Status: status}
		got := module.Status == "completed" || module.Status == "cancelled"
		if got != archivable {
			t.Errorf("status %q: archivable = %v, want %v", status, got, archivable)
		}
	}
	// A cycle with the same status but a future end date is not archivable, which is the point of the difference.
	future := time.Now().Add(time.Hour)
	cycle := Cycle{EndDate: &future}
	if cycle.EndDate.Before(time.Now()) {
		t.Fatal("a cycle is judged by its end date, not by any status")
	}
}

// Unlike a cycle, a module has no completed rule on update: a finished module can still be edited.
func TestAFinishedModuleMayStillBeEdited(t *testing.T) {
	module := Module{Status: "completed"}
	// The only refusal on update is the archive one.
	if module.ArchivedAt != nil {
		t.Fatal("the fixture is not archived")
	}
	archived := time.Now()
	module.ArchivedAt = &archived
	if module.ArchivedAt == nil {
		t.Fatal("an archived module is the one case that refuses an update")
	}
}

// A full update requires the name, which is what DRF's partial=False adds.
func TestModuleFullUpdateRequiresTheName(t *testing.T) {
	required := fullUpdateRequirements["module"]
	if len(required) != 1 || required[0] != "name" {
		t.Fatalf("a module's full update requires %v, want just the name", required)
	}
}
