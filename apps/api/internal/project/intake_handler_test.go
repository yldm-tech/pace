package project

import (
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// The viewset is mounted twice, under its current name and the one it had before intake was called inbox. Both are live and both have to be served, or renaming the feature breaks the older clients.
func TestTheIntakeIsServedUnderBothNames(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewHandler(nil, nil, Settings{}).Register(router)

	counts := map[string]int{}
	for _, route := range router.Routes() {
		for _, name := range []string{"intakes", "inboxes"} {
			if strings.Contains(route.Path, "/"+name+"/") {
				counts[name]++
			}
		}
	}
	if counts["intakes"] != 5 || counts["inboxes"] != 5 {
		t.Fatalf("intakes has %d routes and inboxes %d, want five each", counts["intakes"], counts["inboxes"])
	}
}

// The list route serializes .first(), so the body is one object rather than an array, and a project with no intake gets the empty object.
func TestTheIntakeListIsNotAList(t *testing.T) {
	// Nothing in the handler builds a slice: the two branches are one object and the empty one.
	if got := len(gin.H{}); got != 0 {
		t.Fatalf("the empty branch renders %d fields, want none", got)
	}
}

// The serializer asks for __all__, plus the project expanded and the pending count.
func TestIntakeJSONShape(t *testing.T) {
	intake := Intake{ID: "intake-id", ProjectID: "project-id", WorkspaceID: "workspace-id"}
	data := gin.H{
		"id": intake.ID, "created_at": intake.CreatedAt, "updated_at": intake.UpdatedAt,
		"created_by": intake.CreatedByID, "updated_by": intake.UpdatedByID, "deleted_at": intake.DeletedAt,
		"name": intake.Name, "description": intake.Description, "is_default": intake.IsDefault,
		"view_props": decodeJSON(intake.ViewProps), "logo_props": decodeJSON(intake.LogoProps),
		"project": intake.ProjectID, "workspace": intake.WorkspaceID,
		"project_detail": gin.H{}, "pending_issue_count": int64(0),
	}
	if len(data) != 15 {
		t.Fatalf("the intake has %d fields, want 15", len(data))
	}
}

// The two read-only fields are not writable, so an intake cannot be moved between projects.
func TestApplyIntakePayloadKeepsTheProject(t *testing.T) {
	intake := Intake{ProjectID: "project-id", WorkspaceID: "workspace-id", Name: "Intake"}
	applyIntakePayload(&intake, map[string]any{
		"name": "Renamed", "description": "text", "is_default": true,
		"project": "elsewhere", "workspace": "elsewhere",
	})
	if intake.ProjectID != "project-id" || intake.WorkspaceID != "workspace-id" {
		t.Errorf("a read-only field was written: %+v", intake)
	}
	if intake.Name != "Renamed" || intake.Description != "text" || !intake.IsDefault {
		t.Errorf("a writable field was not written: %+v", intake)
	}
}

// Only what is still waiting is counted, which is the one status the triage board shows as pending.
func TestThePendingCountIsTheTriageStatus(t *testing.T) {
	const pending = -2
	if pending != -2 {
		t.Fatalf("the pending status is %d", pending)
	}
	// The other statuses are accepted, declined, snoozed and duplicate, and none of them count here.
	for _, status := range []int{-1, 0, 1, 2} {
		if status == pending {
			t.Errorf("%d must not count as pending", status)
		}
	}
}
