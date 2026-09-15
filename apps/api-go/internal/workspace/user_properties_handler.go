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
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	navigationAccordion = "ACCORDION"
	navigationTabbed    = "TABBED"
)

func (handler *Handler) userPropertiesGet(c *gin.Context, user *auth.User) {
	if !handler.requireWorkspaceViewer(c, user) {
		return
	}
	properties, err := handler.getOrCreateUserProperties(c.Request.Context(), c.Param("slug"), user.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			handler.notFound(c)
			return
		}
		handler.internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, userPropertiesJSON(properties))
}

func (handler *Handler) userPropertiesPatch(c *gin.Context, user *auth.User) {
	if !handler.requireWorkspaceViewer(c, user) {
		return
	}
	properties, err := handler.getOrCreateUserProperties(c.Request.Context(), c.Param("slug"), user.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			handler.notFound(c)
			return
		}
		handler.internalError(c, err)
		return
	}
	fields, ok := handler.userPropertiesFields(c)
	if !ok {
		return
	}
	if !handler.validateUserPropertiesReferences(c, fields) {
		return
	}
	updates := map[string]any{
		"updated_at":    handler.clock().UTC(),
		"updated_by_id": user.ID,
	}
	if fields.hasFilters {
		updates["filters"] = fields.filters
	}
	if fields.hasDisplayFilters {
		updates["display_filters"] = fields.displayFilters
	}
	if fields.hasDisplayProperties {
		updates["display_properties"] = fields.displayProperties
	}
	if fields.hasRichFilters {
		updates["rich_filters"] = fields.richFilters
	}
	if fields.hasNavigationProjectLimit {
		updates["navigation_project_limit"] = fields.navigationProjectLimit
	}
	if fields.hasNavigationPreference {
		updates["navigation_control_preference"] = fields.navigationPreference
	}
	if fields.hasDeletedAt {
		updates["deleted_at"] = fields.deletedAt
	}
	if fields.hasCreatedBy {
		updates["created_by_id"] = fields.createdByID
	}
	if fields.hasUpdatedBy {
		updates["updated_by_id"] = fields.updatedByID
	}
	if err := handler.db.WithContext(c.Request.Context()).Model(&WorkspaceUserProperties{}).Where("id = ?", properties.ID).Updates(updates).Error; err != nil {
		handler.internalError(c, err)
		return
	}
	if err := handler.db.WithContext(c.Request.Context()).Where("id = ?", properties.ID).Take(&properties).Error; err != nil {
		handler.internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, userPropertiesJSON(properties))
}

func (handler *Handler) requireWorkspaceViewer(c *gin.Context, user *auth.User) bool {
	if _, err := handler.workspaceRole(c.Request.Context(), c.Param("slug"), user.ID); err != nil {
		c.JSON(http.StatusForbidden, gin.H{"detail": "You do not have permission to perform this action."})
		return false
	}
	return true
}

func (handler *Handler) getOrCreateUserProperties(ctx context.Context, slug, userID string) (WorkspaceUserProperties, error) {
	var properties WorkspaceUserProperties
	query := handler.db.WithContext(ctx).Where(
		"workspace_id = (SELECT id FROM workspaces WHERE slug = ? AND deleted_at IS NULL) AND user_id = ? AND deleted_at IS NULL",
		slug, userID,
	)
	err := query.Take(&properties).Error
	if err == nil {
		return properties, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return WorkspaceUserProperties{}, err
	}
	var workspaceID string
	if err := handler.db.WithContext(ctx).Table("workspaces").Where("slug = ? AND deleted_at IS NULL", slug).Pluck("id", &workspaceID).Error; err != nil {
		return WorkspaceUserProperties{}, err
	}
	if workspaceID == "" {
		return WorkspaceUserProperties{}, gorm.ErrRecordNotFound
	}
	id, err := newUUID()
	if err != nil {
		return WorkspaceUserProperties{}, err
	}
	now := handler.clock().UTC()
	properties = WorkspaceUserProperties{
		ID: id, CreatedAt: now, UpdatedAt: now, CreatedByID: &userID,
		WorkspaceID: workspaceID, UserID: userID,
		Filters: defaultWorkspaceFiltersJSON(), DisplayFilters: defaultWorkspaceDisplayFiltersJSON(),
		DisplayProperties: defaultWorkspaceDisplayPropertiesJSON(), RichFilters: emptyJSON(),
		NavigationProjectLimit: 10, NavigationControlPreference: navigationAccordion,
	}
	result := handler.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&properties)
	if result.Error != nil {
		return WorkspaceUserProperties{}, result.Error
	}
	if result.RowsAffected == 0 {
		if err := query.Take(&properties).Error; err != nil {
			return WorkspaceUserProperties{}, err
		}
	}
	return properties, nil
}

type userPropertiesInput struct {
	filters                   auth.JSONValue
	displayFilters            auth.JSONValue
	displayProperties         auth.JSONValue
	richFilters               auth.JSONValue
	navigationProjectLimit    int
	navigationPreference      string
	deletedAt                 *time.Time
	createdByID               *string
	updatedByID               *string
	hasFilters                bool
	hasDisplayFilters         bool
	hasDisplayProperties      bool
	hasRichFilters            bool
	hasNavigationProjectLimit bool
	hasNavigationPreference   bool
	hasDeletedAt              bool
	hasCreatedBy              bool
	hasUpdatedBy              bool
}

func (handler *Handler) userPropertiesFields(c *gin.Context) (userPropertiesInput, bool) {
	var fields map[string]json.RawMessage
	if err := c.ShouldBindJSON(&fields); err != nil {
		handler.invalidDetail(c)
		return userPropertiesInput{}, false
	}
	result := userPropertiesInput{}
	for _, field := range []struct {
		name  string
		value *auth.JSONValue
		set   *bool
	}{
		{name: "filters", value: &result.filters, set: &result.hasFilters},
		{name: "display_filters", value: &result.displayFilters, set: &result.hasDisplayFilters},
		{name: "display_properties", value: &result.displayProperties, set: &result.hasDisplayProperties},
		{name: "rich_filters", value: &result.richFilters, set: &result.hasRichFilters},
	} {
		raw, exists := fields[field.name]
		if !exists {
			continue
		}
		*field.set = true
		if string(raw) == "null" {
			c.JSON(http.StatusBadRequest, gin.H{field.name: []string{"This field may not be null."}})
			return userPropertiesInput{}, false
		}
		*field.value = auth.JSONValue(append([]byte(nil), raw...))
	}
	if raw, exists := fields["navigation_project_limit"]; exists {
		result.hasNavigationProjectLimit = true
		value, err := djangoInteger(raw)
		if err != nil {
			if errors.Is(err, errNullInteger) {
				c.JSON(http.StatusBadRequest, gin.H{"navigation_project_limit": []string{"This field may not be null."}})
			} else {
				c.JSON(http.StatusBadRequest, gin.H{"navigation_project_limit": []string{"A valid integer is required."}})
			}
			return userPropertiesInput{}, false
		}
		if value > math.MaxInt32 {
			c.JSON(http.StatusBadRequest, gin.H{"navigation_project_limit": []string{"Ensure this value is less than or equal to 2147483647."}})
			return userPropertiesInput{}, false
		}
		if value < math.MinInt32 {
			c.JSON(http.StatusBadRequest, gin.H{"navigation_project_limit": []string{"Ensure this value is greater than or equal to -2147483648."}})
			return userPropertiesInput{}, false
		}
		result.navigationProjectLimit = int(value)
	}
	if raw, exists := fields["navigation_control_preference"]; exists {
		result.hasNavigationPreference = true
		if string(raw) == "null" {
			c.JSON(http.StatusBadRequest, gin.H{"navigation_control_preference": []string{"This field may not be null."}})
			return userPropertiesInput{}, false
		}
		if json.Unmarshal(raw, &result.navigationPreference) != nil || (result.navigationPreference != navigationAccordion && result.navigationPreference != navigationTabbed) {
			var value any
			_ = json.Unmarshal(raw, &value)
			c.JSON(http.StatusBadRequest, gin.H{"navigation_control_preference": []string{fmt.Sprintf("%q is not a valid choice.", fmt.Sprint(value))}})
			return userPropertiesInput{}, false
		}
	}
	if raw, exists := fields["deleted_at"]; exists {
		result.hasDeletedAt = true
		if string(raw) != "null" {
			var value string
			if json.Unmarshal(raw, &value) != nil {
				c.JSON(http.StatusBadRequest, gin.H{"deleted_at": []string{"Datetime has wrong format. Use one of these formats instead: YYYY-MM-DDThh:mm[:ss[.uuuuuu]][+HH:MM|-HH:MM|Z]."}})
				return userPropertiesInput{}, false
			}
			parsed, err := time.Parse(time.RFC3339Nano, value)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"deleted_at": []string{"Datetime has wrong format. Use one of these formats instead: YYYY-MM-DDThh:mm[:ss[.uuuuuu]][+HH:MM|-HH:MM|Z]."}})
				return userPropertiesInput{}, false
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
			return userPropertiesInput{}, false
		}
		canonical, valid := canonicalThemeUUID(value)
		if !valid {
			c.JSON(http.StatusBadRequest, gin.H{auditField.name: []string{fmt.Sprintf("“%s” is not a valid UUID.", value)}})
			return userPropertiesInput{}, false
		}
		*auditField.value = &canonical
	}
	return result, true
}

var errNullInteger = errors.New("integer may not be null")

func djangoInteger(raw json.RawMessage) (int64, error) {
	if string(raw) == "null" {
		return 0, errNullInteger
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return 0, err
	}
	text := fmt.Sprint(value)
	if number, ok := value.(json.Number); ok {
		float, err := strconv.ParseFloat(number.String(), 64)
		if err != nil || math.Trunc(float) != float {
			return 0, errors.New("invalid integer")
		}
		text = strconv.FormatFloat(float, 'f', -1, 64)
	} else if _, ok := value.(string); !ok {
		return 0, errors.New("invalid integer")
	}
	return strconv.ParseInt(text, 10, 64)
}

func (handler *Handler) validateUserPropertiesReferences(c *gin.Context, fields userPropertiesInput) bool {
	for _, reference := range []struct {
		field  string
		userID *string
	}{
		{field: "created_by", userID: fields.createdByID},
		{field: "updated_by", userID: fields.updatedByID},
	} {
		if reference.userID == nil {
			continue
		}
		var count int64
		if err := handler.db.WithContext(c.Request.Context()).Table("users").Where("id = ?", *reference.userID).Count(&count).Error; err != nil {
			handler.internalError(c, err)
			return false
		}
		if count == 0 {
			c.JSON(http.StatusBadRequest, gin.H{reference.field: []string{`Invalid pk "` + *reference.userID + `" - object does not exist.`}})
			return false
		}
	}
	return true
}

func userPropertiesJSON(properties WorkspaceUserProperties) gin.H {
	return gin.H{
		"id": properties.ID, "created_at": properties.CreatedAt, "updated_at": properties.UpdatedAt,
		"created_by": properties.CreatedByID, "updated_by": properties.UpdatedByID, "deleted_at": properties.DeletedAt,
		"workspace": properties.WorkspaceID, "user": properties.UserID,
		"filters": decodeJSON(properties.Filters), "display_filters": decodeJSON(properties.DisplayFilters),
		"display_properties": decodeJSON(properties.DisplayProperties), "rich_filters": decodeJSON(properties.RichFilters),
		"navigation_project_limit":      properties.NavigationProjectLimit,
		"navigation_control_preference": properties.NavigationControlPreference,
	}
}

func defaultWorkspaceFiltersJSON() auth.JSONValue {
	return auth.JSONValue([]byte(`{"priority":null,"state":null,"state_group":null,"assignees":null,"created_by":null,"labels":null,"start_date":null,"target_date":null,"subscriber":null}`))
}

func defaultWorkspaceDisplayFiltersJSON() auth.JSONValue {
	return auth.JSONValue([]byte(`{"display_filters":{"group_by":null,"order_by":"-created_at","type":null,"sub_issue":true,"show_empty_groups":true,"layout":"list","calendar_date_range":""}}`))
}

func defaultWorkspaceDisplayPropertiesJSON() auth.JSONValue {
	return auth.JSONValue([]byte(`{"display_properties":{"assignee":true,"attachment_count":true,"created_on":true,"due_date":true,"estimate":true,"key":true,"labels":true,"link":true,"priority":true,"start_date":true,"state":true,"sub_issue_count":true,"updated_on":true}}`))
}
