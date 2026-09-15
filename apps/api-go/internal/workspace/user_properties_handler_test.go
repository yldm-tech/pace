package workspace

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestWorkspaceUserPropertiesValidationAndDefaults(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name string
		body string
		want string
	}{
		{name: "null filters", body: `{"filters":null}`, want: `{"filters":["This field may not be null."]}`},
		{name: "invalid navigation preference", body: `{"navigation_control_preference":"FLOATING"}`, want: `{"navigation_control_preference":["\"FLOATING\" is not a valid choice."]}`},
		{name: "invalid navigation limit", body: `{"navigation_project_limit":"many"}`, want: `{"navigation_project_limit":["A valid integer is required."]}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			router := gin.New()
			handler := NewHandler(nil, nil, nil, Settings{})
			router.PATCH("/properties", func(c *gin.Context) { _, _ = handler.userPropertiesFields(c) })
			request := httptest.NewRequest(http.MethodPatch, "/properties", strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest || response.Body.String() != test.want {
				t.Fatalf("response = %d %s", response.Code, response.Body.String())
			}
		})
	}

	router := gin.New()
	handler := NewHandler(nil, nil, nil, Settings{})
	var parsed userPropertiesInput
	router.PATCH("/properties", func(c *gin.Context) { parsed, _ = handler.userPropertiesFields(c) })
	request := httptest.NewRequest(http.MethodPatch, "/properties", strings.NewReader(`{"navigation_project_limit":"25","navigation_control_preference":"TABBED","rich_filters":{"priority":[]}}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || parsed.navigationProjectLimit != 25 || parsed.navigationPreference != navigationTabbed || string(parsed.richFilters) != `{"priority":[]}` {
		t.Fatalf("parsed properties = %#v, response = %d %s", parsed, response.Code, response.Body.String())
	}

	now := time.Date(2026, time.September, 15, 4, 0, 0, 0, time.UTC)
	data := userPropertiesJSON(WorkspaceUserProperties{
		ID: "properties-id", CreatedAt: now, UpdatedAt: now, WorkspaceID: "workspace-id", UserID: "user-id",
		Filters: defaultWorkspaceFiltersJSON(), DisplayFilters: defaultWorkspaceDisplayFiltersJSON(),
		DisplayProperties: defaultWorkspaceDisplayPropertiesJSON(), RichFilters: emptyJSON(),
		NavigationProjectLimit: 10, NavigationControlPreference: navigationAccordion,
	})
	encoded, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`"workspace":"workspace-id"`, `"user":"user-id"`, `"navigation_project_limit":10`, `"navigation_control_preference":"ACCORDION"`} {
		if !strings.Contains(string(encoded), expected) {
			t.Fatalf("serialized properties %s does not contain %s", encoded, expected)
		}
	}
}
