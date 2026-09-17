package project

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api/internal/auth"
	"github.com/yldm-tech/pace/apps/api/internal/drf"
	"gorm.io/gorm"
)

func (handler *Handler) registerIntakeRoutes(router gin.IRouter) {
	// The same viewset is mounted twice, under its current name and the one it had before intake was called inbox. Both are live and both have to be served.
	for _, name := range []string{"intakes", "inboxes"} {
		base := "/api/workspaces/:slug/projects/:id/" + name + "/"
		router.GET(base, handler.authenticated(handler.intakeList))
		router.POST(base, handler.authenticated(handler.intakeCreate))
		router.GET(base+":intake/", handler.authenticated(handler.intakeRetrieve))
		router.PATCH(base+":intake/", handler.authenticated(handler.intakeUpdate))
		router.DELETE(base+":intake/", handler.authenticated(handler.intakeDestroy))
	}
}

// intakeRow is the intake with the count of what is still waiting in it.
type intakeRow struct {
	Intake
	PendingIssueCount int64 `gorm:"column:pending_issue_count"`
}

// intakeList returns the project's intake — singular.
//
// The route is a list route and the handler serializes `.first()`, so the body is one object rather than an array. A project with no intake gets the **empty object**, because a serializer handed nothing renders nothing rather than failing.
func (handler *Handler) intakeList(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember) {
		return
	}
	rows, err := handler.intakeRows(c, "")
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if len(rows) == 0 {
		drf.Respond(c, http.StatusOK, gin.H{})
		return
	}
	data, err := handler.intakeJSON(c, rows[0])
	if err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, data)
}

// intakeRetrieve returns one intake.
func (handler *Handler) intakeRetrieve(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	rows, err := handler.intakeRows(c, c.Param("intake"))
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if len(rows) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"detail": "No Intake matches the given query."})
		return
	}
	data, err := handler.intakeJSON(c, rows[0])
	if err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, data)
}

// intakeCreate adds an intake to a project.
func (handler *Handler) intakeCreate(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember) {
		return
	}
	projectID := c.Param("id")

	var payload map[string]any
	if err := c.ShouldBindJSON(&payload); err != nil {
		handler.invalidDetail(c)
		return
	}
	if name, ok := payload["name"].(string); !ok || strings.TrimSpace(name) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"name": []string{"This field is required."}})
		return
	}

	var project Project
	if err := handler.db.WithContext(c.Request.Context()).Where("id = ?", projectID).Take(&project).Error; err != nil {
		handler.internalError(c, err)
		return
	}
	identifier, err := newUUID()
	if err != nil {
		handler.internalError(c, err)
		return
	}
	now := handler.clock().UTC()
	intake := Intake{
		ID: identifier, CreatedAt: now, UpdatedAt: now, CreatedByID: &user.ID, UpdatedByID: &user.ID,
		ProjectID: projectID, WorkspaceID: project.WorkspaceID,
		ViewProps: auth.JSONValue("{}"), LogoProps: auth.JSONValue("{}"),
	}
	applyIntakePayload(&intake, payload)

	if err := handler.db.WithContext(c.Request.Context()).Create(&intake).Error; err != nil {
		handler.internalError(c, err)
		return
	}
	data, err := handler.intakeJSON(c, intakeRow{Intake: intake})
	if err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusCreated, data)
}

// intakeUpdate edits an intake. The project and the workspace are read-only, so an intake cannot be moved between projects.
func (handler *Handler) intakeUpdate(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember) {
		return
	}
	var payload map[string]any
	if err := c.ShouldBindJSON(&payload); err != nil {
		handler.invalidDetail(c)
		return
	}
	rows, err := handler.intakeRows(c, c.Param("intake"))
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if len(rows) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"detail": "No Intake matches the given query."})
		return
	}

	intake := rows[0].Intake
	applyIntakePayload(&intake, payload)
	now := handler.clock().UTC()
	intake.UpdatedAt, intake.UpdatedByID = now, &user.ID

	err = handler.db.WithContext(c.Request.Context()).Model(&Intake{}).Where("id = ?", intake.ID).
		Updates(map[string]any{
			"name": intake.Name, "description": intake.Description, "is_default": intake.IsDefault,
			"view_props": intake.ViewProps, "logo_props": intake.LogoProps,
			"updated_at": intake.UpdatedAt, "updated_by_id": intake.UpdatedByID,
		}).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	data, err := handler.intakeJSON(c, intakeRow{Intake: intake, PendingIssueCount: rows[0].PendingIssueCount})
	if err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, data)
}

// intakeDestroy removes an intake, unless it is the one the project falls back to.
func (handler *Handler) intakeDestroy(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember) {
		return
	}
	slug, projectID, intakeID := c.Param("slug"), c.Param("id"), c.Param("intake")

	var intake Intake
	err := handler.db.WithContext(c.Request.Context()).Table("intakes i").Select("i.*").
		Joins("JOIN workspaces w ON w.id = i.workspace_id").
		Where("w.slug = ? AND i.project_id = ? AND i.id = ? AND i.deleted_at IS NULL", slug, projectID, intakeID).
		Take(&intake).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// Django reads is_default off the result of .first() with no guard, so an intake that is not there raises rather than answering 404.
		handler.internalError(c, errors.New("intake: the intake does not exist"))
		return
	}
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if intake.IsDefault {
		c.JSON(http.StatusBadRequest, gin.H{"error": "You cannot delete the default intake"})
		return
	}
	err = handler.db.WithContext(c.Request.Context()).Model(&Intake{}).
		Where("id = ?", intake.ID).Update("deleted_at", handler.clock().UTC()).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// intakeRows reads the intakes of a project with the pending count annotated on.
func (handler *Handler) intakeRows(c *gin.Context, intakeID string) ([]intakeRow, error) {
	query := handler.db.WithContext(c.Request.Context()).Table("intakes i").
		Select(`i.*, (SELECT COUNT(*) FROM intake_issues ii
			WHERE ii.intake_id = i.id AND ii.status = -2 AND ii.deleted_at IS NULL) AS pending_issue_count`).
		Joins("JOIN workspaces w ON w.id = i.workspace_id").
		Where("w.slug = ? AND i.project_id = ? AND i.deleted_at IS NULL", c.Param("slug"), c.Param("id"))
	if intakeID != "" {
		query = query.Where("i.id = ?", intakeID)
	}
	var rows []intakeRow
	err := query.Order("i.created_at DESC").Limit(1).Scan(&rows).Error
	return rows, err
}

// intakeJSON is IntakeSerializer: every column, plus the project expanded and the pending count.
func (handler *Handler) intakeJSON(c *gin.Context, row intakeRow) (gin.H, error) {
	project, err := handler.projectLite(c.Request.Context(), row.ProjectID)
	if err != nil {
		return nil, err
	}
	return gin.H{
		"id": row.ID, "created_at": row.CreatedAt, "updated_at": row.UpdatedAt,
		"created_by": row.CreatedByID, "updated_by": row.UpdatedByID, "deleted_at": row.DeletedAt,
		"name": row.Name, "description": row.Description, "is_default": row.IsDefault,
		"view_props": decodeJSON(row.ViewProps), "logo_props": decodeJSON(row.LogoProps),
		"project": row.ProjectID, "workspace": row.WorkspaceID,
		"project_detail": project, "pending_issue_count": row.PendingIssueCount,
	}, nil
}

// applyIntakePayload writes the writable fields.
func applyIntakePayload(intake *Intake, payload map[string]any) {
	if value, ok := payload["name"].(string); ok {
		intake.Name = value
	}
	if value, ok := payload["description"].(string); ok {
		intake.Description = value
	}
	if value, ok := payload["is_default"].(bool); ok {
		intake.IsDefault = value
	}
	for key, target := range map[string]*auth.JSONValue{"view_props": &intake.ViewProps, "logo_props": &intake.LogoProps} {
		value, present := payload[key]
		if !present {
			continue
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			continue
		}
		*target = auth.JSONValue(encoded)
	}
}
