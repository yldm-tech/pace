package project

import (
	"testing"
)

// Every update clears the stored query, because the serializer reads a key the model does not have.
func TestAnUpdateAlwaysClearsTheQuery(t *testing.T) {
	// The create path reads query_dict and derives from it.
	view := AnalyticView{QueryDict: []byte(`{"priority":["urgent"]}`)}
	created, err := viewQueryJSON(view.QueryDict, frozenViewQueryToday)
	if err != nil {
		t.Fatal(err)
	}
	if string(created) == "{}" {
		t.Fatal("the create path derives a query from the filters it was given")
	}
	// The update path reads query_data, which nothing sends, so it derives from nothing.
	payload := map[string]any{"query_dict": map[string]any{"priority": []any{"urgent"}}}
	if _, present := payload["query_data"]; present {
		t.Fatal("no caller sends query_data; that is the point")
	}
	updated, err := viewQueryJSON(nil, frozenViewQueryToday)
	if err != nil {
		t.Fatal(err)
	}
	if string(updated) != "{}" {
		t.Errorf("the update path derived %s, want the empty object", updated)
	}
}

// The axis validation is two checks in a fixed order, and an empty segment is not a segment.
func TestTheAxisValidation(t *testing.T) {
	for name, test := range map[string]struct {
		x, y, segment any
		message       string
	}{
		"valid":              {"priority", "issue_count", nil, ""},
		"valid with segment": {"priority", "estimate", "state_id", ""},
		"missing x":          {nil, "issue_count", nil, "x-axis and y-axis dimensions are required and the values should be valid"},
		"missing y":          {"priority", nil, nil, "x-axis and y-axis dimensions are required and the values should be valid"},
		"unknown x":          {"nonsense", "issue_count", nil, "x-axis and y-axis dimensions are required and the values should be valid"},
		"unknown y":          {"priority", "nonsense", nil, "x-axis and y-axis dimensions are required and the values should be valid"},
		"segment equals x":   {"priority", "issue_count", "priority", "Both segment and x axis cannot be same and segment should be valid"},
		"unknown segment":    {"priority", "issue_count", "nonsense", "Both segment and x axis cannot be same and segment should be valid"},
		"empty segment":      {"priority", "issue_count", "", ""},
		"y is not an axis":   {"priority", "state_id", nil, "x-axis and y-axis dimensions are required and the values should be valid"},
	} {
		if got := validateAnalyticsAxes(test.x, test.y, test.segment); got != test.message {
			t.Errorf("%s: got %q, want %q", name, got, test.message)
		}
	}
}

// The two allowlists are the ones the plot module defines, and the y axis is a much shorter list than the x.
func TestTheAnalyticsAllowlists(t *testing.T) {
	if len(validAnalyticsFields) != 12 {
		t.Fatalf("there are %d groupable fields, want 12", len(validAnalyticsFields))
	}
	if len(validAnalyticsYAxis) != 2 {
		t.Fatalf("there are %d measures, want 2", len(validAnalyticsYAxis))
	}
	// A field that can be grouped by is not necessarily one that can be measured, and none of them are.
	for field := range validAnalyticsFields {
		if validAnalyticsYAxis[field] {
			t.Errorf("%q is in both lists, and nothing should be", field)
		}
	}
}

// The serializer renders every column plus the workspace.
func TestAnalyticViewJSONShape(t *testing.T) {
	data := analyticViewJSON(AnalyticView{ID: "view-id"})
	for _, field := range []string{
		"id", "created_at", "updated_at", "created_by", "updated_by", "deleted_at",
		"name", "description", "query", "query_dict", "workspace",
	} {
		if _, present := data[field]; !present {
			t.Errorf("the analytic view is missing %q", field)
		}
	}
	if len(data) != 11 {
		t.Fatalf("the analytic view has %d fields, want 11", len(data))
	}
}

// The query is read-only, so a caller naming it changes nothing.
func TestTheQueryIsNotWritable(t *testing.T) {
	view := AnalyticView{Query: []byte(`{"a":1}`), WorkspaceID: "workspace-id"}
	applyAnalyticViewPayload(&view, map[string]any{
		"name": "Renamed", "query": map[string]any{"b": 2}, "workspace": "elsewhere",
	})
	if string(view.Query) != `{"a":1}` || view.WorkspaceID != "workspace-id" {
		t.Errorf("a read-only field was written: %+v", view)
	}
	if view.Name != "Renamed" {
		t.Error("a writable field was not written")
	}
}
