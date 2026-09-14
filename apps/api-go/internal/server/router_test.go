package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestHealthAndVersionEndpoints(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := NewRouter(Dependencies{CORSOrigins: []string{"http://localhost:3000"}})

	tests := []struct {
		path       string
		statusCode int
		contains   string
	}{
		{path: "/", statusCode: http.StatusOK, contains: `"name":"pace-api"`},
		{path: "/api/health", statusCode: http.StatusOK, contains: `"status":"ok"`},
		{path: "/api/version", statusCode: http.StatusOK, contains: `"runtime":"go"`},
		{path: "/metrics", statusCode: http.StatusOK, contains: "pace_api_up 1"},
		{path: "/api/health/db", statusCode: http.StatusServiceUnavailable, contains: `"status":"unavailable"`},
		{path: "/ready", statusCode: http.StatusServiceUnavailable, contains: `"status":"not_ready"`},
	}

	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != test.statusCode {
				t.Fatalf("status = %d, want %d", response.Code, test.statusCode)
			}
			if !strings.Contains(response.Body.String(), test.contains) {
				t.Fatalf("body %q does not contain %q", response.Body.String(), test.contains)
			}
		})
	}
}

func TestCORSPreflight(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := NewRouter(Dependencies{CORSOrigins: []string{"https://pace.example"}})
	request := httptest.NewRequest(http.MethodOptions, "/api/health", nil)
	request.Header.Set("Origin", "https://pace.example")
	request.Header.Set("Access-Control-Request-Method", http.MethodGet)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want %d", response.Code, http.StatusNoContent)
	}
	if got := response.Header().Get("Access-Control-Allow-Origin"); got != "https://pace.example" {
		t.Fatalf("allow origin = %q", got)
	}
}
