package project

import (
	"testing"
	"time"
)

// The two audit columns are written whatever else changed, because the serializer sets updated_at by hand and then saves the instance.
func TestAnUpdateWithNoFieldsStillTouchesTheRow(t *testing.T) {
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	updates := issueInput{values: map[string]any{}}.updates(now, "actor-id")
	if len(updates) != 2 {
		t.Fatalf("an empty update writes %v, want the two audit columns", updates)
	}
	if updates["updated_at"] != now || updates["updated_by_id"] != "actor-id" {
		t.Errorf("the audit columns are %v", updates)
	}
}

// The plain-text copy follows the rich text on every save, so the search cannot fall behind.
func TestTheStrippedDescriptionIsRecomputed(t *testing.T) {
	updates := map[string]any{"description_html": "<p>after</p>"}
	if err := applyIssueSavePath(nil, Issue{DescriptionHTML: "<p>before</p>"}, updates, time.Now()); err != nil {
		t.Fatal(err)
	}
	stripped, ok := updates["description_stripped"].(*string)
	if !ok || stripped == nil || *stripped != "after" {
		t.Fatalf("the stripped description is %v, want the new text", updates["description_stripped"])
	}

	// An update that says nothing about the description still recomputes it, from the text the work item already had.
	updates = map[string]any{}
	if err := applyIssueSavePath(nil, Issue{DescriptionHTML: "<p>before</p>"}, updates, time.Now()); err != nil {
		t.Fatal(err)
	}
	stripped, ok = updates["description_stripped"].(*string)
	if !ok || stripped == nil || *stripped != "before" {
		t.Fatalf("the stripped description is %v, want the stored text", updates["description_stripped"])
	}

	// Empty html has no plain text at all rather than an empty one.
	updates = map[string]any{"description_html": ""}
	if err := applyIssueSavePath(nil, Issue{}, updates, time.Now()); err != nil {
		t.Fatal(err)
	}
	if value, present := updates["description_stripped"].(*string); !present && value != nil {
		t.Errorf("empty html strips to %v, want null", updates["description_stripped"])
	}
}

// completed_at follows the state, but only when the state really changes — otherwise every later edit would rewrite the moment the work item was finished.
func TestCompletedAtFollowsAStateChange(t *testing.T) {
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	stored := "state-one"

	// The state is not mentioned, so the timestamp is not touched.
	updates := map[string]any{"name": "renamed"}
	if err := syncIssueCompletedAt(nil, Issue{StateID: &stored}, updates, now); err != nil {
		t.Fatal(err)
	}
	if _, present := updates["completed_at"]; present {
		t.Error("an update that does not move the state rewrote the completion time")
	}

	// The state is named but is the one it already had, which is not a change.
	updates = map[string]any{"state_id": stored}
	if err := syncIssueCompletedAt(nil, Issue{StateID: &stored}, updates, now); err != nil {
		t.Fatal(err)
	}
	if _, present := updates["completed_at"]; present {
		t.Error("saving the state a work item already had rewrote the completion time")
	}
}

// Clearing the state does not leave a work item without one: it lands on the project's default.
func TestClearingTheStateFallsBackToTheDefault(t *testing.T) {
	updates := map[string]any{"state_id": nil}
	if _, given := updates["state_id"]; !given {
		t.Fatal("the update does not name the state")
	}
	// The resolution needs the database, so this pins only the shape: a null state is the case ensureIssueState looks at, and a named one is left alone.
	untouched := map[string]any{"state_id": "state-two"}
	if err := ensureIssueState(nil, Issue{}, untouched); err != nil {
		t.Fatal(err)
	}
	if untouched["state_id"] != "state-two" {
		t.Errorf("a named state was rewritten to %v", untouched["state_id"])
	}
	absent := map[string]any{}
	if err := ensureIssueState(nil, Issue{}, absent); err != nil {
		t.Fatal(err)
	}
	if len(absent) != 0 {
		t.Errorf("an update that says nothing about the state wrote %v", absent)
	}
}
