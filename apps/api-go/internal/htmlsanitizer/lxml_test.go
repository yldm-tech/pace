package htmlsanitizer

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// TestRoundTripMatchesLxml diffs the Go round-trip against the markup lxml really writes back. The fixture is generated, which is what pins the behaviours the Python does not show: the wrapper depends on whether a block tag is anywhere in the fragment, markup that parses to nothing is refused rather than emptied, and the whitespace after a single root survives while the whitespace in front of it does not.
func TestRoundTripMatchesLxml(t *testing.T) {
	fixture, err := os.ReadFile("testdata/lxml_fragment.tsv")
	if err != nil {
		t.Fatal(err)
	}
	rows := 0
	for _, line := range strings.Split(string(fixture), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) != 3 {
			t.Fatalf("malformed fixture row %q", line)
		}
		var input, want string
		if err := json.Unmarshal([]byte(parts[0]), &input); err != nil {
			t.Fatalf("row %q: %v", line, err)
		}
		if err := json.Unmarshal([]byte(parts[2]), &want); err != nil {
			t.Fatalf("row %q: %v", line, err)
		}
		rows++
		got, err := RoundTrip(input)
		if parts[1] == "ERROR" {
			if err == nil {
				t.Errorf("%q round-trips to %q, but lxml refuses it", input, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q: %v", input, err)
			continue
		}
		if got != want {
			t.Errorf("%q:\n got  %q\n want %q", input, got, want)
		}
	}
	if rows < 60 {
		t.Fatalf("the fixture has %d rows, which is fewer than it was generated with", rows)
	}
}

// An empty description is refused rather than stored, which is the one behaviour here a caller meets by accident.
func TestAnEmptyDescriptionIsRefused(t *testing.T) {
	for _, input := range []string{"", "   ", "\n\t", "<!-- nothing else -->"} {
		if _, err := RoundTrip(input); err == nil {
			t.Errorf("%q is accepted, and lxml refuses it", input)
		}
	}
}
