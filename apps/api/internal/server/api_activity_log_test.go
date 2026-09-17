package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

type recordingAPILogs struct{ entries []map[string]any }

func (publisher *recordingAPILogs) PublishAPIActivityLog(_ context.Context, data map[string]any) error {
	publisher.entries = append(publisher.entries, data)
	return nil
}

func runLogged(t *testing.T, request *http.Request) *recordingAPILogs {
	t.Helper()
	gin.SetMode(gin.TestMode)
	publisher := &recordingAPILogs{}
	router := gin.New()
	router.Use(apiActivityLog(publisher, "the-secret"))
	router.POST("/api/v1/workspaces/acme/projects/", func(c *gin.Context) {
		c.JSON(http.StatusCreated, gin.H{"id": "made"})
	})
	router.ServeHTTP(httptest.NewRecorder(), request)
	return publisher
}

// A request with no key is not recorded, which is what keeps the table to the external API.
func TestARequestWithNoKeyIsNotRecorded(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/acme/projects/", strings.NewReader(`{"name":"P"}`))
	if entries := runLogged(t, request).entries; len(entries) != 0 {
		t.Errorf("a request with no key was recorded: %#v", entries)
	}
}

// A request with a key is recorded whole: what was asked, what came back, and who asked.
func TestARequestWithAKeyIsRecorded(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/acme/projects/?expand=x", strings.NewReader(`{"name":"P"}`))
	request.Header.Set("X-Api-Key", "plane_api_secret")
	request.Header.Set("User-Agent", "curl/8")
	request.Header.Set("X-Forwarded-For", "203.0.113.9, 10.0.0.1")
	entries := runLogged(t, request).entries
	if len(entries) != 1 {
		t.Fatalf("recorded %d requests", len(entries))
	}
	entry := entries[0]
	if entry["path"] != "/api/v1/workspaces/acme/projects/" || entry["method"] != "POST" {
		t.Errorf("the record is %#v", entry)
	}
	if entry["query_params"] != "expand=x" {
		t.Errorf("the query is %v", entry["query_params"])
	}
	if entry["response_code"] != 201 {
		t.Errorf("the status is %v", entry["response_code"])
	}
	if entry["body"] != `{"name":"P"}` {
		t.Errorf("the body is %v", entry["body"])
	}
	if !strings.Contains(entry["response_body"].(string), `"id":"made"`) {
		t.Errorf("the response body is %v", entry["response_body"])
	}
	// The first address a proxy named is the one recorded.
	if entry["ip_address"] != "203.0.113.9" {
		t.Errorf("the address is %v", entry["ip_address"])
	}
	if entry["user_agent"] != "curl/8" {
		t.Errorf("the agent is %v", entry["user_agent"])
	}
}

// The key is never written down. What is recorded is a keyed digest of it, so a reader can tell two keys apart without learning either.
func TestTheKeyIsNeverWrittenDown(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/acme/projects/", strings.NewReader(""))
	request.Header.Set("X-Api-Key", "plane_api_secret")
	request.Header.Set("Authorization", "Bearer something")
	request.Header.Set("Cookie", "session=abc")
	entry := runLogged(t, request).entries[0]

	headers := entry["headers"].(string)
	for _, secret := range []string{"plane_api_secret", "Bearer something", "session=abc"} {
		if strings.Contains(headers, secret) {
			t.Errorf("the headers carry %q: %s", secret, headers)
		}
	}
	if strings.Count(headers, "[REDACTED]") != 3 {
		t.Errorf("the three sensitive headers are not all replaced: %s", headers)
	}
	identifier := entry["token_identifier"].(string)
	if len(identifier) != 64 || strings.Contains(identifier, "plane_api") {
		t.Errorf("the identifier is %q", identifier)
	}
	// The same key gives the same identifier, which is what makes a key's requests readable together.
	second := runLogged(t, requestWithKey("plane_api_secret")).entries[0]["token_identifier"]
	if second != identifier {
		t.Error("the same key gave two identifiers")
	}
	other := runLogged(t, requestWithKey("plane_api_other")).entries[0]["token_identifier"]
	if other == identifier {
		t.Error("two keys gave the same identifier")
	}
}

func requestWithKey(key string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/acme/projects/", strings.NewReader(""))
	request.Header.Set("X-Api-Key", key)
	return request
}

// A body that is not text is recorded as a sentence rather than as bytes, since the column is text.
func TestABodyThatIsNotTextIsRecordedAsASentence(t *testing.T) {
	if got := textOrNothing(nil); got != nil {
		t.Errorf("an empty body is %v", got)
	}
	if got := textOrNothing([]byte("plain")); got != "plain" {
		t.Errorf("a text body is %v", got)
	}
	if got := textOrNothing([]byte{0xff, 0xfe}); got != "[Could not decode content]" {
		t.Errorf("an undecodable body is %v", got)
	}
	// The three signatures Django recognises are named rather than decoded, even though two of them would decode.
	for _, payload := range [][]byte{[]byte("\x89PNG\r\n"), {0xff, 0xd8, 0xff, 0xe0}, []byte("%PDF-1.7")} {
		if got := textOrNothing(payload); got != "[Binary Content]" {
			t.Errorf("a binary body is %v", got)
		}
	}
	// A null byte decodes, so it is kept. The worker's insert is what then fails, which is what happens upstream too.
	if got := textOrNothing([]byte("a\x00b")); got != "a\x00b" {
		t.Errorf("a body with a null byte is %q", got)
	}
}

// The headers are written the way a python dict prints, since that is the shape the column already holds.
func TestTheHeadersAreWrittenTheWayPythonPrintsThem(t *testing.T) {
	header := http.Header{}
	header.Set("User-Agent", "curl/8")
	header.Set("X-Api-Key", "plane_api_secret")
	header.Set("X-Note", `it's here`)
	header.Set("X-Path", `C:\tmp`)
	got := renderRequestHeaders(header)
	want := `{'User-Agent': 'curl/8', 'X-Api-Key': '[REDACTED]', 'X-Note': "it's here", 'X-Path': 'C:\\tmp'}`
	if got != want {
		t.Errorf("the headers are\n%s\nrather than\n%s", got, want)
	}
}

// The address a proxy named is taken whole, spaces and all, which is what get_client_ip does.
func TestTheAddressIsTakenWhole(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "198.51.100.4:41000"
	if got := clientIP(request); got != "198.51.100.4" {
		t.Errorf("the address is %v", got)
	}
	request.Header.Set("X-Forwarded-For", " 203.0.113.9 , 10.0.0.1")
	if got := clientIP(request); got != " 203.0.113.9 " {
		t.Errorf("the address is %q", got)
	}
}
