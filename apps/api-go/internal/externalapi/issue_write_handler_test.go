package externalapi

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// rawBody reads a payload the way the handlers do, keeping each field's own bytes.
func rawBody(t *testing.T, payload string) map[string]json.RawMessage {
	t.Helper()
	var body map[string]json.RawMessage
	if err := json.Unmarshal([]byte(payload), &body); err != nil {
		t.Fatal(err)
	}
	return body
}

// Both spellings carry all five methods, which is what lets the proxy cut the two paths over by path rather than by method.
func TestTheWorkItemRoutesCarryEveryMethod(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewHandler(nil, Settings{}).Register(router)

	const base = "/api/v1/workspaces/:slug/projects/:project/"
	wanted := map[string]bool{}
	for _, name := range []string{"issues/", "work-items/"} {
		wanted["GET "+base+name] = false
		wanted["POST "+base+name] = false
		wanted["GET "+base+name+":issue/"] = false
		wanted["PATCH "+base+name+":issue/"] = false
		wanted["DELETE "+base+name+":issue/"] = false
	}
	for _, route := range router.Routes() {
		key := route.Method + " " + route.Path
		if _, listed := wanted[key]; listed {
			wanted[key] = true
		}
	}
	for route, found := range wanted {
		if !found {
			t.Errorf("%s is not served", route)
		}
	}
	// Django binds no PUT on either path, so neither does this.
	for _, route := range router.Routes() {
		if route.Method == "PUT" && strings.Contains(route.Path, "/projects/:project/issues/") {
			t.Errorf("%s %s is served and Django does not bind it", route.Method, route.Path)
		}
	}
}

// The two list checks print the identifiers the way Python prints a list of UUIDs, which is what the message carries.
func TestTheRejectedIdentifiersArePrintedAsPython(t *testing.T) {
	got := pythonList([]string{"11111111-2222-3333-4444-555555555555"})
	want := "[UUID('11111111-2222-3333-4444-555555555555')]"
	if got != want {
		t.Errorf("one identifier prints as %s, want %s", got, want)
	}
	got = pythonList([]string{"a", "b"})
	if got != "[UUID('a'), UUID('b')]" {
		t.Errorf("two identifiers print as %s", got)
	}
	if pythonList(nil) != "[]" {
		t.Errorf("an empty list prints as %s", pythonList(nil))
	}
}

// An assignee who is not a member is a refusal here rather than a silent drop, which is where this API and the session API part company.
func TestTheRejectedIdentifiersAreTheOnesNotAllowed(t *testing.T) {
	missing := missingFrom([]string{"a", "b", "c"}, []string{"b"})
	if len(missing) != 2 || missing[0] != "a" || missing[1] != "c" {
		t.Errorf("the rejected identifiers are %v", missing)
	}
	if len(missingFrom([]string{"a"}, []string{"a"})) != 0 {
		t.Error("an allowed identifier is reported as rejected")
	}
}

// The external pair is read off the payload as it was written rather than off the validated fields, so a pair the serializer never looked at still counts.
func TestTheExternalPairIsReadOffThePayload(t *testing.T) {
	body := rawBody(t, `{"external_id": "abc", "external_source": "github"}`)
	if payloadString(body, "external_id") != "abc" || payloadString(body, "external_source") != "github" {
		t.Error("the pair is not read off the payload")
	}
	if payloadString(body, "missing") != "" {
		t.Error("an absent field is not empty")
	}
	// A pair written as something other than a string counts as absent, since Django compares it with a string.
	if payloadString(rawBody(t, `{"external_id": 5}`), "external_id") != "" {
		t.Error("a number is read as an external id")
	}
}
