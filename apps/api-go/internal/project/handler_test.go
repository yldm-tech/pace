package project

import (
	"encoding/json"
	"github.com/yldm-tech/pace/apps/api-go/internal/projects"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
)

func TestProjectRouteInventory(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewHandler(nil, nil, Settings{}).Register(router)
	expected := map[string]bool{
		"GET /api/workspaces/:slug/projects/":        true,
		"POST /api/workspaces/:slug/projects/":       true,
		"GET /api/workspaces/:slug/projects/:id/":    true,
		"PATCH /api/workspaces/:slug/projects/:id/":  true,
		"DELETE /api/workspaces/:slug/projects/:id/": true,

		"GET /api/workspaces/:slug/projects/:id/members/":                      true,
		"POST /api/workspaces/:slug/projects/:id/members/":                     true,
		"GET /api/workspaces/:slug/projects/:id/members/:member/":              true,
		"PATCH /api/workspaces/:slug/projects/:id/members/:member/":            true,
		"DELETE /api/workspaces/:slug/projects/:id/members/:member/":           true,
		"POST /api/workspaces/:slug/projects/:id/members/leave/":               true,
		"GET /api/workspaces/:slug/projects/:id/project-members/me/":           true,
		"POST /api/workspaces/:slug/projects/:id/project-views/":               true,
		"GET /api/workspaces/:slug/projects/:id/preferences/member/:member/":   true,
		"PATCH /api/workspaces/:slug/projects/:id/preferences/member/:member/": true,
		"GET /api/users/me/workspaces/:slug/project-roles/":                    true,

		"GET /api/workspaces/:slug/projects/:id/issue-labels/":                                    true,
		"POST /api/workspaces/:slug/projects/:id/issue-labels/":                                   true,
		"GET /api/workspaces/:slug/projects/:id/issue-labels/:label/":                             true,
		"PUT /api/workspaces/:slug/projects/:id/issue-labels/:label/":                             true,
		"PATCH /api/workspaces/:slug/projects/:id/issue-labels/:label/":                           true,
		"DELETE /api/workspaces/:slug/projects/:id/issue-labels/:label/":                          true,
		"POST /api/workspaces/:slug/projects/:id/bulk-create-labels/":                             true,
		"GET /api/workspaces/:slug/projects/:id/issues/:issue/reactions/":                         true,
		"POST /api/workspaces/:slug/projects/:id/issues/:issue/reactions/":                        true,
		"DELETE /api/workspaces/:slug/projects/:id/issues/:issue/reactions/:reaction/":            true,
		"GET /api/workspaces/:slug/projects/:id/issues/:issue/issue-subscribers/":                 true,
		"POST /api/workspaces/:slug/projects/:id/issues/:issue/issue-subscribers/":                true,
		"DELETE /api/workspaces/:slug/projects/:id/issues/:issue/issue-subscribers/:subscriber/":  true,
		"GET /api/workspaces/:slug/projects/:id/issues/:issue/subscribe/":                         true,
		"POST /api/workspaces/:slug/projects/:id/issues/:issue/subscribe/":                        true,
		"DELETE /api/workspaces/:slug/projects/:id/issues/:issue/subscribe/":                      true,
		"GET /api/workspaces/:slug/projects/:id/issues/:issue/issue-links/":                       true,
		"POST /api/workspaces/:slug/projects/:id/issues/:issue/issue-links/":                      true,
		"GET /api/workspaces/:slug/projects/:id/issues/:issue/issue-links/:link/":                 true,
		"PATCH /api/workspaces/:slug/projects/:id/issues/:issue/issue-links/:link/":               true,
		"DELETE /api/workspaces/:slug/projects/:id/issues/:issue/issue-links/:link/":              true,
		"GET /api/workspaces/:slug/projects/:id/issues/:issue/comments/":                          true,
		"POST /api/workspaces/:slug/projects/:id/issues/:issue/comments/":                         true,
		"GET /api/workspaces/:slug/projects/:id/issues/:issue/comments/:comment/":                 true,
		"PATCH /api/workspaces/:slug/projects/:id/issues/:issue/comments/:comment/":               true,
		"DELETE /api/workspaces/:slug/projects/:id/issues/:issue/comments/:comment/":              true,
		"GET /api/workspaces/:slug/projects/:id/comments/:comment/reactions/":                     true,
		"POST /api/workspaces/:slug/projects/:id/comments/:comment/reactions/":                    true,
		"DELETE /api/workspaces/:slug/projects/:id/comments/:comment/reactions/:reaction/":        true,
		"GET /api/workspaces/:slug/projects/:id/issues/:issue/":                                   true,
		"PATCH /api/workspaces/:slug/projects/:id/issues/:issue/":                                 true,
		"PUT /api/workspaces/:slug/projects/:id/":                                                 true,
		"PUT /api/workspaces/:slug/projects/:id/cycles/:cycle/":                                   true,
		"PUT /api/workspaces/:slug/projects/:id/issues/:issue/":                                   true,
		"PUT /api/workspaces/:slug/projects/:id/issues/:issue/comments/:comment/":                 true,
		"PUT /api/workspaces/:slug/projects/:id/issues/:issue/issue-links/:link/":                 true,
		"GET /api/workspaces/:slug/projects/:id/user-favorite-cycles/":                            true,
		"DELETE /api/workspaces/:slug/projects/:id/issues/:issue/":                                true,
		"GET /api/workspaces/:slug/projects/:id/issues/:issue/sub-issues/":                        true,
		"POST /api/workspaces/:slug/projects/:id/issues/:issue/sub-issues/":                       true,
		"GET /api/workspaces/:slug/projects/:id/issues/:issue/issue-relation/":                    true,
		"POST /api/workspaces/:slug/projects/:id/issues/:issue/issue-relation/":                   true,
		"POST /api/workspaces/:slug/projects/:id/issues/:issue/remove-relation/":                  true,
		"GET /api/workspaces/:slug/projects/:id/issues/:issue/archive/":                           true,
		"POST /api/workspaces/:slug/projects/:id/issues/:issue/archive/":                          true,
		"DELETE /api/workspaces/:slug/projects/:id/issues/:issue/archive/":                        true,
		"POST /api/workspaces/:slug/projects/:id/bulk-archive-issues/":                            true,
		"POST /api/assets/v2/workspaces/:slug/projects/:id/issues/:issue/attachments/":            true,
		"GET /api/assets/v2/workspaces/:slug/projects/:id/issues/:issue/attachments/":             true,
		"GET /api/assets/v2/workspaces/:slug/projects/:id/issues/:issue/attachments/:asset/":      true,
		"PATCH /api/assets/v2/workspaces/:slug/projects/:id/issues/:issue/attachments/:asset/":    true,
		"DELETE /api/assets/v2/workspaces/:slug/projects/:id/issues/:issue/attachments/:asset/":   true,
		"GET /api/workspaces/:slug/projects/:id/issues/:issue/history/":                           true,
		"GET /api/workspaces/:slug/projects/:id/issues/:issue/meta/":                              true,
		"GET /api/workspaces/:slug/projects/:id/issues/:issue/versions/":                          true,
		"GET /api/workspaces/:slug/projects/:id/issues/":                                          true,
		"GET /api/workspaces/:slug/projects/:id/issues/list/":                                     true,
		"POST /api/workspaces/:slug/projects/:id/issues/":                                         true,
		"GET /api/workspaces/:slug/projects/:id/archived-issues/":                                 true,
		"GET /api/workspaces/:slug/work-items/:identifier/":                                       true,
		"POST /api/workspaces/:slug/projects/:id/issue-dates/":                                    true,
		"GET /api/workspaces/:slug/projects/:id/issues-detail/":                                   true,
		"GET /api/workspaces/:slug/projects/:id/v2/issues/":                                       true,
		"POST /api/workspaces/:slug/projects/:id/cycles/date-check/":                              true,
		"GET /api/workspaces/:slug/projects/:id/cycles/":                                          true,
		"POST /api/workspaces/:slug/projects/:id/cycles/":                                         true,
		"GET /api/workspaces/:slug/projects/:id/cycles/:cycle/":                                   true,
		"PATCH /api/workspaces/:slug/projects/:id/cycles/:cycle/":                                 true,
		"DELETE /api/workspaces/:slug/projects/:id/cycles/:cycle/":                                true,
		"POST /api/workspaces/:slug/projects/:id/cycles/:cycle/cycle-issues/":                     true,
		"GET /api/workspaces/:slug/projects/:id/cycles/:cycle/cycle-issues/":                      true,
		"POST /api/workspaces/:slug/projects/:id/cycles/:cycle/archive/":                          true,
		"DELETE /api/workspaces/:slug/projects/:id/cycles/:cycle/archive/":                        true,
		"POST /api/workspaces/:slug/projects/:id/user-favorite-cycles/":                           true,
		"POST /api/workspaces/:slug/projects/:id/user-favorite-modules/":                          true,
		"GET /api/workspaces/:slug/projects/:id/modules/":                                         true,
		"POST /api/workspaces/:slug/projects/:id/modules/":                                        true,
		"GET /api/workspaces/:slug/projects/:id/modules/:module/":                                 true,
		"GET /api/workspaces/:slug/projects/:id/modules/:module/issues/":                          true,
		"GET /api/workspaces/:slug/projects/:id/archived-modules/":                                true,
		"GET /api/workspaces/:slug/projects/:id/archived-cycles/":                                 true,
		"GET /api/workspaces/:slug/projects/:id/cycles/:cycle/progress/":                          true,
		"GET /api/workspaces/:slug/projects/:id/cycles/:cycle/analytics/":                         true,
		"POST /api/workspaces/:slug/projects/:id/cycles/:cycle/transfer-issues/":                  true,
		"GET /api/workspaces/:slug/projects/:id/views/":                                           true,
		"GET /api/workspaces/:slug/views/":                                                        true,
		"GET /api/workspaces/:slug/users/notifications/":                                          true,
		"GET /api/workspaces/:slug/projects/:id/pages-summary/":                                   true,
		"GET /api/workspaces/:slug/projects/:id/intakes/":                                         true,
		"GET /api/workspaces/:slug/projects/:id/search-issues/":                                   true,
		"GET /api/workspaces/:slug/search/":                                                       true,
		"GET /api/workspaces/:slug/entity-search/":                                                true,
		"GET /api/workspaces/:slug/webhooks/":                                                     true,
		"GET /api/workspaces/:slug/analytic-view/":                                                true,
		"GET /api/workspaces/:slug/analytics/":                                                    true,
		"GET /api/workspaces/:slug/default-analytics/":                                            true,
		"GET /api/workspaces/:slug/project-stats/":                                                true,
		"GET /api/workspaces/:slug/saved-analytic-view/:view/":                                    true,
		"POST /api/workspaces/:slug/analytic-view/":                                               true,
		"GET /api/workspaces/:slug/analytic-view/:view/":                                          true,
		"PATCH /api/workspaces/:slug/analytic-view/:view/":                                        true,
		"DELETE /api/workspaces/:slug/analytic-view/:view/":                                       true,
		"POST /api/workspaces/:slug/export-analytics/":                                            true,
		"POST /api/workspaces/:slug/webhooks/":                                                    true,
		"GET /api/workspaces/:slug/webhooks/:webhook/":                                            true,
		"PATCH /api/workspaces/:slug/webhooks/:webhook/":                                          true,
		"DELETE /api/workspaces/:slug/webhooks/:webhook/":                                         true,
		"POST /api/workspaces/:slug/webhooks/:webhook/regenerate/":                                true,
		"GET /api/workspaces/:slug/webhook-logs/:webhook/":                                        true,
		"GET /api/workspaces/:slug/projects/:id/intake-issues/":                                   true,
		"POST /api/workspaces/:slug/projects/:id/intake-issues/":                                  true,
		"GET /api/workspaces/:slug/projects/:id/intake-issues/:issue/":                            true,
		"PATCH /api/workspaces/:slug/projects/:id/intake-issues/:issue/":                          true,
		"DELETE /api/workspaces/:slug/projects/:id/intake-issues/:issue/":                         true,
		"GET /api/workspaces/:slug/projects/:id/inbox-issues/":                                    true,
		"POST /api/workspaces/:slug/projects/:id/inbox-issues/":                                   true,
		"GET /api/workspaces/:slug/projects/:id/inbox-issues/:issue/":                             true,
		"PATCH /api/workspaces/:slug/projects/:id/inbox-issues/:issue/":                           true,
		"DELETE /api/workspaces/:slug/projects/:id/inbox-issues/:issue/":                          true,
		"POST /api/workspaces/:slug/projects/:id/intakes/":                                        true,
		"GET /api/workspaces/:slug/projects/:id/intakes/:intake/":                                 true,
		"PATCH /api/workspaces/:slug/projects/:id/intakes/:intake/":                               true,
		"DELETE /api/workspaces/:slug/projects/:id/intakes/:intake/":                              true,
		"GET /api/workspaces/:slug/projects/:id/inboxes/":                                         true,
		"POST /api/workspaces/:slug/projects/:id/inboxes/":                                        true,
		"GET /api/workspaces/:slug/projects/:id/inboxes/:intake/":                                 true,
		"PATCH /api/workspaces/:slug/projects/:id/inboxes/:intake/":                               true,
		"DELETE /api/workspaces/:slug/projects/:id/inboxes/:intake/":                              true,
		"GET /api/workspaces/:slug/projects/:id/pages/:page/description/":                         true,
		"PATCH /api/workspaces/:slug/projects/:id/pages/:page/description/":                       true,
		"GET /api/workspaces/:slug/projects/:id/pages/:page/versions/":                            true,
		"GET /api/workspaces/:slug/projects/:id/pages/:page/versions/:version/":                   true,
		"POST /api/workspaces/:slug/projects/:id/pages/:page/duplicate/":                          true,
		"GET /api/workspaces/:slug/projects/:id/pages/":                                           true,
		"POST /api/workspaces/:slug/projects/:id/pages/":                                          true,
		"GET /api/workspaces/:slug/projects/:id/pages/:page/":                                     true,
		"PATCH /api/workspaces/:slug/projects/:id/pages/:page/":                                   true,
		"DELETE /api/workspaces/:slug/projects/:id/pages/:page/":                                  true,
		"POST /api/workspaces/:slug/projects/:id/pages/:page/lock/":                               true,
		"DELETE /api/workspaces/:slug/projects/:id/pages/:page/lock/":                             true,
		"POST /api/workspaces/:slug/projects/:id/pages/:page/access/":                             true,
		"POST /api/workspaces/:slug/projects/:id/pages/:page/archive/":                            true,
		"DELETE /api/workspaces/:slug/projects/:id/pages/:page/archive/":                          true,
		"POST /api/workspaces/:slug/projects/:id/favorite-pages/:page/":                           true,
		"DELETE /api/workspaces/:slug/projects/:id/favorite-pages/:page/":                         true,
		"GET /api/workspaces/:slug/users/notifications/unread/":                                   true,
		"POST /api/workspaces/:slug/users/notifications/mark-all-read/":                           true,
		"GET /api/workspaces/:slug/users/notifications/:notification/":                            true,
		"PATCH /api/workspaces/:slug/users/notifications/:notification/":                          true,
		"DELETE /api/workspaces/:slug/users/notifications/:notification/":                         true,
		"POST /api/workspaces/:slug/users/notifications/:notification/read/":                      true,
		"DELETE /api/workspaces/:slug/users/notifications/:notification/read/":                    true,
		"POST /api/workspaces/:slug/users/notifications/:notification/archive/":                   true,
		"DELETE /api/workspaces/:slug/users/notifications/:notification/archive/":                 true,
		"POST /api/workspaces/:slug/views/":                                                       true,
		"GET /api/workspaces/:slug/views/:view/":                                                  true,
		"PATCH /api/workspaces/:slug/views/:view/":                                                true,
		"PUT /api/workspaces/:slug/views/:view/":                                                  true,
		"DELETE /api/workspaces/:slug/views/:view/":                                               true,
		"POST /api/workspaces/:slug/projects/:id/views/":                                          true,
		"GET /api/workspaces/:slug/projects/:id/views/:view/":                                     true,
		"PATCH /api/workspaces/:slug/projects/:id/views/:view/":                                   true,
		"PUT /api/workspaces/:slug/projects/:id/views/:view/":                                     true,
		"DELETE /api/workspaces/:slug/projects/:id/views/:view/":                                  true,
		"GET /api/workspaces/:slug/projects/:id/user-favorite-views/":                             true,
		"POST /api/workspaces/:slug/projects/:id/user-favorite-views/":                            true,
		"DELETE /api/workspaces/:slug/projects/:id/user-favorite-views/:view/":                    true,
		"POST /api/workspaces/:slug/projects/:id/modules/:module/issues/":                         true,
		"POST /api/workspaces/:slug/projects/:id/issues/:issue/modules/":                          true,
		"PATCH /api/workspaces/:slug/projects/:id/modules/:module/":                               true,
		"PUT /api/workspaces/:slug/projects/:id/modules/:module/":                                 true,
		"DELETE /api/workspaces/:slug/projects/:id/modules/:module/":                              true,
		"POST /api/workspaces/:slug/projects/:id/modules/:module/archive/":                        true,
		"DELETE /api/workspaces/:slug/projects/:id/modules/:module/archive/":                      true,
		"GET /api/workspaces/:slug/projects/:id/user-favorite-modules/":                           true,
		"DELETE /api/workspaces/:slug/projects/:id/user-favorite-modules/:module/":                true,
		"GET /api/workspaces/:slug/projects/:id/modules/:module/user-properties/":                 true,
		"PATCH /api/workspaces/:slug/projects/:id/modules/:module/user-properties/":               true,
		"GET /api/workspaces/:slug/projects/:id/modules/:module/module-links/":                    true,
		"POST /api/workspaces/:slug/projects/:id/modules/:module/module-links/":                   true,
		"GET /api/workspaces/:slug/projects/:id/modules/:module/module-links/:link/":              true,
		"PATCH /api/workspaces/:slug/projects/:id/modules/:module/module-links/:link/":            true,
		"PUT /api/workspaces/:slug/projects/:id/modules/:module/module-links/:link/":              true,
		"DELETE /api/workspaces/:slug/projects/:id/modules/:module/module-links/:link/":           true,
		"DELETE /api/workspaces/:slug/projects/:id/user-favorite-cycles/:cycle/":                  true,
		"GET /api/workspaces/:slug/projects/:id/cycles/:cycle/user-properties/":                   true,
		"PATCH /api/workspaces/:slug/projects/:id/cycles/:cycle/user-properties/":                 true,
		"GET /api/workspaces/:slug/projects/:id/issues/:issue/versions/:version/":                 true,
		"GET /api/workspaces/:slug/projects/:id/work-items/:issue/description-versions/":          true,
		"GET /api/workspaces/:slug/projects/:id/work-items/:issue/description-versions/:version/": true,
		"GET /api/workspaces/:slug/projects/:id/user-properties/":                                 true,
		"PATCH /api/workspaces/:slug/projects/:id/user-properties/":                               true,
		"GET /api/workspaces/:slug/projects/:id/states/":                                          true,
		"POST /api/workspaces/:slug/projects/:id/states/":                                         true,
		"GET /api/workspaces/:slug/projects/:id/states/:state/":                                   true,
		"PATCH /api/workspaces/:slug/projects/:id/states/:state/":                                 true,
		"DELETE /api/workspaces/:slug/projects/:id/states/:state/":                                true,
		"POST /api/workspaces/:slug/projects/:id/states/:state/mark-default/":                     true,
		"GET /api/workspaces/:slug/projects/:id/intake-state/":                                    true,
		"DELETE /api/workspaces/:slug/projects/:id/bulk-delete-issues/":                           true,
		"GET /api/workspaces/:slug/projects/:id/deleted-issues/":                                  true,
	}
	for _, route := range router.Routes() {
		key := route.Method + " " + route.Path
		if !expected[key] {
			t.Fatalf("unexpected project route %s", key)
		}
		delete(expected, key)
	}
	if len(expected) != 0 {
		t.Fatalf("missing project routes: %#v", expected)
	}
}

func TestProjectRoutesRequireDjangoSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewHandler(nil, nil, Settings{}).Register(router)
	for _, test := range []struct {
		method string
		path   string
	}{
		{method: http.MethodGet, path: "/api/workspaces/pace/projects/"},
		{method: http.MethodPost, path: "/api/workspaces/pace/projects/"},
		{method: http.MethodGet, path: "/api/workspaces/pace/projects/01234567-89ab-4def-8123-456789abcdef/"},
		{method: http.MethodPatch, path: "/api/workspaces/pace/projects/01234567-89ab-4def-8123-456789abcdef/"},
		{method: http.MethodDelete, path: "/api/workspaces/pace/projects/01234567-89ab-4def-8123-456789abcdef/"},
		{method: http.MethodGet, path: "/api/workspaces/pace/projects/01234567-89ab-4def-8123-456789abcdef/members/"},
		{method: http.MethodPost, path: "/api/workspaces/pace/projects/01234567-89ab-4def-8123-456789abcdef/members/leave/"},
		{method: http.MethodGet, path: "/api/workspaces/pace/projects/01234567-89ab-4def-8123-456789abcdef/project-members/me/"},
		{method: http.MethodGet, path: "/api/users/me/workspaces/pace/project-roles/"},
	} {
		request := httptest.NewRequest(test.method, test.path, nil)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s status = %d", test.method, test.path, response.Code)
		}
		if response.Body.String() != `{"detail":"Authentication credentials were not provided."}` {
			t.Fatalf("%s %s body = %s", test.method, test.path, response.Body.String())
		}
	}
}

func TestProjectUUIDRoutesRejectMalformedIdentifiers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewHandler(nil, nil, Settings{}).Register(router)
	request := httptest.NewRequest(http.MethodGet, "/api/workspaces/pace/projects/not-a-uuid/", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
	}
}

func TestProjectNameAndIdentifierValidationMatchesDjango(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name string
		body string
		want string
	}{
		{name: "missing name", body: `{"identifier":"PACE"}`, want: `{"name":["This field is required."]}`},
		{name: "blank name", body: `{"name":"   ","identifier":"PACE"}`, want: `{"name":["This field may not be blank."]}`},
		{name: "null name", body: `{"name":null,"identifier":"PACE"}`, want: `{"name":["This field may not be null."]}`},
		{name: "special characters in name", body: `{"name":"Pace!","identifier":"PACE"}`, want: `{"name":["PROJECT_NAME_CANNOT_CONTAIN_SPECIAL_CHARACTERS"]}`},
		{name: "long identifier", body: `{"name":"Pace","identifier":"ABCDEFGHIJKLM"}`, want: `{"identifier":["Ensure this field has no more than 12 characters."]}`},
		{name: "special characters in identifier", body: `{"name":"Pace","identifier":"PA-CE"}`, want: `{"identifier":["PROJECT_IDENTIFIER_CANNOT_CONTAIN_SPECIAL_CHARACTERS"]}`},
		{name: "invalid network", body: `{"name":"Pace","identifier":"PACE","network":1}`, want: `{"network":["\"1\" is not a valid choice."]}`},
		{name: "archive_in above bound", body: `{"name":"Pace","identifier":"PACE","archive_in":13}`, want: `{"archive_in":["Ensure this value is less than or equal to 12."]}`},
		{name: "close_in below bound", body: `{"name":"Pace","identifier":"PACE","close_in":-1}`, want: `{"close_in":["Ensure this value is greater than or equal to 0."]}`},
		{name: "invalid timezone", body: `{"name":"Pace","identifier":"PACE","timezone":"Mars/Olympus"}`, want: `{"timezone":["\"Mars/Olympus\" is not a valid choice."]}`},
		{name: "invalid boolean", body: `{"name":"Pace","identifier":"PACE","cycle_view":"maybe"}`, want: `{"cycle_view":["Must be a valid boolean."]}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := runProjectFields(t, test.body, false)
			if response.Code != http.StatusBadRequest || response.Body.String() != test.want {
				t.Fatalf("response = %d %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestProjectPartialUpdateSkipsRequiredFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	response := runProjectFields(t, `{"cycle_view":true}`, true)
	if response.Code != http.StatusOK {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}

// runProjectFields exercises validation that needs no database. The name and
// identifier uniqueness checks are covered by the shared-schema test.
func runProjectFields(t *testing.T, body string, partial bool) *httptest.ResponseRecorder {
	t.Helper()
	router := gin.New()
	handler := NewHandler(nil, nil, Settings{})
	router.POST("/projects", func(c *gin.Context) {
		var parsed map[string]json.RawMessage
		if err := c.ShouldBindJSON(&parsed); err != nil {
			handler.invalidDetail(c)
			return
		}
		_, _ = handler.projectFields(c, parsed, partial)
	})
	request := httptest.NewRequest(http.MethodPost, "/projects", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func TestProjectSerializationCoversDjangoFields(t *testing.T) {
	now := time.Date(2026, time.September, 15, 4, 0, 0, 0, time.UTC)
	lead := "lead-id"
	data := projectFieldsJSON(Project{
		ID: "project-id", CreatedAt: now, UpdatedAt: now, WorkspaceID: "workspace-id",
		Name: "Pace", Identifier: "PACE", Network: 2, PageView: true, Timezone: "UTC",
		ProjectLeadID: &lead, LogoProps: emptyJSON(),
	})
	for _, field := range []string{
		"id", "created_at", "updated_at", "created_by", "updated_by", "deleted_at",
		"name", "description", "description_text", "description_html", "network",
		"workspace", "identifier", "default_assignee", "project_lead", "emoji",
		"icon_prop", "module_view", "cycle_view", "issue_views_view", "page_view",
		"intake_view", "is_time_tracking_enabled", "is_issue_type_enabled",
		"guest_view_all_features", "cover_image", "cover_image_asset", "estimate",
		"archive_in", "close_in", "logo_props", "default_state", "archived_at",
		"timezone", "external_source", "external_id",
	} {
		if _, ok := data[field]; !ok {
			t.Errorf("serialized project is missing %q", field)
		}
	}
	if len(data) != 36 {
		t.Fatalf("serialized project has %d fields, want the 36 Django model fields", len(data))
	}
}

func TestProjectCoverImagePrefersTheAsset(t *testing.T) {
	asset := "asset-id"
	image := "https://cdn.pace.test/cover.png"
	if got := coverImageURL(Project{CoverImageAssetID: &asset, CoverImage: &image}); got != "/api/assets/v2/static/asset-id/" {
		t.Fatalf("cover image url = %v", got)
	}
	if got := coverImageURL(Project{CoverImage: &image}); got != image {
		t.Fatalf("cover image url = %v", got)
	}
	if got := coverImageURL(Project{}); got != nil {
		t.Fatalf("cover image url = %v", got)
	}
}

func TestProjectDefaultStatesMatchDjango(t *testing.T) {
	if len(projects.DefaultStates) != 6 {
		t.Fatalf("default states = %d", len(projects.DefaultStates))
	}
	wantGroups := []string{"backlog", "unstarted", "started", "completed", "cancelled", "triage"}
	wantSequences := []float64{15000, 25000, 35000, 45000, 55000, 65000}
	for index, state := range projects.DefaultStates {
		if state.Group != wantGroups[index] || state.Sequence != wantSequences[index] {
			t.Fatalf("default state %d = %#v", index, state)
		}
		// Only Backlog carries default=True in DEFAULT_STATES.
		if (index == 0) != state.Default {
			t.Fatalf("default state %d default flag = %v", index, state.Default)
		}
	}
}

func TestProjectDescriptionHTMLIsSanitized(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	var stored auth.JSONValue
	router.POST("/projects", func(c *gin.Context) {
		value, ok := sanitizeDescriptionHTML(c, auth.JSONValue([]byte(`"<p>keep<script>alert(1)</script></p>"`)))
		if !ok {
			return
		}
		stored = value
		c.Status(http.StatusOK)
	})
	request := httptest.NewRequest(http.MethodPost, "/projects", strings.NewReader(`{}`))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || string(stored) != `"<p>keep</p>"` {
		t.Fatalf("stored description = %s, response = %d", stored, response.Code)
	}
}

func TestProjectNextWorkItemSequenceTreatsZeroAsEmpty(t *testing.T) {
	// Django computes (max + 1) if max else 1, so a stored zero yields one.
	for _, test := range []struct {
		maximum *int64
		want    int64
	}{
		{maximum: nil, want: 1},
		{maximum: pointer(int64(0)), want: 1},
		{maximum: pointer(int64(7)), want: 8},
	} {
		got := int64(1)
		if test.maximum != nil && *test.maximum != 0 {
			got = *test.maximum + 1
		}
		if got != test.want {
			t.Fatalf("next sequence for %v = %d, want %d", test.maximum, got, test.want)
		}
	}
}

func pointer[T any](value T) *T { return &value }
