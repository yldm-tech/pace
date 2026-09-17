package project

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api/internal/auth"
	"github.com/yldm-tech/pace/apps/api/internal/drf"
	"gorm.io/gorm"
)

func (handler *Handler) registerModuleCreateRoutes(router gin.IRouter) {
	router.POST("/api/workspaces/:slug/projects/:id/modules/", handler.authenticated(handler.moduleCreate))
}

// moduleCreate makes a module. The name has to be free within the project, and the serializer checks that itself rather than letting the unique index answer.
func (handler *Handler) moduleCreate(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember) {
		return
	}
	slug, projectID := c.Param("slug"), c.Param("id")
	var project Project
	err := handler.db.WithContext(c.Request.Context()).Table("projects p").
		Joins("JOIN workspaces w ON w.id = p.workspace_id").
		Where("p.id = ? AND w.slug = ?", projectID, slug).Take(&project).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "The required object does not exist."})
		return
	}
	if err != nil {
		handler.internalError(c, err)
		return
	}

	var body map[string]json.RawMessage
	if err := c.ShouldBindJSON(&body); err != nil {
		handler.invalidDetail(c)
		return
	}
	values, members, ok := handler.moduleFields(c, body, true)
	if !ok {
		return
	}

	// The name check counts every module of the project, archived ones included, since the unique index does not exclude them either.
	var clashes int64
	err = handler.db.WithContext(c.Request.Context()).Model(&Module{}).
		Where("name = ? AND project_id = ? AND deleted_at IS NULL", values["name"], projectID).
		Count(&clashes).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if clashes > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Module with this name already exists"})
		return
	}

	moduleID, err := newUUID()
	if err != nil {
		handler.internalError(c, err)
		return
	}
	now := handler.clock().UTC()
	values["id"] = moduleID
	values["created_at"] = now
	values["updated_at"] = now
	values["created_by_id"] = user.ID
	values["updated_by_id"] = user.ID
	values["project_id"] = projectID
	values["workspace_id"] = project.WorkspaceID

	err = handler.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		// The NOT NULL columns a request does not have to send. Django fills each from its field's default, and a map that does not name one leaves a null behind.
		for column, fallback := range map[string]any{
			"description": "", "status": "planned", "view_props": []byte("{}"),
			"sort_order": 65535.0, "logo_props": []byte("{}"),
		} {
			if _, given := values[column]; !given {
				values[column] = fallback
			}
		}
		if err := tx.Table("modules").Create(values).Error; err != nil {
			return err
		}
		rows := make([]ModuleMember, 0, len(members))
		for _, memberID := range members {
			rowID, err := newUUID()
			if err != nil {
				return err
			}
			rows = append(rows, ModuleMember{
				ID: rowID, CreatedAt: now, UpdatedAt: now, CreatedByID: &user.ID, UpdatedByID: &user.ID,
				ProjectID: projectID, WorkspaceID: project.WorkspaceID,
				ModuleID: moduleID, MemberID: memberID,
			})
		}
		if len(rows) == 0 {
			return nil
		}
		return tx.Create(&rows).Error
	})
	if err != nil {
		handler.internalError(c, err)
		return
	}

	if handler.tasks != nil {
		err := handler.tasks.PublishModelActivity(c.Request.Context(), "module", moduleID,
			decodeRawFields(body), nil, user.ID, slug, handler.origin())
		if err != nil {
			handler.internalError(c, err)
			return
		}
	}

	location, err := userLocation(user)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	rows, err := handler.moduleRows(c, user.ID, moduleID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if len(rows) == 0 {
		handler.internalError(c, errors.New("module create: the module could not be read back"))
		return
	}
	drf.Respond(c, http.StatusCreated, moduleListJSON(rows[0], location))
}

// moduleFields is ModuleWriteSerializer's writable set. member_ids is write-only, so it is taken out here and written to the join table rather than the module row.
func (handler *Handler) moduleFields(c *gin.Context, body map[string]json.RawMessage, creating bool) (map[string]any, []string, bool) {
	values := map[string]any{}

	if raw, present := body["name"]; present {
		value, ok := handler.stringField(c, "name", raw, 255)
		if !ok {
			return nil, nil, false
		}
		values["name"] = value
	} else if creating {
		c.JSON(http.StatusBadRequest, gin.H{"name": []string{"This field is required."}})
		return nil, nil, false
	}
	if raw, present := body["description"]; present {
		var value string
		if json.Unmarshal(raw, &value) != nil {
			c.JSON(http.StatusBadRequest, gin.H{"description": []string{"Not a valid string."}})
			return nil, nil, false
		}
		values["description"] = value
	}
	// Both description columns are JSON on a module, unlike an issue's html column, which is text.
	for _, field := range []string{"description_text", "description_html", "view_props", "logo_props"} {
		raw, present := body[field]
		if !present {
			continue
		}
		if !json.Valid(raw) {
			c.JSON(http.StatusBadRequest, gin.H{field: []string{"Value must be valid JSON."}})
			return nil, nil, false
		}
		values[field] = auth.JSONValue(raw)
	}
	if raw, present := body["status"]; present {
		var value string
		if json.Unmarshal(raw, &value) != nil || !moduleStatuses[value] {
			c.JSON(http.StatusBadRequest, gin.H{"status": []string{"\"" + string(raw) + "\" is not a valid choice."}})
			return nil, nil, false
		}
		values["status"] = value
	}
	for _, field := range []string{"start_date", "target_date"} {
		raw, present := body[field]
		if !present {
			continue
		}
		if string(raw) == "null" {
			values[field] = nil
			continue
		}
		var value string
		if json.Unmarshal(raw, &value) != nil {
			c.JSON(http.StatusBadRequest, gin.H{field: []string{"Date has wrong format."}})
			return nil, nil, false
		}
		parsed, ok := parseDjangoDateTime(value)
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{field: []string{"Date has wrong format."}})
			return nil, nil, false
		}
		values[field] = parsed.Format("2006-01-02")
	}
	// The pair is only compared when both are given, which is what the serializer's validate does.
	start, hasStart := values["start_date"].(string)
	target, hasTarget := values["target_date"].(string)
	if hasStart && hasTarget && start > target {
		c.JSON(http.StatusBadRequest, gin.H{"non_field_errors": []string{"Start date cannot exceed target date"}})
		return nil, nil, false
	}
	if raw, present := body["lead_id"]; present {
		if string(raw) == "null" {
			values["lead_id"] = nil
		} else {
			var value string
			if json.Unmarshal(raw, &value) != nil {
				c.JSON(http.StatusBadRequest, gin.H{"lead_id": []string{"Incorrect type."}})
				return nil, nil, false
			}
			values["lead_id"] = value
		}
	}
	if raw, present := body["sort_order"]; present {
		var value float64
		if json.Unmarshal(raw, &value) != nil {
			c.JSON(http.StatusBadRequest, gin.H{"sort_order": []string{"A valid number is required."}})
			return nil, nil, false
		}
		values["sort_order"] = value
	}
	for _, field := range []string{"external_source", "external_id"} {
		raw, present := body[field]
		if !present {
			continue
		}
		if string(raw) == "null" {
			values[field] = nil
			continue
		}
		var value string
		if json.Unmarshal(raw, &value) != nil {
			c.JSON(http.StatusBadRequest, gin.H{field: []string{"Not a valid string."}})
			return nil, nil, false
		}
		values[field] = value
	}

	members := []string{}
	if raw, present := body["member_ids"]; present && string(raw) != "null" {
		if json.Unmarshal(raw, &members) != nil {
			c.JSON(http.StatusBadRequest, gin.H{"member_ids": []string{"Expected a list of items."}})
			return nil, nil, false
		}
	}
	return values, members, true
}

// moduleStatuses is the choice list on the model.
var moduleStatuses = map[string]bool{
	"backlog": true, "planned": true, "in-progress": true,
	"paused": true, "completed": true, "cancelled": true,
}
