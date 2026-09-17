package project

import (
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
)

// IssueActivitySerializer is fields = "__all__" plus four nested details and source_data. Rendering the real serializer against the model gives twenty base fields, so the response carries twenty-five.
func TestActivitySerializationShape(t *testing.T) {
	handler := &Handler{}
	activity := IssueActivity{ID: "activity-id", ProjectID: "project-id", WorkspaceID: "workspace-id", Verb: "updated"}
	data := handler.activityJSON(activity, gin.H{}, gin.H{}, nil, nil, nil)
	for _, field := range []string{
		"id", "created_at", "updated_at", "created_by", "updated_by", "deleted_at",
		"project", "workspace", "issue", "verb", "field", "old_value", "new_value",
		"comment", "attachments", "issue_comment", "actor", "old_identifier",
		"new_identifier", "epoch",
		"actor_detail", "issue_detail", "project_detail", "workspace_detail", "source_data",
	} {
		if _, present := data[field]; !present {
			t.Errorf("serialized activity is missing %q", field)
		}
	}
	if len(data) != 25 {
		t.Fatalf("serialized activity has %d fields, want 25", len(data))
	}
	// attachments is an ArrayField with an empty default, so it is a list rather than null.
	if got, ok := data["attachments"].([]string); !ok || len(got) != 0 {
		t.Fatalf("attachments = %v, want an empty array", data["attachments"])
	}
	// source_data is null unless the intake prefetch found a row.
	if data["source_data"] != nil {
		t.Fatalf("source_data = %v, want null", data["source_data"])
	}
}

func TestActivitySerializationFillsTheNestedDetails(t *testing.T) {
	handler := &Handler{}
	actor, issue := "actor-id", "issue-id"
	activity := IssueActivity{
		ID: "activity-id", ActorID: &actor, IssueID: &issue,
		Attachments: pq.StringArray{"https://example.invalid/a"},
	}
	source := gin.H{"source": "email"}
	data := handler.activityJSON(activity,
		gin.H{"id": "project-id"}, gin.H{"id": "workspace-id"},
		map[string]gin.H{actor: {"id": actor}}, map[string]gin.H{issue: {"id": issue}},
		map[string]gin.H{issue: source})
	if detail, ok := data["actor_detail"].(gin.H); !ok || detail["id"] != actor {
		t.Fatalf("actor_detail = %v", data["actor_detail"])
	}
	if detail, ok := data["issue_detail"].(gin.H); !ok || detail["id"] != issue {
		t.Fatalf("issue_detail = %v", data["issue_detail"])
	}
	if data["source_data"] == nil {
		t.Fatal("source_data should be filled when the intake row exists")
	}
	if got := data["attachments"].([]string); len(got) != 1 {
		t.Fatalf("attachments = %v", got)
	}
}

// A null actor or issue serializes as null rather than reaching into an empty map.
func TestActivitySerializationHandlesMissingRelations(t *testing.T) {
	handler := &Handler{}
	data := handler.activityJSON(IssueActivity{ID: "x"}, gin.H{}, gin.H{}, map[string]gin.H{}, map[string]gin.H{}, nil)
	if data["actor_detail"] != nil || data["issue_detail"] != nil {
		t.Fatalf("actor_detail = %v, issue_detail = %v; both should be null", data["actor_detail"], data["issue_detail"])
	}
}

// The four hidden fields are the ones the endpoint excludes: comments have their own serializer, and votes, reactions and draft edits are not history.
func TestHiddenActivityFields(t *testing.T) {
	want := map[string]bool{"comment": true, "vote": true, "reaction": true, "draft": true}
	if len(hiddenActivityFields) != len(want) {
		t.Fatalf("hidden fields = %v", hiddenActivityFields)
	}
	for _, field := range hiddenActivityFields {
		if !want[field] {
			t.Errorf("%q should not be hidden", field)
		}
	}
}

func TestCreatedAtFilterParsing(t *testing.T) {
	for _, value := range []string{
		"2026-09-15T04:00:00Z",
		"2026-09-15T04:00:00.123456Z",
		"2026-09-15T04:00:00+08:00",
		"2026-09-15 04:00:00",
		"2026-09-15 04:00:00.123456",
		"2026-09-15",
	} {
		if _, ok := parseDjangoDateTime(value); !ok {
			t.Errorf("parseDjangoDateTime(%q) should be accepted", value)
		}
	}
	for _, value := range []string{"yesterday", "15/09/2026", "", "2026-13-45"} {
		if _, ok := parseDjangoDateTime(value); ok {
			t.Errorf("parseDjangoDateTime(%q) should be refused", value)
		}
	}
	parsed, ok := parseDjangoDateTime("2026-09-15T04:00:00Z")
	if !ok || !parsed.Equal(time.Date(2026, 9, 15, 4, 0, 0, 0, time.UTC)) {
		t.Fatalf("parsed = %v", parsed)
	}
}
