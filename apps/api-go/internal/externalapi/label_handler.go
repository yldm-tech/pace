package externalapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"github.com/yldm-tech/pace/apps/api-go/internal/drf"
	"gorm.io/gorm"
)

func (handler *Handler) registerLabelRoutes(router gin.IRouter) {
	base := "/api/v1/workspaces/:slug/projects/:project/labels/"
	router.GET(base, handler.authenticated(handler.labelList))
	router.POST(base, handler.authenticated(handler.labelCreate))
	router.GET(base+":label/", handler.authenticated(handler.labelRetrieve))
	router.PATCH(base+":label/", handler.authenticated(handler.labelUpdate))
	router.DELETE(base+":label/", handler.authenticated(handler.labelDestroy))
}

// Label is the db.Label table as the external API sees it.
type Label struct {
	ID             string     `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt      time.Time  `gorm:"column:created_at"`
	UpdatedAt      time.Time  `gorm:"column:updated_at"`
	CreatedByID    *string    `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID    *string    `gorm:"column:updated_by_id;type:uuid"`
	DeletedAt      *time.Time `gorm:"column:deleted_at"`
	ProjectID      string     `gorm:"column:project_id;type:uuid"`
	WorkspaceID    string     `gorm:"column:workspace_id;type:uuid"`
	ParentID       *string    `gorm:"column:parent_id;type:uuid"`
	Name           string     `gorm:"column:name"`
	Description    string     `gorm:"column:description"`
	Color          string     `gorm:"column:color"`
	SortOrder      float64    `gorm:"column:sort_order"`
	ExternalSource *string    `gorm:"column:external_source"`
	ExternalID     *string    `gorm:"column:external_id"`
}

func (Label) TableName() string { return "labels" }

func (handler *Handler) labelList(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireProjectMember(c, user, http.MethodGet) {
		return
	}
	var labels []Label
	if err := handler.labelScope(c, user).Order("l.created_at DESC").Scan(&labels).Error; err != nil {
		handler.serverError(c, err)
		return
	}
	fields := requestedFields(c)
	results := make([]gin.H, 0, len(labels))
	for _, label := range labels {
		results = append(results, narrow(labelJSON(label), fields))
	}
	handler.respondPaged(c, results)
}

func (handler *Handler) labelRetrieve(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireProjectMember(c, user, http.MethodGet) {
		return
	}
	label, found, err := handler.labelByID(c, user)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !found {
		notFound(c)
		return
	}
	// The retrieve renders the whole serializer: unlike the list it does not narrow by the fields parameter, because it never reads it.
	drf.Respond(c, http.StatusOK, labelJSON(label))
}

// labelCreate adds a label, with the same pair of conflicts the external states have.
func (handler *Handler) labelCreate(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireProjectMember(c, user, http.MethodPost) {
		return
	}
	slug, projectID := c.Param("slug"), c.Param("project")
	var payload map[string]any
	if err := c.ShouldBindJSON(&payload); err != nil {
		invalidPayload(c)
		return
	}
	name, _ := payload["name"].(string)
	if strings.TrimSpace(name) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"name": []string{"This field is required."}})
		return
	}

	externalID, _ := payload["external_id"].(string)
	externalSource, _ := payload["external_source"].(string)
	if externalID != "" && externalSource != "" {
		existing, err := handler.labelWithExternalID(c, slug, projectID, externalSource, externalID, "")
		if err != nil {
			handler.serverError(c, err)
			return
		}
		if existing != "" {
			c.JSON(http.StatusConflict, gin.H{
				"error": "Label with the same external id and external source already exists", "id": existing,
			})
			return
		}
	}

	var workspaceIDs []string
	err := handler.db.WithContext(c.Request.Context()).Table("projects p").
		Joins("JOIN workspaces w ON w.id = p.workspace_id").
		Where("w.slug = ? AND p.id = ?", slug, projectID).Limit(1).Pluck("p.workspace_id", &workspaceIDs).Error
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
	label := Label{
		ID: identifier, CreatedAt: now, UpdatedAt: now, CreatedByID: &user.ID, UpdatedByID: &user.ID,
		ProjectID: projectID, WorkspaceID: workspaceIDs[0], Color: "", SortOrder: 65535,
	}
	applyLabelPayload(&label, payload)

	if err := handler.db.WithContext(c.Request.Context()).Create(&label).Error; err != nil {
		if isUniqueViolation(err) {
			existing, readErr := handler.labelWithName(c, slug, projectID, name)
			if readErr != nil {
				handler.serverError(c, readErr)
				return
			}
			body := gin.H{"error": "Label with the same name already exists in the project"}
			if existing != "" {
				body["id"] = existing
			}
			c.JSON(http.StatusConflict, body)
			return
		}
		handler.serverError(c, err)
		return
	}
	drf.Respond(c, http.StatusCreated, labelJSON(label))
}

// labelUpdate edits a label.
//
// Its external-id check needs **both** halves in the request, unlike the state's, which fires on the id alone — so an integration changing only the id gets through here and is refused there.
func (handler *Handler) labelUpdate(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireProjectMember(c, user, http.MethodPatch) {
		return
	}
	slug, projectID := c.Param("slug"), c.Param("project")
	var payload map[string]any
	if err := c.ShouldBindJSON(&payload); err != nil {
		invalidPayload(c)
		return
	}
	label, found, err := handler.labelByID(c, user)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !found {
		notFound(c)
		return
	}

	externalID, _ := payload["external_id"].(string)
	externalSource, _ := payload["external_source"].(string)
	if externalID != "" && externalSource != "" {
		existing, err := handler.labelWithExternalID(c, slug, projectID, externalSource, externalID, label.ID)
		if err != nil {
			handler.serverError(c, err)
			return
		}
		if existing != "" {
			// The id in the body is the label being edited, not the one that clashed, which is the opposite of what the message suggests.
			c.JSON(http.StatusConflict, gin.H{
				"error": "Label with the same external id and external source already exists", "id": label.ID,
			})
			return
		}
	}

	applyLabelPayload(&label, payload)
	now := handler.clock().UTC()
	label.UpdatedAt, label.UpdatedByID = now, &user.ID
	err = handler.db.WithContext(c.Request.Context()).Model(&Label{}).Where("id = ?", label.ID).
		Updates(map[string]any{
			"name": label.Name, "color": label.Color, "description": label.Description,
			"parent_id": label.ParentID, "sort_order": label.SortOrder,
			"external_source": label.ExternalSource, "external_id": label.ExternalID,
			"updated_at": label.UpdatedAt, "updated_by_id": label.UpdatedByID,
		}).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, labelJSON(label))
}

func (handler *Handler) labelDestroy(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireProjectMember(c, user, http.MethodDelete) {
		return
	}
	label, found, err := handler.labelByID(c, user)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !found {
		notFound(c)
		return
	}
	err = handler.db.WithContext(c.Request.Context()).Model(&Label{}).
		Where("id = ?", label.ID).Update("deleted_at", handler.clock().UTC()).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// labelScope is the queryset every label route reads through: the caller must be an active member of a project that is not archived.
func (handler *Handler) labelScope(c *gin.Context, user *auth.User) *gorm.DB {
	return handler.db.WithContext(c.Request.Context()).Table("labels l").Select("l.*").
		Joins("JOIN workspaces w ON w.id = l.workspace_id").
		Joins("JOIN projects p ON p.id = l.project_id AND p.archived_at IS NULL").
		Joins("JOIN project_members pm ON pm.project_id = l.project_id AND pm.member_id = ? AND pm.is_active = TRUE", user.ID).
		Where("w.slug = ? AND l.project_id = ? AND l.deleted_at IS NULL", c.Param("slug"), c.Param("project"))
}

func (handler *Handler) labelByID(c *gin.Context, user *auth.User) (Label, bool, error) {
	var labels []Label
	err := handler.labelScope(c, user).Where("l.id = ?", c.Param("label")).Limit(1).Scan(&labels).Error
	if err != nil || len(labels) == 0 {
		return Label{}, false, err
	}
	return labels[0], true, nil
}

// labelWithExternalID finds a label already carrying the pair, optionally ignoring one id.
func (handler *Handler) labelWithExternalID(c *gin.Context, slug, projectID, source, external, exclude string) (string, error) {
	query := handler.db.WithContext(c.Request.Context()).Table("labels l").
		Joins("JOIN workspaces w ON w.id = l.workspace_id").
		Where("w.slug = ? AND l.project_id = ? AND l.external_source = ? AND l.external_id = ? AND l.deleted_at IS NULL",
			slug, projectID, source, external)
	if exclude != "" {
		query = query.Where("l.id <> ?", exclude)
	}
	var identifiers []string
	if err := query.Limit(1).Pluck("l.id", &identifiers).Error; err != nil {
		return "", err
	}
	if len(identifiers) == 0 {
		return "", nil
	}
	return identifiers[0], nil
}

func (handler *Handler) labelWithName(c *gin.Context, slug, projectID, name string) (string, error) {
	var identifiers []string
	err := handler.db.WithContext(c.Request.Context()).Table("labels l").
		Joins("JOIN workspaces w ON w.id = l.workspace_id").
		Where("w.slug = ? AND l.project_id = ? AND l.name = ? AND l.deleted_at IS NULL", slug, projectID, name).
		Limit(1).Pluck("l.id", &identifiers).Error
	if err != nil || len(identifiers) == 0 {
		return "", err
	}
	return identifiers[0], nil
}

// applyLabelPayload writes the seven fields the create-and-update serializer names. Everything else is read-only.
func applyLabelPayload(label *Label, payload map[string]any) {
	if value, ok := payload["name"].(string); ok {
		label.Name = value
	}
	if value, ok := payload["color"].(string); ok {
		label.Color = value
	}
	if value, ok := payload["description"].(string); ok {
		label.Description = value
	}
	if value, ok := payload["sort_order"].(float64); ok {
		label.SortOrder = value
	}
	if value, present := payload["parent"]; present {
		if text, ok := value.(string); ok && text != "" {
			label.ParentID = &text
		} else {
			label.ParentID = nil
		}
	}
	for key, target := range map[string]**string{
		"external_source": &label.ExternalSource, "external_id": &label.ExternalID,
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
}

// labelJSON is the external API's LabelSerializer, which asks for every field.
func labelJSON(label Label) gin.H {
	return gin.H{
		"id": label.ID, "created_at": label.CreatedAt, "updated_at": label.UpdatedAt,
		"created_by": label.CreatedByID, "updated_by": label.UpdatedByID, "deleted_at": label.DeletedAt,
		"name": label.Name, "description": label.Description, "color": label.Color,
		"sort_order": label.SortOrder, "parent": label.ParentID,
		"external_source": label.ExternalSource, "external_id": label.ExternalID,
		"project": label.ProjectID, "workspace": label.WorkspaceID,
	}
}
