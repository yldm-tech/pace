package workspace

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"github.com/yldm-tech/pace/apps/api-go/internal/drf"
	"gorm.io/gorm"
)

func (handler *Handler) themeList(c *gin.Context, user *auth.User) {
	if !handler.requireThemeAccess(c, user) {
		return
	}
	var themes []WorkspaceTheme
	err := handler.db.WithContext(c.Request.Context()).
		Where("workspace_id = (SELECT id FROM workspaces WHERE slug = ? AND deleted_at IS NULL) AND deleted_at IS NULL", c.Param("slug")).
		Order("created_at DESC").Find(&themes).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	response := make([]gin.H, 0, len(themes))
	for _, theme := range themes {
		response = append(response, themeJSON(theme))
	}
	drf.Respond(c, http.StatusOK, response)
}

func (handler *Handler) themeCreate(c *gin.Context, user *auth.User) {
	if !handler.requireThemeAccess(c, user) {
		return
	}
	fields, ok := handler.themeFields(c, false)
	if !ok {
		return
	}
	if !handler.validateThemeUserReferences(c, fields) {
		return
	}
	var workspace Workspace
	if err := handler.db.WithContext(c.Request.Context()).Where("slug = ? AND deleted_at IS NULL", c.Param("slug")).Take(&workspace).Error; err != nil {
		handler.notFound(c)
		return
	}
	themeID, err := newUUID()
	if err != nil {
		handler.internalError(c, err)
		return
	}
	now := handler.clock().UTC()
	theme := WorkspaceTheme{
		ID:          themeID,
		CreatedAt:   now,
		UpdatedAt:   now,
		CreatedByID: &user.ID,
		WorkspaceID: workspace.ID,
		Name:        fields.name,
		ActorID:     user.ID,
		Colors:      fields.colors,
		DeletedAt:   fields.deletedAt,
	}
	if err := handler.db.WithContext(c.Request.Context()).Create(&theme).Error; err != nil {
		if isUniqueViolation(err) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "The payload is not valid"})
			return
		}
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusCreated, themeJSON(theme))
}

func (handler *Handler) themeRetrieve(c *gin.Context, user *auth.User) {
	if !handler.requireThemeAccess(c, user) {
		return
	}
	theme, ok := handler.themeObject(c)
	if !ok {
		return
	}
	drf.Respond(c, http.StatusOK, themeJSON(theme))
}

func (handler *Handler) themePatch(c *gin.Context, user *auth.User) {
	if !handler.requireThemeAccess(c, user) {
		return
	}
	theme, ok := handler.themeObject(c)
	if !ok {
		return
	}
	fields, ok := handler.themeFields(c, true)
	if !ok {
		return
	}
	if !handler.validateThemeUserReferences(c, fields) {
		return
	}
	updates := map[string]any{
		"updated_at":    handler.clock().UTC(),
		"updated_by_id": user.ID,
	}
	if fields.hasName {
		updates["name"] = fields.name
	}
	if fields.hasColors {
		updates["colors"] = fields.colors
	}
	if fields.hasDeletedAt {
		updates["deleted_at"] = fields.deletedAt
	}
	if fields.hasCreatedBy {
		updates["created_by_id"] = fields.createdByID
	}
	if err := handler.db.WithContext(c.Request.Context()).Model(&WorkspaceTheme{}).Where("id = ?", theme.ID).Updates(updates).Error; err != nil {
		if isUniqueViolation(err) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "The payload is not valid"})
			return
		}
		handler.internalError(c, err)
		return
	}
	if err := handler.db.WithContext(c.Request.Context()).Where("id = ?", theme.ID).Take(&theme).Error; err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, themeJSON(theme))
}

func (handler *Handler) themeDelete(c *gin.Context, user *auth.User) {
	if !handler.requireThemeAccess(c, user) {
		return
	}
	theme, ok := handler.themeObject(c)
	if !ok {
		return
	}
	now := handler.clock().UTC()
	err := handler.db.WithContext(c.Request.Context()).Model(&WorkspaceTheme{}).Where("id = ?", theme.ID).Updates(map[string]any{
		"deleted_at":    now,
		"updated_at":    now,
		"updated_by_id": user.ID,
	}).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if handler.tasks != nil {
		if err := handler.tasks.PublishSoftDeleteRelatedObjects(c.Request.Context(), "db", "workspacetheme", theme.ID); err != nil {
			handler.internalError(c, err)
			return
		}
	}
	c.Status(http.StatusNoContent)
}

func (handler *Handler) requireThemeAccess(c *gin.Context, user *auth.User) bool {
	role, err := handler.workspaceRole(c.Request.Context(), c.Param("slug"), user.ID)
	if err != nil || (role != roleAdmin && role != roleMember) {
		c.JSON(http.StatusForbidden, gin.H{"detail": "You do not have permission to perform this action."})
		return false
	}
	return true
}

func (handler *Handler) themeObject(c *gin.Context) (WorkspaceTheme, bool) {
	var theme WorkspaceTheme
	err := handler.db.WithContext(c.Request.Context()).Where(
		"id = ? AND workspace_id = (SELECT id FROM workspaces WHERE slug = ? AND deleted_at IS NULL) AND deleted_at IS NULL",
		c.Param("id"), c.Param("slug"),
	).Take(&theme).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"detail": "No WorkspaceTheme matches the given query."})
		return WorkspaceTheme{}, false
	}
	if err != nil {
		handler.internalError(c, err)
		return WorkspaceTheme{}, false
	}
	return theme, true
}

type themeInput struct {
	name         string
	colors       auth.JSONValue
	deletedAt    *time.Time
	createdByID  *string
	updatedByID  *string
	hasName      bool
	hasColors    bool
	hasDeletedAt bool
	hasCreatedBy bool
	hasUpdatedBy bool
}

func (handler *Handler) themeFields(c *gin.Context, partial bool) (themeInput, bool) {
	var fields map[string]json.RawMessage
	if err := c.ShouldBindJSON(&fields); err != nil {
		handler.invalidDetail(c)
		return themeInput{}, false
	}
	result := themeInput{colors: emptyJSON()}
	if raw, exists := fields["name"]; exists {
		result.hasName = true
		if string(raw) == "null" {
			c.JSON(http.StatusBadRequest, gin.H{"name": []string{"This field may not be null."}})
			return themeInput{}, false
		}
		if err := json.Unmarshal(raw, &result.name); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"name": []string{"Not a valid string."}})
			return themeInput{}, false
		}
		result.name = strings.TrimSpace(result.name)
		if result.name == "" {
			c.JSON(http.StatusBadRequest, gin.H{"name": []string{"This field may not be blank."}})
			return themeInput{}, false
		}
		if utf8.RuneCountInString(result.name) > 300 {
			c.JSON(http.StatusBadRequest, gin.H{"name": []string{"Ensure this field has no more than 300 characters."}})
			return themeInput{}, false
		}
	} else if !partial {
		c.JSON(http.StatusBadRequest, gin.H{"name": []string{"This field is required."}})
		return themeInput{}, false
	}
	if raw, exists := fields["colors"]; exists {
		result.hasColors = true
		if string(raw) == "null" {
			c.JSON(http.StatusBadRequest, gin.H{"colors": []string{"This field may not be null."}})
			return themeInput{}, false
		}
		if !json.Valid(raw) {
			handler.invalidDetail(c)
			return themeInput{}, false
		}
		result.colors = auth.JSONValue(append([]byte(nil), raw...))
	}
	if raw, exists := fields["deleted_at"]; exists {
		result.hasDeletedAt = true
		if string(raw) != "null" {
			var value string
			if json.Unmarshal(raw, &value) != nil {
				c.JSON(http.StatusBadRequest, gin.H{"deleted_at": []string{"Datetime has wrong format. Use one of these formats instead: YYYY-MM-DDThh:mm[:ss[.uuuuuu]][+HH:MM|-HH:MM|Z]."}})
				return themeInput{}, false
			}
			parsed, err := time.Parse(time.RFC3339Nano, value)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"deleted_at": []string{"Datetime has wrong format. Use one of these formats instead: YYYY-MM-DDThh:mm[:ss[.uuuuuu]][+HH:MM|-HH:MM|Z]."}})
				return themeInput{}, false
			}
			result.deletedAt = &parsed
		}
	}
	for _, auditField := range []struct {
		name  string
		value **string
		set   *bool
	}{
		{name: "created_by", value: &result.createdByID, set: &result.hasCreatedBy},
		{name: "updated_by", value: &result.updatedByID, set: &result.hasUpdatedBy},
	} {
		raw, exists := fields[auditField.name]
		if !exists {
			continue
		}
		*auditField.set = true
		if string(raw) == "null" {
			continue
		}
		var value string
		if json.Unmarshal(raw, &value) != nil {
			c.JSON(http.StatusBadRequest, gin.H{auditField.name: []string{fmt.Sprintf("“%s” is not a valid UUID.", value)}})
			return themeInput{}, false
		}
		canonical, valid := canonicalThemeUUID(value)
		if !valid {
			c.JSON(http.StatusBadRequest, gin.H{auditField.name: []string{fmt.Sprintf("“%s” is not a valid UUID.", value)}})
			return themeInput{}, false
		}
		*auditField.value = &canonical
	}
	return result, true
}

func (handler *Handler) validateThemeUserReferences(c *gin.Context, fields themeInput) bool {
	for _, reference := range []struct {
		field  string
		userID *string
	}{
		{field: "created_by", userID: fields.createdByID},
		{field: "updated_by", userID: fields.updatedByID},
	} {
		field, userID := reference.field, reference.userID
		if userID == nil {
			continue
		}
		var count int64
		if err := handler.db.WithContext(c.Request.Context()).Table("users").Where("id = ?", *userID).Count(&count).Error; err != nil {
			handler.internalError(c, err)
			return false
		}
		if count == 0 {
			c.JSON(http.StatusBadRequest, gin.H{field: []string{`Invalid pk "` + *userID + `" - object does not exist.`}})
			return false
		}
	}
	return true
}

func themeJSON(theme WorkspaceTheme) gin.H {
	return gin.H{
		"id": theme.ID, "created_at": theme.CreatedAt, "updated_at": theme.UpdatedAt,
		"created_by": theme.CreatedByID, "updated_by": theme.UpdatedByID, "deleted_at": theme.DeletedAt,
		"workspace": theme.WorkspaceID, "name": theme.Name, "actor": theme.ActorID,
		"colors": decodeJSON(theme.Colors),
	}
}

func canonicalThemeUUID(value string) (string, bool) {
	compact := strings.TrimSpace(strings.ToLower(value))
	compact = strings.TrimPrefix(compact, "urn:uuid:")
	if strings.HasPrefix(compact, "{") && strings.HasSuffix(compact, "}") {
		compact = strings.TrimSuffix(strings.TrimPrefix(compact, "{"), "}")
	}
	compact = strings.ReplaceAll(compact, "-", "")
	if len(compact) != 32 {
		return "", false
	}
	decoded, err := hex.DecodeString(compact)
	if err != nil {
		return "", false
	}
	return fmt.Sprintf("%x-%x-%x-%x-%x", decoded[0:4], decoded[4:6], decoded[6:8], decoded[8:10], decoded[10:16]), true
}
