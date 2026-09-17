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

func TestWorkspaceThemeValidationMatchesDjangoContract(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
		want string
	}{
		{name: "missing name", body: `{}`, want: `{"name":["This field is required."]}`},
		{name: "null name", body: `{"name":null}`, want: `{"name":["This field may not be null."]}`},
		{name: "blank name", body: `{"name":"  "}`, want: `{"name":["This field may not be blank."]}`},
		{name: "long name", body: `{"name":"` + strings.Repeat("界", 301) + `"}`, want: `{"name":["Ensure this field has no more than 300 characters."]}`},
		{name: "null colors", body: `{"name":"Pace","colors":null}`, want: `{"colors":["This field may not be null."]}`},
		{name: "invalid deletion timestamp", body: `{"name":"Pace","deleted_at":"tomorrow"}`, want: `{"deleted_at":["Datetime has wrong format. Use one of these formats instead: YYYY-MM-DDThh:mm[:ss[.uuuuuu]][+HH:MM|-HH:MM|Z]."]}`},
		{name: "invalid audit user", body: `{"name":"Pace","created_by":"not-a-uuid"}`, want: `{"created_by":["“not-a-uuid” is not a valid UUID."]}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			router := gin.New()
			handler := NewHandler(nil, nil, nil, Settings{})
			router.POST("/themes", func(c *gin.Context) { _, _ = handler.themeFields(c, false) })
			request := httptest.NewRequest(http.MethodPost, "/themes", strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest || response.Body.String() != test.want {
				t.Fatalf("response = %d %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestWorkspaceThemeDefaultsAndSerialization(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewHandler(nil, nil, nil, Settings{})
	var parsed themeInput
	router.POST("/themes", func(c *gin.Context) { parsed, _ = handler.themeFields(c, false) })
	request := httptest.NewRequest(http.MethodPost, "/themes", strings.NewReader(`{"name":" Pace Dark "}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if parsed.name != "Pace Dark" || string(parsed.colors) != `{}` {
		t.Fatalf("parsed theme = %#v", parsed)
	}

	now := time.Date(2026, time.September, 15, 3, 0, 0, 0, time.UTC)
	actorID := "01234567-89ab-cdef-8123-456789abcdef"
	data := themeJSON(WorkspaceTheme{
		ID: "theme-id", CreatedAt: now, UpdatedAt: now, CreatedByID: &actorID,
		WorkspaceID: "workspace-id", Name: "Pace Dark", ActorID: actorID,
		Colors: []byte(`{"primary":"#111827","nested":{"accent":true}}`),
	})
	encoded, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`"workspace":"workspace-id"`, `"actor":"` + actorID + `"`, `"primary":"#111827"`, `"nested":{"accent":true}`} {
		if !strings.Contains(string(encoded), expected) {
			t.Fatalf("serialized theme %s does not contain %s", encoded, expected)
		}
	}
}

func TestWorkspaceThemeAuditUUIDAcceptsDjangoFormats(t *testing.T) {
	for _, value := range []string{
		"01234567-89ab-cdef-0123-456789abcdef",
		"0123456789abcdef0123456789abcdef",
		"{01234567-89AB-CDEF-0123-456789ABCDEF}",
		"urn:uuid:01234567-89ab-cdef-0123-456789abcdef",
	} {
		canonical, ok := canonicalThemeUUID(value)
		if !ok || canonical != "01234567-89ab-cdef-0123-456789abcdef" {
			t.Fatalf("canonicalThemeUUID(%q) = %q, %t", value, canonical, ok)
		}
	}
	for _, value := range []string{"", "not-a-uuid", "01234567-89ab-cdef-0123"} {
		if _, ok := canonicalThemeUUID(value); ok {
			t.Fatalf("canonicalThemeUUID(%q) accepted invalid input", value)
		}
	}
}
