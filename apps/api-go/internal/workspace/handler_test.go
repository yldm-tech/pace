package workspace

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
)

type recordingCache struct{ patterns []string }

func (cache *recordingCache) InvalidatePattern(_ context.Context, pattern string) error {
	cache.patterns = append(cache.patterns, pattern)
	return nil
}

func TestWorkspaceRouteInventory(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewHandler(nil, nil, nil, Settings{}).Register(router)
	expected := map[string]bool{
		"GET /api/workspace-slug-check/": true,
		"GET /api/workspaces/":           true, "POST /api/workspaces/": true,
		"GET /api/workspaces/:slug/": true, "PUT /api/workspaces/:slug/": true,
		"PATCH /api/workspaces/:slug/": true, "DELETE /api/workspaces/:slug/": true,
		"GET /api/users/me/workspaces/":                    true,
		"GET /api/workspaces/:slug/members/":               true,
		"GET /api/workspaces/:slug/members/:id/":           true,
		"PATCH /api/workspaces/:slug/members/:id/":         true,
		"DELETE /api/workspaces/:slug/members/:id/":        true,
		"POST /api/workspaces/:slug/members/leave/":        true,
		"GET /api/workspaces/:slug/workspace-members/me/":  true,
		"POST /api/workspaces/:slug/workspace-views/":      true,
		"GET /api/workspaces/:slug/invitations/":           true,
		"POST /api/workspaces/:slug/invitations/":          true,
		"GET /api/workspaces/:slug/invitations/:id/":       true,
		"PATCH /api/workspaces/:slug/invitations/:id/":     true,
		"DELETE /api/workspaces/:slug/invitations/:id/":    true,
		"GET /api/users/me/workspaces/invitations/":        true,
		"POST /api/users/me/workspaces/invitations/":       true,
		"GET /api/workspaces/:slug/invitations/:id/join/":  true,
		"POST /api/workspaces/:slug/invitations/:id/join/": true,
	}
	for _, route := range router.Routes() {
		key := route.Method + " " + route.Path
		if !expected[key] {
			t.Fatalf("unexpected workspace route %s", key)
		}
		delete(expected, key)
	}
	if len(expected) != 0 {
		t.Fatalf("missing workspace routes: %#v", expected)
	}
}

func TestWorkspaceRoutesRequireDjangoSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewHandler(nil, nil, nil, Settings{}).Register(router)
	for _, test := range []struct {
		method string
		path   string
	}{
		{method: http.MethodGet, path: "/api/workspaces/"},
		{method: http.MethodGet, path: "/api/workspace-slug-check/?slug=pace"},
		{method: http.MethodGet, path: "/api/workspaces/pace/members/"},
		{method: http.MethodGet, path: "/api/users/me/workspaces/invitations/"},
	} {
		request := httptest.NewRequest(test.method, test.path, nil)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s status = %d, want %d", test.method, test.path, response.Code, http.StatusUnauthorized)
		}
		if response.Body.String() != `{"detail":"Authentication credentials were not provided."}` {
			t.Fatalf("%s %s body = %s", test.method, test.path, response.Body.String())
		}
	}
}

func TestUUIDRoutesRejectMalformedIdentifiersBeforeDatabaseAccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewHandler(nil, nil, nil, Settings{}).Register(router)
	for _, path := range []string{
		"/api/workspaces/pace/members/not-a-uuid/",
		"/api/workspaces/pace/invitations/not-a-uuid/",
		"/api/workspaces/pace/invitations/not-a-uuid/join/",
	} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusNotFound {
			t.Fatalf("GET %s status = %d, want %d", path, response.Code, http.StatusNotFound)
		}
	}
}

func TestPublicInvitationNeverExposesToken(t *testing.T) {
	data := invitationPublicJSON(
		WorkspaceInvite{ID: "invite-id", Email: "member@pace.test", Token: "must-not-leak", Role: roleMember},
		workspaceRow{Workspace: Workspace{ID: "workspace-id", Name: "Pace", Slug: "pace"}},
	)
	encoded, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "must-not-leak") || strings.Contains(string(encoded), "invite_link") {
		t.Fatalf("public invitation leaked a secret: %s", encoded)
	}
}

func TestWorkspaceSlugAndNameValidation(t *testing.T) {
	for _, slug := range []string{"pace", "pace-team_2", "A1"} {
		if !validWorkspaceSlug(slug) {
			t.Fatalf("valid slug %q rejected", slug)
		}
	}
	for _, slug := range []string{"", "pace team", "pace/team", strings.Repeat("a", 49)} {
		if validWorkspaceSlug(slug) {
			t.Fatalf("invalid slug %q accepted", slug)
		}
	}
	if _, ok := restrictedWorkspaceSlugs["api"]; !ok {
		t.Fatal("Django restricted workspace slug inventory is missing api")
	}
	if hasAlphanumeric("-____-") || !hasAlphanumeric("研发团队") {
		t.Fatal("workspace name alphanumeric validation is not Unicode compatible")
	}
	if !containsURL("visit https://pace.test") || !containsURL("visit pace.test") || containsURL("Pace engineering") {
		t.Fatal("workspace name URL validation mismatch")
	}
}

func TestWorkspaceCreateValidationMatchesDjangoContract(t *testing.T) {
	for _, test := range []struct {
		name       string
		body       string
		statusCode int
		bodyText   string
	}{
		{name: "missing slug", body: `{"name":"Pace"}`, statusCode: http.StatusBadRequest, bodyText: `{"error":"Both name and slug are required"}`},
		{name: "url in name", body: `{"name":"pace.test","slug":"pace"}`, statusCode: http.StatusBadRequest, bodyText: `{"error":"Name cannot contain a URL"}`},
		{name: "symbol-only name", body: `{"name":"-____-","slug":"pace"}`, statusCode: http.StatusBadRequest, bodyText: `{"name":["Name must contain at least one letter or number"]}`},
		{name: "invalid slug", body: `{"name":"Pace","slug":"pace/team"}`, statusCode: http.StatusBadRequest, bodyText: `{"slug":["Slug can only contain letters, numbers, hyphens (-), and underscores (_)"]}`},
		{name: "restricted slug", body: `{"name":"Pace","slug":"api"}`, statusCode: http.StatusBadRequest, bodyText: `{"slug":["Slug is not valid"]}`},
		{name: "blank timezone", body: `{"name":"Pace","slug":"pace","timezone":""}`, statusCode: http.StatusBadRequest, bodyText: `{"timezone":["This field may not be blank."]}`},
		{name: "invalid timezone", body: `{"name":"Pace","slug":"pace","timezone":"Moon/Sea"}`, statusCode: http.StatusBadRequest, bodyText: `{"timezone":["\"Moon/Sea\" is not a valid choice."]}`},
		{name: "null background", body: `{"name":"Pace","slug":"pace","background_color":null}`, statusCode: http.StatusBadRequest, bodyText: `{"background_color":["This field may not be null."]}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			router := gin.New()
			handler := NewHandler(nil, nil, nil, Settings{})
			router.POST("/api/workspaces/", func(c *gin.Context) {
				handler.workspaceCreate(c, &auth.User{ID: "user-id", Email: "owner@pace.test"})
			})
			request := httptest.NewRequest(http.MethodPost, "/api/workspaces/", strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != test.statusCode || response.Body.String() != test.bodyText {
				t.Fatalf("response = %d %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestInvitationTokenUsesHS256JWTContract(t *testing.T) {
	const secret = "pace-workspace-secret"
	token, err := invitationToken(secret, "member@pace.test", time.Unix(1_700_000_000, 0))
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("token has %d parts", len(parts))
	}
	hash := hmac.New(sha256.New, []byte(secret))
	_, _ = hash.Write([]byte(parts[0] + "." + parts[1]))
	wantSignature := base64.RawURLEncoding.EncodeToString(hash.Sum(nil))
	if !hmac.Equal([]byte(parts[2]), []byte(wantSignature)) {
		t.Fatal("invitation token signature is not HS256")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(payload), `"member@pace.test"`) {
		t.Fatalf("payload = %s", payload)
	}
}

func TestWorkspaceMemberDefaultsMatchDjangoShape(t *testing.T) {
	props := decodeJSON(defaultPropsJSON()).(map[string]any)
	display := props["display_filters"].(map[string]any)
	if display["order_by"] != "-created_at" || display["layout"] != "list" || display["sub_issue"] != true {
		t.Fatalf("default display filters = %#v", display)
	}
	issueProps := decodeJSON(issuePropsJSON()).(map[string]any)
	if issueProps["subscribed"] != true || issueProps["all_issues"] != true {
		t.Fatalf("default issue props = %#v", issueProps)
	}
}

func TestWorkspaceLiteUsesStaticLogoAsset(t *testing.T) {
	assetID := "018f3c3e-1234-7abc-9def-1234567890ab"
	data := workspaceLiteJSON(Workspace{ID: "workspace-id", LogoAssetID: &assetID})
	if data["logo_url"] != "/api/assets/v2/static/"+assetID+"/" {
		t.Fatalf("logo_url = %#v", data["logo_url"])
	}
}

func TestMemberCacheInvalidationMatchesDjangoPaths(t *testing.T) {
	cache := &recordingCache{}
	handler := NewHandler(nil, nil, nil, Settings{})
	handler.SetCache(cache)
	if err := handler.invalidateMemberCaches(context.Background(), "pace", "user-id"); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"*/api/workspaces/pace/members/*",
		"/api/users/me/settings/:user-id",
		"*api/users/me/workspaces/*",
	}
	if strings.Join(cache.patterns, "\n") != strings.Join(want, "\n") {
		t.Fatalf("cache patterns = %#v", cache.patterns)
	}
}
