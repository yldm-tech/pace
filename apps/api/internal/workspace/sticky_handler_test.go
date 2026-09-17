package workspace

import (
	"encoding/json"
	"testing"
)

// The stripped copy follows the html on every save, and empty html leaves no copy at all rather than an empty one.
func TestTheStrippedNoteIsNullWhenThereIsNoHTML(t *testing.T) {
	if got := strippedStickyText(""); got != nil {
		t.Errorf("empty html strips to %v, want null", *got)
	}
	got := strippedStickyText("<p>hello <b>there</b></p>")
	if got == nil || *got != "hello there" {
		t.Fatalf("the stripped copy is %v", got)
	}
}

// A search term's own wildcards are escaped, so a note holding a per cent sign can be searched for.
func TestTheSearchTermIsEscaped(t *testing.T) {
	if got := escapeLike("50%"); got != `50\%` {
		t.Errorf("a per cent escapes to %q", got)
	}
	if got := escapeLike("a_b"); got != `a\_b` {
		t.Errorf("an underscore escapes to %q", got)
	}
	if got := escapeLike("plain"); got != "plain" {
		t.Errorf("a plain term becomes %q", got)
	}
}

// The serializer is seventeen fields.
func TestTheStickyShape(t *testing.T) {
	data := stickyJSON(Sticky{ID: "sticky-id"})
	if len(data) != 17 {
		t.Fatalf("a note has %d fields, want 17", len(data))
	}
	for _, field := range []string{
		"id", "created_at", "updated_at", "deleted_at", "name", "description",
		"description_html", "description_stripped", "description_binary", "logo_props",
		"color", "background_color", "sort_order", "created_by", "updated_by", "workspace", "owner",
	} {
		if _, present := data[field]; !present {
			t.Errorf("the note is missing %q", field)
		}
	}
}

// An edit leaves what the payload did not name as it was.
func TestAnEditLeavesTheRestAlone(t *testing.T) {
	name := "before"
	sticky := Sticky{Name: &name, DescriptionHTML: "<p>text</p>", SortOrder: 100}
	applyStickyFields(&sticky, map[string]json.RawMessage{"sort_order": json.RawMessage(`200`)})
	if sticky.SortOrder != 200 {
		t.Errorf("the order is %v", sticky.SortOrder)
	}
	if sticky.Name == nil || *sticky.Name != "before" {
		t.Errorf("the name became %v", sticky.Name)
	}
	if sticky.DescriptionHTML != "<p>text</p>" {
		t.Errorf("the html became %q", sticky.DescriptionHTML)
	}
}
