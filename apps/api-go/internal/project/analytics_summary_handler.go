package project

import (
	"errors"
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"github.com/yldm-tech/pace/apps/api-go/internal/drf"
	"gorm.io/gorm"
)

func (handler *Handler) registerAnalyticsSummaryRoutes(router gin.IRouter) {
	router.GET("/api/workspaces/:slug/default-analytics/", handler.authenticated(handler.defaultAnalytics))
	router.GET("/api/workspaces/:slug/project-stats/", handler.authenticated(handler.projectStats))
}

// defaultAnalytics is the workspace dashboard: ten numbers and lists over the same filtered set of issues.
func (handler *Handler) defaultAnalytics(c *gin.Context, user *auth.User) {
	if !handler.requireWorkspaceRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	slug := c.Param("slug")
	filters := issueFilters(queryParams(c), "GET", "", handler.clock().UTC())
	joins, conditions, arguments, translatable := issueFilterSQL(filters)
	if !translatable {
		handler.internalError(c, errors.New("default analytics: a filter names a field the schema does not have"))
		return
	}

	scope := func() *gorm.DB {
		query := handler.db.WithContext(c.Request.Context()).Table("issues i").
			Joins("JOIN workspaces w ON w.id = i.workspace_id").
			Where("w.slug = ?", slug).
			Where(issueObjectsPredicate("i"))
		for _, join := range joins {
			query = query.Joins(join)
		}
		consumed := 0
		for _, condition := range conditions {
			count := countPlaceholders(condition)
			query = query.Where(condition, arguments[consumed:consumed+count]...)
			consumed += count
		}
		return query
	}
	// The open set is the three groups that are not finished either way.
	openGroups := []string{"backlog", "unstarted", "started"}
	openScope := func() *gorm.DB {
		return scope().Where(`(SELECT s.group FROM states s WHERE s.id = i.state_id) IN ?`, openGroups)
	}

	var total, open int64
	if err := scope().Count(&total).Error; err != nil {
		handler.internalError(c, err)
		return
	}
	if err := openScope().Count(&open).Error; err != nil {
		handler.internalError(c, err)
		return
	}

	totalClassified, err := stateGroupCounts(scope())
	if err != nil {
		handler.internalError(c, err)
		return
	}
	openClassified, err := stateGroupCounts(openScope())
	if err != nil {
		handler.internalError(c, err)
		return
	}

	var monthly []struct {
		Month *int64 `gorm:"column:month"`
		Count int64  `gorm:"column:count"`
	}
	// Only the current year is charted, and the year is the server's rather than the caller's.
	err = scope().Where(`EXTRACT(YEAR FROM i.completed_at AT TIME ZONE 'UTC') = ?`, handler.clock().UTC().Year()).
		Select(`EXTRACT(MONTH FROM i.completed_at AT TIME ZONE 'UTC')::bigint AS month, COUNT(*) AS count`).
		Group(`EXTRACT(MONTH FROM i.completed_at AT TIME ZONE 'UTC')`).
		Order(`EXTRACT(MONTH FROM i.completed_at AT TIME ZONE 'UTC')`).Scan(&monthly).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	completedByMonth := make([]gin.H, 0, len(monthly))
	for _, row := range monthly {
		completedByMonth = append(completedByMonth, gin.H{"month": row.Month, "count": row.Count})
	}

	created, err := handler.analyticsUserCounts(scope().Where("i.created_by_id IS NOT NULL"), "created_by", 5)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	closed, err := handler.analyticsUserCounts(
		scope().Where("i.completed_at IS NOT NULL").
			Where(`EXISTS (SELECT 1 FROM issue_assignees xa WHERE xa.issue_id = i.id)`), "assignees", 5)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	// The pending list does **not** exclude the unassigned, so it carries a bucket whose user fields are all null.
	pending, err := handler.analyticsUserCounts(scope().Where("i.completed_at IS NULL"), "assignees", 0)
	if err != nil {
		handler.internalError(c, err)
		return
	}

	openEstimate, err := estimateSumOf(openScope())
	if err != nil {
		handler.internalError(c, err)
		return
	}
	totalEstimate, err := estimateSumOf(scope())
	if err != nil {
		handler.internalError(c, err)
		return
	}

	drf.Respond(c, http.StatusOK, gin.H{
		"total_issues": total, "total_issues_classified": totalClassified,
		"open_issues": open, "open_issues_classified": openClassified,
		"issue_completed_month_wise": completedByMonth,
		"most_issue_created_user":    created,
		"most_issue_closed_user":     closed,
		"pending_issue_user":         pending,
		"open_estimate_sum":          openEstimate, "total_estimate_sum": totalEstimate,
	})
}

// stateGroupCounts is the classification both halves of the dashboard report, ordered by the group's name.
func stateGroupCounts(query *gorm.DB) ([]gin.H, error) {
	const group = `(SELECT s.group FROM states s WHERE s.id = i.state_id)`
	var rows []struct {
		StateGroup *string `gorm:"column:state_group"`
		StateCount int64   `gorm:"column:state_count"`
	}
	// The count is over the group rather than over the row, so the bucket of issues with no state counts zero of them.
	err := query.Select(group + ` AS state_group, COUNT(` + group + `) AS state_count`).
		Group(group).Order(group).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	results := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		results = append(results, gin.H{"state_group": row.StateGroup, "state_count": row.StateCount})
	}
	return results, nil
}

// estimateSumOf adds up the point column, which is a plain number on the issue rather than a join to an estimate. A set with nothing in it sums to null rather than to zero.
func estimateSumOf(query *gorm.DB) (*float64, error) {
	var sums []*float64
	if err := query.Select("SUM(i.point)").Scan(&sums).Error; err != nil {
		return nil, err
	}
	if len(sums) == 0 {
		return nil, nil
	}
	return sums[0], nil
}

// analyticsUserCounts is the three "who did the most" lists, which differ only in whose name they group by and how many they keep.
func (handler *Handler) analyticsUserCounts(query *gorm.DB, relation string, limit int) ([]gin.H, error) {
	prefix, join := "cu", "LEFT JOIN users cu ON cu.id = i.created_by_id"
	if relation == "assignees" {
		prefix, join = "au", "LEFT JOIN issue_assignees aj ON aj.issue_id = i.id LEFT JOIN users au ON au.id = aj.assignee_id"
	}
	var rows []struct {
		FirstName   *string `gorm:"column:first_name"`
		LastName    *string `gorm:"column:last_name"`
		DisplayName *string `gorm:"column:display_name"`
		UserID      *string `gorm:"column:user_id"`
		AvatarURL   *string `gorm:"column:avatar_url"`
		Count       int64   `gorm:"column:count"`
	}
	avatar := `CASE WHEN ` + prefix + `.avatar_asset_id IS NOT NULL
		THEN '/api/assets/v2/static/' || CAST(` + prefix + `.avatar_asset_id AS TEXT) || '/'
		ELSE ` + prefix + `.avatar END`
	selection := prefix + `.first_name, ` + prefix + `.last_name, ` + prefix + `.display_name,
		` + prefix + `.id AS user_id, ` + avatar + ` AS avatar_url, COUNT(i.id) AS count`
	grouping := prefix + `.first_name, ` + prefix + `.last_name, ` + prefix + `.display_name, ` + prefix + `.id, ` + avatar

	statement := query.Joins(join).Select(selection).Group(grouping).Order("count DESC")
	if limit > 0 {
		statement = statement.Limit(limit)
	}
	if err := statement.Scan(&rows).Error; err != nil {
		return nil, err
	}
	results := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		results = append(results, gin.H{
			relation + "__first_name": row.FirstName, relation + "__last_name": row.LastName,
			relation + "__display_name": row.DisplayName, relation + "__id": row.UserID,
			relation + "__avatar_url": row.AvatarURL, "count": row.Count,
		})
	}
	return results, nil
}

// projectStatsValidFields are the five counts a caller may ask for.
var projectStatsValidFields = map[string]bool{
	"total_issues": true, "completed_issues": true, "total_members": true,
	"total_cycles": true, "total_modules": true,
}

// projectStats counts things per project, computing only what was asked for.
//
// A `fields` list with nothing valid in it is treated as asking for **everything**, not for nothing.
func (handler *Handler) projectStats(c *gin.Context, user *auth.User) {
	if !handler.requireWorkspaceRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	requested := []string{}
	for _, field := range strings.Split(c.Query("fields"), ",") {
		if projectStatsValidFields[field] {
			requested = append(requested, field)
		}
	}
	if len(requested) == 0 {
		for field := range projectStatsValidFields {
			requested = append(requested, field)
		}
	}
	sort.Strings(requested)

	query := handler.db.WithContext(c.Request.Context()).Table("projects p").
		Joins("JOIN workspaces w ON w.id = p.workspace_id").
		Where("w.slug = ? AND p.deleted_at IS NULL", c.Param("slug"))
	if raw := c.Query("project_ids"); raw != "" {
		query = query.Where("p.id IN ?", strings.Split(raw, ","))
	}

	selection := []string{"p.id"}
	for _, field := range requested {
		selection = append(selection, projectStatsColumn(field)+" AS "+field)
	}
	var rows []struct {
		ID              string `gorm:"column:id"`
		TotalIssues     *int64 `gorm:"column:total_issues"`
		CompletedIssues *int64 `gorm:"column:completed_issues"`
		TotalMembers    *int64 `gorm:"column:total_members"`
		TotalCycles     *int64 `gorm:"column:total_cycles"`
		TotalModules    *int64 `gorm:"column:total_modules"`
	}
	if err := query.Select(strings.Join(selection, ", ")).Scan(&rows).Error; err != nil {
		handler.internalError(c, err)
		return
	}

	results := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		data := gin.H{"id": row.ID}
		values := map[string]*int64{
			"total_issues": row.TotalIssues, "completed_issues": row.CompletedIssues,
			"total_members": row.TotalMembers, "total_cycles": row.TotalCycles, "total_modules": row.TotalModules,
		}
		for _, field := range requested {
			data[field] = values[field]
		}
		results = append(results, data)
	}
	drf.Respond(c, http.StatusOK, results)
}

// projectStatsColumn is the subquery each field counts with. A project with none of a thing reports zero rather than null, because a count over no rows is still a count.
func projectStatsColumn(field string) string {
	switch field {
	case "total_issues":
		return `(SELECT COUNT(ti.id) FROM issues ti WHERE ti.project_id = p.id AND ` + issueObjectsPredicate("ti") + `)`
	case "completed_issues":
		return `(SELECT COUNT(ti.id) FROM issues ti
			JOIN states ts ON ts.id = ti.state_id AND ts.group IN ('completed', 'cancelled')
			WHERE ti.project_id = p.id AND ` + issueObjectsPredicate("ti") + `)`
	case "total_cycles":
		return `(SELECT COUNT(tc.id) FROM cycles tc WHERE tc.project_id = p.id AND tc.deleted_at IS NULL)`
	case "total_modules":
		return `(SELECT COUNT(tm.id) FROM modules tm WHERE tm.project_id = p.id AND tm.deleted_at IS NULL)`
	case "total_members":
		return `(SELECT COUNT(tpm.id) FROM project_members tpm
			JOIN users tu ON tu.id = tpm.member_id AND tu.is_bot = FALSE
			WHERE tpm.project_id = p.id AND tpm.is_active = TRUE AND tpm.deleted_at IS NULL)`
	}
	return "NULL"
}
