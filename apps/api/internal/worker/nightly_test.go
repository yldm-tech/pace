package worker

import (
	"testing"
)

// The two windows are the ones the Python tasks carry: eight days for an export link, and thirty-day blocks for a project's archive and close settings.
func TestTheNightlyWindows(t *testing.T) {
	if exportLinkLifetimeDays != 8 {
		t.Errorf("an export link lives %d days", exportLinkLifetimeDays)
	}
	// archive_in and close_in are named in months and counted in thirty-day blocks, which is not the same as calendar months.
	if daysPerMonth != 30 {
		t.Errorf("a month is %d days here", daysPerMonth)
	}
}

// The bulk update writes each row once even where the activity rows are not deduplicated.
func TestTheBulkUpdateDeduplicates(t *testing.T) {
	got := uniqueIdentifiers([]string{"a", "b", "a", "c", "b"})
	if len(got) != 3 {
		t.Fatalf("deduplicated to %#v", got)
	}
	// The order the rows arrived in is kept, since it is the order the activities go out in.
	if got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Errorf("the order changed: %#v", got)
	}
	if len(uniqueIdentifiers(nil)) != 0 {
		t.Error("nothing deduplicated to something")
	}
}
