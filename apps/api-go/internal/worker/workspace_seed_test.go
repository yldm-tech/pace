package worker

import (
	"encoding/json"
	"testing"
)

// The seed files embedded here parse into the shapes the task reads, so a file changed upstream and copied across without looking is caught here rather than at run time.
func TestTheSeedFilesParse(t *testing.T) {
	var projects []seedProject
	if err := json.Unmarshal(projectSeedJSON, &projects); err != nil || len(projects) != 1 {
		t.Fatalf("the project seed is %v (%d)", err, len(projects))
	}
	var states []seedState
	if err := json.Unmarshal(stateSeedJSON, &states); err != nil || len(states) != 5 {
		t.Fatalf("the state seed is %v (%d)", err, len(states))
	}
	// The seed has no triage state, so a project seeded this way has nothing for an intake to file into — the same gap the dummy project has.
	for _, state := range states {
		if state.Group == "triage" {
			t.Error("the seed writes a triage state")
		}
	}
	var labels []seedLabel
	if err := json.Unmarshal(labelSeedJSON, &labels); err != nil || len(labels) != 2 {
		t.Fatalf("the label seed is %v (%d)", err, len(labels))
	}
	var cycles []seedCycle
	if err := json.Unmarshal(cycleSeedJSON, &cycles); err != nil || len(cycles) != 2 {
		t.Fatalf("the cycle seed is %v (%d)", err, len(cycles))
	}
	for _, cycle := range cycles {
		if cycle.Type != "CURRENT" && cycle.Type != "UPCOMING" {
			t.Errorf("a cycle is typed %q, and only two types are dated", cycle.Type)
		}
	}
	var modules []seedModule
	if err := json.Unmarshal(moduleSeedJSON, &modules); err != nil || len(modules) != 3 {
		t.Fatalf("the module seed is %v (%d)", err, len(modules))
	}
	var issues []seedIssue
	if err := json.Unmarshal(issueSeedJSON, &issues); err != nil || len(issues) == 0 {
		t.Fatalf("the issue seed is %v (%d)", err, len(issues))
	}
	var views []seedView
	if err := json.Unmarshal(viewSeedJSON, &views); err != nil || len(views) != 1 {
		t.Fatalf("the view seed is %v (%d)", err, len(views))
	}
	// The query column is not nullable and the file never names it; IssueView.save fills it from the filters, and an empty filter set gives an empty object.
	if string(views[0].Filters) != "{}" {
		t.Errorf("the view's filters are %s, and an empty set is what makes the query an empty object", views[0].Filters)
	}
	var pages []seedPage
	if err := json.Unmarshal(pageSeedJSON, &pages); err != nil || len(pages) != 2 {
		t.Fatalf("the page seed is %v (%d)", err, len(pages))
	}
}

// Every work item names a state, a project and labels that the maps really carry, so the seed cannot point at something that was never written.
func TestTheSeedReferencesResolve(t *testing.T) {
	var states []seedState
	_ = json.Unmarshal(stateSeedJSON, &states)
	var labels []seedLabel
	_ = json.Unmarshal(labelSeedJSON, &labels)
	var cycles []seedCycle
	_ = json.Unmarshal(cycleSeedJSON, &cycles)
	var modules []seedModule
	_ = json.Unmarshal(moduleSeedJSON, &modules)
	var issues []seedIssue
	_ = json.Unmarshal(issueSeedJSON, &issues)

	known := func(items []int, name string, ids map[int]bool) {
		t.Helper()
		for _, item := range items {
			if !ids[item] {
				t.Errorf("a work item names %s %d, which the seed never writes", name, item)
			}
		}
	}
	stateIDs, labelIDs, cycleIDs, moduleIDs := map[int]bool{}, map[int]bool{}, map[int]bool{}, map[int]bool{}
	for _, state := range states {
		stateIDs[state.ID] = true
	}
	for _, label := range labels {
		labelIDs[label.ID] = true
	}
	for _, cycle := range cycles {
		cycleIDs[cycle.ID] = true
	}
	for _, module := range modules {
		moduleIDs[module.ID] = true
	}
	for _, issue := range issues {
		known([]int{issue.StateID}, "state", stateIDs)
		known(issue.Labels, "label", labelIDs)
		known(issue.ModuleIDs, "module", moduleIDs)
		if issue.CycleID != 0 {
			known([]int{issue.CycleID}, "cycle", cycleIDs)
		}
	}
}

// The project identifier is the workspace name's letters and digits cut to five, so spaces and punctuation are dropped rather than replaced.
func TestTheSeededProjectIdentifier(t *testing.T) {
	cases := map[string]string{
		"Acme Corp":     "AcmeC",
		"a":             "a",
		"!!!":           "",
		"Ünicode Works": "Ünico",
		"12 Monkeys":    "12Mon",
	}
	for name, want := range cases {
		if got := alphanumericPrefix(name, 5); got != want {
			t.Errorf("%q gives %q rather than %q", name, got, want)
		}
	}
}

// A state's slug is its name lowercased with the spaces turned into dashes, which is what State.save writes and what the seeded states really get.
func TestTheSeededStateSlug(t *testing.T) {
	cases := map[string]string{
		"Backlog":     "backlog",
		"In Progress": "in-progress",
		"Done":        "done",
		"  Spaced  ":  "spaced",
	}
	for name, want := range cases {
		if got := slugify(name); got != want {
			t.Errorf("%q slugs to %q rather than %q", name, got, want)
		}
	}
}

// The invitor is named by whichever of their three names is set first, which is what the subject's or-chain picks.
func TestTheInvitationSubject(t *testing.T) {
	cases := []struct{ first, display, email, want string }{
		{"Ada", "ada", "ada@example.test", "Ada invited you to join Apollo on Plane"},
		{"", "ada", "ada@example.test", "ada invited you to join Apollo on Plane"},
		{"", "", "ada@example.test", "ada@example.test invited you to join Apollo on Plane"},
	}
	for _, testCase := range cases {
		if got := invitationSubject(testCase.first, testCase.display, testCase.email, "Apollo"); got != testCase.want {
			t.Errorf("the subject is %q rather than %q", got, testCase.want)
		}
	}
}
