package project

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (handler *Handler) registerMemberRoutes(router gin.IRouter) {
	router.GET("/api/workspaces/:slug/projects/:id/members/", handler.authenticatedUUID(handler.memberList))
	router.POST("/api/workspaces/:slug/projects/:id/members/", handler.authenticatedUUID(handler.memberCreate))
	router.GET("/api/workspaces/:slug/projects/:id/members/:member/", handler.authenticatedMemberUUID(handler.memberRetrieve))
	router.PATCH("/api/workspaces/:slug/projects/:id/members/:member/", handler.authenticatedMemberUUID(handler.memberPatch))
	router.DELETE("/api/workspaces/:slug/projects/:id/members/:member/", handler.authenticatedMemberUUID(handler.memberDelete))
	router.POST("/api/workspaces/:slug/projects/:id/members/leave/", handler.authenticatedUUID(handler.memberLeave))
	router.GET("/api/workspaces/:slug/projects/:id/project-members/me/", handler.authenticatedUUID(handler.memberMe))
	router.POST("/api/workspaces/:slug/projects/:id/project-views/", handler.authenticatedUUID(handler.memberViews))
	router.GET("/api/workspaces/:slug/projects/:id/preferences/member/:member/", handler.authenticatedMemberUUID(handler.memberPreferencesGet))
	router.PATCH("/api/workspaces/:slug/projects/:id/preferences/member/:member/", handler.authenticatedMemberUUID(handler.memberPreferencesPatch))
	router.GET("/api/users/me/workspaces/:slug/project-roles/", handler.authenticated(handler.userProjectRoles))
}

func (handler *Handler) authenticatedMemberUUID(next func(*gin.Context, *auth.User)) gin.HandlerFunc {
	inner := handler.authenticatedUUID(next)
	return func(c *gin.Context) {
		if !uuidPattern.MatchString(c.Param("member")) {
			handler.notFound(c)
			return
		}
		inner(c)
	}
}

func (handler *Handler) memberList(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	members, err := handler.activeProjectMembers(c.Request.Context(), c.Param("slug"), c.Param("id"))
	if err != nil {
		handler.internalError(c, err)
		return
	}
	response := make([]gin.H, 0, len(members))
	for _, member := range members {
		response = append(response, projectMemberRoleJSON(member))
	}
	c.JSON(http.StatusOK, response)
}

func (handler *Handler) memberCreate(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin) {
		return
	}
	slug, projectID := c.Param("slug"), c.Param("id")
	var project Project
	err := handler.db.WithContext(c.Request.Context()).
		Where("id = ? AND workspace_id = (SELECT id FROM workspaces WHERE slug = ?) AND deleted_at IS NULL", projectID, slug).
		Take(&project).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		handler.notFound(c)
		return
	}
	if err != nil {
		handler.internalError(c, err)
		return
	}
	var request struct {
		Members []struct {
			MemberID string           `json:"member_id"`
			Role     *json.RawMessage `json:"role"`
		} `json:"members"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		handler.invalidDetail(c)
		return
	}
	if len(request.Members) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "At least one member is required"})
		return
	}
	// Django builds a dict keyed by member_id, so a repeated id keeps the last role.
	requestedRoles := make(map[string]int, len(request.Members))
	order := make([]string, 0, len(request.Members))
	for _, member := range request.Members {
		role := roleGuest
		if member.Role != nil {
			value, err := handler.roleValue(*member.Role)
			if err != nil {
				handler.invalidDetail(c)
				return
			}
			role = value
		}
		if _, seen := requestedRoles[member.MemberID]; !seen {
			order = append(order, member.MemberID)
		}
		requestedRoles[member.MemberID] = role
	}
	for _, memberID := range order {
		workspaceRole, found, err := handler.workspaceMemberRole(c.Request.Context(), slug, memberID)
		if err != nil {
			handler.internalError(c, err)
			return
		}
		if !found {
			handler.notFound(c)
			return
		}
		requested := requestedRoles[memberID]
		if workspaceRole == roleAdmin && (requested == roleGuest || requested == roleMember) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "You cannot add a user with role lower than the workspace role"})
			return
		}
		if workspaceRole == roleGuest && (requested == roleMember || requested == roleAdmin) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "You cannot add a user with role higher than the workspace role"})
			return
		}
	}
	now := handler.clock().UTC()
	err = handler.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		// Existing rows are reactivated and re-roled, whether or not they were active.
		var existing []ProjectMember
		err := tx.Where("project_id = ? AND member_id IN ? AND deleted_at IS NULL", projectID, order).Find(&existing).Error
		if err != nil {
			return err
		}
		present := make(map[string]struct{}, len(existing))
		for _, member := range existing {
			present[member.MemberID] = struct{}{}
			err := tx.Model(&ProjectMember{}).Where("id = ?", member.ID).Updates(map[string]any{
				"role": requestedRoles[member.MemberID], "is_active": true,
			}).Error
			if err != nil {
				return err
			}
		}
		sortOrders, err := handler.minimumPropertySortOrders(tx, project.WorkspaceID, order)
		if err != nil {
			return err
		}
		newMembers := make([]ProjectMember, 0, len(order))
		newProperties := make([]ProjectUserProperty, 0, len(order))
		for _, memberID := range order {
			if _, exists := present[memberID]; !exists {
				memberRowID, err := newUUID()
				if err != nil {
					return err
				}
				// bulk_create bypasses ProjectMember.save, so created_by stays null
				// and the property below is written explicitly instead.
				newMembers = append(newMembers, ProjectMember{
					ID: memberRowID, CreatedAt: now, UpdatedAt: now,
					ProjectID: projectID, WorkspaceID: project.WorkspaceID, MemberID: memberID,
					Role: requestedRoles[memberID], ViewProps: defaultPropsJSON(),
					DefaultProps: defaultPropsJSON(), Preferences: defaultPreferencesJSON(),
					SortOrder: 65535, IsActive: true,
				})
			}
			propertyID, err := newUUID()
			if err != nil {
				return err
			}
			sortOrder := 65535.0
			if minimum, ok := sortOrders[memberID]; ok {
				sortOrder = minimum - 10000
			}
			newProperties = append(newProperties, ProjectUserProperty{
				ID: propertyID, CreatedAt: now, UpdatedAt: now,
				ProjectID: projectID, WorkspaceID: project.WorkspaceID, UserID: memberID,
				Filters: defaultFiltersJSON(), DisplayFilters: defaultDisplayFiltersJSON(),
				DisplayProperties: defaultDisplayPropertiesJSON(), RichFilters: emptyJSON(),
				Preferences: defaultPreferencesJSON(), SortOrder: sortOrder,
			})
		}
		if len(newMembers) > 0 {
			err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&newMembers).Error
			if err != nil {
				return err
			}
		}
		if len(newProperties) > 0 {
			return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&newProperties).Error
		}
		return nil
	})
	if err != nil {
		handler.internalError(c, err)
		return
	}
	var members []ProjectMember
	err = handler.db.WithContext(c.Request.Context()).
		Where("project_id = ? AND member_id IN ? AND deleted_at IS NULL", projectID, order).
		Order("created_at DESC").Find(&members).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if handler.tasks != nil {
		for _, member := range members {
			err := handler.tasks.PublishProjectAddUserEmail(c.Request.Context(), handler.origin(), member.ID, user.ID)
			if err != nil {
				handler.internalError(c, err)
				return
			}
		}
	}
	response := make([]gin.H, 0, len(members))
	for _, member := range members {
		response = append(response, projectMemberRoleJSON(member))
	}
	c.JSON(http.StatusCreated, response)
}

func (handler *Handler) memberRetrieve(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	requester, found, err := handler.activeProjectMember(c.Request.Context(), c.Param("slug"), c.Param("id"), user.ID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		handler.notFound(c)
		return
	}
	var member ProjectMember
	err = handler.db.WithContext(c.Request.Context()).
		Where(`id = ? AND project_id = ? AND workspace_id = (SELECT id FROM workspaces WHERE slug = ?)
			AND member_id IN (SELECT id FROM users WHERE is_bot = FALSE) AND is_active = TRUE AND deleted_at IS NULL`,
			c.Param("member"), c.Param("id"), c.Param("slug")).
		Order("created_at DESC").Take(&member).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "Project member not found"})
		return
	}
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if requester.Role > roleGuest {
		data, err := handler.projectMemberJSON(c.Request.Context(), member, true)
		if err != nil {
			handler.internalError(c, err)
			return
		}
		c.JSON(http.StatusOK, data)
		return
	}
	c.JSON(http.StatusOK, projectMemberRoleJSON(member))
}

func (handler *Handler) memberPatch(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	slug, projectID := c.Param("slug"), c.Param("id")
	var member ProjectMember
	err := handler.db.WithContext(c.Request.Context()).
		Where("id = ? AND project_id = ? AND workspace_id = (SELECT id FROM workspaces WHERE slug = ?) AND is_active = TRUE AND deleted_at IS NULL",
			c.Param("member"), projectID, slug).
		Order("created_at DESC").Take(&member).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		handler.notFound(c)
		return
	}
	if err != nil {
		handler.internalError(c, err)
		return
	}
	targetWorkspaceRole, found, err := handler.workspaceMemberRole(c.Request.Context(), slug, member.MemberID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		handler.notFound(c)
		return
	}
	requesterWorkspaceRole, found, err := handler.workspaceMemberRole(c.Request.Context(), slug, user.ID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		handler.notFound(c)
		return
	}
	isWorkspaceAdmin := requesterWorkspaceRole == roleAdmin
	if member.MemberID == user.ID && !isWorkspaceAdmin {
		c.JSON(http.StatusBadRequest, gin.H{"error": "You cannot update your own role"})
		return
	}
	requester, found, err := handler.activeProjectMember(c.Request.Context(), slug, projectID, user.ID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		handler.notFound(c)
		return
	}
	var body map[string]json.RawMessage
	if err := c.ShouldBindJSON(&body); err != nil {
		handler.invalidDetail(c)
		return
	}
	updates := map[string]any{}
	if raw, exists := body["role"]; exists {
		if requester.Role < roleAdmin && !isWorkspaceAdmin {
			c.JSON(http.StatusForbidden, gin.H{"error": "You do not have permission to update roles"})
			return
		}
		if member.Role >= requester.Role && !isWorkspaceAdmin {
			c.JSON(http.StatusForbidden, gin.H{"error": "You cannot update the role of a member with a role equal to or higher than your own"})
			return
		}
		newRole, err := handler.roleValue(raw)
		if err != nil {
			handler.invalidDetail(c)
			return
		}
		if newRole >= requester.Role && !isWorkspaceAdmin {
			c.JSON(http.StatusForbidden, gin.H{"error": "You cannot assign a role equal to or higher than your own"})
			return
		}
		if targetWorkspaceRole == roleGuest && (newRole == roleMember || newRole == roleAdmin) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "You cannot add a user with role higher than the workspace role"})
			return
		}
		if newRole != roleGuest && newRole != roleMember && newRole != roleAdmin {
			c.JSON(http.StatusBadRequest, gin.H{"role": []string{`"` + string(raw) + `" is not a valid choice.`}})
			return
		}
		updates["role"] = newRole
	}
	if raw, exists := body["is_active"]; exists {
		if requester.Role < roleAdmin && !isWorkspaceAdmin {
			c.JSON(http.StatusForbidden, gin.H{"error": "You do not have permission to update member status"})
			return
		}
		if member.Role >= requester.Role && !isWorkspaceAdmin {
			c.JSON(http.StatusForbidden, gin.H{"error": "You cannot update the status of a member with a role equal to or higher than your own"})
			return
		}
		var active bool
		if json.Unmarshal(raw, &active) != nil {
			c.JSON(http.StatusBadRequest, gin.H{"is_active": []string{"Must be a valid boolean."}})
			return
		}
		updates["is_active"] = active
	}
	for _, field := range []string{"view_props", "default_props", "preferences"} {
		raw, exists := body[field]
		if !exists {
			continue
		}
		if string(raw) == "null" || !json.Valid(raw) {
			c.JSON(http.StatusBadRequest, gin.H{field: []string{"This field may not be null."}})
			return
		}
		updates[field] = auth.JSONValue(append([]byte(nil), raw...))
	}
	if raw, exists := body["comment"]; exists {
		if string(raw) == "null" {
			updates["comment"] = nil
		} else {
			var comment string
			if json.Unmarshal(raw, &comment) != nil {
				c.JSON(http.StatusBadRequest, gin.H{"comment": []string{"Not a valid string."}})
				return
			}
			updates["comment"] = comment
		}
	}
	if raw, exists := body["sort_order"]; exists {
		var sortOrder float64
		if json.Unmarshal(raw, &sortOrder) != nil {
			c.JSON(http.StatusBadRequest, gin.H{"sort_order": []string{"A valid number is required."}})
			return
		}
		updates["sort_order"] = sortOrder
	}
	updates["updated_at"] = handler.clock().UTC()
	updates["updated_by_id"] = user.ID
	err = handler.db.WithContext(c.Request.Context()).Model(&ProjectMember{}).Where("id = ?", member.ID).Updates(updates).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if err := handler.db.WithContext(c.Request.Context()).Where("id = ?", member.ID).Take(&member).Error; err != nil {
		handler.internalError(c, err)
		return
	}
	data, err := handler.projectMemberJSON(c.Request.Context(), member, false)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, data)
}

func (handler *Handler) memberDelete(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin) {
		return
	}
	slug, projectID := c.Param("slug"), c.Param("id")
	var member ProjectMember
	err := handler.db.WithContext(c.Request.Context()).
		Where(`id = ? AND project_id = ? AND workspace_id = (SELECT id FROM workspaces WHERE slug = ?)
			AND member_id IN (SELECT id FROM users WHERE is_bot = FALSE) AND is_active = TRUE AND deleted_at IS NULL`,
			c.Param("member"), projectID, slug).
		Order("created_at DESC").Take(&member).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		handler.notFound(c)
		return
	}
	if err != nil {
		handler.internalError(c, err)
		return
	}
	requester, found, err := handler.activeProjectMember(c.Request.Context(), slug, projectID, user.ID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		handler.notFound(c)
		return
	}
	if member.ID == requester.ID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "You cannot remove yourself from the workspace. Please use leave workspace"})
		return
	}
	if requester.Role < member.Role {
		c.JSON(http.StatusBadRequest, gin.H{"error": "You cannot remove a user having role higher than you"})
		return
	}
	err = handler.deactivateProjectMember(c.Request.Context(), member.ID, user.ID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (handler *Handler) memberLeave(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	slug, projectID := c.Param("slug"), c.Param("id")
	member, found, err := handler.activeProjectMember(c.Request.Context(), slug, projectID, user.ID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		handler.notFound(c)
		return
	}
	if member.Role == roleAdmin {
		var admins int64
		err := handler.db.WithContext(c.Request.Context()).Model(&ProjectMember{}).
			Where("project_id = ? AND workspace_id = (SELECT id FROM workspaces WHERE slug = ?) AND role = ? AND is_active = TRUE AND deleted_at IS NULL",
				projectID, slug, roleAdmin).Count(&admins).Error
		if err != nil {
			handler.internalError(c, err)
			return
		}
		if admins <= 1 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "You cannot leave the project as your the only admin of the project you will have to either delete the project or create an another admin"})
			return
		}
	}
	if err := handler.deactivateProjectMember(c.Request.Context(), member.ID, user.ID); err != nil {
		handler.internalError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// memberMe is ProjectMemberUserEndpoint, which carries no role decorator and
// only needs the caller to have an active membership.
func (handler *Handler) memberMe(c *gin.Context, user *auth.User) {
	member, found, err := handler.activeProjectMember(c.Request.Context(), c.Param("slug"), c.Param("id"), user.ID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		handler.notFound(c)
		return
	}
	data, err := handler.projectMemberJSON(c.Request.Context(), member, false)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, data)
}

// memberViews is ProjectUserViewsEndpoint, which answers 403 rather than 404
// when the caller has no active membership.
func (handler *Handler) memberViews(c *gin.Context, user *auth.User) {
	slug, projectID := c.Param("slug"), c.Param("id")
	var count int64
	err := handler.db.WithContext(c.Request.Context()).Model(&Project{}).
		Where("id = ? AND workspace_id = (SELECT id FROM workspaces WHERE slug = ?) AND deleted_at IS NULL", projectID, slug).
		Count(&count).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if count == 0 {
		handler.notFound(c)
		return
	}
	member, found, err := handler.activeProjectMember(c.Request.Context(), slug, projectID, user.ID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		c.JSON(http.StatusForbidden, gin.H{"error": "Forbidden"})
		return
	}
	var body map[string]json.RawMessage
	if err := c.ShouldBindJSON(&body); err != nil {
		handler.invalidDetail(c)
		return
	}
	updates := map[string]any{
		"updated_at": handler.clock().UTC(), "updated_by_id": user.ID,
	}
	for _, field := range []string{"view_props", "default_props", "preferences"} {
		raw, exists := body[field]
		if !exists || !json.Valid(raw) || string(raw) == "null" {
			continue
		}
		updates[field] = auth.JSONValue(append([]byte(nil), raw...))
	}
	if raw, exists := body["sort_order"]; exists {
		var sortOrder float64
		if json.Unmarshal(raw, &sortOrder) == nil {
			updates["sort_order"] = sortOrder
		}
	}
	err = handler.db.WithContext(c.Request.Context()).Model(&ProjectMember{}).Where("id = ?", member.ID).Updates(updates).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (handler *Handler) memberPreferencesGet(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	member, found, err := handler.projectMemberByMemberID(c.Request.Context(), c.Param("slug"), c.Param("id"), c.Param("member"))
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		handler.notFound(c)
		return
	}
	c.JSON(http.StatusOK, projectMemberPreferenceJSON(member))
}

func (handler *Handler) memberPreferencesPatch(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	member, found, err := handler.projectMemberByMemberID(c.Request.Context(), c.Param("slug"), c.Param("id"), c.Param("member"))
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		handler.notFound(c)
		return
	}
	var incoming map[string]any
	if err := c.ShouldBindJSON(&incoming); err != nil {
		handler.invalidDetail(c)
		return
	}
	// validate_preferences merges the body into the stored value one level deep.
	merged := map[string]any{}
	if len(member.Preferences) > 0 {
		if json.Unmarshal(member.Preferences, &merged) != nil {
			merged = map[string]any{}
		}
	}
	for key, value := range incoming {
		merged[key] = value
	}
	encoded, err := json.Marshal(merged)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	err = handler.db.WithContext(c.Request.Context()).Model(&ProjectMember{}).Where("id = ?", member.ID).Updates(map[string]any{
		"preferences": auth.JSONValue(encoded),
		"updated_at":  handler.clock().UTC(), "updated_by_id": user.ID,
	}).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"preferences": merged})
}

func (handler *Handler) userProjectRoles(c *gin.Context, user *auth.User) {
	role, err := handler.workspaceRole(c.Request.Context(), c.Param("slug"), user.ID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if role == 0 {
		c.JSON(http.StatusForbidden, gin.H{"detail": "You do not have permission to perform this action."})
		return
	}
	var rows []struct {
		ProjectID string `gorm:"column:project_id"`
		Role      int    `gorm:"column:role"`
	}
	err = handler.db.WithContext(c.Request.Context()).Table("project_members pm").
		Joins("JOIN workspaces w ON w.id = pm.workspace_id").
		Where(`w.slug = ? AND pm.member_id = ? AND pm.is_active = TRUE AND pm.deleted_at IS NULL
			AND EXISTS (SELECT 1 FROM workspace_members wm WHERE wm.member_id = pm.member_id AND wm.workspace_id = w.id AND wm.is_active = TRUE AND wm.deleted_at IS NULL)`,
			c.Param("slug"), user.ID).
		Select("pm.project_id, pm.role").Scan(&rows).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	response := make(gin.H, len(rows))
	for _, row := range rows {
		response[row.ProjectID] = row.Role
	}
	c.JSON(http.StatusOK, response)
}

// requireProjectRole is allow_permission at its default PROJECT level: the
// caller needs one of the roles on the project, or an active membership plus
// the workspace admin role.
func (handler *Handler) requireProjectRole(c *gin.Context, user *auth.User, allowed ...int) bool {
	member, found, err := handler.activeProjectMember(c.Request.Context(), c.Param("slug"), c.Param("id"), user.ID)
	if err != nil {
		handler.internalError(c, err)
		return false
	}
	if found {
		for _, candidate := range allowed {
			if member.Role == candidate {
				return true
			}
		}
		workspaceRole, _, err := handler.workspaceMemberRole(c.Request.Context(), c.Param("slug"), user.ID)
		if err != nil {
			handler.internalError(c, err)
			return false
		}
		if workspaceRole == roleAdmin {
			return true
		}
	}
	c.JSON(http.StatusForbidden, gin.H{"error": "You don't have the required permissions."})
	return false
}

func (handler *Handler) activeProjectMember(ctx context.Context, slug, projectID, memberID string) (ProjectMember, bool, error) {
	var member ProjectMember
	err := handler.db.WithContext(ctx).
		Where("project_id = ? AND workspace_id = (SELECT id FROM workspaces WHERE slug = ?) AND member_id = ? AND is_active = TRUE AND deleted_at IS NULL",
			projectID, slug, memberID).
		Order("created_at DESC").Take(&member).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ProjectMember{}, false, nil
	}
	return member, err == nil, err
}

// projectMemberByMemberID backs the preference routes, which look the row up by
// member instead of primary key and do not filter on is_active.
func (handler *Handler) projectMemberByMemberID(ctx context.Context, slug, projectID, memberID string) (ProjectMember, bool, error) {
	var member ProjectMember
	err := handler.db.WithContext(ctx).
		Where("project_id = ? AND member_id = ? AND workspace_id = (SELECT id FROM workspaces WHERE slug = ?) AND deleted_at IS NULL",
			projectID, memberID, slug).
		Order("created_at DESC").Take(&member).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ProjectMember{}, false, nil
	}
	return member, err == nil, err
}

func (handler *Handler) activeProjectMembers(ctx context.Context, slug, projectID string) ([]ProjectMember, error) {
	var members []ProjectMember
	err := handler.db.WithContext(ctx).
		Where(`project_id = ? AND workspace_id = (SELECT id FROM workspaces WHERE slug = ?)
			AND member_id IN (SELECT id FROM users WHERE is_bot = FALSE)
			AND is_active = TRUE AND deleted_at IS NULL
			AND EXISTS (SELECT 1 FROM workspace_members wm JOIN workspaces w2 ON w2.id = wm.workspace_id
				WHERE wm.member_id = project_members.member_id AND w2.slug = ? AND wm.is_active = TRUE AND wm.deleted_at IS NULL)`,
			projectID, slug, slug).
		Order("created_at DESC").Find(&members).Error
	return members, err
}

func (handler *Handler) workspaceMemberRole(ctx context.Context, slug, memberID string) (int, bool, error) {
	var role *int
	err := handler.db.WithContext(ctx).Table("workspace_members wm").
		Joins("JOIN workspaces w ON w.id = wm.workspace_id").
		Where("w.slug = ? AND wm.member_id = ? AND wm.is_active = TRUE AND wm.deleted_at IS NULL", slug, memberID).
		Select("wm.role").Limit(1).Scan(&role).Error
	if err != nil || role == nil {
		return 0, false, err
	}
	return *role, true, nil
}

func (handler *Handler) minimumPropertySortOrders(tx *gorm.DB, workspaceID string, memberIDs []string) (map[string]float64, error) {
	var rows []struct {
		UserID    string  `gorm:"column:user_id"`
		SortOrder float64 `gorm:"column:min_sort_order"`
	}
	err := tx.Table("project_user_properties").
		Where("workspace_id = ? AND user_id IN ? AND deleted_at IS NULL", workspaceID, memberIDs).
		Select("user_id, MIN(sort_order) AS min_sort_order").Group("user_id").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	result := make(map[string]float64, len(rows))
	for _, row := range rows {
		result[row.UserID] = row.SortOrder
	}
	return result, nil
}

func (handler *Handler) deactivateProjectMember(ctx context.Context, memberRowID, actorID string) error {
	return handler.db.WithContext(ctx).Model(&ProjectMember{}).Where("id = ?", memberRowID).Updates(map[string]any{
		"is_active": false, "updated_at": handler.clock().UTC(), "updated_by_id": actorID,
	}).Error
}

func (handler *Handler) roleValue(raw json.RawMessage) (int, error) {
	var number float64
	if json.Unmarshal(raw, &number) == nil {
		return int(number), nil
	}
	var text string
	if json.Unmarshal(raw, &text) != nil {
		return 0, errors.New("invalid role")
	}
	var parsed int
	if _, err := fmt.Sscanf(text, "%d", &parsed); err != nil {
		return 0, errors.New("invalid role")
	}
	return parsed, nil
}
