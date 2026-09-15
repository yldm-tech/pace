package workspace

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var workspacePreferenceKeys = []string{"views", "active_cycles", "analytics", "drafts", "your_work", "archives", "stickies"}

func (handler *Handler) sidebarPreferencesGet(c *gin.Context, user *auth.User) {
	if !handler.requireWorkspaceMember(c, user) {
		return
	}
	if err := handler.ensureSidebarPreferences(c, user); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			handler.notFound(c)
			return
		}
		handler.internalError(c, err)
		return
	}
	var preferences []WorkspaceUserPreference
	err := handler.db.WithContext(c.Request.Context()).Where(
		"workspace_id = (SELECT id FROM workspaces WHERE slug = ? AND deleted_at IS NULL) AND user_id = ? AND deleted_at IS NULL",
		c.Param("slug"), user.ID,
	).Order("sort_order").Find(&preferences).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	response := make(gin.H, len(preferences))
	for _, preference := range preferences {
		response[preference.Key] = gin.H{"is_pinned": preference.IsPinned, "sort_order": preference.SortOrder}
	}
	c.JSON(http.StatusOK, response)
}

func (handler *Handler) sidebarPreferencesPatch(c *gin.Context, user *auth.User) {
	if !handler.requireWorkspaceMember(c, user) {
		return
	}
	var entries []map[string]json.RawMessage
	if err := c.ShouldBindJSON(&entries); err != nil {
		handler.invalidDetail(c)
		return
	}
	for _, entry := range entries {
		rawKey, exists := entry["key"]
		if !exists {
			continue
		}
		var key string
		if json.Unmarshal(rawKey, &key) != nil || key == "" {
			continue
		}
		var preference WorkspaceUserPreference
		query := handler.db.WithContext(c.Request.Context()).Where(
			"key = ? AND workspace_id = (SELECT id FROM workspaces WHERE slug = ? AND deleted_at IS NULL) AND user_id = ? AND deleted_at IS NULL",
			key, c.Param("slug"), user.ID,
		)
		if err := query.First(&preference).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			continue
		} else if err != nil {
			handler.internalError(c, err)
			return
		}
		// Django saves with update_fields=["is_pinned", "sort_order"], so neither
		// the auto_now updated_at nor updated_by is written on this route.
		updates := map[string]any{}
		if raw, ok := entry["is_pinned"]; ok {
			var isPinned bool
			if json.Unmarshal(raw, &isPinned) != nil {
				c.JSON(http.StatusBadRequest, gin.H{"is_pinned": []string{"Must be a valid boolean."}})
				return
			}
			updates["is_pinned"] = isPinned
		}
		if raw, ok := entry["sort_order"]; ok {
			value, err := preferenceFloat(raw)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"sort_order": []string{"A valid number is required."}})
				return
			}
			updates["sort_order"] = value
		}
		if len(updates) == 0 {
			continue
		}
		if err := handler.db.WithContext(c.Request.Context()).Model(&WorkspaceUserPreference{}).Where("id = ?", preference.ID).Updates(updates).Error; err != nil {
			handler.internalError(c, err)
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"message": "Successfully updated"})
}

func (handler *Handler) ensureSidebarPreferences(c *gin.Context, user *auth.User) error {
	return handler.ensureSidebarPreferencesFor(c.Request.Context(), c.Param("slug"), user)
}

func (handler *Handler) ensureSidebarPreferencesFor(ctx context.Context, slug string, user *auth.User) error {
	var workspaceID string
	if err := handler.db.WithContext(ctx).Table("workspaces").Where("slug = ? AND deleted_at IS NULL", slug).Pluck("id", &workspaceID).Error; err != nil {
		return err
	}
	if workspaceID == "" {
		return gorm.ErrRecordNotFound
	}
	var existing []WorkspaceUserPreference
	if err := handler.db.WithContext(ctx).Where("workspace_id = ? AND user_id = ? AND deleted_at IS NULL", workspaceID, user.ID).Find(&existing).Error; err != nil {
		return err
	}
	seen := make(map[string]struct{}, len(existing))
	for _, preference := range existing {
		seen[preference.Key] = struct{}{}
	}
	now := handler.clock().UTC()
	missing := make([]WorkspaceUserPreference, 0, len(workspacePreferenceKeys))
	missingIndex := 0
	for _, key := range workspacePreferenceKeys {
		if _, ok := seen[key]; ok {
			continue
		}
		id, err := newUUID()
		if err != nil {
			return err
		}
		// Django bulk_create bypasses BaseModel.save, so created_by stays null.
		missing = append(missing, WorkspaceUserPreference{
			ID: id, CreatedAt: now, UpdatedAt: now,
			WorkspaceID: workspaceID, UserID: user.ID, Key: key,
			IsPinned:  key == "drafts" || key == "your_work" || key == "stickies",
			SortOrder: 65535 + float64(missingIndex)*10000,
		})
		missingIndex++
	}
	if len(missing) == 0 {
		return nil
	}
	return handler.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&missing).Error
}

func preferenceFloat(raw json.RawMessage) (float64, error) {
	if string(raw) == "null" {
		return 0, errors.New("null")
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return 0, err
	}
	var text string
	switch typed := value.(type) {
	case json.Number:
		text = typed.String()
	case string:
		text = typed
	default:
		return 0, errors.New("invalid number")
	}
	result, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsNaN(result) || math.IsInf(result, 0) {
		return 0, fmt.Errorf("invalid number")
	}
	return result, nil
}

func sidebarPreferenceJSON(preference WorkspaceUserPreference) gin.H {
	return gin.H{"key": preference.Key, "is_pinned": preference.IsPinned, "sort_order": preference.SortOrder, "updated_at": preference.UpdatedAt}
}
