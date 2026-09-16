package externalapi

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"github.com/yldm-tech/pace/apps/api-go/internal/drf"
	"github.com/yldm-tech/pace/apps/api-go/internal/pagination"
	"gorm.io/gorm"
)

func (handler *Handler) registerStateRoutes(router gin.IRouter) {
	base := "/api/v1/workspaces/:slug/projects/:project/states/"
	router.GET(base, handler.authenticated(handler.stateList))
	router.POST(base, handler.authenticated(handler.stateCreate))
	router.GET(base+":state/", handler.authenticated(handler.stateRetrieve))
	router.PATCH(base+":state/", handler.authenticated(handler.stateUpdate))
	router.DELETE(base+":state/", handler.authenticated(handler.stateDestroy))
}

// State is the db.State table as the external API sees it.
type State struct {
	ID             string     `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt      time.Time  `gorm:"column:created_at"`
	UpdatedAt      time.Time  `gorm:"column:updated_at"`
	CreatedByID    *string    `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID    *string    `gorm:"column:updated_by_id;type:uuid"`
	DeletedAt      *time.Time `gorm:"column:deleted_at"`
	ProjectID      string     `gorm:"column:project_id;type:uuid"`
	WorkspaceID    string     `gorm:"column:workspace_id;type:uuid"`
	Name           string     `gorm:"column:name"`
	Description    string     `gorm:"column:description"`
	Color          string     `gorm:"column:color"`
	Slug           string     `gorm:"column:slug"`
	Sequence       float64    `gorm:"column:sequence"`
	Group          string     `gorm:"column:group"`
	IsTriage       bool       `gorm:"column:is_triage"`
	Default        bool       `gorm:"column:default"`
	ExternalSource *string    `gorm:"column:external_source"`
	ExternalID     *string    `gorm:"column:external_id"`
}

func (State) TableName() string { return "states" }

// stateList returns a project's states, paged.
//
// The triage state is hidden from this API entirely: it is an implementation detail of intake, and an integration is never shown it.
func (handler *Handler) stateList(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireProjectMember(c, user, http.MethodGet) {
		return
	}
	var states []State
	if err := handler.stateScope(c, user).Order("s.sequence").Scan(&states).Error; err != nil {
		handler.serverError(c, err)
		return
	}
	fields := requestedFields(c)
	results := make([]gin.H, 0, len(states))
	for _, state := range states {
		results = append(results, narrow(stateJSON(state), fields))
	}
	handler.respondPaged(c, results)
}

func (handler *Handler) stateRetrieve(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireProjectMember(c, user, http.MethodGet) {
		return
	}
	var states []State
	err := handler.stateScope(c, user).Where("s.id = ?", c.Param("state")).Limit(1).Scan(&states).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if len(states) == 0 {
		notFound(c)
		return
	}
	drf.Respond(c, http.StatusOK, narrow(stateJSON(states[0]), requestedFields(c)))
}

// stateCreate adds a state to a project.
//
// It answers **200**, not 201. And it has two different conflicts: one for a repeated external id, checked before the write, and one for a repeated name, caught from the database afterwards. Both carry the id of the state that was already there, which is what makes them useful to an integration replaying a sync.
func (handler *Handler) stateCreate(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireProjectMember(c, user, http.MethodPost) {
		return
	}
	slug, projectID := c.Param("slug"), c.Param("project")
	var payload map[string]any
	if err := c.ShouldBindJSON(&payload); err != nil {
		invalidPayload(c)
		return
	}
	name, _ := payload["name"].(string)
	if strings.TrimSpace(name) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"name": []string{"This field is required."}})
		return
	}
	if group, ok := payload["group"].(string); ok && group == "triage" {
		c.JSON(http.StatusBadRequest, gin.H{"non_field_errors": []string{"Cannot create triage state"}})
		return
	}

	externalID, _ := payload["external_id"].(string)
	externalSource, _ := payload["external_source"].(string)
	if externalID != "" && externalSource != "" {
		var existing []string
		err := handler.db.WithContext(c.Request.Context()).Table("states s").
			Joins("JOIN workspaces w ON w.id = s.workspace_id").
			Where("w.slug = ? AND s.project_id = ? AND s.external_source = ? AND s.external_id = ? AND s.deleted_at IS NULL",
				slug, projectID, externalSource, externalID).
			Limit(1).Pluck("s.id", &existing).Error
		if err != nil {
			handler.serverError(c, err)
			return
		}
		if len(existing) > 0 {
			c.JSON(http.StatusConflict, gin.H{
				"error": "State with the same external id and external source already exists",
				"id":    existing[0],
			})
			return
		}
	}

	var workspaceIDs []string
	err := handler.db.WithContext(c.Request.Context()).Table("projects p").
		Joins("JOIN workspaces w ON w.id = p.workspace_id").
		Where("w.slug = ? AND p.id = ?", slug, projectID).Limit(1).Pluck("p.workspace_id", &workspaceIDs).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if len(workspaceIDs) == 0 {
		notFound(c)
		return
	}

	identifier, err := newUUID()
	if err != nil {
		handler.serverError(c, err)
		return
	}
	now := handler.clock().UTC()
	state := State{
		ID: identifier, CreatedAt: now, UpdatedAt: now, CreatedByID: &user.ID, UpdatedByID: &user.ID,
		ProjectID: projectID, WorkspaceID: workspaceIDs[0], Color: "#000000", Group: "backlog",
	}
	applyStatePayload(&state, payload)

	err = handler.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		// Making a state the default clears the flag from every other state in the project, and the serializer does it during validation rather than on save — so it happens even when the save that follows fails.
		if state.Default {
			err := tx.Model(&State{}).Where("project_id = ?", projectID).Update("default", false).Error
			if err != nil {
				return err
			}
		}
		return tx.Create(&state).Error
	})
	if err != nil {
		if isUniqueViolation(err) {
			var existing []string
			readErr := handler.db.WithContext(c.Request.Context()).Table("states s").
				Joins("JOIN workspaces w ON w.id = s.workspace_id").
				Where("w.slug = ? AND s.project_id = ? AND s.name = ? AND s.deleted_at IS NULL", slug, projectID, name).
				Limit(1).Pluck("s.id", &existing).Error
			if readErr != nil {
				handler.serverError(c, readErr)
				return
			}
			body := gin.H{"error": "State with the same name already exists in the project"}
			if len(existing) > 0 {
				body["id"] = existing[0]
			}
			c.JSON(http.StatusConflict, body)
			return
		}
		handler.serverError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, stateJSON(state))
}

// stateUpdate edits a state.
//
// Its queryset is not the list's: it does not exclude the triage state, so an integration that knows the id can edit the one the list never showed it.
func (handler *Handler) stateUpdate(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireProjectMember(c, user, http.MethodPatch) {
		return
	}
	slug, projectID := c.Param("slug"), c.Param("project")
	var payload map[string]any
	if err := c.ShouldBindJSON(&payload); err != nil {
		invalidPayload(c)
		return
	}

	var state State
	err := handler.db.WithContext(c.Request.Context()).Table("states s").Select("s.*").
		Joins("JOIN workspaces w ON w.id = s.workspace_id").
		Where("w.slug = ? AND s.project_id = ? AND s.id = ? AND s.deleted_at IS NULL", slug, projectID, c.Param("state")).
		Take(&state).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		notFound(c)
		return
	}
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if group, ok := payload["group"].(string); ok && group == "triage" {
		c.JSON(http.StatusBadRequest, gin.H{"non_field_errors": []string{"Cannot create triage state"}})
		return
	}

	// The external id is compared against the one the state already has, so rewriting a state with its own id is not a conflict.
	if externalID, ok := payload["external_id"].(string); ok && externalID != "" &&
		(state.ExternalID == nil || *state.ExternalID != externalID) {
		source := ""
		if state.ExternalSource != nil {
			source = *state.ExternalSource
		}
		if value, ok := payload["external_source"].(string); ok {
			source = value
		}
		var existing []string
		err := handler.db.WithContext(c.Request.Context()).Table("states s").
			Joins("JOIN workspaces w ON w.id = s.workspace_id").
			Where("w.slug = ? AND s.project_id = ? AND s.external_source = ? AND s.external_id = ? AND s.deleted_at IS NULL",
				slug, projectID, source, externalID).
			Limit(1).Pluck("s.id", &existing).Error
		if err != nil {
			handler.serverError(c, err)
			return
		}
		if len(existing) > 0 {
			c.JSON(http.StatusConflict, gin.H{
				"error": "State with the same external id and external source already exists",
				"id":    state.ID,
			})
			return
		}
	}

	applyStatePayload(&state, payload)
	now := handler.clock().UTC()
	state.UpdatedAt, state.UpdatedByID = now, &user.ID

	err = handler.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if wantsDefault(payload) {
			err := tx.Model(&State{}).Where("project_id = ?", projectID).Update("default", false).Error
			if err != nil {
				return err
			}
		}
		return tx.Model(&State{}).Where("id = ?", state.ID).Updates(map[string]any{
			"name": state.Name, "description": state.Description, "color": state.Color,
			"sequence": state.Sequence, "group": state.Group, "default": state.Default,
			"external_source": state.ExternalSource, "external_id": state.ExternalID,
			"updated_at": state.UpdatedAt, "updated_by_id": state.UpdatedByID,
		}).Error
	})
	if err != nil {
		handler.serverError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, stateJSON(state))
}

// stateDestroy removes a state, refusing the default one and any that still holds work.
//
// The check counts issues in **any** state of that id, through the plain manager — so an archived or draft issue keeps a state alive just as a live one does.
func (handler *Handler) stateDestroy(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireProjectMember(c, user, http.MethodDelete) {
		return
	}
	slug, projectID := c.Param("slug"), c.Param("project")

	var state State
	err := handler.db.WithContext(c.Request.Context()).Table("states s").Select("s.*").
		Joins("JOIN workspaces w ON w.id = s.workspace_id").
		Where("w.slug = ? AND s.project_id = ? AND s.id = ? AND s.is_triage = FALSE AND s.deleted_at IS NULL",
			slug, projectID, c.Param("state")).
		Take(&state).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		notFound(c)
		return
	}
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if state.Default {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Default state cannot be deleted"})
		return
	}
	var issues int64
	err = handler.db.WithContext(c.Request.Context()).Table("issues").
		Where("state_id = ? AND deleted_at IS NULL", state.ID).Count(&issues).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if issues > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "The state is not empty, only empty states can be deleted"})
		return
	}
	err = handler.db.WithContext(c.Request.Context()).Model(&State{}).
		Where("id = ?", state.ID).Update("deleted_at", handler.clock().UTC()).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// stateScope is the queryset the list and the retrieve read through.
func (handler *Handler) stateScope(c *gin.Context, user *auth.User) *gorm.DB {
	return handler.db.WithContext(c.Request.Context()).Table("states s").Select("s.*").
		Joins("JOIN workspaces w ON w.id = s.workspace_id").
		Joins("JOIN projects p ON p.id = s.project_id AND p.archived_at IS NULL").
		Joins("JOIN project_members pm ON pm.project_id = s.project_id AND pm.member_id = ? AND pm.is_active = TRUE", user.ID).
		Where("w.slug = ? AND s.project_id = ? AND s.is_triage = FALSE AND s.deleted_at IS NULL",
			c.Param("slug"), c.Param("project"))
}

// wantsDefault reports whether the request is turning the default on, which is the only case that clears it elsewhere.
func wantsDefault(payload map[string]any) bool {
	value, ok := payload["default"].(bool)
	return ok && value
}

// applyStatePayload writes the writable fields. Nine are read-only on the serializer, the slug among them.
func applyStatePayload(state *State, payload map[string]any) {
	if value, ok := payload["name"].(string); ok {
		state.Name = value
	}
	if value, ok := payload["description"].(string); ok {
		state.Description = value
	}
	if value, ok := payload["color"].(string); ok {
		state.Color = value
	}
	if value, ok := payload["group"].(string); ok {
		state.Group = value
	}
	if value, ok := payload["sequence"].(float64); ok {
		state.Sequence = value
	}
	if value, ok := payload["default"].(bool); ok {
		state.Default = value
	}
	if value, present := payload["external_source"]; present {
		if text, ok := value.(string); ok {
			state.ExternalSource = &text
		} else {
			state.ExternalSource = nil
		}
	}
	if value, present := payload["external_id"]; present {
		if text, ok := value.(string); ok {
			state.ExternalID = &text
		} else {
			state.ExternalID = nil
		}
	}
}

// stateJSON is the external API's StateSerializer, which asks for every field.
func stateJSON(state State) gin.H {
	return gin.H{
		"id": state.ID, "created_at": state.CreatedAt, "updated_at": state.UpdatedAt,
		"created_by": state.CreatedByID, "updated_by": state.UpdatedByID, "deleted_at": state.DeletedAt,
		"name": state.Name, "description": state.Description, "color": state.Color,
		"slug": state.Slug, "sequence": state.Sequence, "group": state.Group,
		"is_triage": state.IsTriage, "default": state.Default,
		"external_source": state.ExternalSource, "external_id": state.ExternalID,
		"project": state.ProjectID, "workspace": state.WorkspaceID,
	}
}

// respondPaged wraps a list in the same offset envelope the session API uses, which the external API inherits from the same paginator.
func (handler *Handler) respondPaged(c *gin.Context, results []gin.H) {
	handler.respondPagedWithDefault(c, results, pagination.DefaultPerPage)
}

// respondPagedWithDefault is the same envelope with the page size a route chooses. Most take the paginator's own default; the sticky list asks for twenty.
// The ceiling is the paginator's own, not the route's page size: only default_per_page is what a view like the sticky list overrides, and passing the two as one refuses any per_page above the default.
func (handler *Handler) respondPagedWithDefault(c *gin.Context, results []gin.H, defaultPerPage int) {
	perPage, err := pagination.PerPage(c.Query("per_page"), defaultPerPage, pagination.DefaultPerPage)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	cursor := pagination.OffsetCursor{Value: perPage}
	if raw := c.Query("cursor"); raw != "" {
		parsed, err := pagination.ParseOffsetCursor(raw)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"detail": "Invalid cursor parameter."})
			return
		}
		cursor = parsed
	}
	// OffsetPaginator's max_limit, which is MAX_LIMIT rather than the route's page size.
	page := pagination.PlanOffsetPage(perPage, cursor, len(results), len(results), pagination.DefaultPerPage)
	offset := page.Offset
	if offset > len(results) {
		offset = len(results)
	}
	end := offset + page.Limit
	if end > len(results) {
		end = len(results)
	}
	window := results[offset:end]
	drf.Respond(c, http.StatusOK, page.Envelope(window, len(window), nil, nil, nil))
}
