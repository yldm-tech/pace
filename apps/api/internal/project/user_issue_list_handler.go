package project

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api/internal/auth"
	"github.com/yldm-tech/pace/apps/api/internal/complexfilters"
	"github.com/yldm-tech/pace/apps/api/internal/drf"
	"github.com/yldm-tech/pace/apps/api/internal/pagination"
)

func (handler *Handler) registerUserIssueListRoutes(router gin.IRouter) {
	router.GET("/api/workspaces/:slug/user-issues/:user/", handler.authenticated(handler.userIssueList))
}

// userIssueList is one person's work across the workspace: everything assigned to them, raised by them, or that they are following.
//
// The three ways in are ORed against each other and then narrowed to projects the **caller** belongs to, so two people looking at the same person's list see different work items. It groups and sub-groups like the project list does, and takes both filter languages.
func (handler *Handler) userIssueList(c *gin.Context, user *auth.User) {
	if !handler.requireWorkspaceRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	slug, target := c.Param("slug"), c.Param("user")

	tree, refusal := complexfilters.Parse(c.Query("filters"))
	if refusal != nil {
		c.JSON(http.StatusBadRequest, refusal.Body())
		return
	}
	complex, err := complexfilters.SQL(tree, "i")
	if err != nil {
		handler.internalError(c, err)
		return
	}

	legacy := issueFilters(queryParams(c), "GET", "", handler.clock().UTC())
	joins, conditions, arguments, translatable := issueFilterSQL(legacy)
	if !translatable {
		handler.internalError(c, errors.New("user issue list: a filter names a field the schema does not have"))
		return
	}
	for _, join := range complex.Joins {
		joins = append(joins, join.Clause("i"))
	}
	if complex.Condition != "" {
		conditions = append(conditions, complex.Condition)
		arguments = append(arguments, complex.Arguments...)
	}

	// The three ways a work item belongs to this person. They are read as an id list rather than as a join, which is how Django writes it and why a work item assigned twice is still one row.
	conditions = append(conditions, userIssueMembershipPredicate)
	arguments = append(arguments, target, target, target, slug)
	// And the caller has to be in the project, whatever their role there.
	conditions = append(conditions, `EXISTS (SELECT 1 FROM project_members upm
		WHERE upm.project_id = i.project_id AND upm.member_id = ? AND upm.is_active = TRUE)`)
	arguments = append(arguments, user.ID)

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

	// No project id: this list spans the workspace, and the group value lists read the workspace's own rows rather than a project's.
	request := issueListRequest{
		slug: slug, perPage: perPage, cursor: cursor,
		joins: joins, conditions: conditions, arguments: arguments,
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

// userIssueMembershipPredicate is the three ways a work item counts as somebody's: they have it, they raised it, or they are following it.
//
// The inner query is the plain work item manager over the workspace rather than the outer one's, so a work item that is archived or still a draft can put an id into the list — and the outer manager then takes it back out. The net effect is the same, and the shape is Django's.
const userIssueMembershipPredicate = `i.id IN (
	SELECT ui.id FROM issues ui
	JOIN workspaces uw ON uw.id = ui.workspace_id
	LEFT JOIN issue_assignees uia ON uia.issue_id = ui.id
	LEFT JOIN issue_subscribers uis ON uis.issue_id = ui.id
	WHERE ui.deleted_at IS NULL AND (uia.assignee_id = ? OR ui.created_by_id = ? OR uis.subscriber_id = ?)
		AND uw.slug = ?)`
