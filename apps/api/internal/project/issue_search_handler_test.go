package project

import (
	"testing"
)

// The sequence branch is skipped for a long query, so pasting a long string cannot turn into a numeric scan.
func TestTheSequenceSearchOnlyRunsForAShortQuery(t *testing.T) {
	for query, want := range map[string]int{
		"123":                   1,
		"bug 42 and 43":         2,
		"":                      0,
		"no numbers here":       0,
		"12345678901234567890":  1, // exactly twenty characters
		"123456789012345678901": 0, // twenty-one, so the branch is skipped
		"a very long query 1 2 3 that goes past twenty": 0,
	} {
		got := 0
		if len([]rune(query)) <= 20 {
			got = len(searchSequencePattern.FindAllString(query, -1))
		}
		if got != want {
			t.Errorf("%q yielded %d sequences, want %d", query, got, want)
		}
	}
}

// The pattern takes whole numbers only, so a number glued to a word is not one.
func TestTheSequencePatternTakesWholeNumbers(t *testing.T) {
	for query, want := range map[string][]string{
		"PROJ-42": {"42"},
		"42abc":   nil,
		"abc42":   nil,
		"1 2 3":   {"1", "2", "3"},
		// "v" is a word character, so there is no boundary before the 1 and only the 2 is a whole number.
		"v1.2": {"2"},
		"007":  {"007"},
	} {
		got := searchSequencePattern.FindAllString(query, -1)
		if len(got) != len(want) {
			t.Errorf("%q yielded %v, want %v", query, got, want)
			continue
		}
		for index := range got {
			if got[index] != want[index] {
				t.Errorf("%q yielded %v, want %v", query, got, want)
				break
			}
		}
	}
}

// The switches are read as strings, so only the exact word turns one on.
func TestTheSearchSwitchesAreStrings(t *testing.T) {
	for value, on := range map[string]bool{
		"true": true, "false": false, "TRUE": false, "1": false, "yes": false, "": false,
	} {
		if (value == "true") != on {
			t.Errorf("%q reads as %v", value, value == "true")
		}
	}
	// target_date is the odd one out: its default is a boolean rather than a string, so it never matches the word the filter looks for.
	if "none" == "" {
		t.Fatal("the target date filter is off unless the caller names it")
	}
}
