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

func TestCommunityProxyCutsOverTheSessionEstimates(t *testing.T) {
	config := communityProxyConfig(t)
	matcher := communityProxyMatcher(t, config, "go_estimates")
	project := "/api/workspaces/acme/projects/01234567-89ab-cdef-0123-456789abcdef/"
	estimate := "11111111-2222-3333-4444-555555555555/"
	point := "66666666-7777-8888-9999-000000000000/"
	for _, route := range []string{
		project + "project-estimates/",
		project + "estimates/",
		project + "estimates/" + estimate,
		project + "estimates/" + estimate + "estimate-points/",
		project + "estimates/" + estimate + "estimate-points/" + point,
	} {
		if !matcher.MatchString(route) {
			t.Errorf("Session estimate route %q is not cut over to Go", route)
		}
	}
	for _, route := range []string{
		// The workspace's own estimate list is a different route and is still Django's.
		"/api/workspaces/acme/estimates/",
		// And the external API's estimates are a different application — and dead there besides.
		"/api/v1/workspaces/acme/projects/01234567-89ab-cdef-0123-456789abcdef/estimates/",
	} {
		if matcher.MatchString(route) {
			t.Errorf("route %q would be cut over by the estimate matcher", route)
		}
	}
	if !strings.Contains(config, "reverse_proxy @go_estimates api-go:8000") {
		t.Error("community proxy is missing the estimate reverse proxy")
	}
}

func TestCommunityProxyCutsOverTheWorkspaceAssets(t *testing.T) {
	config := communityProxyConfig(t)
	matcher := communityProxyMatcher(t, config, "go_workspace_assets")
	asset := "11111111-2222-3333-4444-555555555555/"
	for _, route := range []string{
		"/api/assets/v2/workspaces/acme/",
		"/api/assets/v2/workspaces/acme/" + asset,
		"/api/assets/v2/workspaces/acme/check/" + asset,
		"/api/assets/v2/workspaces/acme/download/" + asset,
		"/api/assets/v2/workspaces/acme/restore/" + asset,
		"/api/assets/v2/static/" + asset,
	} {
		if !matcher.MatchString(route) {
			t.Errorf("Workspace asset route %q is not cut over to Go", route)
		}
	}
	project := "/api/assets/v2/workspaces/acme/projects/01234567-89ab-cdef-0123-456789abcdef/"
	for _, route := range []string{
		project,
		project + asset,
		project + asset + "bulk/",
		project + "download/" + asset,
		"/api/assets/v2/workspaces/acme/duplicate-assets/" + asset,
	} {
		if !matcher.MatchString(route) {
			t.Errorf("Project asset route %q is not cut over to Go", route)
		}
	}
	for _, route := range []string{
		"/api/assets/v2/user-assets/",
		"/api/assets/v2/user-assets/" + asset,
	} {
		if !matcher.MatchString(route) {
			t.Errorf("User asset route %q is not cut over to Go", route)
		}
	}
	for _, route := range []string{
		// The work item attachments under a project are their own route and were cut over separately.
		project + "issues/11111111-2222-3333-4444-555555555555/attachments/",
		// And the external API's user assets are a different application.
		"/api/v1/assets/user-assets/",
	} {
		if matcher.MatchString(route) {
			t.Errorf("unmigrated route %q would be cut over to Go", route)
		}
	}
	if !strings.Contains(config, "reverse_proxy @go_workspace_assets api-go:8000") {
		t.Error("community proxy is missing the workspace asset reverse proxy")
	}
}

func TestCommunityProxyCutsOverTheProjectInvitations(t *testing.T) {
	config := communityProxyConfig(t)
	matcher := communityProxyMatcher(t, config, "go_project_invites")
	project := "/api/workspaces/acme/projects/01234567-89ab-cdef-0123-456789abcdef/"
	invite := "11111111-2222-3333-4444-555555555555/"
	for _, route := range []string{
		project + "invitations/",
		project + "invitations/" + invite,
		project + "join/" + invite,
		"/api/users/me/workspaces/acme/projects/invitations/",
	} {
		if !matcher.MatchString(route) {
			t.Errorf("Project invitation route %q is not cut over to Go", route)
		}
	}
	for _, route := range []string{
		// The workspace's own invitations are a different route and were cut over separately.
		"/api/workspaces/acme/invitations/",
		// And the project roles beside this one are still Django's.
		"/api/users/me/workspaces/acme/project-roles/",
	} {
		if matcher.MatchString(route) {
			t.Errorf("route %q would be cut over by the project invitation matcher", route)
		}
	}
	if !strings.Contains(config, "reverse_proxy @go_project_invites api-go:8000") {
		t.Error("community proxy is missing the project invitation reverse proxy")
	}
}

func TestCommunityProxyCutsOverTheDeployBoards(t *testing.T) {
	config := communityProxyConfig(t)
	matcher := communityProxyMatcher(t, config, "go_deploy_boards")
	project := "/api/workspaces/acme/projects/01234567-89ab-cdef-0123-456789abcdef/"
	board := "11111111-2222-3333-4444-555555555555/"
	for _, route := range []string{
		project + "project-deploy-boards/",
		project + "project-deploy-boards/" + board,
	} {
		if !matcher.MatchString(route) {
			t.Errorf("Deploy board route %q is not cut over to Go", route)
		}
	}
	for _, route := range []string{
		// The space app reads a published project through its anchor, and that is still Django's.
		"/api/public/anchor/abc123/settings/",
	} {
		if matcher.MatchString(route) {
			t.Errorf("route %q would be cut over by the deploy board matcher", route)
		}
	}
	if !strings.Contains(config, "reverse_proxy @go_deploy_boards api-go:8000") {
		t.Error("community proxy is missing the deploy board reverse proxy")
	}
}

func TestCommunityProxyCutsOverTheSpaceAssets(t *testing.T) {
	config := communityProxyConfig(t)
	matcher := communityProxyMatcher(t, config, "go_space_assets")
	base := "/api/public/assets/v2/anchor/0123456789abcdef0123456789abcdef/"
	asset := "11111111-2222-3333-4444-555555555555/"
	for _, route := range []string{
		base,
		base + asset,
		base + asset + "bulk/",
		base + "restore/" + asset,
	} {
		if !matcher.MatchString(route) {
			t.Errorf("Space asset route %q is not cut over to Go", route)
		}
	}
	for _, route := range []string{
		// The application's own assets are a different route.
		"/api/assets/v2/workspaces/acme/",
		"/api/assets/v2/static/" + asset,
	} {
		if matcher.MatchString(route) {
			t.Errorf("route %q would be cut over by the space asset matcher", route)
		}
	}
	if !strings.Contains(config, "reverse_proxy @go_space_assets api-go:8000") {
		t.Error("community proxy is missing the space asset reverse proxy")
	}
}

func TestCommunityProxyCutsOverTheSpaceReadRoutes(t *testing.T) {
	config := communityProxyConfig(t)
	matcher := communityProxyMatcher(t, config, "go_space_read")
	anchor := "/api/public/anchor/0123456789abcdef0123456789abcdef/"
	for _, route := range []string{
		anchor + "settings/",
		anchor + "meta/",
		anchor + "members/",
		anchor + "states/",
		anchor + "labels/",
		anchor + "cycles/",
		anchor + "modules/",
		"/api/public/workspaces/acme/projects/01234567-89ab-cdef-0123-456789abcdef/anchor/",
		anchor + "issues/11111111-2222-3333-4444-555555555555/votes/",
		anchor + "issues/11111111-2222-3333-4444-555555555555/reactions/",
		anchor + "issues/11111111-2222-3333-4444-555555555555/reactions/smile/",
		anchor + "comments/11111111-2222-3333-4444-555555555555/reactions/",
		anchor + "comments/11111111-2222-3333-4444-555555555555/reactions/smile/",
		anchor + "issues/11111111-2222-3333-4444-555555555555/comments/",
		anchor + "issues/11111111-2222-3333-4444-555555555555/comments/66666666-7777-8888-9999-000000000000/",
		anchor + "intakes/11111111-2222-3333-4444-555555555555/intake-issues/",
		anchor + "intakes/11111111-2222-3333-4444-555555555555/intake-issues/66666666-7777-8888-9999-000000000000/",
		anchor + "intakes/11111111-2222-3333-4444-555555555555/inbox-issues/",
		anchor + "issues/11111111-2222-3333-4444-555555555555/",
		anchor + "issues/",
	} {
		if !matcher.MatchString(route) {
			t.Errorf("Space route %q is not cut over to Go", route)
		}
	}
	for _, route := range []string{
		// The space app is complete; what is left under this prefix belongs to other matchers.
		"/api/public/assets/v2/anchor/0123456789abcdef0123456789abcdef/",
	} {
		if matcher.MatchString(route) {
			t.Errorf("route %q would be cut over by the space read matcher", route)
		}
	}
	if !strings.Contains(config, "reverse_proxy @go_space_read api-go:8000") {
		t.Error("community proxy is missing the space reverse proxy")
	}
}

func TestCommunityProxyCutsOverTheProjectDetailRoutes(t *testing.T) {
	config := communityProxyConfig(t)
	matcher := communityProxyMatcher(t, config, "go_project_details")
	for _, route := range []string{
		"/api/workspaces/acme/projects/details/",
		"/api/workspaces/acme/project-identifiers/",
		"/api/workspaces/acme/projects/01234567-89ab-cdef-0123-456789abcdef/archive/",
	} {
		if !matcher.MatchString(route) {
			t.Errorf("Project route %q is not cut over to Go", route)
		}
	}
	for _, route := range []string{
		// The project list itself is its own matcher, and a work item's archive is a different route.
		"/api/workspaces/acme/projects/",
		"/api/workspaces/acme/projects/01234567-89ab-cdef-0123-456789abcdef/issues/11111111-2222-3333-4444-555555555555/archive/",
	} {
		if matcher.MatchString(route) {
			t.Errorf("route %q would be cut over by the project detail matcher", route)
		}
	}
	if !strings.Contains(config, "reverse_proxy @go_project_details api-go:8000") {
		t.Error("community proxy is missing the project detail reverse proxy")
	}
}

func TestCommunityProxyCutsOverTheWorkspaceAggregates(t *testing.T) {
	config := communityProxyConfig(t)
	matcher := communityProxyMatcher(t, config, "go_workspace_aggregates")
	for _, route := range []string{
		"/api/workspaces/acme/labels/",
		"/api/workspaces/acme/states/",
		"/api/workspaces/acme/cycles/",
		"/api/workspaces/acme/modules/",
	} {
		if !matcher.MatchString(route) {
			t.Errorf("Workspace aggregate route %q is not cut over to Go", route)
		}
	}
	for _, route := range []string{
		// A project's own lists are different routes with different shapes.
		"/api/workspaces/acme/projects/01234567-89ab-cdef-0123-456789abcdef/labels/",
		"/api/workspaces/acme/projects/01234567-89ab-cdef-0123-456789abcdef/cycles/",
	} {
		if matcher.MatchString(route) {
			t.Errorf("route %q would be cut over by the workspace aggregate matcher", route)
		}
	}
	if !strings.Contains(config, "reverse_proxy @go_workspace_aggregates api-go:8000") {
		t.Error("community proxy is missing the workspace aggregate reverse proxy")
	}
}

func TestCommunityProxyCutsOverTheAPITokens(t *testing.T) {
	config := communityProxyConfig(t)
	matcher := communityProxyMatcher(t, config, "go_api_tokens")
	for _, route := range []string{
		"/api/users/api-tokens/",
		"/api/users/api-tokens/11111111-2222-3333-4444-555555555555/",
	} {
		if !matcher.MatchString(route) {
			t.Errorf("API token route %q is not cut over to Go", route)
		}
	}
	if matcher.MatchString("/api/users/me/") {
		t.Error("the person's own route would be cut over by the token matcher")
	}
	if !strings.Contains(config, "reverse_proxy @go_api_tokens api-go:8000") {
		t.Error("community proxy is missing the API token reverse proxy")
	}
}

func TestCommunityProxyCutsOverTheAdvanceAnalytics(t *testing.T) {
	config := communityProxyConfig(t)
	matcher := communityProxyMatcher(t, config, "go_advance_analytics")
	for _, route := range []string{
		"/api/workspaces/acme/advance-analytics/",
		"/api/workspaces/acme/advance-analytics-stats/",
		"/api/workspaces/acme/advance-analytics-charts/",
	} {
		if !matcher.MatchString(route) {
			t.Errorf("analytics route %q is not cut over to Go", route)
		}
	}
	for _, route := range []string{
		// The project-scoped copies of all three, which are a separate set of views over the same filters.
		"/api/workspaces/acme/projects/01234567-89ab-cdef-0123-456789abcdef/advance-analytics/",
		"/api/workspaces/acme/projects/01234567-89ab-cdef-0123-456789abcdef/advance-analytics-stats/",
		"/api/workspaces/acme/projects/01234567-89ab-cdef-0123-456789abcdef/advance-analytics-charts/",
	} {
		if !matcher.MatchString(route) {
			t.Errorf("analytics route %q is not cut over to Go", route)
		}
	}
	for _, route := range []string{
		// The older analytics page is a different view and is already served elsewhere.
		"/api/workspaces/acme/analytics/",
		"/api/workspaces/acme/projects/01234567-89ab-cdef-0123-456789abcdef/analytics/",
	} {
		if matcher.MatchString(route) {
			t.Errorf("unmigrated route %q would be cut over to Go", route)
		}
	}
	if !strings.Contains(config, "reverse_proxy @go_advance_analytics api-go:8000") {
		t.Error("community proxy is missing the advance analytics reverse proxy")
	}
}

func TestCommunityProxyCutsOverOnePersonsCornerOfAWorkspace(t *testing.T) {
	config := communityProxyConfig(t)
	matcher := communityProxyMatcher(t, config, "go_workspace_user")
	const person = "11111111-2222-3333-4444-555555555555"
	for _, route := range []string{
		"/api/workspaces/acme/user-profile/" + person + "/",
		"/api/workspaces/acme/user-stats/" + person + "/",
		"/api/workspaces/acme/user-activity/" + person + "/",
		"/api/workspaces/acme/user-activity/" + person + "/export/",
		"/api/workspaces/acme/recent-visits/",
		"/api/workspaces/acme/project-members/",
	} {
		if !matcher.MatchString(route) {
			t.Errorf("route %q is not cut over to Go", route)
		}
	}
	// The workspace's own member list is a different route and stays where it is.
	if matcher.MatchString("/api/workspaces/acme/members/") {
		t.Error("the member list would be cut over by the profile matcher")
	}
	if !strings.Contains(config, "reverse_proxy @go_workspace_user api-go:8000") {
		t.Error("community proxy is missing the workspace user reverse proxy")
	}
}

func TestCommunityProxyCutsOverTheDraftWorkItems(t *testing.T) {
	config := communityProxyConfig(t)
	matcher := communityProxyMatcher(t, config, "go_draft_issues")
	for _, route := range []string{
		"/api/workspaces/acme/draft-issues/",
		"/api/workspaces/acme/draft-issues/11111111-2222-3333-4444-555555555555/",
		"/api/workspaces/acme/draft-to-issue/11111111-2222-3333-4444-555555555555/",
	} {
		if !matcher.MatchString(route) {
			t.Errorf("draft route %q is not cut over to Go", route)
		}
	}
	// The move needs a draft to move, so the bare path is not a route at all.
	if matcher.MatchString("/api/workspaces/acme/draft-to-issue/") {
		t.Error("the bare draft-to-issue path would be cut over")
	}
	if !strings.Contains(config, "reverse_proxy @go_draft_issues api-go:8000") {
		t.Error("community proxy is missing the draft reverse proxy")
	}
}

func TestCommunityProxyCutsOverTheStickies(t *testing.T) {
	config := communityProxyConfig(t)
	matcher := communityProxyMatcher(t, config, "go_stickies")
	for _, route := range []string{
		"/api/workspaces/acme/stickies/",
		"/api/workspaces/acme/stickies/11111111-2222-3333-4444-555555555555/",
	} {
		if !matcher.MatchString(route) {
			t.Errorf("Sticky route %q is not cut over to Go", route)
		}
	}
	for _, route := range []string{
		// The external API's stickies are a different application and were cut over separately.
		"/api/v1/workspaces/acme/stickies/",
	} {
		if matcher.MatchString(route) {
			t.Errorf("route %q would be cut over by the sticky matcher", route)
		}
	}
	if !strings.Contains(config, "reverse_proxy @go_stickies api-go:8000") {
		t.Error("community proxy is missing the sticky reverse proxy")
	}
}

func TestCommunityProxyCutsOverTheFavorites(t *testing.T) {
	config := communityProxyConfig(t)
	matcher := communityProxyMatcher(t, config, "go_favorites")
	favorite := "11111111-2222-3333-4444-555555555555/"
	for _, route := range []string{
		"/api/workspaces/acme/user-favorites/",
		"/api/workspaces/acme/user-favorites/" + favorite,
		"/api/workspaces/acme/user-favorites/" + favorite + "group/",
		"/api/workspaces/acme/user-favorite-projects/",
		"/api/workspaces/acme/user-favorite-projects/" + favorite,
	} {
		if !matcher.MatchString(route) {
			t.Errorf("Favourite route %q is not cut over to Go", route)
		}
	}
	for _, route := range []string{
		// A project's own favourite routes are a different pair and were cut over separately.
		"/api/workspaces/acme/projects/01234567-89ab-cdef-0123-456789abcdef/user-favorite-cycles/",
	} {
		if matcher.MatchString(route) {
			t.Errorf("route %q would be cut over by the favourite matcher", route)
		}
	}
	if !strings.Contains(config, "reverse_proxy @go_favorites api-go:8000") {
		t.Error("community proxy is missing the favourite reverse proxy")
	}
}

func TestCommunityProxyCutsOverTheSessionStates(t *testing.T) {
	config := communityProxyConfig(t)
	matcher := communityProxyMatcher(t, config, "go_states")
	project := "/api/workspaces/acme/projects/01234567-89ab-cdef-0123-456789abcdef/"
	state := "11111111-2222-3333-4444-555555555555/"
	for _, route := range []string{
		project + "states/",
		project + "states/" + state,
		project + "states/" + state + "mark-default/",
		project + "intake-state/",
	} {
		if !matcher.MatchString(route) {
			t.Errorf("Session state route %q is not cut over to Go", route)
		}
	}
	for _, route := range []string{
		// The workspace's own state list is a different route and is still Django's.
		"/api/workspaces/acme/states/",
		// And the external API's states are a different application.
		"/api/v1/workspaces/acme/projects/01234567-89ab-cdef-0123-456789abcdef/states/",
	} {
		if matcher.MatchString(route) {
			t.Errorf("route %q would be cut over by the state matcher", route)
		}
	}
	if !strings.Contains(config, "reverse_proxy @go_states api-go:8000") {
		t.Error("community proxy is missing the state reverse proxy")
	}
}

func TestCommunityProxyCutsOverOnlyTheExternalWorkItems(t *testing.T) {
	config := communityProxyConfig(t)
	matcher := communityProxyMatcher(t, config, "go_external_work_items")
	project := "/api/v1/workspaces/acme/projects/01234567-89ab-cdef-0123-456789abcdef/"
	issue := "11111111-2222-3333-4444-555555555555/"
	for _, route := range []string{
		project + "issues/",
		project + "issues/" + issue,
		project + "work-items/",
		project + "work-items/" + issue,
	} {
		if !matcher.MatchString(route) {
			t.Errorf("External work item route %q is not cut over to Go", route)
		}
	}
	for _, route := range []string{
		// Everything hanging off a work item is its own route and stays where it was.
		project + "issues/" + issue + "comments/",
		project + "issues/" + issue + "issue-attachments/",
		project + "work-items/" + issue + "attachments/",
		"/api/v1/workspaces/acme/issues/search/",
		// The session API's work items are a different application on a different path.
		"/api/workspaces/acme/projects/01234567-89ab-cdef-0123-456789abcdef/issues/",
	} {
		if matcher.MatchString(route) {
			t.Errorf("route %q would be cut over by the work item matcher", route)
		}
	}
	if !strings.Contains(config, "reverse_proxy @go_external_work_items api-go:8000") {
		t.Error("community proxy is missing the external work item reverse proxy")
	}
}

func TestCommunityProxyCutsOverTheExternalUserAssets(t *testing.T) {
	config := communityProxyConfig(t)
	matcher := communityProxyMatcher(t, config, "go_external_user_assets")
	asset := "11111111-2222-3333-4444-555555555555/"
	for _, route := range []string{
		"/api/v1/assets/user-assets/",
		"/api/v1/assets/user-assets/" + asset,
		"/api/v1/assets/user-assets/server/",
		"/api/v1/assets/user-assets/" + asset + "server/",
	} {
		if !matcher.MatchString(route) {
			t.Errorf("External user asset route %q is not cut over to Go", route)
		}
	}
	for _, route := range []string{
		// The workspace's own assets are a different route.
		"/api/v1/workspaces/acme/assets/",
		"/api/assets/v2/user-assets/",
	} {
		if matcher.MatchString(route) {
			t.Errorf("route %q would be cut over by the user asset matcher", route)
		}
	}
	if !strings.Contains(config, "reverse_proxy @go_external_user_assets api-go:8000") {
		t.Error("community proxy is missing the external user asset reverse proxy")
	}
}

func TestCommunityProxyCutsOverOnlyTheExternalAttachments(t *testing.T) {
	config := communityProxyConfig(t)
	matcher := communityProxyMatcher(t, config, "go_external_attachments")
	project := "/api/v1/workspaces/acme/projects/01234567-89ab-cdef-0123-456789abcdef/"
	issue := "11111111-2222-3333-4444-555555555555/"
	asset := "66666666-7777-8888-9999-000000000000/"
	// The two spellings do not carry the same path: the older one says issue-attachments and the newer one attachments.
	for _, route := range []string{
		project + "issues/" + issue + "issue-attachments/",
		project + "issues/" + issue + "issue-attachments/" + asset,
		project + "work-items/" + issue + "attachments/",
		project + "work-items/" + issue + "attachments/" + asset,
	} {
		if !matcher.MatchString(route) {
			t.Errorf("External attachment route %q is not cut over to Go", route)
		}
	}
	for _, route := range []string{
		// Neither spelling serves the other's path.
		project + "issues/" + issue + "attachments/",
		project + "work-items/" + issue + "issue-attachments/",
		"/api/v1/workspaces/acme/assets/",
	} {
		if matcher.MatchString(route) {
			t.Errorf("unserved route %q would be cut over to Go", route)
		}
	}
	if !strings.Contains(config, "reverse_proxy @go_external_attachments api-go:8000") {
		t.Error("community proxy is missing the external attachments reverse proxy")
	}
}

func TestCommunityProxyCutsOverOnlyTheExternalIssueSearch(t *testing.T) {
	config := communityProxyConfig(t)
	matcher := communityProxyMatcher(t, config, "go_external_issue_search")
	for _, name := range []string{"issues", "work-items"} {
		for _, route := range []string{
			"/api/v1/workspaces/acme/" + name + "/search/",
			// The one route addressed by something other than a uuid.
			"/api/v1/workspaces/acme/" + name + "/PROJ-42/",
		} {
			if !matcher.MatchString(route) {
				t.Errorf("External search route %q is not cut over to Go", route)
			}
		}
	}
	for _, route := range []string{
		// The session API's workspace issue list is a different path and stays on Django.
		"/api/workspaces/acme/issues/",
		"/api/v1/workspaces/acme/issues/",
		"/api/v1/workspaces/acme/projects/01234567-89ab-cdef-0123-456789abcdef/issues/",
	} {
		if matcher.MatchString(route) {
			t.Errorf("unmigrated route %q would be cut over to Go", route)
		}
	}
	if !strings.Contains(config, "reverse_proxy @go_external_issue_search api-go:8000") {
		t.Error("community proxy is missing the external issue search reverse proxy")
	}
}

func TestCommunityProxyCutsOverOnlyTheExternalIssueRelations(t *testing.T) {
	config := communityProxyConfig(t)
	matcher := communityProxyMatcher(t, config, "go_external_relations")
	project := "/api/v1/workspaces/acme/projects/01234567-89ab-cdef-0123-456789abcdef/"
	issue := "11111111-2222-3333-4444-555555555555/"
	if !matcher.MatchString(project + "work-items/" + issue + "relations/") {
		t.Error("the external relations route is not cut over to Go")
	}
	for _, route := range []string{
		// Relations are the one work item route mounted under a single name: the older spelling answers 404 on Django.
		project + "issues/" + issue + "relations/",
		project + "work-items/" + issue + "relations/66666666-7777-8888-9999-000000000000/",
	} {
		if matcher.MatchString(route) {
			t.Errorf("unserved route %q would be cut over to Go", route)
		}
	}
	if !strings.Contains(config, "reverse_proxy @go_external_relations api-go:8000") {
		t.Error("community proxy is missing the external relations reverse proxy")
	}
}

func TestCommunityProxyCutsOverOnlyTheExternalIssueActivities(t *testing.T) {
	config := communityProxyConfig(t)
	matcher := communityProxyMatcher(t, config, "go_external_activities")
	project := "/api/v1/workspaces/acme/projects/01234567-89ab-cdef-0123-456789abcdef/"
	issue := "11111111-2222-3333-4444-555555555555/"
	for _, name := range []string{"issues", "work-items"} {
		for _, route := range []string{
			project + name + "/" + issue + "activities/",
			project + name + "/" + issue + "activities/66666666-7777-8888-9999-000000000000/",
		} {
			if !matcher.MatchString(route) {
				t.Errorf("External activity route %q is not cut over to Go", route)
			}
		}
	}
	for _, route := range []string{
		project + "issues/" + issue + "attachments/",
		project + "work-items/" + issue + "relations/",
	} {
		if matcher.MatchString(route) {
			t.Errorf("unmigrated route %q would be cut over to Go", route)
		}
	}
	if !strings.Contains(config, "reverse_proxy @go_external_activities api-go:8000") {
		t.Error("community proxy is missing the external activities reverse proxy")
	}
}

func TestCommunityProxyCutsOverOnlyTheExternalIssueComments(t *testing.T) {
	config := communityProxyConfig(t)
	matcher := communityProxyMatcher(t, config, "go_external_comments")
	project := "/api/v1/workspaces/acme/projects/01234567-89ab-cdef-0123-456789abcdef/"
	issue := "11111111-2222-3333-4444-555555555555/"
	comment := "66666666-7777-8888-9999-000000000000/"
	for _, name := range []string{"issues", "work-items"} {
		for _, route := range []string{
			project + name + "/" + issue + "comments/",
			project + name + "/" + issue + "comments/" + comment,
		} {
			if !matcher.MatchString(route) {
				t.Errorf("External comment route %q is not cut over to Go", route)
			}
		}
	}
	for _, route := range []string{
		project + "issues/" + issue + "activities/",
		project + "work-items/" + issue + "attachments/",
	} {
		if matcher.MatchString(route) {
			t.Errorf("unmigrated route %q would be cut over to Go", route)
		}
	}
	if !strings.Contains(config, "reverse_proxy @go_external_comments api-go:8000") {
		t.Error("community proxy is missing the external comments reverse proxy")
	}
}

func TestCommunityProxyCutsOverOnlyTheExternalIssueLinks(t *testing.T) {
	config := communityProxyConfig(t)
	matcher := communityProxyMatcher(t, config, "go_external_links")
	project := "/api/v1/workspaces/acme/projects/01234567-89ab-cdef-0123-456789abcdef/"
	issue := "11111111-2222-3333-4444-555555555555/"
	link := "66666666-7777-8888-9999-000000000000/"
	for _, name := range []string{"issues", "work-items"} {
		for _, route := range []string{
			project + name + "/" + issue + "links/",
			project + name + "/" + issue + "links/" + link,
		} {
			if !matcher.MatchString(route) {
				t.Errorf("External link route %q is not cut over to Go", route)
			}
		}
	}
	for _, route := range []string{
		// The comments, activities, attachments and relations are not migrated.
		project + "issues/" + issue + "comments/",
		project + "work-items/" + issue + "attachments/",
		project + "issues/" + issue,
	} {
		if matcher.MatchString(route) {
			t.Errorf("unmigrated route %q would be cut over to Go", route)
		}
	}
	if !strings.Contains(config, "reverse_proxy @go_external_links api-go:8000") {
		t.Error("community proxy is missing the external links reverse proxy")
	}
}

func TestCommunityProxyCutsOverTheExternalProjectCollection(t *testing.T) {
	config := communityProxyConfig(t)
	matcher := communityProxyMatcher(t, config, "go_external_project_crud")
	project := "/api/v1/workspaces/acme/projects/01234567-89ab-cdef-0123-456789abcdef/"
	for _, route := range []string{
		project,
		"/api/v1/workspaces/acme/projects/",
	} {
		if !matcher.MatchString(route) {
			t.Errorf("External project route %q is not cut over to Go", route)
		}
	}
	for _, route := range []string{
		// Everything hanging off a project is its own route.
		project + "states/",
		project + "cycles/",
		"/api/v1/workspaces/acme/projects-lite/",
	} {
		if matcher.MatchString(route) {
			t.Errorf("route %q would be cut over by the project matcher", route)
		}
	}
	if !strings.Contains(config, "reverse_proxy @go_external_project_crud api-go:8000") {
		t.Error("community proxy is missing the external project reverse proxy")
	}
}

func TestCommunityProxyCutsOverOnlyTheExternalModuleRoutes(t *testing.T) {
	config := communityProxyConfig(t)
	matcher := communityProxyMatcher(t, config, "go_external_modules")
	project := "/api/v1/workspaces/acme/projects/01234567-89ab-cdef-0123-456789abcdef/"
	module := "11111111-2222-3333-4444-555555555555/"
	for _, route := range []string{
		project + "modules-lite/",
		project + "archived-modules/",
		project + "archived-modules/" + module + "unarchive/",
		project + "modules/" + module + "archive/",
		project + "modules/",
		project + "modules/" + module,
		project + "modules/" + module + "module-issues/",
		project + "modules/" + module + "module-issues/66666666-7777-8888-9999-000000000000/",
	} {
		if !matcher.MatchString(route) {
			t.Errorf("External module route %q is not cut over to Go", route)
		}
	}
	for _, route := range []string{
		// The archived module detail is the one route here that is not migrated.
		project + "archived-modules/" + module,
	} {
		if matcher.MatchString(route) {
			t.Errorf("unmigrated route %q would be cut over to Go", route)
		}
	}
	if !strings.Contains(config, "reverse_proxy @go_external_modules api-go:8000") {
		t.Error("community proxy is missing the external modules reverse proxy")
	}
}

func TestCommunityProxyCutsOverOnlyTheExternalCycleRoutes(t *testing.T) {
	config := communityProxyConfig(t)
	matcher := communityProxyMatcher(t, config, "go_external_cycles")
	project := "/api/v1/workspaces/acme/projects/01234567-89ab-cdef-0123-456789abcdef/"
	cycle := "11111111-2222-3333-4444-555555555555/"
	for _, route := range []string{
		project + "cycles/",
		project + "cycles/" + cycle,
		project + "cycles-lite/",
		project + "archived-cycles/",
		project + "archived-cycles/" + cycle + "unarchive/",
		project + "cycles/" + cycle + "archive/",
		project + "cycles/" + cycle + "cycle-issues/",
		project + "cycles/" + cycle + "cycle-issues/66666666-7777-8888-9999-000000000000/",
		project + "cycles/" + cycle + "transfer-issues/",
	} {
		if !matcher.MatchString(route) {
			t.Errorf("External cycle route %q is not cut over to Go", route)
		}
	}
	for _, route := range []string{
		// Django binds no detail under the archived list, so the matcher must not claim one.
		project + "archived-cycles/" + cycle,
	} {
		if matcher.MatchString(route) {
			t.Errorf("unmigrated route %q would be cut over to Go", route)
		}
	}
	if !strings.Contains(config, "reverse_proxy @go_external_cycles api-go:8000") {
		t.Error("community proxy is missing the external cycles reverse proxy")
	}
}

func TestCommunityProxyCutsOverOnlyTheExternalAssetRoutes(t *testing.T) {
	config := communityProxyConfig(t)
	matcher := communityProxyMatcher(t, config, "go_external_assets")
	for _, route := range []string{
		"/api/v1/workspaces/acme/assets/",
		"/api/v1/workspaces/acme/assets/11111111-2222-3333-4444-555555555555/",
	} {
		if !matcher.MatchString(route) {
			t.Errorf("External asset route %q is not cut over to Go", route)
		}
	}
	for _, route := range []string{
		// The user asset routes are a separate endpoint and are not migrated.
		"/api/v1/assets/user-assets/",
		"/api/v1/assets/user-assets/11111111-2222-3333-4444-555555555555/",
		"/api/v1/assets/user-assets/11111111-2222-3333-4444-555555555555/server/",
	} {
		if matcher.MatchString(route) {
			t.Errorf("unmigrated route %q would be cut over to Go", route)
		}
	}
	if !strings.Contains(config, "reverse_proxy @go_external_assets api-go:8000") {
		t.Error("community proxy is missing the external assets reverse proxy")
	}
}

func TestCommunityProxyCutsOverOnlyTheExternalIntakeRoutes(t *testing.T) {
	config := communityProxyConfig(t)
	matcher := communityProxyMatcher(t, config, "go_external_intake")
	project := "/api/v1/workspaces/acme/projects/01234567-89ab-cdef-0123-456789abcdef/"
	for _, route := range []string{
		project + "intake-issues/",
		project + "intake-issues/11111111-2222-3333-4444-555555555555/",
	} {
		if !matcher.MatchString(route) {
			t.Errorf("External intake route %q is not cut over to Go", route)
		}
	}
	for _, route := range []string{
		// The session API's intake issues are a different app and a different path.
		"/api/workspaces/acme/projects/01234567-89ab-cdef-0123-456789abcdef/intake-issues/",
		project + "issues/",
	} {
		if matcher.MatchString(route) {
			t.Errorf("unmigrated route %q would be cut over to Go", route)
		}
	}
	if !strings.Contains(config, "reverse_proxy @go_external_intake api-go:8000") {
		t.Error("community proxy is missing the external intake reverse proxy")
	}
}

func TestCommunityProxyCutsOverOnlyTheExternalStickyAndInviteRoutes(t *testing.T) {
	config := communityProxyConfig(t)
	matcher := communityProxyMatcher(t, config, "go_external_stickies")
	workspace := "/api/v1/workspaces/acme/"
	identifier := "11111111-2222-3333-4444-555555555555/"
	for _, route := range []string{
		workspace + "stickies/",
		workspace + "stickies/" + identifier,
		workspace + "invitations/",
		workspace + "invitations/" + identifier,
	} {
		if !matcher.MatchString(route) {
			t.Errorf("External route %q is not cut over to Go", route)
		}
	}
	for _, route := range []string{
		// The format-suffix routes a DRF router appends stay on Django: nothing here claims them.
		workspace + "stickies.json",
		workspace + "stickies/11111111-2222-3333-4444-555555555555.json",
		workspace + "invitations.json",
	} {
		if matcher.MatchString(route) {
			t.Errorf("unmigrated route %q would be cut over to Go", route)
		}
	}
	if !strings.Contains(config, "reverse_proxy @go_external_stickies api-go:8000") {
		t.Error("community proxy is missing the external stickies reverse proxy")
	}
}

func TestCommunityProxyCutsOverOnlyTheExternalLabelRoutes(t *testing.T) {
	config := communityProxyConfig(t)
	matcher := communityProxyMatcher(t, config, "go_external_labels")
	project := "/api/v1/workspaces/acme/projects/01234567-89ab-cdef-0123-456789abcdef/"
	for _, route := range []string{
		project + "labels/",
		project + "labels/11111111-2222-3333-4444-555555555555/",
	} {
		if !matcher.MatchString(route) {
			t.Errorf("External label route %q is not cut over to Go", route)
		}
	}
	for _, route := range []string{
		project + "issues/",
		// The external estimate urls exist as a file but are never wired into the URLconf, so Django answers 404 and nothing here may claim them.
		project + "estimates/",
	} {
		if matcher.MatchString(route) {
			t.Errorf("unmigrated route %q would be cut over to Go", route)
		}
	}
	if !strings.Contains(config, "reverse_proxy @go_external_labels api-go:8000") {
		t.Error("community proxy is missing the external labels reverse proxy")
	}
}

func TestCommunityProxyCutsOverOnlyTheExternalMemberRoutes(t *testing.T) {
	config := communityProxyConfig(t)
	matcher := communityProxyMatcher(t, config, "go_external_members")
	project := "/api/v1/workspaces/acme/projects/01234567-89ab-cdef-0123-456789abcdef/"
	member := "11111111-2222-3333-4444-555555555555/"
	for _, route := range []string{
		"/api/v1/workspaces/acme/members/",
		"/api/v1/workspaces/acme/members-lite/",
		// The same endpoints are mounted under both names.
		project + "members/",
		project + "members/" + member,
		project + "project-members/",
		project + "project-members/" + member,
		project + "project-members-lite/",
	} {
		if !matcher.MatchString(route) {
			t.Errorf("External member route %q is not cut over to Go", route)
		}
	}
	for _, route := range []string{
		"/api/v1/workspaces/acme/members/" + member,
		project + "members-lite/",
		project + "states/",
	} {
		if matcher.MatchString(route) {
			t.Errorf("unmigrated route %q would be cut over to Go", route)
		}
	}
	if !strings.Contains(config, "reverse_proxy @go_external_members api-go:8000") {
		t.Error("community proxy is missing the external members reverse proxy")
	}
}

func TestCommunityProxyCutsOverOnlyTheExternalProjectRoutes(t *testing.T) {
	config := communityProxyConfig(t)
	matcher := communityProxyMatcher(t, config, "go_external_projects")
	project := "/api/v1/workspaces/acme/projects/01234567-89ab-cdef-0123-456789abcdef/"
	for _, route := range []string{
		"/api/v1/workspaces/acme/projects-lite/",
		project + "archive/",
		project + "summary/",
	} {
		if !matcher.MatchString(route) {
			t.Errorf("External project route %q is not cut over to Go", route)
		}
	}
	for _, route := range []string{
		// The project list, create and detail need the full serializer and are not migrated.
		"/api/v1/workspaces/acme/projects/",
		project,
	} {
		if matcher.MatchString(route) {
			t.Errorf("unmigrated route %q would be cut over to Go", route)
		}
	}
	if !strings.Contains(config, "reverse_proxy @go_external_projects api-go:8000") {
		t.Error("community proxy is missing the external projects reverse proxy")
	}
}

func TestCommunityProxyCutsOverOnlyTheExternalStateRoutes(t *testing.T) {
	config := communityProxyConfig(t)
	matcher := communityProxyMatcher(t, config, "go_external_states")
	project := "/api/v1/workspaces/acme/projects/01234567-89ab-cdef-0123-456789abcdef/"
	for _, route := range []string{
		project + "states/",
		project + "states/11111111-2222-3333-4444-555555555555/",
	} {
		if !matcher.MatchString(route) {
			t.Errorf("External state route %q is not cut over to Go", route)
		}
	}
	for _, route := range []string{
		// The rest of the external API is not migrated.
		project + "issues/",
		project + "cycles/",
		project + "modules/",
		"/api/v1/workspaces/acme/projects/",
	} {
		if matcher.MatchString(route) {
			t.Errorf("unmigrated route %q would be cut over to Go", route)
		}
	}
	if !strings.Contains(config, "reverse_proxy @go_external_states api-go:8000") {
		t.Error("community proxy is missing the external states reverse proxy")
	}
}

func TestCommunityProxyCutsOverOnlyTheAnalyticViewRoutes(t *testing.T) {
	config := communityProxyConfig(t)
	matcher := communityProxyMatcher(t, config, "go_analytic_views")
	for _, route := range []string{
		"/api/workspaces/acme/analytic-view/",
		"/api/workspaces/acme/analytic-view/11111111-2222-3333-4444-555555555555/",
		"/api/workspaces/acme/export-analytics/",
		"/api/workspaces/acme/analytics/",
		"/api/workspaces/acme/saved-analytic-view/11111111-2222-3333-4444-555555555555/",
		"/api/workspaces/acme/default-analytics/",
		"/api/workspaces/acme/project-stats/",
	} {
		if !matcher.MatchString(route) {
			t.Errorf("Analytic view route %q is not cut over to Go", route)
		}
	}
	for _, route := range []string{
		// The three advance-analytics endpoints are not migrated.
		"/api/workspaces/acme/advance-analytics/",
		"/api/workspaces/acme/advance-analytics-stats/",
		"/api/workspaces/acme/advance-analytics-charts/",
	} {
		if matcher.MatchString(route) {
			t.Errorf("unmigrated route %q would be cut over to Go", route)
		}
	}
	if !strings.Contains(config, "reverse_proxy @go_analytic_views api-go:8000") {
		t.Error("community proxy is missing the Analytic views reverse proxy")
	}
}

func TestCommunityProxyCutsOverOnlyTheWebhookRoutes(t *testing.T) {
	config := communityProxyConfig(t)
	matcher := communityProxyMatcher(t, config, "go_webhooks")
	webhook := "/api/workspaces/acme/webhooks/11111111-2222-3333-4444-555555555555/"
	for _, route := range []string{
		"/api/workspaces/acme/webhooks/",
		webhook,
		webhook + "regenerate/",
		"/api/workspaces/acme/webhook-logs/11111111-2222-3333-4444-555555555555/",
	} {
		if !matcher.MatchString(route) {
			t.Errorf("Webhook route %q is not cut over to Go", route)
		}
	}
	for _, route := range []string{
		"/api/workspaces/acme/webhook-logs/",
		webhook + "something-else/",
	} {
		if matcher.MatchString(route) {
			t.Errorf("unmigrated route %q would be cut over to Go", route)
		}
	}
	if !strings.Contains(config, "reverse_proxy @go_webhooks api-go:8000") {
		t.Error("community proxy is missing the Webhooks reverse proxy")
	}
}

func TestCommunityProxyCutsOverOnlyTheWorkspaceSearches(t *testing.T) {
	config := communityProxyConfig(t)
	matcher := communityProxyMatcher(t, config, "go_global_search")
	for _, route := range []string{
		"/api/workspaces/acme/search/",
		"/api/workspaces/acme/entity-search/",
	} {
		if !matcher.MatchString(route) {
			t.Errorf("workspace search route %q is not cut over to Go", route)
		}
	}
	for _, route := range []string{
		"/api/workspaces/acme/projects/01234567-89ab-cdef-0123-456789abcdef/search-issues/",
		"/api/workspaces/acme/search/something/",
	} {
		if matcher.MatchString(route) {
			t.Errorf("unmigrated route %q would be cut over to Go", route)
		}
	}
	if !strings.Contains(config, "reverse_proxy @go_global_search api-go:8000") {
		t.Error("community proxy is missing the global search reverse proxy")
	}
}

func TestCommunityProxyCutsOverOnlyTheIssueSearch(t *testing.T) {
	config := communityProxyConfig(t)
	matcher := communityProxyMatcher(t, config, "go_issue_search")
	project := "/api/workspaces/acme/projects/01234567-89ab-cdef-0123-456789abcdef/"
	if !matcher.MatchString(project + "search-issues/") {
		t.Error("the issue search is not cut over to Go")
	}
	for _, route := range []string{
		// The workspace-wide searches are a different endpoint and are not migrated.
		"/api/workspaces/acme/search/",
		"/api/workspaces/acme/entity-search/",
		project + "issues/",
	} {
		if matcher.MatchString(route) {
			t.Errorf("unmigrated route %q would be cut over to Go", route)
		}
	}
	if !strings.Contains(config, "reverse_proxy @go_issue_search api-go:8000") {
		t.Error("community proxy is missing the issue search reverse proxy")
	}
}

func TestCommunityProxyCutsOverOnlyTheIntakeRoutes(t *testing.T) {
	config := communityProxyConfig(t)
	matcher := communityProxyMatcher(t, config, "go_intakes")
	project := "/api/workspaces/acme/projects/01234567-89ab-cdef-0123-456789abcdef/"
	for _, route := range []string{
		project + "intakes/",
		project + "intakes/11111111-2222-3333-4444-555555555555/",
		// The viewset is mounted twice, under its current name and the one it had before intake was called inbox.
		project + "inboxes/",
		project + "inboxes/11111111-2222-3333-4444-555555555555/",
	} {
		if !matcher.MatchString(route) {
			t.Errorf("Intake route %q is not cut over to Go", route)
		}
	}
	for _, route := range []string{
		// The issues inside an intake are a separate viewset, mounted under both names as well.
		project + "intake-issues/",
		project + "inbox-issues/",
		project + "intake-issues/11111111-2222-3333-4444-555555555555/",
		project + "inbox-issues/11111111-2222-3333-4444-555555555555/",
		// The intake's own copy of the description versions, which is a different list from the work item's.
		project + "intake-work-items/11111111-2222-3333-4444-555555555555/description-versions/",
		project + "intake-work-items/11111111-2222-3333-4444-555555555555/description-versions/66666666-7777-8888-9999-000000000000/",
	} {
		if !matcher.MatchString(route) {
			t.Errorf("Intake route %q is not cut over to Go", route)
		}
	}
	for _, route := range []string{
		// The published board reads its intake through the space app, which is a different matcher.
		"/api/public/anchor/abc/intakes/11111111-2222-3333-4444-555555555555/intake-issues/",
	} {
		if matcher.MatchString(route) {
			t.Errorf("unmigrated route %q would be cut over to Go", route)
		}
	}
	if !strings.Contains(config, "reverse_proxy @go_intakes api-go:8000") {
		t.Error("community proxy is missing the Intakes reverse proxy")
	}
}

func TestCommunityProxyCutsOverOnlyTheMigratedPageRoutes(t *testing.T) {
	config := communityProxyConfig(t)
	matcher := communityProxyMatcher(t, config, "go_pages")
	project := "/api/workspaces/acme/projects/01234567-89ab-cdef-0123-456789abcdef/"
	page := project + "pages/11111111-2222-3333-4444-555555555555/"
	for _, route := range []string{
		project + "pages-summary/",
		project + "pages/",
		page,
		page + "lock/",
		page + "access/",
		page + "archive/",
		project + "favorite-pages/11111111-2222-3333-4444-555555555555/",
		page + "description/",
		page + "versions/",
		page + "versions/66666666-7777-8888-9999-000000000000/",
		page + "duplicate/",
	} {
		if !matcher.MatchString(route) {
			t.Errorf("Page route %q is not cut over to Go", route)
		}
	}
	for _, route := range []string{
		// The workspace-level pages are a different app.
		"/api/workspaces/acme/pages/",
		page + "something-else/",
	} {
		if matcher.MatchString(route) {
			t.Errorf("unmigrated route %q would be cut over to Go", route)
		}
	}
	if !strings.Contains(config, "reverse_proxy @go_pages api-go:8000") {
		t.Error("community proxy is missing the Pages reverse proxy")
	}
}

func TestCommunityProxyCutsOverOnlyTheNotificationRoutes(t *testing.T) {
	config := communityProxyConfig(t)
	matcher := communityProxyMatcher(t, config, "go_notifications")
	base := "/api/workspaces/acme/users/notifications/"
	notification := base + "11111111-2222-3333-4444-555555555555/"
	for _, route := range []string{
		base,
		base + "unread/",
		base + "mark-all-read/",
		notification,
		notification + "read/",
		notification + "archive/",
	} {
		if !matcher.MatchString(route) {
			t.Errorf("Notification route %q is not cut over to Go", route)
		}
	}
	for _, route := range []string{
		// The notification preferences live under the user app, not this one.
		"/api/users/me/notification-preferences/",
		notification + "something-else/",
	} {
		if matcher.MatchString(route) {
			t.Errorf("unmigrated route %q would be cut over to Go", route)
		}
	}
	if !strings.Contains(config, "reverse_proxy @go_notifications api-go:8000") {
		t.Error("community proxy is missing the Notifications reverse proxy")
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
