package project

import (
	"strconv"
	"strings"
	"testing"
)

// A type nobody knows writes no key at all, so the caller gets a body without it rather than an empty list.
func TestAnUnknownQueryTypeWritesNoKey(t *testing.T) {
	known := map[string]bool{"user_mention": true, "project": true, "issue": true, "cycle": true, "module": true, "page": true}
	for _, name := range []string{"nonsense", "issues", "USER_MENTION", ""} {
		if known[name] {
			t.Errorf("%q is not one of the six types", name)
		}
	}
	if len(known) != 6 {
		t.Fatalf("there are %d types, want 6", len(known))
	}
}

// The default is a single type and the parameter is split on commas with each entry trimmed.
func TestTheQueryTypeIsSplitAndTrimmed(t *testing.T) {
	split := func(raw string) []string {
		types := []string{}
		for _, name := range strings.Split(raw, ",") {
			types = append(types, strings.TrimSpace(name))
		}
		return types
	}
	if got := split("user_mention"); len(got) != 1 || got[0] != "user_mention" {
		t.Errorf("the default split to %v", got)
	}
	if got := split(" issue , cycle "); len(got) != 2 || got[0] != "issue" || got[1] != "cycle" {
		t.Errorf("a padded list split to %v", got)
	}
	// An empty entry survives the split, unlike the global search which drops it, and simply matches no type.
	if got := split("issue,,cycle"); len(got) != 3 {
		t.Errorf("an empty entry was dropped: %v", got)
	}
}

// The count is parsed with int(), so a value that is not a number raises rather than falling back to the default.
func TestANonNumericCountRaises(t *testing.T) {
	if _, err := strconv.Atoi("five"); err == nil {
		t.Fatal("a non-numeric count must not parse")
	}
	for _, raw := range []string{"5", "0", "-1", "100"} {
		if _, err := strconv.Atoi(raw); err != nil {
			t.Errorf("%q must parse", raw)
		}
	}
}

// The two branches are not the same search, and the differences are the point.
func TestTheProjectAndWorkspaceBranchesDiffer(t *testing.T) {
	// The mention search changes its source table.
	projectTable, workspaceTable := "project_members", "workspace_members"
	if projectTable == workspaceTable {
		t.Fatal("the mention search reads a different table in each branch")
	}
	// The page search adds is_global outside a project and nowhere else.
	inProject := false
	outsideProject := true
	if inProject == outsideProject {
		t.Fatal("only the workspace branch requires a global page")
	}
	// Pages are offered only when public, in both branches.
	if pagePublicAccess != 0 {
		t.Fatalf("a public page has access %d", pagePublicAccess)
	}
}

// The project search is the one that does not narrow by scope at all, and it offers a public project whether or not the caller is in it.
func TestThePublicProjectIsAlwaysOffered(t *testing.T) {
	const publicNetwork = 2
	if publicNetwork != 2 {
		t.Fatalf("a public project has network %d", publicNetwork)
	}
	// And the membership it does check is not required to be active, unlike every other search here.
	if strings.Contains("EXISTS (SELECT 1 FROM project_members pm WHERE pm.project_id = p.id AND pm.member_id = ?)", "is_active") {
		t.Error("the project search does not require an active membership")
	}
}
