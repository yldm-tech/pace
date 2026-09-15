package workspace

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"github.com/yldm-tech/pace/apps/api-go/internal/drf"
)

func (handler *Handler) registerFavoriteRoutes(router gin.IRouter) {
	const base = "/api/workspaces/:slug/user-favorites/"
	router.GET(base, handler.authenticated(handler.favoriteList))
	router.POST(base, handler.authenticated(handler.favoriteCreate))
	router.PATCH(base+":favorite/", handler.authenticated(handler.favoriteUpdate))
	router.DELETE(base+":favorite/", handler.authenticated(handler.favoriteDestroy))
	router.GET(base+":favorite/group/", handler.authenticated(handler.favoriteGroup))
	// The project favourites are the same table under an older pair of routes, which write a row of their own shape.
	router.GET("/api/workspaces/:slug/user-favorite-projects/", handler.authenticated(handler.projectFavoriteList))
	router.POST("/api/workspaces/:slug/user-favorite-projects/", handler.authenticated(handler.projectFavoriteCreate))
	router.DELETE("/api/workspaces/:slug/user-favorite-projects/:project/", handler.authenticated(handler.projectFavoriteDestroy))
}

// UserFavorite is one thing a person has starred, which may be a folder holding others.
type UserFavorite struct {
	ID               string     `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt        time.Time  `gorm:"column:created_at"`
	UpdatedAt        time.Time  `gorm:"column:updated_at"`
	CreatedByID      *string    `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID      *string    `gorm:"column:updated_by_id;type:uuid"`
	DeletedAt        *time.Time `gorm:"column:deleted_at"`
	WorkspaceID      string     `gorm:"column:workspace_id;type:uuid"`
	ProjectID        *string    `gorm:"column:project_id;type:uuid"`
	UserID           string     `gorm:"column:user_id;type:uuid"`
	EntityType       string     `gorm:"column:entity_type"`
	EntityIdentifier *string    `gorm:"column:entity_identifier;type:uuid"`
	Name             *string    `gorm:"column:name"`
	IsFolder         bool       `gorm:"column:is_folder"`
	Sequence         float64    `gorm:"column:sequence"`
	ParentID         *string    `gorm:"column:parent_id;type:uuid"`
}

func (UserFavorite) TableName() string { return "user_favorites" }

// favoriteList reports what the caller has starred at the top level.
//
// A favourite that belongs to a project is reported only while the caller is still in that project, and a **page** with no project is hidden outright — the one entity type the top-level list refuses.
func (handler *Handler) favoriteList(c *gin.Context, user *auth.User) {
	if !handler.requireFavoriteRole(c, user) {
		return
	}
	var rows []UserFavorite
	err := handler.db.WithContext(c.Request.Context()).Table("user_favorites f").Select("f.*").
		Joins("JOIN workspaces w ON w.id = f.workspace_id").
		Where("f.user_id = ? AND w.slug = ? AND f.parent_id IS NULL AND f.deleted_at IS NULL", user.ID, c.Param("slug")).
		Where(`(f.project_id IS NULL AND f.entity_type <> 'page')
			OR (f.project_id IS NOT NULL AND EXISTS (
				SELECT 1 FROM project_members pm WHERE pm.project_id = f.project_id
				AND pm.member_id = ? AND pm.is_active = TRUE AND pm.deleted_at IS NULL))`, user.ID).
		Order("f.sequence DESC").Scan(&rows).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	handler.respondWithFavorites(c, rows)
}

// favoriteGroup reports what is inside a folder. Unlike the top-level list it hides no entity type, so a page inside a folder is reported where the same page outside one is not.
func (handler *Handler) favoriteGroup(c *gin.Context, user *auth.User) {
	if !handler.requireFavoriteRole(c, user) {
		return
	}
	var rows []UserFavorite
	err := handler.db.WithContext(c.Request.Context()).Table("user_favorites f").Select("f.*").
		Joins("JOIN workspaces w ON w.id = f.workspace_id").
		Where("f.user_id = ? AND w.slug = ? AND f.parent_id = ? AND f.deleted_at IS NULL",
			user.ID, c.Param("slug"), c.Param("favorite")).
		Where(`f.project_id IS NULL OR EXISTS (
			SELECT 1 FROM project_members pm WHERE pm.project_id = f.project_id
			AND pm.member_id = ? AND pm.is_active = TRUE AND pm.deleted_at IS NULL)`, user.ID).
		Order("f.sequence DESC").Scan(&rows).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	handler.respondWithFavorites(c, rows)
}

// favoriteCreate stars something, and answers the one that is already there rather than refusing a second star.
func (handler *Handler) favoriteCreate(c *gin.Context, user *auth.User) {
	if !handler.requireFavoriteRole(c, user) {
		return
	}
	var payload map[string]json.RawMessage
	if err := c.ShouldBindJSON(&payload); err != nil {
		handler.invalidDetail(c)
		return
	}
	var workspaceIDs []string
	err := handler.db.WithContext(c.Request.Context()).Table("workspaces").
		Where("slug = ?", c.Param("slug")).Limit(1).Pluck("id", &workspaceIDs).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if len(workspaceIDs) == 0 {
		handler.notFound(c)
		return
	}
	entityType := payloadText(payload, "entity_type")
	entityIdentifier := payloadText(payload, "entity_identifier")
	if entityIdentifier != "" {
		var existing []UserFavorite
		err := handler.db.WithContext(c.Request.Context()).Table("user_favorites").
			Where(`workspace_id = ? AND user_id = ? AND entity_type = ? AND entity_identifier = ? AND deleted_at IS NULL`,
				workspaceIDs[0], user.ID, entityType, entityIdentifier).
			Limit(1).Scan(&existing).Error
		if err != nil {
			handler.internalError(c, err)
			return
		}
		if len(existing) > 0 {
			// The one that is already there is answered with, and with a 200 rather than a 201.
			handler.respondWithFavorite(c, existing[0])
			return
		}
	}

	identifier, err := newUUID()
	if err != nil {
		handler.internalError(c, err)
		return
	}
	now := handler.clock().UTC()
	favorite := UserFavorite{
		ID: identifier, CreatedAt: now, UpdatedAt: now, CreatedByID: &user.ID, UpdatedByID: &user.ID,
		WorkspaceID: workspaceIDs[0], UserID: user.ID, EntityType: entityType,
		Sequence: 65535,
	}
	if entityIdentifier != "" {
		favorite.EntityIdentifier = &entityIdentifier
	}
	if name := payloadText(payload, "name"); name != "" {
		favorite.Name = &name
	}
	if projectID := payloadText(payload, "project_id"); projectID != "" {
		favorite.ProjectID = &projectID
	}
	if parent := payloadText(payload, "parent"); parent != "" {
		favorite.ParentID = &parent
	}
	if raw, given := payload["is_folder"]; given {
		_ = json.Unmarshal(raw, &favorite.IsFolder)
	}
	if raw, given := payload["sequence"]; given {
		_ = json.Unmarshal(raw, &favorite.Sequence)
	}
	if err := handler.db.WithContext(c.Request.Context()).Create(&favorite).Error; err != nil {
		if isUniqueViolation(err) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Favorite already exists"})
			return
		}
		handler.internalError(c, err)
		return
	}
	handler.respondWithFavorite(c, favorite)
}

// favoriteUpdate moves a favourite: into a folder, out of one, or up and down the list.
func (handler *Handler) favoriteUpdate(c *gin.Context, user *auth.User) {
	if !handler.requireFavoriteRole(c, user) {
		return
	}
	favorite, found, err := handler.favoriteByID(c, user)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		handler.notFound(c)
		return
	}
	var payload map[string]json.RawMessage
	if err := c.ShouldBindJSON(&payload); err != nil {
		handler.invalidDetail(c)
		return
	}
	updates := map[string]any{}
	if raw, given := payload["name"]; given {
		var value string
		if json.Unmarshal(raw, &value) == nil {
			updates["name"] = value
			favorite.Name = &value
		}
	}
	if raw, given := payload["sequence"]; given {
		var value float64
		if json.Unmarshal(raw, &value) != nil {
			c.JSON(http.StatusBadRequest, gin.H{"sequence": []string{"A valid number is required."}})
			return
		}
		updates["sequence"] = value
		favorite.Sequence = value
	}
	if raw, given := payload["is_folder"]; given {
		var value bool
		if json.Unmarshal(raw, &value) != nil {
			c.JSON(http.StatusBadRequest, gin.H{"is_folder": []string{"Must be a valid boolean."}})
			return
		}
		updates["is_folder"] = value
		favorite.IsFolder = value
	}
	if raw, given := payload["parent"]; given {
		if string(raw) == "null" {
			updates["parent_id"] = nil
			favorite.ParentID = nil
		} else {
			var value string
			if json.Unmarshal(raw, &value) == nil {
				updates["parent_id"] = value
				favorite.ParentID = &value
			}
		}
	}
	if len(updates) > 0 {
		now := handler.clock().UTC()
		updates["updated_at"] = now
		updates["updated_by_id"] = user.ID
		err := handler.db.WithContext(c.Request.Context()).Table("user_favorites").
			Where("id = ?", favorite.ID).Updates(updates).Error
		if err != nil {
			handler.internalError(c, err)
			return
		}
	}
	handler.respondWithFavorite(c, favorite)
}

// favoriteDestroy unstars something. The row goes **for good** rather than being soft deleted, which is what lets the same thing be starred again.
func (handler *Handler) favoriteDestroy(c *gin.Context, user *auth.User) {
	if !handler.requireFavoriteRole(c, user) {
		return
	}
	favorite, found, err := handler.favoriteByID(c, user)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		handler.notFound(c)
		return
	}
	err = handler.db.WithContext(c.Request.Context()).
		Exec("DELETE FROM user_favorites WHERE id = ?", favorite.ID).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// projectFavoriteList is broken upstream and reproduced as such. The viewset inherits DRF's list and declares no serializer_class, so get_serializer_class asserts and answers 500.
func (handler *Handler) projectFavoriteList(c *gin.Context, user *auth.User) {
	handler.internalError(c, errProjectFavoritesHaveNoSerializer)
}

var errProjectFavoritesHaveNoSerializer = &favoriteError{
	"project favourites: the viewset declares no serializer_class, so DRF cannot list them",
}

type favoriteError struct{ message string }

func (err *favoriteError) Error() string { return err.message }

// projectFavoriteCreate stars a project, and answers **204** with no body.
//
// The row is written with the project in both of its identifier columns and the workspace read off the project, which is what the model does on its way in.
func (handler *Handler) projectFavoriteCreate(c *gin.Context, user *auth.User) {
	var payload map[string]json.RawMessage
	if err := c.ShouldBindJSON(&payload); err != nil {
		handler.invalidDetail(c)
		return
	}
	projectID := payloadText(payload, "project")
	if projectID == "" {
		// Django writes a row with no project at all, which the database refuses.
		handler.internalError(c, errProjectFavoriteNeedsAProject)
		return
	}
	var workspaceIDs []string
	err := handler.db.WithContext(c.Request.Context()).Table("projects").
		Where("id = ? AND deleted_at IS NULL", projectID).Limit(1).Pluck("workspace_id", &workspaceIDs).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if len(workspaceIDs) == 0 {
		handler.internalError(c, errProjectFavoriteNeedsAProject)
		return
	}
	identifier, err := newUUID()
	if err != nil {
		handler.internalError(c, err)
		return
	}
	now := handler.clock().UTC()
	favorite := UserFavorite{
		ID: identifier, CreatedAt: now, UpdatedAt: now, CreatedByID: &user.ID, UpdatedByID: &user.ID,
		WorkspaceID: workspaceIDs[0], ProjectID: &projectID, UserID: user.ID,
		EntityType: "project", EntityIdentifier: &projectID, Sequence: 65535,
	}
	if err := handler.db.WithContext(c.Request.Context()).Create(&favorite).Error; err != nil {
		if isUniqueViolation(err) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "The payload is not valid"})
			return
		}
		handler.internalError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

var errProjectFavoriteNeedsAProject = &favoriteError{"a project favourite is written with no project, which the database refuses"}

func (handler *Handler) projectFavoriteDestroy(c *gin.Context, user *auth.User) {
	result := handler.db.WithContext(c.Request.Context()).Exec(
		`DELETE FROM user_favorites WHERE entity_identifier = ? AND entity_type = 'project'
		AND project_id = ? AND user_id = ? AND workspace_id = (SELECT id FROM workspaces WHERE slug = ?)`,
		c.Param("project"), c.Param("project"), user.ID, c.Param("slug"))
	if result.Error != nil {
		handler.internalError(c, result.Error)
		return
	}
	if result.RowsAffected == 0 {
		handler.notFound(c)
		return
	}
	c.Status(http.StatusNoContent)
}

func (handler *Handler) favoriteByID(c *gin.Context, user *auth.User) (UserFavorite, bool, error) {
	var rows []UserFavorite
	err := handler.db.WithContext(c.Request.Context()).Table("user_favorites f").Select("f.*").
		Joins("JOIN workspaces w ON w.id = f.workspace_id").
		Where("f.user_id = ? AND w.slug = ? AND f.id = ? AND f.deleted_at IS NULL",
			user.ID, c.Param("slug"), c.Param("favorite")).
		Limit(1).Scan(&rows).Error
	if err != nil || len(rows) == 0 {
		return UserFavorite{}, false, err
	}
	return rows[0], true, nil
}

// requireFavoriteRole asks for an admin or a member, which is what the favourite routes carry — a guest has no favourites here.
func (handler *Handler) requireFavoriteRole(c *gin.Context, user *auth.User) bool {
	role, err := handler.workspaceRole(c.Request.Context(), c.Param("slug"), user.ID)
	if err != nil {
		handler.internalError(c, err)
		return false
	}
	if role != roleAdmin && role != roleMember {
		forbidden(c)
		return false
	}
	return true
}

func (handler *Handler) respondWithFavorite(c *gin.Context, favorite UserFavorite) {
	body, err := handler.favoriteJSON(c, favorite)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, body)
}

func (handler *Handler) respondWithFavorites(c *gin.Context, rows []UserFavorite) {
	results := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		body, err := handler.favoriteJSON(c, row)
		if err != nil {
			handler.internalError(c, err)
			return
		}
		results = append(results, body)
	}
	drf.Respond(c, http.StatusOK, results)
}

// favoriteJSON is UserFavoriteSerializer, which reads the thing itself out of whichever table its type names.
//
// A type nobody recognises, a folder, and a **work item** all report a null there: the work item is in the table of types and has no serializer beside it, so starring one shows nothing about it.
func (handler *Handler) favoriteJSON(c *gin.Context, favorite UserFavorite) (gin.H, error) {
	data, err := handler.favoriteEntityData(c, favorite)
	if err != nil {
		return nil, err
	}
	return gin.H{
		"id": favorite.ID, "entity_type": favorite.EntityType,
		"entity_identifier": favorite.EntityIdentifier, "entity_data": data,
		"name": favorite.Name, "is_folder": favorite.IsFolder, "sequence": favorite.Sequence,
		"parent":       favorite.ParentID,
		"workspace_id": favorite.WorkspaceID, "project_id": favorite.ProjectID,
	}, nil
}

func (handler *Handler) favoriteEntityData(c *gin.Context, favorite UserFavorite) (any, error) {
	if favorite.EntityIdentifier == nil {
		return nil, nil
	}
	table := ""
	withProject := true
	switch favorite.EntityType {
	case "cycle":
		table = "cycles"
	case "module":
		table = "modules"
	case "view":
		table = "issue_views"
	case "page":
		table = "pages"
	case "project":
		table = "projects"
		withProject = false
	default:
		// issue and folder both land here: the first has no serializer beside it and the second has no model at all.
		return nil, nil
	}
	var rows []struct {
		ID        string  `gorm:"column:id"`
		Name      string  `gorm:"column:name"`
		LogoProps []byte  `gorm:"column:logo_props"`
		ProjectID *string `gorm:"column:project_id"`
	}
	selection := "id, name, logo_props, project_id"
	if !withProject {
		selection = "id, name, logo_props, NULL AS project_id"
	}
	err := handler.db.WithContext(c.Request.Context()).Table(table).Select(selection).
		Where("id = ?", *favorite.EntityIdentifier).Limit(1).Scan(&rows).Error
	if err != nil || len(rows) == 0 {
		// A favourite whose thing is gone reports a null rather than failing the whole list.
		return nil, err
	}
	body := gin.H{"id": rows[0].ID, "name": rows[0].Name, "logo_props": decodeJSON(auth.JSONValue(rows[0].LogoProps))}
	if withProject {
		body["project_id"] = rows[0].ProjectID
	}
	return body, nil
}

func payloadText(payload map[string]json.RawMessage, field string) string {
	raw, given := payload[field]
	if !given {
		return ""
	}
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return ""
	}
	return value
}
