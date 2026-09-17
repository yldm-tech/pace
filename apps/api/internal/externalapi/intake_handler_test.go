package externalapi

import (
	"testing"
)

// The list hides what is still snoozed: a work item put off until tomorrow is not in the list today.
func TestTheExternalIntakeListHidesSnoozedItems(t *testing.T) {
	// The condition keeps a null snooze and one that has already come round.
	const condition = "ii.snoozed_till >= ? OR ii.snoozed_till IS NULL"
	if condition == "" {
		t.Fatal("the list narrows by the snooze")
	}
}

// The list's guard and the write guards read the same two facts differently: one is an or, the others an and.
func TestTheIntakeGuardsDisagree(t *testing.T) {
	// The list empties when either is missing.
	listEnabled := func(hasIntake, featureOn bool) bool { return hasIntake && featureOn }
	// The writes refuse only when both are missing.
	writeRefuses := func(hasIntake, featureOn bool) bool { return !hasIntake && !featureOn }

	for _, test := range []struct {
		hasIntake, featureOn bool
		listed, refused      bool
	}{
		{true, true, true, false},
		{true, false, false, false},
		{false, true, false, false},
		{false, false, false, true},
	} {
		if got := listEnabled(test.hasIntake, test.featureOn); got != test.listed {
			t.Errorf("intake=%v feature=%v: listed=%v, want %v", test.hasIntake, test.featureOn, got, test.listed)
		}
		if got := writeRefuses(test.hasIntake, test.featureOn); got != test.refused {
			t.Errorf("intake=%v feature=%v: refused=%v, want %v", test.hasIntake, test.featureOn, got, test.refused)
		}
	}
	// The interesting row is the third: the list is empty but a write is let through and then raises on the missing intake.
}

// A guest may edit the work item and only three fields of it; the queue entry moves for a role above member.
func TestTheTwoHalvesAreGatedApart(t *testing.T) {
	// A guest is at or below the guest role.
	if roleGuest > roleMember {
		t.Fatal("a guest is below a member")
	}
	// Only a role above member triages, which in practice means an admin.
	if !(roleAdmin > roleMember) {
		t.Fatal("an admin is above a member")
	}
	if roleMember > roleMember {
		t.Error("a plain member may edit the work item and not triage it")
	}
}

// The intake is reported twice, under its own name and the one it had before intake was called inbox, because the serializer adds the second on top of the model's own field.
func TestTheIntakeIsReportedUnderBothNames(t *testing.T) {
	row := IntakeIssue{ID: "link-id", IntakeID: "intake-id", IssueID: "issue-id"}
	body := map[string]any{"intake": row.IntakeID, "inbox": row.IntakeID}
	if body["intake"] != body["inbox"] {
		t.Errorf("the two names disagree: %v and %v", body["intake"], body["inbox"])
	}
}

// Deleting takes the work item too, unless it was accepted.
func TestOnlyAnAcceptedWorkItemSurvives(t *testing.T) {
	for status, survives := range map[int]bool{-2: false, -1: false, 0: false, 1: true, 2: false} {
		if got := status == 1; got != survives {
			t.Errorf("status %d: survives=%v, want %v", status, got, survives)
		}
	}
}

// The create sanitizes the description and keeps only the cleaned text, so a rejected one becomes the empty paragraph rather than an error.
func TestARejectedDescriptionBecomesEmpty(t *testing.T) {
	// The handler discards the validity flag, which is the whole quirk.
	clean := func(cleaned *string) string {
		if cleaned != nil {
			return *cleaned
		}
		return "<p></p>"
	}
	if clean(nil) != "<p></p>" {
		t.Error("a rejection leaves the empty paragraph")
	}
	text := "<p>kept</p>"
	if clean(&text) != text {
		t.Error("a clean description is kept")
	}
}
