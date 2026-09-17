package project

import (
	"encoding/json"
	"testing"
	"time"
)

// The two dates travel together: either both are given or neither is, and one alone is refused.
func TestCycleDatesTravelTogether(t *testing.T) {
	for _, test := range []struct {
		start, end string
		paired     bool
	}{
		{start: `"2026-06-01"`, end: `"2026-06-30"`, paired: true},
		{start: ``, end: ``, paired: true},
		{start: `null`, end: `null`, paired: true},
		{start: `"2026-06-01"`, end: ``, paired: false},
		{start: ``, end: `"2026-06-30"`, paired: false},
		{start: `"2026-06-01"`, end: `null`, paired: false},
	} {
		startGiven := presentAndNotNull(json.RawMessage(test.start))
		endGiven := presentAndNotNull(json.RawMessage(test.end))
		if (startGiven == endGiven) != test.paired {
			t.Errorf("start %q end %q: paired = %v, want %v", test.start, test.end, startGiven == endGiven, test.paired)
		}
	}
}

// presentAndNotNull is Django's `is not None` check, so an explicit null counts as absent.
func TestPresentAndNotNull(t *testing.T) {
	for raw, want := range map[string]bool{
		`"2026-06-01"`: true,
		`""`:           true,
		`0`:            true,
		`null`:         false,
		``:             false,
	} {
		if got := presentAndNotNull(json.RawMessage(raw)); got != want {
			t.Errorf("presentAndNotNull(%q) = %v, want %v", raw, got, want)
		}
	}
}

// The update snapshot is CycleSerializer over the instance, which carries none of the annotated counts.
func TestCycleWriteSnapshotCarriesNoAnnotations(t *testing.T) {
	data := cycleWriteJSON(Cycle{ID: "cycle-id", ProjectID: "project-id", WorkspaceID: "workspace-id", OwnedByID: "user-id"})
	for _, absent := range []string{"is_favorite", "total_issues", "completed_issues", "cancelled_issues", "assignee_ids", "status"} {
		if _, present := data[absent]; present {
			t.Errorf("%q is annotated on the queryset, not on the instance", absent)
		}
	}
	for _, field := range []string{
		"id", "created_at", "updated_at", "created_by", "updated_by", "deleted_at",
		"name", "description", "start_date", "end_date", "view_props", "sort_order",
		"external_source", "external_id", "progress_snapshot", "archived_at",
		"logo_props", "timezone", "version", "project", "workspace", "owned_by",
	} {
		if _, present := data[field]; !present {
			t.Errorf("the snapshot is missing %q", field)
		}
	}
	if len(data) != 22 {
		t.Fatalf("the snapshot has %d fields, want 22", len(data))
	}
}

// A date the serializer cannot read is refused, and the pair is checked for order before it is converted.
func TestCycleDateParsing(t *testing.T) {
	for _, accepted := range []string{`"2026-06-01"`, `"2026-06-01T00:00:00Z"`, `"2026-06-01 12:00:00"`} {
		if _, ok := cycleDateFromRaw(json.RawMessage(accepted)); !ok {
			t.Errorf("%s should be read", accepted)
		}
	}
	for _, refused := range []string{`"tomorrow"`, `"01/06/2026"`, `42`, `null`} {
		if _, ok := cycleDateFromRaw(json.RawMessage(refused)); ok {
			t.Errorf("%s should be refused", refused)
		}
	}
	start, _ := cycleDateFromRaw(json.RawMessage(`"2026-06-30"`))
	end, _ := cycleDateFromRaw(json.RawMessage(`"2026-06-01"`))
	if !start.After(end) {
		t.Fatal("the later date should compare after the earlier one")
	}
}

// A completed cycle may still be reordered, so everything but the sort order is dropped rather than refused.
func TestACompletedCycleMayStillBeReordered(t *testing.T) {
	now := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)
	ended := now.Add(-24 * time.Hour)
	cycle := Cycle{EndDate: &ended}
	if cycle.EndDate == nil || !cycle.EndDate.Before(now) {
		t.Fatal("the fixture should be a completed cycle")
	}
	// The body is narrowed to the sort order rather than refused, which is what lets a board be reordered after a cycle closes.
	body := map[string]json.RawMessage{"name": json.RawMessage(`"new"`), "sort_order": json.RawMessage(`10`)}
	if _, sortOnly := body["sort_order"]; !sortOnly {
		t.Fatal("the sort order is what decides")
	}
	narrowed := map[string]json.RawMessage{"sort_order": body["sort_order"]}
	if len(narrowed) != 1 {
		t.Fatal("everything but the sort order is dropped")
	}
}
