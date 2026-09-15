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

func (handler *Handler) registerAdvanceAnalyticsRoutes(router gin.IRouter) {
	router.GET("/api/workspaces/:slug/advance-analytics/", handler.authenticated(handler.advanceAnalytics))
	router.GET("/api/workspaces/:slug/advance-analytics-stats/", handler.authenticated(handler.advanceAnalyticsStats))
	router.GET("/api/workspaces/:slug/advance-analytics-charts/", handler.authenticated(handler.advanceAnalyticsChart))
}

// intakeAnalyticsStatuses are the five intake statuses the overview counts, which is every status there is — so the count is really "work items that arrived through an intake".
var intakeAnalyticsStatuses = []int{-2, -1, 0, 1, 2}

// advanceAnalytics is the numbers along the top of the analytics page, in one of two shapes.
func (handler *Handler) advanceAnalytics(c *gin.Context, user *auth.User) {
	if !handler.requireWorkspaceRole(c, user, roleAdmin, roleMember) {
		return
	}
	filters := handler.analyticsFilters(c, user, "analytics")
	switch c.DefaultQuery("tab", "overview") {
	case "overview":
		data, err := handler.analyticsOverview(c, filters)
		if err != nil {
			handler.internalError(c, err)
			return
		}
		drf.Respond(c, http.StatusOK, data)
	case "work-items":
		data, err := handler.analyticsWorkItemTotals(c, filters)
		if err != nil {
			handler.internalError(c, err)
			return
		}
		drf.Respond(c, http.StatusOK, data)
	default:
		c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid tab"})
	}
}

// analyticsOverview is the eight totals the overview tab shows, each of them wrapped in its own object because a comparison against the previous period used to sit beside it.
//
// Naming projects changes what "users" means: without project_ids it counts the people in the workspace, with them it counts the memberships of those projects — so somebody in two of the named projects is counted twice.
func (handler *Handler) analyticsOverview(c *gin.Context, filters analyticsFilters) (gin.H, error) {
	members := func() *gorm.DB {
		if len(filters.projectIDs) > 0 {
			return handler.db.WithContext(c.Request.Context()).Table("project_members m").
				Joins("JOIN users mu ON mu.id = m.member_id").
				Where("m.project_id IN ? AND m.is_active = TRUE AND m.deleted_at IS NULL AND mu.is_bot = FALSE", filters.projectIDs)
		}
		return handler.db.WithContext(c.Request.Context()).Table("workspace_members m").
			Joins("JOIN workspaces mw ON mw.id = m.workspace_id").
			Joins("JOIN users mu ON mu.id = m.member_id").
			Where("mw.slug = ? AND m.is_active = TRUE AND m.deleted_at IS NULL AND mu.is_bot = FALSE", filters.slug)
	}

	counts := gin.H{}
	for _, entry := range []struct {
		name string
		role int
	}{
		{name: "total_users"}, {name: "total_admins", role: roleAdmin},
		{name: "total_members", role: roleMember}, {name: "total_guests", role: roleGuest},
	} {
		query := members()
		if entry.role != 0 {
			query = query.Where("m.role = ?", entry.role)
		}
		total, err := handler.windowedCount(query, filters, "m")
		if err != nil {
			return nil, err
		}
		counts[entry.name] = gin.H{"count": total}
	}

	projectCondition, projectArguments := filters.projectScope("p")
	projects, err := handler.windowedCount(handler.db.WithContext(c.Request.Context()).Table("projects p").
		Where(projectCondition, projectArguments...), filters, "p")
	if err != nil {
		return nil, err
	}
	counts["total_projects"] = gin.H{"count": projects}

	workItems, err := handler.windowedCount(handler.workItemScope(c, filters), filters, "i")
	if err != nil {
		return nil, err
	}
	counts["total_work_items"] = gin.H{"count": workItems}

	cycleCondition, cycleArguments := filters.baseScope("cy")
	cycles, err := handler.windowedCount(handler.db.WithContext(c.Request.Context()).Table("cycles cy").
		Where("cy.deleted_at IS NULL").Where(cycleCondition, cycleArguments...), filters, "cy")
	if err != nil {
		return nil, err
	}
	counts["total_cycles"] = gin.H{"count": cycles}

	intake, err := handler.windowedCount(handler.intakeWorkItemScope(c, filters, true), filters, "i")
	if err != nil {
		return nil, err
	}
	counts["total_intake"] = gin.H{"count": intake}
	return counts, nil
}

// analyticsWorkItemTotals is the work-items tab: the same total split by the four state groups that are not cancelled.
func (handler *Handler) analyticsWorkItemTotals(c *gin.Context, filters analyticsFilters) (gin.H, error) {
	counts := gin.H{}
	for _, entry := range []struct {
		name  string
		group string
	}{
		{name: "total_work_items"}, {name: "started_work_items", group: "started"},
		{name: "backlog_work_items", group: "backlog"}, {name: "un_started_work_items", group: "unstarted"},
		{name: "completed_work_items", group: "completed"},
	} {
		query := handler.workItemScope(c, filters)
		if entry.group != "" {
			query = query.Where(`s."group" = ?`, entry.group)
		}
		total, err := handler.windowedCount(query, filters, "i")
		if err != nil {
			return nil, err
		}
		counts[entry.name] = gin.H{"count": total}
	}
	return counts, nil
}

// advanceAnalyticsStats is one row per project with its work items split by state group.
//
// It asks for the chart date range and then never uses it: the method the route calls is the one without the date filter, and the one with it is unreachable. So the rows are the whole history however the caller narrows the dates.
func (handler *Handler) advanceAnalyticsStats(c *gin.Context, user *auth.User) {
	if !handler.requireWorkspaceRole(c, user, roleAdmin, roleMember) {
		return
	}
	if c.DefaultQuery("type", "work-items") != "work-items" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid type"})
		return
	}
	filters := handler.analyticsFilters(c, user, "chart")
	var rows []struct {
		ProjectID          string `gorm:"column:project_id"`
		ProjectName        string `gorm:"column:project__name"`
		Cancelled          int64  `gorm:"column:cancelled_work_items"`
		Completed          int64  `gorm:"column:completed_work_items"`
		Backlog            int64  `gorm:"column:backlog_work_items"`
		UnStarted          int64  `gorm:"column:un_started_work_items"`
		StartedWorkItemsNo int64  `gorm:"column:started_work_items"`
	}
	err := handler.workItemScope(c, filters).
		Select(`i.project_id, p.name AS project__name,
			COUNT(i.id) FILTER (WHERE s."group" = 'cancelled') AS cancelled_work_items,
			COUNT(i.id) FILTER (WHERE s."group" = 'completed') AS completed_work_items,
			COUNT(i.id) FILTER (WHERE s."group" = 'backlog') AS backlog_work_items,
			COUNT(i.id) FILTER (WHERE s."group" = 'unstarted') AS un_started_work_items,
			COUNT(i.id) FILTER (WHERE s."group" = 'started') AS started_work_items`).
		Group("i.project_id, p.name").Order("i.project_id").Scan(&rows).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	results := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		results = append(results, gin.H{
			"project_id": row.ProjectID, "project__name": row.ProjectName,
			"cancelled_work_items": row.Cancelled, "completed_work_items": row.Completed,
			"backlog_work_items": row.Backlog, "un_started_work_items": row.UnStarted,
			"started_work_items": row.StartedWorkItemsNo,
		})
	}
	drf.Respond(c, http.StatusOK, results)
}

// advanceAnalyticsChart is the three charts the analytics page draws.
func (handler *Handler) advanceAnalyticsChart(c *gin.Context, user *auth.User) {
	if !handler.requireWorkspaceRole(c, user, roleAdmin, roleMember) {
		return
	}
	filters := handler.analyticsFilters(c, user, "chart")
	switch c.DefaultQuery("type", "projects") {
	case "projects":
		data, err := handler.analyticsProjectChart(c, filters)
		if err != nil {
			handler.internalError(c, err)
			return
		}
		drf.Respond(c, http.StatusOK, data)
	case "custom-work-items":
		scope := handler.workItemScope(c, filters)
		if condition, arguments := filters.periodScope("i"); condition != "" {
			scope = scope.Where(condition, arguments...)
		}
		xAxis, groupBy := c.DefaultQuery("x_axis", "PRIORITY"), c.Query("group_by")
		chart, err := buildAnalyticsChart(scope, xAxis, groupBy)
		// DRF wraps a ValidationError raised with a bare string in a list, which is what the client receives.
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
		data, err := handler.analyticsCompletionChart(c, filters)
		if err != nil {
			handler.internalError(c, err)
			return
		}
		drf.Respond(c, http.StatusOK, data)
	default:
		c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid type"})
	}
}

// analyticsProjectChart is seven totals rendered as a bar chart, in a fixed order.
//
// The member total here is not the overview's: it counts every active workspace member including the bots, and it ignores project_ids entirely.
func (handler *Handler) analyticsProjectChart(c *gin.Context, filters analyticsFilters) ([]gin.H, error) {
	period := func(query *gorm.DB, alias string) *gorm.DB {
		if condition, arguments := filters.periodScope(alias); condition != "" {
			return query.Where(condition, arguments...)
		}
		return query
	}
	simple := func(table, alias string) *gorm.DB {
		condition, arguments := filters.baseScope(alias)
		return period(handler.db.WithContext(c.Request.Context()).Table(table+" "+alias).
			Where(alias+".deleted_at IS NULL").Where(condition, arguments...), alias)
	}

	totals := []struct {
		key   string
		query *gorm.DB
	}{
		{key: "work_items", query: period(handler.workItemScope(c, filters), "i")},
		{key: "cycles", query: simple("cycles", "cy")},
		{key: "modules", query: simple("modules", "mo")},
		{key: "intake", query: period(handler.intakeWorkItemScope(c, filters, false), "i")},
		{key: "members", query: period(handler.db.WithContext(c.Request.Context()).Table("workspace_members m").
			Joins("JOIN workspaces mw ON mw.id = m.workspace_id").
			Where("mw.slug = ? AND m.is_active = TRUE AND m.deleted_at IS NULL", filters.slug), "m")},
		{key: "pages", query: simple("project_pages", "pp")},
		{key: "views", query: simple("issue_views", "iv")},
	}
	data := make([]gin.H, 0, len(totals))
	for _, total := range totals {
		var count int64
		if err := total.query.Count(&count).Error; err != nil {
			return nil, err
		}
		data = append(data, gin.H{"key": total.key, "name": chartKeyName(total.key), "count": count})
	}
	return data, nil
}

// analyticsCompletionChart is one point per month from the workspace's first month to this one, with the months that have nothing in them filled in as zero.
//
// Narrowing the dates moves the first month but not the last: the loop always runs to the current month, so a range ending last year still draws every month since as an empty one.
func (handler *Handler) analyticsCompletionChart(c *gin.Context, filters analyticsFilters) (gin.H, error) {
	var created []time.Time
	err := handler.db.WithContext(c.Request.Context()).Table("workspaces").
		Where("slug = ?", filters.slug).Limit(1).Pluck("created_at", &created).Error
	if err != nil {
		return nil, err
	}
	if len(created) == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	start := monthStart(created[0].UTC())

	scope := handler.workItemScope(c, filters)
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
		return nil, err
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
	return gin.H{
		"data":   data,
		"schema": gin.H{"completed_issues": "completed_issues", "created_issues": "created_issues"},
	}, nil
}

func monthStart(moment time.Time) time.Time {
	return time.Date(moment.Year(), moment.Month(), 1, 0, 0, 0, 0, time.UTC)
}

// analyticsFilters reads the query string into the filter set every one of the three routes starts from.
func (handler *Handler) analyticsFilters(c *gin.Context, user *auth.User, shape string) analyticsFilters {
	return newAnalyticsFilters(c.Param("slug"), user.ID, c.Query("project_ids"), c.Query("date_filter"), shape, handler.clock().UTC())
}

// workItemScope is the issue_objects manager over the analytics filters, with the state joined because four of the five numbers are cut by its group.
func (handler *Handler) workItemScope(c *gin.Context, filters analyticsFilters) *gorm.DB {
	condition, arguments := filters.baseScope("i")
	return handler.db.WithContext(c.Request.Context()).Table("issues i").
		Joins(`LEFT JOIN states s ON s.id = i.state_id`).
		Joins("JOIN projects p ON p.id = i.project_id").
		Where(`i.deleted_at IS NULL AND s."group" IS DISTINCT FROM 'triage' AND i.archived_at IS NULL
			AND p.archived_at IS NULL AND i.is_draft = FALSE`).
		Where(condition, arguments...)
}

// intakeWorkItemScope counts work items that came in through an intake. It reads through the plain manager rather than issue_objects, so unlike every other number on the page it includes the archived ones, the drafts and the ones still in triage.
func (handler *Handler) intakeWorkItemScope(c *gin.Context, filters analyticsFilters, byStatus bool) *gorm.DB {
	condition, arguments := filters.baseScope("i")
	query := handler.db.WithContext(c.Request.Context()).Table("issues i").
		Where("i.deleted_at IS NULL").Where(condition, arguments...)
	if byStatus {
		return query.Joins("JOIN intake_issues ii ON ii.issue_id = i.id AND ii.status IN ?", intakeAnalyticsStatuses)
	}
	return query.Joins("JOIN intake_issues ii ON ii.issue_id = i.id")
}

// windowedCount is get_filtered_counts: the count, narrowed to the analytics period when the caller named one.
func (handler *Handler) windowedCount(query *gorm.DB, filters analyticsFilters, alias string) (int64, error) {
	if condition, arguments := filters.windowScope(alias); condition != "" {
		query = query.Where(condition, arguments...)
	}
	var total int64
	err := query.Count(&total).Error
	return total, err
}
