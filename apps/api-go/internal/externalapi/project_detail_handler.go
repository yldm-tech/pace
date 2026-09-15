package externalapi

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"github.com/yldm-tech/pace/apps/api-go/internal/drf"
	"gorm.io/gorm"
)

func (handler *Handler) registerProjectDetailRoutes(router gin.IRouter) {
	base := "/api/v1/workspaces/:slug/projects/:project/"
	router.GET(base, handler.authenticated(handler.projectRetrieve))
	router.PATCH(base, handler.authenticated(handler.projectUpdate))
	router.DELETE(base, handler.authenticated(handler.projectDestroy))
}

// projectRow is the project with the seven things the full serializer annotates onto it.
type projectRow struct {
	Project
	TotalMembers int64    `gorm:"column:total_members"`
	TotalCycles  int64    `gorm:"column:total_cycles"`
	TotalModules int64    `gorm:"column:total_modules"`
	IsMember     bool     `gorm:"column:is_member"`
	SortOrder    *float64 `gorm:"column:sort_order"`
	MemberRole   *int     `gorm:"column:member_role"`
	IsDeployed   bool     `gorm:"column:is_deployed"`
}

// forbiddenIdentifierChars is Project.FORBIDDEN_IDENTIFIER_CHARS_PATTERN.
//
// It is applied to the **name** as well as the identifier, so a project cannot be called "Q1 (planning)" or "Auth & billing" through this API — every one of those characters is refused. The pattern is anchored at both ends around a wildcard, so it is really a containment test.
var forbiddenIdentifierChars = regexp.MustCompile(`[&+,:;$^}{*=?@#|'<>.()%!-]`)

// projectRetrieve returns one project with its counts.
func (handler *Handler) projectRetrieve(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireProjectBase(c, user, http.MethodGet) {
		return
	}
	rows, err := handler.projectRows(c, user, c.Param("project"))
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if len(rows) == 0 {
		notFound(c)
		return
	}
	drf.Respond(c, http.StatusOK, narrow(fullProjectJSON(rows[0]), requestedFields(c)))
}

// projectUpdate edits a project.
//
// An **archived** project refuses every change, which is checked before the serializer runs. Turning intake on creates the project's default intake if it has none, and the name it gets is built from the project's name **as it was before this request** — the serializer has already saved by then, but the handler is holding the instance it read at the start.
func (handler *Handler) projectUpdate(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireProjectBase(c, user, http.MethodPatch) {
		return
	}
	slug, projectID := c.Param("slug"), c.Param("project")
	var payload map[string]any
	if err := c.ShouldBindJSON(&payload); err != nil {
		invalidPayload(c)
		return
	}
	project, found, err := handler.projectByID(c)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "Project does not exist"})
		return
	}
	if project.ArchivedAt != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Archived project cannot be updated"})
		return
	}

	if name, ok := payload["name"].(string); ok && forbiddenIdentifierChars.MatchString(name) {
		c.JSON(http.StatusBadRequest, gin.H{
			"non_field_errors": []string{"Project name cannot contain special characters."},
		})
		return
	}
	if identifier, ok := payload["identifier"].(string); ok && forbiddenIdentifierChars.MatchString(identifier) {
		c.JSON(http.StatusBadRequest, gin.H{
			"non_field_errors": []string{"Project identifier cannot contain special characters."},
		})
		return
	}
	if value, ok := payload["default_state"].(string); ok && value != "" {
		belongs, err := handler.rowBelongsToProject(c, "states", value, projectID)
		if err != nil {
			handler.serverError(c, err)
			return
		}
		if !belongs {
			c.JSON(http.StatusBadRequest, gin.H{
				"non_field_errors": []string{"Default state should be a state in the project"},
			})
			return
		}
	}
	if value, ok := payload["estimate"].(string); ok && value != "" {
		belongs, err := handler.rowBelongsToProject(c, "estimates", value, projectID)
		if err != nil {
			handler.serverError(c, err)
			return
		}
		if !belongs {
			c.JSON(http.StatusBadRequest, gin.H{
				"non_field_errors": []string{"Estimate should be a estimate in the project"},
			})
			return
		}
	}

	snapshot, err := handler.projectSnapshot(c, user, projectID)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	updates := projectUpdates(payload)
	// intake_view is read back out of the request with the project's current value as the default, and then written whether the request named it or not.
	intakeOn := project.IntakeView
	if value, ok := payload["intake_view"].(bool); ok {
		intakeOn = value
	}
	updates["intake_view"] = intakeOn
	now := handler.clock().UTC()
	updates["updated_at"] = now
	updates["updated_by_id"] = user.ID

	err = handler.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Table("projects").Where("id = ?", project.ID).Updates(updates).Error; err != nil {
			return err
		}
		if !intakeOn {
			return nil
		}
		var existing int64
		err := tx.Table("intakes").
			Where("project_id = ? AND is_default = TRUE AND deleted_at IS NULL", project.ID).
			Count(&existing).Error
		if err != nil || existing > 0 {
			return err
		}
		identifier, err := newUUID()
		if err != nil {
			return err
		}
		// The name uses the project's name from before the save, which differs from the one just written whenever the same request renamed it.
		return tx.Table("intakes").Create(map[string]any{
			"id": identifier, "created_at": now, "updated_at": now,
			"name": project.Name + " Intake", "description": "", "is_default": true,
			"project_id": project.ID, "workspace_id": project.WorkspaceID,
			"view_props": []byte("{}"), "logo_props": []byte("{}"),
		}).Error
	})
	if err != nil {
		if isUniqueViolation(err) {
			c.JSON(http.StatusConflict, gin.H{"name": "The project name is already taken"})
			return
		}
		handler.serverError(c, err)
		return
	}

	if handler.tasks != nil {
		requested, err := json.Marshal(payload)
		if err != nil {
			handler.serverError(c, err)
			return
		}
		err = handler.tasks.PublishModelActivity(c.Request.Context(), "project", project.ID,
			string(requested), &snapshot, user.ID, slug, handler.origin(c))
		if err != nil {
			handler.serverError(c, err)
			return
		}
	}

	rows, err := handler.projectRows(c, user, project.ID)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if len(rows) == 0 {
		notFound(c)
		return
	}
	drf.Respond(c, http.StatusOK, fullProjectJSON(rows[0]))
}

// projectDestroy removes a project.
//
// The favourite clear names the project **twice** — as the entity and as the scope — so it only ever removes somebody's favourite of the project itself, not the favourites of the things inside it. The archive route clears those; deleting does not.
func (handler *Handler) projectDestroy(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireProjectBase(c, user, http.MethodDelete) {
		return
	}
	slug := c.Param("slug")
	project, found, err := handler.projectByID(c)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !found {
		notFound(c)
		return
	}
	now := handler.clock().UTC()
	err = handler.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		err := tx.Table("user_favorites").
			Where(`entity_type = 'project' AND entity_identifier = ? AND project_id = ? AND deleted_at IS NULL`,
				project.ID, project.ID).
			Update("deleted_at", now).Error
		if err != nil {
			return err
		}
		return tx.Table("projects").Where("id = ?", project.ID).Update("deleted_at", now).Error
	})
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if handler.tasks != nil {
		err = handler.tasks.PublishWebhookActivity(c.Request.Context(), "project", "deleted",
			user.ID, slug, handler.origin(c), project.ID)
		if err != nil {
			handler.serverError(c, err)
			return
		}
	}
	c.Status(http.StatusNoContent)
}

// projectRows reads a project through the annotated queryset the full serializer needs.
func (handler *Handler) projectRows(c *gin.Context, user *auth.User, projectID string) ([]projectRow, error) {
	var rows []projectRow
	err := handler.db.WithContext(c.Request.Context()).Table("projects p").
		Select(`p.*,
			(SELECT COUNT(*) FROM project_members tm
				JOIN users tu ON tu.id = tm.member_id AND tu.is_bot = FALSE
				WHERE tm.project_id = p.id AND tm.is_active = TRUE) AS total_members,
			(SELECT COUNT(*) FROM cycles tc WHERE tc.project_id = p.id) AS total_cycles,
			(SELECT COUNT(*) FROM modules tmo WHERE tmo.project_id = p.id) AS total_modules,
			EXISTS (SELECT 1 FROM project_members im
				JOIN workspaces iw ON iw.id = im.workspace_id
				WHERE im.project_id = p.id AND im.member_id = ? AND im.is_active = TRUE AND iw.slug = ?) AS is_member,
			(SELECT sm.sort_order FROM project_members sm
				JOIN workspaces sw ON sw.id = sm.workspace_id
				WHERE sm.project_id = p.id AND sm.member_id = ? AND sm.is_active = TRUE AND sw.slug = ? LIMIT 1) AS sort_order,
			(SELECT rm.role FROM project_members rm
				WHERE rm.project_id = p.id AND rm.member_id = ? AND rm.is_active = TRUE LIMIT 1) AS member_role,
			EXISTS (SELECT 1 FROM deploy_boards db
				JOIN workspaces dw ON dw.id = db.workspace_id
				WHERE db.project_id = p.id AND dw.slug = ?) AS is_deployed`,
			user.ID, c.Param("slug"), user.ID, c.Param("slug"), user.ID, c.Param("slug")).
		Joins("JOIN workspaces w ON w.id = p.workspace_id").
		Where("w.slug = ? AND p.id = ? AND p.deleted_at IS NULL", c.Param("slug"), projectID).
		Limit(1).Scan(&rows).Error
	return rows, err
}

// projectSnapshot is the body the activity task compares against, taken before anything is written.
func (handler *Handler) projectSnapshot(c *gin.Context, user *auth.User, projectID string) (string, error) {
	rows, err := handler.projectRows(c, user, projectID)
	if err != nil || len(rows) == 0 {
		return "", err
	}
	encoded, err := json.Marshal(fullProjectJSON(rows[0]))
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

// rowBelongsToProject reports whether a named row is in this project, which is what the two update validations ask.
func (handler *Handler) rowBelongsToProject(c *gin.Context, table, identifier, projectID string) (bool, error) {
	var count int64
	err := handler.db.WithContext(c.Request.Context()).Table(table).
		Where("id = ? AND project_id = ?", identifier, projectID).Count(&count).Error
	return count > 0, err
}

// requireProjectBase is ProjectBasePermission: a safe method wants any active workspace member, a create wants an admin or member of the workspace, and everything else wants a project admin — or a workspace admin who is in the project.
func (handler *Handler) requireProjectBase(c *gin.Context, user *auth.User, method string) bool {
	slug := c.Param("slug")
	workspaceRole, inWorkspace, err := handler.workspaceRole(c, user, slug)
	if err != nil {
		handler.serverError(c, err)
		return false
	}
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		if inWorkspace {
			return true
		}
	case http.MethodPost:
		if inWorkspace && (workspaceRole == roleAdmin || workspaceRole == roleMember) {
			return true
		}
	default:
		role, inProject, err := handler.projectRole(c, user)
		if err != nil {
			handler.serverError(c, err)
			return false
		}
		if inProject && role == roleAdmin {
			return true
		}
		if inProject && inWorkspace && workspaceRole == roleAdmin {
			return true
		}
	}
	c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
		"detail": "You do not have permission to perform this action.",
	})
	return false
}

func (handler *Handler) workspaceRole(c *gin.Context, user *auth.User, slug string) (int, bool, error) {
	var roles []int
	err := handler.db.WithContext(c.Request.Context()).Table("workspace_members wm").
		Joins("JOIN workspaces w ON w.id = wm.workspace_id").
		Where("w.slug = ? AND wm.member_id = ? AND wm.is_active = TRUE", slug, user.ID).
		Limit(1).Pluck("wm.role", &roles).Error
	if err != nil || len(roles) == 0 {
		return 0, false, err
	}
	return roles[0], true, nil
}

// origin is what the activity tasks record a change as having come from.
func (handler *Handler) origin(c *gin.Context) string {
	scheme := "https"
	if c.Request.TLS == nil && !strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https") {
		scheme = "http"
	}
	return scheme + "://" + c.Request.Host
}

// projectUpdates is the twenty-three writable fields, minus the one the handler writes itself.
func projectUpdates(payload map[string]any) map[string]any {
	updates := map[string]any{}
	for _, field := range []string{"name", "description", "identifier", "timezone", "cover_image",
		"external_source", "external_id", "emoji"} {
		if value, ok := payload[field].(string); ok {
			updates[field] = value
		}
	}
	for _, field := range []string{"cycle_view", "module_view", "issue_views_view", "page_view",
		"is_time_tracking_enabled", "is_issue_type_enabled", "guest_view_all_features"} {
		if value, ok := payload[field].(bool); ok {
			updates[field] = value
		}
	}
	for _, field := range []string{"archive_in", "close_in"} {
		if value, ok := payload[field].(float64); ok {
			updates[field] = int(value)
		}
	}
	for payloadName, column := range map[string]string{
		"default_assignee": "default_assignee_id", "project_lead": "project_lead_id",
		"default_state": "default_state_id", "estimate": "estimate_id",
	} {
		value, present := payload[payloadName]
		if !present {
			continue
		}
		if text, ok := value.(string); ok && text != "" {
			updates[column] = text
		} else {
			updates[column] = nil
		}
	}
	if value, present := payload["icon_prop"]; present {
		encoded, err := json.Marshal(value)
		if err == nil {
			updates["icon_prop"] = encoded
		}
	}
	return updates
}

// fullProjectJSON is the external API's ProjectSerializer, which asks for every field and adds eight of its own.
func fullProjectJSON(row projectRow) gin.H {
	return gin.H{
		"id": row.ID, "created_at": row.CreatedAt, "updated_at": row.UpdatedAt,
		"deleted_at": row.DeletedAt, "created_by": row.CreatedByID, "updated_by": row.UpdatedByID,
		"name": row.Name, "description": row.Description, "identifier": row.Identifier,
		"description_text": decodeJSON(row.DescriptionText), "description_html": decodeJSON(row.DescriptionHTML),
		"network": row.Network, "cover_image": row.CoverImage, "cover_image_url": coverImageURL(row.Project),
		"icon_prop": decodeJSON(row.IconProp), "emoji": row.Emoji, "logo_props": decodeJSON(row.LogoProps),
		"module_view": row.ModuleViewOn, "cycle_view": row.CycleView, "issue_views_view": row.IssueViewsView,
		"page_view": row.PageView, "intake_view": row.IntakeView,
		"is_time_tracking_enabled": row.TimeTrackingEnabled, "is_issue_type_enabled": row.IssueTypeEnabled,
		"guest_view_all_features": row.GuestViewAllFeatures,
		"archive_in":              row.ArchiveIn, "close_in": row.CloseIn,
		"archived_at": row.ArchivedAt, "timezone": row.Timezone,
		"external_source": row.ExternalSource, "external_id": row.ExternalID,
		"workspace": row.WorkspaceID, "default_assignee": row.DefaultAssigneeID,
		"project_lead": row.ProjectLeadID, "cover_image_asset": row.CoverAsset,
		"estimate": row.EstimateID, "default_state": row.DefaultStateID,
		"total_members": row.TotalMembers, "total_cycles": row.TotalCycles, "total_modules": row.TotalModules,
		"is_member": row.IsMember, "sort_order": row.SortOrder, "member_role": row.MemberRole,
		"is_deployed": row.IsDeployed,
	}
}
