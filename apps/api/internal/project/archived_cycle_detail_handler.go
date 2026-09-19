package project

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api/internal/access"
	"github.com/yldm-tech/pace/apps/api/internal/auth"
	"github.com/yldm-tech/pace/apps/api/internal/drf"
)

func (handler *Handler) registerArchivedCycleDetailRoutes(router gin.IRouter) {
	router.GET("/api/workspaces/:slug/projects/:id/archived-cycles/:cycle/", handler.authenticated(handler.archivedCycleRetrieve))
}

// archivedCycleRetrieve is one archived cycle with both of its distributions.
//
// A cycle that is not archived, or not there at all, is a **500** rather than a 404. The queryset this reads filters `archived_at__isnull=False` before the id is applied, so a live cycle gives nothing back and the view then subscripts that nothing. Reproduced rather than corrected, because a 404 here would be a different answer to the same request.
func (handler *Handler) archivedCycleRetrieve(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember) {
		return
	}
	slug, projectID, cycleID := c.Param("slug"), c.Param("id"), c.Param("cycle")
	now := handler.clock().UTC()

	rows, err := handler.archivedCycleRowByID(c, slug, projectID, user.ID, cycleID, now)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if len(rows) == 0 {
		handler.internalError(c, errors.New("archived cycle: the cycle is not archived or does not exist"))
		return
	}
	row := rows[0]
	data := archivedCycleDetailJSON(row)

	// The point distribution is built only when the project measures in points at all; otherwise the key is an empty object.
	points, err := handler.projectEstimatesPoints(c, slug, projectID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	estimateDistribution := gin.H{}
	if points {
		assignees, labels, err := handler.cycleEstimateDistributions(c, slug, projectID, cycleID)
		if err != nil {
			handler.internalError(c, err)
			return
		}
		chart := gin.H{}
		// This one asks the *serialized* dates, which is why a cycle with only one of them still skips the chart.
		if row.StartDate != nil && row.EndDate != nil {
			chart, err = handler.cycleBurndown(c, slug, projectID, cycleID, row.Cycle, row.TotalIssues, true)
			if err != nil {
				handler.internalError(c, err)
				return
			}
		}
		estimateDistribution = gin.H{"assignees": assignees, "labels": labels, "completion_chart": chart}
	}
	data["estimate_distribution"] = estimateDistribution

	assignees, labels, err := handler.cycleIssueDistributions(c, slug, projectID, cycleID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	chart := gin.H{}
	if row.StartDate != nil && row.EndDate != nil {
		chart, err = handler.cycleBurndown(c, slug, projectID, cycleID, row.Cycle, row.TotalIssues, false)
		if err != nil {
			handler.internalError(c, err)
			return
		}
	}
	data["distribution"] = gin.H{"assignees": assignees, "labels": labels, "completion_chart": chart}

	drf.Respond(c, http.StatusOK, data)
}

// archivedCycleRowByID reads the one cycle with everything the detail projection names.
//
// It is the archived list's select plus three things that projection does not carry: how many of the cycle's work items are sub-items, and the two point totals — both of which coalesce to zero rather than to null, so a project that measures in issues still reports two zeroes here.
func (handler *Handler) archivedCycleRowByID(c *gin.Context, slug, projectID, userID, cycleID string, now time.Time) ([]cycleRow, error) {
	selection := archivedCycleAnnotations() + `,
		(SELECT COUNT(*) FROM issues si
			JOIN cycle_issues sci ON sci.issue_id = si.id AND sci.cycle_id = c.id AND sci.deleted_at IS NULL
			WHERE si.project_id = ? AND si.parent_id IS NOT NULL AND ` + issueObjectsPredicate("si") + `) AS sub_issues,
		COALESCE((SELECT SUM(CAST(ep.value AS DOUBLE PRECISION)) FROM cycle_issues eci
			JOIN issues ei ON ei.id = eci.issue_id
			JOIN estimate_points ep ON ep.id = ei.estimate_point_id
			JOIN estimates es ON es.id = ep.estimate_id AND es.type = 'points'
			JOIN states est ON est.id = ei.state_id AND est.group = 'completed'
			WHERE eci.cycle_id = c.id AND eci.deleted_at IS NULL AND ` + issueObjectsPredicate("ei") + `), 0) AS completed_estimate_points,
		COALESCE((SELECT SUM(CAST(tp.value AS DOUBLE PRECISION)) FROM cycle_issues tci
			JOIN issues ti ON ti.id = tci.issue_id
			JOIN estimate_points tp ON tp.id = ti.estimate_point_id
			JOIN estimates ts ON ts.id = tp.estimate_id AND ts.type = 'points'
			WHERE tci.cycle_id = c.id AND tci.deleted_at IS NULL AND ` + issueObjectsPredicate("ti") + `), 0) AS total_estimate_points`

	var rows []cycleRow
	err := handler.db.WithContext(c.Request.Context()).Table("cycles c").
		Select(selection, userID, projectID, slug, now, now, now, now, projectID).
		Joins("JOIN workspaces w ON w.id = c.workspace_id").
		Joins("JOIN projects p ON p.id = c.project_id AND p.archived_at IS NULL").
		Joins(access.MemberJoin("c", "project_id"), userID).
		Where("w.slug = ? AND c.project_id = ? AND c.id = ? AND c.deleted_at IS NULL AND c.archived_at IS NOT NULL",
			slug, projectID, cycleID).
		Group("c.id").Limit(1).Scan(&rows).Error
	return rows, err
}

// archivedCycleDetailJSON is the values() projection: twenty-eight fields, which is the archived list's twenty-three plus five.
//
// The five it adds are sub_issues, logo_props, the two point totals and created_by. Like the archived list it renders the dates in UTC rather than in the project's zone, because nothing here converts them.
func archivedCycleDetailJSON(row cycleRow) gin.H {
	data := archivedCycleListJSON(row)
	data["sub_issues"] = row.SubIssues
	data["logo_props"] = decodeJSON(row.LogoProps)
	data["completed_estimate_points"] = drf.Float(row.CompletedEstimatePoints)
	data["total_estimate_points"] = drf.Float(row.TotalEstimatePoints)
	data["created_by"] = row.CreatedByID
	return data
}
