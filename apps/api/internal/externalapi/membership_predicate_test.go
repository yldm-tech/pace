package externalapi

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

// These tests pin the membership join every external-API queryset carries. The join is the whole gate on these routes — the API-key middleware at auth.go checks the key and the user, not the project — and it lives in string literals nothing else reads, so this renders the statement and reads the fragment back out of it.
//
// As in the session API, the membership row's own soft delete is deliberately absent: it is set only by the worker cascade when the parent project or workspace is soft-deleted, and a join traversed through another model's queryset does not apply the joined model's manager. The base row's `deleted_at IS NULL` in the same statement is what excludes a cascaded row, and it is stamped by the same cascade, so the filter left out here would change no row.

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
		{Key: "project", Value: "00000000-0000-0000-0000-0000000000p1"},
		{Key: "issue", Value: "00000000-0000-0000-0000-0000000000i1"},
	}
	return context
}

// Every queryset the external API reads through joins the caller's membership of the project in the url, on its own base table's project column.
func TestTheExternalScopesJoinTheCallersMembership(t *testing.T) {
	handler := dryRunHandler(t)
	context := dryRunContext(t)
	user := &auth.User{ID: "00000000-0000-0000-0000-0000000000u1"}

	for _, test := range []struct {
		name  string
		scope *gorm.DB
		want  string
	}{
		{"stateScope", handler.stateScope(context, user),
			"JOIN project_members pm ON pm.project_id = s.project_id AND pm.member_id = ? AND pm.is_active = TRUE"},
		{"labelScope", handler.labelScope(context, user),
			"JOIN project_members pm ON pm.project_id = l.project_id AND pm.member_id = ? AND pm.is_active = TRUE"},
		{"issueLinkScope", handler.issueLinkScope(context, user),
			"JOIN project_members pm ON pm.project_id = l.project_id AND pm.member_id = ? AND pm.is_active = TRUE"},
		{"issueActivityScope", handler.issueActivityScope(context, user),
			"JOIN project_members pm ON pm.project_id = a.project_id AND pm.member_id = ? AND pm.is_active = TRUE"},
		{"issueCommentScope", handler.issueCommentScope(context, user),
			"JOIN project_members pm ON pm.project_id = cm.project_id AND pm.member_id = ? AND pm.is_active = TRUE"},
		{"cycleScope", handler.cycleScope(context, user),
			"JOIN project_members pm ON pm.project_id = c.project_id AND pm.member_id = ? AND pm.is_active = TRUE"},
	} {
		t.Run(test.name, func(t *testing.T) {
			rendered := renderedSQL(t, test.scope)
			if !strings.Contains(rendered, test.want) {
				t.Errorf("%s does not carry\n  %s\nit renders\n  %s", test.name, test.want, rendered)
			}
			if strings.Contains(rendered, "pm.deleted_at") {
				t.Errorf("%s checks the membership's soft delete, and Django's join does not", test.name)
			}
			if strings.Contains(rendered, "LEFT JOIN project_members") {
				t.Errorf("%s joins the membership outer, which lets a non-member's row through", test.name)
			}
			// Whatever a cascaded membership would have hidden, the base row's own soft delete hides first: the two are stamped by the same cascade.
			if !strings.Contains(rendered, "deleted_at IS NULL") {
				t.Errorf("%s filters no soft-deleted row at all:\n  %s", test.name, rendered)
			}
		})
	}
}
