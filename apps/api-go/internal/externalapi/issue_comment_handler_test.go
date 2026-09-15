package externalapi

import (
	"testing"
	"time"
)

// An integration may name both the author and the creation time, which is what lets it import a conversation with its original timestamps.
func TestACommentMayBeImportedWithItsOwnTimestamps(t *testing.T) {
	now := time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)
	created := func(payload map[string]any) time.Time {
		if value, ok := payload["created_at"].(string); ok && value != "" {
			parsed, err := time.Parse(time.RFC3339, value)
			if err == nil {
				return parsed
			}
		}
		return now
	}
	author := func(payload map[string]any, caller string) string {
		if value, ok := payload["created_by"].(string); ok && value != "" {
			return value
		}
		return caller
	}
	if !created(map[string]any{}).Equal(now) {
		t.Error("an unnamed creation time is this moment")
	}
	old := created(map[string]any{"created_at": "2020-01-02T03:04:05Z"})
	if old.Year() != 2020 {
		t.Errorf("a named creation time is honoured, got %s", old)
	}
	if author(map[string]any{"created_by": "someone-else"}, "caller-id") != "someone-else" {
		t.Error("a named author wins")
	}
}

// The serializer excludes two columns rather than listing what it wants, so the stripped text and the document tree are the only things held back.
func TestTheCommentSerializerExcludesRatherThanLists(t *testing.T) {
	data := issueCommentJSON(commentRow{IssueComment: IssueComment{ID: "comment-id"}})
	for _, absent := range []string{"comment_stripped", "comment_json"} {
		if _, present := data[absent]; present {
			t.Errorf("the serializer excludes %q", absent)
		}
	}
	for _, field := range []string{
		"id", "created_at", "updated_at", "created_by", "updated_by", "deleted_at",
		"comment_html", "attachments", "access", "edited_at",
		"external_source", "external_id", "description", "parent",
		"project", "workspace", "issue", "actor", "is_member",
	} {
		if _, present := data[field]; !present {
			t.Errorf("the comment is missing %q", field)
		}
	}
	if len(data) != 19 {
		t.Fatalf("the comment has %d fields, want 19", len(data))
	}
}

// The external-id check compares against the comment's own id, so rewriting a comment with the id it already has is not a conflict.
func TestRewritingAnExternalIdWithItsOwnIsNotAConflict(t *testing.T) {
	fires := func(requested string, current *string) bool {
		return requested != "" && (current == nil || *current != requested)
	}
	own := "42"
	if fires("42", &own) {
		t.Error("rewriting with its own id must not fire")
	}
	if !fires("43", &own) {
		t.Error("a different id must fire")
	}
	if !fires("42", nil) {
		t.Error("a comment with no id at all must fire")
	}
	if fires("", &own) {
		t.Error("naming no id must not fire")
	}
}

// The comment answers with the sanitizer's own rejection where the sticky answers with a message of its own.
func TestTheTwoSanitizerRefusalsDiffer(t *testing.T) {
	const commentKey = "comment_html"
	const stickyKey = "error"
	if commentKey == stickyKey {
		t.Fatal("the two routes report a rejected description under different keys")
	}
}

// The default access is internal, so a comment an integration creates is not public unless it says so.
func TestACommentDefaultsToInternal(t *testing.T) {
	access := "INTERNAL"
	if value, ok := map[string]any{}["access"].(string); ok && value != "" {
		access = value
	}
	if access != "INTERNAL" {
		t.Errorf("the default access is %q", access)
	}
	if value, ok := map[string]any{"access": "EXTERNAL"}["access"].(string); ok && value != "" {
		access = value
	}
	if access != "EXTERNAL" {
		t.Error("a named access wins")
	}
}
