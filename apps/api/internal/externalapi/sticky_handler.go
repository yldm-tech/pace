package externalapi

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api/internal/auth"
	"github.com/yldm-tech/pace/apps/api/internal/drf"
	"github.com/yldm-tech/pace/apps/api/internal/htmlsanitizer"
	"gorm.io/gorm"
)

func (handler *Handler) registerStickyRoutes(router gin.IRouter) {
	base := "/api/v1/workspaces/:slug/stickies/"
	router.GET(base, handler.authenticated(handler.stickyList))
	router.POST(base, handler.authenticated(handler.stickyCreate))
	router.GET(base+":sticky/", handler.authenticated(handler.stickyRetrieve))
	router.PATCH(base+":sticky/", handler.authenticated(handler.stickyUpdate))
	// The router binds PUT as well, and the serializer requires nothing at all, so a full update is the partial one.
	router.PUT(base+":sticky/", handler.authenticated(handler.stickyUpdate))
	router.DELETE(base+":sticky/", handler.authenticated(handler.stickyDestroy))
}

// Sticky is the db.Sticky table: a private note pinned to a workspace.
type Sticky struct {
	ID                string     `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt         time.Time  `gorm:"column:created_at"`
	UpdatedAt         time.Time  `gorm:"column:updated_at"`
	CreatedByID       *string    `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID       *string    `gorm:"column:updated_by_id;type:uuid"`
	DeletedAt         *time.Time `gorm:"column:deleted_at"`
	WorkspaceID       string     `gorm:"column:workspace_id;type:uuid"`
	OwnerID           string     `gorm:"column:owner_id;type:uuid"`
	Name              *string    `gorm:"column:name"`
	Description       []byte     `gorm:"column:description;type:jsonb"`
	DescriptionHTML   string     `gorm:"column:description_html"`
	DescriptionStrpd  *string    `gorm:"column:description_stripped"`
	DescriptionBinary []byte     `gorm:"column:description_binary"`
	LogoProps         []byte     `gorm:"column:logo_props;type:jsonb"`
	Color             *string    `gorm:"column:color"`
	BackgroundColor   *string    `gorm:"column:background_color"`
	SortOrder         float64    `gorm:"column:sort_order"`
}

func (Sticky) TableName() string { return "stickies" }

// stickyList returns the caller's own notes.
//
// A sticky belongs to one person, and the queryset is narrowed to the **owner** rather than to the workspace's members — so there is no way to read somebody else's through this API, whatever their role.
func (handler *Handler) stickyList(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireWorkspaceUser(c, user) {
		return
	}
	query := handler.stickyScope(c, user).Order("s.created_at DESC")
	// The search looks at the stripped text rather than the html, so a note is found by what it says and not by how it is marked up.
	if search := c.Query("query"); search != "" {
		query = query.Where("s.description_stripped ILIKE ?", "%"+search+"%")
	}
	var stickies []Sticky
	if err := query.Scan(&stickies).Error; err != nil {
		handler.serverError(c, err)
		return
	}
	results := make([]gin.H, 0, len(stickies))
	for _, sticky := range stickies {
		results = append(results, stickyJSON(sticky))
	}
	// This list's page is twenty rather than the hundred every other list in this API defaults to.
	handler.respondPagedWithDefault(c, results, 20)
}

func (handler *Handler) stickyRetrieve(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireWorkspaceUser(c, user) {
		return
	}
	sticky, found, err := handler.stickyByID(c, user)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !found {
		notFound(c)
		return
	}
	drf.Respond(c, http.StatusOK, stickyJSON(sticky))
}

func (handler *Handler) stickyCreate(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireWorkspaceUser(c, user) {
		return
	}
	var payload map[string]any
	if err := c.ShouldBindJSON(&payload); err != nil {
		invalidPayload(c)
		return
	}
	var workspaceIDs []string
	err := handler.db.WithContext(c.Request.Context()).Table("workspaces").
		Where("slug = ?", c.Param("slug")).Limit(1).Pluck("id", &workspaceIDs).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if len(workspaceIDs) == 0 {
		notFound(c)
		return
	}
	identifier, err := newUUID()
	if err != nil {
		handler.serverError(c, err)
		return
	}
	now := handler.clock().UTC()
	sticky := Sticky{
		ID: identifier, CreatedAt: now, UpdatedAt: now, CreatedByID: &user.ID, UpdatedByID: &user.ID,
		WorkspaceID: workspaceIDs[0], OwnerID: user.ID, SortOrder: 65535,
		Description: []byte("{}"), DescriptionHTML: "<p></p>", LogoProps: []byte("{}"),
	}
	// The name is not required, which is unusual: a note can be saved with nothing but its colour.
	if ok := applyStickyPayload(c, &sticky, payload); !ok {
		return
	}
	if err := handler.db.WithContext(c.Request.Context()).Create(&sticky).Error; err != nil {
		handler.serverError(c, err)
		return
	}
	drf.Respond(c, http.StatusCreated, stickyJSON(sticky))
}

func (handler *Handler) stickyUpdate(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireWorkspaceUser(c, user) {
		return
	}
	var payload map[string]any
	if err := c.ShouldBindJSON(&payload); err != nil {
		invalidPayload(c)
		return
	}
	sticky, found, err := handler.stickyByID(c, user)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !found {
		notFound(c)
		return
	}
	if ok := applyStickyPayload(c, &sticky, payload); !ok {
		return
	}
	now := handler.clock().UTC()
	sticky.UpdatedAt, sticky.UpdatedByID = now, &user.ID
	err = handler.db.WithContext(c.Request.Context()).Model(&Sticky{}).Where("id = ?", sticky.ID).
		Updates(map[string]any{
			"name": sticky.Name, "description": sticky.Description,
			"description_html": sticky.DescriptionHTML, "description_stripped": sticky.DescriptionStrpd,
			"logo_props": sticky.LogoProps, "color": sticky.Color,
			"background_color": sticky.BackgroundColor, "sort_order": sticky.SortOrder,
			"updated_at": sticky.UpdatedAt, "updated_by_id": sticky.UpdatedByID,
		}).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, stickyJSON(sticky))
}

func (handler *Handler) stickyDestroy(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireWorkspaceUser(c, user) {
		return
	}
	sticky, found, err := handler.stickyByID(c, user)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !found {
		notFound(c)
		return
	}
	err = handler.db.WithContext(c.Request.Context()).Model(&Sticky{}).
		Where("id = ?", sticky.ID).Update("deleted_at", handler.clock().UTC()).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// applyStickyPayload writes the writable fields, sanitizing the html on the way in. It reports whether the request may carry on.
func applyStickyPayload(c *gin.Context, sticky *Sticky, payload map[string]any) bool {
	if value, present := payload["name"]; present {
		if text, ok := value.(string); ok {
			sticky.Name = &text
		} else {
			sticky.Name = nil
		}
	}
	if value, ok := payload["description_html"].(string); ok && value != "" {
		valid, _, cleaned := htmlsanitizer.ValidateHTMLContent(value)
		if !valid {
			// The serializer discards the sanitizer's own message and answers with one of its own.
			c.JSON(http.StatusBadRequest, gin.H{"error": "html content is not valid"})
			return false
		}
		if cleaned != nil {
			value = *cleaned
		}
		sticky.DescriptionHTML = value
	}
	for key, target := range map[string]**string{
		"color": &sticky.Color, "background_color": &sticky.BackgroundColor,
	} {
		value, present := payload[key]
		if !present {
			continue
		}
		if text, ok := value.(string); ok {
			*target = &text
		} else {
			*target = nil
		}
	}
	if value, ok := payload["sort_order"].(float64); ok {
		sticky.SortOrder = value
	}
	for key, target := range map[string]*[]byte{
		"description": &sticky.Description, "logo_props": &sticky.LogoProps,
	} {
		value, present := payload[key]
		if !present {
			continue
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			continue
		}
		*target = encoded
	}
	return true
}

// requireWorkspaceUser is WorkspaceUserPermission: any active member will do, whatever their role.
func (handler *Handler) requireWorkspaceUser(c *gin.Context, user *auth.User) bool {
	var members int64
	err := handler.db.WithContext(c.Request.Context()).Table("workspace_members wm").
		Joins("JOIN workspaces w ON w.id = wm.workspace_id").
		Where("w.slug = ? AND wm.member_id = ? AND wm.is_active = TRUE", c.Param("slug"), user.ID).
		Count(&members).Error
	if err != nil {
		handler.serverError(c, err)
		return false
	}
	if members == 0 {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
			"detail": "You do not have permission to perform this action.",
		})
		return false
	}
	return true
}

func (handler *Handler) stickyScope(c *gin.Context, user *auth.User) *gorm.DB {
	return handler.db.WithContext(c.Request.Context()).Table("stickies s").Select("s.*").
		Joins("JOIN workspaces w ON w.id = s.workspace_id").
		Where("w.slug = ? AND s.owner_id = ? AND s.deleted_at IS NULL", c.Param("slug"), user.ID)
}

func (handler *Handler) stickyByID(c *gin.Context, user *auth.User) (Sticky, bool, error) {
	var stickies []Sticky
	err := handler.stickyScope(c, user).Where("s.id = ?", c.Param("sticky")).Limit(1).Scan(&stickies).Error
	if err != nil || len(stickies) == 0 {
		return Sticky{}, false, err
	}
	return stickies[0], true, nil
}

// stickyJSON is StickySerializer, which asks for every field.
func stickyJSON(sticky Sticky) gin.H {
	return gin.H{
		"id": sticky.ID, "created_at": sticky.CreatedAt, "updated_at": sticky.UpdatedAt,
		"created_by": sticky.CreatedByID, "updated_by": sticky.UpdatedByID, "deleted_at": sticky.DeletedAt,
		"name": sticky.Name, "description": decodeJSON(sticky.Description),
		"description_html": sticky.DescriptionHTML, "description_stripped": sticky.DescriptionStrpd,
		"description_binary": sticky.DescriptionBinary,
		"logo_props":         decodeJSON(sticky.LogoProps),
		"color":              sticky.Color, "background_color": sticky.BackgroundColor,
		"sort_order": sticky.SortOrder, "workspace": sticky.WorkspaceID, "owner": sticky.OwnerID,
	}
}
