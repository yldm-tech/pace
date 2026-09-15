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

func (handler *Handler) registerProjectAnalyticsRoutes(router gin.IRouter) {
	router.GET("/api/workspaces/:slug/projects/:id/advance-analytics/", handler.authenticated(handler.projectAnalytics))
	router.GET("/api/workspaces/:slug/projects/:id/advance-analytics-stats/", handler.authenticated(handler.projectAnalyticsStats))
	router.GET("/api/workspaces/:slug/projects/:id/advance-analytics-charts/", handler.authenticated(handler.projectAnalyticsChart))
}

// The two errors these routes reach a 500 through, both of them Django's rather than ours.
var (
	errAnalyticsCycleWindow  = errors.New("project analytics: the cycle has a start date and no end date, and Django asks the missing one for its date")
	errAnalyticsModuleWindow = errors.New("project analytics: the module has a start date and no target date, and Django compares a date with nothing")
	errAnalyticsNoProject    = errors.New("project analytics: the project does not exist, and Django asks it for its created_at anyway")
)

// projectAnalytics is the five totals for one project, or for a cycle or a module inside it.
func (handler *Handler) projectAnalytics(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember) {
		return
	}
	filters := handler.analyticsFilters(c, user, "analytics")
	scope := handler.projectAnalyticsScope(c, filters)
	counts := gin.H{}
	for _, entry := range []struct {
		name  string
		group string
	}{
		{name: "total_work_items"}, {name: "started_work_items", group: "started"},
		{name: "backlog_work_items", group: "backlog"}, {name: "un_started_work_items", group: "unstarted"},
		{name: "completed_work_items", group: "completed"},
	} {
		query := scope()
		if entry.group != "" {
			query = query.Where(`s."group" = ?`, entry.group)
		}
		total, err := handler.windowedCount(query, filters, "i")
		if err != nil {
			handler.internalError(c, err)
			return
		}
		counts[entry.name] = gin.H{"count": total}
	}
	drf.Respond(c, http.StatusOK, counts)
}

// projectAnalyticsStats is the same five totals cut by the person they are assigned to.
//
// A work item with nobody on it still produces a row, with no name and no id, because the assignee join is an outer one. And the join does not check whether the assignment was taken back: a deleted assignee link still puts the person in the list.
func (handler *Handler) projectAnalyticsStats(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember) {
		return
	}
	if c.DefaultQuery("type", "work-items") != "work-items" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid type"})
		return
	}
	filters := handler.analyticsFilters(c, user, "chart")
	scope := handler.projectAnalyticsScope(c, filters)

	var rows []struct {
		DisplayName *string `gorm:"column:display_name"`
		AssigneeID  *string `gorm:"column:assignee_id"`
		AvatarURL   *string `gorm:"column:avatar_url"`
		Cancelled   int64   `gorm:"column:cancelled_work_items"`
		Completed   int64   `gorm:"column:completed_work_items"`
		Backlog     int64   `gorm:"column:backlog_work_items"`
		UnStarted   int64   `gorm:"column:un_started_work_items"`
		Started     int64   `gorm:"column:started_work_items"`
	}
	err := scope().
		Joins("LEFT JOIN issue_assignees sa ON sa.issue_id = i.id").
		Joins("LEFT JOIN users su ON su.id = sa.assignee_id").
		Select(`su.display_name AS display_name, su.id AS assignee_id,
			CASE WHEN su.avatar_asset_id IS NOT NULL
				THEN '/api/assets/v2/static/' || su.avatar_asset_id::text || '/'
				ELSE su.avatar END AS avatar_url,
			COUNT(DISTINCT i.id) FILTER (WHERE s."group" = 'cancelled') AS cancelled_work_items,
			COUNT(DISTINCT i.id) FILTER (WHERE s."group" = 'completed') AS completed_work_items,
			COUNT(DISTINCT i.id) FILTER (WHERE s."group" = 'backlog') AS backlog_work_items,
			COUNT(DISTINCT i.id) FILTER (WHERE s."group" = 'unstarted') AS un_started_work_items,
			COUNT(DISTINCT i.id) FILTER (WHERE s."group" = 'started') AS started_work_items`).
		Group("su.display_name, su.id, su.avatar_asset_id, su.avatar").
		Order("su.display_name").Scan(&rows).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	results := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		results = append(results, gin.H{
			"display_name": row.DisplayName, "assignee_id": row.AssigneeID, "avatar_url": row.AvatarURL,
			"cancelled_work_items": row.Cancelled, "completed_work_items": row.Completed,
			"backlog_work_items": row.Backlog, "un_started_work_items": row.UnStarted,
			"started_work_items": row.Started,
		})
	}
	drf.Respond(c, http.StatusOK, results)
}

// projectAnalyticsChart is the project's two charts.
func (handler *Handler) projectAnalyticsChart(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	filters := handler.analyticsFilters(c, user, "chart")
	switch c.DefaultQuery("type", "projects") {
	case "custom-work-items":
		// Unlike the two totals routes, this one starts from the project in the url and then narrows to the cycle or module rather than replacing the project with it.
		scope := handler.workItemScope(c, filters).Where("i.project_id = ?", c.Param("id"))
		narrowed := handler.narrowToCycleOrModule(c, filters, scope)
		if condition, arguments := filters.periodScope("i"); condition != "" {
			narrowed = narrowed.Where(condition, arguments...)
		}
		xAxis, groupBy := c.DefaultQuery("x_axis", "PRIORITY"), c.Query("group_by")
		chart, err := buildAnalyticsChart(narrowed, xAxis, groupBy)
		if errors.Is(err, errInvalidXAxis) {
			c.JSON(http.StatusBadRequest, []string{"Invalid x_axis field: " + xAxis})
			return
		}
		if errors.Is(err, errInvalidGroupBy) {
			c.JSON(http.StatusBadRequest, []string{"Invalid group_by field: " + groupBy})
			return
		}
		if err != nil {
			handler.internalError(c, err)
			return
		}
		drf.Respond(c, http.StatusOK, chart)
	case "work-items":
		handler.projectCompletionChart(c, filters)
	default:
		c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid type"})
	}
}

// projectCompletionChart is the completion curve, and it is two different charts wearing one name.
//
// For a project it is monthly, counts only what was created, and runs to the current month whatever the caller asked for. For a cycle or a module it is **daily**, it counts links rather than work items — so the created curve is when work items were put into the cycle rather than when they were raised — and its count is the created and the completed added together rather than the created alone.
func (handler *Handler) projectCompletionChart(c *gin.Context, filters analyticsFilters) {
	cycleID, moduleID := c.Query("cycle_id"), c.Query("module_id")
	if cycleID != "" || moduleID != "" {
		handler.projectDailyCompletionChart(c, filters, cycleID, moduleID)
		return
	}

	var created []time.Time
	err := handler.db.WithContext(c.Request.Context()).Table("projects").
		Where("id = ?", c.Param("id")).Limit(1).Pluck("created_at", &created).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if len(created) == 0 {
		handler.internalError(c, errAnalyticsNoProject)
		return
	}
	start := monthStart(created[0].UTC())

	scope := handler.workItemScope(c, filters).Where("i.project_id = ?", c.Param("id"))
	if filters.period != nil {
		start = monthStart(filters.period.start)
		if condition, arguments := filters.periodScope("i"); condition != "" {
			scope = scope.Where(condition, arguments...)
		}
	}

	var rows []struct {
		Month     time.Time `gorm:"column:month"`
		Created   int64     `gorm:"column:created_count"`
		Completed int64     `gorm:"column:completed_count"`
	}
	err = scope.Select(`DATE_TRUNC('month', i.created_at AT TIME ZONE 'UTC') AS month,
		COUNT(i.id) AS created_count,
		COUNT(i.id) FILTER (WHERE s."group" = 'completed') AS completed_count`).
		Group("1").Order("1").Scan(&rows).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	monthly := map[string][2]int64{}
	for _, row := range rows {
		monthly[row.Month.Format("2006-01-02")] = [2]int64{row.Created, row.Completed}
	}

	data := []gin.H{}
	last := monthStart(handler.clock().UTC())
	for month := start; !month.After(last); month = month.AddDate(0, 1, 0) {
		label := month.Format("2006-01-02")
		counts := monthly[label]
		data = append(data, gin.H{
			"key": label, "name": label, "count": counts[0],
			"completed_issues": counts[1], "created_issues": counts[0],
		})
	}
	drf.Respond(c, http.StatusOK, gin.H{"data": data, "schema": completionChartSchema()})
}

// projectDailyCompletionChart is the cycle and module half of the curve, one point per day between the two dates the cycle or module carries.
func (handler *Handler) projectDailyCompletionChart(c *gin.Context, filters analyticsFilters, cycleID, moduleID string) {
	var start, end time.Time
	var scope *gorm.DB

	if cycleID != "" {
		var window []struct {
			StartDate *time.Time `gorm:"column:start_date"`
			EndDate   *time.Time `gorm:"column:end_date"`
		}
		err := handler.db.WithContext(c.Request.Context()).Table("cycles").
			Select("start_date, end_date").Where("id = ?", cycleID).Limit(1).Scan(&window).Error
		if err != nil {
			handler.internalError(c, err)
			return
		}
		// A cycle that does not exist, or one with no start date, answers with an empty chart rather than an error.
		if len(window) == 0 || window[0].StartDate == nil {
			drf.Respond(c, http.StatusOK, gin.H{"data": []gin.H{}, "schema": gin.H{}})
			return
		}
		if window[0].EndDate == nil {
			// Django asks the missing end date for its date, which raises.
			handler.internalError(c, errAnalyticsCycleWindow)
			return
		}
		start, end = dayOf(*window[0].StartDate), dayOf(*window[0].EndDate)
		condition, arguments := filters.baseScope("ci")
		scope = handler.db.WithContext(c.Request.Context()).Table("cycle_issues ci").
			Joins("JOIN issues ci_i ON ci_i.id = ci.issue_id").
			Joins(`LEFT JOIN states s ON s.id = ci_i.state_id`).
			Where("ci.deleted_at IS NULL AND ci.cycle_id = ?", cycleID).Where(condition, arguments...)
	} else {
		var window []struct {
			StartDate  *time.Time `gorm:"column:start_date"`
			TargetDate *time.Time `gorm:"column:target_date"`
		}
		err := handler.db.WithContext(c.Request.Context()).Table("modules").
			Select("start_date, target_date").Where("id = ?", moduleID).Limit(1).Scan(&window).Error
		if err != nil {
			handler.internalError(c, err)
			return
		}
		if len(window) == 0 || window[0].StartDate == nil {
			drf.Respond(c, http.StatusOK, gin.H{"data": []gin.H{}, "schema": gin.H{}})
			return
		}
		if window[0].TargetDate == nil {
			// Django compares a date with nothing, which raises.
			handler.internalError(c, errAnalyticsModuleWindow)
			return
		}
		start, end = dayOf(*window[0].StartDate), dayOf(*window[0].TargetDate)
		condition, arguments := filters.baseScope("mi")
		scope = handler.db.WithContext(c.Request.Context()).Table("module_issues mi").
			Joins("JOIN issues mi_i ON mi_i.id = mi.issue_id").
			Joins(`LEFT JOIN states s ON s.id = mi_i.state_id`).
			Where("mi.deleted_at IS NULL AND mi.module_id = ?", moduleID).Where(condition, arguments...)
	}

	alias := "ci"
	if cycleID == "" {
		alias = "mi"
	}
	var rows []struct {
		Day       time.Time `gorm:"column:day"`
		Created   int64     `gorm:"column:created_count"`
		Completed int64     `gorm:"column:completed_count"`
	}
	err := scope.Select(`(` + alias + `.created_at AT TIME ZONE 'UTC')::date AS day,
		COUNT(` + alias + `.id) AS created_count,
		COUNT(` + alias + `.id) FILTER (WHERE s."group" = 'completed') AS completed_count`).
		Group("1").Order("1").Scan(&rows).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	daily := map[string][2]int64{}
	for _, row := range rows {
		daily[row.Day.Format("2006-01-02")] = [2]int64{row.Created, row.Completed}
	}

	data := []gin.H{}
	for day := start; !day.After(end); day = day.AddDate(0, 0, 1) {
		label := day.Format("2006-01-02")
		counts := daily[label]
		data = append(data, gin.H{
			"key": label, "name": label, "count": counts[0] + counts[1],
			"completed_issues": counts[1], "created_issues": counts[0],
		})
	}
	drf.Respond(c, http.StatusOK, gin.H{"data": data, "schema": completionChartSchema()})
}

func completionChartSchema() gin.H {
	return gin.H{"completed_issues": "completed_issues", "created_issues": "created_issues"}
}

// projectAnalyticsScope is what the two totals routes count over.
//
// Naming a cycle or a module **replaces** the project rather than narrowing it: the work items are whichever ones that cycle or module holds, and the project in the url stops mattering — as do the analytics filters on the work items themselves, which move onto the link table instead. So a cycle in a different project of the same workspace is accepted and its work items are counted.
func (handler *Handler) projectAnalyticsScope(c *gin.Context, filters analyticsFilters) func() *gorm.DB {
	cycleID, moduleID := c.Query("cycle_id"), c.Query("module_id")
	switch {
	case cycleID != "":
		condition, arguments := filters.baseScope("ci")
		return func() *gorm.DB {
			return handler.workItemManagerScope(c).Where(`i.id IN (
				SELECT ci.issue_id FROM cycle_issues ci WHERE ci.deleted_at IS NULL AND ci.cycle_id = ? AND `+condition+`)`,
				append([]any{cycleID}, arguments...)...)
		}
	case moduleID != "":
		condition, arguments := filters.baseScope("mi")
		return func() *gorm.DB {
			return handler.workItemManagerScope(c).Where(`i.id IN (
				SELECT mi.issue_id FROM module_issues mi WHERE mi.deleted_at IS NULL AND mi.module_id = ? AND `+condition+`)`,
				append([]any{moduleID}, arguments...)...)
		}
	}
	return func() *gorm.DB {
		return handler.workItemScope(c, filters).Where("i.project_id = ?", c.Param("id"))
	}
}

// narrowToCycleOrModule is the custom chart's version of the same choice, and it narrows rather than replaces — the project in the url still applies, and so do the analytics filters.
func (handler *Handler) narrowToCycleOrModule(c *gin.Context, filters analyticsFilters, scope *gorm.DB) *gorm.DB {
	cycleID, moduleID := c.Query("cycle_id"), c.Query("module_id")
	switch {
	case cycleID != "":
		condition, arguments := filters.baseScope("ci")
		return scope.Where(`i.id IN (
			SELECT ci.issue_id FROM cycle_issues ci WHERE ci.deleted_at IS NULL AND ci.cycle_id = ? AND `+condition+`)`,
			append([]any{cycleID}, arguments...)...)
	case moduleID != "":
		condition, arguments := filters.baseScope("mi")
		return scope.Where(`i.id IN (
			SELECT mi.issue_id FROM module_issues mi WHERE mi.deleted_at IS NULL AND mi.module_id = ? AND `+condition+`)`,
			append([]any{moduleID}, arguments...)...)
	}
	return scope
}

// workItemManagerScope is issue_objects on its own, with none of the analytics filters. It is what the cycle and module branches count over, because there the narrowing has already happened in the subquery.
func (handler *Handler) workItemManagerScope(c *gin.Context) *gorm.DB {
	return handler.db.WithContext(c.Request.Context()).Table("issues i").
		Joins(`LEFT JOIN states s ON s.id = i.state_id`).
		Joins("JOIN projects p ON p.id = i.project_id").
		Where(`i.deleted_at IS NULL AND s."group" IS DISTINCT FROM 'triage' AND i.archived_at IS NULL
			AND p.archived_at IS NULL AND i.is_draft = FALSE`)
}
