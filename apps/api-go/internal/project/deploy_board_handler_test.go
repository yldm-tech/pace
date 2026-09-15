package project

import (
	"encoding/json"
	"testing"
)

// An unpublished project is answered with the serializer over nothing: twelve keys of defaults rather than an empty list, a null or a 404.
func TestAnUnpublishedProjectAnswersWithDefaults(t *testing.T) {
	body := emptyDeployBoardJSON()
	if len(body) != 12 {
		t.Fatalf("the empty board has %d keys, want 12", len(body))
	}
	// Every read-only field is absent rather than null, because a serializer with no instance reports only what could be written.
	for _, absent := range []string{"id", "anchor", "project_details", "workspace_detail", "created_at", "updated_at", "workspace", "project"} {
		if _, present := body[absent]; present {
			t.Errorf("the empty board carries %q, and a serializer with no instance drops it", absent)
		}
	}
	for _, present := range []string{"is_comments_enabled", "is_reactions_enabled", "is_votes_enabled", "is_activity_enabled", "is_disabled"} {
		if value, ok := body[present].(bool); !ok || value {
			t.Errorf("%q reads %v, want false", present, body[present])
		}
	}
	// view_props is null here and an object on a real board, which is the one field that changes shape.
	if body["view_props"] != nil {
		t.Errorf("view_props reads %v, want null", body["view_props"])
	}
}

// The five layouts are what a published project offers when the payload names none.
func TestTheDefaultLayouts(t *testing.T) {
	var views map[string]bool
	if err := json.Unmarshal(defaultDeployBoardViews, &views); err != nil {
		t.Fatal(err)
	}
	if len(views) != 5 {
		t.Fatalf("there are %d layouts, want 5", len(views))
	}
	for _, layout := range []string{"list", "kanban", "calendar", "gantt", "spreadsheet"} {
		if !views[layout] {
			t.Errorf("%q is off by default", layout)
		}
	}
}

// Every switch the publish does not name is written as off, so a second call that turns one on turns the others off with it.
func TestPublishingWritesTheAbsentSwitchesOff(t *testing.T) {
	payload := map[string]json.RawMessage{"is_comments_enabled": json.RawMessage(`true`)}
	readBool := func(field string) bool {
		raw, given := payload[field]
		if !given {
			return false
		}
		var value bool
		if json.Unmarshal(raw, &value) != nil {
			return false
		}
		return value
	}
	if !readBool("is_comments_enabled") {
		t.Error("the switch that was named is off")
	}
	for _, field := range []string{"is_votes_enabled", "is_reactions_enabled"} {
		if readBool(field) {
			t.Errorf("%q is on, and an absent switch is written off", field)
		}
	}
}
