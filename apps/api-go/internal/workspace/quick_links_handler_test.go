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

func TestQuickLinkValidationMatchesDjangoContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name    string
		body    string
		partial bool
		want    string
	}{
		{name: "missing url", body: `{}`, want: `{"url":["This field is required."]}`},
		{name: "null url", body: `{"url":null}`, want: `{"url":["This field may not be null."]}`},
		{name: "blank url", body: `{"url":""}`, want: `{"url":["This field may not be blank."]}`},
		{name: "non string url", body: `{"url":12}`, want: `{"url":["Not a valid string."]}`},
		{name: "invalid url", body: `{"url":"not a url"}`, want: `{"url":{"error":"Invalid URL format."}}`},
		{name: "null metadata", body: `{"url":"example.com","metadata":null}`, want: `{"metadata":["This field may not be null."]}`},
		{name: "long title", body: `{"url":"example.com","title":"` + strings.Repeat("t", 256) + `"}`, want: `{"title":["Ensure this field has no more than 255 characters."]}`},
		{name: "bad project", body: `{"url":"example.com","project":"nope"}`, want: `{"project":["“nope” is not a valid UUID."]}`},
		{name: "bad deleted_at", body: `{"url":"example.com","deleted_at":"yesterday"}`, want: `{"deleted_at":["Datetime has wrong format. Use one of these formats instead: YYYY-MM-DDThh:mm[:ss[.uuuuuu]][+HH:MM|-HH:MM|Z]."]}`},
		{name: "partial skips required url", body: `{"title":"Docs"}`, partial: true, want: ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			router := gin.New()
			handler := NewHandler(nil, nil, nil, Settings{})
			router.POST("/links", func(c *gin.Context) { _, _ = handler.quickLinkFields(c, test.partial) })
			request := httptest.NewRequest(http.MethodPost, "/links", strings.NewReader(test.body))
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

func TestQuickLinkAcceptedPayloadIsNormalized(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewHandler(nil, nil, nil, Settings{})
	var parsed quickLinkInput
	router.POST("/links", func(c *gin.Context) { parsed, _ = handler.quickLinkFields(c, false) })
	body := `{"url":"example.com/docs","title":"  Docs  ","metadata":{"icon":"book"},"project":"01234567-89AB-4DEF-8123-456789ABCDEF"}`
	request := httptest.NewRequest(http.MethodPost, "/links", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
	if parsed.url != "http://example.com/docs" {
		t.Fatalf("url = %q, want the scheme Django prepends", parsed.url)
	}
	if parsed.title == nil || *parsed.title != "Docs" {
		t.Fatalf("title = %#v, want the trimmed value", parsed.title)
	}
	if string(parsed.metadata) != `{"icon":"book"}` {
		t.Fatalf("metadata = %s", parsed.metadata)
	}
	if parsed.projectID == nil || *parsed.projectID != "01234567-89ab-4def-8123-456789abcdef" {
		t.Fatalf("project = %#v, want the canonical UUID", parsed.projectID)
	}
}

func TestQuickLinkBlankTitleSkipsLengthValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewHandler(nil, nil, nil, Settings{})
	var parsed quickLinkInput
	router.POST("/links", func(c *gin.Context) { parsed, _ = handler.quickLinkFields(c, true) })
	request := httptest.NewRequest(http.MethodPost, "/links", strings.NewReader(`{"title":"   "}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || parsed.title == nil || *parsed.title != "" {
		t.Fatalf("response = %d %s, title = %#v", response.Code, response.Body.String(), parsed.title)
	}

	parsed = quickLinkInput{}
	request = httptest.NewRequest(http.MethodPost, "/links", strings.NewReader(`{"title":null}`))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !parsed.hasTitle || parsed.title != nil {
		t.Fatalf("response = %d %s, title = %#v", response.Code, response.Body.String(), parsed.title)
	}
}

func TestQuickLinkSerializationMatchesDjangoFields(t *testing.T) {
	now := time.Date(2026, time.September, 15, 4, 0, 0, 0, time.UTC)
	title := "Docs"
	project := "project-id"
	data := quickLinkJSON(WorkspaceUserLink{
		ID: "link-id", CreatedAt: now, UpdatedAt: now, WorkspaceID: "workspace-id",
		ProjectID: &project, OwnerID: "owner-id", Title: &title,
		URL: "http://example.com", Metadata: emptyJSON(),
	})
	for _, field := range []string{
		"id", "created_at", "updated_at", "deleted_at", "title", "url",
		"metadata", "created_by", "updated_by", "workspace", "project", "owner",
	} {
		if _, ok := data[field]; !ok {
			t.Fatalf("serialized quick link is missing %q", field)
		}
	}
	if len(data) != 12 {
		t.Fatalf("serialized quick link has %d fields, want the 12 Django model fields", len(data))
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"url":"http://example.com"`) {
		t.Fatalf("serialized quick link = %s", encoded)
	}
}
