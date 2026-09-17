package project

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api/internal/auth"
	"github.com/yldm-tech/pace/apps/api/internal/complexfilters"
	"github.com/yldm-tech/pace/apps/api/internal/drf"
	"github.com/yldm-tech/pace/apps/api/internal/pagination"
	"gorm.io/gorm"
)

func (handler *Handler) registerWorkspaceIssueListRoutes(router gin.IRouter) {
	router.GET("/api/workspaces/:slug/issues/", handler.authenticated(handler.workspaceIssueList))
}

// workspaceIssueList is every work item in the workspace the caller can see, across all their projects.
//
// It takes two filter languages at once. `?filters=` is the JSON tree internal/complexfilters reads; the rest of the query string is the older flat one every other list takes. Both are applied, and they are ANDed together.
func (handler *Handler) workspaceIssueList(c *gin.Context, user *auth.User) {
	if !handler.requireWorkspaceRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	slug := c.Param("slug")

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
		handler.internalError(c, errors.New("workspace issue list: a filter names a field the schema does not have"))
		return
	}

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

	scope := func() *gorm.DB {
		query := handler.db.WithContext(c.Request.Context()).Table("issues i").
			Joins("JOIN workspaces w ON w.id = i.workspace_id").
			Where("w.slug = ?", slug).
			Where(issueObjectsPredicate("i"))
		for _, join := range complex.Joins {
			query = query.Joins(join.Clause("i"))
		}
		if complex.Condition != "" {
			query = query.Where(complex.Condition, complex.Arguments...)
		}
		for _, join := range joins {
			query = query.Joins(join)
		}
		consumed := 0
		for _, condition := range conditions {
			count := countPlaceholders(condition)
			query = query.Where(condition, arguments[consumed:consumed+count]...)
			consumed += count
		}
		// A guest sees everything when the project opens all features to guests, and otherwise only what they raised; anybody above guest sees it all.
		return query.Where(issueVisibilityPredicate, user.ID, user.ID, user.ID, user.ID)
	}

	// The count is over distinct work items, since both filter languages can join a relation that multiplies rows.
	var total int64
	if err := scope().Distinct("i.id").Count(&total).Error; err != nil {
		handler.internalError(c, err)
		return
	}
	window := pagination.PlanOffsetPage(perPage, cursor, int(total), 0, pagination.DefaultPerPage)

	var rows []issueListRow
	err = scope().Select(workspaceIssueAnnotations()).Group("i.id").
		Order(issueOrderClause(c.Query("order_by"))).
		Offset(window.Offset).Limit(window.Stop - window.Offset).Scan(&rows).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	page := pagination.PlanOffsetPage(perPage, cursor, int(total), len(rows), pagination.DefaultPerPage)
	if len(rows) > page.Limit {
		rows = rows[:page.Limit]
	}
	results := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		results = append(results, viewIssueListJSON(row))
	}
	drf.Respond(c, http.StatusOK, page.Envelope(results, len(results), nil, nil, nil))
}

// workspaceIssueAnnotations is this list's own set, which differs from the project list's in one way: the module ids come from the prefetched links rather than an aggregate, so a module that has since been archived is still reported.
func workspaceIssueAnnotations() string {
	return `i.*,
		(SELECT ci.cycle_id FROM cycle_issues ci WHERE ci.issue_id = i.id AND ci.deleted_at IS NULL LIMIT 1) AS cycle_id,
		(SELECT COUNT(*) FROM issue_links il WHERE il.issue_id = i.id AND il.deleted_at IS NULL) AS link_count,
		(SELECT COUNT(*) FROM file_assets fa WHERE fa.issue_id = i.id AND fa.entity_type = 'ISSUE_ATTACHMENT' AND fa.deleted_at IS NULL) AS attachment_count,
		(SELECT COUNT(*) FROM issues sub WHERE sub.parent_id = i.id AND ` + issueObjectsPredicate("sub") + `) AS sub_issues_count,
		(SELECT s.group FROM states s WHERE s.id = i.state_id) AS state_group,
		COALESCE((SELECT ARRAY_AGG(DISTINCT il2.label_id) FROM issue_labels il2 WHERE il2.issue_id = i.id AND il2.deleted_at IS NULL), '{}') AS label_ids,
		COALESCE((SELECT ARRAY_AGG(DISTINCT ia.assignee_id) FROM issue_assignees ia WHERE ia.issue_id = i.id AND ia.deleted_at IS NULL), '{}') AS assignee_ids,
		COALESCE((SELECT ARRAY_AGG(DISTINCT mi.module_id) FROM module_issues mi WHERE mi.issue_id = i.id AND mi.deleted_at IS NULL), '{}') AS module_ids`
}

// viewIssueListJSON is ViewIssueListSerializer: twenty-six fields, with the three counts left as the raw annotations rather than coalesced to zero.
func viewIssueListJSON(row issueListRow) gin.H {
	return gin.H{
		"id": row.ID, "name": row.Name, "state_id": row.StateID, "sort_order": row.SortOrder,
		"completed_at": row.CompletedAt, "estimate_point": row.EstimatePointID,
		"priority": row.Priority, "start_date": dateOnly(row.StartDate), "target_date": dateOnly(row.TargetDate),
		"sequence_id": row.SequenceID, "project_id": row.ProjectID, "parent_id": row.ParentID,
		"cycle_id": row.CycleID, "sub_issues_count": row.SubIssuesCount,
		"created_at": row.CreatedAt, "updated_at": row.UpdatedAt,
		"created_by": row.CreatedByID, "updated_by": row.UpdatedByID,
		"attachment_count": row.AttachmentCount, "link_count": row.LinkCount,
		"is_draft": row.IsDraft, "archived_at": dateOnly(row.ArchivedAt),
		"state__group": row.StateGroup,
		"assignee_ids": stringsOrEmpty(row.AssigneeIDs),
		"label_ids":    stringsOrEmpty(row.LabelIDs),
		"module_ids":   stringsOrEmpty(row.ModuleIDs),
	}
}
