package issues

import (
	"os"
	"strconv"
	"strings"
	"testing"
)

// The advisory lock key has to match Django's exactly. During the transition a create can arrive at either side, and a lock the two compute differently is no lock at all — two work items would take the same sequence number.
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
		if got := AdvisoryLockKey(projectID); got != want {
			t.Errorf("AdvisoryLockKey(%s) = %d, want %d", projectID, got, want)
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
