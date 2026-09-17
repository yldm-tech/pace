package project

import (
	"encoding/json"
	"testing"
	"time"
)

// The validation compares a date against whichever half the request did not supply, so moving one end cannot cross the other.
func TestDatesAreValidatedAgainstTheExistingColumn(t *testing.T) {
	existing := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	// A submitted value wins over the column.
	value, ok := dateOrExisting("2026-07-01", true, &existing)
	if !ok || value == nil || !value.Equal(time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("submitted = %v, %v", value, ok)
	}
	// An absent one falls back to the column.
	fallback, ok := dateOrExisting("", false, &existing)
	if !ok || fallback == nil || !fallback.Equal(existing) {
		t.Fatalf("fallback = %v, %v", fallback, ok)
	}
	// A null column stays null rather than becoming a zero date.
	empty, ok := dateOrExisting("", false, nil)
	if !ok || empty != nil {
		t.Fatalf("empty = %v, %v", empty, ok)
	}
	// A malformed date is not a comparison at all; Django's strptime raises and answers 500.
	if _, ok := dateOrExisting("01/07/2026", true, nil); ok {
		t.Fatal("a malformed date must not be accepted")
	}
	if _, ok := dateOrExisting("2026-13-45", true, nil); ok {
		t.Fatal("an impossible date must not be accepted")
	}
}

// Dates are ordered as dates rather than as text, which a lexicographic comparison would get wrong for a value written without padding.
func TestDatesAreOrderedAsDates(t *testing.T) {
	start, _ := dateOrExisting("2026-09-02", true, nil)
	target, _ := dateOrExisting("2026-09-10", true, nil)
	if start.After(*target) {
		t.Fatal("September the second is before the tenth")
	}
}

// A falsy value is not a change: Django tests the parsed value for truth, so an explicit null or an empty string leaves the column alone rather than clearing it.
func TestAFalsyDateIsNotAChange(t *testing.T) {
	for _, raw := range []string{`null`, `""`, ``} {
		value, given := dateFromRaw(json.RawMessage(raw))
		if raw == `""` {
			if !given || value != "" {
				t.Fatalf("an empty string is present but falsy: %q %v", value, given)
			}
			continue
		}
		if given {
			t.Errorf("%q should not count as given", raw)
		}
	}
	value, given := dateFromRaw(json.RawMessage(`"2026-09-15"`))
	if !given || value != "2026-09-15" {
		t.Fatalf("a real date gave %q %v", value, given)
	}
}

// The previous value in the activity is stringified, which for a null column is the literal None.
func TestThePreviousDateIsStringified(t *testing.T) {
	if stringifyDate(nil) != "None" {
		t.Fatalf("a null date renders as %q", stringifyDate(nil))
	}
	moment := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	if stringifyDate(&moment) != "2026-09-15" {
		t.Fatalf("a date renders as %q", stringifyDate(&moment))
	}
}
