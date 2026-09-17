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
		if _, permitted := viewOrderByAllowlist[field]; !permitted {
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

// TestOrderByOnlyEverYieldsAnAllowlistedColumn pins what the sanitiser guarantees at runtime: whatever arrives in order_by, what comes out is one of the columns the allowlist names, or the fallback. A quote, a comment marker or a second clause all end up as the fallback.
//
// It does not distinguish where that string came from. sanitizeOrderBy reads the column out of the table rather than returning the caller's own string -- which is what makes the safety visible to a reader and to static analysis -- but since every value in the table equals its key, both spellings behave identically and no runtime test can tell them apart.
func TestOrderByOnlyEverYieldsAnAllowlistedColumn(t *testing.T) {
	columns := map[string]bool{}
	for _, column := range viewOrderByAllowlist {
		columns[column] = true
		columns["-"+column] = true
	}
	columns["-created_at"] = true // the fallback

	for _, hostile := range []string{
		"name; DROP TABLE views--",
		"created_at, (SELECT 1)",
		"name/*",
		"' OR '1'='1",
		"-name; DELETE FROM views",
		"..//..//etc",
		"",
		"-",
		"--created_at",
		"NAME",
	} {
		got := sanitizeOrderBy(hostile, viewOrderByAllowlist, "-created_at")
		if !columns[got] {
			t.Errorf("sanitizeOrderBy(%q) = %q, which is not one of the allowlisted columns", hostile, got)
		}
	}

	// And the permitted values still come through, in both directions.
	for field := range viewOrderByAllowlist {
		if got := sanitizeOrderBy(field, viewOrderByAllowlist, "-created_at"); got != field {
			t.Errorf("sanitizeOrderBy(%q) = %q", field, got)
		}
		if got := sanitizeOrderBy("-"+field, viewOrderByAllowlist, "-created_at"); got != "-"+field {
			t.Errorf("sanitizeOrderBy(%q) = %q", "-"+field, got)
		}
	}
}
