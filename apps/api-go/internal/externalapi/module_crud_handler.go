package externalapi

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"github.com/yldm-tech/pace/apps/api-go/internal/drf"
	"gorm.io/gorm"
)

func (handler *Handler) registerModuleCrudRoutes(router gin.IRouter) {
	const base = "/api/v1/workspaces/:slug/projects/:project/modules/"
	router.GET(base, handler.authenticated(handler.moduleList))
	router.POST(base, handler.authenticated(handler.moduleCreate))
	router.GET(base+":module/", handler.authenticated(handler.moduleRetrieve))
	router.PATCH(base+":module/", handler.authenticated(handler.moduleUpdate))
	router.DELETE(base+":module/", handler.authenticated(handler.moduleDestroy))
}

// moduleList returns a project's live modules.
//
// Unlike the cycle list this queryset does not ask that the caller be a member of the project — the permission class has already asked, and the queryset adds nothing on top.
func (handler *Handler) moduleList(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireProjectMember(c, user, c.Request.Method) {
		return
	}
	rows, err := handler.moduleRows(c, "m.archived_at IS NULL")
	if err != nil {
		handler.serverError(c, err)
		return
	}
	fields := requestedFields(c)
	results := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		results = append(results, narrow(moduleJSON(row, true), fields))
	}
	handler.respondPaged(c, results)
}

// moduleRetrieve returns one live module.
func (handler *Handler) moduleRetrieve(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireProjectMember(c, user, c.Request.Method) {
		return
	}
	rows, err := handler.moduleRows(c, "m.archived_at IS NULL AND m.id = ?", c.Param("module"))
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if len(rows) == 0 {
		notFound(c)
		return
	}
	drf.Respond(c, http.StatusOK, narrow(moduleJSON(rows[0], true), requestedFields(c)))
}

// moduleRows reads the annotated modules the list and the detail share.
func (handler *Handler) moduleRows(c *gin.Context, condition string, arguments ...any) ([]moduleRow, error) {
	var rows []moduleRow
	err := handler.db.WithContext(c.Request.Context()).Table("modules m").
		Select(externalModuleAnnotations()).
		Joins("JOIN workspaces w ON w.id = m.workspace_id").
		Where("w.slug = ? AND m.project_id = ? AND m.deleted_at IS NULL", c.Param("slug"), c.Param("project")).
		Where(condition, arguments...).
		Order("m.created_at DESC").Scan(&rows).Error
	return rows, err
}

// moduleCreate makes a module.
//
// A name that is taken is refused with a body of its own — four keys, flat rather than the lists a field error carries, because the serializer raises it from create() where nothing wraps it.
func (handler *Handler) moduleCreate(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireProjectMember(c, user, c.Request.Method) {
		return
	}
	project, found, err := handler.projectByID(c)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !found {
		notFound(c)
		return
	}
	var body map[string]json.RawMessage
	if err := c.ShouldBindJSON(&body); err != nil {
		invalidPayload(c)
		return
	}
	input, failures := handler.validateExternalModule(c, body, project)
	if failures != nil {
		c.JSON(http.StatusBadRequest, failures)
		return
	}

	slug, projectID := c.Param("slug"), c.Param("project")
	externalID, externalSource := payloadString(body, "external_id"), payloadString(body, "external_source")
	if externalID != "" && externalSource != "" {
		existing, err := handler.moduleByExternalPair(c, projectID, externalSource, externalID)
		if err != nil {
			handler.serverError(c, err)
			return
		}
		if existing != "" {
			c.JSON(http.StatusConflict, gin.H{
				"error": "Module with the same external id and external source already exists",
				"id":    existing,
			})
			return
		}
	}
	if name, named := input.values["name"].(string); named {
		taken, err := handler.moduleByName(c, projectID, name, "")
		if err != nil {
			handler.serverError(c, err)
			return
		}
		if taken != "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"id":      taken,
				"code":    "MODULE_NAME_ALREADY_EXISTS",
				"error":   "Module with this name already exists",
				"message": "Module with this name already exists",
			})
			return
		}
	}

	now := handler.clock().UTC()
	moduleID, err := newUUID()
	if err != nil {
		handler.serverError(c, err)
		return
	}
	values := map[string]any{"id": moduleID}
	for key, value := range input.values {
		values[key] = value
	}
	values["created_at"] = now
	values["updated_at"] = now
	values["created_by_id"] = user.ID
	values["updated_by_id"] = user.ID
	values["project_id"] = projectID
	values["workspace_id"] = project.WorkspaceID
	err = handler.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		// Like a cycle, a new module is placed **ahead** of the ones already there.
		var smallest *float64
		err := tx.Table("modules").Where("project_id = ? AND deleted_at IS NULL", projectID).
			Select("MIN(sort_order)").Scan(&smallest).Error
		if err != nil {
			return err
		}
		values["sort_order"] = 65535.0
		if smallest != nil {
			values["sort_order"] = *smallest - 10000
		}
		if err := tx.Table("modules").Create(values).Error; err != nil {
			return err
		}
		return handler.writeModuleMembers(tx, moduleID, projectID, project.WorkspaceID, user.ID, input.members, now)
	})
	if err != nil {
		handler.serverError(c, err)
		return
	}

	if handler.tasks != nil {
		err := handler.tasks.PublishModelActivity(c.Request.Context(), "module", moduleID,
			decodeRawBody(body), nil, user.ID, slug, handler.origin(c))
		if err != nil {
			handler.serverError(c, err)
			return
		}
	}
	rows, err := handler.moduleRows(c, "m.id = ?", moduleID)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if len(rows) == 0 {
		handler.serverError(c, errModuleVanished)
		return
	}
	// The create reads the module back through the plain manager, so the six counts are absent from its body — twenty-three fields rather than twenty-nine.
	drf.Respond(c, http.StatusCreated, moduleJSON(rows[0], false))
}

var errModuleVanished = &orderError{"the module could not be read back after it was written"}

// moduleUpdate edits a module. An archived one is refused outright.
func (handler *Handler) moduleUpdate(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireProjectMember(c, user, c.Request.Method) {
		return
	}
	current, found, err := handler.externalModuleByIdentifier(c, c.Param("module"))
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !found {
		notFound(c)
		return
	}
	if current.ArchivedAt != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Archived module cannot be edited"})
		return
	}
	project, found, err := handler.projectByID(c)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !found {
		notFound(c)
		return
	}
	var body map[string]json.RawMessage
	if err := c.ShouldBindJSON(&body); err != nil {
		invalidPayload(c)
		return
	}
	input, failures := handler.validateExternalModule(c, body, project)
	if failures != nil {
		c.JSON(http.StatusBadRequest, failures)
		return
	}

	slug, projectID := c.Param("slug"), c.Param("project")
	if externalID := payloadString(body, "external_id"); externalID != "" && stringOrNil(current.ExternalID) != externalID {
		source := stringOrNil(current.ExternalSource)
		if given := payloadString(body, "external_source"); given != "" {
			source = given
		}
		existing, err := handler.moduleByExternalPair(c, projectID, source, externalID)
		if err != nil {
			handler.serverError(c, err)
			return
		}
		if existing != "" {
			c.JSON(http.StatusConflict, gin.H{
				"error": "Module with the same external id and external source already exists",
				"id":    current.ID,
			})
			return
		}
	}
	if name, named := input.values["name"].(string); named {
		taken, err := handler.moduleByName(c, projectID, name, current.ID)
		if err != nil {
			handler.serverError(c, err)
			return
		}
		if taken != "" {
			// The update's version of the same refusal carries one key rather than four.
			c.JSON(http.StatusBadRequest, gin.H{"error": "Module with this name already exists"})
			return
		}
	}

	snapshot, err := handler.moduleSnapshot(c, current.ID)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	now := handler.clock().UTC()
	updates := map[string]any{"updated_at": now, "updated_by_id": user.ID}
	for key, value := range input.values {
		updates[key] = value
	}
	err = handler.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Table("modules").Where("id = ?", current.ID).Updates(updates).Error; err != nil {
			return err
		}
		if !input.hasMembers {
			return nil
		}
		// The member list is replaced rather than merged, and the old rows go for good.
		if err := tx.Exec("DELETE FROM module_members WHERE module_id = ?", current.ID).Error; err != nil {
			return err
		}
		return handler.writeModuleMembers(tx, current.ID, projectID, current.WorkspaceID, user.ID, input.members, now)
	})
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if handler.tasks != nil {
		err := handler.tasks.PublishModelActivity(c.Request.Context(), "module", current.ID,
			decodeRawBody(body), &snapshot, user.ID, slug, handler.origin(c))
		if err != nil {
			handler.serverError(c, err)
			return
		}
	}
	rows, err := handler.moduleRows(c, "m.id = ?", current.ID)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if len(rows) == 0 {
		handler.serverError(c, errModuleVanished)
		return
	}
	// The update answers through the same serializer the create does, which reads a plain instance rather than an annotated one.
	drf.Respond(c, http.StatusOK, moduleJSON(rows[0], false))
}

// moduleDestroy removes a module. Only an admin or the person who made it may.
//
// The activity is queued **before** the module is gone, so it can still name the work items that were in it, and the links and the favourites are removed rather than soft deleted.
func (handler *Handler) moduleDestroy(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireProjectMember(c, user, c.Request.Method) {
		return
	}
	current, found, err := handler.externalModuleByIdentifier(c, c.Param("module"))
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !found {
		notFound(c)
		return
	}
	if stringOrNil(current.CreatedByID) != user.ID {
		role, member, err := handler.projectRole(c, user)
		if err != nil {
			handler.serverError(c, err)
			return
		}
		if !member || role != roleAdmin {
			c.JSON(http.StatusForbidden, gin.H{"error": "Only admin or creator can delete the module"})
			return
		}
	}

	var issueIDs []string
	err = handler.db.WithContext(c.Request.Context()).Table("module_issues").
		Where("module_id = ? AND deleted_at IS NULL", current.ID).Pluck("issue_id", &issueIDs).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if issueIDs == nil {
		issueIDs = []string{}
	}
	now := handler.clock().UTC()
	if handler.tasks != nil {
		requested, err := json.Marshal(map[string]any{
			"module_id": current.ID, "module_name": current.Name, "issues": issueIDs,
		})
		if err != nil {
			handler.serverError(c, err)
			return
		}
		snapshot, err := json.Marshal(map[string]any{"module_name": current.Name})
		if err != nil {
			handler.serverError(c, err)
			return
		}
		// This one carries an origin and no notification, where the cycle's delete carries neither.
		err = handler.tasks.PublishIssueActivity(c.Request.Context(), map[string]any{
			"type": "module.activity.deleted", "requested_data": string(requested),
			"actor_id": user.ID, "issue_id": nil, "project_id": c.Param("project"),
			"current_instance": string(snapshot), "epoch": now.Unix(), "origin": handler.origin(c),
		})
		if err != nil {
			handler.serverError(c, err)
			return
		}
	}
	err = handler.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		err := tx.Table("modules").Where("id = ?", current.ID).
			Updates(map[string]any{"deleted_at": now, "updated_at": now}).Error
		if err != nil {
			return err
		}
		// Both of these are soft deletes through the queryset rather than hard ones.
		err = tx.Table("module_issues").
			Where("module_id = ? AND project_id = ? AND deleted_at IS NULL", current.ID, c.Param("project")).
			Update("deleted_at", now).Error
		if err != nil {
			return err
		}
		return tx.Table("user_favorites").
			Where(`entity_type = 'module' AND entity_identifier = ? AND project_id = ? AND deleted_at IS NULL`,
				current.ID, c.Param("project")).
			Update("deleted_at", now).Error
	})
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if handler.tasks != nil {
		if err := handler.tasks.PublishSoftDeleteRelatedObjects(c.Request.Context(), "db", "module", current.ID); err != nil {
			handler.serverError(c, err)
			return
		}
	}
	c.Status(http.StatusNoContent)
}

// externalModuleInput is what the module serializer makes of a payload.
type externalModuleInput struct {
	values     map[string]any
	members    []string
	hasMembers bool
}

// validateExternalModule is ModuleCreateSerializer, which the update inherits whole.
//
// The member list is **narrowed** rather than refused: an id that is not a member of the project is dropped, and unlike the work item's assignees nobody is told.
func (handler *Handler) validateExternalModule(c *gin.Context, body map[string]json.RawMessage, project Project) (externalModuleInput, gin.H) {
	input := externalModuleInput{values: map[string]any{}}
	errors := gin.H{}

	for _, field := range []string{"name", "description"} {
		raw, given := body[field]
		if !given {
			continue
		}
		value, failure := charField(raw, moduleFieldLimit(field))
		if failure != nil {
			errors[field] = failure
			continue
		}
		if field == "name" && *value == "" {
			errors["name"] = []string{"This field may not be blank."}
			continue
		}
		input.values[field] = *value
	}
	for _, field := range []string{"start_date", "target_date"} {
		raw, given := body[field]
		if !given {
			continue
		}
		value, failure := dateField(raw)
		if failure != nil {
			errors[field] = failure
			continue
		}
		if value == nil {
			input.values[field] = nil
			continue
		}
		input.values[field] = *value
	}
	if raw, given := body["status"]; given {
		if value, failure := choiceField(raw, moduleStatusChoices); failure != nil {
			errors["status"] = failure
		} else {
			input.values["status"] = *value
		}
	}
	for _, field := range []string{"external_source", "external_id"} {
		raw, given := body[field]
		if !given {
			continue
		}
		if string(raw) == "null" {
			input.values[field] = nil
			continue
		}
		value, failure := charField(raw, 255)
		if failure != nil {
			errors[field] = failure
			continue
		}
		input.values[field] = *value
	}
	if raw, given := body["lead"]; given {
		value, failure := primaryKeyField(raw)
		switch {
		case failure != nil:
			errors["lead"] = failure
		case value == nil:
			input.values["lead_id"] = nil
		default:
			exists, err := handler.rowExists(c, "users", "id = ?", *value)
			if err != nil {
				handler.serverError(c, err)
				return externalModuleInput{}, gin.H{}
			}
			if !exists {
				errors["lead"] = []string{"Invalid pk \"" + *value + "\" - object does not exist."}
			} else {
				input.values["lead_id"] = *value
			}
		}
	}
	if raw, given := body["members"]; given {
		values, failure := primaryKeyList(raw)
		if failure != nil {
			errors["members"] = failure
		} else {
			input.members = values
			input.hasMembers = true
		}
	}
	if len(errors) > 0 {
		return externalModuleInput{}, errors
	}

	if !project.ModuleView {
		return externalModuleInput{}, gin.H{"non_field_errors": []string{"Modules are not enabled for this project"}}
	}
	start, hasStart := input.values["start_date"].(time.Time)
	target, hasTarget := input.values["target_date"].(time.Time)
	if hasStart && hasTarget && start.After(target) {
		return externalModuleInput{}, gin.H{"non_field_errors": []string{"Start date cannot exceed target date"}}
	}
	if input.hasMembers && len(input.members) > 0 {
		// The narrowing asks only for a membership row, not for an active one, so somebody removed from the project can still be put on a module.
		var allowed []string
		err := handler.db.WithContext(c.Request.Context()).Table("project_members").
			Where("project_id = ? AND member_id IN ?", c.Param("project"), input.members).
			Pluck("member_id", &allowed).Error
		if err != nil {
			handler.serverError(c, err)
			return externalModuleInput{}, gin.H{}
		}
		input.members = allowed
	}
	return input, nil
}

// moduleStatusChoices is the status list the model declares.
var moduleStatusChoices = []string{"backlog", "planned", "in-progress", "paused", "completed", "cancelled"}

func moduleFieldLimit(field string) int {
	if field == "name" {
		return 255
	}
	return 0
}

func (handler *Handler) writeModuleMembers(tx *gorm.DB, moduleID, projectID, workspaceID, actorID string, members []string, now time.Time) error {
	if len(members) == 0 {
		return nil
	}
	rows := make([]map[string]any, 0, len(members))
	for _, member := range members {
		identifier, err := newUUID()
		if err != nil {
			return err
		}
		rows = append(rows, map[string]any{
			"id": identifier, "created_at": now, "updated_at": now,
			"created_by_id": actorID, "updated_by_id": actorID,
			"project_id": projectID, "workspace_id": workspaceID,
			"module_id": moduleID, "member_id": member,
		})
	}
	return tx.Table("module_members").Clauses(onConflictDoNothing()).Create(rows).Error
}

func (handler *Handler) moduleByExternalPair(c *gin.Context, projectID, source, identifier string) (string, error) {
	var found []string
	err := handler.db.WithContext(c.Request.Context()).Table("modules m").
		Joins("JOIN workspaces w ON w.id = m.workspace_id").
		Where("w.slug = ? AND m.project_id = ? AND m.external_source = ? AND m.external_id = ? AND m.deleted_at IS NULL",
			c.Param("slug"), projectID, source, identifier).
		Order("m.created_at").Limit(1).Pluck("m.id", &found).Error
	if err != nil || len(found) == 0 {
		return "", err
	}
	return found[0], nil
}

// moduleByName answers with the id of a module already carrying a name, ignoring the one being edited.
func (handler *Handler) moduleByName(c *gin.Context, projectID, name, ignoring string) (string, error) {
	query := handler.db.WithContext(c.Request.Context()).Table("modules").
		Where("name = ? AND project_id = ? AND deleted_at IS NULL", name, projectID)
	if ignoring != "" {
		query = query.Where("id <> ?", ignoring)
	}
	var found []string
	if err := query.Order("created_at").Limit(1).Pluck("id", &found).Error; err != nil || len(found) == 0 {
		return "", err
	}
	return found[0], nil
}

// externalModuleByIdentifier reads one module through the plain manager, which is what the write routes act on.
func (handler *Handler) externalModuleByIdentifier(c *gin.Context, identifier string) (Module, bool, error) {
	var modules []Module
	err := handler.db.WithContext(c.Request.Context()).Table("modules m").Select("m.*").
		Joins("JOIN workspaces w ON w.id = m.workspace_id").
		Where("w.slug = ? AND m.project_id = ? AND m.id = ? AND m.deleted_at IS NULL",
			c.Param("slug"), c.Param("project"), identifier).
		Limit(1).Scan(&modules).Error
	if err != nil || len(modules) == 0 {
		return Module{}, false, err
	}
	return modules[0], true, nil
}

// moduleSnapshot is the annotated serializer's view of a module before it changes, which the activity carries.
func (handler *Handler) moduleSnapshot(c *gin.Context, moduleID string) (string, error) {
	rows, err := handler.moduleRows(c, "m.id = ?", moduleID)
	if err != nil || len(rows) == 0 {
		return "", err
	}
	encoded, err := json.Marshal(drf.Rendered(moduleJSON(rows[0], true)))
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}
