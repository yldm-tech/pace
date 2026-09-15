package workspace

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"github.com/yldm-tech/pace/apps/api-go/internal/validate"
	"gorm.io/gorm"
)

func (handler *Handler) quickLinkList(c *gin.Context, user *auth.User) {
	if !handler.requireWorkspaceMember(c, user) {
		return
	}
	var links []WorkspaceUserLink
	err := handler.db.WithContext(c.Request.Context()).Where(
		"workspace_id = (SELECT id FROM workspaces WHERE slug = ? AND deleted_at IS NULL) AND owner_id = ? AND deleted_at IS NULL",
		c.Param("slug"), user.ID,
	).Order("created_at DESC").Find(&links).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	response := make([]gin.H, 0, len(links))
	for _, link := range links {
		response = append(response, quickLinkJSON(link))
	}
	c.JSON(http.StatusOK, response)
}

func (handler *Handler) quickLinkCreate(c *gin.Context, user *auth.User) {
	if !handler.requireWorkspaceMember(c, user) {
		return
	}
	fields, ok := handler.quickLinkFields(c, false)
	if !ok {
		return
	}
	if !handler.validateQuickLinkReferences(c, fields) {
		return
	}
	var workspace Workspace
	if err := handler.db.WithContext(c.Request.Context()).Where("slug = ? AND deleted_at IS NULL", c.Param("slug")).Take(&workspace).Error; err != nil {
		handler.notFound(c)
		return
	}
	workspaceID := workspace.ID
	if fields.projectID != nil {
		// WorkspaceBaseModel.save resolves the workspace from the project.
		projectWorkspaceID, err := handler.projectWorkspace(c, *fields.projectID)
		if err != nil {
			handler.internalError(c, err)
			return
		}
		workspaceID = projectWorkspaceID
	}
	linkID, err := newUUID()
	if err != nil {
		handler.internalError(c, err)
		return
	}
	now := handler.clock().UTC()
	metadata := fields.metadata
	if !fields.hasMetadata {
		metadata = emptyJSON()
	}
	link := WorkspaceUserLink{
		ID: linkID, CreatedAt: now, UpdatedAt: now,
		// BaseModel.save stamps the acting user over any created_by in the body.
		CreatedByID: &user.ID,
		WorkspaceID: workspaceID, ProjectID: fields.projectID, OwnerID: user.ID,
		Title: fields.title, URL: fields.url, Metadata: metadata,
		DeletedAt: fields.deletedAt,
	}
	if err := handler.db.WithContext(c.Request.Context()).Create(&link).Error; err != nil {
		if isUniqueViolation(err) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "The payload is not valid"})
			return
		}
		handler.internalError(c, err)
		return
	}
	c.JSON(http.StatusCreated, quickLinkJSON(link))
}

func (handler *Handler) quickLinkRetrieve(c *gin.Context, user *auth.User) {
	if !handler.requireWorkspaceMember(c, user) {
		return
	}
	link, ok := handler.quickLinkObject(c, user, gin.H{"error": "Quick link not found."})
	if !ok {
		return
	}
	c.JSON(http.StatusOK, quickLinkJSON(link))
}

func (handler *Handler) quickLinkPatch(c *gin.Context, user *auth.User) {
	if !handler.requireWorkspaceMember(c, user) {
		return
	}
	// Django reports a missing quick link with "detail" on this route and with
	// "error" on the retrieve route.
	link, ok := handler.quickLinkObject(c, user, gin.H{"detail": "Quick link not found."})
	if !ok {
		return
	}
	fields, ok := handler.quickLinkFields(c, true)
	if !ok {
		return
	}
	if !handler.validateQuickLinkReferences(c, fields) {
		return
	}
	updates := map[string]any{
		"updated_at":    handler.clock().UTC(),
		"updated_by_id": user.ID,
	}
	if fields.hasTitle {
		updates["title"] = fields.title
	}
	if fields.hasURL {
		updates["url"] = fields.url
	}
	if fields.hasMetadata {
		updates["metadata"] = fields.metadata
	}
	if fields.hasDeletedAt {
		updates["deleted_at"] = fields.deletedAt
	}
	if fields.hasCreatedBy {
		updates["created_by_id"] = fields.createdByID
	}
	// WorkspaceBaseModel.save runs on every update and re-resolves the workspace
	// from whichever project the row ends up pointing at.
	projectID := link.ProjectID
	if fields.hasProject {
		updates["project_id"] = fields.projectID
		projectID = fields.projectID
	}
	if projectID != nil {
		projectWorkspaceID, err := handler.projectWorkspace(c, *projectID)
		if err != nil {
			handler.internalError(c, err)
			return
		}
		if projectWorkspaceID != "" {
			updates["workspace_id"] = projectWorkspaceID
		}
	}
	if err := handler.db.WithContext(c.Request.Context()).Model(&WorkspaceUserLink{}).Where("id = ?", link.ID).Updates(updates).Error; err != nil {
		if isUniqueViolation(err) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "The payload is not valid"})
			return
		}
		handler.internalError(c, err)
		return
	}
	if err := handler.db.WithContext(c.Request.Context()).Where("id = ?", link.ID).Take(&link).Error; err != nil {
		handler.internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, quickLinkJSON(link))
}

func (handler *Handler) quickLinkDelete(c *gin.Context, user *auth.User) {
	if !handler.requireWorkspaceMember(c, user) {
		return
	}
	// Django lets the DoesNotExist bubble up to BaseAPIView.handle_exception here.
	link, ok := handler.quickLinkObject(c, user, gin.H{"error": "The required object does not exist."})
	if !ok {
		return
	}
	now := handler.clock().UTC()
	err := handler.db.WithContext(c.Request.Context()).Model(&WorkspaceUserLink{}).Where("id = ?", link.ID).Updates(map[string]any{
		"deleted_at":    now,
		"updated_at":    now,
		"updated_by_id": user.ID,
	}).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if handler.tasks != nil {
		if err := handler.tasks.PublishSoftDeleteRelatedObjects(c.Request.Context(), "db", "workspaceuserlink", link.ID); err != nil {
			handler.internalError(c, err)
			return
		}
	}
	c.Status(http.StatusNoContent)
}

func (handler *Handler) quickLinkObject(c *gin.Context, user *auth.User, missing gin.H) (WorkspaceUserLink, bool) {
	var link WorkspaceUserLink
	err := handler.db.WithContext(c.Request.Context()).Where(
		"id = ? AND workspace_id = (SELECT id FROM workspaces WHERE slug = ? AND deleted_at IS NULL) AND owner_id = ? AND deleted_at IS NULL",
		c.Param("id"), c.Param("slug"), user.ID,
	).Take(&link).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, missing)
		return WorkspaceUserLink{}, false
	}
	if err != nil {
		handler.internalError(c, err)
		return WorkspaceUserLink{}, false
	}
	return link, true
}

func (handler *Handler) projectWorkspace(c *gin.Context, projectID string) (string, error) {
	var workspaceID string
	err := handler.db.WithContext(c.Request.Context()).Table("projects").
		Where("id = ? AND deleted_at IS NULL", projectID).Pluck("workspace_id", &workspaceID).Error
	return workspaceID, err
}

type quickLinkInput struct {
	title        *string
	url          string
	metadata     auth.JSONValue
	projectID    *string
	createdByID  *string
	updatedByID  *string
	deletedAt    *time.Time
	hasTitle     bool
	hasURL       bool
	hasMetadata  bool
	hasProject   bool
	hasCreatedBy bool
	hasUpdatedBy bool
	hasDeletedAt bool
}

func (handler *Handler) quickLinkFields(c *gin.Context, partial bool) (quickLinkInput, bool) {
	var fields map[string]json.RawMessage
	if err := c.ShouldBindJSON(&fields); err != nil {
		handler.invalidDetail(c)
		return quickLinkInput{}, false
	}
	result := quickLinkInput{}
	if raw, exists := fields["title"]; exists {
		result.hasTitle = true
		if string(raw) != "null" {
			var value string
			if json.Unmarshal(raw, &value) != nil {
				c.JSON(http.StatusBadRequest, gin.H{"title": []string{"Not a valid string."}})
				return quickLinkInput{}, false
			}
			// CharField trims before the blank and length checks, and a blank
			// value short-circuits the length validator.
			value = strings.TrimSpace(value)
			if value != "" && utf8.RuneCountInString(value) > 255 {
				c.JSON(http.StatusBadRequest, gin.H{"title": []string{"Ensure this field has no more than 255 characters."}})
				return quickLinkInput{}, false
			}
			result.title = &value
		}
	}
	if raw, exists := fields["url"]; exists {
		result.hasURL = true
		if string(raw) == "null" {
			c.JSON(http.StatusBadRequest, gin.H{"url": []string{"This field may not be null."}})
			return quickLinkInput{}, false
		}
		var value string
		if json.Unmarshal(raw, &value) != nil {
			c.JSON(http.StatusBadRequest, gin.H{"url": []string{"Not a valid string."}})
			return quickLinkInput{}, false
		}
		value = validate.PrefixScheme(value)
		if strings.TrimSpace(value) == "" {
			c.JSON(http.StatusBadRequest, gin.H{"url": []string{"This field may not be blank."}})
			return quickLinkInput{}, false
		}
		if !validate.URL(value) {
			c.JSON(http.StatusBadRequest, gin.H{"url": gin.H{"error": "Invalid URL format."}})
			return quickLinkInput{}, false
		}
		result.url = value
	} else if !partial {
		c.JSON(http.StatusBadRequest, gin.H{"url": []string{"This field is required."}})
		return quickLinkInput{}, false
	}
	if raw, exists := fields["metadata"]; exists {
		result.hasMetadata = true
		if string(raw) == "null" {
			c.JSON(http.StatusBadRequest, gin.H{"metadata": []string{"This field may not be null."}})
			return quickLinkInput{}, false
		}
		if !json.Valid(raw) {
			c.JSON(http.StatusBadRequest, gin.H{"metadata": []string{"Value must be valid JSON."}})
			return quickLinkInput{}, false
		}
		result.metadata = auth.JSONValue(append([]byte(nil), raw...))
	}
	if raw, exists := fields["deleted_at"]; exists {
		result.hasDeletedAt = true
		if string(raw) != "null" {
			var value string
			if json.Unmarshal(raw, &value) != nil {
				c.JSON(http.StatusBadRequest, gin.H{"deleted_at": []string{"Datetime has wrong format. Use one of these formats instead: YYYY-MM-DDThh:mm[:ss[.uuuuuu]][+HH:MM|-HH:MM|Z]."}})
				return quickLinkInput{}, false
			}
			parsed, err := time.Parse(time.RFC3339Nano, value)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"deleted_at": []string{"Datetime has wrong format. Use one of these formats instead: YYYY-MM-DDThh:mm[:ss[.uuuuuu]][+HH:MM|-HH:MM|Z]."}})
				return quickLinkInput{}, false
			}
			result.deletedAt = &parsed
		}
	}
	for _, relation := range []struct {
		name  string
		value **string
		set   *bool
	}{
		{name: "project", value: &result.projectID, set: &result.hasProject},
		{name: "created_by", value: &result.createdByID, set: &result.hasCreatedBy},
		{name: "updated_by", value: &result.updatedByID, set: &result.hasUpdatedBy},
	} {
		raw, exists := fields[relation.name]
		if !exists {
			continue
		}
		*relation.set = true
		if string(raw) == "null" {
			continue
		}
		var value string
		if json.Unmarshal(raw, &value) != nil {
			c.JSON(http.StatusBadRequest, gin.H{relation.name: []string{fmt.Sprintf("“%s” is not a valid UUID.", value)}})
			return quickLinkInput{}, false
		}
		canonical, valid := canonicalThemeUUID(value)
		if !valid {
			c.JSON(http.StatusBadRequest, gin.H{relation.name: []string{fmt.Sprintf("“%s” is not a valid UUID.", value)}})
			return quickLinkInput{}, false
		}
		*relation.value = &canonical
	}
	return result, true
}

func (handler *Handler) validateQuickLinkReferences(c *gin.Context, fields quickLinkInput) bool {
	for _, reference := range []struct {
		field  string
		table  string
		target *string
	}{
		{field: "project", table: "projects", target: fields.projectID},
		{field: "created_by", table: "users", target: fields.createdByID},
		{field: "updated_by", table: "users", target: fields.updatedByID},
	} {
		if reference.target == nil {
			continue
		}
		query := handler.db.WithContext(c.Request.Context()).Table(reference.table).Where("id = ?", *reference.target)
		if reference.table == "projects" {
			query = query.Where("deleted_at IS NULL")
		}
		var count int64
		if err := query.Count(&count).Error; err != nil {
			handler.internalError(c, err)
			return false
		}
		if count == 0 {
			c.JSON(http.StatusBadRequest, gin.H{reference.field: []string{`Invalid pk "` + *reference.target + `" - object does not exist.`}})
			return false
		}
	}
	return true
}

func quickLinkJSON(link WorkspaceUserLink) gin.H {
	return gin.H{
		"id": link.ID, "created_at": link.CreatedAt, "updated_at": link.UpdatedAt,
		"deleted_at": link.DeletedAt, "title": link.Title, "url": link.URL,
		"metadata": decodeJSON(link.Metadata), "created_by": link.CreatedByID,
		"updated_by": link.UpdatedByID, "workspace": link.WorkspaceID,
		"project": link.ProjectID, "owner": link.OwnerID,
	}
}
