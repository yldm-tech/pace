package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api/internal/auth"
	"github.com/yldm-tech/pace/apps/api/internal/drf"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Django seeds every HomeWidgetKeys choice except quick_tutorial and
// new_at_plane, and the declaration order decides the seeded sort_order.
var workspaceHomePreferenceKeys = []string{"quick_links", "recents", "my_stickies"}

func (handler *Handler) homePreferencesGet(c *gin.Context, user *auth.User) {
	if !handler.requireWorkspaceMember(c, user) {
		return
	}
	if err := handler.ensureHomePreferencesFor(c.Request.Context(), c.Param("slug"), user); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			handler.notFound(c)
			return
		}
		handler.internalError(c, err)
		return
	}
	var preferences []WorkspaceHomePreference
	// Django orders by the model's Meta ordering ("-created_at"). The seeded rows
	// share a timestamp here because Go inserts them in one statement, so the
	// sort_order tiebreak reproduces the order Django's per-key inserts produce.
	err := handler.db.WithContext(c.Request.Context()).Where(
		"workspace_id = (SELECT id FROM workspaces WHERE slug = ? AND deleted_at IS NULL) AND user_id = ? AND deleted_at IS NULL",
		c.Param("slug"), user.ID,
	).Order("created_at DESC, sort_order ASC").Find(&preferences).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	response := make([]gin.H, 0, len(preferences))
	for _, preference := range preferences {
		response = append(response, gin.H{
			"key":        preference.Key,
			"is_enabled": preference.IsEnabled,
			"config":     decodeJSON(preference.Config),
			"sort_order": preference.SortOrder,
		})
	}
	drf.Respond(c, http.StatusOK, response)
}

func (handler *Handler) homePreferencePatch(c *gin.Context, user *auth.User) {
	if !handler.requireWorkspaceMember(c, user) {
		return
	}
	var preference WorkspaceHomePreference
	err := handler.db.WithContext(c.Request.Context()).Where(
		"key = ? AND workspace_id = (SELECT id FROM workspaces WHERE slug = ? AND deleted_at IS NULL) AND user_id = ? AND deleted_at IS NULL",
		c.Param("key"), c.Param("slug"), user.ID,
	).Order("created_at DESC").Take(&preference).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "Preference not found"})
		return
	}
	if err != nil {
		handler.internalError(c, err)
		return
	}
	fields, ok := handler.homePreferenceFields(c)
	if !ok {
		return
	}
	updates := map[string]any{
		"updated_at":    handler.clock().UTC(),
		"updated_by_id": user.ID,
	}
	if fields.hasKey {
		updates["key"] = fields.key
	}
	if fields.hasIsEnabled {
		updates["is_enabled"] = fields.isEnabled
	}
	if fields.hasSortOrder {
		updates["sort_order"] = fields.sortOrder
	}
	if err := handler.db.WithContext(c.Request.Context()).Model(&WorkspaceHomePreference{}).Where("id = ?", preference.ID).Updates(updates).Error; err != nil {
		if isUniqueViolation(err) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "The payload is not valid"})
			return
		}
		handler.internalError(c, err)
		return
	}
	if err := handler.db.WithContext(c.Request.Context()).Where("id = ?", preference.ID).Take(&preference).Error; err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, homePreferenceJSON(preference))
}

// ensureHomePreferencesFor reproduces Django's seeding loop: each key missing
// for the user is created with sort_order 1000 minus its one-based position
// among the missing keys, and existing rows are left untouched.
func (handler *Handler) ensureHomePreferencesFor(ctx context.Context, slug string, user *auth.User) error {
	var workspaceID string
	if err := handler.db.WithContext(ctx).Table("workspaces").Where("slug = ? AND deleted_at IS NULL", slug).Pluck("id", &workspaceID).Error; err != nil {
		return err
	}
	if workspaceID == "" {
		return gorm.ErrRecordNotFound
	}
	var existing []string
	err := handler.db.WithContext(ctx).Model(&WorkspaceHomePreference{}).
		Where("workspace_id = ? AND user_id = ? AND deleted_at IS NULL", workspaceID, user.ID).
		Pluck("key", &existing).Error
	if err != nil {
		return err
	}
	seen := make(map[string]struct{}, len(existing))
	for _, key := range existing {
		seen[key] = struct{}{}
	}
	now := handler.clock().UTC()
	missing := make([]WorkspaceHomePreference, 0, len(workspaceHomePreferenceKeys))
	for _, key := range workspaceHomePreferenceKeys {
		if _, ok := seen[key]; ok {
			continue
		}
		id, err := newUUID()
		if err != nil {
			return err
		}
		// Django bulk_create bypasses BaseModel.save, so created_by stays null.
		missing = append(missing, WorkspaceHomePreference{
			ID: id, CreatedAt: now, UpdatedAt: now,
			WorkspaceID: workspaceID, UserID: user.ID, Key: key,
			IsEnabled: true, Config: emptyJSON(),
			SortOrder: float64(1000 - (len(missing) + 1)),
		})
	}
	if len(missing) == 0 {
		return nil
	}
	return handler.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&missing).Error
}

type homePreferenceInput struct {
	key          string
	isEnabled    bool
	sortOrder    float64
	hasKey       bool
	hasIsEnabled bool
	hasSortOrder bool
}

func (handler *Handler) homePreferenceFields(c *gin.Context) (homePreferenceInput, bool) {
	var fields map[string]json.RawMessage
	if err := c.ShouldBindJSON(&fields); err != nil {
		handler.invalidDetail(c)
		return homePreferenceInput{}, false
	}
	result := homePreferenceInput{}
	if raw, exists := fields["key"]; exists {
		result.hasKey = true
		if string(raw) == "null" {
			c.JSON(http.StatusBadRequest, gin.H{"key": []string{"This field may not be null."}})
			return homePreferenceInput{}, false
		}
		if json.Unmarshal(raw, &result.key) != nil {
			c.JSON(http.StatusBadRequest, gin.H{"key": []string{"Not a valid string."}})
			return homePreferenceInput{}, false
		}
		result.key = strings.TrimSpace(result.key)
		if result.key == "" {
			c.JSON(http.StatusBadRequest, gin.H{"key": []string{"This field may not be blank."}})
			return homePreferenceInput{}, false
		}
		if utf8.RuneCountInString(result.key) > 255 {
			c.JSON(http.StatusBadRequest, gin.H{"key": []string{"Ensure this field has no more than 255 characters."}})
			return homePreferenceInput{}, false
		}
	}
	if raw, exists := fields["is_enabled"]; exists {
		result.hasIsEnabled = true
		if string(raw) == "null" {
			c.JSON(http.StatusBadRequest, gin.H{"is_enabled": []string{"This field may not be null."}})
			return homePreferenceInput{}, false
		}
		value, err := djangoBoolean(raw)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"is_enabled": []string{"Must be a valid boolean."}})
			return homePreferenceInput{}, false
		}
		result.isEnabled = value
	}
	if raw, exists := fields["sort_order"]; exists {
		result.hasSortOrder = true
		if string(raw) == "null" {
			c.JSON(http.StatusBadRequest, gin.H{"sort_order": []string{"This field may not be null."}})
			return homePreferenceInput{}, false
		}
		value, err := djangoFloat(raw)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"sort_order": []string{"A valid number is required."}})
			return homePreferenceInput{}, false
		}
		result.sortOrder = value
	}
	return result, true
}

// djangoBoolean mirrors the values DRF's BooleanField accepts.
func djangoBoolean(raw json.RawMessage) (bool, error) {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return false, err
	}
	switch typed := value.(type) {
	case bool:
		return typed, nil
	case float64:
		if typed == 1 {
			return true, nil
		}
		if typed == 0 {
			return false, nil
		}
	case string:
		switch typed {
		case "t", "T", "y", "Y", "yes", "Yes", "YES", "true", "True", "TRUE", "on", "On", "ON", "1":
			return true, nil
		case "f", "F", "n", "N", "no", "No", "NO", "false", "False", "FALSE", "off", "Off", "OFF", "0":
			return false, nil
		}
	}
	return false, errors.New("invalid boolean")
}

// djangoFloat mirrors DRF's FloatField, which runs Python's float() and so also
// accepts booleans and numeric strings.
func djangoFloat(raw json.RawMessage) (float64, error) {
	switch string(raw) {
	case "true":
		return 1, nil
	case "false":
		return 0, nil
	}
	return preferenceFloat(raw)
}

// homePreferenceJSON matches WorkspaceHomePreferenceSerializer, which omits the
// config the list route returns.
func homePreferenceJSON(preference WorkspaceHomePreference) gin.H {
	return gin.H{
		"key":        preference.Key,
		"is_enabled": preference.IsEnabled,
		"sort_order": preference.SortOrder,
	}
}
