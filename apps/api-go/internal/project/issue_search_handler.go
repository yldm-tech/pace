package project

import (
	"net/http"
	"regexp"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"github.com/yldm-tech/pace/apps/api-go/internal/drf"
	"gorm.io/gorm"
)

func (handler *Handler) registerIssueSearchRoutes(router gin.IRouter) {
	router.GET("/api/workspaces/:slug/projects/:id/search-issues/", handler.authenticated(handler.issueSearch))
}

// issueSearch is the picker behind every "link this to something" box: choosing a parent, a related issue, a sub-issue, or an issue to put in a cycle or a module.
//
// Each of those asks for a different set of exclusions, and the parameters that switch them on are read as **strings** rather than as booleans, so the value has to be exactly "true" to count.
func (handler *Handler) issueSearch(c *gin.Context, user *auth.User) {
	slug, projectID := c.Param("slug"), c.Param("id")

	query := handler.db.WithContext(c.Request.Context()).Table("issues i").
		Joins("JOIN workspaces w ON w.id = i.workspace_id").
		Joins("JOIN projects p ON p.id = i.project_id AND p.archived_at IS NULL").
		Joins("JOIN project_members pm ON pm.project_id = i.project_id AND pm.member_id = ? AND pm.is_active = TRUE", user.ID).
		Where("w.slug = ?", slug).
		Where(issueObjectsPredicate("i"))

	// Without workspace_search the picker stays inside the project it was opened from.
	if c.DefaultQuery("workspace_search", "false") == "false" {
		query = query.Where("i.project_id = ?", projectID)
	}
	if search := c.Query("search"); search != "" {
		query = applyIssueSearch(query, search)
	}

	issueID := c.Query("issue_id")
	if c.DefaultQuery("parent", "false") == "true" && issueID != "" {
		// Choosing a parent must not offer the issue itself, its own parent, or anything already under it.
		query = query.Where(`i.id <> ? AND i.id IS DISTINCT FROM (SELECT pi.parent_id FROM issues pi WHERE pi.id = ?)
			AND (i.parent_id IS NULL OR i.parent_id <> ?)`, issueID, issueID, issueID)
	}
	if c.DefaultQuery("issue_relation", "false") == "true" && issueID != "" {
		// Choosing a related issue must not offer one that is already related either way round, nor the issue itself.
		query = query.Where(`i.id NOT IN (
			SELECT r.issue_id FROM issue_relations r WHERE r.issue_id = ? OR r.related_issue_id = ?
			UNION ALL
			SELECT r.related_issue_id FROM issue_relations r WHERE r.issue_id = ? OR r.related_issue_id = ?
			UNION ALL SELECT ?::uuid)`, issueID, issueID, issueID, issueID, issueID)
	}
	if c.DefaultQuery("sub_issue", "false") == "true" && issueID != "" {
		// Choosing a sub-issue offers only issues that have no parent yet, and never the issue itself or its parent.
		query = query.Where("i.id <> ? AND i.parent_id IS NULL", issueID)
		query = query.Where(`i.id IS DISTINCT FROM (SELECT si.parent_id FROM issues si WHERE si.id = ?)`, issueID)
	}
	if c.DefaultQuery("cycle", "false") == "true" {
		query = query.Where(`NOT EXISTS (SELECT 1 FROM cycle_issues ci WHERE ci.issue_id = i.id AND ci.deleted_at IS NULL)`)
	}
	if module := c.Query("module"); module != "" {
		query = query.Where(`NOT EXISTS (SELECT 1 FROM module_issues mi
			WHERE mi.issue_id = i.id AND mi.module_id = ? AND mi.deleted_at IS NULL)`, module)
	}
	// The default is the boolean True rather than a string, so it never equals "none" and the filter is off unless the caller asks for it by name.
	if c.Query("target_date") == "none" {
		query = query.Where("i.target_date IS NULL")
	}

	// The guest narrowing here has no escape hatch: guest_view_all_features does not reach this endpoint, and the membership is not scoped to the workspace either.
	var guests int64
	err := handler.db.WithContext(c.Request.Context()).Table("project_members").
		Where("project_id = ? AND member_id = ? AND is_active = TRUE AND role = ?", projectID, user.ID, roleGuest).
		Count(&guests).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if guests > 0 {
		query = query.Where("i.created_by_id = ?", user.ID)
	}

	var rows []issueSearchRow
	err = query.Distinct().Select(`i.name, i.id, i.start_date, i.sequence_id,
		p.name AS project_name, p.identifier AS project_identifier, i.project_id,
		w.slug AS workspace_slug,
		(SELECT s.name FROM states s WHERE s.id = i.state_id) AS state_name,
		(SELECT s.group FROM states s WHERE s.id = i.state_id) AS state_group,
		(SELECT s.color FROM states s WHERE s.id = i.state_id) AS state_color`).
		Limit(100).Scan(&rows).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}

	results := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		results = append(results, gin.H{
			"name": row.Name, "id": row.ID, "start_date": dateOnly(row.StartDate),
			"sequence_id": row.SequenceID, "project__name": row.ProjectName,
			"project__identifier": row.ProjectIdentifier, "project_id": row.ProjectID,
			"workspace__slug": row.WorkspaceSlug, "state__name": row.StateName,
			"state__group": row.StateGroup, "state__color": row.StateColor,
		})
	}
	drf.Respond(c, http.StatusOK, results)
}

// issueSearchRow is the values() projection, with the related fields read through their joins.
type issueSearchRow struct {
	Name              string     `gorm:"column:name"`
	ID                string     `gorm:"column:id"`
	StartDate         *time.Time `gorm:"column:start_date"`
	SequenceID        int64      `gorm:"column:sequence_id"`
	ProjectName       string     `gorm:"column:project_name"`
	ProjectIdentifier string     `gorm:"column:project_identifier"`
	ProjectID         string     `gorm:"column:project_id"`
	WorkspaceSlug     string     `gorm:"column:workspace_slug"`
	StateName         *string    `gorm:"column:state_name"`
	StateGroup        *string    `gorm:"column:state_group"`
	StateColor        *string    `gorm:"column:state_color"`
}

// searchSequencePattern is the \b\d+\b the sequence search pulls out of the query.
var searchSequencePattern = regexp.MustCompile(`\b\d+\b`)

// applyIssueSearch is plane.utils.issue_search.search_issues.
//
// It looks in three places at once: the name, the project's identifier, and the sequence number. The sequence is only consulted for a **short** query — over twenty characters the branch is skipped entirely, so pasting a long string cannot turn into a numeric scan.
func applyIssueSearch(query *gorm.DB, search string) *gorm.DB {
	conditions := []string{"i.name ILIKE ?", "p.identifier ILIKE ?"}
	arguments := []any{"%" + search + "%", "%" + search + "%"}
	if len([]rune(search)) <= 20 {
		for _, sequence := range searchSequencePattern.FindAllString(search, -1) {
			conditions = append(conditions, "i.sequence_id = ?")
			arguments = append(arguments, sequence)
		}
	}
	joined := conditions[0]
	for _, condition := range conditions[1:] {
		joined += " OR " + condition
	}
	return query.Where("("+joined+")", arguments...)
}
