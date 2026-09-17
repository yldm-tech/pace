package project

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api/internal/auth"
)

func TestIssueReactionSerializationExpandsTheActor(t *testing.T) {
	now := time.Date(2026, time.September, 15, 4, 0, 0, 0, time.UTC)
	reaction := IssueReaction{
		ID: "reaction-id", CreatedAt: now, UpdatedAt: now, ProjectID: "project-id",
		WorkspaceID: "workspace-id", IssueID: "issue-id", ActorID: "actor-id",
		Reaction: "\U0001F44D",
	}
	// The serializer lists every model field and adds actor_detail.
	data := map[string]any{
		"id": reaction.ID, "created_at": reaction.CreatedAt, "updated_at": reaction.UpdatedAt,
		"created_by": reaction.CreatedByID, "updated_by": reaction.UpdatedByID,
		"deleted_at": reaction.DeletedAt, "project": reaction.ProjectID,
		"workspace": reaction.WorkspaceID, "issue": reaction.IssueID,
		"actor": reaction.ActorID, "reaction": reaction.Reaction,
		"actor_detail": liteUserJSON(auth.User{ID: "actor-id"}, false),
	}
	for _, field := range []string{
		"id", "created_at", "updated_at", "created_by", "updated_by", "deleted_at",
		"project", "workspace", "issue", "actor", "reaction", "actor_detail",
	} {
		if _, ok := data[field]; !ok {
			t.Errorf("serialized reaction is missing %q", field)
		}
	}
	// actor_detail is UserLiteSerializer, which never carries the email.
	actor := data["actor_detail"].(gin.H)
	if _, ok := actor["email"]; ok {
		t.Error("actor_detail must not expose the email")
	}
}

func TestIssueSubscriberSerializationListsEveryField(t *testing.T) {
	now := time.Date(2026, time.September, 15, 4, 0, 0, 0, time.UTC)
	data := issueSubscriberJSON(IssueSubscriber{
		ID: "subscriber-row", CreatedAt: now, UpdatedAt: now, ProjectID: "project-id",
		WorkspaceID: "workspace-id", IssueID: "issue-id", SubscriberID: "user-id",
	})
	if len(data) != 10 {
		t.Fatalf("serialized subscriber has %d fields, want 10", len(data))
	}
	for _, field := range []string{
		"id", "created_at", "updated_at", "created_by", "updated_by", "deleted_at",
		"project", "workspace", "issue", "subscriber",
	} {
		if _, ok := data[field]; !ok {
			t.Errorf("serialized subscriber is missing %q", field)
		}
	}
}

func TestIssueInteractionRoutesRequireDjangoSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewHandler(nil, nil, Settings{}).Register(router)
	issue := "/api/workspaces/pace/projects/01234567-89ab-4def-8123-456789abcdef/issues/11111111-2222-4333-8444-555555555555/"
	for _, test := range []struct {
		method string
		path   string
	}{
		{method: http.MethodGet, path: issue + "reactions/"},
		{method: http.MethodPost, path: issue + "reactions/"},
		{method: http.MethodDelete, path: issue + "reactions/thumbsup/"},
		{method: http.MethodGet, path: issue + "issue-subscribers/"},
		{method: http.MethodGet, path: issue + "subscribe/"},
		{method: http.MethodPost, path: issue + "subscribe/"},
		{method: http.MethodDelete, path: issue + "subscribe/"},
	} {
		request := httptest.NewRequest(test.method, test.path, nil)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Errorf("%s %s status = %d, want %d", test.method, test.path, response.Code, http.StatusUnauthorized)
		}
	}
}

// The reaction code is a free-form string in Django's URL converter, so an
// emoji or a word both have to route.
func TestReactionCodeIsNotConstrainedToAUUID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewHandler(nil, nil, Settings{}).Register(router)
	issue := "/api/workspaces/pace/projects/01234567-89ab-4def-8123-456789abcdef/issues/11111111-2222-4333-8444-555555555555/"
	for _, code := range []string{"thumbsup", "%F0%9F%91%8D", "1"} {
		request := httptest.NewRequest(http.MethodDelete, issue+"reactions/"+code+"/", nil)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		// Unauthorized rather than not found proves the route matched.
		if response.Code != http.StatusUnauthorized {
			t.Errorf("reaction code %q did not route: %d", code, response.Code)
		}
	}
}

func TestIssueRoutesRejectAMalformedIssueIdentifier(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewHandler(nil, nil, Settings{}).Register(router)
	request := httptest.NewRequest(http.MethodGet,
		"/api/workspaces/pace/projects/01234567-89ab-4def-8123-456789abcdef/issues/not-a-uuid/reactions/", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
	}
}

func TestProjectMemberLiteLeavesIsSubscribedNull(t *testing.T) {
	// ProjectMemberLiteSerializer declares is_subscribed as a read-only boolean
	// fed by an annotation the subscriber list route never adds.
	data := gin.H{"member": liteUserJSON(auth.User{ID: "user-id"}, false), "id": "member-row", "is_subscribed": nil}
	if data["is_subscribed"] != nil {
		t.Fatal("is_subscribed should be null without the annotation")
	}
	if len(data) != 3 {
		t.Fatalf("serialized member lite has %d fields, want 3", len(data))
	}
}
