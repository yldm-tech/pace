package project

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"github.com/yldm-tech/pace/apps/api-go/internal/drf"
	"github.com/yldm-tech/pace/apps/api-go/internal/pagination"
	"gorm.io/gorm"
)

// The two ways these routes reach a 500, named so the log says which one it was.
var (
	errNoSuchFilterField     = errors.New("workspace user stats: a filter names a field the schema does not have")
	errStatsSubscriberFilter = errors.New("workspace user stats: the subscribed count applies work item filters to IssueSubscriber, which has none of those fields")
)

// These six routes are what one person's corner of a workspace looks like: their profile, their numbers, what they have been doing, and the projects and pages they were last in.
func (handler *Handler) registerWorkspaceUserRoutes(router gin.IRouter) {
	router.GET("/api/workspaces/:slug/user-profile/:user/", handler.authenticated(handler.workspaceUserProfile))
	router.GET("/api/workspaces/:slug/user-stats/:user/", handler.authenticated(handler.workspaceUserStats))
	router.GET("/api/workspaces/:slug/user-activity/:user/", handler.authenticated(handler.workspaceUserActivity))
	router.POST("/api/workspaces/:slug/user-activity/:user/export/", handler.authenticated(handler.workspaceUserActivityExport))
	router.GET("/api/workspaces/:slug/recent-visits/", handler.authenticated(handler.workspaceRecentVisits))
	router.GET("/api/workspaces/:slug/project-members/", handler.authenticated(handler.workspaceProjectMembers))
}

// workspaceUserProfile reports what one person has in each project the caller can see.
//
// It guards nothing but the session. What stands in for a permission is the pair of lookups it opens with: the caller has to be an active member of the workspace and so does the person being asked about, and either one missing is a 404 rather than a 403.
func (handler *Handler) workspaceUserProfile(c *gin.Context, user *auth.User) {
	slug, target := c.Param("slug"), c.Param("user")
	callerRole, found, err := handler.activeWorkspaceRole(c.Request.Context(), slug, user.ID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		handler.notFound(c)
		return
	}
	profile, found, err := handler.workspaceMemberProfile(c.Request.Context(), slug, target)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		handler.notFound(c)
		return
	}

	// A guest sees the person but none of the numbers, because the project list is only built from member level up.
	projects := []gin.H{}
	if callerRole >= roleMember {
		projects, err = handler.workspaceUserProjectCounts(c.Request.Context(), slug, user.ID, target)
		if err != nil {
			handler.internalError(c, err)
			return
		}
	}
	drf.Respond(c, http.StatusOK, gin.H{"project_data": projects, "user_data": profile})
}

// workspaceUserProjectCounts is the four annotated counts, which are counted over one joined row set rather than four.
//
// That is why created_issues is larger than the number of work items somebody raised whenever any of them has more than one assignee: the assignee join is added for the other three counts, and all four then count rows of the join rather than work items. Reproduced rather than corrected — the numbers on the profile page are the numbers this query returns.
//
// The joins are also the plain ones Django writes for a related lookup, which do not apply the soft-delete manager. A deleted work item still counts.
func (handler *Handler) workspaceUserProjectCounts(ctx context.Context, slug, callerID, targetID string) ([]gin.H, error) {
	const counted = `p.id, p.logo_props,
		COUNT(i.id) FILTER (WHERE i.archived_at IS NULL AND i.created_by_id = ? AND NOT i.is_draft) AS created_issues,
		COUNT(i.id) FILTER (WHERE i.archived_at IS NULL AND ia.assignee_id = ? AND NOT i.is_draft) AS assigned_issues,
		COUNT(i.id) FILTER (WHERE i.archived_at IS NULL AND ia.assignee_id = ? AND i.completed_at IS NOT NULL AND NOT i.is_draft) AS completed_issues,
		COUNT(i.id) FILTER (WHERE i.archived_at IS NULL AND ia.assignee_id = ? AND NOT i.is_draft AND s.group IN ('backlog','unstarted','started')) AS pending_issues`
	var rows []struct {
		ID              string         `gorm:"column:id"`
		LogoProps       auth.JSONValue `gorm:"column:logo_props"`
		CreatedIssues   int64          `gorm:"column:created_issues"`
		AssignedIssues  int64          `gorm:"column:assigned_issues"`
		CompletedIssues int64          `gorm:"column:completed_issues"`
		PendingIssues   int64          `gorm:"column:pending_issues"`
	}
	err := handler.db.WithContext(ctx).Table("projects p").
		Select(counted, targetID, targetID, targetID, targetID).
		Joins("JOIN project_members pm ON pm.project_id = p.id").
		Joins("JOIN workspaces w ON w.id = p.workspace_id").
		Joins("LEFT JOIN issues i ON i.project_id = p.id").
		Joins("LEFT JOIN issue_assignees ia ON ia.issue_id = i.id").
		Joins("LEFT JOIN states s ON s.id = i.state_id").
		Where("p.deleted_at IS NULL AND p.archived_at IS NULL AND pm.is_active = TRUE AND pm.member_id = ? AND w.slug = ?", callerID, slug).
		Group("p.id").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	projects := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		projects = append(projects, gin.H{
			"id": row.ID, "logo_props": decodeJSON(row.LogoProps),
			"created_issues": row.CreatedIssues, "assigned_issues": row.AssignedIssues,
			"completed_issues": row.CompletedIssues, "pending_issues": row.PendingIssues,
		})
	}
	return projects, nil
}

// workspaceUserStats is the profile page's numbers: two distributions, five counts and the cycles the person is working through.
//
// Every one of them narrows to work items in projects the **caller** belongs to, so two people looking at the same person's profile can see different totals.
func (handler *Handler) workspaceUserStats(c *gin.Context, user *auth.User) {
	slug, target := c.Param("slug"), c.Param("user")
	filters := issueFilters(queryParams(c), "GET", "", handler.clock().UTC())
	joins, conditions, arguments, translatable := issueFilterSQL(filters)
	if !translatable {
		handler.internalError(c, errNoSuchFilterField)
		return
	}
	if len(filters) > 0 {
		// The subscribed count applies the work item filters to IssueSubscriber, which has none of those fields. Django raises FieldError, so any filter at all makes the whole route a 500 — and it is the whole route, because the count is computed before the response is built.
		handler.internalError(c, errStatsSubscriberFilter)
		return
	}

	assigned := func() *gormScope {
		return handler.statsScope(c, slug, target, user.ID, true, joins, conditions, arguments)
	}

	var stateRows []struct {
		StateGroup *string `gorm:"column:state_group"`
		StateCount int64   `gorm:"column:state_count"`
	}
	err := assigned().query.Select(`s.group AS state_group, COUNT(s.group) AS state_count`).
		Group("s.group").Order("s.group ASC").Scan(&stateRows).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	stateDistribution := make([]gin.H, 0, len(stateRows))
	for _, row := range stateRows {
		stateDistribution = append(stateDistribution, gin.H{"state_group": row.StateGroup, "state_count": row.StateCount})
	}

	var priorityRows []struct {
		Priority      string `gorm:"column:priority"`
		PriorityCount int64  `gorm:"column:priority_count"`
		PriorityOrder int    `gorm:"column:priority_order"`
	}
	err = assigned().query.Select(`i.priority, COUNT(i.priority) AS priority_count, ` + priorityOrderCase).
		Group("i.priority, " + priorityOrderCase).
		Having("COUNT(i.priority) >= 1").
		Order(priorityOrderCase + " ASC").Scan(&priorityRows).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	priorityDistribution := make([]gin.H, 0, len(priorityRows))
	for _, row := range priorityRows {
		priorityDistribution = append(priorityDistribution, gin.H{
			"priority": row.Priority, "priority_count": row.PriorityCount, "priority_order": row.PriorityOrder,
		})
	}

	// The created count is the one that does not go through the assignee join, since it asks who raised the work item rather than who has it.
	createdIssues, err := handler.statsCount(handler.statsScope(c, slug, target, user.ID, false, joins, conditions, arguments).
		query.Where("i.created_by_id = ?", target))
	if err != nil {
		handler.internalError(c, err)
		return
	}
	assignedIssues, err := handler.statsCount(assigned().query)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	pendingIssues, err := handler.statsCount(assigned().query.Where("s.group IS DISTINCT FROM 'completed' AND s.group IS DISTINCT FROM 'cancelled'"))
	if err != nil {
		handler.internalError(c, err)
		return
	}
	completedIssues, err := handler.statsCount(assigned().query.Where("s.group = ?", "completed"))
	if err != nil {
		handler.internalError(c, err)
		return
	}

	var subscribed int64
	err = handler.db.WithContext(c.Request.Context()).Table("issue_subscribers isub").
		Joins("JOIN projects p ON p.id = isub.project_id").
		Joins("JOIN project_members pm ON pm.project_id = p.id").
		Joins("JOIN workspaces w ON w.id = isub.workspace_id").
		Where(`isub.deleted_at IS NULL AND p.archived_at IS NULL AND pm.is_active = TRUE
			AND pm.member_id = ? AND isub.subscriber_id = ? AND w.slug = ?`, user.ID, target, slug).
		Count(&subscribed).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}

	now := handler.clock().UTC()
	upcoming, err := handler.statsCycles(c, slug, target, "c.start_date > ?", now)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	present, err := handler.statsCycles(c, slug, target, "c.start_date < ? AND c.end_date > ?", now, now)
	if err != nil {
		handler.internalError(c, err)
		return
	}

	drf.Respond(c, http.StatusOK, gin.H{
		"state_distribution": stateDistribution, "priority_distribution": priorityDistribution,
		"created_issues": createdIssues, "assigned_issues": assignedIssues,
		"completed_issues": completedIssues, "pending_issues": pendingIssues,
		"subscribed_issues": subscribed,
		"present_cycles":    present, "upcoming_cycles": upcoming,
	})
}

// priorityOrderCase is the Case expression that sorts the five priorities into their own order rather than alphabetically.
const priorityOrderCase = `CASE WHEN i.priority = 'urgent' THEN 0 WHEN i.priority = 'high' THEN 1 WHEN i.priority = 'medium' THEN 2 WHEN i.priority = 'low' THEN 3 WHEN i.priority = 'none' THEN 4 ELSE 5 END`

// statsCycles reads the cycles the person's work items sit in. The rows are not made distinct, so a person with three work items in one cycle sees that cycle three times — which is what the endpoint returns today.
func (handler *Handler) statsCycles(c *gin.Context, slug, target, window string, arguments ...any) ([]gin.H, error) {
	var rows []struct {
		Name      string `gorm:"column:cycle__name"`
		ID        string `gorm:"column:cycle__id"`
		ProjectID string `gorm:"column:cycle__project_id"`
	}
	query := handler.db.WithContext(c.Request.Context()).Table("cycle_issues ci").
		Select(`c.name AS cycle__name, ci.cycle_id AS cycle__id, c.project_id AS cycle__project_id`).
		Joins("JOIN cycles c ON c.id = ci.cycle_id").
		Joins("JOIN issues i ON i.id = ci.issue_id").
		Joins("JOIN issue_assignees ia ON ia.issue_id = i.id").
		Joins("JOIN workspaces w ON w.id = ci.workspace_id").
		Where("ci.deleted_at IS NULL AND ia.assignee_id = ? AND w.slug = ?", target, slug).
		Where(window, arguments...).
		Order("ci.created_at DESC")
	if err := query.Scan(&rows).Error; err != nil {
		return nil, err
	}
	cycles := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		cycles = append(cycles, gin.H{
			"cycle__name": row.Name, "cycle__id": row.ID, "cycle__project_id": row.ProjectID,
		})
	}
	return cycles, nil
}

// gormScope carries the query so each statistic can add its own conditions without the callers sharing one builder.
type gormScope struct{ query *gorm.DB }

// statsScope is the queryset every statistic starts from: the issue_objects manager, the workspace, and the rule that the caller is an active member of the work item's project.
func (handler *Handler) statsScope(c *gin.Context, slug, target, callerID string, byAssignee bool, joins, conditions []string, arguments []any) *gormScope {
	query := handler.db.WithContext(c.Request.Context()).Table("issues i").
		Joins("LEFT JOIN states s ON s.id = i.state_id").
		Joins("JOIN projects p ON p.id = i.project_id").
		Joins("JOIN project_members pm ON pm.project_id = p.id").
		Joins("JOIN workspaces w ON w.id = i.workspace_id").
		Where(`i.deleted_at IS NULL AND s.group IS DISTINCT FROM 'triage' AND i.archived_at IS NULL
			AND p.archived_at IS NULL AND i.is_draft = FALSE
			AND pm.is_active = TRUE AND pm.member_id = ? AND w.slug = ?`, callerID, slug)
	if byAssignee {
		query = query.Joins("JOIN issue_assignees ia ON ia.issue_id = i.id").
			Where("ia.assignee_id = ? AND ia.deleted_at IS NULL", target)
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
	return &gormScope{query: query}
}

// statsCount counts rows of the join rather than work items, because Django's .count() over these querysets does the same.
func (handler *Handler) statsCount(query *gorm.DB) (int64, error) {
	var total int64
	err := query.Count(&total).Error
	return total, err
}

// workspaceUserActivity is what one person has been doing across the workspace, a page at a time.
func (handler *Handler) workspaceUserActivity(c *gin.Context, user *auth.User) {
	if !handler.requireWorkspaceRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	slug, target := c.Param("slug"), c.Param("user")
	query := handler.workspaceActivityScope(c, slug, target, user.ID)
	if projects := c.QueryArray("project"); len(projects) > 0 {
		query = query.Where("ia.project_id IN ?", projects)
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

	var total int64
	if err := query.Session(&gorm.Session{}).Count(&total).Error; err != nil {
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
	err = query.Session(&gorm.Session{}).Select("ia.*").Order(clause).
		Offset(window.Offset).Limit(window.Stop - window.Offset).Scan(&activities).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	page := pagination.PlanOffsetPage(perPage, cursor, int(total), len(activities), pagination.DefaultPerPage)
	if len(activities) > page.Limit {
		activities = activities[:page.Limit]
	}
	// The activities span projects here rather than sitting in one, so each row's project and workspace are looked up for it.
	serialized, err := handler.serializeActivitiesAcrossProjects(c.Request.Context(), activities)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, page.Envelope(serialized, len(serialized), nil, nil, nil))
}

// activityOrderByAllowlist is ACTIVITY_ORDER_BY_ALLOWLIST: two fields and nothing else, so an ordering the caller invents falls back to newest first rather than reaching the database.
var activityOrderByAllowlist = map[string]bool{"created_at": true, "updated_at": true}

// workspaceUserActivityExport answers with a csv of one day's activity rather than with json.
func (handler *Handler) workspaceUserActivityExport(c *gin.Context, user *auth.User) {
	if !handler.requireWorkspaceRole(c, user, roleAdmin, roleMember) {
		return
	}
	var body map[string]json.RawMessage
	if err := c.ShouldBindJSON(&body); err != nil {
		handler.invalidDetail(c)
		return
	}
	raw, given := body["date"]
	if !given || string(raw) == "null" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Date is required"})
		return
	}
	var day string
	if json.Unmarshal(raw, &day) != nil || day == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Date is required"})
		return
	}

	slug, target := c.Param("slug"), c.Param("user")
	// The archived-project rule the json route applies is not here: the export reads activity from archived projects too.
	query := handler.db.WithContext(c.Request.Context()).Table("issue_activities ia").
		Select(`ia.*`).
		Joins("JOIN projects p ON p.id = ia.project_id").
		Joins("JOIN project_members pm ON pm.project_id = p.id").
		Joins("JOIN workspaces w ON w.id = ia.workspace_id").
		Where(`ia.deleted_at IS NULL AND w.slug = ? AND pm.is_active = TRUE AND pm.member_id = ?
			AND ia.actor_id = ? AND (ia.created_at AT TIME ZONE 'UTC')::date = ?
			AND (ia.field IS NULL OR ia.field NOT IN ?)`, slug, user.ID, target, day, hiddenActivityFields).
		Limit(10000)
	var activities []IssueActivity
	if err := query.Scan(&activities).Error; err != nil {
		handler.internalError(c, err)
		return
	}
	rows, err := handler.activityExportRows(c.Request.Context(), activities)
	if err != nil {
		handler.internalError(c, err)
		return
	}

	c.Header("Content-Disposition", `attachment; filename="workspace-user-activity.csv"`)
	c.Header("Content-Type", "text/csv")
	c.Status(http.StatusOK)
	writer := csv.NewWriter(c.Writer)
	// Every field is quoted, which is what csv.QUOTE_ALL does and what keeps a value that looks like a formula from being read as one.
	for _, row := range rows {
		quoted := make([]string, 0, len(row))
		for _, value := range row {
			quoted = append(quoted, sanitizeCSVCell(value))
		}
		if err := writer.Write(quoted); err != nil {
			return
		}
	}
	writer.Flush()
}

// activityExportRows is the header plus one row per activity, with the work item written as its project's identifier and its number.
func (handler *Handler) activityExportRows(ctx context.Context, activities []IssueActivity) ([][]string, error) {
	rows := [][]string{{
		"Actor name", "Issue ID", "Project", "Created at", "Updated at",
		"Action", "Field", "Old value", "New value",
	}}
	if len(activities) == 0 {
		return rows, nil
	}
	actors, err := handler.activityActorNames(ctx, activities)
	if err != nil {
		return nil, err
	}
	projects, err := handler.activityProjectNames(ctx, activities)
	if err != nil {
		return nil, err
	}
	sequences, err := handler.activityIssueSequences(ctx, activities)
	if err != nil {
		return nil, err
	}
	for _, activity := range activities {
		project := projects[activity.ProjectID]
		sequence := ""
		if activity.IssueID != nil {
			sequence = sequences[*activity.IssueID]
		}
		actor := ""
		if activity.ActorID != nil {
			actor = actors[*activity.ActorID]
		}
		rows = append(rows, []string{
			actor,
			project.identifier + " - " + sequence,
			project.name,
			activity.CreatedAt.Format(time.RFC3339Nano),
			activity.UpdatedAt.Format(time.RFC3339Nano),
			activity.Verb,
			stringOrBlank(activity.Field),
			stringOrBlank(activity.OldValue),
			stringOrBlank(activity.NewValue),
		})
	}
	return rows, nil
}

// sanitizeCSVCell is sanitize_csv_row: a value that opens with one of the four characters a spreadsheet reads as a formula is prefixed with a quote so it is read as text.
func sanitizeCSVCell(value string) string {
	if value == "" {
		return value
	}
	switch value[0] {
	case '=', '+', '-', '@':
		return "'" + value
	}
	return value
}

func stringOrBlank(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// workspaceActivityScope is the activity queryset both routes share, minus the day the export narrows to.
func (handler *Handler) workspaceActivityScope(c *gin.Context, slug, target, callerID string) *gorm.DB {
	return handler.db.WithContext(c.Request.Context()).Table("issue_activities ia").
		Joins("JOIN projects p ON p.id = ia.project_id").
		Joins("JOIN project_members pm ON pm.project_id = p.id").
		Joins("JOIN workspaces w ON w.id = ia.workspace_id").
		Where(`ia.deleted_at IS NULL AND w.slug = ? AND pm.is_active = TRUE AND pm.member_id = ?
			AND p.archived_at IS NULL AND ia.actor_id = ?
			AND (ia.field IS NULL OR ia.field NOT IN ?)`, slug, callerID, target, hiddenActivityFields)
}

// serializeActivitiesAcrossProjects is IssueActivitySerializer over rows that do not share a project, which is the difference from the per-work-item history.
func (handler *Handler) serializeActivitiesAcrossProjects(ctx context.Context, activities []IssueActivity) ([]gin.H, error) {
	serialized := make([]gin.H, 0, len(activities))
	if len(activities) == 0 {
		return serialized, nil
	}
	actors, err := handler.activityActors(ctx, activities)
	if err != nil {
		return nil, err
	}
	issues, err := handler.activityIssues(ctx, activities)
	if err != nil {
		return nil, err
	}
	projects := map[string]gin.H{}
	workspaces := map[string]gin.H{}
	for _, activity := range activities {
		if _, known := projects[activity.ProjectID]; !known {
			project, err := handler.projectLiteJSON(ctx, activity.ProjectID)
			if err != nil {
				return nil, err
			}
			projects[activity.ProjectID] = project
		}
		if _, known := workspaces[activity.WorkspaceID]; !known {
			workspace, err := handler.workspaceLiteJSON(ctx, activity.WorkspaceID)
			if err != nil {
				return nil, err
			}
			workspaces[activity.WorkspaceID] = workspace
		}
	}
	for _, activity := range activities {
		// This route does not prefetch the intake row, so source_data is null on every one of these.
		serialized = append(serialized, handler.activityJSON(activity,
			projects[activity.ProjectID], workspaces[activity.WorkspaceID], actors, issues, nil))
	}
	return serialized, nil
}

// workspaceRecentVisits is the last twenty things the caller opened, with enough of each one to draw a row for it.
func (handler *Handler) workspaceRecentVisits(c *gin.Context, user *auth.User) {
	if !handler.requireWorkspaceRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	type visitRow struct {
		ID               string    `gorm:"column:id"`
		EntityName       string    `gorm:"column:entity_name"`
		EntityIdentifier *string   `gorm:"column:entity_identifier"`
		VisitedAt        time.Time `gorm:"column:visited_at"`
	}
	query := handler.db.WithContext(c.Request.Context()).Table("user_recent_visits v").
		Select("v.id, v.entity_name, v.entity_identifier, v.visited_at").
		Joins("JOIN workspaces w ON w.id = v.workspace_id").
		Where("w.slug = ? AND v.user_id = ? AND v.deleted_at IS NULL", c.Param("slug"), user.ID)
	if name := c.Query("entity_name"); name != "" {
		query = query.Where("v.entity_name = ?", name)
	}
	var rows []visitRow
	err := query.Where("v.entity_name IN ?", []string{"issue", "page", "project"}).
		Order("v.created_at DESC").Limit(20).Scan(&rows).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}

	results := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		entity, err := handler.recentVisitEntity(c.Request.Context(), row.EntityName, row.EntityIdentifier)
		if err != nil {
			handler.internalError(c, err)
			return
		}
		results = append(results, gin.H{
			"id": row.ID, "entity_name": row.EntityName, "entity_identifier": row.EntityIdentifier,
			"entity_data": entity, "visited_at": row.VisitedAt,
		})
	}
	drf.Respond(c, http.StatusOK, results)
}

// recentVisitEntity reads the thing that was visited. A row pointing at something that has since gone renders as null rather than dropping out of the list.
func (handler *Handler) recentVisitEntity(ctx context.Context, name string, identifier *string) (gin.H, error) {
	if identifier == nil {
		return nil, nil
	}
	switch name {
	case "issue":
		return handler.recentVisitIssue(ctx, *identifier)
	case "page":
		return handler.recentVisitPage(ctx, *identifier)
	case "project":
		return handler.recentVisitProject(ctx, *identifier)
	}
	return nil, nil
}

// recentVisitIssue reads a work item through the plain manager rather than issue_objects, so an archived or draft work item is still described.
func (handler *Handler) recentVisitIssue(ctx context.Context, identifier string) (gin.H, error) {
	var rows []struct {
		ID                string  `gorm:"column:id"`
		Name              string  `gorm:"column:name"`
		StateID           *string `gorm:"column:state_id"`
		Priority          string  `gorm:"column:priority"`
		TypeID            *string `gorm:"column:type_id"`
		SequenceID        int     `gorm:"column:sequence_id"`
		ProjectID         string  `gorm:"column:project_id"`
		ProjectIdentifier *string `gorm:"column:project_identifier"`
	}
	err := handler.db.WithContext(ctx).Table("issues i").
		Select(`i.id, i.name, i.state_id, i.priority, i.type_id, i.sequence_id, i.project_id,
			(SELECT p.identifier FROM projects p WHERE p.id = i.project_id) AS project_identifier`).
		Where("i.id = ? AND i.deleted_at IS NULL", identifier).Limit(1).Scan(&rows).Error
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	row := rows[0]
	assignees := []string{}
	err = handler.db.WithContext(ctx).Table("issue_assignees").
		Where("issue_id = ? AND deleted_at IS NULL", identifier).
		Pluck("assignee_id", &assignees).Error
	if err != nil {
		return nil, err
	}
	return gin.H{
		"id": row.ID, "name": row.Name, "state": row.StateID, "priority": row.Priority,
		"assignees": assignees, "type": row.TypeID, "sequence_id": row.SequenceID,
		"project_id": row.ProjectID, "project_identifier": row.ProjectIdentifier,
	}, nil
}

// recentVisitPage reads a page. A page belongs to its projects through a join table, and both project fields take whichever project comes first — newest first, which is the model's own ordering.
func (handler *Handler) recentVisitPage(ctx context.Context, identifier string) (gin.H, error) {
	var rows []struct {
		ID        string         `gorm:"column:id"`
		Name      *string        `gorm:"column:name"`
		LogoProps auth.JSONValue `gorm:"column:logo_props"`
		OwnedByID *string        `gorm:"column:owned_by_id"`
	}
	err := handler.db.WithContext(ctx).Table("pages").
		Select("id, name, logo_props, owned_by_id").
		Where("id = ? AND deleted_at IS NULL", identifier).Limit(1).Scan(&rows).Error
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	row := rows[0]
	var owners []struct {
		ID         string `gorm:"column:id"`
		Identifier string `gorm:"column:identifier"`
	}
	err = handler.db.WithContext(ctx).Table("project_pages pp").
		Select("p.id, p.identifier").
		Joins("JOIN projects p ON p.id = pp.project_id").
		Where("pp.page_id = ? AND pp.deleted_at IS NULL AND p.deleted_at IS NULL", identifier).
		Order("p.created_at DESC").Limit(1).Scan(&owners).Error
	if err != nil {
		return nil, err
	}
	var projectID, projectIdentifier any
	if len(owners) > 0 {
		projectID, projectIdentifier = owners[0].ID, owners[0].Identifier
	}
	return gin.H{
		"id": row.ID, "name": row.Name, "logo_props": decodeJSON(row.LogoProps),
		"project_id": projectID, "owned_by": row.OwnedByID, "project_identifier": projectIdentifier,
	}, nil
}

func (handler *Handler) recentVisitProject(ctx context.Context, identifier string) (gin.H, error) {
	var rows []struct {
		ID         string         `gorm:"column:id"`
		Name       string         `gorm:"column:name"`
		LogoProps  auth.JSONValue `gorm:"column:logo_props"`
		Identifier string         `gorm:"column:identifier"`
	}
	err := handler.db.WithContext(ctx).Table("projects").
		Select("id, name, logo_props, identifier").
		Where("id = ? AND deleted_at IS NULL", identifier).Limit(1).Scan(&rows).Error
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	members := []string{}
	err = handler.db.WithContext(ctx).Table("project_members pm").
		Joins("JOIN users u ON u.id = pm.member_id").
		Where("pm.project_id = ? AND pm.is_active = TRUE AND pm.deleted_at IS NULL AND u.is_bot = FALSE", identifier).
		Order("pm.created_at DESC").Pluck("pm.member_id", &members).Error
	if err != nil {
		return nil, err
	}
	row := rows[0]
	return gin.H{
		"id": row.ID, "name": row.Name, "logo_props": decodeJSON(row.LogoProps),
		"project_members": members, "identifier": row.Identifier,
	}, nil
}

// workspaceProjectMembers is who is in each project the caller belongs to, keyed by project.
//
// The projects are chosen by the caller's membership **anywhere**, not in this workspace, and only then narrowed to the workspace the url names. Two workspaces cannot share a project, so the extra breadth changes nothing — but it is why the query reads the way it does.
func (handler *Handler) workspaceProjectMembers(c *gin.Context, user *auth.User) {
	if !handler.requireWorkspaceRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	var rows []struct {
		ID        string    `gorm:"column:id"`
		Role      int       `gorm:"column:role"`
		MemberID  string    `gorm:"column:member_id"`
		ProjectID string    `gorm:"column:project_id"`
		CreatedAt time.Time `gorm:"column:created_at"`
	}
	err := handler.db.WithContext(c.Request.Context()).Table("project_members pm").
		Select("pm.id, pm.role, pm.member_id, pm.project_id, pm.created_at").
		Joins("JOIN workspaces w ON w.id = pm.workspace_id").
		Where(`w.slug = ? AND pm.is_active = TRUE AND pm.deleted_at IS NULL AND pm.project_id IN (
			SELECT mine.project_id FROM project_members mine
			WHERE mine.member_id = ? AND mine.is_active = TRUE AND mine.deleted_at IS NULL)`,
			c.Param("slug"), user.ID).
		Order("pm.created_at DESC").Scan(&rows).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	grouped := gin.H{}
	for _, row := range rows {
		// The project is the key rather than a field, so it is taken off the row on the way into the bucket.
		member := gin.H{
			"id": row.ID, "role": row.Role, "member": row.MemberID,
			"original_role": row.Role, "created_at": row.CreatedAt,
		}
		existing, _ := grouped[row.ProjectID].([]gin.H)
		grouped[row.ProjectID] = append(existing, member)
	}
	drf.Respond(c, http.StatusOK, grouped)
}

// activeWorkspaceRole reads the caller's role, and reports whether they are a member at all.
func (handler *Handler) activeWorkspaceRole(ctx context.Context, slug, userID string) (int, bool, error) {
	var roles []int
	err := handler.db.WithContext(ctx).Table("workspace_members wm").
		Joins("JOIN workspaces w ON w.id = wm.workspace_id").
		Where("w.slug = ? AND wm.member_id = ? AND wm.is_active = TRUE AND wm.deleted_at IS NULL", slug, userID).
		Limit(1).Pluck("wm.role", &roles).Error
	if err != nil || len(roles) == 0 {
		return 0, false, err
	}
	return roles[0], true, nil
}

// workspaceMemberProfile is the eight fields the profile reports about a person, which stop short of anything an account needs.
func (handler *Handler) workspaceMemberProfile(ctx context.Context, slug, memberID string) (gin.H, bool, error) {
	var rows []struct {
		Email         string    `gorm:"column:email"`
		FirstName     string    `gorm:"column:first_name"`
		LastName      string    `gorm:"column:last_name"`
		Avatar        string    `gorm:"column:avatar"`
		AvatarAssetID *string   `gorm:"column:avatar_asset_id"`
		CoverImage    *string   `gorm:"column:cover_image"`
		CoverAssetID  *string   `gorm:"column:cover_image_asset_id"`
		DateJoined    time.Time `gorm:"column:date_joined"`
		UserTimezone  string    `gorm:"column:user_timezone"`
		DisplayName   string    `gorm:"column:display_name"`
	}
	err := handler.db.WithContext(ctx).Table("workspace_members wm").
		Select(`u.email, u.first_name, u.last_name, u.avatar, u.avatar_asset_id,
			u.cover_image, u.cover_image_asset_id, u.date_joined, u.user_timezone, u.display_name`).
		Joins("JOIN workspaces w ON w.id = wm.workspace_id").
		Joins("JOIN users u ON u.id = wm.member_id").
		Where("w.slug = ? AND wm.member_id = ? AND wm.is_active = TRUE AND wm.deleted_at IS NULL", slug, memberID).
		Limit(1).Scan(&rows).Error
	if err != nil || len(rows) == 0 {
		return nil, false, err
	}
	row := rows[0]
	return gin.H{
		"email": row.Email, "first_name": row.FirstName, "last_name": row.LastName,
		"avatar_url":      staticAssetURL(row.AvatarAssetID, row.Avatar),
		"cover_image_url": staticAssetURL(row.CoverAssetID, stringOrBlank(row.CoverImage)),
		"date_joined":     row.DateJoined, "user_timezone": row.UserTimezone,
		"display_name": row.DisplayName,
	}, true, nil
}

// staticAssetURL is the avatar_url and cover_image_url properties: the uploaded asset first, then whatever url was stored, and null when there is neither.
func staticAssetURL(assetID *string, stored string) any {
	if assetID != nil {
		return "/api/assets/v2/static/" + *assetID + "/"
	}
	if stored != "" {
		return stored
	}
	return nil
}

// activityActorNames reads the display names the csv reports rather than the whole lite user.
func (handler *Handler) activityActorNames(ctx context.Context, activities []IssueActivity) (map[string]string, error) {
	identifiers := make([]string, 0, len(activities))
	for _, activity := range activities {
		if activity.ActorID != nil {
			identifiers = append(identifiers, *activity.ActorID)
		}
	}
	names := map[string]string{}
	if len(identifiers) == 0 {
		return names, nil
	}
	var rows []struct {
		ID          string `gorm:"column:id"`
		DisplayName string `gorm:"column:display_name"`
	}
	err := handler.db.WithContext(ctx).Table("users").Select("id, display_name").
		Where("id IN ?", identifiers).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		names[row.ID] = row.DisplayName
	}
	return names, nil
}

type activityProject struct {
	name       string
	identifier string
}

func (handler *Handler) activityProjectNames(ctx context.Context, activities []IssueActivity) (map[string]activityProject, error) {
	identifiers := make([]string, 0, len(activities))
	for _, activity := range activities {
		identifiers = append(identifiers, activity.ProjectID)
	}
	projects := map[string]activityProject{}
	if len(identifiers) == 0 {
		return projects, nil
	}
	var rows []struct {
		ID         string `gorm:"column:id"`
		Name       string `gorm:"column:name"`
		Identifier string `gorm:"column:identifier"`
	}
	err := handler.db.WithContext(ctx).Table("projects").Select("id, name, identifier").
		Where("id IN ?", identifiers).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		projects[row.ID] = activityProject{name: row.Name, identifier: row.Identifier}
	}
	return projects, nil
}

// activityIssueSequences reads each work item's number, which the csv writes after its project's identifier. An activity with no work item leaves that half of the cell empty.
func (handler *Handler) activityIssueSequences(ctx context.Context, activities []IssueActivity) (map[string]string, error) {
	identifiers := make([]string, 0, len(activities))
	for _, activity := range activities {
		if activity.IssueID != nil {
			identifiers = append(identifiers, *activity.IssueID)
		}
	}
	sequences := map[string]string{}
	if len(identifiers) == 0 {
		return sequences, nil
	}
	var rows []struct {
		ID         string `gorm:"column:id"`
		SequenceID int    `gorm:"column:sequence_id"`
	}
	err := handler.db.WithContext(ctx).Table("issues").Select("id, sequence_id").
		Where("id IN ?", identifiers).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		sequences[row.ID] = strconv.Itoa(row.SequenceID)
	}
	return sequences, nil
}
