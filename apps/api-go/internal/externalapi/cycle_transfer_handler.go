package externalapi

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"github.com/yldm-tech/pace/apps/api-go/internal/drf"
	"github.com/yldm-tech/pace/apps/api-go/internal/project"
)

func (handler *Handler) registerCycleTransferRoutes(router gin.IRouter) {
	router.POST("/api/v1/workspaces/:slug/projects/:project/cycles/:cycle/transfer-issues/",
		handler.authenticated(handler.cycleTransferIssues))
}

// cycleTransferIssues moves a cycle's unfinished work into another cycle, and freezes what the old one looked like on the way out.
//
// The work itself is the utility the session API's own transfer runs, which lives in internal/project because the snapshot it freezes is that API's analytics. What this route adds is a guard of its own: the **old** cycle has to be finished, which the session API never asks.
func (handler *Handler) cycleTransferIssues(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireProjectMember(c, user, c.Request.Method) {
		return
	}
	var request struct {
		NewCycleID string `json:"new_cycle_id"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		invalidPayload(c)
		return
	}
	if request.NewCycleID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "New Cycle Id is required"})
		return
	}
	slug, projectID, cycleID := c.Param("slug"), c.Param("project"), c.Param("cycle")
	source, found, err := handler.externalCycleByID(c, cycleID)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !found {
		notFound(c)
		return
	}
	now := handler.clock().UTC()
	// A cycle with no end date at all passes, since there is no date to be past.
	if source.EndDate != nil && source.EndDate.After(now) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "The old cycle is not completed yet"})
		return
	}

	moved, refusal, err := project.TransferCycleIssues(c, handler.db, now, slug, projectID, cycleID, request.NewCycleID)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if refusal != nil {
		c.JSON(refusal.Status, refusal.Body)
		return
	}
	if err := handler.publishCycleTransferActivity(c, user, projectID, moved, now); err != nil {
		handler.serverError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, gin.H{"message": "Success"})
}

// publishCycleTransferActivity sends the one activity the transfer produces. It names no work item: the move is about the cycle, and the task fans it out over the list it carries.
func (handler *Handler) publishCycleTransferActivity(c *gin.Context, user *auth.User, projectID string, moved []map[string]string, now interface{ Unix() int64 }) error {
	if handler.tasks == nil {
		return nil
	}
	if moved == nil {
		moved = []map[string]string{}
	}
	requested, err := json.Marshal(map[string]any{"cycles_list": []any{}})
	if err != nil {
		return err
	}
	snapshot, err := json.Marshal(map[string]any{
		"updated_cycle_issues": moved,
		"created_cycle_issues": []any{},
	})
	if err != nil {
		return err
	}
	return handler.tasks.PublishIssueActivity(c.Request.Context(), map[string]any{
		"type": "cycle.activity.created", "requested_data": string(requested),
		"actor_id": user.ID, "issue_id": nil, "project_id": projectID,
		"current_instance": string(snapshot), "epoch": now.Unix(),
		"notification": true, "origin": handler.origin(c),
	})
}
