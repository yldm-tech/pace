package project

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"github.com/yldm-tech/pace/apps/api-go/internal/htmlsanitizer"
)

// defaultStates is DEFAULT_STATES from plane/db/models/state.py.
var defaultStates = []struct {
	Name     string
	Color    string
	Sequence float64
	Group    string
	Default  bool
}{
	{Name: "Backlog", Color: "#60646C", Sequence: 15000, Group: "backlog", Default: true},
	{Name: "Todo", Color: "#60646C", Sequence: 25000, Group: "unstarted"},
	{Name: "In Progress", Color: "#F59E0B", Sequence: 35000, Group: "started"},
	{Name: "Done", Color: "#46A758", Sequence: 45000, Group: "completed"},
	{Name: "Cancelled", Color: "#9AA4BC", Sequence: 55000, Group: "cancelled"},
	{Name: "Triage", Color: "#4E5355", Sequence: 65000, Group: "triage"},
}

type projectRelation struct {
	field string
	table string
	id    string
}

type projectInput struct {
	values      map[string]any
	relations   []projectRelation
	hasTimezone bool
}

func (input projectInput) apply(project *Project) {
	for name, value := range input.values {
		switch name {
		case "name":
			project.Name = value.(string)
		case "description":
			project.Description = value.(string)
		case "description_text":
			project.DescriptionText = value.(auth.JSONValue)
		case "description_html":
			project.DescriptionHTML = value.(auth.JSONValue)
		case "network":
			project.Network = value.(int)
		case "identifier":
			project.Identifier = value.(string)
		case "default_assignee":
			project.DefaultAssigneeID = value.(*string)
		case "project_lead":
			project.ProjectLeadID = value.(*string)
		case "emoji":
			project.Emoji = value.(*string)
		case "icon_prop":
			project.IconProp = value.(auth.JSONValue)
		case "module_view":
			project.ModuleView = value.(bool)
		case "cycle_view":
			project.CycleView = value.(bool)
		case "issue_views_view":
			project.IssueViewsView = value.(bool)
		case "page_view":
			project.PageView = value.(bool)
		case "intake_view":
			project.IntakeView = value.(bool)
		case "is_time_tracking_enabled":
			project.IsTimeTrackingEnabled = value.(bool)
		case "is_issue_type_enabled":
			project.IsIssueTypeEnabled = value.(bool)
		case "guest_view_all_features":
			project.GuestViewAllFeatures = value.(bool)
		case "cover_image":
			project.CoverImage = value.(*string)
		case "cover_image_asset":
			project.CoverImageAssetID = value.(*string)
		case "estimate":
			project.EstimateID = value.(*string)
		case "archive_in":
			project.ArchiveIn = value.(int)
		case "close_in":
			project.CloseIn = value.(int)
		case "logo_props":
			project.LogoProps = value.(auth.JSONValue)
		case "default_state":
			project.DefaultStateID = value.(*string)
		case "archived_at":
			project.ArchivedAt = value.(*time.Time)
		case "timezone":
			project.Timezone = value.(string)
		case "external_source":
			project.ExternalSource = value.(*string)
		case "external_id":
			project.ExternalID = value.(*string)
		}
	}
}

// projectFields is the format half of ProjectSerializer's validation. The
// checks that need the database (uniqueness and related-object existence) run
// in validateProjectReferences so this half stays independently testable.
func (handler *Handler) projectFields(c *gin.Context, body map[string]json.RawMessage, partial bool) (projectInput, bool) {
	result := projectInput{values: make(map[string]any)}

	if raw, exists := body["name"]; exists {
		value, ok := handler.stringField(c, "name", raw, 255)
		if !ok {
			return projectInput{}, false
		}
		if forbiddenIdentifierChars.MatchString(value) {
			c.JSON(http.StatusBadRequest, gin.H{"name": []string{"PROJECT_NAME_CANNOT_CONTAIN_SPECIAL_CHARACTERS"}})
			return projectInput{}, false
		}
		result.values["name"] = value
	} else if !partial {
		c.JSON(http.StatusBadRequest, gin.H{"name": []string{"This field is required."}})
		return projectInput{}, false
	}

	if raw, exists := body["identifier"]; exists {
		value, ok := handler.stringField(c, "identifier", raw, 12)
		if !ok {
			return projectInput{}, false
		}
		if forbiddenIdentifierChars.MatchString(value) {
			c.JSON(http.StatusBadRequest, gin.H{"identifier": []string{"PROJECT_IDENTIFIER_CANNOT_CONTAIN_SPECIAL_CHARACTERS"}})
			return projectInput{}, false
		}
		result.values["identifier"] = value
	} else if !partial {
		c.JSON(http.StatusBadRequest, gin.H{"identifier": []string{"This field is required."}})
		return projectInput{}, false
	}

	if raw, exists := body["description"]; exists {
		value, ok := handler.stringField(c, "description", raw, 0)
		if !ok {
			return projectInput{}, false
		}
		result.values["description"] = value
	}

	for _, name := range []string{"description_text", "description_html", "icon_prop", "logo_props"} {
		raw, exists := body[name]
		if !exists {
			continue
		}
		if string(raw) == "null" {
			if name == "logo_props" {
				c.JSON(http.StatusBadRequest, gin.H{name: []string{"This field may not be null."}})
				return projectInput{}, false
			}
			result.values[name] = auth.JSONValue(nil)
			continue
		}
		if !json.Valid(raw) {
			c.JSON(http.StatusBadRequest, gin.H{name: []string{"Value must be valid JSON."}})
			return projectInput{}, false
		}
		result.values[name] = auth.JSONValue(append([]byte(nil), raw...))
	}
	// ProjectSerializer.validate sanitizes description_html with nh3 before it
	// is stored; Django stringifies the JSON value first.
	if value, exists := result.values["description_html"]; exists {
		stored, ok := sanitizeDescriptionHTML(c, value.(auth.JSONValue))
		if !ok {
			return projectInput{}, false
		}
		result.values["description_html"] = stored
	}

	for _, flag := range []string{
		"module_view", "cycle_view", "issue_views_view", "page_view", "intake_view",
		"is_time_tracking_enabled", "is_issue_type_enabled", "guest_view_all_features",
	} {
		raw, exists := body[flag]
		if !exists {
			continue
		}
		value, ok := handler.booleanField(c, flag, raw)
		if !ok {
			return projectInput{}, false
		}
		result.values[flag] = value
	}

	if raw, exists := body["network"]; exists {
		value, ok := handler.integerField(c, "network", raw)
		if !ok {
			return projectInput{}, false
		}
		if value != 0 && value != 2 {
			c.JSON(http.StatusBadRequest, gin.H{"network": []string{fmt.Sprintf(`"%d" is not a valid choice.`, value)}})
			return projectInput{}, false
		}
		result.values["network"] = value
	}

	for _, bounded := range []string{"archive_in", "close_in"} {
		raw, exists := body[bounded]
		if !exists {
			continue
		}
		value, ok := handler.integerField(c, bounded, raw)
		if !ok {
			return projectInput{}, false
		}
		if value < 0 {
			c.JSON(http.StatusBadRequest, gin.H{bounded: []string{"Ensure this value is greater than or equal to 0."}})
			return projectInput{}, false
		}
		if value > 12 {
			c.JSON(http.StatusBadRequest, gin.H{bounded: []string{"Ensure this value is less than or equal to 12."}})
			return projectInput{}, false
		}
		result.values[bounded] = value
	}

	if raw, exists := body["timezone"]; exists {
		result.hasTimezone = string(raw) != "null"
		value, ok := handler.stringField(c, "timezone", raw, 255)
		if !ok {
			return projectInput{}, false
		}
		if _, err := time.LoadLocation(value); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"timezone": []string{fmt.Sprintf(`"%s" is not a valid choice.`, value)}})
			return projectInput{}, false
		}
		result.values["timezone"] = value
	}

	for _, optional := range []struct {
		name   string
		length int
	}{
		{name: "emoji", length: 255}, {name: "cover_image", length: 0},
		{name: "external_source", length: 255}, {name: "external_id", length: 255},
	} {
		raw, exists := body[optional.name]
		if !exists {
			continue
		}
		if string(raw) == "null" {
			result.values[optional.name] = (*string)(nil)
			continue
		}
		value, ok := handler.stringField(c, optional.name, raw, optional.length)
		if !ok {
			return projectInput{}, false
		}
		stored := value
		result.values[optional.name] = &stored
	}

	for _, relation := range []struct {
		field string
		table string
	}{
		{field: "default_assignee", table: "users"},
		{field: "project_lead", table: "users"},
		{field: "cover_image_asset", table: "file_assets"},
		{field: "estimate", table: "estimates"},
		{field: "default_state", table: "states"},
	} {
		raw, exists := body[relation.field]
		if !exists {
			continue
		}
		if string(raw) == "null" {
			result.values[relation.field] = (*string)(nil)
			continue
		}
		var value string
		if json.Unmarshal(raw, &value) != nil {
			c.JSON(http.StatusBadRequest, gin.H{relation.field: []string{fmt.Sprintf("“%s” is not a valid UUID.", value)}})
			return projectInput{}, false
		}
		canonical, valid := canonicalUUID(value)
		if !valid {
			c.JSON(http.StatusBadRequest, gin.H{relation.field: []string{fmt.Sprintf("“%s” is not a valid UUID.", value)}})
			return projectInput{}, false
		}
		stored := canonical
		result.values[relation.field] = &stored
		result.relations = append(result.relations, projectRelation{field: relation.field, table: relation.table, id: canonical})
	}

	if raw, exists := body["archived_at"]; exists {
		if string(raw) == "null" {
			result.values["archived_at"] = (*time.Time)(nil)
		} else {
			var value string
			parsed, err := time.Time{}, error(nil)
			if json.Unmarshal(raw, &value) == nil {
				parsed, err = time.Parse(time.RFC3339Nano, value)
			} else {
				err = fmt.Errorf("invalid datetime")
			}
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"archived_at": []string{"Datetime has wrong format. Use one of these formats instead: YYYY-MM-DDThh:mm[:ss[.uuuuuu]][+HH:MM|-HH:MM|Z]."}})
				return projectInput{}, false
			}
			result.values["archived_at"] = &parsed
		}
	}

	return result, true
}

// validateProjectReferences runs the checks ProjectSerializer performs against
// the database: validate_name and validate_identifier uniqueness within the
// workspace, and the related objects the writable relations point at.
func (handler *Handler) validateProjectReferences(c *gin.Context, input projectInput, workspaceID, excludeProjectID string) bool {
	for _, unique := range []struct {
		field   string
		message string
	}{
		{field: "name", message: "PROJECT_NAME_ALREADY_EXIST"},
		{field: "identifier", message: "PROJECT_IDENTIFIER_ALREADY_EXIST"},
	} {
		value, exists := input.values[unique.field]
		if !exists {
			continue
		}
		text := value.(string)
		if unique.field == "identifier" {
			// Project.save uppercases the identifier before it is stored.
			text = strings.ToUpper(strings.TrimSpace(text))
		}
		taken, err := handler.projectFieldTaken(c.Request.Context(), unique.field, text, workspaceID, excludeProjectID)
		if err != nil {
			handler.internalError(c, err)
			return false
		}
		if taken {
			c.JSON(http.StatusBadRequest, gin.H{unique.field: []string{unique.message}})
			return false
		}
	}
	for _, relation := range input.relations {
		exists, err := handler.rowExists(c.Request.Context(), relation.table, relation.id)
		if err != nil {
			handler.internalError(c, err)
			return false
		}
		if !exists {
			c.JSON(http.StatusBadRequest, gin.H{relation.field: []string{`Invalid pk "` + relation.id + `" - object does not exist.`}})
			return false
		}
	}
	return true
}

// sanitizeDescriptionHTML mirrors ProjectSerializer.validate, which runs str()
// over the JSON value before handing it to nh3 and stores the cleaned string.
func sanitizeDescriptionHTML(c *gin.Context, value auth.JSONValue) (auth.JSONValue, bool) {
	var decoded any
	if json.Unmarshal(value, &decoded) != nil {
		return value, true
	}
	text, isString := decoded.(string)
	if !isString || text == "" {
		return value, true
	}
	valid, _, cleaned := htmlsanitizer.ValidateHTMLContent(text)
	if cleaned != nil {
		// Encode without Go's HTML escaping so the stored text reads the way
		// Django stores it; jsonb would normalize either form to the same value.
		var buffer bytes.Buffer
		encoder := json.NewEncoder(&buffer)
		encoder.SetEscapeHTML(false)
		if encoder.Encode(*cleaned) == nil {
			value = auth.JSONValue(bytes.TrimRight(buffer.Bytes(), "\n"))
		}
	}
	if !valid {
		c.JSON(http.StatusBadRequest, gin.H{"error": "html content is not valid"})
		return nil, false
	}
	return value, true
}

func (handler *Handler) stringField(c *gin.Context, name string, raw json.RawMessage, maxLength int) (string, bool) {
	if string(raw) == "null" {
		c.JSON(http.StatusBadRequest, gin.H{name: []string{"This field may not be null."}})
		return "", false
	}
	var value string
	if json.Unmarshal(raw, &value) != nil {
		c.JSON(http.StatusBadRequest, gin.H{name: []string{"Not a valid string."}})
		return "", false
	}
	value = strings.TrimSpace(value)
	if value == "" {
		c.JSON(http.StatusBadRequest, gin.H{name: []string{"This field may not be blank."}})
		return "", false
	}
	if maxLength > 0 && utf8.RuneCountInString(value) > maxLength {
		c.JSON(http.StatusBadRequest, gin.H{name: []string{fmt.Sprintf("Ensure this field has no more than %d characters.", maxLength)}})
		return "", false
	}
	return value, true
}

func (handler *Handler) booleanField(c *gin.Context, name string, raw json.RawMessage) (bool, bool) {
	if string(raw) == "null" {
		c.JSON(http.StatusBadRequest, gin.H{name: []string{"This field may not be null."}})
		return false, false
	}
	var decoded any
	if json.Unmarshal(raw, &decoded) == nil {
		switch typed := decoded.(type) {
		case bool:
			return typed, true
		case float64:
			if typed == 1 {
				return true, true
			}
			if typed == 0 {
				return false, true
			}
		case string:
			switch typed {
			case "t", "T", "y", "Y", "yes", "Yes", "YES", "true", "True", "TRUE", "on", "On", "ON", "1":
				return true, true
			case "f", "F", "n", "N", "no", "No", "NO", "false", "False", "FALSE", "off", "Off", "OFF", "0":
				return false, true
			}
		}
	}
	c.JSON(http.StatusBadRequest, gin.H{name: []string{"Must be a valid boolean."}})
	return false, false
}

func (handler *Handler) integerField(c *gin.Context, name string, raw json.RawMessage) (int, bool) {
	if string(raw) == "null" {
		c.JSON(http.StatusBadRequest, gin.H{name: []string{"This field may not be null."}})
		return 0, false
	}
	var decoded any
	if json.Unmarshal(raw, &decoded) == nil {
		switch typed := decoded.(type) {
		case float64:
			if math.Trunc(typed) == typed && typed <= math.MaxInt32 && typed >= math.MinInt32 {
				return int(typed), true
			}
		case string:
			var parsed int
			if _, err := fmt.Sscanf(typed, "%d", &parsed); err == nil {
				return parsed, true
			}
		}
	}
	c.JSON(http.StatusBadRequest, gin.H{name: []string{"A valid integer is required."}})
	return 0, false
}

func (handler *Handler) projectFieldTaken(ctx context.Context, column, value, workspaceID, excludeID string) (bool, error) {
	query := handler.db.WithContext(ctx).Model(&Project{}).
		Where(column+" = ? AND workspace_id = ? AND deleted_at IS NULL", value, workspaceID)
	if excludeID != "" {
		query = query.Where("id <> ?", excludeID)
	}
	var count int64
	err := query.Count(&count).Error
	return count > 0, err
}

func (handler *Handler) rowExists(ctx context.Context, table, id string) (bool, error) {
	var count int64
	query := handler.db.WithContext(ctx).Table(table).Where("id = ?", id)
	if table != "users" {
		query = query.Where("deleted_at IS NULL")
	}
	err := query.Count(&count).Error
	return count > 0, err
}

func canonicalUUID(value string) (string, bool) {
	compact := strings.TrimSpace(strings.ToLower(value))
	compact = strings.TrimPrefix(compact, "urn:uuid:")
	if strings.HasPrefix(compact, "{") && strings.HasSuffix(compact, "}") {
		compact = strings.TrimSuffix(strings.TrimPrefix(compact, "{"), "}")
	}
	compact = strings.ReplaceAll(compact, "-", "")
	if len(compact) != 32 {
		return "", false
	}
	for _, character := range compact {
		if !strings.ContainsRune("0123456789abcdef", character) {
			return "", false
		}
	}
	return compact[0:8] + "-" + compact[8:12] + "-" + compact[12:16] + "-" + compact[16:20] + "-" + compact[20:32], true
}

func newUUID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", value[0:4], value[4:6], value[6:8], value[8:10], value[10:16]), nil
}

func emptyJSON() auth.JSONValue { return auth.JSONValue([]byte(`{}`)) }

func defaultPropsJSON() auth.JSONValue {
	return auth.JSONValue([]byte(`{"filters":{"priority":null,"state":null,"state_group":null,"assignees":null,"created_by":null,"labels":null,"start_date":null,"target_date":null,"subscriber":null},"display_filters":{"group_by":null,"order_by":"-created_at","type":null,"sub_issue":true,"show_empty_groups":true,"layout":"list","calendar_date_range":""}}`))
}

func defaultPreferencesJSON() auth.JSONValue {
	return auth.JSONValue([]byte(`{"pages":{"block_display":true},"navigation":{"default_tab":"work_items","hide_in_more_menu":[]}}`))
}

func defaultFiltersJSON() auth.JSONValue {
	return auth.JSONValue([]byte(`{"priority":null,"state":null,"state_group":null,"assignees":null,"created_by":null,"labels":null,"start_date":null,"target_date":null,"subscriber":null}`))
}

func defaultDisplayFiltersJSON() auth.JSONValue {
	return auth.JSONValue([]byte(`{"group_by":null,"order_by":"-created_at","type":null,"sub_issue":true,"show_empty_groups":true,"layout":"list","calendar_date_range":""}`))
}

func defaultDisplayPropertiesJSON() auth.JSONValue {
	return auth.JSONValue([]byte(`{"assignee":true,"attachment_count":true,"created_on":true,"due_date":true,"estimate":true,"key":true,"labels":true,"link":true,"priority":true,"start_date":true,"state":true,"sub_issue_count":true,"updated_on":true}`))
}
