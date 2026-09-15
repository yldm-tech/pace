package project

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"github.com/yldm-tech/pace/apps/api-go/internal/drf"
	"gorm.io/gorm"
)

func (handler *Handler) registerCycleProgressRoutes(router gin.IRouter) {
	router.GET("/api/workspaces/:slug/projects/:id/cycles/:cycle/progress/", handler.authenticated(handler.cycleProgress))
}

// cycleProgress reports what a cycle has left to do, in issues and in points.
//
// The two halves do not come from the same place. The point sums are always computed live, while the issue counts are read from the cycle's progress snapshot whenever it has one — a cycle whose issues were transferred away keeps the numbers it had at the moment of transfer, and the live count would report the empty cycle it has become.
func (handler *Handler) cycleProgress(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	slug, projectID, cycleID := c.Param("slug"), c.Param("id"), c.Param("cycle")

	var cycle Cycle
	err := handler.db.WithContext(c.Request.Context()).Table("cycles c").
		Joins("JOIN workspaces w ON w.id = c.workspace_id").
		Where("w.slug = ? AND c.project_id = ? AND c.id = ? AND c.deleted_at IS NULL", slug, projectID, cycleID).
		Select("c.*").Take(&cycle).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "Cycle not found"})
		return
	}
	if err != nil {
		handler.internalError(c, err)
		return
	}

	estimates, err := handler.cycleEstimateTotals(c, slug, projectID, cycleID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	counts, err := handler.cycleIssueCounts(c, slug, projectID, cycleID, cycle.ProgressSnapshot)
	if err != nil {
		handler.internalError(c, err)
		return
	}

	drf.Respond(c, http.StatusOK, gin.H{
		"backlog_estimate_points":   orZero(estimates.Backlog),
		"unstarted_estimate_points": orZero(estimates.Unstarted),
		"started_estimate_points":   orZero(estimates.Started),
		"cancelled_estimate_points": orZero(estimates.Cancelled),
		"completed_estimate_points": orZero(estimates.Completed),
		// The total is the one sum Django gives a default, so it is a float even when the cycle holds nothing. Its five neighbours go through `or 0` instead and come back as the integer zero.
		"total_estimate_points": coalesceFloat(estimates.Total),
		"backlog_issues":        counts["backlog_issues"],
		"total_issues":          counts["total_issues"],
		"completed_issues":      counts["completed_issues"],
		"cancelled_issues":      counts["cancelled_issues"],
		"started_issues":        counts["started_issues"],
		"unstarted_issues":      counts["unstarted_issues"],
	})
}

// cycleEstimateSums holds the six aggregates, each null when the cycle has no estimated issues at all.
type cycleEstimateSums struct {
	Backlog   *float64 `gorm:"column:backlog_estimate_point"`
	Unstarted *float64 `gorm:"column:unstarted_estimate_point"`
	Started   *float64 `gorm:"column:started_estimate_point"`
	Cancelled *float64 `gorm:"column:cancelled_estimate_point"`
	Completed *float64 `gorm:"column:completed_estimate_points"`
	Total     *float64 `gorm:"column:total_estimate_points"`
}

// cycleEstimateTotals adds up a cycle's points, one aggregate per state group.
//
// Only an estimate of the points type has a number to add, and the five grouped sums count a non-matching issue as zero rather than skipping it — which is why they are non-null as soon as the cycle holds one estimated issue, whatever its state.
func (handler *Handler) cycleEstimateTotals(c *gin.Context, slug, projectID, cycleID string) (cycleEstimateSums, error) {
	groupSum := func(group, alias string) string {
		return `SUM(CASE WHEN st.group = '` + group + `' THEN CAST(ep.value AS DOUBLE PRECISION) ELSE 0 END) AS ` + alias
	}
	selection := groupSum("backlog", "backlog_estimate_point") + `,
		` + groupSum("unstarted", "unstarted_estimate_point") + `,
		` + groupSum("started", "started_estimate_point") + `,
		` + groupSum("cancelled", "cancelled_estimate_point") + `,
		` + groupSum("completed", "completed_estimate_points") + `,
		SUM(CAST(ep.value AS DOUBLE PRECISION)) AS total_estimate_points`

	var sums cycleEstimateSums
	err := handler.db.WithContext(c.Request.Context()).Table("issues i").
		Select(selection).
		Joins("JOIN workspaces w ON w.id = i.workspace_id").
		Joins("JOIN cycle_issues ci ON ci.issue_id = i.id AND ci.cycle_id = ? AND ci.deleted_at IS NULL", cycleID).
		Joins("JOIN estimate_points ep ON ep.id = i.estimate_point_id").
		Joins("JOIN estimates es ON es.id = ep.estimate_id AND es.type = 'points'").
		Joins("LEFT JOIN states st ON st.id = i.state_id").
		Where("w.slug = ? AND i.project_id = ?", slug, projectID).
		Where(issueObjectsPredicate("i")).
		Scan(&sums).Error
	return sums, err
}

// cycleIssueCounts is the issue half. A cycle with a progress snapshot reports the snapshot, down to the zero that a missing key defaults to; one without it is counted live, one state group at a time.
func (handler *Handler) cycleIssueCounts(c *gin.Context, slug, projectID, cycleID string, snapshot auth.JSONValue) (map[string]any, error) {
	keys := []string{"backlog_issues", "unstarted_issues", "started_issues", "cancelled_issues", "completed_issues", "total_issues"}
	counts := make(map[string]any, len(keys))

	if decoded, ok := drf.DecodeJSON([]byte(snapshot)).(map[string]any); ok && len(decoded) > 0 {
		for _, key := range keys {
			if value, present := decoded[key]; present {
				counts[key] = value
			} else {
				counts[key] = 0
			}
		}
		return counts, nil
	}

	for _, key := range keys {
		query := handler.db.WithContext(c.Request.Context()).Table("issues i").
			Joins("JOIN workspaces w ON w.id = i.workspace_id").
			Joins("JOIN cycle_issues ci ON ci.issue_id = i.id AND ci.cycle_id = ? AND ci.deleted_at IS NULL", cycleID).
			Where("w.slug = ? AND i.project_id = ?", slug, projectID).
			Where(issueObjectsPredicate("i"))
		if key != "total_issues" {
			group := key[:len(key)-len("_issues")]
			query = query.Joins("JOIN states st ON st.id = i.state_id AND st.group = ?", group)
		}
		var count int64
		if err := query.Count(&count).Error; err != nil {
			return nil, err
		}
		counts[key] = count
	}
	return counts, nil
}

// orZero is Python's `or 0`: a sum that is absent or exactly zero is reported as the integer zero, and only a non-zero one keeps its type. The difference is visible in the body, where 0 and 0.0 are not the same three characters.
func orZero(value *float64) any {
	if value == nil || *value == 0 {
		return 0
	}
	return *value
}

// coalesceFloat is the other half of that pair: a sum Django gave a default to is a float even when there was nothing to add.
func coalesceFloat(value *float64) float64 {
	if value == nil {
		return 0
	}
	return *value
}
