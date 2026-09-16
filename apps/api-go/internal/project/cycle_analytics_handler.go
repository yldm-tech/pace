package project

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"github.com/yldm-tech/pace/apps/api-go/internal/drf"
	"gorm.io/gorm"
)

func (handler *Handler) registerCycleAnalyticsRoutes(router gin.IRouter) {
	router.GET("/api/workspaces/:slug/projects/:id/cycles/:cycle/analytics/", handler.authenticated(handler.cycleAnalytics))
}

// cycleAnalytics is the cycle board's chart: who and what the work is spread across, and a day-by-day burndown.
//
// The `type` parameter chooses between counting issues and summing points, and a points request against a project with no points estimate is not an error — it returns two empty lists and an empty chart.
func (handler *Handler) cycleAnalytics(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	slug, projectID, cycleID := c.Param("slug"), c.Param("id"), c.Param("cycle")
	analyticType := c.DefaultQuery("type", "issues")

	cycle, total, err := handler.cycleWithBurndownTotal(c, slug, projectID, cycleID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// Django reads the start date off the result of .first() with no guard, so a cycle that is not there raises rather than answering 404.
		handler.internalError(c, errors.New("cycle analytics: the cycle does not exist"))
		return
	}
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if cycle.StartDate == nil || cycle.EndDate == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Cycle has no start or end date"})
		return
	}

	// A cycle whose issues were transferred away carries the distribution it had at that moment, and the live queries are never run.
	if snapshot, ok := drf.DecodeJSON([]byte(cycle.ProgressSnapshot)).(map[string]any); ok && len(snapshot) > 0 {
		distribution, _ := snapshot["distribution"].(map[string]any)
		drf.Respond(c, http.StatusOK, gin.H{
			"labels":           snapshotList(distribution, "labels"),
			"assignees":        snapshotList(distribution, "assignees"),
			"completion_chart": snapshotObject(distribution, "completion_chart"),
		})
		return
	}

	points, err := handler.projectEstimatesPoints(c, slug, projectID)
	if err != nil {
		handler.internalError(c, err)
		return
	}

	assignees := []gin.H{}
	labels := []gin.H{}
	chart := gin.H{}

	switch {
	case analyticType == "points" && points:
		assignees, labels, err = handler.cycleEstimateDistributions(c, slug, projectID, cycleID)
		if err == nil {
			chart, err = handler.cycleBurndown(c, slug, projectID, cycleID, cycle, total, true)
		}
	case analyticType == "issues":
		assignees, labels, err = handler.cycleIssueDistributions(c, slug, projectID, cycleID)
		if err == nil {
			chart, err = handler.cycleBurndown(c, slug, projectID, cycleID, cycle, total, false)
		}
	}
	if err != nil {
		handler.internalError(c, err)
		return
	}

	drf.Respond(c, http.StatusOK, gin.H{"assignees": assignees, "labels": labels, "completion_chart": chart})
}

// cycleWithBurndownTotal reads the cycle and the issue count the burndown counts down from.
//
// That count is not the issue_objects manager's. It excludes deleted, archived and draft issues and dead links, and it does **not** exclude a triage issue or one in an archived project, because it is written as a filtered Count on the cycle queryset rather than through the manager. The distributions drawn next to it do go through the manager, so the two can disagree about the same cycle.
func (handler *Handler) cycleWithBurndownTotal(c *gin.Context, slug, projectID, cycleID string) (Cycle, int64, error) {
	var row struct {
		Cycle
		TotalIssues int64 `gorm:"column:total_issues"`
	}
	err := handler.db.WithContext(c.Request.Context()).Table("cycles c").
		Select(`c.*, (SELECT COUNT(DISTINCT bi.id) FROM cycle_issues bci
			JOIN issues bi ON bi.id = bci.issue_id
			WHERE bci.cycle_id = c.id AND bci.deleted_at IS NULL
			AND bi.deleted_at IS NULL AND bi.archived_at IS NULL AND bi.is_draft = FALSE) AS total_issues`).
		Joins("JOIN workspaces w ON w.id = c.workspace_id").
		Where("w.slug = ? AND c.project_id = ? AND c.id = ? AND c.deleted_at IS NULL", slug, projectID, cycleID).
		Take(&row).Error
	return row.Cycle, row.TotalIssues, err
}

// projectEstimatesPoints reports whether the project's estimate counts in points. A project with none, or with a category estimate, answers no.
func (handler *Handler) projectEstimatesPoints(c *gin.Context, slug, projectID string) (bool, error) {
	var count int64
	err := handler.db.WithContext(c.Request.Context()).Table("projects p").
		Joins("JOIN workspaces w ON w.id = p.workspace_id").
		Joins("JOIN estimates e ON e.id = p.estimate_id AND e.type = 'points'").
		Where("w.slug = ? AND p.id = ? AND p.estimate_id IS NOT NULL", slug, projectID).
		Count(&count).Error
	return count > 0, err
}

// distributionRow carries both shapes: the assignee columns or the label ones, and either the three sums or the three counts.
type distributionRow struct {
	DisplayName *string `gorm:"column:display_name"`
	// The module distributions name a first and a last name where the cycle's name a display name, so both are read here.
	FirstName   *string  `gorm:"column:first_name"`
	LastName    *string  `gorm:"column:last_name"`
	AssigneeID  *string  `gorm:"column:assignee_id"`
	AvatarURL   *string  `gorm:"column:avatar_url"`
	LabelName   *string  `gorm:"column:label_name"`
	Color       *string  `gorm:"column:color"`
	LabelID     *string  `gorm:"column:label_id"`
	TotalPoints *float64 `gorm:"column:total_estimates"`
	DonePoints  *float64 `gorm:"column:completed_estimates"`
	LeftPoints  *float64 `gorm:"column:pending_estimates"`
	TotalIssues int64    `gorm:"column:total_issues"`
	DoneIssues  int64    `gorm:"column:completed_issues"`
	LeftIssues  int64    `gorm:"column:pending_issues"`
}

// The three conditions the completed and pending aggregates carry. They repeat the manager's own exclusions, which is harmless, and add the one that matters.
const (
	completedIssues = "i.completed_at IS NOT NULL AND i.archived_at IS NULL AND i.is_draft = FALSE"
	pendingIssues   = "i.completed_at IS NULL AND i.archived_at IS NULL AND i.is_draft = FALSE"
	liveIssue       = "i.archived_at IS NULL AND i.is_draft = FALSE"
)

// completedDay is TruncDate over the completion time. The zone is named rather than left to the session, because Postgres' DATE() reads the connection's TimeZone while Django always writes the zone it is configured with, and the two do not have to agree.
const completedDay = "(i.completed_at AT TIME ZONE 'UTC')::date"

// avatarURL is the Case that prefers the uploaded asset over the stored URL.
const avatarURL = `CASE WHEN u.avatar_asset_id IS NOT NULL
	THEN '/api/assets/v2/static/' || CAST(u.avatar_asset_id AS TEXT) || '/'
	ELSE u.avatar END AS avatar_url`

// cycleAssigneeScope is the join every assignee distribution starts from. The link to the assignee is not filtered on deleted_at, because a many-to-many traversal joins the through table plainly — a soft-deleted assignment still shows up here.
func (handler *Handler) cycleAssigneeScope(c *gin.Context, slug, projectID, cycleID string) *gorm.DB {
	return handler.db.WithContext(c.Request.Context()).Table("issues i").
		Joins("JOIN workspaces w ON w.id = i.workspace_id").
		Joins("JOIN cycle_issues ci ON ci.issue_id = i.id AND ci.cycle_id = ? AND ci.deleted_at IS NULL", cycleID).
		Joins("LEFT JOIN issue_assignees ia ON ia.issue_id = i.id").
		Joins("LEFT JOIN users u ON u.id = ia.assignee_id").
		Where("w.slug = ? AND i.project_id = ?", slug, projectID).
		Where(issueObjectsPredicate("i")).
		Group("u.display_name, ia.assignee_id, u.avatar_asset_id, u.avatar")
}

// cycleLabelScope is the same for labels.
func (handler *Handler) cycleLabelScope(c *gin.Context, slug, projectID, cycleID string) *gorm.DB {
	return handler.db.WithContext(c.Request.Context()).Table("issues i").
		Joins("JOIN workspaces w ON w.id = i.workspace_id").
		Joins("JOIN cycle_issues ci ON ci.issue_id = i.id AND ci.cycle_id = ? AND ci.deleted_at IS NULL", cycleID).
		Joins("LEFT JOIN issue_labels il ON il.issue_id = i.id").
		Joins("LEFT JOIN labels l ON l.id = il.label_id").
		Where("w.slug = ? AND i.project_id = ?", slug, projectID).
		Where(issueObjectsPredicate("i")).
		Group("l.name, l.color, il.label_id")
}

// cycleIssueDistributions counts issues per assignee and per label.
//
// Each count is over the grouping column rather than over the row, so the bucket holding the issues with no assignee counts **zero** of them — counting a null column counts nothing. That is upstream's behaviour and the frontend draws it.
func (handler *Handler) cycleIssueDistributions(c *gin.Context, slug, projectID, cycleID string) ([]gin.H, []gin.H, error) {
	counts := func(column string) string {
		return `COUNT(` + column + `) FILTER (WHERE ` + liveIssue + `) AS total_issues,
			COUNT(` + column + `) FILTER (WHERE ` + completedIssues + `) AS completed_issues,
			COUNT(` + column + `) FILTER (WHERE ` + pendingIssues + `) AS pending_issues`
	}
	var assigneeRows []distributionRow
	err := handler.cycleAssigneeScope(c, slug, projectID, cycleID).
		Select("u.display_name, ia.assignee_id, " + avatarURL + ", " + counts("ia.assignee_id")).
		Order("u.display_name").Scan(&assigneeRows).Error
	if err != nil {
		return nil, nil, err
	}
	var labelRows []distributionRow
	err = handler.cycleLabelScope(c, slug, projectID, cycleID).
		Select("l.name AS label_name, l.color, il.label_id, " + counts("il.label_id")).
		Order("l.name").Scan(&labelRows).Error
	if err != nil {
		return nil, nil, err
	}

	assignees := make([]gin.H, 0, len(assigneeRows))
	for _, row := range assigneeRows {
		assignees = append(assignees, gin.H{
			"display_name": row.DisplayName, "assignee_id": row.AssigneeID, "avatar_url": row.AvatarURL,
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

// cycleEstimateDistributions is the same two groupings, summing points instead of counting issues.
//
// A sum with nothing to add is null rather than zero, and nothing here rewrites it: a bucket whose issues are all still open reports `null` completed points.
func (handler *Handler) cycleEstimateDistributions(c *gin.Context, slug, projectID, cycleID string) ([]gin.H, []gin.H, error) {
	const sums = `SUM(CAST(ep.value AS DOUBLE PRECISION)) AS total_estimates,
		SUM(CAST(ep.value AS DOUBLE PRECISION)) FILTER (WHERE ` + completedIssues + `) AS completed_estimates,
		SUM(CAST(ep.value AS DOUBLE PRECISION)) FILTER (WHERE ` + pendingIssues + `) AS pending_estimates`

	var assigneeRows []distributionRow
	err := handler.cycleAssigneeScope(c, slug, projectID, cycleID).
		Joins("LEFT JOIN estimate_points ep ON ep.id = i.estimate_point_id").
		Select("u.display_name, ia.assignee_id, " + avatarURL + ", " + sums).
		Order("u.display_name").Scan(&assigneeRows).Error
	if err != nil {
		return nil, nil, err
	}
	var labelRows []distributionRow
	err = handler.cycleLabelScope(c, slug, projectID, cycleID).
		Joins("LEFT JOIN estimate_points ep ON ep.id = i.estimate_point_id").
		Select("l.name AS label_name, l.color, il.label_id, " + sums).
		Order("l.name").Scan(&labelRows).Error
	if err != nil {
		return nil, nil, err
	}

	assignees := make([]gin.H, 0, len(assigneeRows))
	for _, row := range assigneeRows {
		assignees = append(assignees, gin.H{
			"display_name": row.DisplayName, "assignee_id": row.AssigneeID, "avatar_url": row.AvatarURL,
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

// cycleBurndown gathers what the chart counts down from and the days it was whittled away on.
func (handler *Handler) cycleBurndown(c *gin.Context, slug, projectID, cycleID string, cycle Cycle, totalIssues int64, points bool) (gin.H, error) {
	input := burndownInput{Start: cycle.StartDate, End: cycle.EndDate, Points: points, Total: float64(totalIssues)}

	scope := func() *gorm.DB {
		return handler.db.WithContext(c.Request.Context()).Table("issues i").
			Joins("JOIN workspaces w ON w.id = i.workspace_id").
			Joins("JOIN cycle_issues ci ON ci.issue_id = i.id AND ci.cycle_id = ? AND ci.deleted_at IS NULL", cycleID).
			Where("w.slug = ? AND i.project_id = ?", slug, projectID).
			Where(issueObjectsPredicate("i"))
	}

	if !points {
		// One row per day, already counted, which is what the issues plot subtracts.
		var rows []struct {
			Date  *time.Time `gorm:"column:date"`
			Total int64      `gorm:"column:total_completed"`
		}
		err := scope().Select(completedDay + " AS date, COUNT(i.id) AS total_completed").
			Group(completedDay).Order(completedDay).Scan(&rows).Error
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			input.Completions = append(input.Completions, burndownCompletion{Date: row.Date, Value: float64(row.Total)})
		}
		return burndownChart(input, handler.clock().UTC()), nil
	}

	// The points plot subtracts one row per estimated issue rather than one per day: the Python never groups them.
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
	// The total is summed over the same rows, which is why an unestimated issue counts towards neither.
	var total float64
	for _, row := range rows {
		total += row.Value
		input.Completions = append(input.Completions, burndownCompletion{Date: row.Date, Value: row.Value})
	}
	input.Total = total
	input.TotalIsFloat = len(rows) > 0
	return burndownChart(input, handler.clock().UTC()), nil
}

// snapshotList reads a list out of a stored distribution, answering the empty list the way .get(key, []) does.
func snapshotList(distribution map[string]any, key string) any {
	if value, present := distribution[key]; present {
		return value
	}
	return []any{}
}

// snapshotObject is the same for the chart, which defaults to an empty object.
func snapshotObject(distribution map[string]any, key string) any {
	if value, present := distribution[key]; present {
		return value
	}
	return map[string]any{}
}
