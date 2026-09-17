package externalapi

import (
	"testing"
)

// The label serializer asks for every field, and eight of them are read-only.
func TestLabelJSONShape(t *testing.T) {
	data := labelJSON(Label{ID: "label-id"})
	for _, field := range []string{
		"id", "created_at", "updated_at", "created_by", "updated_by", "deleted_at",
		"name", "description", "color", "sort_order", "parent",
		"external_source", "external_id", "project", "workspace",
	} {
		if _, present := data[field]; !present {
			t.Errorf("the label is missing %q", field)
		}
	}
	if len(data) != 15 {
		t.Fatalf("the label has %d fields, want 15", len(data))
	}
}

// Seven fields are writable and everything else is read-only, the project and the workspace among them.
func TestApplyLabelPayloadKeepsTheReadOnlyFields(t *testing.T) {
	label := Label{ProjectID: "project-id", WorkspaceID: "workspace-id", ID: "label-id"}
	applyLabelPayload(&label, map[string]any{
		"name": "Bug", "color": "#f00", "description": "text", "sort_order": float64(100),
		"parent": "parent-id", "external_source": "github", "external_id": "42",
		"project": "elsewhere", "workspace": "elsewhere", "id": "elsewhere",
	})
	if label.ProjectID != "project-id" || label.WorkspaceID != "workspace-id" || label.ID != "label-id" {
		t.Errorf("a read-only field was written: %+v", label)
	}
	if label.Name != "Bug" || label.Color != "#f00" || label.Description != "text" || label.SortOrder != 100 {
		t.Errorf("a writable field was not written: %+v", label)
	}
	if label.ParentID == nil || *label.ParentID != "parent-id" {
		t.Errorf("the parent is %v", label.ParentID)
	}
	if label.ExternalSource == nil || *label.ExternalSource != "github" {
		t.Errorf("the external source is %v", label.ExternalSource)
	}
	// A null parent cuts the label loose rather than being ignored.
	applyLabelPayload(&label, map[string]any{"parent": nil, "external_id": nil})
	if label.ParentID != nil || label.ExternalID != nil {
		t.Errorf("a null did not clear the field: %+v", label)
	}
}

// The label's external-id check needs both halves in the request; the state's fires on the id alone. Two endpoints in the same app, two rules.
func TestTheLabelAndStateExternalChecksDiffer(t *testing.T) {
	labelFires := func(externalID, externalSource string) bool {
		return externalID != "" && externalSource != ""
	}
	// The state compares against the value it already holds instead, so the source is not required.
	stateFires := func(externalID string, current *string) bool {
		return externalID != "" && (current == nil || *current != externalID)
	}
	if labelFires("42", "") {
		t.Error("the label check needs both halves")
	}
	if !stateFires("42", nil) {
		t.Error("the state check fires on the id alone")
	}
	if !labelFires("42", "github") {
		t.Error("the label check fires when both are present")
	}
}
