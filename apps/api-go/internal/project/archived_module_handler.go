package project

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"github.com/yldm-tech/pace/apps/api-go/internal/drf"
)

func (handler *Handler) registerArchivedModuleRoutes(router gin.IRouter) {
	router.GET("/api/workspaces/:slug/projects/:id/archived-modules/", handler.authenticated(handler.archivedModuleList))
}

// archivedModuleList returns the modules of a project that have been archived.
//
// It is the live list's twin, annotated identically, but the two querysets are not built the same way. The live one goes through the viewset's base queryset, which narrows the modules to projects the caller is an active member of and drops archived projects. This one builds from the manager directly, so it does neither: an archived project's archived modules still appear, and membership is left entirely to the permission check. That is upstream's shape, reproduced rather than tidied.
func (handler *Handler) archivedModuleList(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	location, err := userLocation(user)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	rows, err := handler.archivedModuleRows(c, user.ID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	results := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		results = append(results, archivedModuleListJSON(row, location))
	}
	drf.Respond(c, http.StatusOK, results)
}

// archivedModuleRows reads the annotated archived modules. The annotations are the live list's, because the endpoint carries the same ones even though its projection drops the estimate sums.
func (handler *Handler) archivedModuleRows(c *gin.Context, userID string) ([]moduleRow, error) {
	var rows []moduleRow
	err := handler.db.WithContext(c.Request.Context()).Table("modules m").
		Select(moduleAnnotations(), userID, c.Param("id"), c.Param("slug")).
		Joins("JOIN workspaces w ON w.id = m.workspace_id").
		Where("w.slug = ? AND m.project_id = ? AND m.deleted_at IS NULL AND m.archived_at IS NOT NULL",
			c.Param("slug"), c.Param("id")).
		Group("m.id").Order("is_favorite DESC, m.created_at DESC").Scan(&rows).Error
	return rows, err
}

// archivedModuleListJSON is the values() projection: twenty-six fields.
//
// It is not the live list's. This one has no logo_props and neither estimate sum, and it carries archived_at — which is rendered in UTC, because only the two audit timestamps are handed to the timezone converter.
func archivedModuleListJSON(row moduleRow, location *time.Location) gin.H {
	return gin.H{
		"id": row.ID, "workspace_id": row.WorkspaceID, "project_id": row.ProjectID,
		"name": row.Name, "description": row.Description,
		"description_text": decodeJSON(row.DescriptionText), "description_html": decodeJSON(row.DescriptionHTML),
		"start_date": dateOnly(row.StartDate), "target_date": dateOnly(row.TargetDate),
		"status": row.Status, "lead_id": row.LeadID, "member_ids": stringsOrEmpty(row.MemberIDs),
		"view_props": decodeJSON(row.ViewProps), "sort_order": row.SortOrder,
		"external_source": row.ExternalSource, "external_id": row.ExternalID,
		"total_issues": row.TotalIssues, "is_favorite": row.IsFavorite,
		"cancelled_issues": row.CancelledIssues, "completed_issues": row.CompletedIssues,
		"started_issues": row.StartedIssues, "unstarted_issues": row.UnstartedIssues,
		"backlog_issues": row.BacklogIssues,
		"created_at":     row.CreatedAt.In(location), "updated_at": row.UpdatedAt.In(location),
		"archived_at": inUTC(row.ArchivedAt),
	}
}

// inUTC renders an archive time the way the untouched field is rendered: Django hands the raw value straight to the JSON encoder, which writes it in UTC because that is what the column holds.
func inUTC(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	utc := value.UTC()
	return &utc
}
