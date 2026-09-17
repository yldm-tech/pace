package project

import (
	"testing"
)

// The serializer renders every field the model has, since it asks for __all__.
func TestViewJSONRendersTheWholeModel(t *testing.T) {
	data := viewJSON(viewRow{IssueView: IssueView{ID: "view-id"}, IsFavorite: true})
	for _, field := range []string{
		"id", "created_at", "updated_at", "created_by", "updated_by", "deleted_at",
		"name", "description", "query", "filters", "display_filters", "display_properties",
		"rich_filters", "logo_props", "access", "sort_order", "is_locked", "archived_at",
		"owned_by", "project", "workspace", "is_favorite",
	} {
		if _, present := data[field]; !present {
			t.Errorf("the view is missing %q", field)
		}
	}
	if len(data) != 22 {
		t.Fatalf("the view has %d fields, want 22", len(data))
	}
}

// The fields parameter narrows what is rendered, and an empty one narrows nothing. A trailing comma must not ask for a field with no name.
func TestTheFieldsParameterNarrowsTheList(t *testing.T) {
	data := viewJSON(viewRow{IssueView: IssueView{ID: "view-id", Name: "Bugs"}})

	if got := narrowToFields(data, requestedFields("")); len(got) != 22 {
		t.Errorf("an empty fields parameter kept %d fields, want all of them", len(got))
	}
	if got := requestedFields("id,name,"); len(got) != 2 {
		t.Errorf("a trailing comma produced %d fields, want 2", len(got))
	}
	narrowed := narrowToFields(data, requestedFields("id,name,not_a_field"))
	if len(narrowed) != 2 || narrowed["id"] != "view-id" || narrowed["name"] != "Bugs" {
		t.Errorf("narrowing produced %v", narrowed)
	}
}

// Four fields are read-only on the serializer, so a caller naming them changes nothing.
func TestTheReadOnlyFieldsAreNotWritable(t *testing.T) {
	view := IssueView{Access: 1, OwnedByID: "owner-id", IsLocked: true, Query: []byte(`{"a":1}`)}
	applyViewPayload(&view, map[string]any{
		"access": 0, "owned_by": "someone-else", "is_locked": false,
		"query": map[string]any{"b": 2}, "workspace": "elsewhere", "project": "elsewhere",
		"name": "Renamed",
	})
	if view.Access != 1 || view.OwnedByID != "owner-id" || !view.IsLocked {
		t.Errorf("a read-only field was written: %+v", view)
	}
	if string(view.Query) != `{"a":1}` {
		t.Errorf("the query was written from the payload, and it is derived rather than accepted")
	}
	if view.Name != "Renamed" {
		t.Error("a writable field was not written")
	}
}

// An update that does not mention filters clears the stored query along with them, because the model recomputes it from whatever the instance now holds.
func TestAnUpdateWithoutFiltersClearsTheQuery(t *testing.T) {
	view := IssueView{Filters: []byte(`{"priority":["urgent"]}`)}
	applyViewPayload(&view, map[string]any{"name": "Renamed"})
	if string(view.Filters) != `{"priority":["urgent"]}` {
		t.Fatal("the filters themselves are left alone")
	}

	cleared := IssueView{Filters: []byte(`{"priority":["urgent"]}`)}
	applyViewPayload(&cleared, map[string]any{"filters": map[string]any{}})
	query, err := viewQueryJSON(cleared.Filters, frozenViewQueryToday)
	if err != nil {
		t.Fatal(err)
	}
	if string(query) != "{}" {
		t.Errorf("clearing the filters left %s behind", query)
	}
}

// A full update requires the name, which is what DRF's partial=False adds.
func TestViewFullUpdateRequiresTheName(t *testing.T) {
	required := fullUpdateRequirements["view"]
	if len(required) != 1 || required[0] != "name" {
		t.Fatalf("a view's full update requires %v, want just the name", required)
	}
}
