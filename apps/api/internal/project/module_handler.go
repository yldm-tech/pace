package project

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api/internal/auth"
	"github.com/yldm-tech/pace/apps/api/internal/drf"
	"github.com/yldm-tech/pace/apps/api/internal/validate"
	"gorm.io/gorm"
)

func (handler *Handler) registerModuleBasicsRoutes(router gin.IRouter) {
	router.POST("/api/workspaces/:slug/projects/:id/user-favorite-modules/", handler.authenticated(handler.moduleFavoriteCreate))
	router.GET("/api/workspaces/:slug/projects/:id/user-favorite-modules/", handler.authenticated(handler.moduleFavoriteList))
	router.DELETE("/api/workspaces/:slug/projects/:id/user-favorite-modules/:module/", handler.authenticated(handler.moduleFavoriteDestroy))
	router.GET("/api/workspaces/:slug/projects/:id/modules/:module/user-properties/", handler.authenticated(handler.moduleUserProperties))
	router.PATCH("/api/workspaces/:slug/projects/:id/modules/:module/user-properties/", handler.authenticated(handler.updateModuleUserProperties))
	router.GET("/api/workspaces/:slug/projects/:id/modules/:module/module-links/", handler.authenticated(handler.moduleLinkList))
	router.POST("/api/workspaces/:slug/projects/:id/modules/:module/module-links/", handler.authenticated(handler.moduleLinkCreate))
	router.GET("/api/workspaces/:slug/projects/:id/modules/:module/module-links/:link/", handler.authenticated(handler.moduleLinkRetrieve))
	router.PATCH("/api/workspaces/:slug/projects/:id/modules/:module/module-links/:link/", handler.authenticated(handler.moduleLinkPatch))
	router.PUT("/api/workspaces/:slug/projects/:id/modules/:module/module-links/:link/", handler.authenticated(handler.fullUpdate("link", handler.moduleLinkPatch)))
	router.DELETE("/api/workspaces/:slug/projects/:id/modules/:module/module-links/:link/", handler.authenticated(handler.moduleLinkDestroy))
}

// moduleFavoriteCreate marks a module as one of the caller's own. It is the module twin of the cycle route, down to writing unconditionally.
func (handler *Handler) moduleFavoriteCreate(c *gin.Context, user *auth.User) {
	if !handler.requireProjectMembership(c, user) {
		return
	}
	var request struct {
		Module *string `json:"module"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		handler.invalidDetail(c)
		return
	}
	if !handler.createFavorite(c, user, "module", request.Module) {
		return
	}
	c.Status(http.StatusNoContent)
}

// moduleFavoriteList is broken upstream the same way the cycle one is: the viewset inherits DRF's list but declares no serializer_class, so get_serializer_class asserts and answers 500.
func (handler *Handler) moduleFavoriteList(c *gin.Context, user *auth.User) {
	if !handler.requireProjectMembership(c, user) {
		return
	}
	handler.internalError(c, errFavouriteListHasNoSerializer)
}

func (handler *Handler) moduleFavoriteDestroy(c *gin.Context, user *auth.User) {
	if !handler.requireProjectMembership(c, user) {
		return
	}
	handler.destroyFavorite(c, user, "module", c.Param("module"))
}

func (handler *Handler) moduleUserProperties(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	properties, err := handler.ensureModuleUserProperties(c, user)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, moduleUserPropertiesJSON(properties))
}

// updateModuleUserProperties answers 201, the same way the cycle one does, even though it creates nothing.
func (handler *Handler) updateModuleUserProperties(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	var properties ModuleUserProperties
	err := handler.db.WithContext(c.Request.Context()).Table("module_user_properties p").
		Joins("JOIN workspaces w ON w.id = p.workspace_id").
		Where("p.user_id = ? AND p.module_id = ? AND p.project_id = ? AND w.slug = ? AND p.deleted_at IS NULL",
			user.ID, c.Param("module"), c.Param("id"), c.Param("slug")).
		Take(&properties).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "The required object does not exist."})
		return
	}
	if err != nil {
		handler.internalError(c, err)
		return
	}
	var body map[string]json.RawMessage
	if err := c.ShouldBindJSON(&body); err != nil {
		handler.invalidDetail(c)
		return
	}
	updates := map[string]any{}
	for _, field := range []string{"filters", "rich_filters", "display_filters", "display_properties"} {
		raw, present := body[field]
		if !present || !json.Valid(raw) {
			continue
		}
		updates[field] = auth.JSONValue(raw)
	}
	if len(updates) > 0 {
		updates["updated_at"] = handler.clock().UTC()
		err := handler.db.WithContext(c.Request.Context()).Model(&ModuleUserProperties{}).
			Where("id = ?", properties.ID).Updates(updates).Error
		if err != nil {
			handler.internalError(c, err)
			return
		}
		if err := handler.db.WithContext(c.Request.Context()).Where("id = ?", properties.ID).Take(&properties).Error; err != nil {
			handler.internalError(c, err)
			return
		}
	}
	drf.Respond(c, http.StatusCreated, moduleUserPropertiesJSON(properties))
}

func (handler *Handler) ensureModuleUserProperties(c *gin.Context, user *auth.User) (ModuleUserProperties, error) {
	slug, projectID, moduleID := c.Param("slug"), c.Param("id"), c.Param("module")
	var properties ModuleUserProperties
	err := handler.db.WithContext(c.Request.Context()).Table("module_user_properties p").
		Joins("JOIN workspaces w ON w.id = p.workspace_id").
		Where("p.user_id = ? AND p.module_id = ? AND p.project_id = ? AND w.slug = ? AND p.deleted_at IS NULL",
			user.ID, moduleID, projectID, slug).Take(&properties).Error
	if err == nil {
		return properties, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return ModuleUserProperties{}, err
	}
	var workspaceIDs []string
	err = handler.db.WithContext(c.Request.Context()).Table("projects").
		Where("id = ?", projectID).Limit(1).Pluck("workspace_id", &workspaceIDs).Error
	if err != nil {
		return ModuleUserProperties{}, err
	}
	if len(workspaceIDs) == 0 {
		return ModuleUserProperties{}, gorm.ErrRecordNotFound
	}
	propertiesID, err := newUUID()
	if err != nil {
		return ModuleUserProperties{}, err
	}
	now := handler.clock().UTC()
	properties = ModuleUserProperties{
		ID: propertiesID, CreatedAt: now, UpdatedAt: now,
		ProjectID: projectID, WorkspaceID: workspaceIDs[0], ModuleID: moduleID, UserID: user.ID,
		Filters: defaultFiltersJSON(), DisplayFilters: defaultDisplayFiltersJSON(),
		DisplayProperties: defaultDisplayPropertiesJSON(), RichFilters: emptyJSON(),
	}
	if err := handler.db.WithContext(c.Request.Context()).Create(&properties).Error; err != nil {
		return ModuleUserProperties{}, err
	}
	return properties, nil
}

// moduleUserPropertiesJSON is ModuleUserPropertiesSerializer: fields = "__all__" with the four relations read-only.
func moduleUserPropertiesJSON(properties ModuleUserProperties) gin.H {
	return gin.H{
		"id": properties.ID, "created_at": properties.CreatedAt, "updated_at": properties.UpdatedAt,
		"created_by": properties.CreatedByID, "updated_by": properties.UpdatedByID,
		"deleted_at": properties.DeletedAt,
		"filters":    decodeJSON(properties.Filters), "display_filters": decodeJSON(properties.DisplayFilters),
		"display_properties": decodeJSON(properties.DisplayProperties),
		"rich_filters":       decodeJSON(properties.RichFilters),
		"project":            properties.ProjectID, "workspace": properties.WorkspaceID,
		"module": properties.ModuleID, "user": properties.UserID,
	}
}

// moduleLinkList returns a module's links, newest first. The queryset also requires the caller to be an active member of a project that is not archived.
func (handler *Handler) moduleLinkList(c *gin.Context, user *auth.User) {
	if !handler.requireProjectMembership(c, user) {
		return
	}
	var links []ModuleLink
	err := handler.moduleLinkScope(c, user).Order("l.created_at DESC").Find(&links).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	serialized := make([]gin.H, 0, len(links))
	for _, link := range links {
		serialized = append(serialized, moduleLinkJSON(link))
	}
	drf.Respond(c, http.StatusOK, serialized)
}

func (handler *Handler) moduleLinkRetrieve(c *gin.Context, user *auth.User) {
	if !handler.requireProjectMembership(c, user) {
		return
	}
	var link ModuleLink
	err := handler.moduleLinkScope(c, user).Where("l.id = ?", c.Param("link")).Take(&link).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		handler.notFound(c)
		return
	}
	if err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, moduleLinkJSON(link))
}

func (handler *Handler) moduleLinkCreate(c *gin.Context, user *auth.User) {
	if !handler.requireProjectMembership(c, user) {
		return
	}
	slug, projectID, moduleID := c.Param("slug"), c.Param("id"), c.Param("module")
	var body map[string]json.RawMessage
	if err := c.ShouldBindJSON(&body); err != nil {
		handler.invalidDetail(c)
		return
	}
	values, ok := handler.moduleLinkFields(c, body, true)
	if !ok {
		return
	}
	var workspaceIDs []string
	err := handler.db.WithContext(c.Request.Context()).Table("workspaces").
		Where("slug = ?", slug).Limit(1).Pluck("id", &workspaceIDs).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if len(workspaceIDs) == 0 {
		handler.notFound(c)
		return
	}
	linkID, err := newUUID()
	if err != nil {
		handler.internalError(c, err)
		return
	}
	now := handler.clock().UTC()
	values["id"] = linkID
	values["created_at"] = now
	values["updated_at"] = now
	values["created_by_id"] = user.ID
	values["updated_by_id"] = user.ID
	values["project_id"] = projectID
	values["workspace_id"] = workspaceIDs[0]
	values["module_id"] = moduleID
	if _, given := values["metadata"]; !given {
		values["metadata"] = emptyJSON()
	}
	if err := handler.db.WithContext(c.Request.Context()).Table("module_links").Create(values).Error; err != nil {
		handler.internalError(c, err)
		return
	}
	var created ModuleLink
	if err := handler.db.WithContext(c.Request.Context()).Where("id = ?", linkID).Take(&created).Error; err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusCreated, moduleLinkJSON(created))
}

func (handler *Handler) moduleLinkPatch(c *gin.Context, user *auth.User) {
	if !handler.requireProjectMembership(c, user) {
		return
	}
	var link ModuleLink
	err := handler.moduleLinkScope(c, user).Where("l.id = ?", c.Param("link")).Take(&link).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		handler.notFound(c)
		return
	}
	if err != nil {
		handler.internalError(c, err)
		return
	}
	var body map[string]json.RawMessage
	if err := c.ShouldBindBodyWithJSON(&body); err != nil {
		handler.invalidDetail(c)
		return
	}
	values, ok := handler.moduleLinkFields(c, body, false)
	if !ok {
		return
	}
	if len(values) > 0 {
		values["updated_at"] = handler.clock().UTC()
		values["updated_by_id"] = user.ID
		err := handler.db.WithContext(c.Request.Context()).Model(&ModuleLink{}).
			Where("id = ?", link.ID).Updates(values).Error
		if err != nil {
			handler.internalError(c, err)
			return
		}
		if err := handler.db.WithContext(c.Request.Context()).Where("id = ?", link.ID).Take(&link).Error; err != nil {
			handler.internalError(c, err)
			return
		}
	}
	drf.Respond(c, http.StatusOK, moduleLinkJSON(link))
}

func (handler *Handler) moduleLinkDestroy(c *gin.Context, user *auth.User) {
	if !handler.requireProjectMembership(c, user) {
		return
	}
	var link ModuleLink
	err := handler.moduleLinkScope(c, user).Where("l.id = ?", c.Param("link")).Take(&link).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		handler.notFound(c)
		return
	}
	if err != nil {
		handler.internalError(c, err)
		return
	}
	now := handler.clock().UTC()
	err = handler.db.WithContext(c.Request.Context()).Model(&ModuleLink{}).Where("id = ?", link.ID).
		Updates(map[string]any{"deleted_at": now, "updated_at": now}).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// moduleLinkScope is the queryset the link routes share: the module's links, within a project the caller actively belongs to and which is not archived.
func (handler *Handler) moduleLinkScope(c *gin.Context, user *auth.User) *gorm.DB {
	return handler.db.WithContext(c.Request.Context()).Table("module_links l").
		Joins("JOIN workspaces w ON w.id = l.workspace_id").
		Joins("JOIN projects p ON p.id = l.project_id AND p.archived_at IS NULL").
		Joins("JOIN project_members pm ON pm.project_id = l.project_id AND pm.member_id = ? AND pm.is_active = TRUE", user.ID).
		Where("w.slug = ? AND l.project_id = ? AND l.module_id = ? AND l.deleted_at IS NULL",
			c.Param("slug"), c.Param("id"), c.Param("module"))
}

// moduleLinkFields is ModuleLinkSerializer's writable set. The url goes through Django's own validator, which is stricter than a bare parse.
func (handler *Handler) moduleLinkFields(c *gin.Context, body map[string]json.RawMessage, creating bool) (map[string]any, bool) {
	values := map[string]any{}
	if raw, present := body["url"]; present {
		var value string
		if json.Unmarshal(raw, &value) != nil {
			c.JSON(http.StatusBadRequest, gin.H{"url": []string{"Not a valid string."}})
			return nil, false
		}
		value = validate.PrefixScheme(value)
		if !validate.URL(value) {
			c.JSON(http.StatusBadRequest, gin.H{"url": []string{"Enter a valid URL."}})
			return nil, false
		}
		values["url"] = value
	} else if creating {
		c.JSON(http.StatusBadRequest, gin.H{"url": []string{"This field is required."}})
		return nil, false
	}
	if raw, present := body["title"]; present {
		if string(raw) == "null" {
			values["title"] = nil
		} else {
			var value string
			if json.Unmarshal(raw, &value) != nil {
				c.JSON(http.StatusBadRequest, gin.H{"title": []string{"Not a valid string."}})
				return nil, false
			}
			values["title"] = value
		}
	}
	if raw, present := body["metadata"]; present && json.Valid(raw) && string(raw) != "null" {
		values["metadata"] = auth.JSONValue(raw)
	}
	return values, true
}

// moduleLinkJSON is ModuleLinkSerializer: fields = "__all__" with the three relations read-only.
func moduleLinkJSON(link ModuleLink) gin.H {
	return gin.H{
		"id": link.ID, "created_at": link.CreatedAt, "updated_at": link.UpdatedAt,
		"created_by": link.CreatedByID, "updated_by": link.UpdatedByID, "deleted_at": link.DeletedAt,
		"title": link.Title, "url": link.URL, "metadata": decodeJSON(link.Metadata),
		"project": link.ProjectID, "workspace": link.WorkspaceID, "module": link.ModuleID,
	}
}
