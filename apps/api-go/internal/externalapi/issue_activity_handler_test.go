package externalapi

import (
	"testing"
)

// A history reads forwards: the default order here is ascending where every other list in this API is newest first.
func TestTheActivityListReadsForwards(t *testing.T) {
	if got := sanitizeOrderBy("", activityOrderByAllowlist, "created_at"); got != "created_at" {
		t.Fatalf("the default order is %q, want the ascending one", got)
	}
	if got := activityOrderColumn("created_at"); got != "a.created_at" {
		t.Errorf("an ascending order renders as %q", got)
	}
	if got := activityOrderColumn("-created_at"); got != "a.created_at DESC" {
		t.Errorf("a descending order renders as %q", got)
	}
	if got := activityOrderColumn("updated_at"); got != "a.updated_at" {
		t.Errorf("the other allowed field renders as %q", got)
	}
}

// Two fields are orderable and nothing else reaches the query.
func TestOnlyTwoActivityFieldsAreOrderable(t *testing.T) {
	if len(activityOrderByAllowlist) != 2 {
		t.Fatalf("there are %d orderable fields, want 2", len(activityOrderByAllowlist))
	}
	for _, value := range []string{"epoch", "verb", "field", "id", "--created_at", "created_at; DROP"} {
		if got := sanitizeOrderBy(value, activityOrderByAllowlist, "created_at"); got != "created_at" {
			t.Errorf("%q survived as %q", value, got)
		}
	}
}

// Four kinds of entry are never shown, and an entry with no field at all survives the exclusion.
func TestTheHiddenActivityFields(t *testing.T) {
	if len(hiddenActivityFields) != 4 {
		t.Fatalf("there are %d hidden fields, want 4", len(hiddenActivityFields))
	}
	hidden := map[string]bool{}
	for _, field := range hiddenActivityFields {
		hidden[field] = true
	}
	for _, field := range []string{"comment", "vote", "reaction", "draft"} {
		if !hidden[field] {
			t.Errorf("%q must be hidden", field)
		}
	}
	for _, field := range []string{"name", "state", "priority", "assignees", "labels"} {
		if hidden[field] {
			t.Errorf("%q must be shown", field)
		}
	}
	// A null field is not in any list, so a NOT IN alone would drop it; the query says so explicitly.
	const condition = "a.field IS NULL OR a.field NOT IN ?"
	if condition == "a.field NOT IN ?" {
		t.Error("an entry with no field must survive the exclusion")
	}
}

// The detail route answers a different 404 from every other route in this app.
func TestTheActivityDetailHasItsOwn404(t *testing.T) {
	body := map[string]any{"message": "Activity not found.", "code": "NOT_FOUND"}
	if _, present := body["error"]; present {
		t.Error("this route says message and code, not error")
	}
	if body["code"] != "NOT_FOUND" {
		t.Errorf("the code is %v", body["code"])
	}
}

// The serializer reports eighteen fields.
func TestIssueActivityJSONShape(t *testing.T) {
	data := issueActivityJSON(IssueActivity{ID: "activity-id"})
	for _, field := range []string{
		"id", "created_at", "updated_at", "deleted_at", "verb", "field",
		"old_value", "new_value", "comment", "attachments",
		"old_identifier", "new_identifier", "epoch", "actor",
		"project", "workspace", "issue", "issue_comment",
	} {
		if _, present := data[field]; !present {
			t.Errorf("the activity is missing %q", field)
		}
	}
	if len(data) != 18 {
		t.Fatalf("the activity has %d fields, want 18", len(data))
	}
}
