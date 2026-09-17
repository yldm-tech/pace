package project

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestLabelSerializationMatchesTheNarrowFieldList(t *testing.T) {
	project := "project-id"
	parent := "parent-id"
	data := labelJSON(Label{
		ID: "label-id", WorkspaceID: "workspace-id", ProjectID: &project,
		ParentID: &parent, Name: "Bug", Color: "#FF0000", SortOrder: 75535,
		// Neither of these is in LabelSerializer's field list.
		Description: "not serialized", ExternalID: nil,
	})
	if len(data) != 7 {
		t.Fatalf("serialized label has %d fields, want the 7 LabelSerializer lists", len(data))
	}
	for _, field := range []string{"parent", "name", "color", "id", "project_id", "workspace_id", "sort_order"} {
		if _, ok := data[field]; !ok {
			t.Errorf("serialized label is missing %q", field)
		}
	}
	if _, ok := data["description"]; ok {
		t.Error("description is not in LabelSerializer's field list")
	}
	// The serializer uses the id attribute names, not the relation names.
	if data["project_id"] == nil || data["workspace_id"] != "workspace-id" {
		t.Fatalf("serialized label = %#v", data)
	}
}

func TestLabelValidationMatchesDjango(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name    string
		body    string
		partial bool
		want    string
	}{
		{name: "missing name", body: `{}`, want: `{"name":["This field is required."]}`},
		{name: "null name", body: `{"name":null}`, want: `{"name":["This field may not be null."]}`},
		{name: "blank name", body: `{"name":"   "}`, want: `{"name":["This field may not be blank."]}`},
		{name: "long name", body: `{"name":"` + strings.Repeat("n", 256) + `"}`, want: `{"name":["Ensure this field has no more than 255 characters."]}`},
		{name: "null color", body: `{"name":"Bug","color":null}`, want: `{"color":["This field may not be null."]}`},
		{name: "bad parent", body: `{"name":"Bug","parent":"nope"}`, want: `{"parent":["“nope” is not a valid UUID."]}`},
		{name: "bad sort order", body: `{"name":"Bug","sort_order":"first"}`, want: `{"sort_order":["A valid number is required."]}`},
		{name: "partial skips the required name", body: `{"color":"#fff"}`, partial: true, want: ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			router := gin.New()
			handler := NewHandler(nil, nil, Settings{})
			router.POST("/labels", func(c *gin.Context) {
				var body map[string]json.RawMessage
				if err := c.ShouldBindJSON(&body); err != nil {
					handler.invalidDetail(c)
					return
				}
				_, _ = handler.labelFieldsFrom(c, body, test.partial)
			})
			request := httptest.NewRequest(http.MethodPost, "/labels", strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if test.want == "" {
				if response.Code != http.StatusOK {
					t.Fatalf("response = %d %s", response.Code, response.Body.String())
				}
				return
			}
			if response.Code != http.StatusBadRequest || response.Body.String() != test.want {
				t.Fatalf("response = %d %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestLabelRoutesRejectMalformedIdentifiers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewHandler(nil, nil, Settings{}).Register(router)
	for _, path := range []string{
		"/api/workspaces/pace/projects/01234567-89ab-4def-8123-456789abcdef/issue-labels/not-a-uuid/",
		"/api/workspaces/pace/projects/not-a-uuid/issue-labels/",
	} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusNotFound {
			t.Errorf("GET %s status = %d, want %d", path, response.Code, http.StatusNotFound)
		}
	}
}

func TestBulkLabelColourIsAnUppercaseHexTriplet(t *testing.T) {
	for range 20 {
		colour, err := randomLabelColour()
		if err != nil {
			t.Fatal(err)
		}
		// Django formats it as f"#{random.randint(...):06X}".
		if len(colour) != 7 || colour[0] != '#' {
			t.Fatalf("colour = %q", colour)
		}
		if strings.ToUpper(colour) != colour {
			t.Fatalf("colour = %q, want uppercase hex", colour)
		}
		for _, character := range colour[1:] {
			if !strings.ContainsRune("0123456789ABCDEF", character) {
				t.Fatalf("colour = %q", colour)
			}
		}
	}
}
