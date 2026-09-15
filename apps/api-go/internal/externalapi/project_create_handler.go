package externalapi

import (
	"encoding/json"
	"math/rand"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"github.com/yldm-tech/pace/apps/api-go/internal/drf"
	"github.com/yldm-tech/pace/apps/api-go/internal/projects"
	"gorm.io/gorm"
)

func (handler *Handler) registerProjectListCreateRoutes(router gin.IRouter) {
	const base = "/api/v1/workspaces/:slug/projects/"
	router.GET(base, handler.authenticated(handler.projectList))
	router.POST(base, handler.authenticated(handler.projectCreate))
}

// projectList returns the projects of a workspace the caller can see: the ones they are in, and every public one.
func (handler *Handler) projectList(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireProjectBase(c, user, c.Request.Method) {
		return
	}
	order := sanitizeOrderBy(c.Query("order_by"), projectOrderByAllowlist, "sort_order")
	descending := strings.HasPrefix(order, "-")
	column := strings.TrimPrefix(order, "-")
	if column == "sort_order" {
		// The ordering is the caller's own place in the list rather than a column of the project.
		column = "sort_order"
	} else {
		column = "p." + column
	}
	clause := column + " ASC"
	if descending {
		clause = column + " DESC"
	}

	var rows []projectRow
	err := handler.db.WithContext(c.Request.Context()).Table("projects p").
		Select(externalProjectAnnotations(),
			user.ID, c.Param("slug"), user.ID, c.Param("slug"), user.ID, c.Param("slug")).
		Joins("JOIN workspaces w ON w.id = p.workspace_id").
		Where("w.slug = ? AND p.deleted_at IS NULL", c.Param("slug")).
		Where(`EXISTS (SELECT 1 FROM project_members pm WHERE pm.project_id = p.id
			AND pm.member_id = ? AND pm.is_active = TRUE) OR p.network = 2`, user.ID).
		Order(clause).Scan(&rows).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	fields := requestedFields(c)
	results := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		results = append(results, narrow(fullProjectJSON(row), fields))
	}
	handler.respondPaged(c, results)
}

// projectCreate makes a project, its six workflow states and its first memberships.
//
// The identifier is upper-cased and trimmed before anything is written, which is the model's doing rather than the serializer's.
func (handler *Handler) projectCreate(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireProjectBase(c, user, c.Request.Method) {
		return
	}
	slug := c.Param("slug")
	var workspaces []struct {
		ID       string `gorm:"column:id"`
		Timezone string `gorm:"column:timezone"`
	}
	err := handler.db.WithContext(c.Request.Context()).Table("workspaces").
		Where("slug = ?", slug).Limit(1).Scan(&workspaces).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if len(workspaces) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Workspace does not exist"})
		return
	}
	workspace := workspaces[0]

	var body map[string]json.RawMessage
	if err := c.ShouldBindJSON(&body); err != nil {
		invalidPayload(c)
		return
	}
	values, failures := handler.validateExternalProject(c, body, workspace.ID)
	if failures != nil {
		c.JSON(http.StatusBadRequest, failures)
		return
	}

	identifier, _ := values["identifier"].(string)
	if identifier == "" {
		c.JSON(http.StatusBadRequest, gin.H{"non_field_errors": []string{"Project Identifier is required"}})
		return
	}
	// The identifier table is read but never written by this endpoint, so it only ever holds what the other API put there.
	taken, err := handler.rowExists(c, "project_identifiers", "name = ? AND workspace_id = ?", identifier, workspace.ID)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if taken {
		c.JSON(http.StatusConflict, gin.H{"identifier": "The project identifier is already taken"})
		return
	}

	now := handler.clock().UTC()
	projectID, err := newUUID()
	if err != nil {
		handler.serverError(c, err)
		return
	}
	values["id"] = projectID
	values["created_at"] = now
	values["updated_at"] = now
	values["created_by_id"] = user.ID
	values["updated_by_id"] = user.ID
	values["workspace_id"] = workspace.ID
	if _, given := values["timezone"]; !given {
		// A project with no timezone of its own takes the workspace's, which the model reads on the way in.
		values["timezone"] = workspace.Timezone
	}
	if _, given := values["logo_props"]; !given {
		values["logo_props"] = randomProjectLogo()
	}

	var lead string
	if value, given := values["project_lead_id"].(string); given {
		lead = value
	}
	err = handler.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Table("projects").Create(values).Error; err != nil {
			return err
		}
		if err := projects.AddMember(tx, projectID, workspace.ID, user.ID, user.ID, roleAdmin, now, newUUID); err != nil {
			return err
		}
		// A lead who is not the caller is made an administrator too.
		if lead != "" && lead != user.ID {
			err := projects.AddMember(tx, projectID, workspace.ID, lead, user.ID, roleAdmin, now, newUUID)
			if err != nil {
				return err
			}
		}
		return projects.CreateDefaultStates(tx, projectID, workspace.ID, user.ID, now, newUUID)
	})
	if err != nil {
		if isUniqueViolation(err) {
			// Both unique constraints answer with the same message, because Django reads the database's own wording and cannot tell them apart.
			c.JSON(http.StatusConflict, gin.H{"name": "The project name is already taken"})
			return
		}
		handler.serverError(c, err)
		return
	}

	if handler.tasks != nil {
		err := handler.tasks.PublishModelActivity(c.Request.Context(), "project", projectID,
			decodeRawBody(body), nil, user.ID, slug, handler.origin(c))
		if err != nil {
			handler.serverError(c, err)
			return
		}
	}
	rows, err := handler.projectRows(c, user, projectID)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if len(rows) == 0 {
		handler.serverError(c, errProjectVanished)
		return
	}
	drf.Respond(c, http.StatusCreated, fullProjectJSON(rows[0]))
}

var errProjectVanished = &orderError{"the project could not be read back after it was written"}

// externalProjectAnnotations is what the list and the detail both select.
func externalProjectAnnotations() string {
	return `p.*,
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
			WHERE db.project_id = p.id AND dw.slug = ?) AS is_deployed`
}

// forbiddenProjectChars is FORBIDDEN_IDENTIFIER_CHARS_PATTERN, which both the name and the identifier are held to.
var forbiddenProjectChars = regexp.MustCompile(`[&+,:;$^}{*=?@#|'<>.()%!-]`)

// validateExternalProject is ProjectCreateSerializer.
func (handler *Handler) validateExternalProject(c *gin.Context, body map[string]json.RawMessage, workspaceID string) (map[string]any, gin.H) {
	values := map[string]any{}
	errors := gin.H{}

	for _, field := range []struct {
		name     string
		limit    int
		required bool
	}{
		{"name", 255, true}, {"description", 0, false}, {"identifier", 12, true},
		{"emoji", 255, false}, {"cover_image", 800, false},
		{"external_source", 255, false}, {"external_id", 255, false},
	} {
		raw, given := body[field.name]
		if !given {
			if field.required {
				errors[field.name] = []string{"This field is required."}
			}
			continue
		}
		if string(raw) == "null" && !field.required {
			values[field.name] = nil
			continue
		}
		value, failure := charField(raw, field.limit)
		if failure != nil {
			errors[field.name] = failure
			continue
		}
		if field.required && *value == "" {
			errors[field.name] = []string{"This field may not be blank."}
			continue
		}
		values[field.name] = *value
	}
	for _, field := range []string{
		"module_view", "cycle_view", "issue_views_view", "page_view", "intake_view",
		"guest_view_all_features", "is_issue_type_enabled", "is_time_tracking_enabled",
	} {
		raw, given := body[field]
		if !given {
			continue
		}
		if value, failure := booleanField(raw); failure != nil {
			errors[field] = failure
		} else if value != nil {
			values[field] = *value
		}
	}
	for _, field := range []string{"archive_in", "close_in"} {
		raw, given := body[field]
		if !given {
			continue
		}
		// Both are bounded at a year, which is what the model's validators ask for.
		if value, failure := integerField(raw, 0, 12); failure != nil {
			errors[field] = failure
		} else if value != nil {
			values[field] = *value
		}
	}
	if raw, given := body["icon_prop"]; given {
		if !json.Valid(raw) {
			errors["icon_prop"] = []string{"Value must be valid JSON."}
		} else {
			values["icon_prop"] = jsonValue(raw)
		}
	}
	if raw, given := body["timezone"]; given {
		value, failure := charField(raw, 255)
		switch {
		case failure != nil:
			errors["timezone"] = failure
		default:
			if !validTimezone(*value) {
				errors["timezone"] = []string{`"` + *value + `" is not a valid choice.`}
			} else {
				values["timezone"] = *value
			}
		}
	}
	for _, relation := range []struct{ field, column string }{
		{"project_lead", "project_lead_id"}, {"default_assignee", "default_assignee_id"},
	} {
		raw, given := body[relation.field]
		if !given {
			continue
		}
		value, failure := primaryKeyField(raw)
		if failure != nil {
			errors[relation.field] = failure
			continue
		}
		if value == nil {
			values[relation.column] = nil
			continue
		}
		values[relation.column] = *value
	}
	if len(errors) > 0 {
		return nil, errors
	}

	if name, given := values["name"].(string); given && forbiddenProjectChars.MatchString(name) {
		return nil, gin.H{"non_field_errors": []string{"Project name cannot contain special characters."}}
	}
	if identifier, given := values["identifier"].(string); given {
		if forbiddenProjectChars.MatchString(identifier) {
			return nil, gin.H{"non_field_errors": []string{"Project identifier cannot contain special characters."}}
		}
		// The model upper-cases and trims the identifier on its way in, whatever the caller sent.
		values["identifier"] = strings.ToUpper(strings.TrimSpace(identifier))
	}
	// The lead has to be an **active** member of the workspace, and the error is reported under its own key. The default assignee only has to be a member at all, and its error is a non-field one — two checks that read alike and are not alike.
	if lead, given := values["project_lead_id"].(string); given {
		member, err := handler.workspaceMember(c, workspaceID, lead, true)
		if err != nil {
			handler.serverError(c, err)
			return nil, gin.H{}
		}
		if !member {
			return nil, gin.H{"project_lead": []string{"The provided user is not a member of this workspace."}}
		}
	}
	if assignee, given := values["default_assignee_id"].(string); given {
		member, err := handler.workspaceMember(c, workspaceID, assignee, false)
		if err != nil {
			handler.serverError(c, err)
			return nil, gin.H{}
		}
		if !member {
			return nil, gin.H{"non_field_errors": []string{"Default assignee should be a user in the workspace"}}
		}
	}
	return values, nil
}

func (handler *Handler) workspaceMember(c *gin.Context, workspaceID, memberID string, active bool) (bool, error) {
	query := handler.db.WithContext(c.Request.Context()).Table("workspace_members").
		Where("workspace_id = ? AND member_id = ?", workspaceID, memberID)
	if active {
		query = query.Where("is_active = TRUE")
	}
	var count int64
	err := query.Count(&count).Error
	return count > 0, err
}

// validTimezone is the choice check the timezone field carries, which is the zone database rather than a list of our own.
func validTimezone(value string) bool {
	_, err := time.LoadLocation(value)
	return err == nil
}

// projectLogoIcons and projectLogoColors are the two lists a project's icon is drawn from when the caller names none.
var projectLogoIcons = []string{
	"home", "apps", "settings", "star", "favorite", "done", "check_circle", "add_task",
	"create_new_folder", "dataset", "terminal", "key", "rocket", "public", "quiz", "mood",
	"gavel", "eco", "diamond", "forest", "bolt", "sync", "cached", "library_add",
	"view_timeline", "view_kanban", "empty_dashboard", "cycle",
}

var projectLogoColors = []string{
	"#95999f", "#6d7b8a", "#5e6ad2", "#02b5ed", "#02b55c", "#f2be02", "#e57a00", "#f38e82",
}

// randomProjectLogo picks an icon the way the serializer does, which is at random rather than from anything about the project.
func randomProjectLogo() any {
	logo := map[string]any{
		"in_use": "icon",
		"icon": map[string]any{
			"name":  projectLogoIcons[rand.Intn(len(projectLogoIcons))],
			"color": projectLogoColors[rand.Intn(len(projectLogoColors))],
		},
	}
	encoded, err := json.Marshal(logo)
	if err != nil {
		return jsonValue([]byte(`{}`))
	}
	return jsonValue(encoded)
}
