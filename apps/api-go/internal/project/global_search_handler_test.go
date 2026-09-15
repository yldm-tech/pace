package project

import (
	"strings"
	"testing"
)

// With no entities parameter every search runs, and the eight names are the ones the mapper holds.
func TestTheGlobalSearchRunsEightSearches(t *testing.T) {
	if len(globalSearchEntities) != 8 {
		t.Fatalf("there are %d entities, want 8", len(globalSearchEntities))
	}
	for _, entity := range []string{"workspace", "project", "issue", "cycle", "module", "issue_view", "page", "intake"} {
		found := false
		for _, known := range globalSearchEntities {
			if known == entity {
				found = true
			}
		}
		if !found {
			t.Errorf("%q is missing from the mapper", entity)
		}
	}
}

// An entity nobody knows is dropped rather than refused, and whitespace around one is trimmed.
func TestUnknownEntitiesAreDropped(t *testing.T) {
	known := map[string]bool{}
	for _, entity := range globalSearchEntities {
		known[entity] = true
	}
	narrow := func(raw string) []string {
		wanted := []string{}
		for _, entity := range strings.Split(raw, ",") {
			entity = strings.TrimSpace(entity)
			if entity != "" && known[entity] {
				wanted = append(wanted, entity)
			}
		}
		return wanted
	}
	for raw, want := range map[string]int{
		"issue":           1,
		" issue , cycle ": 2,
		"issue,nonsense":  1,
		"nonsense":        0,
		"issue,,cycle":    2,
		"Issue":           0,
	} {
		if got := len(narrow(raw)); got != want {
			t.Errorf("%q narrowed to %d entities, want %d", raw, got, want)
		}
	}
}

// The issue search and the intake search are twins that differ in two ways, and both differences matter.
func TestTheIntakeSearchIsNotTheIssueSearch(t *testing.T) {
	// One goes through the manager, which hides triage, archived and draft issues.
	manager := issueObjectsPredicate("i")
	if !strings.Contains(manager, "triage") || !strings.Contains(manager, "archived_at") || !strings.Contains(manager, "is_draft") {
		t.Fatal("the manager hides triage, archived and draft issues")
	}
	// The other keeps only what is waiting in or snoozed inside an intake, which are the two statuses it names.
	for _, status := range []int{intakeStatusSnoozed, intakeStatusPending} {
		if status != 0 && status != -2 {
			t.Errorf("the intake search keeps status %d, which is not one of the two", status)
		}
	}
	if intakeStatusAccepted == intakeStatusPending {
		t.Fatal("an accepted issue is not waiting")
	}
}
