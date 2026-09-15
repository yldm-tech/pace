package server

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestCommunityProxyCutsOverOnlyCoreWorkspaceRoutes(t *testing.T) {
	configPath := filepath.Join("..", "..", "..", "proxy", "Caddyfile.ce")
	contents, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read %s: %v", configPath, err)
	}
	config := string(contents)

	staticRoutes := []string{
		"/api/workspace-slug-check/",
		"/api/workspaces/",
		"/api/users/me/workspaces/",
		"/api/users/me/workspaces/invitations/",
	}
	for _, route := range staticRoutes {
		directive := "reverse_proxy " + route + " api-go:8000"
		if !strings.Contains(config, directive) {
			t.Errorf("community proxy is missing %q", directive)
		}
	}

	matcherLine := regexp.MustCompile(`(?m)^\s*@go_workspace_core path_regexp go_workspace_core (\S+)\s*$`).FindStringSubmatch(config)
	if len(matcherLine) != 2 {
		t.Fatal("community proxy is missing the go_workspace_core path_regexp matcher")
	}
	workspaceMatcher, err := regexp.Compile(matcherLine[1])
	if err != nil {
		t.Fatalf("compile go_workspace_core matcher: %v", err)
	}

	goRoutes := []string{
		"/api/workspaces/acme/",
		"/api/workspaces/acme/members/",
		"/api/workspaces/acme/members/leave/",
		"/api/workspaces/acme/members/01234567-89ab-cdef-0123-456789abcdef/",
		"/api/workspaces/acme/workspace-members/me/",
		"/api/workspaces/acme/workspace-views/",
		"/api/workspaces/acme/invitations/",
		"/api/workspaces/acme/invitations/01234567-89ab-cdef-0123-456789abcdef/",
		"/api/workspaces/acme/invitations/01234567-89ab-cdef-0123-456789abcdef/join/",
	}
	for _, route := range goRoutes {
		if !workspaceMatcher.MatchString(route) {
			t.Errorf("core Workspace route %q is not cut over to Go", route)
		}
	}

	djangoRoutes := []string{
		"/api/workspaces/acme/workspace-themes/",
		"/api/workspaces/acme/labels/",
		"/api/workspaces/acme/states/",
		"/api/workspaces/acme/estimates/",
		"/api/workspaces/acme/favorites/",
		"/api/workspaces/acme/quick-links/",
		"/api/workspaces/acme/draft-issues/",
		"/api/workspaces/acme/activities/",
		"/api/workspaces/acme/dashboard/",
		"/api/workspaces/acme/export/",
		"/api/workspaces/acme/members/admins/",
		"/api/workspaces/acme/invitations/01234567-89ab-cdef-0123-456789abcdef/resend/",
	}
	for _, route := range djangoRoutes {
		if workspaceMatcher.MatchString(route) {
			t.Errorf("unmigrated Workspace route %q would be cut over to Go", route)
		}
	}

	matcherProxy := "reverse_proxy @go_workspace_core api-go:8000"
	if !strings.Contains(config, matcherProxy) {
		t.Errorf("community proxy is missing %q", matcherProxy)
	}
	if !strings.Contains(config, "reverse_proxy /api/* api:8000") {
		t.Error("community proxy is missing the Django API fallback")
	}
}
