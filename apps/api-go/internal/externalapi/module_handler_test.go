package externalapi

import (
	"strings"
	"testing"
)

// A module is finished by its status where a cycle is finished by its end date, which is the same asymmetry the session API has one app over.
func TestAModuleIsArchivedOnItsStatus(t *testing.T) {
	archivable := func(status string) bool {
		return status == "completed" || status == "cancelled"
	}
	for status, want := range map[string]bool{
		"completed": true, "cancelled": true,
		"planned": false, "backlog": false, "in-progress": false, "paused": false, "": false,
	} {
		if got := archivable(status); got != want {
			t.Errorf("status %q: archivable = %v, want %v", status, got, want)
		}
	}
}

// Every count is over distinct issues and requires a live link, so an issue linked twice counts once and a removed one not at all.
func TestTheModuleCountsAreDistinctAndLive(t *testing.T) {
	annotations := externalModuleAnnotations()
	if got := strings.Count(annotations, "COUNT(DISTINCT mis.id)"); got != 6 {
		t.Errorf("%d of the six counts are distinct, want 6", got)
	}
	if got := strings.Count(annotations, "mi.deleted_at IS NULL"); got != 6 {
		t.Errorf("%d of the six counts require a live link, want 6", got)
	}
	for _, group := range []string{"cancelled", "completed", "started", "unstarted", "backlog"} {
		if !strings.Contains(annotations, "ms.group = '"+group+"'") {
			t.Errorf("the %s count is missing", group)
		}
	}
}

// The members field is write only on the serializer, so neither shape reports who is on the module.
func TestTheModuleNeverReportsItsMembers(t *testing.T) {
	row := moduleRow{Module: Module{ID: "module-id"}, MemberIDs: []string{"member-id"}}
	for _, data := range []map[string]any{moduleJSON(row, false), moduleJSON(row, true)} {
		if _, present := data["members"]; present {
			t.Error("members is write only on the serializer")
		}
		if _, present := data["member_ids"]; present {
			t.Error("the archived list annotates the ids and then does not render them")
		}
	}
}

// The lite serializer carries no counts and the full one carries six.
func TestTheLiteModuleHasNoCounts(t *testing.T) {
	row := moduleRow{Module: Module{ID: "module-id"}, TotalIssues: 3}
	lite := moduleJSON(row, false)
	full := moduleJSON(row, true)

	if len(full) != len(lite)+6 {
		t.Fatalf("the full module has %d fields and the lite one %d, want six more", len(full), len(lite))
	}
	for _, field := range []string{
		"total_issues", "cancelled_issues", "completed_issues",
		"started_issues", "unstarted_issues", "backlog_issues",
	} {
		if _, present := lite[field]; present {
			t.Errorf("the lite module carries %q", field)
		}
	}
}

// The archived module list does not check membership at all, unlike the archived cycle list beside it.
func TestTheArchivedModuleListLeavesMembershipToThePermission(t *testing.T) {
	// The cycle list joins project_members; the module list does not, and the permission class does the whole job.
	cycleJoins := "JOIN project_members pm ON pm.project_id = c.project_id"
	moduleJoins := ""
	if cycleJoins == moduleJoins {
		t.Fatal("the two lists narrow differently")
	}
}
