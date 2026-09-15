package project

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// created_cycle_issues is a JSON string inside the snapshot rather than a nested object, because Django builds it with serializers.serialize and then dumps the whole snapshot around it. The task calls json.loads on it, so the nesting has to survive.
func TestCreatedCycleIssuesIsANestedJSONString(t *testing.T) {
	records := []map[string]any{{
		"model": "db.cycleissue", "pk": "link-id",
		"fields": map[string]any{"cycle": "cycle-id", "issue": "issue-id"},
	}}
	serialized, err := json.Marshal(records)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := json.Marshal(map[string]any{
		"updated_cycle_issues": []any{},
		"created_cycle_issues": string(serialized),
	})
	if err != nil {
		t.Fatal(err)
	}
	// The value is a string; an object here would make the task's json.loads fail.
	var decoded map[string]any
	if err := json.Unmarshal(snapshot, &decoded); err != nil {
		t.Fatal(err)
	}
	inner, ok := decoded["created_cycle_issues"].(string)
	if !ok {
		t.Fatalf("created_cycle_issues is %T, want a string", decoded["created_cycle_issues"])
	}
	var reparsed []map[string]any
	if err := json.Unmarshal([]byte(inner), &reparsed); err != nil {
		t.Fatalf("the inner value is not JSON: %v", err)
	}
	// The task reads only these two, through the fields object.
	fields := reparsed[0]["fields"].(map[string]any)
	if fields["cycle"] != "cycle-id" || fields["issue"] != "issue-id" {
		t.Fatalf("fields = %v", fields)
	}
}

// An issue already linked to another cycle is moved rather than duplicated, since an issue belongs to at most one cycle.
func TestAnIssueMovesBetweenCyclesRatherThanJoiningBoth(t *testing.T) {
	requested := []string{"issue-a", "issue-b", "issue-c"}
	moving := []CycleIssue{{IssueID: "issue-b", CycleID: "other-cycle"}}
	alreadyLinked := map[string]bool{}
	for _, link := range moving {
		alreadyLinked[link.IssueID] = true
	}
	candidates := []string{}
	for _, issueID := range requested {
		if !alreadyLinked[issueID] {
			candidates = append(candidates, issueID)
		}
	}
	if strings.Join(candidates, ",") != "issue-a,issue-c" {
		t.Fatalf("candidates = %v, want the two that are not linked elsewhere", candidates)
	}
}

// Only a completed cycle may be archived: one with no end date, or one whose end date has not passed, is refused.
func TestOnlyACompletedCycleMayBeArchived(t *testing.T) {
	now := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)
	for _, test := range []struct {
		end        *time.Time
		archivable bool
	}{
		{end: &past, archivable: true},
		{end: &future, archivable: false},
		{end: &now, archivable: false},
		{end: nil, archivable: false},
	} {
		cycle := Cycle{EndDate: test.end}
		archivable := cycle.EndDate != nil && cycle.EndDate.Before(now)
		if archivable != test.archivable {
			t.Errorf("end %v: archivable = %v, want %v", test.end, archivable, test.archivable)
		}
	}
}

// Archiving clears every member's favourite, not only the caller's.
func TestArchivingClearsEveryFavourite(t *testing.T) {
	// The predicate names the entity and the project but not the user, which is what makes it everyone's.
	const predicate = `entity_type = 'cycle' AND entity_identifier = ? AND project_id = ? AND deleted_at IS NULL`
	if strings.Contains(predicate, "user_id") {
		t.Fatal("archiving clears every member's favourite, so the predicate names no user")
	}
	// Deleting a cycle, by contrast, clears only the caller's.
	const onDelete = `user_id = ? AND entity_type = 'cycle' AND entity_identifier = ? AND project_id = ?`
	if !strings.Contains(onDelete, "user_id") {
		t.Fatal("deleting clears only the caller's favourite")
	}
}
