package live

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func request(t *testing.T, server *Server, method, path, origin string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	httpRequest := httptest.NewRequest(method, path, nil)
	if origin != "" {
		httpRequest.Header.Set("Origin", origin)
	}
	server.Handler().ServeHTTP(recorder, httpRequest)
	return recorder
}

func TestAllowedOriginIsReflected(t *testing.T) {
	server := testServer(t, Config{CORSAllowedOrigins: []string{"https://app.test", "https://other.test"}})
	response := request(t, server, http.MethodGet, "/live/health", "https://other.test")
	if got := response.Header().Get("Access-Control-Allow-Origin"); got != "https://other.test" {
		t.Errorf("allow origin = %q, want the request's own origin", got)
	}
	if got := response.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Errorf("allow credentials = %q, want true", got)
	}
	if got := response.Header().Get("Vary"); got != "Origin" {
		t.Errorf("vary = %q, want Origin", got)
	}
}

// TestDisallowedOriginGetsNoHeader covers the shape of the refusal: the request is served, and it is the browser that refuses the answer because no allow-origin header came back.
func TestDisallowedOriginGetsNoHeader(t *testing.T) {
	server := testServer(t, Config{CORSAllowedOrigins: []string{"https://app.test"}})
	response := request(t, server, http.MethodGet, "/live/health", "https://somewhere-else.test")
	if got := response.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("allow origin = %q, want it unset", got)
	}
	if response.Code != http.StatusOK {
		t.Errorf("status = %d, want the request served anyway", response.Code)
	}
	if got := response.Header().Get("Vary"); got != "Origin" {
		t.Errorf("vary = %q, want Origin even on a refusal", got)
	}
}

// TestUnsetOriginsAllowNothing is the default deployment: CORS_ALLOWED_ORIGINS unset splits into one empty entry, which no real origin matches.
func TestUnsetOriginsAllowNothing(t *testing.T) {
	config, err := loadConfig(fromMap(map[string]string{"API_BASE_URL": "http://api.test", "LIVE_SERVER_SECRET_KEY": "secret"}))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	server := testServer(t, config)
	response := request(t, server, http.MethodGet, "/live/health", "https://app.test")
	if got := response.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("allow origin = %q, want it unset", got)
	}
}

// TestPreflightIsAnsweredHere covers the short circuit: an OPTIONS request never reaches a route, and it is answered even for a path nothing serves.
func TestPreflightIsAnsweredHere(t *testing.T) {
	server := testServer(t, Config{CORSAllowedOrigins: []string{"https://app.test"}})
	for _, path := range []string{"/live/health", "/live/nothing-here"} {
		response := request(t, server, http.MethodOptions, path, "https://app.test")
		if response.Code != http.StatusNoContent {
			t.Errorf("%s: status = %d, want 204", path, response.Code)
		}
		if got := response.Header().Get("Content-Length"); got != "0" {
			t.Errorf("%s: content length = %q, want 0", path, got)
		}
		if got := response.Header().Get("Access-Control-Allow-Methods"); got != "GET,POST,PUT,DELETE,OPTIONS" {
			t.Errorf("%s: allow methods = %q", path, got)
		}
		if got := response.Header().Get("Access-Control-Allow-Headers"); got != "Content-Type,Authorization,x-api-key" {
			t.Errorf("%s: allow headers = %q", path, got)
		}
		if response.Body.Len() != 0 {
			t.Errorf("%s: a preflight came back with a body", path)
		}
	}
}
