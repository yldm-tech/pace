package project

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"github.com/yldm-tech/pace/apps/api-go/internal/drf"
	"gorm.io/gorm"
)

func (handler *Handler) registerStateRoutes(router gin.IRouter) {
	const base = "/api/workspaces/:slug/projects/:id/states/"
	router.GET(base, handler.authenticated(handler.stateList))
	router.POST(base, handler.authenticated(handler.stateCreate))
	router.GET(base+":state/", handler.authenticated(handler.stateRetrieve))
	router.PATCH(base+":state/", handler.authenticated(handler.stateUpdate))
	router.DELETE(base+":state/", handler.authenticated(handler.stateDestroy))
	router.POST(base+":state/mark-default/", handler.authenticated(handler.stateMarkDefault))
	router.GET("/api/workspaces/:slug/projects/:id/intake-state/", handler.authenticated(handler.intakeState))
}

// stateList returns a project's workflow states.
//
// It is the one shape here that carries `order`, which is not a column: the states are numbered within their own group and each one reports its **place as a fraction** of the group's size. A group of four numbers its states 0.25, 0.5, 0.75 and 1. The serializer declares the field and an instance never has it, so every other route drops the key rather than reporting a null.
func (handler *Handler) stateList(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	states, err := handler.projectStates(c, user)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	results := make([]gin.H, 0, len(states))
	counts := map[string]int{}
	for _, state := range states {
		counts[state.Group]++
	}
	seen := map[string]int{}
	for _, state := range states {
		seen[state.Group]++
		body := stateJSON(state)
		body["order"] = float64(seen[state.Group]) / float64(counts[state.Group])
		results = append(results, body)
	}

	if c.Query("grouped") != "true" {
		drf.Respond(c, http.StatusOK, results)
		return
	}
	// grouped=true answers an object keyed by group rather than a list, and the groups come back in the order their names sort.
	grouped := map[string][]gin.H{}
	names := []string{}
	for _, body := range results {
		group, _ := body["group"].(string)
		if _, seenGroup := grouped[group]; !seenGroup {
			names = append(names, group)
		}
		grouped[group] = append(grouped[group], body)
	}
	sort.Strings(names)
	answer := gin.H{}
	for _, name := range names {
		answer[name] = grouped[name]
	}
	drf.Respond(c, http.StatusOK, answer)
}

// stateRetrieve returns one state. It asks for nothing but an active membership, since the route carries no role of its own and the queryset is what narrows it.
func (handler *Handler) stateRetrieve(c *gin.Context, user *auth.User) {
	states, err := handler.projectStates(c, user)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	for _, state := range states {
		if state.ID == c.Param("state") {
			drf.Respond(c, http.StatusOK, stateJSON(state))
			return
		}
	}
	handler.notFound(c)
}

// projectStates is the queryset the list and the retrieve share: the project's states with the triage one hidden, and only while the caller is an active member of an unarchived project.
func (handler *Handler) projectStates(c *gin.Context, user *auth.User) ([]State, error) {
	var states []State
	err := handler.db.WithContext(c.Request.Context()).Table("states s").Select("s.*").
		Joins("JOIN workspaces w ON w.id = s.workspace_id").
		Joins("JOIN projects p ON p.id = s.project_id AND p.archived_at IS NULL").
		Joins("JOIN project_members pm ON pm.project_id = s.project_id AND pm.member_id = ? AND pm.is_active = TRUE", user.ID).
		Where("w.slug = ? AND s.project_id = ? AND s.is_triage = FALSE AND s.deleted_at IS NULL",
			c.Param("slug"), c.Param("id")).
		Order("s.sequence").Scan(&states).Error
	return states, err
}

// stateCreate adds a state. It answers **200** rather than 201, and a name that is taken is a bad request rather than a conflict.
func (handler *Handler) stateCreate(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin) {
		return
	}
	var body map[string]json.RawMessage
	if err := c.ShouldBindJSON(&body); err != nil {
		handler.invalidDetail(c)
		return
	}
	values, failures := handler.stateFields(body, false)
	if failures != nil {
		c.JSON(http.StatusBadRequest, failures)
		return
	}

	projectID := c.Param("id")
	var workspaceIDs []string
	err := handler.db.WithContext(c.Request.Context()).Table("projects").
		Where("id = ?", projectID).Limit(1).Pluck("workspace_id", &workspaceIDs).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if len(workspaceIDs) == 0 {
		handler.notFound(c)
		return
	}
	stateID, err := newUUID()
	if err != nil {
		handler.internalError(c, err)
		return
	}
	now := handler.clock().UTC()
	state := State{
		ID: stateID, CreatedAt: now, UpdatedAt: now, CreatedByID: &user.ID, UpdatedByID: &user.ID,
		ProjectID: projectID, WorkspaceID: workspaceIDs[0],
		Name: values.name, Color: values.color, Group: values.group,
		Description: values.description, Sequence: values.sequence, Default: values.isDefault,
	}
	if err := handler.db.WithContext(c.Request.Context()).Create(&state).Error; err != nil {
		if isUniqueViolation(err) {
			c.JSON(http.StatusBadRequest, gin.H{"name": "The state name is already taken"})
			return
		}
		handler.internalError(c, err)
		return
	}
	if err := handler.invalidateWorkspaceStates(c.Request.Context(), c.Param("slug")); err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, stateJSON(state))
}

// stateUpdate edits a state. Unlike the create it is open to every member, including a guest — the only write on a project's workflow that is.
func (handler *Handler) stateUpdate(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	state, found, err := handler.stateByID(c)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		handler.notFound(c)
		return
	}
	var body map[string]json.RawMessage
	if err := c.ShouldBindJSON(&body); err != nil {
		handler.invalidDetail(c)
		return
	}
	values, failures := handler.stateFields(body, true)
	if failures != nil {
		c.JSON(http.StatusBadRequest, failures)
		return
	}
	updates := map[string]any{"updated_at": handler.clock().UTC(), "updated_by_id": user.ID}
	if values.hasName {
		updates["name"] = values.name
	}
	if values.hasColor {
		updates["color"] = values.color
	}
	if values.hasGroup {
		updates["group"] = values.group
	}
	if values.hasDescription {
		updates["description"] = values.description
	}
	if values.hasSequence {
		updates["sequence"] = values.sequence
	}
	if values.hasDefault {
		updates["default"] = values.isDefault
	}
	err = handler.db.WithContext(c.Request.Context()).Model(&State{}).Where("id = ?", state.ID).Updates(updates).Error
	if err != nil {
		if isUniqueViolation(err) {
			c.JSON(http.StatusBadRequest, gin.H{"name": "The state name is already taken"})
			return
		}
		handler.internalError(c, err)
		return
	}
	// The update writes no cache invalidation, unlike the create, the delete and the default switch.
	refreshed, found, err := handler.stateByID(c)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		handler.notFound(c)
		return
	}
	drf.Respond(c, http.StatusOK, stateJSON(refreshed))
}

// stateMarkDefault moves the default flag onto one state. It clears the flag across the whole project first, so a project always has at most one default.
func (handler *Handler) stateMarkDefault(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin) {
		return
	}
	slug, projectID, stateID := c.Param("slug"), c.Param("id"), c.Param("state")
	err := handler.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		// Both updates are unguarded: naming a state that is not there clears the default and sets nothing, which is what Django does.
		err := tx.Table("states").
			Where(`project_id = ? AND workspace_id = (SELECT id FROM workspaces WHERE slug = ?) AND "default" = TRUE AND deleted_at IS NULL`,
				projectID, slug).
			Update("default", false).Error
		if err != nil {
			return err
		}
		return tx.Table("states").
			Where(`project_id = ? AND workspace_id = (SELECT id FROM workspaces WHERE slug = ?) AND id = ? AND deleted_at IS NULL`,
				projectID, slug, stateID).
			Update("default", true).Error
	})
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if err := handler.invalidateWorkspaceStates(c.Request.Context(), slug); err != nil {
		handler.internalError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// stateDestroy removes a state, and refuses two of them: the project's default, and any state that still holds work.
//
// The emptiness check reads the plain manager, so an archived or draft work item keeps its state alive while a soft-deleted one does not.
func (handler *Handler) stateDestroy(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin) {
		return
	}
	state, found, err := handler.stateByID(c)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		handler.notFound(c)
		return
	}
	if state.Default {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Default state cannot be deleted"})
		return
	}
	var holding int64
	err = handler.db.WithContext(c.Request.Context()).Table("issues").
		Where("state_id = ? AND deleted_at IS NULL", state.ID).Count(&holding).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if holding > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "The state is not empty, only empty states can be deleted"})
		return
	}
	now := handler.clock().UTC()
	err = handler.db.WithContext(c.Request.Context()).Model(&State{}).Where("id = ?", state.ID).
		Updates(map[string]any{"deleted_at": now, "updated_at": now}).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if handler.tasks != nil {
		if err := handler.tasks.PublishSoftDeleteRelatedObjects(c.Request.Context(), "db", "state", state.ID); err != nil {
			handler.internalError(c, err)
			return
		}
	}
	if err := handler.invalidateWorkspaceStates(c.Request.Context(), c.Param("slug")); err != nil {
		handler.internalError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// intakeState returns the project's triage state, which is the one state the other routes here hide.
func (handler *Handler) intakeState(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	var states []State
	err := handler.db.WithContext(c.Request.Context()).Table("states s").Select("s.*").
		Joins("JOIN workspaces w ON w.id = s.workspace_id").
		Where("w.slug = ? AND s.project_id = ? AND s.is_triage = TRUE AND s.deleted_at IS NULL",
			c.Param("slug"), c.Param("id")).
		Order("s.sequence").Limit(1).Scan(&states).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if len(states) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Triage state not found"})
		return
	}
	drf.Respond(c, http.StatusOK, stateJSON(states[0]))
}

// stateByID reads one state of the project, triage included on the write paths — the delete asks for a non-triage one of its own.
func (handler *Handler) stateByID(c *gin.Context) (State, bool, error) {
	var states []State
	err := handler.db.WithContext(c.Request.Context()).Table("states s").Select("s.*").
		Joins("JOIN workspaces w ON w.id = s.workspace_id").
		Where("w.slug = ? AND s.project_id = ? AND s.id = ? AND s.deleted_at IS NULL",
			c.Param("slug"), c.Param("id"), c.Param("state")).
		Limit(1).Scan(&states).Error
	if err != nil || len(states) == 0 {
		return State{}, false, err
	}
	return states[0], true, nil
}

// stateGroups is the five groups a state may belong to. Triage is a sixth the serializer refuses to write.
var stateGroups = []string{"backlog", "unstarted", "started", "completed", "cancelled"}

type stateInput struct {
	name           string
	color          string
	group          string
	description    string
	sequence       float64
	isDefault      bool
	hasName        bool
	hasColor       bool
	hasGroup       bool
	hasDescription bool
	hasSequence    bool
	hasDefault     bool
}

// stateFields is StateSerializer's validation. The group defaults to backlog and triage is refused outright, which is what keeps the intake state out of reach of these routes.
func (handler *Handler) stateFields(body map[string]json.RawMessage, partial bool) (stateInput, gin.H) {
	input := stateInput{group: "backlog", sequence: 65535}
	errors := gin.H{}

	for _, field := range []struct {
		name     string
		target   *string
		marker   *bool
		required bool
	}{
		{"name", &input.name, &input.hasName, true},
		{"color", &input.color, &input.hasColor, true},
		{"description", &input.description, &input.hasDescription, false},
	} {
		raw, given := body[field.name]
		if !given {
			if field.required && !partial {
				errors[field.name] = []string{"This field is required."}
			}
			continue
		}
		if string(raw) == "null" {
			errors[field.name] = []string{"This field may not be null."}
			continue
		}
		var value string
		if json.Unmarshal(raw, &value) != nil {
			errors[field.name] = []string{"Not a valid string."}
			continue
		}
		if field.required && value == "" {
			errors[field.name] = []string{"This field may not be blank."}
			continue
		}
		*field.target = value
		*field.marker = true
	}
	if raw, given := body["group"]; given {
		var value string
		if json.Unmarshal(raw, &value) != nil {
			errors["group"] = []string{`"" is not a valid choice.`}
		} else {
			known := false
			for _, group := range stateGroups {
				if group == value {
					known = true
				}
			}
			switch {
			case value == "triage":
				// The refusal is a non-field one, since the serializer raises it from validate rather than from the field.
				return stateInput{}, gin.H{"non_field_errors": []string{"Cannot create triage state"}}
			case !known:
				errors["group"] = []string{`"` + value + `" is not a valid choice.`}
			default:
				input.group, input.hasGroup = value, true
			}
		}
	}
	if raw, given := body["sequence"]; given {
		var value float64
		if json.Unmarshal(raw, &value) != nil {
			errors["sequence"] = []string{"A valid number is required."}
		} else {
			input.sequence, input.hasSequence = value, true
		}
	}
	if raw, given := body["default"]; given {
		var value bool
		if json.Unmarshal(raw, &value) != nil {
			errors["default"] = []string{"Must be a valid boolean."}
		} else {
			input.isDefault, input.hasDefault = value, true
		}
	}
	if len(errors) > 0 {
		return stateInput{}, errors
	}
	return input, nil
}

// invalidateWorkspaceStates drops the cached workspace state list, which is what the decorator on three of these routes does. The update is not one of them.
func (handler *Handler) invalidateWorkspaceStates(ctx context.Context, slug string) error {
	if handler.cache == nil {
		return nil
	}
	return handler.cache.InvalidatePattern(ctx, "*/api/workspaces/"+slug+"/states/*")
}

// stateJSON is StateSerializer: nine fields, and `order` only where the list adds it.
func stateJSON(state State) gin.H {
	return gin.H{
		"id": state.ID, "project_id": state.ProjectID, "workspace_id": state.WorkspaceID,
		"name": state.Name, "color": state.Color, "group": state.Group,
		"default": state.Default, "description": state.Description, "sequence": state.Sequence,
	}
}
