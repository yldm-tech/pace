package project

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"github.com/yldm-tech/pace/apps/api-go/internal/drf"
	"github.com/yldm-tech/pace/apps/api-go/internal/pagination"
)

func (handler *Handler) registerArchivedIssueListRoutes(router gin.IRouter) {
	router.GET("/api/workspaces/:slug/projects/:id/archived-issues/", handler.authenticated(handler.archivedIssueList))
}

// archivedIssuePredicate is the archive queryset's own manager: the plain soft-delete one, narrowed to rows that are archived and are not epics. issue_objects could not be used here, since it hides archived rows by definition.
const archivedIssuePredicate = `i.deleted_at IS NULL AND i.archived_at IS NOT NULL
	AND (i.type_id IS NULL OR NOT EXISTS (SELECT 1 FROM issue_types t WHERE t.id = i.type_id AND t.is_epic))`

// archivedIssueList is the archive's own list. It shares the live list's filtering, ordering, grouping and paging, and differs only in which rows it reads and in carrying no guest narrowing and no recorded visit.
func (handler *Handler) archivedIssueList(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember) {
		return
	}
	slug, projectID := c.Param("slug"), c.Param("id")

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
		handler.internalError(c, errors.New("archived issues: a filter names a field the schema does not have"))
		return
	}
	// show_sub_issues defaults to true, so the sub-issues are hidden only when it is asked for explicitly.
	if c.DefaultQuery("show_sub_issues", "true") != "true" {
		conditions = append(conditions, "i.parent_id IS NULL")
	}

	request := issueListRequest{
		slug: slug, projectID: projectID, perPage: perPage, cursor: cursor,
		joins: joins, conditions: conditions, arguments: arguments,
		basePredicate: archivedIssuePredicate,
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
