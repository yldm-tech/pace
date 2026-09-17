package project

import (
	"testing"
)

// sanitize_order_by strips at most one leading dash, so a doubled one is rejected rather than reaching the ORM.
func TestTheOrderByAllowlistRejectsWhatItShould(t *testing.T) {
	for value, want := range map[string]string{
		"":             "-created_at",
		"created_at":   "created_at",
		"-created_at":  "-created_at",
		"updated_at":   "updated_at",
		"-updated_at":  "-updated_at",
		"name":         "name",
		"-name":        "-name",
		"--created_at": "-created_at",
		"-":            "-created_at",
		"sort_order":   "-created_at",
		"-sort_order":  "-created_at",
		"id":           "-created_at",
		"name; DROP":   "-created_at",
		"-name; DROP":  "-created_at",
	} {
		if got := sanitizeOrderBy(value, viewOrderByAllowlist, "-created_at"); got != want {
			t.Errorf("sanitizeOrderBy(%q) = %q, want %q", value, got, want)
		}
	}
}

// Three fields are orderable and nothing else, which is the whole point of the allowlist.
func TestOnlyThreeFieldsAreOrderable(t *testing.T) {
	if len(viewOrderByAllowlist) != 3 {
		t.Fatalf("the allowlist has %d fields, want 3", len(viewOrderByAllowlist))
	}
	for _, field := range []string{"created_at", "updated_at", "name"} {
		if !viewOrderByAllowlist[field] {
			t.Errorf("%q must be orderable", field)
		}
	}
}

// The workspace list has no favourite flag: nothing annotates one, so every row reports false.
func TestTheWorkspaceListCarriesNoFavourite(t *testing.T) {
	data := viewJSON(viewRow{IssueView: IssueView{ID: "view-id"}})
	if data["is_favorite"] != false {
		t.Errorf("is_favorite is %v on an unannotated row, want false", data["is_favorite"])
	}
	if data["project"] != (*string)(nil) {
		t.Errorf("a workspace view belongs to no project, got %v", data["project"])
	}
}
