package workspace

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestSidebarPreferenceDefaultsAndNumberParsing(t *testing.T) {
	for _, test := range []struct {
		raw  string
		want float64
	}{
		{raw: `25`, want: 25},
		{raw: `25.5`, want: 25.5},
		{raw: `"30"`, want: 30},
	} {
		got, err := preferenceFloat(json.RawMessage(test.raw))
		if err != nil || got != test.want {
			t.Fatalf("preferenceFloat(%s) = %v, %v", test.raw, got, err)
		}
	}
	for _, raw := range []string{`null`, `true`, `"many"`} {
		if _, err := preferenceFloat(json.RawMessage(raw)); err == nil {
			t.Fatalf("preferenceFloat(%s) accepted invalid input", raw)
		}
	}
}

func TestSidebarPreferencePatchRequiresArray(t *testing.T) {
	router := gin.New()
	handler := NewHandler(nil, nil, nil, Settings{})
	router.PATCH("/preferences", func(c *gin.Context) {
		var entries []map[string]json.RawMessage
		if err := c.ShouldBindJSON(&entries); err != nil {
			handler.invalidDetail(c)
			return
		}
		c.Status(http.StatusOK)
	})
	request := httptest.NewRequest(http.MethodPatch, "/preferences", strings.NewReader(`{"key":"views"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("response status = %d, want %d", response.Code, http.StatusBadRequest)
	}
}
