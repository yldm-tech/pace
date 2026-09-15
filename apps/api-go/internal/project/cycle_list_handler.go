package project

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"github.com/yldm-tech/pace/apps/api-go/internal/drf"
	"gorm.io/gorm"
)

func (handler *Handler) registerCycleListRoutes(router gin.IRouter) {
	router.GET("/api/workspaces/:slug/projects/:id/cycles/", handler.authenticated(handler.cycleList))
}

// cycleRow is the values() projection the list returns, with the counts and the derived status annotated on.
type cycleRow struct {
	Cycle
	IsFavorite      bool           `gorm:"column:is_favorite"`
	TotalIssues     int64          `gorm:"column:total_issues"`
	CompletedIssues int64          `gorm:"column:completed_issues"`
	CancelledIssues int64          `gorm:"column:cancelled_issues"`
	AssigneeIDs     pq.StringArray `gorm:"column:assignee_ids;type:uuid[]"`
	Status          string         `gorm:"column:status"`
	// SubIssues is annotated only by the retrieve, which is the one route that reports how many of a cycle's issues are sub-issues.
	SubIssues *int64 `gorm:"column:sub_issues"`
}

// cycleList returns every cycle of a project that is not archived. Its timestamps are rendered in the **project's** timezone rather than the caller's, which is the one place in the codebase that distinction is made.
func (handler *Handler) cycleList(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	slug, projectID := c.Param("slug"), c.Param("id")

	var project Project
	err := handler.db.WithContext(c.Request.Context()).Where("id = ?", projectID).Take(&project).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "The required object does not exist."})
		return
	}
	if err != nil {
		handler.internalError(c, err)
		return
	}
	location, err := time.LoadLocation(project.Timezone)
	if err != nil {
		// pytz raises on a name it does not know, which answers 500.
		handler.internalError(c, err)
		return
	}
	// The status is measured against now as it reads in the project's timezone, which for a comparison against an instant is the same instant; the conversion matters only for what it is compared with.
	now := handler.clock().UTC()

	rows, err := handler.cycleRows(c, slug, projectID, user.ID, now, c.Query("cycle_view") == "current")
	if err != nil {
		handler.internalError(c, err)
		return
	}
	// cycle_view=current narrows the list, and falls through to the whole list when nothing is running.
	if c.Query("cycle_view") == "current" && len(rows) == 0 {
		rows, err = handler.cycleRows(c, slug, projectID, user.ID, now, false)
		if err != nil {
			handler.internalError(c, err)
			return
		}
	}

	results := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		results = append(results, cycleListJSON(row, location))
	}
	drf.Respond(c, http.StatusOK, results)
}

// cycleRows reads the annotated cycles. The caller must be an active member of the project and the project must not be archived, both of which are joined in rather than checked separately.
func (handler *Handler) cycleRows(c *gin.Context, slug, projectID, userID string, now time.Time, currentOnly bool) ([]cycleRow, error) {
	query := handler.db.WithContext(c.Request.Context()).Table("cycles c").
		Select(cycleAnnotations(), userID, projectID, slug, now, now, now, now).
		Joins("JOIN workspaces w ON w.id = c.workspace_id").
		Joins("JOIN projects p ON p.id = c.project_id AND p.archived_at IS NULL").
		Joins("JOIN project_members pm ON pm.project_id = c.project_id AND pm.member_id = ? AND pm.is_active = TRUE", userID).
		Where("w.slug = ? AND c.project_id = ? AND c.deleted_at IS NULL AND c.archived_at IS NULL", slug, projectID)
	if currentOnly {
		query = query.Where("c.start_date <= ? AND c.end_date >= ?", now, now)
	}
	var rows []cycleRow
	// The list overrides the queryset's own ordering: favourites first, then newest.
	err := query.Group("c.id").Order("is_favorite DESC, c.created_at DESC").Scan(&rows).Error
	return rows, err
}

// cycleRowsByID reads one cycle through the same annotated queryset the list uses, optionally with the sub-issue count only the retrieve carries.
func (handler *Handler) cycleRowsByID(c *gin.Context, slug, projectID, userID, cycleID string, now time.Time, withSubIssues bool) ([]cycleRow, error) {
	selection := cycleAnnotations()
	arguments := []any{userID, projectID, slug, now, now, now, now}
	if withSubIssues {
		selection += `,
		(SELECT COUNT(*) FROM issues si
			JOIN cycle_issues sci ON sci.issue_id = si.id AND sci.cycle_id = c.id AND sci.deleted_at IS NULL
			WHERE si.project_id = ? AND si.parent_id IS NOT NULL AND ` + issueObjectsPredicate("si") + `) AS sub_issues`
		arguments = append(arguments, projectID)
	}
	var rows []cycleRow
	err := handler.db.WithContext(c.Request.Context()).Table("cycles c").
		Select(selection, arguments...).
		Joins("JOIN workspaces w ON w.id = c.workspace_id").
		Joins("JOIN projects p ON p.id = c.project_id AND p.archived_at IS NULL").
		Joins("JOIN project_members pm ON pm.project_id = c.project_id AND pm.member_id = ? AND pm.is_active = TRUE", userID).
		Where("w.slug = ? AND c.project_id = ? AND c.id = ? AND c.deleted_at IS NULL", slug, projectID, cycleID).
		Group("c.id").Limit(1).Scan(&rows).Error
	return rows, err
}

// cycleAnnotations is the select the list builds. Every count carries the same four exclusions — the link and the issue must both be live, and the issue must be neither archived nor a draft — so a cycle's totals agree with what its board shows.
func cycleAnnotations() string {
	const liveIssues = `ci.deleted_at IS NULL AND ii.deleted_at IS NULL
		AND ii.archived_at IS NULL AND ii.is_draft = FALSE`
	return `c.*,
		EXISTS (SELECT 1 FROM user_favorites uf
			JOIN workspaces uw ON uw.id = uf.workspace_id
			WHERE uf.user_id = ? AND uf.entity_identifier = c.id AND uf.entity_type = 'cycle'
			AND uf.project_id = ? AND uw.slug = ? AND uf.deleted_at IS NULL) AS is_favorite,
		(SELECT COUNT(DISTINCT ii.id) FROM cycle_issues ci
			JOIN issues ii ON ii.id = ci.issue_id
			WHERE ci.cycle_id = c.id AND ` + liveIssues + `) AS total_issues,
		(SELECT COUNT(DISTINCT ii.id) FROM cycle_issues ci
			JOIN issues ii ON ii.id = ci.issue_id
			JOIN states st ON st.id = ii.state_id AND st.group = 'completed'
			WHERE ci.cycle_id = c.id AND ` + liveIssues + `) AS completed_issues,
		(SELECT COUNT(DISTINCT ii.id) FROM cycle_issues ci
			JOIN issues ii ON ii.id = ci.issue_id
			JOIN states st ON st.id = ii.state_id AND st.group = 'cancelled'
			WHERE ci.cycle_id = c.id AND ` + liveIssues + `) AS cancelled_issues,
		COALESCE((SELECT ARRAY_AGG(DISTINCT ia.assignee_id) FROM cycle_issues ci
			JOIN issue_assignees ia ON ia.issue_id = ci.issue_id AND ia.deleted_at IS NULL
			WHERE ci.cycle_id = c.id), '{}') AS assignee_ids,
		CASE
			WHEN c.start_date <= ? AND c.end_date >= ? THEN 'CURRENT'
			WHEN c.start_date > ? THEN 'UPCOMING'
			WHEN c.end_date < ? THEN 'COMPLETED'
			ELSE 'DRAFT'
		END AS status`
}

// cycleListJSON is the values() projection: twenty-three fields, with the two dates rendered in the project's timezone.
func cycleListJSON(row cycleRow, location *time.Location) gin.H {
	return gin.H{
		"id": row.ID, "workspace_id": row.WorkspaceID, "project_id": row.ProjectID,
		"name": row.Name, "description": row.Description,
		"start_date": timeIn(row.StartDate, location), "end_date": timeIn(row.EndDate, location),
		"owned_by_id": row.OwnedByID, "view_props": decodeJSON(row.ViewProps),
		"sort_order": row.SortOrder, "external_source": row.ExternalSource, "external_id": row.ExternalID,
		"progress_snapshot": decodeJSON(row.ProgressSnapshot), "logo_props": decodeJSON(row.LogoProps),
		"is_favorite": row.IsFavorite, "total_issues": row.TotalIssues,
		"cancelled_issues": row.CancelledIssues, "completed_issues": row.CompletedIssues,
		"assignee_ids": stringsOrEmpty(row.AssigneeIDs), "status": row.Status,
		"version": row.Version, "created_by": row.CreatedByID,
	}
}

// timeIn renders a nullable instant in a zone, leaving a null one alone.
func timeIn(value *time.Time, location *time.Location) any {
	if value == nil {
		return nil
	}
	return value.In(location)
}
