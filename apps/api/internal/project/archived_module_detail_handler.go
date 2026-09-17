package project

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api/internal/auth"
	"github.com/yldm-tech/pace/apps/api/internal/drf"
	"gorm.io/gorm"
)

func (handler *Handler) registerArchivedModuleDetailRoutes(router gin.IRouter) {
	router.GET("/api/workspaces/:slug/projects/:id/archived-modules/:module/", handler.authenticated(handler.archivedModuleRetrieve))
}

// archivedModuleRetrieve is one archived module with both of its distributions.
//
// Like its cycle twin, a module that is not archived or not there at all is a **500** rather than a 404: the queryset filters `archived_at__isnull=False` before the id is applied, and the view then subscripts the nothing that comes back. Reproduced.
//
// Where the two differ is the burndown. The cycle's is drawn from the cycle's own dates; the module's is drawn from the module's, and the module detail asks the *serializer's* dates rather than the row's — which are already strings by then, so an empty one is falsy and an unset date skips the chart exactly as it does on the cycle.
func (handler *Handler) archivedModuleRetrieve(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	location, err := userLocation(user)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	slug, projectID, moduleID := c.Param("slug"), c.Param("id"), c.Param("module")

	rows, err := handler.archivedModuleRowByID(c, user.ID, slug, projectID, moduleID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if len(rows) == 0 {
		handler.internalError(c, errors.New("archived module: the module is not archived or does not exist"))
		return
	}
	row := rows[0]

	links, err := handler.moduleLinks(c, moduleID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	data := archivedModuleDetailJSON(row, links, location)

	points, err := handler.projectEstimatesPoints(c, slug, projectID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	estimateDistribution := gin.H{}
	if points {
		assignees, labels, err := handler.moduleEstimateDistributions(c, slug, projectID, moduleID)
		if err != nil {
			handler.internalError(c, err)
			return
		}
		chart := gin.H{}
		if row.StartDate != nil && row.TargetDate != nil {
			chart, err = handler.moduleBurndown(c, slug, projectID, moduleID, row, true)
			if err != nil {
				handler.internalError(c, err)
				return
			}
		}
		estimateDistribution = gin.H{"assignees": assignees, "labels": labels, "completion_chart": chart}
	}
	data["estimate_distribution"] = estimateDistribution

	assignees, labels, err := handler.moduleIssueDistributions(c, slug, projectID, moduleID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	chart := gin.H{}
	if row.StartDate != nil && row.TargetDate != nil {
		chart, err = handler.moduleBurndown(c, slug, projectID, moduleID, row, false)
		if err != nil {
			handler.internalError(c, err)
			return
		}
	}
	data["distribution"] = gin.H{"assignees": assignees, "labels": labels, "completion_chart": chart}

	drf.Respond(c, http.StatusOK, data)
}

// archivedModuleRowByID reads the one module with the four extra point sums the detail projection names on top of the list's.
func (handler *Handler) archivedModuleRowByID(c *gin.Context, userID, slug, projectID, moduleID string) ([]moduleRow, error) {
	pointSum := func(alias, group string) string {
		condition := ""
		if group != "" {
			condition = ` JOIN states ` + alias + `st ON ` + alias + `st.id = ` + alias + `i.state_id AND ` + alias + `st.group = '` + group + `'`
		}
		return `COALESCE((SELECT SUM(CAST(` + alias + `p.value AS DOUBLE PRECISION)) FROM module_issues ` + alias + `mi
			JOIN issues ` + alias + `i ON ` + alias + `i.id = ` + alias + `mi.issue_id
			JOIN estimate_points ` + alias + `p ON ` + alias + `p.id = ` + alias + `i.estimate_point_id
			JOIN estimates ` + alias + `e ON ` + alias + `e.id = ` + alias + `p.estimate_id AND ` + alias + `e.type = 'points'` + condition + `
			WHERE ` + alias + `mi.module_id = m.id AND ` + alias + `mi.deleted_at IS NULL
			AND ` + issueObjectsPredicate(alias+"i") + `), 0)`
	}
	selection := moduleAnnotations() + `,
		(SELECT COUNT(*) FROM issues si
			JOIN module_issues smi ON smi.issue_id = si.id AND smi.module_id = m.id AND smi.deleted_at IS NULL
			WHERE si.project_id = ? AND si.parent_id IS NOT NULL AND ` + issueObjectsPredicate("si") + `) AS sub_issues,
		` + pointSum("b", "backlog") + ` AS backlog_estimate_points,
		` + pointSum("u", "unstarted") + ` AS unstarted_estimate_points,
		` + pointSum("s", "started") + ` AS started_estimate_points,
		` + pointSum("x", "cancelled") + ` AS cancelled_estimate_points`

	var rows []moduleRow
	err := handler.db.WithContext(c.Request.Context()).Table("modules m").
		Select(selection, userID, projectID, slug, projectID).
		Joins("JOIN workspaces w ON w.id = m.workspace_id").
		Where("w.slug = ? AND m.project_id = ? AND m.id = ? AND m.deleted_at IS NULL AND m.archived_at IS NOT NULL",
			slug, projectID, moduleID).
		Group("m.id").Limit(1).Scan(&rows).Error
	return rows, err
}

// moduleLinks reads the links nested inside the detail. They are ordered newest first, which is the model's own ordering.
func (handler *Handler) moduleLinks(c *gin.Context, moduleID string) ([]ModuleLink, error) {
	var links []ModuleLink
	err := handler.db.WithContext(c.Request.Context()).Table("module_links").
		Where("module_id = ? AND deleted_at IS NULL", moduleID).
		Order("created_at DESC").Scan(&links).Error
	return links, err
}

// archivedModuleDetailJSON is ModuleDetailSerializer: the module serializer's twenty-nine fields plus the six the detail adds.
func archivedModuleDetailJSON(row moduleRow, links []ModuleLink, location *time.Location) gin.H {
	data := moduleListJSON(row, location)
	data["archived_at"] = row.ArchivedAt
	serialized := make([]gin.H, 0, len(links))
	for _, link := range links {
		serialized = append(serialized, moduleLinkJSON(link))
	}
	data["link_module"] = serialized
	data["sub_issues"] = row.SubIssues
	data["backlog_estimate_points"] = drf.Float(row.BacklogEstimatePoints)
	data["unstarted_estimate_points"] = drf.Float(row.UnstartedEstimatePoints)
	data["started_estimate_points"] = drf.Float(row.StartedEstimatePoints)
	data["cancelled_estimate_points"] = drf.Float(row.CancelledEstimatePoints)
	return data
}

// moduleBurndown is the cycle's chart drawn over a module's own dates and its own join.
func (handler *Handler) moduleBurndown(c *gin.Context, slug, projectID, moduleID string, row moduleRow, points bool) (gin.H, error) {
	input := burndownInput{Start: row.StartDate, End: row.TargetDate, Points: points, Total: float64(row.TotalIssues)}

	scope := func() *gorm.DB {
		return handler.db.WithContext(c.Request.Context()).Table("issues i").
			Joins("JOIN workspaces w ON w.id = i.workspace_id").
			Joins("JOIN module_issues mi ON mi.issue_id = i.id AND mi.module_id = ? AND mi.deleted_at IS NULL", moduleID).
			Where("w.slug = ? AND i.project_id = ?", slug, projectID).
			Where(issueObjectsPredicate("i"))
	}

	if !points {
		var rows []struct {
			Date  *time.Time `gorm:"column:date"`
			Total int64      `gorm:"column:total_completed"`
		}
		err := scope().Select(completedDay + " AS date, COUNT(i.id) AS total_completed").
			Group(completedDay).Order(completedDay).Scan(&rows).Error
		if err != nil {
			return nil, err
		}
		for _, completion := range rows {
			input.Completions = append(input.Completions, burndownCompletion{Date: completion.Date, Value: float64(completion.Total)})
		}
		return burndownChart(input, handler.clock().UTC()), nil
	}

	var rows []struct {
		Date  *time.Time `gorm:"column:date"`
		Value float64    `gorm:"column:value"`
	}
	err := scope().Joins("JOIN estimate_points ep ON ep.id = i.estimate_point_id").
		Select(completedDay + " AS date, CAST(ep.value AS DOUBLE PRECISION) AS value").
		Order(completedDay).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	var total float64
	for _, completion := range rows {
		total += completion.Value
		input.Completions = append(input.Completions, burndownCompletion{Date: completion.Date, Value: completion.Value})
	}
	input.Total = total
	input.TotalIsFloat = len(rows) > 0
	return burndownChart(input, handler.clock().UTC()), nil
}

// moduleAssigneeScope and moduleLabelScope are the cycle's two scopes with the module's own join in place of the cycle's.
func (handler *Handler) moduleAssigneeScope(c *gin.Context, slug, projectID, moduleID string) *gorm.DB {
	return handler.db.WithContext(c.Request.Context()).Table("issues i").
		Joins("JOIN workspaces w ON w.id = i.workspace_id").
		Joins("JOIN module_issues mi ON mi.issue_id = i.id AND mi.module_id = ? AND mi.deleted_at IS NULL", moduleID).
		Joins("LEFT JOIN issue_assignees ia ON ia.issue_id = i.id").
		Joins("LEFT JOIN users u ON u.id = ia.assignee_id").
		Where("w.slug = ? AND i.project_id = ?", slug, projectID).
		Where(issueObjectsPredicate("i")).
		Group("u.first_name, u.last_name, ia.assignee_id, u.avatar_asset_id, u.avatar, u.display_name")
}

func (handler *Handler) moduleLabelScope(c *gin.Context, slug, projectID, moduleID string) *gorm.DB {
	return handler.db.WithContext(c.Request.Context()).Table("issues i").
		Joins("JOIN workspaces w ON w.id = i.workspace_id").
		Joins("JOIN module_issues mi ON mi.issue_id = i.id AND mi.module_id = ? AND mi.deleted_at IS NULL", moduleID).
		Joins("LEFT JOIN issue_labels il ON il.issue_id = i.id").
		Joins("LEFT JOIN labels l ON l.id = il.label_id").
		Where("w.slug = ? AND i.project_id = ?", slug, projectID).
		Where(issueObjectsPredicate("i")).
		Group("l.name, l.color, il.label_id")
}

// moduleIssueDistributions counts work items per assignee and per label.
//
// The assignee rows carry a first and a last name where the cycle's carry a display name, and they are ordered by the first name — so somebody with no first name sorts to the top rather than under their display name.
func (handler *Handler) moduleIssueDistributions(c *gin.Context, slug, projectID, moduleID string) ([]gin.H, []gin.H, error) {
	counts := func(column string) string {
		return `COUNT(` + column + `) FILTER (WHERE ` + liveIssue + `) AS total_issues,
			COUNT(` + column + `) FILTER (WHERE ` + completedIssues + `) AS completed_issues,
			COUNT(` + column + `) FILTER (WHERE ` + pendingIssues + `) AS pending_issues`
	}
	var assigneeRows []distributionRow
	err := handler.moduleAssigneeScope(c, slug, projectID, moduleID).
		Select("u.first_name, u.last_name, ia.assignee_id, " + avatarURL + ", u.display_name, " + counts("ia.assignee_id")).
		Order("u.first_name").Scan(&assigneeRows).Error
	if err != nil {
		return nil, nil, err
	}
	var labelRows []distributionRow
	err = handler.moduleLabelScope(c, slug, projectID, moduleID).
		Select("l.name AS label_name, l.color, il.label_id, " + counts("il.label_id")).
		Order("l.name").Scan(&labelRows).Error
	if err != nil {
		return nil, nil, err
	}

	assignees := make([]gin.H, 0, len(assigneeRows))
	for _, row := range assigneeRows {
		assignees = append(assignees, gin.H{
			"first_name": row.FirstName, "last_name": row.LastName,
			"assignee_id": row.AssigneeID, "avatar_url": row.AvatarURL, "display_name": row.DisplayName,
			"total_issues": row.TotalIssues, "completed_issues": row.DoneIssues, "pending_issues": row.LeftIssues,
		})
	}
	labels := make([]gin.H, 0, len(labelRows))
	for _, row := range labelRows {
		labels = append(labels, gin.H{
			"label_name": row.LabelName, "color": row.Color, "label_id": row.LabelID,
			"total_issues": row.TotalIssues, "completed_issues": row.DoneIssues, "pending_issues": row.LeftIssues,
		})
	}
	return assignees, labels, nil
}

// moduleEstimateDistributions is the same two groupings, summing points instead of counting work items.
func (handler *Handler) moduleEstimateDistributions(c *gin.Context, slug, projectID, moduleID string) ([]gin.H, []gin.H, error) {
	const sums = `SUM(CAST(ep.value AS DOUBLE PRECISION)) AS total_estimates,
		SUM(CAST(ep.value AS DOUBLE PRECISION)) FILTER (WHERE ` + completedIssues + `) AS completed_estimates,
		SUM(CAST(ep.value AS DOUBLE PRECISION)) FILTER (WHERE ` + pendingIssues + `) AS pending_estimates`

	var assigneeRows []distributionRow
	err := handler.moduleAssigneeScope(c, slug, projectID, moduleID).
		Joins("LEFT JOIN estimate_points ep ON ep.id = i.estimate_point_id").
		Select("u.first_name, u.last_name, ia.assignee_id, " + avatarURL + ", u.display_name, " + sums).
		Order("u.first_name").Scan(&assigneeRows).Error
	if err != nil {
		return nil, nil, err
	}
	var labelRows []distributionRow
	err = handler.moduleLabelScope(c, slug, projectID, moduleID).
		Joins("LEFT JOIN estimate_points ep ON ep.id = i.estimate_point_id").
		Select("l.name AS label_name, l.color, il.label_id, " + sums).
		Order("l.name").Scan(&labelRows).Error
	if err != nil {
		return nil, nil, err
	}

	assignees := make([]gin.H, 0, len(assigneeRows))
	for _, row := range assigneeRows {
		assignees = append(assignees, gin.H{
			"first_name": row.FirstName, "last_name": row.LastName,
			"assignee_id": row.AssigneeID, "avatar_url": row.AvatarURL, "display_name": row.DisplayName,
			"total_estimates": row.TotalPoints, "completed_estimates": row.DonePoints, "pending_estimates": row.LeftPoints,
		})
	}
	labels := make([]gin.H, 0, len(labelRows))
	for _, row := range labelRows {
		labels = append(labels, gin.H{
			"label_name": row.LabelName, "color": row.Color, "label_id": row.LabelID,
			"total_estimates": row.TotalPoints, "completed_estimates": row.DonePoints, "pending_estimates": row.LeftPoints,
		})
	}
	return assignees, labels, nil
}
