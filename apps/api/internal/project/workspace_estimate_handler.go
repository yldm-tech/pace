package project

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api/internal/auth"
	"github.com/yldm-tech/pace/apps/api/internal/drf"
)

func (handler *Handler) registerWorkspaceEstimateRoutes(router gin.IRouter) {
	router.GET("/api/workspaces/:slug/estimates/", handler.authenticated(handler.workspaceEstimateList))
}

// workspaceEstimateList is every scale in the workspace that a project is actually using.
//
// It starts from the projects rather than from the scales, so a scale nobody has selected is left out even though it exists — and a scale two projects share is listed once, because the ids go through an IN.
//
// The read is any active member's, guests included: the permission only narrows on the unsafe methods, and this route has none.
func (handler *Handler) workspaceEstimateList(c *gin.Context, user *auth.User) {
	if !handler.requireWorkspaceRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	slug := c.Param("slug")

	var estimates []Estimate
	// The project side does not filter deleted_at, which is the manager's own doing rather than the view's: Project.objects hides a soft-deleted project, so a scale only that project used disappears with it.
	err := handler.db.WithContext(c.Request.Context()).Table("estimates e").Select("e.*").
		Joins("JOIN workspaces w ON w.id = e.workspace_id").
		Where(`w.slug = ? AND e.deleted_at IS NULL AND e.id IN (
			SELECT p.estimate_id FROM projects p
			JOIN workspaces pw ON pw.id = p.workspace_id
			WHERE pw.slug = ? AND p.estimate_id IS NOT NULL AND p.deleted_at IS NULL)`, slug, slug).
		Order("e.created_at DESC").Scan(&estimates).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}

	points, err := handler.workspaceEstimatePoints(c, slug, estimates)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	results := make([]gin.H, 0, len(estimates))
	for _, estimate := range estimates {
		results = append(results, estimateReadJSON(estimate, points[estimate.ID]))
	}
	drf.Respond(c, http.StatusOK, results)
}

// workspaceEstimatePoints reads the points of every scale in one go, which is what the prefetch does.
func (handler *Handler) workspaceEstimatePoints(c *gin.Context, slug string, estimates []Estimate) (map[string][]EstimatePoint, error) {
	byEstimate := map[string][]EstimatePoint{}
	if len(estimates) == 0 {
		return byEstimate, nil
	}
	ids := make([]string, 0, len(estimates))
	for _, estimate := range estimates {
		ids = append(ids, estimate.ID)
	}
	var points []EstimatePoint
	err := handler.db.WithContext(c.Request.Context()).Table("estimate_points ep").Select("ep.*").
		Joins("JOIN workspaces w ON w.id = ep.workspace_id").
		Where("w.slug = ? AND ep.estimate_id IN ? AND ep.deleted_at IS NULL", slug, ids).
		Order("ep.key").Scan(&points).Error
	if err != nil {
		return nil, err
	}
	for _, point := range points {
		byEstimate[point.EstimateID] = append(byEstimate[point.EstimateID], point)
	}
	return byEstimate, nil
}
