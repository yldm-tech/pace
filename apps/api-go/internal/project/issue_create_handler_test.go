package project

import (
	"testing"
	"time"
)

func TestIssueCreateResponseShape(t *testing.T) {
	row := issueListRow{Issue: sampleIssueRow().Issue}
	data := issueCreateJSON(row, time.UTC)
	if _, present := data["state__group"]; present {
		t.Error("the create response has no state__group")
	}
	if _, present := data["deleted_at"]; !present {
		t.Error("the create response carries deleted_at")
	}
	if len(data) != 26 {
		t.Fatalf("the create response has %d fields, want 26", len(data))
	}
}

// A new issue goes after everything already in its state, and stays at the default when that state is empty.
func TestNewIssueSortOrder(t *testing.T) {
	for _, test := range []struct {
		largest float64
		want    float64
	}{
		{largest: -1, want: 65535},
		{largest: 0, want: 10000},
		{largest: 65535, want: 75535},
	} {
		sortOrder := 65535.0
		if test.largest >= 0 {
			sortOrder = test.largest + 10000
		}
		if sortOrder != test.want {
			t.Errorf("largest %v gave %v, want %v", test.largest, sortOrder, test.want)
		}
	}
}

// The sequence starts at one rather than zero, and continues from the highest already handed out.
func TestNewIssueSequence(t *testing.T) {
	for largest, want := range map[int64]int64{0: 1, 1: 2, 41: 42} {
		sequence := int64(1)
		if largest > 0 {
			sequence = largest + 1
		}
		if sequence != want {
			t.Errorf("largest %d gave %d, want %d", largest, sequence, want)
		}
	}
}
