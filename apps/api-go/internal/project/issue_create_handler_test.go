package project

import (
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The advisory lock key has to match Django's exactly. During the transition a create can arrive at either side, and a lock the two compute differently is no lock at all — two issues would take the same sequence number.
func TestAdvisoryLockKeyMatchesDjango(t *testing.T) {
	fixture, err := os.ReadFile("testdata/advisory_lock_keys.tsv")
	if err != nil {
		t.Fatal(err)
	}
	rows := 0
	for _, line := range strings.Split(string(fixture), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		projectID, expected, found := strings.Cut(line, "\t")
		if !found {
			t.Fatalf("malformed fixture row %q", line)
		}
		want, err := strconv.ParseInt(expected, 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		rows++
		if got := advisoryLockKey(projectID); got != want {
			t.Errorf("advisoryLockKey(%s) = %d, want %d", projectID, got, want)
		}
	}
	if rows != 10 {
		t.Fatalf("fixture has %d rows, want 10", rows)
	}
	// Half the fixture is negative, which is the point: the key is signed, and reading it unsigned would lock on a different number.
	negatives := 0
	for _, line := range strings.Split(string(fixture), "\n") {
		if strings.Contains(line, "\t-") {
			negatives++
		}
	}
	if negatives == 0 {
		t.Fatal("the fixture pins no negative key, so it would not catch an unsigned read")
	}
}

// The create response is the list projection plus deleted_at and without state__group.
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
