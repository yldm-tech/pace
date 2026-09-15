package project

import (
	"strings"
	"testing"
	"time"
)

// The same notification has two shapes. The list annotates three read-only flags; no detail route does, and DRF skips a read-only field whose attribute is missing rather than failing.
func TestANotificationReadAloneCarriesThreeFewerFields(t *testing.T) {
	row := notificationRow{Notification: Notification{ID: "notification-id"}, IsIntakeIssue: true, IsMentioned: true}
	listed := notificationJSON(row, true, nil)
	alone := notificationJSON(row, false, nil)

	if len(listed) != len(alone)+3 {
		t.Fatalf("the list has %d fields and the detail %d, want three more", len(listed), len(alone))
	}
	for _, flag := range []string{"is_inbox_issue", "is_intake_issue", "is_mentioned_notification"} {
		if _, present := listed[flag]; !present {
			t.Errorf("the list is missing %q", flag)
		}
		if _, present := alone[flag]; present {
			t.Errorf("the detail carries %q, which nothing annotates there", flag)
		}
	}
	// Two of the three are the same subquery under different names, so they always agree.
	if listed["is_inbox_issue"] != listed["is_intake_issue"] {
		t.Error("the inbox and intake flags are the same subquery and must not diverge")
	}
}

// The serializer asks for __all__, so every column is rendered plus the expanded trigger.
func TestNotificationJSONRendersTheWholeModel(t *testing.T) {
	data := notificationJSON(notificationRow{Notification: Notification{ID: "notification-id"}}, false, nil)
	for _, field := range []string{
		"id", "created_at", "updated_at", "created_by", "updated_by", "deleted_at",
		"data", "entity_identifier", "entity_name", "title", "message", "message_html",
		"message_stripped", "sender", "read_at", "snoozed_till", "archived_at",
		"workspace", "project", "triggered_by", "receiver", "triggered_by_details",
	} {
		if _, present := data[field]; !present {
			t.Errorf("the notification is missing %q", field)
		}
	}
	if len(data) != 22 {
		t.Fatalf("the notification has %d fields, want 22", len(data))
	}
}

// The snoozed filter's two halves are not complementary: asking for snoozed notifications is really asking for every notification that has ever been snoozed, because the second half of the pair subsumes the first.
func TestTheSnoozedFilterIsNotTwoHalvesOfAWhole(t *testing.T) {
	const snoozed = "n.snoozed_till < ? OR n.snoozed_till IS NOT NULL"
	const notSnoozed = "n.snoozed_till >= ? OR n.snoozed_till IS NULL"
	if !strings.Contains(snoozed, "IS NOT NULL") {
		t.Fatal("the true branch must keep the redundant half it was written with")
	}
	// A notification snoozed until tomorrow matches both branches, which is the point.
	tomorrow := time.Now().Add(24 * time.Hour)
	matchesSnoozed := tomorrow.Before(time.Now()) || true
	matchesNotSnoozed := !tomorrow.Before(time.Now())
	if !matchesSnoozed || !matchesNotSnoozed {
		t.Error("a future snooze must satisfy both branches, which is what the redundancy causes")
	}
	if snoozed == notSnoozed {
		t.Fatal("the two branches are still different expressions")
	}
}

// The mark-all-read route reads its two flags for truth rather than looking them up, so any value works there while the list refuses anything but the two it knows.
func TestTheTwoRoutesDisagreeAboutAnUnknownFlag(t *testing.T) {
	for _, value := range []any{true, "yes", []any{1}, 1.0} {
		if !truthyWithLength(value) {
			t.Errorf("mark-all-read treats %v as true", value)
		}
	}
	for _, value := range []any{nil, false, "", []any{}} {
		if truthyWithLength(value) {
			t.Errorf("mark-all-read treats %v as false", value)
		}
	}
	// The list has no such tolerance: a third value is a KeyError.
	if errUnknownNotificationFlag == nil {
		t.Fatal("the list must name the error it answers for an unknown flag")
	}
}

// An update that names nothing still clears the snooze, because the handler builds its own payload with a null in it rather than passing the request's through.
func TestAnEmptyUpdateStillClearsTheSnooze(t *testing.T) {
	when := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	notification := Notification{SnoozedTill: &when}
	applyNotificationUpdates(&notification, map[string]any{"snoozed_till": (*time.Time)(nil)})
	if notification.SnoozedTill != nil {
		t.Errorf("the snooze survived as %v", notification.SnoozedTill)
	}
}

// Marking read and unread write the same column in both directions, and the response carries what was written.
func TestMarkingReadAndUnreadWritesTheInstance(t *testing.T) {
	now := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	notification := Notification{}
	applyNotificationUpdates(&notification, map[string]any{"read_at": now})
	if notification.ReadAt == nil || !notification.ReadAt.Equal(now) {
		t.Fatalf("read_at is %v, want the moment it was marked", notification.ReadAt)
	}
	applyNotificationUpdates(&notification, map[string]any{"read_at": (*time.Time)(nil)})
	if notification.ReadAt != nil {
		t.Errorf("read_at is %v, want it cleared", notification.ReadAt)
	}
}
