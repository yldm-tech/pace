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

func TestCommunityProxyCutsOverOnlyWorkspaceUserPropertiesRoutes(t *testing.T) {
	config := communityProxyConfig(t)
	matcher := communityProxyMatcher(t, config, "go_workspace_user_properties")
	for _, route := range []string{
		"/api/workspaces/acme/user-properties/",
	} {
		if !matcher.MatchString(route) {
			t.Errorf("Workspace User Properties route %q is not cut over to Go", route)
		}
	}
	for _, route := range []string{
		"/api/workspaces/acme/user-properties/extra/",
		"/api/workspaces/acme/workspace-themes/",
	} {
		if matcher.MatchString(route) {
			t.Errorf("unmigrated Workspace route %q would be cut over to Go", route)
		}
	}
	if !strings.Contains(config, "reverse_proxy @go_workspace_user_properties api-go:8000") {
		t.Error("community proxy is missing the Workspace User Properties reverse proxy")
	}
}

func TestCommunityProxyCutsOverOnlyWorkspaceSidebarPreferencesRoutes(t *testing.T) {
	config := communityProxyConfig(t)
	matcher := communityProxyMatcher(t, config, "go_workspace_sidebar_preferences")
	if !matcher.MatchString("/api/workspaces/acme/sidebar-preferences/") {
		t.Error("Workspace Sidebar Preferences route is not cut over to Go")
	}
	for _, route := range []string{
		"/api/workspaces/acme/sidebar-preferences/views/",
		"/api/workspaces/acme/home-preferences/",
	} {
		if matcher.MatchString(route) {
			t.Errorf("unmigrated Workspace route %q would be cut over to Go", route)
		}
	}
	if !strings.Contains(config, "reverse_proxy @go_workspace_sidebar_preferences api-go:8000") {
		t.Error("community proxy is missing the Workspace Sidebar Preferences reverse proxy")
	}
}

func TestCommunityProxyCutsOverOnlyWorkspaceHomePreferencesRoutes(t *testing.T) {
	config := communityProxyConfig(t)
	matcher := communityProxyMatcher(t, config, "go_workspace_home_preferences")
	for _, route := range []string{
		"/api/workspaces/acme/home-preferences/",
		"/api/workspaces/acme/home-preferences/quick_links/",
	} {
		if !matcher.MatchString(route) {
			t.Errorf("Workspace Home Preferences route %q is not cut over to Go", route)
		}
	}
	for _, route := range []string{
		"/api/workspaces/acme/home-preferences/quick_links/extra/",
		"/api/workspaces/acme/sidebar-preferences/",
		"/api/workspaces/acme/quick-links/",
		"/api/workspaces/acme/recent-visits/",
		"/api/workspaces/acme/stickies/",
	} {
		if matcher.MatchString(route) {
			t.Errorf("unmigrated Workspace route %q would be cut over to Go", route)
		}
	}
	if !strings.Contains(config, "reverse_proxy @go_workspace_home_preferences api-go:8000") {
		t.Error("community proxy is missing the Workspace Home Preferences reverse proxy")
	}
}

func TestCommunityProxyCutsOverOnlyWorkspaceQuickLinkRoutes(t *testing.T) {
	config := communityProxyConfig(t)
	matcher := communityProxyMatcher(t, config, "go_workspace_quick_links")
	for _, route := range []string{
		"/api/workspaces/acme/quick-links/",
		"/api/workspaces/acme/quick-links/01234567-89ab-cdef-0123-456789abcdef/",
	} {
		if !matcher.MatchString(route) {
			t.Errorf("Workspace Quick Link route %q is not cut over to Go", route)
		}
	}
	for _, route := range []string{
		"/api/workspaces/acme/quick-links/01234567-89ab-cdef-0123-456789abcdef/history/",
		"/api/workspaces/acme/home-preferences/",
		"/api/workspaces/acme/stickies/",
		"/api/workspaces/acme/recent-visits/",
	} {
		if matcher.MatchString(route) {
			t.Errorf("unmigrated Workspace route %q would be cut over to Go", route)
		}
	}
	if !strings.Contains(config, "reverse_proxy @go_workspace_quick_links api-go:8000") {
		t.Error("community proxy is missing the Workspace Quick Links reverse proxy")
	}
}

func TestCommunityProxyCutsOverOnlyCoreProjectRoutes(t *testing.T) {
	config := communityProxyConfig(t)
	matcher := communityProxyMatcher(t, config, "go_project_core")
	for _, route := range []string{
		"/api/workspaces/acme/projects/",
		"/api/workspaces/acme/projects/01234567-89ab-cdef-0123-456789abcdef/",
	} {
		if !matcher.MatchString(route) {
			t.Errorf("core Project route %q is not cut over to Go", route)
		}
	}
	// The remaining project routes stay on Django until their own migration.
	for _, route := range []string{
		"/api/workspaces/acme/projects/details/",
		"/api/workspaces/acme/project-identifiers/",
		"/api/workspaces/acme/projects/01234567-89ab-cdef-0123-456789abcdef/members/",
		"/api/workspaces/acme/projects/01234567-89ab-cdef-0123-456789abcdef/invitations/",
		"/api/workspaces/acme/projects/01234567-89ab-cdef-0123-456789abcdef/archive/",
		"/api/workspaces/acme/projects/01234567-89ab-cdef-0123-456789abcdef/project-views/",
		"/api/workspaces/acme/user-favorite-projects/",
	} {
		if matcher.MatchString(route) {
			t.Errorf("unmigrated Project route %q would be cut over to Go", route)
		}
	}
	if !strings.Contains(config, "reverse_proxy @go_project_core api-go:8000") {
		t.Error("community proxy is missing the core Project reverse proxy")
	}
}

func TestCommunityProxyCutsOverOnlyProjectMemberRoutes(t *testing.T) {
	config := communityProxyConfig(t)
	matcher := communityProxyMatcher(t, config, "go_project_members")
	project := "/api/workspaces/acme/projects/01234567-89ab-cdef-0123-456789abcdef/"
	for _, route := range []string{
		project + "members/",
		project + "members/11111111-2222-3333-4444-555555555555/",
		project + "members/leave/",
		project + "project-members/me/",
		project + "project-views/",
		project + "preferences/member/11111111-2222-3333-4444-555555555555/",
	} {
		if !matcher.MatchString(route) {
			t.Errorf("Project member route %q is not cut over to Go", route)
		}
	}
	for _, route := range []string{
		project + "invitations/",
		project + "members/11111111-2222-3333-4444-555555555555/history/",
		project + "preferences/",
		project + "issues/",
		project,
	} {
		if matcher.MatchString(route) {
			t.Errorf("unmigrated Project route %q would be cut over to Go", route)
		}
	}
	for _, directive := range []string{
		"reverse_proxy @go_project_members api-go:8000",
		"reverse_proxy /api/users/me/workspaces/*/project-roles/ api-go:8000",
	} {
		if !strings.Contains(config, directive) {
			t.Errorf("community proxy is missing %q", directive)
		}
	}
}

func TestCommunityProxyCutsOverOnlyProjectLabelRoutes(t *testing.T) {
	config := communityProxyConfig(t)
	matcher := communityProxyMatcher(t, config, "go_project_labels")
	project := "/api/workspaces/acme/projects/01234567-89ab-cdef-0123-456789abcdef/"
	for _, route := range []string{
		project + "issue-labels/",
		project + "issue-labels/11111111-2222-3333-4444-555555555555/",
		project + "bulk-create-labels/",
	} {
		if !matcher.MatchString(route) {
			t.Errorf("Project Label route %q is not cut over to Go", route)
		}
	}
	for _, route := range []string{
		project + "issues/",
		project + "issue-labels/11111111-2222-3333-4444-555555555555/history/",
		project + "bulk-delete-issues/",
		"/api/workspaces/acme/labels/",
	} {
		if matcher.MatchString(route) {
			t.Errorf("unmigrated route %q would be cut over to Go", route)
		}
	}
	if !strings.Contains(config, "reverse_proxy @go_project_labels api-go:8000") {
		t.Error("community proxy is missing the Project Labels reverse proxy")
	}
}

func TestCommunityProxyCutsOverOnlyTheDescriptionVersionRoutes(t *testing.T) {
	config := communityProxyConfig(t)
	matcher := communityProxyMatcher(t, config, "go_work_item_description_versions")
	versions := "/api/workspaces/acme/projects/01234567-89ab-cdef-0123-456789abcdef/work-items/11111111-2222-3333-4444-555555555555/description-versions/"
	for _, route := range []string{versions, versions + "66666666-7777-8888-9999-000000000000/"} {
		if !matcher.MatchString(route) {
			t.Errorf("Description version route %q is not cut over to Go", route)
		}
	}
	for _, route := range []string{
		// The identifier lookup under work-items is a different route and stays on Django.
		"/api/workspaces/acme/work-items/PROJ-12/",
		versions + "66666666-7777-8888-9999-000000000000/extra/",
	} {
		if matcher.MatchString(route) {
			t.Errorf("unmigrated route %q would be cut over to Go", route)
		}
	}
	if !strings.Contains(config, "reverse_proxy @go_work_item_description_versions api-go:8000") {
		t.Error("community proxy is missing the Description versions reverse proxy")
	}
}

func TestCommunityProxyCutsOverOnlyTheProjectIssueOperations(t *testing.T) {
	config := communityProxyConfig(t)
	matcher := communityProxyMatcher(t, config, "go_project_issue_operations")
	project := "/api/workspaces/acme/projects/01234567-89ab-cdef-0123-456789abcdef/"
	for _, route := range []string{
		project + "bulk-archive-issues/",
		project + "bulk-delete-issues/",
		project + "deleted-issues/",
		project + "user-properties/",
		project + "archived-issues/",
		project + "issue-dates/",
		project + "issues-detail/",
		project + "issues/11111111-2222-3333-4444-555555555555/modules/",
	} {
		if !matcher.MatchString(route) {
			t.Errorf("Project issue operation %q is not cut over to Go", route)
		}
	}
	for _, route := range []string{
		// The live list routes have their own matcher.
		project + "issues/",
		project + "issues/list/",
		project + "bulk-create-labels/",
	} {
		if matcher.MatchString(route) {
			t.Errorf("unmigrated route %q would be cut over to Go", route)
		}
	}
	if !strings.Contains(config, "reverse_proxy @go_project_issue_operations api-go:8000") {
		t.Error("community proxy is missing the Project issue operations reverse proxy")
	}
}

func TestCommunityProxyCutsOverOnlyIssueInteractionRoutes(t *testing.T) {
	config := communityProxyConfig(t)
	matcher := communityProxyMatcher(t, config, "go_issue_interactions")
	issue := "/api/workspaces/acme/projects/01234567-89ab-cdef-0123-456789abcdef/issues/11111111-2222-3333-4444-555555555555/"
	for _, route := range []string{
		issue + "reactions/",
		// The reaction code is a free-form string, not a UUID.
		issue + "reactions/thumbsup/",
		issue + "issue-subscribers/",
		issue + "issue-subscribers/66666666-7777-8888-9999-000000000000/",
		issue + "subscribe/",
		issue + "issue-links/",
		issue + "issue-links/77777777-8888-9999-0000-111111111111/",
		issue + "comments/",
		issue + "comments/88888888-9999-0000-1111-222222222222/",
		issue + "sub-issues/",
		issue + "issue-relation/",
		issue + "remove-relation/",
		issue + "archive/",
		issue + "history/",
		issue + "meta/",
		issue + "versions/",
		issue + "versions/66666666-7777-8888-9999-000000000000/",
	} {
		if !matcher.MatchString(route) {
			t.Errorf("Issue interaction route %q is not cut over to Go", route)
		}
	}
	for _, route := range []string{
		// The archived-issues list needs the grouped paginator and stays on Django.
		"/api/workspaces/acme/projects/01234567-89ab-cdef-0123-456789abcdef/archived-issues/",
		issue + "issue-attachments/",
		issue + "reactions/thumbsup/extra/",
		// Only the collection is migrated; there is no sub-issue detail route.
		issue + "sub-issues/99999999-0000-1111-2222-333333333333/",
		// Both relation routes are collections; there is no detail route under either.
		issue + "issue-relation/99999999-0000-1111-2222-333333333333/",
		"/api/workspaces/acme/projects/01234567-89ab-cdef-0123-456789abcdef/comments/11111111-2222-3333-4444-555555555555/reactions/",
	} {
		if matcher.MatchString(route) {
			t.Errorf("unmigrated route %q would be cut over to Go", route)
		}
	}
	if !strings.Contains(config, "reverse_proxy @go_issue_interactions api-go:8000") {
		t.Error("community proxy is missing the Issue interactions reverse proxy")
	}
}

func TestCommunityProxyCutsOverOnlyCommentReactionRoutes(t *testing.T) {
	config := communityProxyConfig(t)
	matcher := communityProxyMatcher(t, config, "go_comment_reactions")
	comment := "/api/workspaces/acme/projects/01234567-89ab-cdef-0123-456789abcdef/comments/11111111-2222-3333-4444-555555555555/"
	for _, route := range []string{comment + "reactions/", comment + "reactions/thumbsup/"} {
		if !matcher.MatchString(route) {
			t.Errorf("Comment reaction route %q is not cut over to Go", route)
		}
	}
	for _, route := range []string{comment, comment + "reactions/thumbsup/extra/"} {
		if matcher.MatchString(route) {
			t.Errorf("unmigrated route %q would be cut over to Go", route)
		}
	}
	if !strings.Contains(config, "reverse_proxy @go_comment_reactions api-go:8000") {
		t.Error("community proxy is missing the Comment reactions reverse proxy")
	}
}

func TestCommunityProxyCutsOverOnlyTheWorkItemIdentifierRoute(t *testing.T) {
	config := communityProxyConfig(t)
	matcher := communityProxyMatcher(t, config, "go_work_item_identifier")
	for _, route := range []string{
		"/api/workspaces/acme/work-items/PROJ-42/",
		// A project identifier may itself contain a dash.
		"/api/workspaces/acme/work-items/MY-PROJ-7/",
	} {
		if !matcher.MatchString(route) {
			t.Errorf("the work item identifier route %q is not cut over to Go", route)
		}
	}
	for _, route := range []string{
		// The description versions live under a project and have their own matcher.
		"/api/workspaces/acme/projects/01234567-89ab-cdef-0123-456789abcdef/work-items/11111111-2222-3333-4444-555555555555/description-versions/",
		"/api/workspaces/acme/work-items/PROJ-42/extra/",
	} {
		if matcher.MatchString(route) {
			t.Errorf("unmigrated route %q would be cut over to Go", route)
		}
	}
	if !strings.Contains(config, "reverse_proxy @go_work_item_identifier api-go:8000") {
		t.Error("community proxy is missing the work item identifier reverse proxy")
	}
}

func TestCommunityProxyCutsOverOnlyTheIssueSyncRoute(t *testing.T) {
	config := communityProxyConfig(t)
	matcher := communityProxyMatcher(t, config, "go_issue_sync")
	project := "/api/workspaces/acme/projects/01234567-89ab-cdef-0123-456789abcdef/"
	if !matcher.MatchString(project + "v2/issues/") {
		t.Error("the issue sync route is not cut over to Go")
	}
	for _, route := range []string{
		project + "issues/",
		project + "v2/issues/11111111-2222-3333-4444-555555555555/",
		project + "v2/cycles/",
	} {
		if matcher.MatchString(route) {
			t.Errorf("unmigrated route %q would be cut over to Go", route)
		}
	}
	if !strings.Contains(config, "reverse_proxy @go_issue_sync api-go:8000") {
		t.Error("community proxy is missing the Issue sync reverse proxy")
	}
}

func TestCommunityProxyCutsOverOnlyTheIssueListRoute(t *testing.T) {
	config := communityProxyConfig(t)
	matcher := communityProxyMatcher(t, config, "go_issue_list")
	project := "/api/workspaces/acme/projects/01234567-89ab-cdef-0123-456789abcdef/"
	for _, route := range []string{project + "issues/", project + "issues/list/"} {
		if !matcher.MatchString(route) {
			t.Errorf("the issue list route %q is not cut over to Go", route)
		}
	}
	for _, route := range []string{
		// The sync route is a separate endpoint with its own matcher.
		project + "v2/issues/",
		// The detail route is its own matcher.
		project + "issues/11111111-2222-3333-4444-555555555555/",
	} {
		if matcher.MatchString(route) {
			t.Errorf("unmigrated route %q would be cut over to Go", route)
		}
	}
	if !strings.Contains(config, "reverse_proxy @go_issue_list api-go:8000") {
		t.Error("community proxy is missing the Issue list reverse proxy")
	}
}

func TestCommunityProxyCutsOverOnlyTheIssueDetailRoute(t *testing.T) {
	config := communityProxyConfig(t)
	matcher := communityProxyMatcher(t, config, "go_issue_detail")
	project := "/api/workspaces/acme/projects/01234567-89ab-cdef-0123-456789abcdef/"
	if !matcher.MatchString(project + "issues/11111111-2222-3333-4444-555555555555/") {
		t.Error("the issue detail route is not cut over to Go")
	}
	for _, route := range []string{
		// The list route needs the grouped paginator and stays on Django.
		project + "issues/",
		project + "issues/11111111-2222-3333-4444-555555555555/comments/",
		project + "issues/11111111-2222-3333-4444-555555555555/sub-issues/",
		project + "issues/11111111-2222-3333-4444-555555555555/archive/",
	} {
		if matcher.MatchString(route) {
			t.Errorf("unmigrated route %q would be cut over to Go", route)
		}
	}
	if !strings.Contains(config, "reverse_proxy @go_issue_detail api-go:8000") {
		t.Error("community proxy is missing the Issue detail reverse proxy")
	}
}

func TestCommunityProxyCutsOverOnlyTheVersionTwoAttachmentRoutes(t *testing.T) {
	config := communityProxyConfig(t)
	matcher := communityProxyMatcher(t, config, "go_issue_attachments")
	attachments := "/api/assets/v2/workspaces/acme/projects/01234567-89ab-cdef-0123-456789abcdef/issues/11111111-2222-3333-4444-555555555555/attachments/"
	for _, route := range []string{
		attachments,
		attachments + "66666666-7777-8888-9999-000000000000/",
	} {
		if !matcher.MatchString(route) {
			t.Errorf("Attachment route %q is not cut over to Go", route)
		}
	}
	for _, route := range []string{
		// The version one endpoint uploads through the API with a multipart body and stays on Django.
		"/api/workspaces/acme/projects/01234567-89ab-cdef-0123-456789abcdef/issues/11111111-2222-3333-4444-555555555555/issue-attachments/",
		// Other asset entities are not migrated.
		"/api/assets/v2/workspaces/acme/projects/01234567-89ab-cdef-0123-456789abcdef/66666666-7777-8888-9999-000000000000/",
		"/api/assets/v2/static/66666666-7777-8888-9999-000000000000/",
		attachments + "66666666-7777-8888-9999-000000000000/extra/",
	} {
		if matcher.MatchString(route) {
			t.Errorf("unmigrated route %q would be cut over to Go", route)
		}
	}
	if !strings.Contains(config, "reverse_proxy @go_issue_attachments api-go:8000") {
		t.Error("community proxy is missing the Issue attachments reverse proxy")
	}
}

func TestCommunityProxyCutsOverOnlyTheMigratedCycleRoutes(t *testing.T) {
	config := communityProxyConfig(t)
	matcher := communityProxyMatcher(t, config, "go_cycle_basics")
	project := "/api/workspaces/acme/projects/01234567-89ab-cdef-0123-456789abcdef/"
	cycle := project + "cycles/11111111-2222-3333-4444-555555555555/"
	for _, route := range []string{
		project + "cycles/date-check/",
		project + "user-favorite-cycles/",
		project + "user-favorite-cycles/11111111-2222-3333-4444-555555555555/",
		cycle + "user-properties/",
		project + "cycles/",
		cycle,
		cycle + "archive/",
		cycle + "cycle-issues/",
		project + "archived-cycles/",
		cycle + "progress/",
		cycle + "analytics/",
		cycle + "transfer-issues/",
	} {
		if !matcher.MatchString(route) {
			t.Errorf("Cycle route %q is not cut over to Go", route)
		}
	}
	for _, route := range []string{
		// The rest of the cycle module is not migrated. The cycle issue detail path serves four methods on Django and only one here, so it stays until the other three exist.
		cycle + "cycle-issues/66666666-7777-8888-9999-000000000000/",
		project + "archived-cycles/11111111-2222-3333-4444-555555555555/",
	} {
		if matcher.MatchString(route) {
			t.Errorf("unmigrated route %q would be cut over to Go", route)
		}
	}
	if !strings.Contains(config, "reverse_proxy @go_cycle_basics api-go:8000") {
		t.Error("community proxy is missing the Cycle basics reverse proxy")
	}
}

func TestCommunityProxyCutsOverOnlyTheWorkspaceViewRoutes(t *testing.T) {
	config := communityProxyConfig(t)
	matcher := communityProxyMatcher(t, config, "go_workspace_views")
	for _, route := range []string{
		"/api/workspaces/acme/views/",
		"/api/workspaces/acme/views/11111111-2222-3333-4444-555555555555/",
	} {
		if !matcher.MatchString(route) {
			t.Errorf("Workspace view route %q is not cut over to Go", route)
		}
	}
	for _, route := range []string{
		// The workspace issue list behind these views is not migrated, and neither are the project-level views.
		"/api/workspaces/acme/issues/",
		"/api/workspaces/acme/workspace-views/",
		"/api/workspaces/acme/projects/01234567-89ab-cdef-0123-456789abcdef/views/",
	} {
		if matcher.MatchString(route) {
			t.Errorf("unmigrated route %q would be cut over to Go", route)
		}
	}
	if !strings.Contains(config, "reverse_proxy @go_workspace_views api-go:8000") {
		t.Error("community proxy is missing the Workspace views reverse proxy")
	}
}

func TestCommunityProxyCutsOverOnlyTheProjectViewRoutes(t *testing.T) {
	config := communityProxyConfig(t)
	matcher := communityProxyMatcher(t, config, "go_project_views")
	project := "/api/workspaces/acme/projects/01234567-89ab-cdef-0123-456789abcdef/"
	for _, route := range []string{
		project + "views/",
		project + "views/11111111-2222-3333-4444-555555555555/",
		project + "user-favorite-views/",
		project + "user-favorite-views/11111111-2222-3333-4444-555555555555/",
	} {
		if !matcher.MatchString(route) {
			t.Errorf("Project view route %q is not cut over to Go", route)
		}
	}
	for _, route := range []string{
		// The workspace-level views are a different app and are not migrated.
		"/api/workspaces/acme/views/",
		"/api/workspaces/acme/views/11111111-2222-3333-4444-555555555555/",
		"/api/workspaces/acme/issues/",
		project + "project-views/",
	} {
		if matcher.MatchString(route) {
			t.Errorf("unmigrated route %q would be cut over to Go", route)
		}
	}
	if !strings.Contains(config, "reverse_proxy @go_project_views api-go:8000") {
		t.Error("community proxy is missing the Project views reverse proxy")
	}
}

func TestCommunityProxyCutsOverOnlyTheMigratedModuleRoutes(t *testing.T) {
	config := communityProxyConfig(t)
	matcher := communityProxyMatcher(t, config, "go_module_basics")
	project := "/api/workspaces/acme/projects/01234567-89ab-cdef-0123-456789abcdef/"
	module := project + "modules/11111111-2222-3333-4444-555555555555/"
	for _, route := range []string{
		project + "user-favorite-modules/",
		project + "user-favorite-modules/11111111-2222-3333-4444-555555555555/",
		module + "user-properties/",
		module + "module-links/",
		module + "module-links/66666666-7777-8888-9999-000000000000/",
		project + "modules/",
		module,
		module + "archive/",
		module + "issues/",
		project + "archived-modules/",
	} {
		if !matcher.MatchString(route) {
			t.Errorf("Module route %q is not cut over to Go", route)
		}
	}
	for _, route := range []string{
		// The rest of the module app is not migrated, and the issue detail path stays on Django whole.
		module + "issues/66666666-7777-8888-9999-000000000000/",
		project + "archived-modules/11111111-2222-3333-4444-555555555555/",
	} {
		if matcher.MatchString(route) {
			t.Errorf("unmigrated route %q would be cut over to Go", route)
		}
	}
	if !strings.Contains(config, "reverse_proxy @go_module_basics api-go:8000") {
		t.Error("community proxy is missing the Module basics reverse proxy")
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
