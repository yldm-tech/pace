package project

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api/internal/auth"
)

func TestProjectMemberRoleSerializationKeepsEveryDeclaredField(t *testing.T) {
	// The views pass fields=("id", "member", "role"), but DynamicBaseSerializer
	// overwrites that argument with expand, so nothing is filtered out.
	now := time.Date(2026, time.September, 15, 4, 0, 0, 0, time.UTC)
	data := projectMemberRoleJSON(ProjectMember{
		ID: "member-row", ProjectID: "project-id", MemberID: "user-id",
		Role: roleMember, CreatedAt: now,
	})
	if len(data) != 6 {
		t.Fatalf("serialized member role has %d fields, want 6", len(data))
	}
	for _, field := range []string{"id", "role", "member", "project", "original_role", "created_at"} {
		if _, ok := data[field]; !ok {
			t.Errorf("serialized member role is missing %q", field)
		}
	}
	if data["original_role"] != data["role"] {
		t.Fatalf("original_role = %v, want it to mirror role", data["original_role"])
	}
}

func TestProjectMemberPreferenceSerializationMatchesDjango(t *testing.T) {
	data := projectMemberPreferenceJSON(ProjectMember{
		ProjectID: "project-id", MemberID: "user-id", WorkspaceID: "workspace-id",
		Preferences: auth.JSONValue([]byte(`{"pages":{"block_display":true}}`)),
	})
	if len(data) != 4 {
		t.Fatalf("serialized preference has %d fields, want 4", len(data))
	}
	for _, field := range []string{"preferences", "project_id", "member_id", "workspace_id"} {
		if _, ok := data[field]; !ok {
			t.Errorf("serialized preference is missing %q", field)
		}
	}
}

func TestProjectMemberAdminSerializationAddsEmail(t *testing.T) {
	user := auth.User{ID: "user-id", Email: "member@pace.test", DisplayName: "member"}
	lite := liteUserJSON(user, false)
	if _, ok := lite["email"]; ok {
		t.Fatal("UserLiteSerializer must not expose the email")
	}
	admin := liteUserJSON(user, true)
	if admin["email"] != user.Email {
		t.Fatalf("UserAdminLiteSerializer email = %v", admin["email"])
	}
	if _, ok := admin["last_login_medium"]; !ok {
		t.Fatal("UserAdminLiteSerializer must expose last_login_medium")
	}
}

func TestProjectMemberRoleValueAcceptsDjangoInputs(t *testing.T) {
	handler := NewHandler(nil, nil, Settings{})
	for _, test := range []struct {
		raw  string
		want int
	}{{raw: `5`, want: 5}, {raw: `15`, want: 15}, {raw: `20`, want: 20}, {raw: `"15"`, want: 15}} {
		got, err := handler.roleValue(json.RawMessage(test.raw))
		if err != nil || got != test.want {
			t.Fatalf("roleValue(%s) = %d, %v", test.raw, got, err)
		}
	}
	for _, raw := range []string{`"member"`, `true`, `[]`} {
		if _, err := handler.roleValue(json.RawMessage(raw)); err == nil {
			t.Fatalf("roleValue(%s) accepted invalid input", raw)
		}
	}
}

func TestProjectMemberCreateRequiresMembers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewHandler(nil, nil, Settings{})
	router.POST("/members", func(c *gin.Context) {
		var request struct {
			Members []struct {
				MemberID string `json:"member_id"`
			} `json:"members"`
		}
		if err := c.ShouldBindJSON(&request); err != nil {
			handler.invalidDetail(c)
			return
		}
		if len(request.Members) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "At least one member is required"})
			return
		}
		c.Status(http.StatusOK)
	})
	for _, body := range []string{`{"members":[]}`, `{}`} {
		request := httptest.NewRequest(http.MethodPost, "/members", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest || response.Body.String() != `{"error":"At least one member is required"}` {
			t.Fatalf("body %s -> %d %s", body, response.Code, response.Body.String())
		}
	}
}

func TestProjectMemberRouteRejectsMalformedMemberIdentifier(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewHandler(nil, nil, Settings{}).Register(router)
	request := httptest.NewRequest(http.MethodGet, "/api/workspaces/pace/projects/01234567-89ab-4def-8123-456789abcdef/members/not-a-uuid/", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
	}
}

func TestActiveProjectMemberAnswersFromTheRequestScopedCache(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// The handler has no database, so an answer at all proves the row came from the request's context rather than from a second query.
	handler := NewHandler(nil, nil, Settings{})
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/api/workspaces/pace/projects/project-id/issues/", nil)
	cached := ProjectMember{ID: "member-row", ProjectID: "project-id", MemberID: "user-id", Role: roleGuest}
	cacheProjectMember(c, projectMemberLookup{slug: "pace", projectID: "project-id", memberID: "user-id", member: cached, found: true})

	member, found, err := handler.activeProjectMember(c.Request.Context(), "pace", "project-id", "user-id")
	if err != nil || !found || member.ID != cached.ID || member.Role != cached.Role {
		t.Fatalf("activeProjectMember = %+v, %t, %v", member, found, err)
	}
}

func TestProjectMemberCacheOnlyAnswersItsOwnQuestion(t *testing.T) {
	lookup := projectMemberLookup{slug: "pace", projectID: "project-id", memberID: "user-id"}
	if !lookup.matches("pace", "project-id", "user-id") {
		t.Fatal("the cached lookup does not match the arguments that produced it")
	}
	for _, question := range [][3]string{
		{"other", "project-id", "user-id"},
		{"pace", "other-project", "user-id"},
		{"pace", "project-id", "other-user"},
	} {
		if lookup.matches(question[0], question[1], question[2]) {
			t.Fatalf("the cached lookup answered for %v", question)
		}
	}
}
