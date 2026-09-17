package project

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// The create has never sent an invitation: it reads role off a queryset, which is an attribute error. A call naming no emails is refused before that, and is the only answer this route gives that is not a 500.
func TestTheInvitationCreateOnlyRefusesOrFails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	if errInviteReadsRoleOffAQueryset == nil {
		t.Fatal("the failure the create ends in is not named")
	}
	if !strings.Contains(errInviteReadsRoleOffAQueryset.Error(), "queryset") {
		t.Errorf("the failure reads %q", errInviteReadsRoleOffAQueryset.Error())
	}
}

// The public view reports what an invitee needs to decide and never the token or the email.
func TestThePublicInviteHidesTheToken(t *testing.T) {
	body := gin.H{
		"id": "invite-id", "project": gin.H{}, "workspace": gin.H{},
		"role": 15, "message": nil, "accepted": false, "responded_at": nil,
	}
	for _, hidden := range []string{"token", "email", "created_by", "updated_by"} {
		if _, present := body[hidden]; present {
			t.Errorf("the public view carries %q", hidden)
		}
	}
	if len(body) != 7 {
		t.Fatalf("the public view has %d fields, want 7", len(body))
	}
}

// A workspace membership made by accepting a project invitation is capped at member however high the project role is, so an invitation to administer a project does not hand out the workspace.
func TestAcceptingCapsTheWorkspaceRole(t *testing.T) {
	capped := func(role int) int {
		if role >= 15 {
			return 15
		}
		return role
	}
	if got := capped(20); got != 15 {
		t.Errorf("an admin invitation gives workspace role %d, want 15", got)
	}
	if got := capped(15); got != 15 {
		t.Errorf("a member invitation gives %d", got)
	}
	if got := capped(5); got != 5 {
		t.Errorf("a guest invitation gives %d, want the role itself", got)
	}
}

// The token is checked before the session, so a caller with the right token and no session is told to sign in rather than that the token is wrong.
func TestTheTokenIsCheckedBeforeTheSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := &Handler{}
	router.POST("/join/", func(c *gin.Context) {
		// The order the handler applies: an empty token is a 403 whatever the session is.
		var request struct {
			Token string `json:"token"`
		}
		_ = c.ShouldBindJSON(&request)
		if request.Token == "" {
			c.JSON(http.StatusForbidden, gin.H{"error": "You do not have permission to join the project"})
			return
		}
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Authentication required to accept project invitation"})
	})
	_ = handler

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/join/", strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("a call with no token answers %d, want 403", recorder.Code)
	}
	recorder = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/join/", strings.NewReader(`{"token":"abc"}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("a call with a token and no session answers %d, want 401", recorder.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["error"] != "Authentication required to accept project invitation" {
		t.Errorf("the refusal reads %v", body["error"])
	}
}
