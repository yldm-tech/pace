package project

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api/internal/access"
	"github.com/yldm-tech/pace/apps/api/internal/auth"
	"github.com/yldm-tech/pace/apps/api/internal/drf"
)

func (handler *Handler) registerWorkspaceAggregateRoutes(router gin.IRouter) {
	router.GET("/api/workspaces/:slug/labels/", handler.authenticated(handler.workspaceLabels))
	router.GET("/api/workspaces/:slug/states/", handler.authenticated(handler.workspaceStates))
	router.GET("/api/workspaces/:slug/cycles/", handler.authenticated(handler.workspaceCycles))
	router.GET("/api/workspaces/:slug/modules/", handler.authenticated(handler.workspaceModules))
}

// These four are the same question asked four ways: everything of a kind the caller can see across a workspace. Each is scoped to the projects they are an **active member** of, and each skips an archived project — so the answer is what their sidebar could show rather than what the workspace holds.

// workspaceLabels returns every label of every project the caller is in.
func (handler *Handler) workspaceLabels(c *gin.Context, user *auth.User) {
	if !handler.requireWorkspaceRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	var labels []Label
	err := handler.db.WithContext(c.Request.Context()).Table("labels l").Select("l.*").
		Joins("JOIN workspaces w ON w.id = l.workspace_id").
		Joins("JOIN projects p ON p.id = l.project_id AND p.archived_at IS NULL").
		Joins(access.MemberJoin("l", "project_id", access.WithMembershipSoftDelete()), user.ID).
		Where("w.slug = ? AND l.deleted_at IS NULL", c.Param("slug")).
		Order("l.created_at").Scan(&labels).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	results := make([]gin.H, 0, len(labels))
	for _, label := range labels {
		results = append(results, labelJSON(label))
	}
	drf.Respond(c, http.StatusOK, results)
}

// workspaceStates returns every state of every project the caller is in, with the triage ones hidden.
//
// Each state carries the same `order` the project's own list adds — its place as a fraction of its group — but the group is counted **across the whole workspace** here, so a state's order differs between this list and its project's.
func (handler *Handler) workspaceStates(c *gin.Context, user *auth.User) {
	if !handler.requireWorkspaceRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	var states []State
	err := handler.db.WithContext(c.Request.Context()).Table("states s").Select("s.*").
		Joins("JOIN workspaces w ON w.id = s.workspace_id").
		Joins("JOIN projects p ON p.id = s.project_id AND p.archived_at IS NULL").
		Joins(access.MemberJoin("s", "project_id", access.WithMembershipSoftDelete()), user.ID).
		Where("w.slug = ? AND s.is_triage = FALSE AND s.deleted_at IS NULL", c.Param("slug")).
		Order("s.sequence").Scan(&states).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	counts := map[string]int{}
	for _, state := range states {
		counts[state.Group]++
	}
	seen := map[string]int{}
	results := make([]gin.H, 0, len(states))
	for _, state := range states {
		seen[state.Group]++
		body := stateJSON(state)
		body["order"] = float64(seen[state.Group]) / float64(counts[state.Group])
		results = append(results, body)
	}
	drf.Respond(c, http.StatusOK, results)
}

// workspaceCycles returns every live cycle of every project the caller is in.
//
// Its timestamps are rendered in the **caller's** timezone rather than each project's, which is the asymmetry the project's own cycle list has the other way round.
func (handler *Handler) workspaceCycles(c *gin.Context, user *auth.User) {
	if !handler.requireWorkspaceRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	location, err := userLocation(user)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	now := handler.clock().UTC()
	var rows []cycleRow
	err = handler.db.WithContext(c.Request.Context()).Table("cycles c").
		Select(cycleAnnotations(), user.ID, nil, c.Param("slug"), now, now, now, now).
		Joins("JOIN workspaces w ON w.id = c.workspace_id").
		Joins("JOIN projects p ON p.id = c.project_id AND p.archived_at IS NULL").
		Joins(access.MemberJoin("c", "project_id", access.WithMembershipSoftDelete()), user.ID).
		Where("w.slug = ? AND c.archived_at IS NULL AND c.deleted_at IS NULL", c.Param("slug")).
		Group("c.id").Order("c.created_at DESC").Scan(&rows).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	results := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		body := cycleListJSON(row, location)
		// The workspace list reports neither the assignees nor the version the project's own list carries.
		delete(body, "assignee_ids")
		delete(body, "version")
		delete(body, "created_by")
		results = append(results, body)
	}
	drf.Respond(c, http.StatusOK, results)
}

// workspaceModules returns every live module of every project the caller is in, with the archive stamp the project's own list leaves out.
func (handler *Handler) workspaceModules(c *gin.Context, user *auth.User) {
	if !handler.requireWorkspaceRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	location, err := userLocation(user)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	var rows []moduleRow
	err = handler.db.WithContext(c.Request.Context()).Table("modules m").
		Select(moduleAnnotations(), user.ID, nil, c.Param("slug")).
		Joins("JOIN workspaces w ON w.id = m.workspace_id").
		Joins("JOIN projects p ON p.id = m.project_id AND p.archived_at IS NULL").
		Joins(access.MemberJoin("m", "project_id", access.WithMembershipSoftDelete()), user.ID).
		Where("w.slug = ? AND m.archived_at IS NULL AND m.deleted_at IS NULL", c.Param("slug")).
		Group("m.id").Order("m.created_at DESC").Scan(&rows).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	results := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		body := moduleListJSON(row, location)
		body["archived_at"] = row.ArchivedAt
		results = append(results, body)
	}
	// The modules come back newest first, where the project's own list puts the favourites at the top.
	drf.Respond(c, http.StatusOK, results)
}
