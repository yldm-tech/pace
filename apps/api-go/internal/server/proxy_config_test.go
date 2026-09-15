package server

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestCommunityProxyCutsOverOnlyCoreWorkspaceRoutes(t *testing.T) {
	config := communityProxyConfig(t)

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

	workspaceMatcher := communityProxyMatcher(t, config, "go_workspace_core")

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

func TestCommunityProxyCutsOverOnlyWorkspaceThemeRoutes(t *testing.T) {
	config := communityProxyConfig(t)
	themeMatcher := communityProxyMatcher(t, config, "go_workspace_themes")

	goRoutes := []string{
		"/api/workspaces/acme/workspace-themes/",
		"/api/workspaces/acme/workspace-themes/01234567-89ab-cdef-0123-456789abcdef/",
	}
	for _, route := range goRoutes {
		if !themeMatcher.MatchString(route) {
			t.Errorf("Workspace Theme route %q is not cut over to Go", route)
		}
	}

	djangoRoutes := []string{
		"/api/workspaces/acme/workspace-themes/not-a-uuid/",
		"/api/workspaces/acme/workspace-themes/01234567-89ab-cdef-0123-456789abcdef/history/",
		"/api/workspaces/acme/labels/",
		"/api/workspaces/acme/user-properties/",
	}
	for _, route := range djangoRoutes {
		if themeMatcher.MatchString(route) {
			t.Errorf("unmigrated Workspace route %q would be cut over to Go", route)
		}
	}

	if !strings.Contains(config, "reverse_proxy @go_workspace_themes api-go:8000") {
		t.Error("community proxy is missing the Workspace Themes reverse proxy")
	}
}

func communityProxyConfig(t *testing.T) string {
	t.Helper()
	configPath := filepath.Join("..", "..", "..", "proxy", "Caddyfile.ce")
	contents, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read %s: %v", configPath, err)
	}
	return string(contents)
}

func communityProxyMatcher(t *testing.T, config, name string) *regexp.Regexp {
	t.Helper()
	matcherLine := regexp.MustCompile(`(?m)^\s*@` + regexp.QuoteMeta(name) + ` path_regexp ` + regexp.QuoteMeta(name) + ` (\S+)\s*$`).FindStringSubmatch(config)
	if len(matcherLine) != 2 {
		t.Fatalf("community proxy is missing the %s path_regexp matcher", name)
	}
	matcher, err := regexp.Compile(matcherLine[1])
	if err != nil {
		t.Fatalf("compile %s matcher: %v", name, err)
	}
	return matcher
}
