package drf

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// TestISO8601MatchesDRF diffs the rendering against output captured from DRF itself. The fixture is generated, because the rule it pins is not the one Go's own encoder follows: the fraction is always six digits when present and absent when zero, where Go trims trailing zeros.
func TestISO8601MatchesDRF(t *testing.T) {
	fixture, err := os.ReadFile("testdata/iso8601.tsv")
	if err != nil {
		t.Fatal(err)
	}
	rows := 0
	for _, line := range strings.Split(string(fixture), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		input, want, found := strings.Cut(line, "\t")
		if !found {
			t.Fatalf("malformed fixture row %q", line)
		}
		rows++
		parsed, err := time.Parse(time.RFC3339Nano, input)
		if err != nil {
			t.Fatalf("parse %q: %v", input, err)
		}
		if got := ISO8601(parsed); got != want {
			t.Errorf("ISO8601(%s) = %s, want %s", input, got, want)
		}
		encoded, err := json.Marshal(Time(parsed))
		if err != nil {
			t.Fatal(err)
		}
		if string(encoded) != `"`+want+`"` {
			t.Errorf("json.Marshal(%s) = %s, want %q", input, encoded, want)
		}
	}
	if rows != 24 {
		t.Fatalf("fixture has %d rows, want 24", rows)
	}
}

// The divergence this type exists to fix, named directly.
func TestTrailingZeroMicrosecondsSurviveTheRendering(t *testing.T) {
	instant := time.Date(2026, 1, 2, 3, 4, 5, 120000000, time.UTC)
	standard, err := json.Marshal(instant)
	if err != nil {
		t.Fatal(err)
	}
	if string(standard) != `"2026-01-02T03:04:05.12Z"` {
		t.Fatalf("Go's own encoding changed to %s; re-check whether this type is still needed", standard)
	}
	if got := ISO8601(instant); got != "2026-01-02T03:04:05.120000Z" {
		t.Fatalf("ISO8601 = %s, want the six-digit fraction", got)
	}
}

// Postgres stores microseconds, but a value carrying sub-microsecond precision must still render six digits rather than nine.
func TestSubMicrosecondPrecisionIsTruncatedToSixDigits(t *testing.T) {
	instant := time.Date(2026, 1, 2, 3, 4, 5, 123456789, time.UTC)
	if got := ISO8601(instant); got != "2026-01-02T03:04:05.123456Z" {
		t.Fatalf("ISO8601 = %s, want six digits", got)
	}
}

func TestNullableTimesStayNull(t *testing.T) {
	if At(nil) != nil {
		t.Fatal("a null column must render as null")
	}
	instant := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	encoded, err := json.Marshal(map[string]any{"at": At(&instant)})
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `{"at":"2026-01-02T03:04:05Z"}` {
		t.Fatalf("encoded = %s", encoded)
	}
}

// A zero fraction is omitted entirely rather than written as six zeros.
func TestWholeSecondsCarryNoFraction(t *testing.T) {
	instant := time.Date(2026, 1, 2, 3, 4, 5, 0, time.FixedZone("", 8*3600))
	if got := ISO8601(instant); got != "2026-01-02T03:04:05+08:00" {
		t.Fatalf("ISO8601 = %s", got)
	}
}
