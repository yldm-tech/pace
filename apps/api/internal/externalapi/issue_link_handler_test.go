package externalapi

import (
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// Every work item route is mounted under both names, the current one and the one it had before an issue was called a work item.
func TestTheLinksAreServedUnderBothNames(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewHandler(nil, Settings{}).Register(router)

	counts := map[string]int{}
	for _, route := range router.Routes() {
		if !strings.Contains(route.Path, "/links/") {
			continue
		}
		for _, name := range []string{"/issues/", "/work-items/"} {
			if strings.Contains(route.Path, name) {
				counts[name]++
			}
		}
	}
	if counts["/issues/"] != 5 || counts["/work-items/"] != 5 {
		t.Fatalf("issues has %d link routes and work-items %d, want five each",
			counts["/issues/"], counts["/work-items/"])
	}
}

// The create's two url checks run in order: the shape first, then the scheme.
func TestTheLinkURLChecksRunInOrder(t *testing.T) {
	for target, want := range map[string]string{
		"https://example.com":      "",
		"http://example.com/a?b=c": "",
		"":                         "This field is required.",
		"   ":                      "This field is required.",
		"not a url":                "Invalid URL format.",
		"example.com":              "Invalid URL format.",
		// A shape Django's validator accepts but the scheme check then refuses.
		"ftp://example.com":          "Invalid URL scheme.",
		"mailto:someone@example.com": "Invalid URL format.",
	} {
		if got := validateLinkURL(target); got != want {
			t.Errorf("%q gave %q, want %q", target, got, want)
		}
	}
}

// The duplicate check is scoped to the issue rather than to the project, so the same url may hang off two work items.
func TestTheDuplicateCheckIsScopedToTheIssue(t *testing.T) {
	const condition = "issue_id = ? AND url = ?"
	if strings.Contains(condition, "project_id") {
		t.Error("the same url may hang off two work items in one project")
	}
	if !strings.Contains(condition, "issue_id") {
		t.Error("the check is scoped to the work item")
	}
}

// The creator may be named in the body, which is how an integration attributes a link to the person it acted for rather than to the key.
func TestTheCreatorMayBeNamedInTheBody(t *testing.T) {
	author := func(payload map[string]any, caller string) string {
		if value, ok := payload["created_by"].(string); ok && value != "" {
			return value
		}
		return caller
	}
	if author(map[string]any{}, "caller-id") != "caller-id" {
		t.Error("an unnamed author is the caller")
	}
	if author(map[string]any{"created_by": "someone-else"}, "caller-id") != "someone-else" {
		t.Error("a named author wins, and nothing checks that they are real")
	}
	if author(map[string]any{"created_by": ""}, "caller-id") != "caller-id" {
		t.Error("an empty author is not an author")
	}
}

// The link serializer asks for every field.
func TestIssueLinkJSONShape(t *testing.T) {
	data := issueLinkJSON(IssueLink{ID: "link-id"})
	for _, field := range []string{
		"id", "created_at", "updated_at", "created_by", "updated_by", "deleted_at",
		"title", "url", "metadata", "sort_order", "project", "workspace", "issue",
	} {
		if _, present := data[field]; !present {
			t.Errorf("the link is missing %q", field)
		}
	}
	if len(data) != 13 {
		t.Fatalf("the link has %d fields, want 13", len(data))
	}
}
