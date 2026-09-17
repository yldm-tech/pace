package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// TestUUIDPathParametersMatchDjangosConverter covers what <uuid:project_id> does in Django's urls: it is a matcher, so a segment that is not a uuid never reaches a view and the resolver answers 404.
//
// Without it the segment reached the handler and then the database, which ended the request with `invalid input syntax for type uuid (SQLSTATE 22P02)` and a 500 -- reachable from the address bar.
func TestUUIDPathParametersMatchDjangosConverter(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for _, testCase := range []struct {
		name   string
		route  string
		path   string
		status int
	}{
		{
			name:  "a project id that is not a uuid is a route that does not exist",
			route: "/api/public/workspaces/:slug/projects/:project/anchor/",
			path:  "/api/public/workspaces/accounts/projects/sign-in/anchor/", status: http.StatusNotFound,
		},
		{
			name:  "a real project id goes through",
			route: "/api/public/workspaces/:slug/projects/:project/anchor/",
			path:  "/api/public/workspaces/acme/projects/7d691891-5b5c-41d6-a770-11a5a449be1c/anchor/", status: http.StatusOK,
		},
		{
			name:  "slug is a string, and a uuid-shaped one is not required",
			route: "/api/workspaces/:slug/members/",
			path:  "/api/workspaces/not-a-uuid/members/", status: http.StatusOK,
		},
		{
			name:  "anchor is a string too",
			route: "/api/public/anchor/:anchor/settings/",
			path:  "/api/public/anchor/some-anchor/settings/", status: http.StatusOK,
		},
		{
			name:  "a reset token is not a uuid and must still reach its handler",
			route: "/auth/reset-password/:uidb64/:token/",
			path:  "/auth/reset-password/abc/set-password-token/", status: http.StatusOK,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			router := gin.New()
			router.Use(requireUUIDPathParameters())
			router.GET(testCase.route, func(c *gin.Context) { c.Status(http.StatusOK) })

			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, testCase.path, nil))
			if recorder.Code != testCase.status {
				t.Errorf("GET %s = %d, want %d", testCase.path, recorder.Code, testCase.status)
			}
		})
	}
}
