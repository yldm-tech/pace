package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestTheGuards404IsTheSameBytes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(requireUUIDPathParameters())
	router.GET("/api/workspaces/:slug/projects/:project/", func(c *gin.Context) { c.Status(200) })
	router.NoRoute(notFound)

	guard := httptest.NewRecorder()
	router.ServeHTTP(guard, httptest.NewRequest(http.MethodGet, "/api/workspaces/a/projects/nope/", nil))
	miss := httptest.NewRecorder()
	router.ServeHTTP(miss, httptest.NewRequest(http.MethodGet, "/nothing/here/", nil))

	if guard.Body.String() != miss.Body.String() {
		t.Errorf("the guard answers %q, a missing route answers %q", guard.Body.String(), miss.Body.String())
	}
	if guard.Header().Get("Content-Type") != miss.Header().Get("Content-Type") {
		t.Errorf("content types differ: %q vs %q", guard.Header().Get("Content-Type"), miss.Header().Get("Content-Type"))
	}
}
