package project

import (
	"strings"
	"testing"
)

// A fields list with nothing valid in it asks for everything, not for nothing.
func TestAnEmptyFieldListAsksForEverything(t *testing.T) {
	narrow := func(raw string) []string {
		requested := []string{}
		for _, field := range strings.Split(raw, ",") {
			if projectStatsValidFields[field] {
				requested = append(requested, field)
			}
		}
		if len(requested) == 0 {
			for field := range projectStatsValidFields {
				requested = append(requested, field)
			}
		}
		return requested
	}
	for raw, want := range map[string]int{
		"":                          5,
		"nonsense":                  5,
		"total_issues":              1,
		"total_issues,nonsense":     1,
		"total_issues,total_cycles": 2,
		",,":                        5,
	} {
		if got := len(narrow(raw)); got != want {
			t.Errorf("%q asked for %d fields, want %d", raw, got, want)
		}
	}
}

// Every valid field has a subquery, and nothing else does.
func TestEveryProjectStatFieldHasASubquery(t *testing.T) {
	if len(projectStatsValidFields) != 5 {
		t.Fatalf("there are %d fields, want 5", len(projectStatsValidFields))
	}
	for field := range projectStatsValidFields {
		column := projectStatsColumn(field)
		if column == "NULL" || !strings.HasPrefix(column, "(SELECT COUNT(") {
			t.Errorf("%q counts nothing: %s", field, column)
		}
	}
	if projectStatsColumn("nonsense") != "NULL" {
		t.Error("an unknown field counts nothing")
	}
}

// The two issue counts go through the manager and the other three do not, which is the asymmetry the endpoint carries.
func TestOnlyTheIssueStatsGoThroughTheManager(t *testing.T) {
	for _, field := range []string{"total_issues", "completed_issues"} {
		if !strings.Contains(projectStatsColumn(field), "triage") {
			t.Errorf("%q must exclude triage, archived and draft issues", field)
		}
	}
	for _, field := range []string{"total_cycles", "total_modules", "total_members"} {
		if strings.Contains(projectStatsColumn(field), "triage") {
			t.Errorf("%q counts rows of its own table and has no manager to apply", field)
		}
	}
	// A bot is not a member for this purpose.
	if !strings.Contains(projectStatsColumn("total_members"), "is_bot = FALSE") {
		t.Error("a bot must not be counted as a member")
	}
}

// The completed count is two state groups rather than one, so a cancelled issue counts as finished here.
func TestCompletedCountsCancelledToo(t *testing.T) {
	column := projectStatsColumn("completed_issues")
	for _, group := range []string{"completed", "cancelled"} {
		if !strings.Contains(column, "'"+group+"'") {
			t.Errorf("the completed count does not include %q", group)
		}
	}
}

// The open set is the three groups that are not finished either way.
func TestTheOpenSetIsThreeGroups(t *testing.T) {
	open := []string{"backlog", "unstarted", "started"}
	finished := map[string]bool{"completed": true, "cancelled": true}
	for _, group := range open {
		if finished[group] {
			t.Errorf("%q is not open", group)
		}
	}
	if len(open)+len(finished) != 5 {
		t.Fatalf("the five state groups do not add up: %d open and %d finished", len(open), len(finished))
	}
}
