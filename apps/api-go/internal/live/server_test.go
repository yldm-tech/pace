package live

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func testServer(t *testing.T, config Config) *Server {
	t.Helper()
	gin.SetMode(gin.TestMode)
	if config.BasePath == "" {
		config.BasePath = "/live"
	}
	if config.AppVersion == "" {
		config.AppVersion = "1.0.0"
	}
	if config.APIBaseURL == "" {
		config.APIBaseURL = "http://api.test"
	}
	return NewServer(config, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func do(t *testing.T, server *Server, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(method, path, nil))
	return recorder
}

// TestHealthAnswersUnderBothSpellings covers the trailing slash: Express matches a route with or without one and answers both the same way, where Gin would redirect.
func TestHealthAnswersUnderBothSpellings(t *testing.T) {
	server := testServer(t, Config{AppVersion: "2.3.4"})
	for _, path := range []string{"/live/health", "/live/health/"} {
		response := do(t, server, http.MethodGet, path)
		if response.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200", path, response.Code)
		}
		var body struct {
			Status    string `json:"status"`
			Timestamp string `json:"timestamp"`
			Version   string `json:"version"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s: decode: %v", path, err)
		}
		if body.Status != "OK" || body.Version != "2.3.4" {
			t.Errorf("%s: body = %+v", path, body)
		}
		if len(body.Timestamp) != len("2006-01-02T15:04:05.000Z") {
			t.Errorf("%s: timestamp = %q, want the shape the service it replaces writes", path, body.Timestamp)
		}
	}
}

func TestHealthHonoursTheBasePath(t *testing.T) {
	server := testServer(t, Config{BasePath: "/somewhere-else"})
	if response := do(t, server, http.MethodGet, "/somewhere-else/health"); response.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", response.Code)
	}
	if response := do(t, server, http.MethodGet, "/live/health"); response.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 outside the base path", response.Code)
	}
}

func TestUnknownRouteAnswersNotFound(t *testing.T) {
	server := testServer(t, Config{})
	response := do(t, server, http.MethodGet, "/live/nothing-here")
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", response.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["message"] != "Not Found" {
		t.Errorf("body = %v", body)
	}
}

// TestSecurityHeaders pins every header helmet writes by default, because a client of the live service is a browser and these are what it is told.
func TestSecurityHeaders(t *testing.T) {
	server := testServer(t, Config{})
	response := do(t, server, http.MethodGet, "/live/health")

	for _, pair := range helmetHeaders {
		if got := response.Header().Get(pair[0]); got != pair[1] {
			t.Errorf("%s = %q, want %q", pair[0], got, pair[1])
		}
	}
	// Not set by helmet since version five, and setting it would break the editor's cross-origin assets.
	if got := response.Header().Get("Cross-Origin-Embedder-Policy"); got != "" {
		t.Errorf("Cross-Origin-Embedder-Policy = %q, want it unset", got)
	}
}

func TestSecurityHeadersAreOnEveryResponse(t *testing.T) {
	server := testServer(t, Config{})
	response := do(t, server, http.MethodGet, "/live/nothing-here")
	if got := response.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("a 404 went out without the security headers")
	}
}
