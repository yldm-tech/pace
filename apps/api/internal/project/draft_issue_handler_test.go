package project

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// A draft's list shape is the detail shape, which is why one projection serves the list, the read and the create.
func TestTheDraftProjectionIsTwentyOneFields(t *testing.T) {
	data := draftIssueJSON(draftIssueRow{})
	if len(data) != 21 {
		t.Fatalf("a draft has %d fields, want 21", len(data))
	}
	for _, key := range []string{"id", "name", "state_id", "sort_order", "completed_at", "estimate_point",
		"priority", "start_date", "target_date", "project_id", "parent_id", "cycle_id", "module_ids",
		"label_ids", "assignee_ids", "created_at", "updated_at", "created_by", "updated_by", "type_id",
		"description_html"} {
		if _, present := data[key]; !present {
			t.Errorf("the projection is missing %q", key)
		}
	}
	// The three id arrays are arrays even when the draft has none of any.
	for _, key := range []string{"module_ids", "label_ids", "assignee_ids"} {
		list, ok := data[key].([]string)
		if !ok || list == nil {
			t.Errorf("%q is %#v, want an empty array", key, data[key])
		}
	}
	// A draft carries no sequence number and no archived_at, because neither means anything until it is raised.
	for _, key := range []string{"sequence_id", "archived_at", "is_draft", "state__group"} {
		if _, present := data[key]; present {
			t.Errorf("the projection carries %q, which belongs to a work item", key)
		}
	}
}

// Only the lookups that land on a draft's own columns are translatable. Everything else reaches a relation name that belongs to Issue, which the ORM refuses.
func TestOnlyADraftsOwnColumnsCanBeFilteredOn(t *testing.T) {
	today := time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)
	for name, params := range map[string]map[string]string{
		"priority":   {"priority": "high"},
		"state":      {"state": "11111111-2222-3333-4444-555555555555"},
		"created_by": {"created_by": "11111111-2222-3333-4444-555555555555"},
		"project":    {"project": "11111111-2222-3333-4444-555555555555"},
		"start_date": {"start_date": "2026-01-01;after"},
	} {
		conditions, _, ok := draftIssueFilterSQL(issueFilters(params, "GET", "", today))
		if !ok {
			t.Errorf("%s is refused, and it names a column a draft has", name)
			continue
		}
		for _, condition := range conditions {
			if strings.Contains(condition, "i.") {
				t.Errorf("%s still reads the work item table: %s", name, condition)
			}
		}
	}

	for name, params := range map[string]map[string]string{
		"labels":    {"labels": "11111111-2222-3333-4444-555555555555"},
		"assignees": {"assignees": "11111111-2222-3333-4444-555555555555"},
		"modules":   {"module": "11111111-2222-3333-4444-555555555555"},
		"cycle":     {"cycle": "11111111-2222-3333-4444-555555555555"},
		"mentions":  {"mentions": "11111111-2222-3333-4444-555555555555"},
	} {
		if _, _, ok := draftIssueFilterSQL(issueFilters(params, "GET", "", today)); ok {
			t.Errorf("%s is accepted, and the ORM raises FieldError for it", name)
		}
	}
}

// The filters that do work rewrite onto the draft table rather than the work item one.
func TestTheFilterConditionsNameTheDraftTable(t *testing.T) {
	today := time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)
	conditions, arguments, ok := draftIssueFilterSQL(issueFilters(map[string]string{"priority": "high,urgent"}, "GET", "", today))
	if !ok {
		t.Fatal("a priority filter is refused")
	}
	if len(conditions) != 1 || !strings.Contains(conditions[0], "d.priority") {
		t.Fatalf("conditions = %#v", conditions)
	}
	// One placeholder per value, since the lookup is an IN over both priorities.
	if len(arguments) != 2 {
		t.Fatalf("arguments = %#v", arguments)
	}
}

// The work item serializer answers the move, and its two id lists are echoed from the request rather than read back.
func TestTheMoveAnswersWithTheWorkItemSerializer(t *testing.T) {
	body := map[string]json.RawMessage{
		"assignee_ids": json.RawMessage(`["11111111-2222-3333-4444-555555555555"]`),
	}
	data := issueCreateSerializerJSON(Issue{ID: "issue-id"}, nil, nil, body)
	if len(data) != 36 {
		t.Fatalf("the response has %d fields, want 36", len(data))
	}
	assignees, _ := data["assignee_ids"].([]string)
	if len(assignees) != 1 {
		t.Errorf("assignee_ids = %#v, want what the request sent", data["assignee_ids"])
	}
	labels, ok := data["label_ids"].([]string)
	if !ok || len(labels) != 0 {
		t.Errorf("label_ids = %#v, want an empty array", data["label_ids"])
	}
	// Both the suffixed and the unsuffixed name are reported, because the serializer declares both.
	for _, pair := range [][2]string{{"state_id", "state"}, {"parent_id", "parent"}, {"project_id", "project"}, {"workspace_id", "workspace"}} {
		if _, present := data[pair[0]]; !present {
			t.Errorf("the response is missing %q", pair[0])
		}
		if _, present := data[pair[1]]; !present {
			t.Errorf("the response is missing %q", pair[1])
		}
	}
}
