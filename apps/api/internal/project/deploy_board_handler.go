package project

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api/internal/auth"
	"github.com/yldm-tech/pace/apps/api/internal/drf"
)

func (handler *Handler) registerDeployBoardRoutes(router gin.IRouter) {
	const base = "/api/workspaces/:slug/projects/:id/project-deploy-boards/"
	router.GET(base, handler.authenticated(handler.deployBoardRead))
	router.POST(base, handler.authenticated(handler.deployBoardPublish))
	router.GET(base+":board/", handler.authenticated(handler.deployBoardRetrieve))
	router.PATCH(base+":board/", handler.authenticated(handler.deployBoardUpdate))
	router.DELETE(base+":board/", handler.authenticated(handler.deployBoardDestroy))
}

// DeployBoard is what makes a project readable without an account: the anchor in its url is the whole of the credential.
type DeployBoard struct {
	ID                 string     `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt          time.Time  `gorm:"column:created_at"`
	UpdatedAt          time.Time  `gorm:"column:updated_at"`
	CreatedByID        *string    `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID        *string    `gorm:"column:updated_by_id;type:uuid"`
	DeletedAt          *time.Time `gorm:"column:deleted_at"`
	ProjectID          *string    `gorm:"column:project_id;type:uuid"`
	WorkspaceID        string     `gorm:"column:workspace_id;type:uuid"`
	EntityIdentifier   *string    `gorm:"column:entity_identifier;type:uuid"`
	EntityName         string     `gorm:"column:entity_name"`
	Anchor             string     `gorm:"column:anchor"`
	IsCommentsEnabled  bool       `gorm:"column:is_comments_enabled"`
	IsReactionsEnabled bool       `gorm:"column:is_reactions_enabled"`
	IsVotesEnabled     bool       `gorm:"column:is_votes_enabled"`
	ViewProps          []byte     `gorm:"column:view_props;type:jsonb"`
	IsActivityEnabled  bool       `gorm:"column:is_activity_enabled"`
	IsDisabled         bool       `gorm:"column:is_disabled"`
	IntakeID           *string    `gorm:"column:intake_id;type:uuid"`
}

func (DeployBoard) TableName() string { return "deploy_boards" }

// deployBoardRead answers with the project's board, and with a **partial object** when there is none.
//
// The list route reports one board rather than a list, and a project that was never published is answered with the serializer over nothing: twelve keys of defaults, no id and no anchor, rather than an empty list or a 404.
func (handler *Handler) deployBoardRead(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember) {
		return
	}
	board, found, err := handler.deployBoardForProject(c)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		drf.Respond(c, http.StatusOK, emptyDeployBoardJSON())
		return
	}
	body, err := handler.deployBoardJSON(c, board)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, body)
}

// deployBoardPublish makes the board or edits the one that is there, and answers **200** either way.
//
// Every switch it does not name is written as **off**: a second call that means to turn comments on turns the other three off with it, because the payload's absent keys are read as false rather than left alone.
func (handler *Handler) deployBoardPublish(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember) {
		return
	}
	var payload map[string]json.RawMessage
	if err := c.ShouldBindJSON(&payload); err != nil {
		handler.invalidDetail(c)
		return
	}
	readBool := func(field string) bool {
		raw, given := payload[field]
		if !given {
			return false
		}
		var value bool
		if json.Unmarshal(raw, &value) != nil {
			return false
		}
		return value
	}
	var intake any
	if raw, given := payload["intake"]; given && string(raw) != "null" {
		var value string
		if json.Unmarshal(raw, &value) == nil && value != "" {
			intake = value
		}
	}
	views := auth.JSONValue(defaultDeployBoardViews)
	if raw, given := payload["views"]; given && json.Valid(raw) {
		views = auth.JSONValue(append([]byte(nil), raw...))
	}

	projectID := c.Param("id")
	board, found, err := handler.deployBoardForProject(c)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	now := handler.clock().UTC()
	updates := map[string]any{
		"intake_id": intake, "view_props": views,
		"is_votes_enabled":     readBool("is_votes_enabled"),
		"is_comments_enabled":  readBool("is_comments_enabled"),
		"is_reactions_enabled": readBool("is_reactions_enabled"),
		"updated_at":           now,
	}
	if !found {
		identifier, err := newUUID()
		if err != nil {
			handler.internalError(c, err)
			return
		}
		anchor, err := newUUID()
		if err != nil {
			handler.internalError(c, err)
			return
		}
		var workspaceIDs []string
		err = handler.db.WithContext(c.Request.Context()).Table("projects").
			Where("id = ?", projectID).Limit(1).Pluck("workspace_id", &workspaceIDs).Error
		if err != nil {
			handler.internalError(c, err)
			return
		}
		if len(workspaceIDs) == 0 {
			handler.notFound(c)
			return
		}
		row := map[string]any{
			"id": identifier, "created_at": now, "updated_at": now,
			"project_id": projectID, "workspace_id": workspaceIDs[0],
			"entity_name": "project", "entity_identifier": projectID,
			// The anchor is thirty-two hexadecimal characters with no dashes, which is what a project's public url carries.
			"anchor": strings.ReplaceAll(anchor, "-", ""),
			// get_or_create writes the row with its column defaults, and the save that follows writes the payload over them.
			"is_activity_enabled": true, "is_disabled": false,
		}
		for key, value := range updates {
			row[key] = value
		}
		if err := handler.db.WithContext(c.Request.Context()).Table("deploy_boards").Create(row).Error; err != nil {
			handler.internalError(c, err)
			return
		}
		board, found, err = handler.deployBoardForProject(c)
		if err != nil || !found {
			handler.internalError(c, err)
			return
		}
	} else {
		err := handler.db.WithContext(c.Request.Context()).Table("deploy_boards").
			Where("id = ?", board.ID).Updates(updates).Error
		if err != nil {
			handler.internalError(c, err)
			return
		}
		board, _, err = handler.deployBoardForProject(c)
		if err != nil {
			handler.internalError(c, err)
			return
		}
	}
	body, err := handler.deployBoardJSON(c, board)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, body)
}

// deployBoardRetrieve, deployBoardUpdate and deployBoardDestroy act on the board the url names.
//
// Django looks it up by id alone — the viewset's queryset is every board there is — so a board of another project could be read or edited through a project the caller is in. The port scopes the lookup to the project in the url instead.
func (handler *Handler) deployBoardRetrieve(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember) {
		return
	}
	board, found, err := handler.deployBoardByID(c)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		handler.notFound(c)
		return
	}
	body, err := handler.deployBoardJSON(c, board)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, body)
}

func (handler *Handler) deployBoardUpdate(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember) {
		return
	}
	board, found, err := handler.deployBoardByID(c)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		handler.notFound(c)
		return
	}
	var payload map[string]json.RawMessage
	if err := c.ShouldBindJSON(&payload); err != nil {
		handler.invalidDetail(c)
		return
	}
	updates := map[string]any{}
	for _, field := range []string{
		"is_comments_enabled", "is_reactions_enabled", "is_votes_enabled",
		"is_activity_enabled", "is_disabled",
	} {
		raw, given := payload[field]
		if !given {
			continue
		}
		var value bool
		if json.Unmarshal(raw, &value) != nil {
			c.JSON(http.StatusBadRequest, gin.H{field: []string{"Must be a valid boolean."}})
			return
		}
		updates[field] = value
	}
	if raw, given := payload["view_props"]; given && json.Valid(raw) {
		updates["view_props"] = auth.JSONValue(append([]byte(nil), raw...))
	}
	if raw, given := payload["intake"]; given {
		if string(raw) == "null" {
			updates["intake_id"] = nil
		} else {
			var value string
			if json.Unmarshal(raw, &value) == nil {
				updates["intake_id"] = value
			}
		}
	}
	// The anchor is read-only on the serializer, so a payload naming one is ignored rather than refused.
	if len(updates) > 0 {
		now := handler.clock().UTC()
		updates["updated_at"] = now
		updates["updated_by_id"] = user.ID
		err := handler.db.WithContext(c.Request.Context()).Table("deploy_boards").
			Where("id = ?", board.ID).Updates(updates).Error
		if err != nil {
			handler.internalError(c, err)
			return
		}
	}
	refreshed, _, err := handler.deployBoardByID(c)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	body, err := handler.deployBoardJSON(c, refreshed)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, body)
}

func (handler *Handler) deployBoardDestroy(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember) {
		return
	}
	board, found, err := handler.deployBoardByID(c)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		handler.notFound(c)
		return
	}
	now := handler.clock().UTC()
	err = handler.db.WithContext(c.Request.Context()).Table("deploy_boards").Where("id = ?", board.ID).
		Updates(map[string]any{"deleted_at": now, "updated_at": now}).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if handler.tasks != nil {
		if err := handler.tasks.PublishSoftDeleteRelatedObjects(c.Request.Context(), "db", "deployboard", board.ID); err != nil {
			handler.internalError(c, err)
			return
		}
	}
	c.Status(http.StatusNoContent)
}

// defaultDeployBoardViews is the five layouts a published project offers when the payload names none.
var defaultDeployBoardViews = []byte(`{"list":true,"kanban":true,"calendar":true,"gantt":true,"spreadsheet":true}`)

func (handler *Handler) deployBoardForProject(c *gin.Context) (DeployBoard, bool, error) {
	var boards []DeployBoard
	err := handler.db.WithContext(c.Request.Context()).Table("deploy_boards d").Select("d.*").
		Joins("JOIN workspaces w ON w.id = d.workspace_id").
		Where(`w.slug = ? AND d.entity_name = 'project' AND d.entity_identifier = ? AND d.deleted_at IS NULL`,
			c.Param("slug"), c.Param("id")).
		Order("d.created_at").Limit(1).Scan(&boards).Error
	if err != nil || len(boards) == 0 {
		return DeployBoard{}, false, err
	}
	return boards[0], true, nil
}

func (handler *Handler) deployBoardByID(c *gin.Context) (DeployBoard, bool, error) {
	var boards []DeployBoard
	err := handler.db.WithContext(c.Request.Context()).Table("deploy_boards d").Select("d.*").
		Joins("JOIN workspaces w ON w.id = d.workspace_id").
		Where("w.slug = ? AND d.project_id = ? AND d.id = ? AND d.deleted_at IS NULL",
			c.Param("slug"), c.Param("id"), c.Param("board")).
		Limit(1).Scan(&boards).Error
	if err != nil || len(boards) == 0 {
		return DeployBoard{}, false, err
	}
	return boards[0], true, nil
}

// deployBoardJSON is DeployBoardSerializer, which nests the project and the workspace beside the board.
func (handler *Handler) deployBoardJSON(c *gin.Context, board DeployBoard) (gin.H, error) {
	project := any(nil)
	if board.ProjectID != nil {
		rendered, err := handler.projectLiteJSON(c.Request.Context(), *board.ProjectID)
		if err != nil {
			return nil, err
		}
		project = rendered
	}
	workspace, err := handler.workspaceLiteJSON(c.Request.Context(), board.WorkspaceID)
	if err != nil {
		return nil, err
	}
	return gin.H{
		"id": board.ID, "project_details": project, "workspace_detail": workspace,
		"created_at": board.CreatedAt, "updated_at": board.UpdatedAt, "deleted_at": board.DeletedAt,
		"entity_identifier": board.EntityIdentifier, "entity_name": board.EntityName,
		"anchor": board.Anchor, "is_comments_enabled": board.IsCommentsEnabled,
		"is_reactions_enabled": board.IsReactionsEnabled, "is_votes_enabled": board.IsVotesEnabled,
		"view_props": decodeJSON(board.ViewProps), "is_activity_enabled": board.IsActivityEnabled,
		"is_disabled": board.IsDisabled, "created_by": board.CreatedByID, "updated_by": board.UpdatedByID,
		"workspace": board.WorkspaceID, "project": board.ProjectID, "intake": board.IntakeID,
	}, nil
}

// emptyDeployBoardJSON is the serializer over nothing, which is what an unpublished project answers with.
//
// It is **twelve** keys rather than twenty: a serializer with no instance reports the fields that could be written and the defaults they would take, so every read-only field — the id, the anchor, the two nested objects, the timestamps — is absent rather than null.
func emptyDeployBoardJSON() gin.H {
	return gin.H{
		"deleted_at": nil, "entity_identifier": nil, "entity_name": "",
		"is_comments_enabled": false, "is_reactions_enabled": false, "is_votes_enabled": false,
		"view_props": nil, "is_activity_enabled": false, "is_disabled": false,
		"created_by": nil, "updated_by": nil, "intake": nil,
	}
}
