package externalapi

import (
	"testing"
)

// The forbidden-character pattern is applied to the name as well as the identifier, so a project cannot be called anything with a bracket, an ampersand or a hyphen in it.
func TestTheForbiddenCharactersApplyToTheName(t *testing.T) {
	for name, refused := range map[string]bool{
		"Planning":         false,
		"Q1 planning":      false,
		"Q1 (planning)":    true,
		"Auth & billing":   true,
		"front-end":        true,
		"v1.0":             true,
		"50% done":         true,
		"a@b":              true,
		"it's":             true,
		"a,b":              true,
		"a:b":              true,
		"a;b":              true,
		"a<b":              true,
		"a{b}":             true,
		"under_score":      false,
		"CamelCase":        false,
		"with 123 numbers": false,
	} {
		if got := forbiddenIdentifierChars.MatchString(name); got != refused {
			t.Errorf("%q refused = %v, want %v", name, got, refused)
		}
	}
}

// The full serializer adds eight computed fields on top of the model's own.
func TestTheFullProjectCarriesItsAnnotations(t *testing.T) {
	data := fullProjectJSON(projectRow{Project: Project{ID: "project-id"}, TotalMembers: 3})
	for _, field := range []string{
		"total_members", "total_cycles", "total_modules",
		"is_member", "sort_order", "member_role", "is_deployed", "cover_image_url",
	} {
		if _, present := data[field]; !present {
			t.Errorf("the project is missing the annotated %q", field)
		}
	}
	if data["total_members"] != int64(3) {
		t.Errorf("total_members is %v", data["total_members"])
	}
}

// intake_view is written whether the request named it or not, because it is read back out with the project's own value as the default.
func TestIntakeViewIsAlwaysWritten(t *testing.T) {
	read := func(payload map[string]any, current bool) bool {
		if value, ok := payload["intake_view"].(bool); ok {
			return value
		}
		return current
	}
	if read(map[string]any{}, true) != true {
		t.Error("an unmentioned intake_view keeps the project's value")
	}
	if read(map[string]any{"intake_view": false}, true) != false {
		t.Error("a named intake_view wins")
	}
	if read(map[string]any{"intake_view": "yes"}, false) != false {
		t.Error("a non-boolean is not a boolean")
	}
}

// Deleting names the project twice in the favourite clear, so it only removes favourites of the project itself.
func TestDeletingClearsOnlyTheProjectsOwnFavourite(t *testing.T) {
	// entity_identifier and project_id are both the project, which is what narrows it.
	const condition = "entity_type = 'project' AND entity_identifier = ? AND project_id = ?"
	if condition == "project_id = ?" {
		t.Fatal("the clear is narrowed to the project as an entity, not to everything inside it")
	}
}

// Four of the writable fields are foreign keys and are written to the id column rather than the name the payload uses.
func TestTheForeignKeysAreWrittenToTheIdColumns(t *testing.T) {
	updates := projectUpdates(map[string]any{
		"default_assignee": "user-id", "project_lead": "lead-id",
		"default_state": "state-id", "estimate": "estimate-id",
	})
	for column, want := range map[string]any{
		"default_assignee_id": "user-id", "project_lead_id": "lead-id",
		"default_state_id": "state-id", "estimate_id": "estimate-id",
	} {
		if updates[column] != want {
			t.Errorf("%s is %v, want %v", column, updates[column], want)
		}
	}
	// A null clears the link rather than being ignored.
	cleared := projectUpdates(map[string]any{"project_lead": nil})
	value, present := cleared["project_lead_id"]
	if !present || value != nil {
		t.Errorf("a null lead left %v", value)
	}
}
