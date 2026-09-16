package project

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"github.com/yldm-tech/pace/apps/api-go/internal/drf"
	"github.com/yldm-tech/pace/apps/api-go/internal/pagination"
	"gorm.io/gorm"
)

// The last six of a person's own routes. They live here rather than in internal/user because five of them read work items or work item activity, which is this package's machinery.
func (handler *Handler) registerUserRestRoutes(router gin.IRouter) {
	router.GET("/api/users/me/notification-preferences/", handler.authenticated(handler.notificationPreferences))
	router.PATCH("/api/users/me/notification-preferences/", handler.authenticated(handler.notificationPreferencesUpdate))
	router.GET("/api/users/me/activities/", handler.authenticated(handler.myActivities))
	router.GET("/api/users/last-visited-workspace/", handler.authenticated(handler.lastVisitedWorkspace))
	router.GET("/api/users/me/workspaces/:slug/activity-graph/", handler.authenticated(handler.activityGraph))
	router.GET("/api/users/me/workspaces/:slug/issues-completed-graph/", handler.authenticated(handler.completedGraph))
	router.GET("/api/users/me/workspaces/:slug/dashboard/", handler.authenticated(handler.workspaceDashboard))
}

// UserNotificationPreference is the row that says which of the five things somebody wants to hear about.
type UserNotificationPreference struct {
	ID             string     `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt      time.Time  `gorm:"column:created_at"`
	UpdatedAt      time.Time  `gorm:"column:updated_at"`
	CreatedByID    *string    `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID    *string    `gorm:"column:updated_by_id;type:uuid"`
	DeletedAt      *time.Time `gorm:"column:deleted_at"`
	UserID         string     `gorm:"column:user_id;type:uuid"`
	WorkspaceID    *string    `gorm:"column:workspace_id;type:uuid"`
	ProjectID      *string    `gorm:"column:project_id;type:uuid"`
	PropertyChange bool       `gorm:"column:property_change"`
	StateChange    bool       `gorm:"column:state_change"`
	Comment        bool       `gorm:"column:comment"`
	Mention        bool       `gorm:"column:mention"`
	IssueCompleted bool       `gorm:"column:issue_completed"`
}

func (UserNotificationPreference) TableName() string { return "user_notification_preferences" }

// notificationPreferences reads the caller's one preference row.
//
// It reads it with a get rather than a filter, so somebody who has a workspace-level row as well as their own gets a 500 rather than either of them, and somebody with none at all gets a 404.
func (handler *Handler) notificationPreferences(c *gin.Context, user *auth.User) {
	preference, ok := handler.onlyNotificationPreference(c, user)
	if !ok {
		return
	}
	drf.Respond(c, http.StatusOK, notificationPreferenceJSON(preference))
}

func (handler *Handler) notificationPreferencesUpdate(c *gin.Context, user *auth.User) {
	preference, ok := handler.onlyNotificationPreference(c, user)
	if !ok {
		return
	}
	var body map[string]json.RawMessage
	if err := c.ShouldBindJSON(&body); err != nil {
		handler.invalidDetail(c)
		return
	}
	updates := map[string]any{}
	for _, field := range []struct {
		name   string
		target *bool
	}{
		{"property_change", &preference.PropertyChange}, {"state_change", &preference.StateChange},
		{"comment", &preference.Comment}, {"mention", &preference.Mention},
		{"issue_completed", &preference.IssueCompleted},
	} {
		raw, given := body[field.name]
		if !given {
			continue
		}
		value, ok := handler.booleanField(c, field.name, raw)
		if !ok {
			return
		}
		*field.target = value
		updates[field.name] = value
	}
	now := handler.clock().UTC()
	updates["updated_at"] = now
	updates["updated_by_id"] = user.ID
	preference.UpdatedAt = now
	preference.UpdatedByID = &user.ID
	err := handler.db.WithContext(c.Request.Context()).Table("user_notification_preferences").
		Where("id = ?", preference.ID).Updates(updates).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, notificationPreferenceJSON(preference))
}

// onlyNotificationPreference is the .get() the two routes share, including what it does when there is not exactly one row.
func (handler *Handler) onlyNotificationPreference(c *gin.Context, user *auth.User) (UserNotificationPreference, bool) {
	var rows []UserNotificationPreference
	err := handler.db.WithContext(c.Request.Context()).Table("user_notification_preferences").
		Where("user_id = ? AND deleted_at IS NULL", user.ID).Limit(2).Scan(&rows).Error
	if err != nil {
		handler.internalError(c, err)
		return UserNotificationPreference{}, false
	}
	if len(rows) == 0 {
		handler.notFound(c)
		return UserNotificationPreference{}, false
	}
	if len(rows) > 1 {
		handler.internalError(c, errTooManyPreferences)
		return UserNotificationPreference{}, false
	}
	return rows[0], true
}

var errTooManyPreferences = errorString("notification preferences: the caller has more than one row and Django asks for exactly one")

type errorString string

func (err errorString) Error() string { return string(err) }

// notificationPreferenceJSON is UserNotificationPreferenceSerializer: fourteen fields, the five switches among them.
func notificationPreferenceJSON(preference UserNotificationPreference) gin.H {
	return gin.H{
		"id": preference.ID, "created_at": preference.CreatedAt, "updated_at": preference.UpdatedAt,
		"deleted_at":      preference.DeletedAt,
		"property_change": preference.PropertyChange, "state_change": preference.StateChange,
		"comment": preference.Comment, "mention": preference.Mention,
		"issue_completed": preference.IssueCompleted,
		"created_by":      preference.CreatedByID, "updated_by": preference.UpdatedByID,
		"user": preference.UserID, "workspace": preference.WorkspaceID, "project": preference.ProjectID,
	}
}

// myActivities is everything the caller has done anywhere, a page at a time.
//
// Unlike the per-workspace feed this one narrows to nothing at all: not to a workspace, not to projects the caller still belongs to, and not away from the four fields that feed hides. Somebody who has left a project still sees what they did there.
func (handler *Handler) myActivities(c *gin.Context, user *auth.User) {
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
		return handler.db.WithContext(c.Request.Context()).Table("issue_activities ia").
			Where("ia.actor_id = ? AND ia.deleted_at IS NULL", user.ID)
	}
	var total int64
	if err := scope().Count(&total).Error; err != nil {
		handler.internalError(c, err)
		return
	}
	window := pagination.PlanOffsetPage(perPage, cursor, int(total), 0, pagination.DefaultPerPage)

	order := sanitizeOrderBy(c.Query("order_by"), activityOrderByAllowlist, "-created_at")
	clause := "ia." + strings.TrimPrefix(order, "-")
	if strings.HasPrefix(order, "-") {
		clause += " DESC"
	} else {
		clause += " ASC"
	}
	var activities []IssueActivity
	err = scope().Select("ia.*").Order(clause).
		Offset(window.Offset).Limit(window.Stop - window.Offset).Scan(&activities).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	page := pagination.PlanOffsetPage(perPage, cursor, int(total), len(activities), pagination.DefaultPerPage)
	if len(activities) > page.Limit {
		activities = activities[:page.Limit]
	}
	serialized, err := handler.serializeActivitiesAcrossProjects(c.Request.Context(), activities)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, page.Envelope(serialized, len(serialized), nil, nil, nil))
}

// lastVisitedWorkspace is the workspace the caller was in last, with their memberships of its projects.
//
// A person who has never been in one gets an empty pair rather than a 404. The memberships are not narrowed to the active ones, so a project they were removed from is still listed.
func (handler *Handler) lastVisitedWorkspace(c *gin.Context, user *auth.User) {
	// profiles, not users: last_workspace_id is one of the twenty-six fields Django keeps on Profile, and asking users for it ends the request with `column "last_workspace_id" does not exist`. It is nullable there -- a person who has never opened a workspace has none -- which is why it is read through sql.NullString.
	var lastWorkspace []sql.NullString
	err := handler.db.WithContext(c.Request.Context()).Table("profiles").
		Where("user_id = ?", user.ID).Limit(1).Pluck("last_workspace_id", &lastWorkspace).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if len(lastWorkspace) == 0 || !lastWorkspace[0].Valid || lastWorkspace[0].String == "" {
		drf.Respond(c, http.StatusOK, gin.H{"project_details": []gin.H{}, "workspace_details": gin.H{}})
		return
	}
	workspaceID := lastWorkspace[0].String

	workspace, found, err := handler.workspaceDetails(c, workspaceID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		handler.notFound(c)
		return
	}
	memberships, err := handler.projectMemberships(c, workspaceID, user.ID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, gin.H{"project_details": memberships, "workspace_details": workspace})
}

// activityGraph is how much the caller did on each of the last six months' days.
func (handler *Handler) activityGraph(c *gin.Context, user *auth.User) {
	since := handler.clock().UTC().AddDate(0, -6, 0)
	rows, err := handler.dailyActivityCounts(c, user.ID, c.Param("slug"), since)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, rows)
}

// completedGraph is how many work items the caller finished in each week of one month.
//
// The month is read off the query string and defaults to January rather than to this month, and the weeks are the calendar week of the year taken modulo four — so two weeks nine apart share a bucket.
func (handler *Handler) completedGraph(c *gin.Context, user *auth.User) {
	month := c.DefaultQuery("month", "1")
	var rows []struct {
		Week           int   `gorm:"column:week"`
		CompletedCount int64 `gorm:"column:completed_count"`
	}
	err := handler.assignedWorkItems(c, user.ID, c.Param("slug")).
		Where("i.completed_at IS NOT NULL AND EXTRACT(MONTH FROM i.completed_at AT TIME ZONE 'UTC') = ?", month).
		Select(`(EXTRACT(WEEK FROM i.completed_at AT TIME ZONE 'UTC')::integer % 4) AS week,
			COUNT(EXTRACT(WEEK FROM i.completed_at AT TIME ZONE 'UTC')) AS completed_count`).
		// Grouped by the alias rather than by the ordinal 1: Group quotes what it is given, so "1" reaches Postgres as an identifier and the query dies with `column "1" does not exist`. Order does not quote, which is why only half of this line was wrong.
		Group("week").Order("1").Scan(&rows).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	results := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		results = append(results, gin.H{"week": row.Week, "completed_count": row.CompletedCount})
	}
	drf.Respond(c, http.StatusOK, results)
}

// workspaceDashboard is the nine numbers and lists the home screen draws.
//
// Every one of them reaches the caller's work through an inner join on the assignee link, and that join does not check whether the assignment was taken back — so a deleted assignment still counts. It also narrows to the workspace alone rather than to the projects the caller belongs to.
func (handler *Handler) workspaceDashboard(c *gin.Context, user *auth.User) {
	slug := c.Param("slug")
	now := handler.clock().UTC()

	activities, err := handler.dailyActivityCounts(c, user.ID, slug, now.AddDate(0, -3, 0))
	if err != nil {
		handler.internalError(c, err)
		return
	}

	month := c.DefaultQuery("month", "1")
	var weekly []struct {
		WeekInMonth    int   `gorm:"column:week_in_month"`
		CompletedCount int64 `gorm:"column:completed_count"`
	}
	err = handler.assignedWorkItems(c, user.ID, slug).
		Where("i.completed_at IS NOT NULL AND EXTRACT(MONTH FROM i.completed_at AT TIME ZONE 'UTC') = ?", month).
		Select(`(((EXTRACT(DAY FROM i.completed_at AT TIME ZONE 'UTC') - 1) / 7) + 1)::INTEGER AS week_in_month,
			COUNT(i.id) AS completed_count`).
		// Grouped by the alias rather than by the ordinal 1: Group quotes what it is given, so "1" reaches Postgres as an identifier and the query dies with `column "1" does not exist`. Order does not quote, which is why only half of this line was wrong.
		Group("week_in_month").Order("1").Scan(&weekly).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	completed := make([]gin.H, 0, len(weekly))
	for _, row := range weekly {
		completed = append(completed, gin.H{"week_in_month": row.WeekInMonth, "completed_count": row.CompletedCount})
	}

	counts := map[string]int64{}
	for _, entry := range []struct {
		name      string
		condition string
		arguments []any
	}{
		{name: "assigned_issues_count"},
		{name: "pending_issues_count", condition: `s."group" IS DISTINCT FROM 'completed' AND s."group" IS DISTINCT FROM 'cancelled'`},
		{name: "completed_issues_count", condition: `s."group" = ?`, arguments: []any{"completed"}},
		{name: "issues_due_week_count", condition: `EXTRACT(WEEK FROM i.target_date) = ?`, arguments: []any{isoWeek(now)}},
	} {
		query := handler.assignedWorkItems(c, user.ID, slug)
		if entry.condition != "" {
			query = query.Where(entry.condition, entry.arguments...)
		}
		var total int64
		if err := query.Count(&total).Error; err != nil {
			handler.internalError(c, err)
			return
		}
		counts[entry.name] = total
	}

	var stateRows []struct {
		StateGroup *string `gorm:"column:state_group"`
		StateCount int64   `gorm:"column:state_count"`
	}
	err = handler.assignedWorkItems(c, user.ID, slug).
		Select(`s."group" AS state_group, COUNT(s."group") AS state_count`).
		Group(`s."group"`).Order(`s."group"`).Scan(&stateRows).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	distribution := make([]gin.H, 0, len(stateRows))
	for _, row := range stateRows {
		distribution = append(distribution, gin.H{"state_group": row.StateGroup, "state_count": row.StateCount})
	}

	overdue, err := handler.dashboardWorkItems(c, user.ID, slug, "target_date",
		"i.completed_at IS NULL AND i.target_date < ?", now)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	upcoming, err := handler.dashboardWorkItems(c, user.ID, slug, "start_date",
		"i.completed_at IS NULL AND i.start_date >= ?", now)
	if err != nil {
		handler.internalError(c, err)
		return
	}

	drf.Respond(c, http.StatusOK, gin.H{
		"issue_activities": activities, "completed_issues": completed,
		"assigned_issues_count":  counts["assigned_issues_count"],
		"pending_issues_count":   counts["pending_issues_count"],
		"completed_issues_count": counts["completed_issues_count"],
		"issues_due_week_count":  counts["issues_due_week_count"],
		"state_distribution":     distribution,
		"overdue_issues":         overdue, "upcoming_issues": upcoming,
	})
}

// dashboardWorkItems is the two lists at the bottom of the dashboard, which carry five fields each and differ only in which date they report.
func (handler *Handler) dashboardWorkItems(c *gin.Context, userID, slug, dateField, condition string, argument any) ([]gin.H, error) {
	var rows []struct {
		ID        string     `gorm:"column:id"`
		Name      string     `gorm:"column:name"`
		Slug      string     `gorm:"column:workspace__slug"`
		ProjectID string     `gorm:"column:project_id"`
		Date      *time.Time `gorm:"column:the_date"`
	}
	err := handler.assignedWorkItems(c, userID, slug).
		Where(`s."group" IS DISTINCT FROM 'completed' AND s."group" IS DISTINCT FROM 'cancelled'`).
		Where(condition, argument).
		Select("i.id, i.name, w.slug AS workspace__slug, i.project_id, i." + dateField + " AS the_date").
		Order("i.created_at DESC").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	results := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		results = append(results, gin.H{
			"id": row.ID, "name": row.Name, "workspace__slug": row.Slug,
			"project_id": row.ProjectID, dateField: dateOnly(row.Date),
		})
	}
	return results, nil
}

// assignedWorkItems is the queryset every dashboard number starts from: the caller's work items in this workspace, read through the issue_objects manager.
func (handler *Handler) assignedWorkItems(c *gin.Context, userID, slug string) *gorm.DB {
	return handler.db.WithContext(c.Request.Context()).Table("issues i").
		Joins(`LEFT JOIN states s ON s.id = i.state_id`).
		Joins("JOIN projects p ON p.id = i.project_id").
		Joins("JOIN issue_assignees ia ON ia.issue_id = i.id").
		Joins("JOIN workspaces w ON w.id = i.workspace_id").
		Where(`i.deleted_at IS NULL AND s."group" IS DISTINCT FROM 'triage' AND i.archived_at IS NULL
			AND p.archived_at IS NULL AND i.is_draft = FALSE
			AND ia.assignee_id = ? AND w.slug = ?`, userID, slug)
}

// dailyActivityCounts is the activity graph both routes draw, which differ only in how far back they look.
func (handler *Handler) dailyActivityCounts(c *gin.Context, userID, slug string, since time.Time) ([]gin.H, error) {
	var rows []struct {
		CreatedDate   time.Time `gorm:"column:created_date"`
		ActivityCount int64     `gorm:"column:activity_count"`
	}
	err := handler.db.WithContext(c.Request.Context()).Table("issue_activities ia").
		Joins("JOIN workspaces w ON w.id = ia.workspace_id").
		Where(`ia.deleted_at IS NULL AND ia.actor_id = ? AND w.slug = ?
			AND (ia.created_at AT TIME ZONE 'UTC')::date >= ?`, userID, slug, since.Format("2006-01-02")).
		Select(`ia.created_at::date AS created_date, COUNT(ia.created_at::date) AS activity_count`).
		// Grouped by the alias rather than by the ordinal 1: Group quotes what it is given, so "1" reaches Postgres as an identifier and the query dies with `column "1" does not exist`. Order does not quote, which is why only half of this line was wrong.
		Group("created_date").Order("1").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	results := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		results = append(results, gin.H{
			"created_date": row.CreatedDate.Format("2006-01-02"), "activity_count": row.ActivityCount,
		})
	}
	return results, nil
}

// workspaceDetails is WorkSpaceSerializer over a plain row, which is fifteen fields — the two annotations it also declares are simply absent when nothing annotated them.
func (handler *Handler) workspaceDetails(c *gin.Context, workspaceID string) (gin.H, bool, error) {
	var rows []struct {
		ID               string     `gorm:"column:id"`
		CreatedAt        time.Time  `gorm:"column:created_at"`
		UpdatedAt        time.Time  `gorm:"column:updated_at"`
		DeletedAt        *time.Time `gorm:"column:deleted_at"`
		Name             string     `gorm:"column:name"`
		Logo             *string    `gorm:"column:logo"`
		Slug             string     `gorm:"column:slug"`
		OrganizationSize string     `gorm:"column:organization_size"`
		Timezone         string     `gorm:"column:timezone"`
		BackgroundColor  *string    `gorm:"column:background_color"`
		CreatedByID      *string    `gorm:"column:created_by_id"`
		UpdatedByID      *string    `gorm:"column:updated_by_id"`
		LogoAssetID      *string    `gorm:"column:logo_asset_id"`
		OwnerID          *string    `gorm:"column:owner_id"`
	}
	err := handler.db.WithContext(c.Request.Context()).Table("workspaces").
		Where("id = ? AND deleted_at IS NULL", workspaceID).Limit(1).Scan(&rows).Error
	if err != nil || len(rows) == 0 {
		return nil, false, err
	}
	row := rows[0]
	return gin.H{
		"id": row.ID, "logo_url": staticAssetURL(row.LogoAssetID, stringOrBlank(row.Logo)),
		"created_at": row.CreatedAt, "updated_at": row.UpdatedAt, "deleted_at": row.DeletedAt,
		"name": row.Name, "logo": row.Logo, "slug": row.Slug,
		"organization_size": row.OrganizationSize, "timezone": row.Timezone,
		"background_color": row.BackgroundColor,
		"created_by":       row.CreatedByID, "updated_by": row.UpdatedByID,
		"logo_asset": row.LogoAssetID, "owner": row.OwnerID,
	}, true, nil
}

// projectMemberships is ProjectMemberSerializer: sixteen fields with three nested objects, and no narrowing to the active memberships.
func (handler *Handler) projectMemberships(c *gin.Context, workspaceID, userID string) ([]gin.H, error) {
	var rows []ProjectMember
	err := handler.db.WithContext(c.Request.Context()).Table("project_members").
		Where("workspace_id = ? AND member_id = ? AND deleted_at IS NULL", workspaceID, userID).
		Order("created_at DESC").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	results := make([]gin.H, 0, len(rows))
	workspace, err := handler.workspaceLiteJSON(c.Request.Context(), workspaceID)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		project, err := handler.projectLiteJSON(c.Request.Context(), row.ProjectID)
		if err != nil {
			return nil, err
		}
		member, err := handler.liteMember(c.Request.Context(), row.MemberID)
		if err != nil {
			return nil, err
		}
		results = append(results, gin.H{
			"id": row.ID, "workspace": workspace, "project": project, "member": member,
			"created_at": row.CreatedAt, "updated_at": row.UpdatedAt, "deleted_at": row.DeletedAt,
			"comment": row.Comment, "role": row.Role,
			"view_props": decodeJSON(row.ViewProps), "default_props": decodeJSON(row.DefaultProps),
			"preferences": decodeJSON(row.Preferences), "sort_order": row.SortOrder,
			"is_active":  row.IsActive,
			"created_by": row.CreatedByID, "updated_by": row.UpdatedByID,
		})
	}
	return results, nil
}

// isoWeek is the calendar week the dashboard compares target dates against.
func isoWeek(moment time.Time) int {
	_, week := moment.ISOWeek()
	return week
}

// liteMember reads one person through UserLiteSerializer, which is what the nested member of a membership is.
func (handler *Handler) liteMember(ctx context.Context, memberID string) (gin.H, error) {
	var people []auth.User
	if err := handler.db.WithContext(ctx).Where("id = ?", memberID).Limit(1).Find(&people).Error; err != nil {
		return nil, err
	}
	if len(people) == 0 {
		return nil, nil
	}
	return liteUserJSON(people[0], false), nil
}
