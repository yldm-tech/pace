package project

import (
	"testing"
	"time"
)

// The preference row reports fourteen fields, five of which are the switches themselves.
func TestTheNotificationPreferenceShape(t *testing.T) {
	data := notificationPreferenceJSON(UserNotificationPreference{})
	if len(data) != 14 {
		t.Fatalf("the preference has %d fields, want 14", len(data))
	}
	switches := 0
	for _, key := range []string{"property_change", "state_change", "comment", "mention", "issue_completed"} {
		if _, present := data[key]; !present {
			t.Errorf("the preference is missing %q", key)
		} else {
			switches++
		}
	}
	if switches != 5 {
		t.Fatalf("there are %d switches, want 5", switches)
	}
	// The row says which workspace and project it is for, and both are empty on a person's own.
	for _, key := range []string{"workspace", "project", "user"} {
		if _, present := data[key]; !present {
			t.Errorf("the preference is missing %q", key)
		}
	}
}

// The dashboard compares target dates against the calendar week of the year rather than a week of the month.
func TestTheDueWeekIsTheCalendarWeek(t *testing.T) {
	// 2026-01-01 is a Thursday, which the ISO calendar puts in week one.
	if got := isoWeek(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)); got != 1 {
		t.Errorf("the first of January is week %d", got)
	}
	if got := isoWeek(time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)); got != 38 {
		t.Errorf("the sixteenth of September is week %d", got)
	}
}
