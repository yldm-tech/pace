package project

import (
	"testing"
	"time"
)

// convert_to_utc turns a start date into the first second of that day in the project's timezone, and an end date into 23:59 of it. The extra second and the missing minute are what keep two adjacent cycles from reading as overlapping.
func TestCycleIntervalBounds(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	start, end, ok := cycleInterval("2026-06-01", "2026-06-30", "Asia/Shanghai", now)
	if !ok {
		t.Fatal("the interval should have been read")
	}
	shanghai, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	if got := start.In(shanghai).Format("2006-01-02 15:04:05"); got != "2026-06-01 00:00:01" {
		t.Fatalf("start = %s", got)
	}
	if got := end.In(shanghai).Format("2006-01-02 15:04:05"); got != "2026-06-30 23:59:00" {
		t.Fatalf("end = %s", got)
	}
	// Both come back as instants, so they compare against a timestamptz column directly.
	if start.Location() != time.UTC || end.Location() != time.UTC {
		t.Fatal("the bounds should be in UTC")
	}
}

// A start date that falls on today in the project's timezone becomes the current instant rather than the start of the day, so a cycle created this afternoon does not claim to have begun this morning.
func TestAStartDateOfTodayBecomesNow(t *testing.T) {
	shanghai, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	// Half past two in the afternoon, Shanghai time.
	now := time.Date(2026, 6, 1, 14, 30, 0, 0, shanghai).UTC()
	start, _, ok := cycleInterval("2026-06-01", "2026-06-30", "Asia/Shanghai", now)
	if !ok {
		t.Fatal("the interval should have been read")
	}
	if !start.Equal(now) {
		t.Fatalf("start = %s, want the current instant", start)
	}
	// A day either side is not today, so it keeps the first second.
	earlier, _, _ := cycleInterval("2026-05-31", "2026-06-30", "Asia/Shanghai", now)
	if earlier.Equal(now) {
		t.Fatal("yesterday is not today")
	}
	if got := earlier.In(shanghai).Format("15:04:05"); got != "00:00:01" {
		t.Fatalf("yesterday's start = %s", got)
	}
}

// The comparison is by calendar day in the project's zone, not by UTC day, which matters either side of midnight.
func TestTodayIsMeasuredInTheProjectsZone(t *testing.T) {
	shanghai, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	// Late on the first in Shanghai is still the first there, but already the first in UTC too; an hour later it is the second in Shanghai and the first in UTC.
	late := time.Date(2026, 6, 1, 23, 30, 0, 0, shanghai)
	if !sameDayIn(late, late, shanghai) {
		t.Fatal("an instant is always on its own day")
	}
	nextDay := late.Add(time.Hour)
	if sameDayIn(late, nextDay, shanghai) {
		t.Fatal("half past midnight is the next day in Shanghai")
	}
	// In UTC the two are still the same day, which is exactly why the zone matters.
	if !sameDayIn(late, nextDay, time.UTC) {
		t.Fatal("in UTC both instants fall on the first")
	}
}

// A malformed date or an unknown timezone is refused rather than coerced.
func TestCycleIntervalRefusesWhatItCannotRead(t *testing.T) {
	now := time.Now().UTC()
	for _, test := range []struct{ start, end, zone string }{
		{start: "01/06/2026", end: "2026-06-30", zone: "UTC"},
		{start: "2026-06-01", end: "not-a-date", zone: "UTC"},
		{start: "2026-13-45", end: "2026-06-30", zone: "UTC"},
		{start: "2026-06-01", end: "2026-06-30", zone: "Mars/Olympus"},
	} {
		if _, _, ok := cycleInterval(test.start, test.end, test.zone, now); ok {
			t.Errorf("%v should have been refused", test)
		}
	}
}

// CycleUserPropertiesSerializer is fields = "__all__" with the four relations read-only, which renders fourteen keys.
func TestCycleUserPropertiesShape(t *testing.T) {
	data := cycleUserPropertiesJSON(CycleUserProperties{
		ID: "properties-id", ProjectID: "project-id", WorkspaceID: "workspace-id",
		CycleID: "cycle-id", UserID: "user-id",
		Filters: defaultFiltersJSON(), DisplayFilters: defaultDisplayFiltersJSON(),
		DisplayProperties: defaultDisplayPropertiesJSON(), RichFilters: emptyJSON(),
	})
	for _, field := range []string{
		"id", "created_at", "updated_at", "created_by", "updated_by", "deleted_at",
		"filters", "display_filters", "display_properties", "rich_filters",
		"project", "workspace", "cycle", "user",
	} {
		if _, present := data[field]; !present {
			t.Errorf("the cycle properties are missing %q", field)
		}
	}
	if len(data) != 14 {
		t.Fatalf("the cycle properties have %d fields, want 14", len(data))
	}
}
