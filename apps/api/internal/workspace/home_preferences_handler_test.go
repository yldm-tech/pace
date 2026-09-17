package workspace

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestWorkspaceHomePreferenceValidationMatchesDjangoContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name string
		body string
		want string
	}{
		{name: "null key", body: `{"key":null}`, want: `{"key":["This field may not be null."]}`},
		{name: "blank key", body: `{"key":"   "}`, want: `{"key":["This field may not be blank."]}`},
		{name: "non string key", body: `{"key":true}`, want: `{"key":["Not a valid string."]}`},
		{name: "long key", body: `{"key":"` + strings.Repeat("a", 256) + `"}`, want: `{"key":["Ensure this field has no more than 255 characters."]}`},
		{name: "null is_enabled", body: `{"is_enabled":null}`, want: `{"is_enabled":["This field may not be null."]}`},
		{name: "invalid is_enabled", body: `{"is_enabled":"maybe"}`, want: `{"is_enabled":["Must be a valid boolean."]}`},
		{name: "null sort_order", body: `{"sort_order":null}`, want: `{"sort_order":["This field may not be null."]}`},
		{name: "invalid sort_order", body: `{"sort_order":"many"}`, want: `{"sort_order":["A valid number is required."]}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			router := gin.New()
			handler := NewHandler(nil, nil, nil, Settings{})
			router.PATCH("/preference", func(c *gin.Context) { _, _ = handler.homePreferenceFields(c) })
			request := httptest.NewRequest(http.MethodPatch, "/preference", strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest || response.Body.String() != test.want {
				t.Fatalf("response = %d %s", response.Code, response.Body.String())
			}
		})
	}

	router := gin.New()
	handler := NewHandler(nil, nil, nil, Settings{})
	var parsed homePreferenceInput
	router.PATCH("/preference", func(c *gin.Context) { parsed, _ = handler.homePreferenceFields(c) })
	request := httptest.NewRequest(http.MethodPatch, "/preference", strings.NewReader(`{"key":" recents ","is_enabled":"0","sort_order":"12.5"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
	if parsed.key != "recents" || !parsed.hasKey || parsed.isEnabled || !parsed.hasIsEnabled || parsed.sortOrder != 12.5 || !parsed.hasSortOrder {
		t.Fatalf("parsed preference = %#v", parsed)
	}
}

func TestWorkspaceHomePreferenceBooleanAndFloatCoercion(t *testing.T) {
	for _, raw := range []string{`true`, `"true"`, `"T"`, `"yes"`, `"on"`, `"1"`, `1`} {
		value, err := djangoBoolean(json.RawMessage(raw))
		if err != nil || !value {
			t.Fatalf("djangoBoolean(%s) = %v, %v", raw, value, err)
		}
	}
	for _, raw := range []string{`false`, `"false"`, `"N"`, `"no"`, `"off"`, `"0"`, `0`} {
		value, err := djangoBoolean(json.RawMessage(raw))
		if err != nil || value {
			t.Fatalf("djangoBoolean(%s) = %v, %v", raw, value, err)
		}
	}
	for _, raw := range []string{`"maybe"`, `2`, `[]`, `{}`} {
		if _, err := djangoBoolean(json.RawMessage(raw)); err == nil {
			t.Fatalf("djangoBoolean(%s) accepted invalid input", raw)
		}
	}

	// Python's float() accepts booleans, so DRF's FloatField does too.
	for _, test := range []struct {
		raw  string
		want float64
	}{{raw: `true`, want: 1}, {raw: `false`, want: 0}, {raw: `"7.5"`, want: 7.5}, {raw: `7`, want: 7}} {
		got, err := djangoFloat(json.RawMessage(test.raw))
		if err != nil || got != test.want {
			t.Fatalf("djangoFloat(%s) = %v, %v", test.raw, got, err)
		}
	}
}

func TestWorkspaceHomePreferenceSerializationOmitsConfig(t *testing.T) {
	encoded, err := json.Marshal(homePreferenceJSON(WorkspaceHomePreference{
		Key: "quick_links", IsEnabled: true, SortOrder: 999, Config: emptyJSON(),
	}))
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `{"is_enabled":true,"key":"quick_links","sort_order":999}` {
		t.Fatalf("serialized home preference = %s", encoded)
	}
}

func TestWorkspaceHomePreferenceSeedKeysMatchDjangoChoices(t *testing.T) {
	want := []string{"quick_links", "recents", "my_stickies"}
	if len(workspaceHomePreferenceKeys) != len(want) {
		t.Fatalf("seed keys = %#v", workspaceHomePreferenceKeys)
	}
	for index, key := range want {
		if workspaceHomePreferenceKeys[index] != key {
			t.Fatalf("seed keys = %#v", workspaceHomePreferenceKeys)
		}
	}
}
