package project

import (
	"strings"
	"testing"
	"time"
)

// The link condition sits inside one EXISTS, the way the cycle's does: Django puts both halves in a single filter call, so they apply to the same joined row.
var moduleLinkFixedTime = time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)

func TestModuleIssueListPredicateKeepsBothHalvesInOneExists(t *testing.T) {
	predicate := moduleIssueListPredicate()
	if strings.Count(predicate, "EXISTS (SELECT 1 FROM module_issues") != 1 {
		t.Fatalf("the link is checked in more than one EXISTS:\n%s", predicate)
	}
	inner := predicate[strings.Index(predicate, "EXISTS ("):]
	for _, half := range []string{"mil.module_id = ?", "mil.deleted_at IS NULL", "mil.issue_id = i.id"} {
		if !strings.Contains(inner, half) {
			t.Errorf("the EXISTS is missing %q", half)
		}
	}
	// And the manager's own exclusions are still there, so the module board shows what the project board shows.
	if !strings.Contains(predicate, issueObjectsPredicate("i")) {
		t.Error("the module issue list does not narrow the issue_objects manager")
	}
	// One placeholder, the module id, which is the only argument the caller passes.
	if strings.Count(predicate, "?") != strings.Count(issueObjectsPredicate("i"), "?")+1 {
		t.Errorf("the predicate does not take exactly one argument of its own:\n%s", predicate)
	}
}

// The link rows carry the workspace read off the project rather than off the request, which is what ProjectBaseModel.save does.
func TestModuleLinkRowsCarryTheProjectsWorkspace(t *testing.T) {
	rows := moduleIssueLinkRows([]string{"issue-one", "issue-two"}, "module-id", "project-id", "workspace-id", "actor-id", moduleLinkFixedTime, func() (string, error) { return "generated-id", nil })
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}
	for _, row := range rows {
		if row.WorkspaceID != "workspace-id" || row.ProjectID != "project-id" || row.ModuleID != "module-id" {
			t.Errorf("a link row lost its scope: %+v", row)
		}
		if row.CreatedByID == nil || *row.CreatedByID != "actor-id" || row.UpdatedByID == nil || *row.UpdatedByID != "actor-id" {
			t.Errorf("a link row lost its actor: %+v", row)
		}
		if !row.CreatedAt.Equal(moduleLinkFixedTime) || !row.UpdatedAt.Equal(moduleLinkFixedTime) {
			t.Errorf("a link row lost its timestamps: %+v", row)
		}
	}
	if rows[0].IssueID != "issue-one" || rows[1].IssueID != "issue-two" {
		t.Errorf("the rows are not in the order the caller gave: %+v", rows)
	}
	if moduleIssueLinkRows(nil, "module-id", "project-id", "workspace-id", "actor-id", moduleLinkFixedTime, nil) != nil {
		t.Error("an empty list must build no rows at all, since the insert is skipped")
	}
}
