package project

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
	"github.com/yldm-tech/pace/apps/api/internal/auth"
	"github.com/yldm-tech/pace/apps/api/internal/drf"
)

func (handler *Handler) registerModuleListRoutes(router gin.IRouter) {
	router.GET("/api/workspaces/:slug/projects/:id/modules/", handler.authenticated(handler.moduleList))
}

// moduleRow is the values() projection the list returns, with the counts and the estimate sums annotated on.
type moduleRow struct {
	Module
	MemberIDs               pq.StringArray `gorm:"column:member_ids;type:uuid[]"`
	IsFavorite              bool           `gorm:"column:is_favorite"`
	TotalIssues             int64          `gorm:"column:total_issues"`
	CancelledIssues         int64          `gorm:"column:cancelled_issues"`
	CompletedIssues         int64          `gorm:"column:completed_issues"`
	StartedIssues           int64          `gorm:"column:started_issues"`
	UnstartedIssues         int64          `gorm:"column:unstarted_issues"`
	BacklogIssues           int64          `gorm:"column:backlog_issues"`
	CompletedEstimatePoints float64        `gorm:"column:completed_estimate_points"`
	TotalEstimatePoints     float64        `gorm:"column:total_estimate_points"`
	// The five below are annotated only by the archived detail, which is the one projection that asks for them.
	SubIssues               *int64  `gorm:"column:sub_issues"`
	BacklogEstimatePoints   float64 `gorm:"column:backlog_estimate_points"`
	UnstartedEstimatePoints float64 `gorm:"column:unstarted_estimate_points"`
	StartedEstimatePoints   float64 `gorm:"column:started_estimate_points"`
	CancelledEstimatePoints float64 `gorm:"column:cancelled_estimate_points"`
}

// moduleList returns every module of a project that is not archived.
//
// Unlike the cycle list its timestamps are rendered in the **caller's** timezone rather than the project's, which is the asymmetry between the two apps worth knowing about.
func (handler *Handler) moduleList(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	location, err := userLocation(user)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	rows, err := handler.moduleRows(c, user.ID, "")
	if err != nil {
		handler.internalError(c, err)
		return
	}
	results := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		results = append(results, moduleListJSON(row, location))
	}
	drf.Respond(c, http.StatusOK, results)
}

// moduleRows reads the annotated modules. The caller must be an active member of the project and the project must not be archived.
func (handler *Handler) moduleRows(c *gin.Context, userID, moduleID string) ([]moduleRow, error) {
	query := handler.db.WithContext(c.Request.Context()).Table("modules m").
		Select(moduleAnnotations(), userID, c.Param("id"), c.Param("slug")).
		Joins("JOIN workspaces w ON w.id = m.workspace_id").
		Joins("JOIN projects p ON p.id = m.project_id AND p.archived_at IS NULL").
		Joins("JOIN project_members pm ON pm.project_id = m.project_id AND pm.member_id = ? AND pm.is_active = TRUE", userID).
		Where("w.slug = ? AND m.project_id = ? AND m.deleted_at IS NULL AND m.archived_at IS NULL",
			c.Param("slug"), c.Param("id"))
	if moduleID != "" {
		query = query.Where("m.id = ?", moduleID)
	}
	var rows []moduleRow
	err := query.Group("m.id").Order("is_favorite DESC, m.created_at DESC").Scan(&rows).Error
	return rows, err
}

// moduleAnnotations is the select the list builds. Every count and sum reads through the issue_objects manager, so an archived, draft or triage issue counts towards none of them.
func moduleAnnotations() string {
	// The link must be live as well as the issue, which is what keeps a removed issue out of a module's totals.
	moduleIssues := `FROM module_issues mi
		JOIN issues mis ON mis.id = mi.issue_id
		WHERE mi.module_id = m.id AND mi.deleted_at IS NULL AND ` + `(` + issueObjectsPredicate("mis") + `)`
	stateCount := func(group string) string {
		return `COALESCE((SELECT COUNT(*) ` + moduleIssues +
			` AND EXISTS (SELECT 1 FROM states ms WHERE ms.id = mis.state_id AND ms.group = '` + group + `')), 0)`
	}
	return `m.*,
		EXISTS (SELECT 1 FROM user_favorites uf
			JOIN workspaces uw ON uw.id = uf.workspace_id
			WHERE uf.user_id = ? AND uf.entity_identifier = m.id AND uf.entity_type = 'module'
			AND uf.project_id = ? AND uw.slug = ? AND uf.deleted_at IS NULL) AS is_favorite,
		COALESCE((SELECT COUNT(*) ` + moduleIssues + `), 0) AS total_issues,
		` + stateCount("cancelled") + ` AS cancelled_issues,
		` + stateCount("completed") + ` AS completed_issues,
		` + stateCount("started") + ` AS started_issues,
		` + stateCount("unstarted") + ` AS unstarted_issues,
		` + stateCount("backlog") + ` AS backlog_issues,
		` + estimateSum("") + ` AS total_estimate_points,
		` + estimateSum(` AND EXISTS (SELECT 1 FROM states ms2 WHERE ms2.id = mis.state_id AND ms2.group = 'completed')`) + ` AS completed_estimate_points,
		COALESCE((SELECT ARRAY_AGG(DISTINCT mm.member_id) FROM module_members mm
			WHERE mm.module_id = m.id AND mm.deleted_at IS NULL), '{}') AS member_ids`
}

// estimateSum adds up the points of a module's issues. Only an estimate of the points type has a number to add, so a category estimate contributes nothing.
func estimateSum(extra string) string {
	return `COALESCE((SELECT SUM(CAST(ep.value AS DOUBLE PRECISION))
		FROM module_issues mi
		JOIN issues mis ON mis.id = mi.issue_id
		JOIN estimate_points ep ON ep.id = mis.estimate_point_id
		JOIN estimates es ON es.id = ep.estimate_id AND es.type = 'points'
		WHERE mi.module_id = m.id AND mi.deleted_at IS NULL AND (` + issueObjectsPredicate("mis") + `)` + extra + `), 0)`
}

// moduleListJSON is the values() projection: twenty-eight fields, with the two audit timestamps in the caller's timezone.
func moduleListJSON(row moduleRow, location *time.Location) gin.H {
	return gin.H{
		"id": row.ID, "workspace_id": row.WorkspaceID, "project_id": row.ProjectID,
		"name": row.Name, "description": row.Description,
		"description_text": decodeJSON(row.DescriptionText), "description_html": decodeJSON(row.DescriptionHTML),
		"start_date": dateOnly(row.StartDate), "target_date": dateOnly(row.TargetDate),
		"status": row.Status, "lead_id": row.LeadID, "member_ids": stringsOrEmpty(row.MemberIDs),
		"view_props": decodeJSON(row.ViewProps), "sort_order": row.SortOrder,
		"external_source": row.ExternalSource, "external_id": row.ExternalID,
		"logo_props":                decodeJSON(row.LogoProps),
		"completed_estimate_points": row.CompletedEstimatePoints,
		"total_estimate_points":     row.TotalEstimatePoints,
		"total_issues":              row.TotalIssues, "is_favorite": row.IsFavorite,
		"cancelled_issues": row.CancelledIssues, "completed_issues": row.CompletedIssues,
		"started_issues": row.StartedIssues, "unstarted_issues": row.UnstartedIssues,
		"backlog_issues": row.BacklogIssues,
		"created_at":     row.CreatedAt.In(location), "updated_at": row.UpdatedAt.In(location),
	}
}
