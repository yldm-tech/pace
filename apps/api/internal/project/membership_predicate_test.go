package project

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api/internal/auth"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// These tests pin the membership join every project-scoped queryset carries, one alias at a time, because the join is held in string literals that nothing else reads: go build says nothing about a string, and the queries themselves only run under a database this suite skips by default.
//
// What they pin is a split that looks like drift and is not. `pm.is_active = TRUE` is the membership test, and it is written everywhere. `pm.deleted_at IS NULL` is the *membership row's* soft delete, which is set only when the parent project or workspace is soft-deleted and only by the worker cascade, and it is written at the sites whose queryset reads project_members through its own manager. A join traversed through another model's queryset carries no such filter — the rule stated at issue_ordering.go:28 and asserted for the analytics scope at advance_analytics_test.go:100 — so every scope below leaves it out on purpose. Adding it here would be a sweep that changes no row (the same cascade stamps the base row, which each of these queries already filters) while breaking parity with Django.
//
// The SQL is rendered rather than reproduced: a dry-run session builds the statement without connecting to anything, so the assertion is over what the database would have been sent.

// placeholderNumbers matches the bind markers the postgres driver numbers, which are put back as question marks so a fragment reads the way the call site wrote it.
var placeholderNumbers = regexp.MustCompile(`\$\d+`)

// dryRunHandler is a Handler whose queries render to SQL instead of reaching a database. DisableAutomaticPing is what keeps gorm.Open from dialling the DSN, which is never connected to.
func dryRunHandler(t *testing.T) *Handler {
	t.Helper()
	database, err := gorm.Open(
		postgres.New(postgres.Config{DSN: "postgres://render@127.0.0.1:1/render"}),
		&gorm.Config{DryRun: true, DisableAutomaticPing: true},
	)
	if err != nil {
		t.Fatalf("opening a dry-run database: %v", err)
	}
	return &Handler{db: database}
}

// renderedSQL is the statement a scope becomes.
func renderedSQL(t *testing.T, scope *gorm.DB) string {
	t.Helper()
	var rows []map[string]any
	statement := scope.Find(&rows).Statement
	if statement.SQL.Len() == 0 {
		t.Fatal("the scope rendered no SQL")
	}
	return placeholderNumbers.ReplaceAllString(statement.SQL.String(), "?")
}

// dryRunContext is a request the scope builders can read their route parameters off.
func dryRunContext(t *testing.T) *gin.Context {
	t.Helper()
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	context.Params = gin.Params{
		{Key: "slug", Value: "acme"},
		{Key: "id", Value: "00000000-0000-0000-0000-0000000000p1"},
		{Key: "module", Value: "00000000-0000-0000-0000-0000000000m1"},
	}
	return context
}

// Every queryset the session API's project routes read through joins the caller's membership, and the alias it joins on is the one its own base table names.
func TestTheProjectScopesJoinTheCallersMembership(t *testing.T) {
	handler := dryRunHandler(t)
	context := dryRunContext(t)
	user := &auth.User{ID: "00000000-0000-0000-0000-0000000000u1"}
	const project = "00000000-0000-0000-0000-0000000000p1"

	for _, test := range []struct {
		name  string
		scope *gorm.DB
		want  string
	}{
		{"memberIssueScope", handler.memberIssueScope(context, user, "acme"),
			"JOIN project_members pm ON pm.project_id = i.project_id AND pm.member_id = ? AND pm.is_active = TRUE"},
		{"memberIssueScopeAnyProject", handler.memberIssueScopeAnyProject(context, user, "acme"),
			"JOIN project_members pm ON pm.project_id = i.project_id AND pm.member_id = ? AND pm.is_active = TRUE"},
		{"projectScopedTable", handler.projectScopedTable(context, user, "cycles", "acme", project),
			"JOIN project_members pm ON pm.project_id = t.project_id AND pm.member_id = ? AND pm.is_active = TRUE"},
		{"viewScope", handler.viewScope(context, user, "acme", project),
			"JOIN project_members pm ON pm.project_id = v.project_id AND pm.member_id = ? AND pm.is_active = TRUE"},
		// A page reaches its project through the link table, so the membership is joined on the link's project rather than the page's.
		{"pageScope", handler.pageScope(context, user, "acme", project),
			"JOIN project_members pm ON pm.project_id = pp.project_id AND pm.member_id = ? AND pm.is_active = TRUE"},
		{"moduleLinkScope", handler.moduleLinkScope(context, user),
			"JOIN project_members pm ON pm.project_id = l.project_id AND pm.member_id = ? AND pm.is_active = TRUE"},
	} {
		t.Run(test.name, func(t *testing.T) {
			rendered := renderedSQL(t, test.scope)
			if !strings.Contains(rendered, test.want) {
				t.Errorf("%s does not carry\n  %s\nit renders\n  %s", test.name, test.want, rendered)
			}
			// The membership's own soft delete belongs to project_members' manager, and a join traversed through another queryset does not apply it.
			if strings.Contains(rendered, "pm.deleted_at") {
				t.Errorf("%s checks the membership's soft delete, and Django's join does not", test.name)
			}
		})
	}
}

// The membership join narrows rather than widens: none of these scopes can reach a row through a LEFT JOIN of it, and each keeps the project-liveness filter that is what actually closes a deleted project.
func TestTheProjectScopesJoinTheMembershipInner(t *testing.T) {
	handler := dryRunHandler(t)
	context := dryRunContext(t)
	user := &auth.User{ID: "00000000-0000-0000-0000-0000000000u1"}
	const project = "00000000-0000-0000-0000-0000000000p1"

	for _, test := range []struct {
		name  string
		scope *gorm.DB
		want  string
	}{
		{"memberIssueScope", handler.memberIssueScope(context, user, "acme"), "JOIN projects p ON p.id = i.project_id AND p.archived_at IS NULL"},
		{"viewScope", handler.viewScope(context, user, "acme", project), "JOIN projects p ON p.id = v.project_id AND p.archived_at IS NULL"},
		{"pageScope", handler.pageScope(context, user, "acme", project), "JOIN project_pages pp ON pp.page_id = p.id AND pp.deleted_at IS NULL"},
		{"moduleLinkScope", handler.moduleLinkScope(context, user), "JOIN projects p ON p.id = l.project_id AND p.archived_at IS NULL"},
	} {
		t.Run(test.name, func(t *testing.T) {
			rendered := renderedSQL(t, test.scope)
			if strings.Contains(rendered, "LEFT JOIN project_members") {
				t.Errorf("%s joins the membership outer, which lets a non-member's row through", test.name)
			}
			if !strings.Contains(rendered, test.want) {
				t.Errorf("%s does not carry\n  %s\nit renders\n  %s", test.name, test.want, rendered)
			}
		})
	}
}
