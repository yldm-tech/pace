package project

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (handler *Handler) registerLabelRoutes(router gin.IRouter) {
	router.GET("/api/workspaces/:slug/projects/:id/issue-labels/", handler.authenticatedUUID(handler.labelList))
	router.POST("/api/workspaces/:slug/projects/:id/issue-labels/", handler.authenticatedUUID(handler.labelCreate))
	router.GET("/api/workspaces/:slug/projects/:id/issue-labels/:label/", handler.authenticatedLabelUUID(handler.labelRetrieve))
	router.PUT("/api/workspaces/:slug/projects/:id/issue-labels/:label/", handler.authenticatedLabelUUID(handler.labelUpdate))
	router.PATCH("/api/workspaces/:slug/projects/:id/issue-labels/:label/", handler.authenticatedLabelUUID(handler.labelUpdate))
	router.DELETE("/api/workspaces/:slug/projects/:id/issue-labels/:label/", handler.authenticatedLabelUUID(handler.labelDelete))
	router.POST("/api/workspaces/:slug/projects/:id/bulk-create-labels/", handler.authenticatedUUID(handler.labelBulkCreate))
}

func (handler *Handler) authenticatedLabelUUID(next func(*gin.Context, *auth.User)) gin.HandlerFunc {
	inner := handler.authenticatedUUID(next)
	return func(c *gin.Context) {
		if !uuidPattern.MatchString(c.Param("label")) {
			handler.notFound(c)
			return
		}
		inner(c)
	}
}

// labelList is LabelViewSet.list, guarded by ProjectBasePermission, whose safe
// method branch only requires an active workspace membership. The queryset then
// narrows to projects the caller belongs to, so a non-member simply sees
// nothing rather than being refused.
func (handler *Handler) labelList(c *gin.Context, user *auth.User) {
	if !handler.requireWorkspaceMembership(c, user) {
		return
	}
	var labels []Label
	err := handler.db.WithContext(c.Request.Context()).
		Where(`project_id = ? AND workspace_id = (SELECT id FROM workspaces WHERE slug = ?) AND deleted_at IS NULL
			AND EXISTS (SELECT 1 FROM project_members pm WHERE pm.project_id = labels.project_id AND pm.member_id = ? AND pm.deleted_at IS NULL)`,
			c.Param("id"), c.Param("slug"), user.ID).
		Order("sort_order").Find(&labels).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	response := make([]gin.H, 0, len(labels))
	for _, label := range labels {
		response = append(response, labelJSON(label))
	}
	c.JSON(http.StatusOK, response)
}

func (handler *Handler) labelRetrieve(c *gin.Context, user *auth.User) {
	if !handler.requireWorkspaceMembership(c, user) {
		return
	}
	label, found, err := handler.labelByID(c.Request.Context(), c.Param("slug"), c.Param("id"), c.Param("label"))
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		handler.notFound(c)
		return
	}
	c.JSON(http.StatusOK, labelJSON(label))
}

func (handler *Handler) labelCreate(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin) {
		return
	}
	slug, projectID := c.Param("slug"), c.Param("id")
	var project Project
	err := handler.db.WithContext(c.Request.Context()).
		Where("id = ? AND workspace_id = (SELECT id FROM workspaces WHERE slug = ?) AND deleted_at IS NULL", projectID, slug).
		Take(&project).Error
	if err != nil {
		handler.notFound(c)
		return
	}
	fields, ok := handler.labelFields(c, false)
	if !ok {
		return
	}
	taken, err := handler.labelNameTaken(c.Request.Context(), projectID, fields.name, "")
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if taken {
		// validate_name raises this code, which DRF reports under the field.
		c.JSON(http.StatusBadRequest, gin.H{"name": []string{"LABEL_NAME_ALREADY_EXISTS"}})
		return
	}
	labelID, err := newUUID()
	if err != nil {
		handler.internalError(c, err)
		return
	}
	now := handler.clock().UTC()
	label := Label{
		ID: labelID, CreatedAt: now, UpdatedAt: now, CreatedByID: &user.ID,
		ProjectID: &projectID, WorkspaceID: project.WorkspaceID,
		Name: fields.name, Description: fields.description, Color: fields.color,
		ParentID: fields.parentID, SortOrder: 65535,
	}
	// Label.save puts a new label after the project's current highest.
	sortOrder, err := handler.nextLabelSortOrder(c.Request.Context(), projectID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	label.SortOrder = sortOrder
	if fields.hasSortOrder {
		label.SortOrder = fields.sortOrder
	}
	if err := handler.db.WithContext(c.Request.Context()).Create(&label).Error; err != nil {
		if isUniqueViolation(err) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Label with the same name already exists in the project"})
			return
		}
		handler.internalError(c, err)
		return
	}
	if err := handler.invalidateWorkspaceLabels(c.Request.Context(), slug); err != nil {
		handler.internalError(c, err)
		return
	}
	c.JSON(http.StatusCreated, labelJSON(label))
}

func (handler *Handler) labelUpdate(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin) {
		return
	}
	slug, projectID := c.Param("slug"), c.Param("id")
	body, ok := handler.labelBody(c)
	if !ok {
		return
	}
	// Django checks the name against the project before it loads the label, and
	// its check is case sensitive and does not filter soft-deleted rows, unlike
	// the serializer's own validate_name.
	if raw, exists := body["name"]; exists {
		var name string
		if json.Unmarshal(raw, &name) == nil {
			var count int64
			err := handler.db.WithContext(c.Request.Context()).Model(&Label{}).
				Where("project_id = ? AND name = ? AND id <> ?", projectID, name, c.Param("label")).
				Count(&count).Error
			if err != nil {
				handler.internalError(c, err)
				return
			}
			if count > 0 {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Label with the same name already exists in the project"})
				return
			}
		}
	}
	label, found, err := handler.labelByID(c.Request.Context(), slug, projectID, c.Param("label"))
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		handler.notFound(c)
		return
	}
	fields, ok := handler.labelFieldsFrom(c, body, true)
	if !ok {
		return
	}
	if fields.hasName {
		// The serializer's own check is case insensitive and excludes this row.
		taken, err := handler.labelNameTaken(c.Request.Context(), projectID, fields.name, label.ID)
		if err != nil {
			handler.internalError(c, err)
			return
		}
		if taken {
			c.JSON(http.StatusBadRequest, gin.H{"name": []string{"LABEL_NAME_ALREADY_EXISTS"}})
			return
		}
	}
	updates := map[string]any{"updated_at": handler.clock().UTC(), "updated_by_id": user.ID}
	if fields.hasName {
		updates["name"] = fields.name
	}
	if fields.hasColor {
		updates["color"] = fields.color
	}
	if fields.hasDescription {
		updates["description"] = fields.description
	}
	if fields.hasParent {
		updates["parent_id"] = fields.parentID
	}
	if fields.hasSortOrder {
		updates["sort_order"] = fields.sortOrder
	}
	err = handler.db.WithContext(c.Request.Context()).Model(&Label{}).Where("id = ?", label.ID).Updates(updates).Error
	if err != nil {
		if isUniqueViolation(err) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Label with the same name already exists in the project"})
			return
		}
		handler.internalError(c, err)
		return
	}
	if err := handler.db.WithContext(c.Request.Context()).Where("id = ?", label.ID).Take(&label).Error; err != nil {
		handler.internalError(c, err)
		return
	}
	if err := handler.invalidateWorkspaceLabels(c.Request.Context(), slug); err != nil {
		handler.internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, labelJSON(label))
}

func (handler *Handler) labelDelete(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin) {
		return
	}
	slug := c.Param("slug")
	label, found, err := handler.labelByID(c.Request.Context(), slug, c.Param("id"), c.Param("label"))
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		handler.notFound(c)
		return
	}
	now := handler.clock().UTC()
	err = handler.db.WithContext(c.Request.Context()).Model(&Label{}).Where("id = ?", label.ID).Updates(map[string]any{
		"deleted_at": now, "updated_at": now, "updated_by_id": user.ID,
	}).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if handler.tasks != nil {
		if err := handler.tasks.PublishSoftDeleteRelatedObjects(c.Request.Context(), "db", "label", label.ID); err != nil {
			handler.internalError(c, err)
			return
		}
	}
	if err := handler.invalidateWorkspaceLabels(c.Request.Context(), slug); err != nil {
		handler.internalError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// labelBulkCreate is BulkCreateIssueLabelsEndpoint, which seeds labels during an
// import. Every label gets a random colour, and conflicts are ignored.
func (handler *Handler) labelBulkCreate(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin) {
		return
	}
	slug, projectID := c.Param("slug"), c.Param("id")
	var project Project
	err := handler.db.WithContext(c.Request.Context()).
		Where("id = ? AND workspace_id = (SELECT id FROM workspaces WHERE slug = ?) AND deleted_at IS NULL", projectID, slug).
		Take(&project).Error
	if err != nil {
		handler.notFound(c)
		return
	}
	var request struct {
		LabelData []struct {
			Name        *string `json:"name"`
			Description *string `json:"description"`
		} `json:"label_data"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		handler.invalidDetail(c)
		return
	}
	now := handler.clock().UTC()
	labels := make([]Label, 0, len(request.LabelData))
	for _, definition := range request.LabelData {
		labelID, err := newUUID()
		if err != nil {
			handler.internalError(c, err)
			return
		}
		name, description := "Migrated", "Migrated Issue"
		if definition.Name != nil {
			name = *definition.Name
		}
		if definition.Description != nil {
			description = *definition.Description
		}
		colour, err := randomLabelColour()
		if err != nil {
			handler.internalError(c, err)
			return
		}
		labels = append(labels, Label{
			ID: labelID, CreatedAt: now, UpdatedAt: now,
			CreatedByID: &user.ID, UpdatedByID: &user.ID,
			ProjectID: &projectID, WorkspaceID: project.WorkspaceID,
			Name: name, Description: description, Color: colour, SortOrder: 65535,
		})
	}
	if len(labels) > 0 {
		err := handler.db.WithContext(c.Request.Context()).
			Clauses(clause.OnConflict{DoNothing: true}).Create(&labels).Error
		if err != nil {
			handler.internalError(c, err)
			return
		}
	}
	// Django serializes the list it handed to bulk_create, so a row dropped by
	// ignore_conflicts still appears in the response.
	response := make([]gin.H, 0, len(labels))
	for _, label := range labels {
		response = append(response, labelJSON(label))
	}
	c.JSON(http.StatusCreated, gin.H{"labels": response})
}

// requireWorkspaceMembership is ProjectBasePermission's safe method branch.
func (handler *Handler) requireWorkspaceMembership(c *gin.Context, user *auth.User) bool {
	role, err := handler.workspaceRole(c.Request.Context(), c.Param("slug"), user.ID)
	if err != nil {
		handler.internalError(c, err)
		return false
	}
	if role == 0 {
		c.JSON(http.StatusForbidden, gin.H{"detail": "You do not have permission to perform this action."})
		return false
	}
	return true
}

func (handler *Handler) labelByID(ctx context.Context, slug, projectID, labelID string) (Label, bool, error) {
	var label Label
	err := handler.db.WithContext(ctx).
		Where("id = ? AND project_id = ? AND workspace_id = (SELECT id FROM workspaces WHERE slug = ?) AND deleted_at IS NULL",
			labelID, projectID, slug).Take(&label).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Label{}, false, nil
	}
	return label, err == nil, err
}

// labelNameTaken is validate_name: a case insensitive match within the project,
// excluding the row being updated.
func (handler *Handler) labelNameTaken(ctx context.Context, projectID, name, excludeID string) (bool, error) {
	query := handler.db.WithContext(ctx).Model(&Label{}).
		Where("project_id = ? AND LOWER(name) = LOWER(?) AND deleted_at IS NULL", projectID, name)
	if excludeID != "" {
		query = query.Where("id <> ?", excludeID)
	}
	var count int64
	err := query.Count(&count).Error
	return count > 0, err
}

// nextLabelSortOrder is Label.save: a new label lands 10000 past the project's
// current highest, or at the 65535 default when the project has none.
func (handler *Handler) nextLabelSortOrder(ctx context.Context, projectID string) (float64, error) {
	var highest *float64
	err := handler.db.WithContext(ctx).Model(&Label{}).
		Where("project_id = ? AND deleted_at IS NULL", projectID).
		Select("MAX(sort_order)").Scan(&highest).Error
	if err != nil {
		return 0, err
	}
	if highest == nil {
		return 65535, nil
	}
	return *highest + 10000, nil
}

// invalidateWorkspaceLabels drops the cached workspace label list, which is
// what the invalidate_cache decorator on these routes does.
func (handler *Handler) invalidateWorkspaceLabels(ctx context.Context, slug string) error {
	if handler.cache == nil {
		return nil
	}
	return handler.cache.InvalidatePattern(ctx, "*/api/workspaces/"+slug+"/labels/*")
}

type labelInput struct {
	name           string
	description    string
	color          string
	parentID       *string
	sortOrder      float64
	hasName        bool
	hasDescription bool
	hasColor       bool
	hasParent      bool
	hasSortOrder   bool
}

func (handler *Handler) labelBody(c *gin.Context) (map[string]json.RawMessage, bool) {
	var body map[string]json.RawMessage
	if err := c.ShouldBindJSON(&body); err != nil {
		handler.invalidDetail(c)
		return nil, false
	}
	return body, true
}

func (handler *Handler) labelFields(c *gin.Context, partial bool) (labelInput, bool) {
	body, ok := handler.labelBody(c)
	if !ok {
		return labelInput{}, false
	}
	return handler.labelFieldsFrom(c, body, partial)
}

func (handler *Handler) labelFieldsFrom(c *gin.Context, body map[string]json.RawMessage, partial bool) (labelInput, bool) {
	result := labelInput{}
	if raw, exists := body["name"]; exists {
		result.hasName = true
		value, ok := handler.stringField(c, "name", raw, 255)
		if !ok {
			return labelInput{}, false
		}
		result.name = value
	} else if !partial {
		c.JSON(http.StatusBadRequest, gin.H{"name": []string{"This field is required."}})
		return labelInput{}, false
	}
	for _, optional := range []struct {
		name   string
		value  *string
		set    *bool
		length int
	}{
		{name: "description", value: &result.description, set: &result.hasDescription},
		{name: "color", value: &result.color, set: &result.hasColor, length: 255},
	} {
		raw, exists := body[optional.name]
		if !exists {
			continue
		}
		*optional.set = true
		if string(raw) == "null" {
			c.JSON(http.StatusBadRequest, gin.H{optional.name: []string{"This field may not be null."}})
			return labelInput{}, false
		}
		var value string
		if json.Unmarshal(raw, &value) != nil {
			c.JSON(http.StatusBadRequest, gin.H{optional.name: []string{"Not a valid string."}})
			return labelInput{}, false
		}
		// description and color are blank-allowed on the model.
		if optional.length > 0 && utf8.RuneCountInString(value) > optional.length {
			c.JSON(http.StatusBadRequest, gin.H{optional.name: []string{fmt.Sprintf("Ensure this field has no more than %d characters.", optional.length)}})
			return labelInput{}, false
		}
		*optional.value = value
	}
	if raw, exists := body["parent"]; exists {
		result.hasParent = true
		if string(raw) != "null" {
			var value string
			if json.Unmarshal(raw, &value) != nil {
				c.JSON(http.StatusBadRequest, gin.H{"parent": []string{fmt.Sprintf("“%s” is not a valid UUID.", value)}})
				return labelInput{}, false
			}
			canonical, valid := canonicalUUID(value)
			if !valid {
				c.JSON(http.StatusBadRequest, gin.H{"parent": []string{fmt.Sprintf("“%s” is not a valid UUID.", value)}})
				return labelInput{}, false
			}
			exists, err := handler.rowExists(c.Request.Context(), "labels", canonical)
			if err != nil {
				handler.internalError(c, err)
				return labelInput{}, false
			}
			if !exists {
				c.JSON(http.StatusBadRequest, gin.H{"parent": []string{`Invalid pk "` + canonical + `" - object does not exist.`}})
				return labelInput{}, false
			}
			result.parentID = &canonical
		}
	}
	if raw, exists := body["sort_order"]; exists {
		result.hasSortOrder = true
		var value float64
		if json.Unmarshal(raw, &value) != nil {
			c.JSON(http.StatusBadRequest, gin.H{"sort_order": []string{"A valid number is required."}})
			return labelInput{}, false
		}
		result.sortOrder = value
	}
	return result, true
}

// labelJSON is LabelSerializer, whose field list is narrower than the model and
// uses the project_id and workspace_id attribute names.
func labelJSON(label Label) gin.H {
	return gin.H{
		"parent": label.ParentID, "name": label.Name, "color": label.Color,
		"id": label.ID, "project_id": label.ProjectID,
		"workspace_id": label.WorkspaceID, "sort_order": label.SortOrder,
	}
}

func randomLabelColour() (string, error) {
	value := make([]byte, 3)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return strings.ToUpper(fmt.Sprintf("#%02X%02X%02X", value[0], value[1], value[2])), nil
}
