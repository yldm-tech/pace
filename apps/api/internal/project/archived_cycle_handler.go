package project

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api/internal/access"
	"github.com/yldm-tech/pace/apps/api/internal/auth"
	"github.com/yldm-tech/pace/apps/api/internal/drf"
)

func (handler *Handler) registerArchivedCycleRoutes(router gin.IRouter) {
	router.GET("/api/workspaces/:slug/projects/:id/archived-cycles/", handler.authenticated(handler.archivedCycleList))
}

// archivedCycleList returns the cycles of a project that have been archived.
//
// It differs from its module counterpart in both of the ways the two apps tend to differ. A guest may read archived modules and may not read archived cycles, because this endpoint is decorated for admins and members while the module one leans on the permission class, which lets any member through a safe method. And this queryset keeps the membership and project-archived filters the live cycle list has, where the module one drops them.
func (handler *Handler) archivedCycleList(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember) {
		return
	}
	slug, projectID := c.Param("slug"), c.Param("id")
	now := handler.clock().UTC()

	var rows []cycleRow
	err := handler.db.WithContext(c.Request.Context()).Table("cycles c").
		Select(archivedCycleAnnotations(), user.ID, projectID, slug, now, now, now, now).
		Joins("JOIN workspaces w ON w.id = c.workspace_id").
		Joins("JOIN projects p ON p.id = c.project_id AND p.archived_at IS NULL").
		Joins(access.MemberJoin("c", "project_id"), user.ID).
		Where("w.slug = ? AND c.project_id = ? AND c.deleted_at IS NULL AND c.archived_at IS NOT NULL", slug, projectID).
		Group("c.id").Order("is_favorite DESC, c.created_at DESC").Scan(&rows).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	results := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		results = append(results, archivedCycleListJSON(row))
	}
	drf.Respond(c, http.StatusOK, results)
}

// archivedCycleAnnotations is the live list's select with the three state counts the archived projection asks for. They carry the same four exclusions as their neighbours.
func archivedCycleAnnotations() string {
	const liveIssues = `ci.deleted_at IS NULL AND ii.deleted_at IS NULL
		AND ii.archived_at IS NULL AND ii.is_draft = FALSE`
	stateCount := func(group string) string {
		return `(SELECT COUNT(DISTINCT ii.id) FROM cycle_issues ci
			JOIN issues ii ON ii.id = ci.issue_id
			JOIN states st ON st.id = ii.state_id AND st.group = '` + group + `'
			WHERE ci.cycle_id = c.id AND ` + liveIssues + `)`
	}
	return cycleAnnotations() + `,
		` + stateCount("started") + ` AS started_issues,
		` + stateCount("unstarted") + ` AS unstarted_issues,
		` + stateCount("backlog") + ` AS backlog_issues`
}

// archivedCycleListJSON is the values() projection: twenty-three fields where the live list has twenty-two, and not a superset of them.
//
// This one has no logo_props, version or created_by, and it adds the three state counts and archived_at. Nothing here moves to another timezone either: the live list renders its two dates in the project's zone, while this endpoint hands the raw values to the JSON encoder, so they come back in UTC.
func archivedCycleListJSON(row cycleRow) gin.H {
	return gin.H{
		"id": row.ID, "workspace_id": row.WorkspaceID, "project_id": row.ProjectID,
		"name": row.Name, "description": row.Description,
		"start_date": inUTC(row.StartDate), "end_date": inUTC(row.EndDate),
		"owned_by_id": row.OwnedByID, "view_props": decodeJSON(row.ViewProps),
		"sort_order": row.SortOrder, "external_source": row.ExternalSource, "external_id": row.ExternalID,
		"progress_snapshot": decodeJSON(row.ProgressSnapshot),
		"total_issues":      row.TotalIssues, "is_favorite": row.IsFavorite,
		"cancelled_issues": row.CancelledIssues, "completed_issues": row.CompletedIssues,
		"started_issues": row.StartedIssues, "unstarted_issues": row.UnstartedIssues,
		"backlog_issues": row.BacklogIssues,
		"assignee_ids":   stringsOrEmpty(row.AssigneeIDs), "status": row.Status,
		"archived_at": inUTC(row.ArchivedAt),
	}
}
