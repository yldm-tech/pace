package project

import (
	"strings"
	"testing"
)

// The three ways a work item counts as somebody's are ORed, not ANDed: they have it, they raised it, or they follow it.
func TestTheThreeWaysAWorkItemIsSomebodys(t *testing.T) {
	for _, fragment := range []string{"uia.assignee_id = ?", "ui.created_by_id = ?", "uis.subscriber_id = ?"} {
		if !strings.Contains(userIssueMembershipPredicate, fragment) {
			t.Errorf("the predicate is missing %q", fragment)
		}
	}
	if strings.Count(userIssueMembershipPredicate, " OR ") != 2 {
		t.Errorf("the three ways are not ORed: %q", userIssueMembershipPredicate)
	}
	// Both are outer joins, since a work item somebody raised need have no assignee and no follower.
	if strings.Count(userIssueMembershipPredicate, "LEFT JOIN") != 2 {
		t.Errorf("the two link tables are not outer joined: %q", userIssueMembershipPredicate)
	}
}

// A list that spans the workspace names no project, and the scope then narrows by the workspace alone.
func TestAListWithNoProjectIsNotNarrowedToOne(t *testing.T) {
	source := issueGroupValues["assignees__id"]
	if source.UnscopedTable != "workspace_members" {
		t.Errorf("without a project the assignees come from %q", source.UnscopedTable)
	}
	// With one, they still come from the project's own membership.
	if source.Table != "project_members" || !source.ProjectScoped {
		t.Errorf("with a project the assignees come from %q", source.Table)
	}
	// Nothing else needs the distinction, because every other group reads a table that carries both ids.
	unscoped := 0
	for _, value := range issueGroupValues {
		if value.UnscopedTable != "" {
			unscoped++
		}
	}
	if unscoped != 1 {
		t.Errorf("%d groups carry a workspace-wide table, want 1", unscoped)
	}
}
