package project

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"github.com/yldm-tech/pace/apps/api-go/internal/drf"
	"gorm.io/gorm"
)

func (handler *Handler) registerIssueMetaRoutes(router gin.IRouter) {
	router.GET("/api/workspaces/:slug/projects/:id/issues/:issue/meta/", handler.authenticatedIssueUUID(handler.issueMeta))
	router.GET("/api/workspaces/:slug/projects/:id/user-properties/", handler.authenticated(handler.projectUserProperties))
	router.PATCH("/api/workspaces/:slug/projects/:id/user-properties/", handler.authenticated(handler.updateProjectUserProperties))
	router.DELETE("/api/workspaces/:slug/projects/:id/bulk-delete-issues/", handler.authenticated(handler.bulkDeleteIssues))
	router.GET("/api/workspaces/:slug/projects/:id/deleted-issues/", handler.authenticated(handler.deletedIssues))
}

// issueMeta is what the issue page reads to build the human-facing identifier before it has the issue itself.
func (handler *Handler) issueMeta(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	var row struct {
		SequenceID int    `gorm:"column:sequence_id"`
		Identifier string `gorm:"column:identifier"`
	}
	err := handler.db.WithContext(c.Request.Context()).Table("issues i").
		Select("i.sequence_id, p.identifier").
		Joins("JOIN projects p ON p.id = i.project_id").
		Joins("JOIN workspaces w ON w.id = i.workspace_id").
		Where("i.id = ? AND i.project_id = ? AND w.slug = ?", c.Param("issue"), c.Param("id"), c.Param("slug")).
		Where(issueObjectsPredicate("i")).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// The unguarded .get raises DoesNotExist, which BaseAPIView turns into this 404.
		c.JSON(http.StatusNotFound, gin.H{"error": "The required object does not exist."})
		return
	}
	if err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, gin.H{"sequence_id": row.SequenceID, "project_identifier": row.Identifier})
}

// projectUserProperties reads the caller's own view preferences for a project, creating the row on first read the way get_or_create does.
func (handler *Handler) projectUserProperties(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	property, err := handler.ensureProjectUserProperty(c, user)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, projectUserPropertyJSON(property))
}

func (handler *Handler) updateProjectUserProperties(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	property, err := handler.ensureProjectUserProperty(c, user)
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
	// The serializer is fields = "__all__" with the three relations read-only, so only the five preference columns and the sort order are writable.
	for _, field := range []string{"filters", "display_filters", "display_properties", "rich_filters", "preferences"} {
		raw, present := body[field]
		if !present {
			continue
		}
		if !json.Valid(raw) || string(raw) == "null" {
			c.JSON(http.StatusBadRequest, gin.H{field: []string{"This field may not be null."}})
			return
		}
		updates[field] = auth.JSONValue(raw)
	}
	if raw, present := body["sort_order"]; present {
		var value float64
		if json.Unmarshal(raw, &value) != nil {
			c.JSON(http.StatusBadRequest, gin.H{"sort_order": []string{"A valid number is required."}})
			return
		}
		updates["sort_order"] = value
	}
	if len(updates) > 0 {
		now := handler.clock().UTC()
		updates["updated_at"] = now
		// The serializer's save writes the instance, so updated_by moves to the caller.
		updates["updated_by_id"] = user.ID
		err := handler.db.WithContext(c.Request.Context()).Model(&ProjectUserProperty{}).
			Where("id = ?", property.ID).Updates(updates).Error
		if err != nil {
			handler.internalError(c, err)
			return
		}
		// Re-read rather than patching the struct in place, so the response is what the row now holds.
		if property, err = handler.projectUserPropertyByID(c, property.ID); err != nil {
			handler.internalError(c, err)
			return
		}
	}
	drf.Respond(c, http.StatusOK, projectUserPropertyJSON(property))
}

// ensureProjectUserProperty is get_or_create. The row is normally seeded when the member joins, so this only fires for a membership that predates it.
func (handler *Handler) ensureProjectUserProperty(c *gin.Context, user *auth.User) (ProjectUserProperty, error) {
	projectID := c.Param("id")
	var property ProjectUserProperty
	err := handler.db.WithContext(c.Request.Context()).
		Where("user_id = ? AND project_id = ? AND deleted_at IS NULL", user.ID, projectID).
		Take(&property).Error
	if err == nil {
		return property, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return ProjectUserProperty{}, err
	}
	var workspaceIDs []string
	err = handler.db.WithContext(c.Request.Context()).Table("projects").
		Where("id = ?", projectID).Limit(1).Pluck("workspace_id", &workspaceIDs).Error
	if err != nil {
		return ProjectUserProperty{}, err
	}
	if len(workspaceIDs) == 0 {
		return ProjectUserProperty{}, gorm.ErrRecordNotFound
	}
	propertyID, err := newUUID()
	if err != nil {
		return ProjectUserProperty{}, err
	}
	now := handler.clock().UTC()
	// get_or_create passes no defaults, so every column takes the model's own default.
	property = ProjectUserProperty{
		ID: propertyID, CreatedAt: now, UpdatedAt: now,
		ProjectID: projectID, WorkspaceID: workspaceIDs[0], UserID: user.ID,
		Filters: defaultFiltersJSON(), DisplayFilters: defaultDisplayFiltersJSON(),
		DisplayProperties: defaultDisplayPropertiesJSON(), RichFilters: emptyJSON(),
		Preferences: defaultPreferencesJSON(), SortOrder: 65535,
	}
	if err := handler.db.WithContext(c.Request.Context()).Create(&property).Error; err != nil {
		return ProjectUserProperty{}, err
	}
	return property, nil
}

func (handler *Handler) projectUserPropertyByID(c *gin.Context, id string) (ProjectUserProperty, error) {
	var property ProjectUserProperty
	err := handler.db.WithContext(c.Request.Context()).Where("id = ?", id).Take(&property).Error
	return property, err
}

// projectUserPropertyJSON is ProjectUserPropertySerializer: fields = "__all__" with the three relations read-only.
func projectUserPropertyJSON(property ProjectUserProperty) gin.H {
	return gin.H{
		"id": property.ID, "created_at": property.CreatedAt, "updated_at": property.UpdatedAt,
		"created_by": property.CreatedByID, "updated_by": property.UpdatedByID,
		"deleted_at": property.DeletedAt,
		"filters":    decodeJSON(property.Filters), "display_filters": decodeJSON(property.DisplayFilters),
		"display_properties": decodeJSON(property.DisplayProperties),
		"rich_filters":       decodeJSON(property.RichFilters), "preferences": decodeJSON(property.Preferences),
		"sort_order": property.SortOrder,
		"project":    property.ProjectID, "workspace": property.WorkspaceID, "user": property.UserID,
	}
}

// bulkDeleteIssues removes a set of issues along with their cycle and module links. Only a project admin may.
func (handler *Handler) bulkDeleteIssues(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin) {
		return
	}
	slug, projectID := c.Param("slug"), c.Param("id")
	var request struct {
		IssueIDs []string `json:"issue_ids"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		handler.invalidDetail(c)
		return
	}
	if len(request.IssueIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Issue IDs are required"})
		return
	}
	var identifiers []string
	err := handler.db.WithContext(c.Request.Context()).Table("issues i").
		Joins("JOIN workspaces w ON w.id = i.workspace_id").
		Where("i.id IN ? AND i.project_id = ? AND w.slug = ?", request.IssueIDs, projectID, slug).
		Where(issueObjectsPredicate("i")).Pluck("i.id", &identifiers).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	now := handler.clock().UTC()
	if len(identifiers) > 0 {
		err := handler.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
			// The two link tables go first, then the issues. All three are queryset deletes, which soft delete the rows without touching any other column.
			for _, table := range []string{"cycle_issues", "module_issues"} {
				err := tx.Table(table).Where("issue_id IN ? AND deleted_at IS NULL", identifiers).
					Update("deleted_at", now).Error
				if err != nil {
					return err
				}
			}
			return tx.Model(&Issue{}).Where("id IN ?", identifiers).Update("deleted_at", now).Error
		})
		if err != nil {
			handler.internalError(c, err)
			return
		}
	}
	// The count is of the issues the filter matched, not of the ids the caller sent.
	drf.Respond(c, http.StatusOK, gin.H{"message": bulkDeleteMessage(len(identifiers))})
}

// bulkDeleteMessage is the body Django formats. Django does not pluralise it, so one issue still reads "issues".
func bulkDeleteMessage(matched int) string {
	return strconv.Itoa(matched) + " issues were deleted"
}

// deletedIssues lists the ids that have gone away since the client last looked, so it can drop them from its local store. It reads through the unfiltered manager, which is the only way to see a soft-deleted row.
func (handler *Handler) deletedIssues(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	query := handler.db.WithContext(c.Request.Context()).Table("issues i").
		Joins("JOIN workspaces w ON w.id = i.workspace_id").
		Where("i.project_id = ? AND w.slug = ? AND (i.archived_at IS NOT NULL OR i.deleted_at IS NOT NULL)",
			c.Param("id"), c.Param("slug"))
	if raw := c.Query("updated_at__gt"); raw != "" {
		since, ok := parseDjangoDateTime(raw)
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Please provide valid detail"})
			return
		}
		query = query.Where("i.updated_at > ?", since)
	}
	identifiers := []string{}
	// The flat projection keeps the model's own ordering, so the newest ids come first.
	if err := query.Order("i.created_at DESC").Pluck("i.id", &identifiers).Error; err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, identifiers)
}
