package project

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"github.com/yldm-tech/pace/apps/api-go/internal/drf"
	"github.com/yldm-tech/pace/apps/api-go/internal/pagination"
)

func (handler *Handler) registerCycleIssueListRoutes(router gin.IRouter) {
	router.GET("/api/workspaces/:slug/projects/:id/cycles/:cycle/cycle-issues/", handler.authenticated(handler.cycleIssueList))
}

// cycleIssueListPredicate is the issue_objects manager narrowed to one cycle. Both halves of the link condition sit inside one EXISTS, because Django puts them in a single filter call and so applies them to the same joined row.
func cycleIssueListPredicate() string {
	return issueObjectsPredicate("i") +
		" AND EXISTS (SELECT 1 FROM cycle_issues cil WHERE cil.issue_id = i.id AND cil.cycle_id = ? AND cil.deleted_at IS NULL)"
}

// cycleIssueList is the issue list of one cycle. It shares the project list's filtering, ordering, grouping and paging, and carries neither the guest narrowing nor the recorded visit that one does.
func (handler *Handler) cycleIssueList(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	slug, projectID, cycleID := c.Param("slug"), c.Param("id"), c.Param("cycle")

	perPage, err := pagination.PerPage(c.Query("per_page"), pagination.DefaultPerPage, pagination.DefaultPerPage)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	cursor := pagination.OffsetCursor{Value: perPage}
	if raw := c.Query("cursor"); raw != "" {
		parsed, err := pagination.ParseOffsetCursor(raw)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"detail": "Invalid cursor parameter."})
			return
		}
		cursor = parsed
	}

	filters := issueFilters(queryParams(c), "GET", "", handler.clock().UTC())
	joins, conditions, arguments, translatable := issueFilterSQL(filters)
	if !translatable {
		handler.internalError(c, errors.New("cycle issues: a filter names a field the schema does not have"))
		return
	}

	request := issueListRequest{
		slug: slug, projectID: projectID, perPage: perPage, cursor: cursor,
		joins: joins, conditions: conditions, arguments: arguments,
		basePredicate: cycleIssueListPredicate(), baseArguments: []any{cycleID},
	}
	var total int64
	if err := handler.issueListScope(c.Request.Context(), request).Distinct("i.id").Count(&total).Error; err != nil {
		handler.internalError(c, err)
		return
	}
	request.total = int(total)

	if c.Query("group_by") != "" {
		handler.issueListGrouped(c, user, request)
		return
	}

	rows, err := handler.issueListPage(c, request)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	page := pagination.PlanOffsetPage(perPage, cursor, request.total, len(rows), pagination.DefaultPerPage)
	if len(rows) > page.Limit {
		rows = rows[:page.Limit]
	}
	results := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		results = append(results, issueListRowJSON(row))
	}
	drf.Respond(c, http.StatusOK, page.Envelope(results, len(results), nil, nil, nil))
}
