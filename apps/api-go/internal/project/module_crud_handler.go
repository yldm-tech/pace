package project

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"github.com/yldm-tech/pace/apps/api-go/internal/drf"
	"gorm.io/gorm"
)

func (handler *Handler) registerModuleCRUDRoutes(router gin.IRouter) {
	router.GET("/api/workspaces/:slug/projects/:id/modules/:module/", handler.authenticated(handler.moduleRetrieve))
	router.PATCH("/api/workspaces/:slug/projects/:id/modules/:module/", handler.authenticated(handler.moduleUpdate))
	router.PUT("/api/workspaces/:slug/projects/:id/modules/:module/", handler.authenticated(handler.fullUpdate("module", handler.moduleUpdate)))
	router.DELETE("/api/workspaces/:slug/projects/:id/modules/:module/", handler.authenticated(handler.moduleDestroy))
	router.POST("/api/workspaces/:slug/projects/:id/modules/:module/archive/", handler.authenticated(handler.moduleArchive))
	router.DELETE("/api/workspaces/:slug/projects/:id/modules/:module/archive/", handler.authenticated(handler.moduleUnarchive))
}

func (handler *Handler) moduleRetrieve(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember) {
		return
	}
	location, err := userLocation(user)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	rows, err := handler.moduleRows(c, user.ID, c.Param("module"))
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if len(rows) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Module not found"})
		return
	}
	if handler.tasks != nil {
		err := handler.tasks.PublishRecentVisit(c.Request.Context(), "module", c.Param("module"), user.ID, c.Param("id"), c.Param("slug"))
		if err != nil {
			handler.internalError(c, err)
			return
		}
	}
	drf.Respond(c, http.StatusOK, moduleListJSON(rows[0], location))
}

// moduleUpdate refuses an archived module outright. Unlike a cycle there is no completed rule: a finished module can still be edited.
func (handler *Handler) moduleUpdate(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember) {
		return
	}
	slug, projectID, moduleID := c.Param("slug"), c.Param("id"), c.Param("module")
	module, found, err := handler.moduleByID(c, slug, projectID, moduleID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "Module not found"})
		return
	}
	if module.ArchivedAt != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Archived module cannot be updated"})
		return
	}

	var body map[string]json.RawMessage
	if err := c.ShouldBindBodyWithJSON(&body); err != nil {
		handler.invalidDetail(c)
		return
	}
	values, members, ok := handler.moduleFields(c, body, false)
	if !ok {
		return
	}
	snapshot, err := json.Marshal(moduleWriteJSON(module))
	if err != nil {
		handler.internalError(c, err)
		return
	}
	currentInstance := string(snapshot)

	now := handler.clock().UTC()
	_, membersGiven := body["member_ids"]
	err = handler.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if len(values) > 0 {
			values["updated_at"] = now
			values["updated_by_id"] = user.ID
			if err := tx.Model(&Module{}).Where("id = ?", moduleID).Updates(values).Error; err != nil {
				return err
			}
		}
		if !membersGiven {
			return nil
		}
		// The member set is replaced rather than merged: the existing rows are soft deleted and the new ones inserted.
		err := tx.Model(&ModuleMember{}).Where("module_id = ? AND deleted_at IS NULL", moduleID).
			Update("deleted_at", now).Error
		if err != nil {
			return err
		}
		rows := make([]ModuleMember, 0, len(members))
		for _, memberID := range members {
			rowID, err := newUUID()
			if err != nil {
				return err
			}
			rows = append(rows, ModuleMember{
				ID: rowID, CreatedAt: now, UpdatedAt: now, CreatedByID: &user.ID, UpdatedByID: &user.ID,
				ProjectID: projectID, WorkspaceID: module.WorkspaceID,
				ModuleID: moduleID, MemberID: memberID,
			})
		}
		if len(rows) == 0 {
			return nil
		}
		return tx.Create(&rows).Error
	})
	if err != nil {
		handler.internalError(c, err)
		return
	}

	if handler.tasks != nil {
		err := handler.tasks.PublishModelActivity(c.Request.Context(), "module", moduleID,
			decodeRawFields(body), &currentInstance, user.ID, slug, handler.origin())
		if err != nil {
			handler.internalError(c, err)
			return
		}
	}

	location, err := userLocation(user)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	rows, err := handler.moduleRows(c, user.ID, moduleID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if len(rows) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Module not found"})
		return
	}
	drf.Respond(c, http.StatusOK, moduleListJSON(rows[0], location))
}

// moduleDestroy sends one activity per issue the module held before the module goes, since each of those issues loses a module.
func (handler *Handler) moduleDestroy(c *gin.Context, user *auth.User) {
	slug, projectID, moduleID := c.Param("slug"), c.Param("id"), c.Param("module")
	module, found, err := handler.moduleByID(c, slug, projectID, moduleID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "The required object does not exist."})
		return
	}
	if !handler.requireModuleAdminOrCreator(c, user, module) {
		return
	}

	var issueIDs []string
	err = handler.db.WithContext(c.Request.Context()).Table("module_issues").
		Where("module_id = ? AND deleted_at IS NULL", moduleID).Pluck("issue_id", &issueIDs).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	now := handler.clock().UTC()
	requested, err := json.Marshal(map[string]any{"module_id": moduleID})
	if err != nil {
		handler.internalError(c, err)
		return
	}
	current, err := json.Marshal(map[string]any{"module_name": module.Name})
	if err != nil {
		handler.internalError(c, err)
		return
	}
	requestedData, currentInstance := string(requested), string(current)
	for _, issueID := range issueIDs {
		err := handler.publishIssueActivity(c, issueActivity{
			Type: "module.activity.deleted", RequestedData: &requestedData, CurrentInstance: &currentInstance,
			ActorID: user.ID, IssueID: issueID, ProjectID: projectID,
			Notification: true, Origin: handler.origin(), Epoch: now,
		})
		if err != nil {
			handler.internalError(c, err)
			return
		}
	}

	err = handler.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&Module{}).Where("id = ?", moduleID).Update("deleted_at", now).Error; err != nil {
			return err
		}
		err := tx.Model(&ModuleIssue{}).Where("module_id = ? AND project_id = ? AND deleted_at IS NULL", moduleID, projectID).
			Update("deleted_at", now).Error
		if err != nil {
			return err
		}
		err = tx.Model(&UserFavorite{}).
			Where("user_id = ? AND entity_type = 'module' AND entity_identifier = ? AND project_id = ? AND deleted_at IS NULL",
				user.ID, moduleID, projectID).Update("deleted_at", now).Error
		if err != nil {
			return err
		}
		return tx.Exec(
			`DELETE FROM user_recent_visits WHERE project_id = ? AND entity_identifier = ? AND entity_name = 'module'
			 AND workspace_id = (SELECT id FROM workspaces WHERE slug = ?)`,
			projectID, moduleID, slug,
		).Error
	})
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if handler.tasks != nil {
		if err := handler.tasks.PublishSoftDeleteRelatedObjects(c.Request.Context(), "db", "module", moduleID); err != nil {
			handler.internalError(c, err)
			return
		}
	}
	c.Status(http.StatusNoContent)
}

// moduleArchive puts a finished module away. Unlike a cycle, which is judged by its end date, a module is judged by its status.
func (handler *Handler) moduleArchive(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember) {
		return
	}
	slug, projectID, moduleID := c.Param("slug"), c.Param("id"), c.Param("module")
	module, found, err := handler.moduleByID(c, slug, projectID, moduleID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "The required object does not exist."})
		return
	}
	if module.Status != "completed" && module.Status != "cancelled" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Only completed or cancelled modules can be archived"})
		return
	}
	now := handler.clock().UTC()
	err = handler.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		err := tx.Model(&Module{}).Where("id = ?", moduleID).
			Updates(map[string]any{"archived_at": now, "updated_at": now}).Error
		if err != nil {
			return err
		}
		// Every member's favourite goes, not only the caller's.
		return tx.Model(&UserFavorite{}).
			Where(`entity_type = 'module' AND entity_identifier = ? AND project_id = ? AND deleted_at IS NULL
				AND workspace_id = (SELECT id FROM workspaces WHERE slug = ?)`, moduleID, projectID, slug).
			Update("deleted_at", now).Error
	})
	if err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, gin.H{"archived_at": now})
}

func (handler *Handler) moduleUnarchive(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember) {
		return
	}
	slug, projectID, moduleID := c.Param("slug"), c.Param("id"), c.Param("module")
	_, found, err := handler.moduleByID(c, slug, projectID, moduleID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "The required object does not exist."})
		return
	}
	now := handler.clock().UTC()
	var cleared any
	err = handler.db.WithContext(c.Request.Context()).Model(&Module{}).Where("id = ?", moduleID).
		Updates(map[string]any{"archived_at": cleared, "updated_at": now}).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (handler *Handler) moduleByID(c *gin.Context, slug, projectID, moduleID string) (Module, bool, error) {
	var module Module
	err := handler.db.WithContext(c.Request.Context()).Table("modules m").
		Joins("JOIN workspaces w ON w.id = m.workspace_id").
		Where("m.id = ? AND m.project_id = ? AND w.slug = ? AND m.deleted_at IS NULL", moduleID, projectID, slug).
		Take(&module).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Module{}, false, nil
	}
	if err != nil {
		return Module{}, false, err
	}
	return module, true, nil
}

// moduleWriteJSON is ModuleSerializer over the instance, which is what the update snapshot carries. The annotated counts are absent from it.
func moduleWriteJSON(module Module) gin.H {
	return gin.H{
		"id": module.ID, "created_at": module.CreatedAt, "updated_at": module.UpdatedAt,
		"created_by": module.CreatedByID, "updated_by": module.UpdatedByID, "deleted_at": module.DeletedAt,
		"name": module.Name, "description": module.Description,
		"description_text": decodeJSON(module.DescriptionText), "description_html": decodeJSON(module.DescriptionHTML),
		"start_date": dateOnly(module.StartDate), "target_date": dateOnly(module.TargetDate),
		"status": module.Status, "lead": module.LeadID,
		"view_props": decodeJSON(module.ViewProps), "sort_order": module.SortOrder,
		"external_source": module.ExternalSource, "external_id": module.ExternalID,
		"archived_at": module.ArchivedAt, "logo_props": decodeJSON(module.LogoProps),
		"project": module.ProjectID, "workspace": module.WorkspaceID,
	}
}

// requireModuleAdminOrCreator is allow_permission([ADMIN], creator=True, model=Module).
func (handler *Handler) requireModuleAdminOrCreator(c *gin.Context, user *auth.User, module Module) bool {
	member, found, err := handler.activeProjectMember(c.Request.Context(), c.Param("slug"), c.Param("id"), user.ID)
	if err != nil {
		handler.internalError(c, err)
		return false
	}
	if found {
		if module.CreatedByID != nil && *module.CreatedByID == user.ID {
			return true
		}
		if member.Role == roleAdmin {
			return true
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
