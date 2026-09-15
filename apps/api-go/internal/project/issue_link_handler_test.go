package project

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestIssueLinkValidationMatchesDjango(t *testing.T) {
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
		{name: "non string url", body: `{"url":7}`, want: `{"url":["Not a valid string."]}`},
		// validate_url raises with a dict, so DRF nests it under the field.
		{name: "invalid url", body: `{"url":"not a url"}`, want: `{"url":{"error":"Invalid URL format."}}`},
		{name: "null metadata", body: `{"url":"example.com","metadata":null}`, want: `{"metadata":["This field may not be null."]}`},
		{name: "long title", body: `{"url":"example.com","title":"` + strings.Repeat("t", 256) + `"}`, want: `{"title":["Ensure this field has no more than 255 characters."]}`},
		{name: "partial skips the required url", body: `{"title":"Docs"}`, partial: true, want: ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			router := gin.New()
			handler := NewHandler(nil, nil, Settings{})
			router.POST("/links", func(c *gin.Context) {
				var body map[string]json.RawMessage
				if err := c.ShouldBindJSON(&body); err != nil {
					handler.invalidDetail(c)
					return
				}
				_, _ = handler.issueLinkFieldsFrom(c, body, test.partial)
			})
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

// The issue link serializer prefixes the scheme exactly as the workspace quick
// link serializer does, which is why both now share the validator.
func TestIssueLinkPrefixesTheSchemeLikeQuickLinks(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewHandler(nil, nil, Settings{})
	var parsed issueLinkInput
	router.POST("/links", func(c *gin.Context) {
		var body map[string]json.RawMessage
		if err := c.ShouldBindJSON(&body); err != nil {
			handler.invalidDetail(c)
			return
		}
		parsed, _ = handler.issueLinkFieldsFrom(c, body, false)
	})
	request := httptest.NewRequest(http.MethodPost, "/links",
		strings.NewReader(`{"url":"example.com/docs","title":"  Docs  ","metadata":{"icon":"book"}}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
	if parsed.url != "http://example.com/docs" {
		t.Fatalf("url = %q, want the prefixed value", parsed.url)
	}
	if parsed.title == nil || *parsed.title != "Docs" {
		t.Fatalf("title = %#v, want the trimmed value", parsed.title)
	}
	if string(parsed.metadata) != `{"icon":"book"}` {
		t.Fatalf("metadata = %s", parsed.metadata)
	}
}

func TestIssueLinkRoutesRejectMalformedIdentifiers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewHandler(nil, nil, Settings{}).Register(router)
	project := "/api/workspaces/pace/projects/01234567-89ab-4def-8123-456789abcdef/"
	for _, path := range []string{
		project + "issues/not-a-uuid/issue-links/",
		project + "issues/11111111-2222-4333-8444-555555555555/issue-links/not-a-uuid/",
	} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusNotFound {
			t.Errorf("GET %s status = %d, want %d", path, response.Code, http.StatusNotFound)
		}
	}
}
