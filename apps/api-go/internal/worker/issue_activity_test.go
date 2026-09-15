package worker

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// TestTheTrackersMatchDjango walks the truth table the real trackers generated. Every line is a change somebody could make and the history it leaves behind.
func TestTheTrackersMatchDjango(t *testing.T) {
	contents, err := os.ReadFile("testdata/issue_activity.tsv")
	if err != nil {
		t.Fatal(err)
	}
	tasks := &IssueActivityTasks{clock: func() time.Time { return time.Unix(0, 0).UTC() }}
	context := activityContext{
		issueID:     "11111111-1111-1111-1111-111111111111",
		projectID:   "22222222-2222-2222-2222-222222222222",
		workspaceID: "33333333-3333-3333-3333-333333333333",
		actorID:     "44444444-4444-4444-4444-444444444444",
	}

	checked := 0
	for _, line := range strings.Split(string(contents), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 4 {
			t.Fatalf("malformed fixture line: %q", line)
		}
		tracker, requestedRaw, currentRaw, want := fields[0], fields[1], fields[2], fields[3]
		checked++

		requested := decodeSnapshot(t, requestedRaw)
		current := decodeSnapshot(t, currentRaw)

		var rows []activityRow
		switch tracker {
		case "name":
			rows = tasks.trackPlain(requested, current, context, "name", "name", "updated the name to")
		case "priority":
			rows = tasks.trackPlain(requested, current, context, "priority", "priority", "updated the priority to")
		case "target_date":
			rows = tasks.trackDate(requested, current, context, "target_date", "updated the target date to")
		case "start_date":
			rows = tasks.trackDate(requested, current, context, "start_date", "updated the start date to ")
		case "archived_at":
			rows = tasks.trackArchivedAt(requested, current, context)
		default:
			t.Fatalf("the fixture names a tracker this test does not run: %q", tracker)
		}

		if got := renderRows(rows); got != want {
			t.Errorf("%s over %s against %s became\n  %s\nwant\n  %s", tracker, requestedRaw, currentRaw, got, want)
		}
	}
	if checked < 15 {
		t.Fatalf("the fixture only carried %d cases, which is too few to be the generated one", checked)
	}
}

func decodeSnapshot(t *testing.T, raw string) map[string]any {
	t.Helper()
	snapshot := map[string]any{}
	if err := json.Unmarshal([]byte(raw), &snapshot); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func renderRows(rows []activityRow) string {
	if len(rows) == 0 {
		return "-"
	}
	parts := make([]string, 0, len(rows))
	for _, row := range rows {
		parts = append(parts, strings.Join([]string{
			renderPointer(row.Field), row.Verb,
			renderPointer(row.OldValue), renderPointer(row.NewValue), row.Comment,
		}, "|"))
	}
	return strings.Join(parts, ";")
}

func renderPointer(value *string) string {
	if value == nil {
		return "<none>"
	}
	return *value
}

// A date that was cleared reads as the empty string rather than as nothing, which is the one way the date trackers differ from the plain ones.
func TestAClearedDateIsEmptyRatherThanNothing(t *testing.T) {
	tasks := &IssueActivityTasks{}
	context := activityContext{}
	rows := tasks.trackDate(map[string]any{"target_date": nil}, map[string]any{"target_date": "2026-01-02"},
		context, "target_date", "updated the target date to")
	if len(rows) != 1 || rows[0].NewValue == nil || *rows[0].NewValue != "" {
		t.Fatalf("a cleared date became %#v", rows)
	}
	// The plain trackers keep nothing as nothing.
	rows = tasks.trackPlain(map[string]any{"name": nil}, map[string]any{"name": "Before"},
		context, "name", "name", "updated the name to")
	if len(rows) != 1 || rows[0].NewValue != nil {
		t.Fatalf("a cleared name became %#v", rows)
	}
}

// The two names for a set of ids are the session API's and the external one's, and the first present wins whatever it holds.
func TestTheTwoNamesForASetOfIDs(t *testing.T) {
	const first = "11111111-1111-1111-1111-111111111111"
	const second = "22222222-2222-2222-2222-222222222222"
	added, dropped := activityIDDifference(
		map[string]any{"label_ids": []any{first, second}},
		map[string]any{"label_ids": []any{first}}, "label_ids", "labels")
	if len(added) != 1 || added[0] != second || len(dropped) != 0 {
		t.Errorf("added %#v and dropped %#v", added, dropped)
	}
	// The fallback name is only read when the first is absent, not when it is empty.
	added, _ = activityIDDifference(
		map[string]any{"labels": []any{first}}, map[string]any{}, "label_ids", "labels")
	if len(added) != 1 || added[0] != first {
		t.Errorf("the fallback name added %#v", added)
	}
	added, _ = activityIDDifference(
		map[string]any{"label_ids": []any{}, "labels": []any{first}}, map[string]any{}, "label_ids", "labels")
	if len(added) != 0 {
		t.Errorf("an empty primary fell through to the fallback: %#v", added)
	}
	// Anything that is not a uuid is dropped rather than refused.
	added, _ = activityIDDifference(
		map[string]any{"label_ids": []any{"not-a-uuid", first}}, map[string]any{}, "label_ids", "labels")
	if len(added) != 1 || added[0] != first {
		t.Errorf("a non-uuid was kept: %#v", added)
	}
}

// A state id that is not one reads as no state; a parent id that is not one abandons the field.
func TestTheTwoWaysABadIDIsRead(t *testing.T) {
	if got := uuidOrNothing("not-a-uuid"); got != "" {
		t.Errorf("a bad state id read as %q", got)
	}
	if got := uuidOrNothing("11111111-1111-1111-1111-111111111111"); got == "" {
		t.Error("a good id read as nothing")
	}
}

// Both names for the same field reach the same tracker, which is why a payload naming both writes the change twice.
func TestBothNamesReachTheSameTracker(t *testing.T) {
	for _, pair := range [][2]string{{"state_id", "state"}, {"parent_id", "parent"}, {"assignee_ids", "assignees"}, {"label_ids", "labels"}} {
		if issueTrackers[pair[0]] != issueTrackers[pair[1]] {
			t.Errorf("%q and %q reach %q and %q", pair[0], pair[1], issueTrackers[pair[0]], issueTrackers[pair[1]])
		}
	}
	if len(issueTrackers) != 16 {
		t.Errorf("there are %d tracked keys, want 16", len(issueTrackers))
	}
}

// Every activity type the Python mapper knows has a handler here, because taking the task over takes all of them over at once.
func TestEveryActivityTypeIsHandled(t *testing.T) {
	tasks := &IssueActivityTasks{clock: func() time.Time { return time.Unix(0, 0).UTC() }}
	// The types that need no database at all: each one must produce a row rather than nothing.
	for activityType, wantRows := range map[string]int{
		"comment.activity.created":        1,
		"comment.activity.deleted":        1,
		"link.activity.created":           1,
		"link.activity.deleted":           1,
		"attachment.activity.created":     1,
		"attachment.activity.deleted":     1,
		"issue_vote.activity.created":     1,
		"issue_draft.activity.created":    1,
		"issue_draft.activity.updated":    1,
		"issue_draft.activity.deleted":    1,
		"intake.activity.created":         1,
		"issue_reaction.activity.deleted": 1,
		"issue_vote.activity.deleted":     1,
	} {
		rows, err := tasks.dispatchRelated(nil, activityType,
			`{"id":"11111111-1111-1111-1111-111111111111","url":"https://example.test","comment_html":"<p>hi</p>","comment_id":"22222222-2222-2222-2222-222222222222","vote":1,"status":1}`,
			`{"id":"33333333-3333-3333-3333-333333333333","asset":"key","url":"https://old.test","reaction":"smile","vote":-1,"status":-2}`,
			activityContext{issueID: "44444444-4444-4444-4444-444444444444"})
		if err != nil {
			t.Errorf("%s gave %v", activityType, err)
			continue
		}
		if len(rows) != wantRows {
			t.Errorf("%s produced %d rows, want %d", activityType, len(rows), wantRows)
		}
	}
	// The work item's own delete is handled a level up, where the three issue.* types are.
	if rows, err := tasks.dispatch(nil, "issue.activity.deleted", "{}", "{}", activityContext{}); err != nil || len(rows) != 1 {
		t.Errorf("the work item delete produced %#v and %v", rows, err)
	}

	// An unknown type produces nothing rather than failing, which is what a missing key in the mapper does.
	rows, err := tasks.dispatchRelated(nil, "nonsense.activity.created", "{}", "{}", activityContext{})
	if err != nil || len(rows) != 0 {
		t.Errorf("an unknown type gave %#v and %v", rows, err)
	}
}

// The intake statuses are reported by name, and a status with no name is reported as nothing.
func TestTheIntakeStatusNames(t *testing.T) {
	for value, want := range map[float64]string{-2: "Pending", -1: "Rejected", 0: "Snoozed", 1: "Accepted", 2: "Duplicate"} {
		got := intakeStatusName(value)
		if got == nil || *got != want {
			t.Errorf("%v is named %v, want %q", value, got, want)
		}
	}
	if got := intakeStatusName(99.0); got != nil {
		t.Errorf("an unknown status is named %v", *got)
	}
	if got := intakeStatusName(nil); got != nil {
		t.Errorf("no status is named %v", *got)
	}
}

// A relation reads the other way round from the far work item's side, and a relation with no opposite reads the same both ways.
func TestTheInverseRelations(t *testing.T) {
	for relation, want := range map[string]string{
		"blocking": "blocked_by", "blocked_by": "blocking",
		"start_after": "start_before", "start_before": "start_after",
		"finish_after": "finish_before", "finish_before": "finish_after",
		"implements": "implemented_by", "implemented_by": "implements",
	} {
		if got := inverseRelation(relation); got != want {
			t.Errorf("%q reads as %q from the other side, want %q", relation, got, want)
		}
	}
	if got := inverseRelation("relates_to"); got != "relates_to" {
		t.Errorf("a relation with no opposite reads as %q", got)
	}
}
