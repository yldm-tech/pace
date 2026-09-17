package externalapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// Both spellings carry the list and the detail, unlike the attachments where each name has a path of its own.
func TestTheWorkItemRoutesAreOnBothSpellings(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewHandler(nil, Settings{}).Register(router)

	wanted := map[string]bool{
		"GET /api/v1/workspaces/:slug/projects/:project/issues/":            false,
		"GET /api/v1/workspaces/:slug/projects/:project/issues/:issue/":     false,
		"GET /api/v1/workspaces/:slug/projects/:project/work-items/":        false,
		"GET /api/v1/workspaces/:slug/projects/:project/work-items/:issue/": false,
	}
	for _, route := range router.Routes() {
		key := route.Method + " " + route.Path
		if _, listed := wanted[key]; listed {
			wanted[key] = true
		}
	}
	for route, found := range wanted {
		if !found {
			t.Errorf("%s is not served", route)
		}
	}
}

// The serializer reports twenty-nine fields.
func TestExternalIssueSerializerShape(t *testing.T) {
	data := externalIssueJSON(externalIssueRow{ID: "issue-id"})
	if len(data) != 29 {
		t.Fatalf("the work item has %d fields, want 29", len(data))
	}
	for field := range data {
		if !externalIssueFields[field] {
			t.Errorf("%q is rendered but is not one of the serializer's fields", field)
		}
	}
	if len(externalIssueFields) != 29 {
		t.Fatalf("the field list has %d names, want 29", len(externalIssueFields))
	}
}

// The two planning dates and the archive stamp are days, not instants, and the point is an integer rather than a float.
func TestTheWorkItemDatesAndPointAreNotInstantsOrFloats(t *testing.T) {
	day := time.Date(2026, 3, 4, 0, 0, 0, 0, time.UTC)
	point := int64(3)
	data := externalIssueJSON(externalIssueRow{
		ID: "issue-id", StartDate: &day, TargetDate: &day, ArchivedAt: &day, Point: &point,
	})
	for _, field := range []string{"start_date", "target_date", "archived_at"} {
		if got, ok := data[field].(string); !ok || got != "2026-03-04" {
			t.Errorf("%s is %v, want the day alone", field, data[field])
		}
	}
	// A float would be rewritten as 3.0 on its way out, which is what Respond does to every float it walks.
	if _, isFloat := data["point"].(*float64); isFloat {
		t.Error("the point is a float, and a float renders with a decimal point the integer column never had")
	}
	if got, ok := data["point"].(*int64); !ok || *got != 3 {
		t.Errorf("the point is %v, want the integer 3", data["point"])
	}
}

// requestWithQuery is a context carrying a query string and nothing else.
func requestWithQuery(t *testing.T, query string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/?"+query, nil)
	return c
}

// A relation that is null expands to an empty object rather than to null, which is what DRF makes of a serializer with no instance.
func TestANullRelationExpandsToAnEmptyObject(t *testing.T) {
	records := map[string]gin.H{"state-id": {"id": "state-id"}}
	if got := lookup(records, nil); len(got.(gin.H)) != 0 {
		t.Errorf("a null relation expands to %v, want the empty object", got)
	}
	missing := "gone"
	if got := lookup(records, &missing); len(got.(gin.H)) != 0 {
		t.Errorf("a relation whose row is gone expands to %v, want the empty object", got)
	}
	present := "state-id"
	if got := lookup(records, &present); len(got.(gin.H)) != 1 {
		t.Errorf("a relation that is there expands to %v", got)
	}
}

// expand only looks at names the serializer declares, and a declared name the expansion table does not know lands as null.
func TestExpandIgnoresWhatTheSerializerDoesNotDeclare(t *testing.T) {
	handler := NewHandler(nil, Settings{})
	rows := []externalIssueRow{{ID: "issue-id"}}
	bodies := []gin.H{externalIssueJSON(rows[0])}
	err := handler.expandIssues(requestWithQuery(t, ""), rows, bodies, []string{"cycle", "module", "name"})
	if err != nil {
		t.Fatal(err)
	}
	for _, absent := range []string{"cycle", "module"} {
		if _, present := bodies[0][absent]; present {
			t.Errorf("%q was expanded although the serializer does not declare it", absent)
		}
	}
	if bodies[0]["name"] != nil {
		t.Errorf("name expands to %v, want null", bodies[0]["name"])
	}
}

// The type is the one declared name with no serializer behind it, and it keeps the id it already carried.
func TestExpandingTheTypeKeepsItsID(t *testing.T) {
	handler := NewHandler(nil, Settings{})
	typeID := "type-id"
	rows := []externalIssueRow{{ID: "issue-id", TypeID: &typeID}}
	bodies := []gin.H{externalIssueJSON(rows[0])}
	if err := handler.expandIssues(requestWithQuery(t, ""), rows, bodies, []string{"type"}); err != nil {
		t.Fatal(err)
	}
	if bodies[0]["type"] != &typeID {
		t.Errorf("the type expands to %v, want the id it already carried", bodies[0]["type"])
	}
}

// The expand parameter is read exactly as fields is.
func TestExpandIsSplitOnCommas(t *testing.T) {
	c := requestWithQuery(t, "expand=state,,labels")
	got := expandedFields(c)
	if len(got) != 2 || got[0] != "state" || got[1] != "labels" {
		t.Fatalf("expand parsed as %v", got)
	}
	if len(expandedFields(requestWithQuery(t, ""))) != 0 {
		t.Error("an absent expand is not empty")
	}
}

// The lite serializers are their own shapes rather than the whole record.
func TestTheExpansionSerializerShapes(t *testing.T) {
	point := estimatePointJSON(EstimatePoint{ID: "point-id"})
	if len(point) != 12 {
		t.Errorf("the estimate point has %d fields, want 12", len(point))
	}
	for _, field := range []string{"id", "created_at", "updated_at", "deleted_at", "key", "description", "value", "created_by", "updated_by", "project", "workspace", "estimate"} {
		if _, present := point[field]; !present {
			t.Errorf("the estimate point is missing %q", field)
		}
	}
}
