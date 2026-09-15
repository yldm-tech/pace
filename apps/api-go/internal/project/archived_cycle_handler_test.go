package project

import (
	"strings"
	"testing"
	"time"
)

// The two cycle lists project the same number of fields and not the same fields, which is the worst kind of difference to carry.
func TestTheArchivedCycleProjectionIsNotTheLiveOne(t *testing.T) {
	archived := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	row := cycleRow{Cycle: Cycle{ID: "cycle-id", ArchivedAt: &archived}}
	live := cycleListJSON(row, time.UTC)
	stale := archivedCycleListJSON(row)

	if len(live) != 22 {
		t.Fatalf("the live projection is %d fields, want 22", len(live))
	}
	if len(stale) != 23 {
		t.Fatalf("the archived projection is %d fields, want 23", len(stale))
	}
	for _, only := range []string{"logo_props", "version", "created_by"} {
		if _, present := stale[only]; present {
			t.Errorf("the archived projection carries %q, which its values() call does not list", only)
		}
	}
	for _, only := range []string{"started_issues", "unstarted_issues", "backlog_issues", "archived_at"} {
		if _, present := live[only]; present {
			t.Errorf("the live projection carries %q, which its values() call does not list", only)
		}
		if _, present := stale[only]; !present {
			t.Errorf("the archived projection is missing %q", only)
		}
	}
}

// The live list renders its two dates in the project's timezone. This endpoint hands the raw values to the JSON encoder, so nothing moves.
func TestTheArchivedCycleListMovesNoTimestamp(t *testing.T) {
	start := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	shanghai := time.FixedZone("Asia/Shanghai", 8*60*60)
	row := cycleRow{Cycle: Cycle{StartDate: &start, EndDate: &start, ArchivedAt: &start}}

	moved, ok := cycleListJSON(row, shanghai)["start_date"].(time.Time)
	if !ok {
		t.Fatal("the live list renders start_date as a time")
	}
	if _, offset := moved.Zone(); offset != 8*60*60 {
		t.Errorf("the live list renders start_date at offset %d, want the project's", offset)
	}
	for _, field := range []string{"start_date", "end_date", "archived_at"} {
		value, ok := archivedCycleListJSON(row)[field].(*time.Time)
		if !ok {
			t.Fatalf("the archived list renders %s as %T, want a time pointer", field, archivedCycleListJSON(row)[field])
		}
		if _, offset := value.Zone(); offset != 0 {
			t.Errorf("the archived list renders %s at offset %d, want UTC", field, offset)
		}
	}
}

// The three state counts carry the same four exclusions as the counts they sit next to, so an archived cycle's totals add up.
func TestTheArchivedCycleStateCountsCarryTheSameExclusions(t *testing.T) {
	annotations := archivedCycleAnnotations()
	if !strings.HasPrefix(annotations, cycleAnnotations()) {
		t.Fatal("the archived list must annotate everything the live one does")
	}
	added := annotations[len(cycleAnnotations()):]
	for _, group := range []string{"started", "unstarted", "backlog"} {
		if !strings.Contains(added, "st.group = '"+group+"'") {
			t.Errorf("the %s count is missing", group)
		}
	}
	if count := strings.Count(added, "ii.archived_at IS NULL AND ii.is_draft = FALSE"); count != 3 {
		t.Errorf("%d of the three added counts exclude archived and draft issues, want 3", count)
	}
	if count := strings.Count(added, "ci.deleted_at IS NULL AND ii.deleted_at IS NULL"); count != 3 {
		t.Errorf("%d of the three added counts require both halves of the link to be live, want 3", count)
	}
}
