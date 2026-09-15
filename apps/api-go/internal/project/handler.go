package project

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"github.com/yldm-tech/pace/apps/api-go/internal/drf"
	"gorm.io/gorm"
)

const (
	roleGuest  = 5
	roleMember = 15
	roleAdmin  = 20
)

// networkSecret is ProjectNetwork.SECRET; retrieve hides secret projects from
// non-members instead of reporting that they are not a member.
const networkSecret = 0

// intakeIssueStatusPending is IntakeIssueStatus.PENDING.
const intakeIssueStatusPending = -2

var uuidPattern = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// forbiddenIdentifierChars is Project.FORBIDDEN_IDENTIFIER_CHARS_PATTERN.
var forbiddenIdentifierChars = regexp.MustCompile(`^.*[&+,:;$^}{*=?@#|'<>.()%!-].*$`)

type Settings struct {
	AppBaseURL string
	WebURL     string
}

type TaskPublisher interface {
	PublishModelActivity(ctx context.Context, modelName, modelID string, requestedData any, currentInstance *string, actorID, slug, origin string) error
	PublishWebhookActivity(ctx context.Context, event, verb string, actorID, slug, currentSite, eventID string) error
	PublishRecentVisit(ctx context.Context, entityName, entityIdentifier, userID, projectID, slug string) error
	PublishSoftDeleteRelatedObjects(ctx context.Context, appLabel, modelName, instanceID string) error
	PublishProjectAddUserEmail(ctx context.Context, currentSite, projectMemberID, invitorID string) error
	PublishIssueActivity(ctx context.Context, keywords map[string]any) error
	PublishCrawlLinkTitle(ctx context.Context, linkID, url string) error
	PublishIssueDescriptionVersion(ctx context.Context, updatedIssue, issueID, userID string) error
}

type Handler struct {
	db       *gorm.DB
	sessions *auth.SessionManager
	settings Settings
	tasks    TaskPublisher
	cache    auth.CacheInvalidator
	clock    func() time.Time
}

func NewHandler(db *gorm.DB, sessions *auth.SessionManager, settings Settings) *Handler {
	return &Handler{db: db, sessions: sessions, settings: settings, clock: time.Now}
}

func (handler *Handler) SetTasks(publisher TaskPublisher) { handler.tasks = publisher }

// SetCache wires the Redis invalidator the label routes need, since Django
// drops the cached workspace label list when a project label changes.
func (handler *Handler) SetCache(invalidator auth.CacheInvalidator) { handler.cache = invalidator }

func (handler *Handler) Register(router gin.IRouter) {
	router.GET("/api/workspaces/:slug/projects/", handler.authenticated(handler.list))
	router.POST("/api/workspaces/:slug/projects/", handler.authenticated(handler.create))
	router.GET("/api/workspaces/:slug/projects/:id/", handler.authenticatedUUID(handler.retrieve))
	router.PATCH("/api/workspaces/:slug/projects/:id/", handler.authenticatedUUID(handler.partialUpdate))
	router.DELETE("/api/workspaces/:slug/projects/:id/", handler.authenticatedUUID(handler.destroy))
	handler.registerMemberRoutes(router)
	handler.registerLabelRoutes(router)
	handler.registerIssueInteractionRoutes(router)
	handler.registerIssueLinkRoutes(router)
	handler.registerIssueCommentRoutes(router)
	handler.registerCommentReactionRoutes(router)
	handler.registerIssueDetailRoutes(router)
	handler.registerSubIssueRoutes(router)
	handler.registerIssueRelationRoutes(router)
}

func (handler *Handler) authenticated(next func(*gin.Context, *auth.User)) gin.HandlerFunc {
	return func(c *gin.Context) {
		if handler.sessions == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"detail": "Authentication credentials were not provided."})
			return
		}
		user, _, err := handler.sessions.Authenticate(c.Request.Context(), c.Request, c.Writer)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"detail": "Authentication credentials were not provided."})
			return
		}
		next(c, user)
	}
}

func (handler *Handler) authenticatedUUID(next func(*gin.Context, *auth.User)) gin.HandlerFunc {
	authenticated := handler.authenticated(next)
	return func(c *gin.Context) {
		if !uuidPattern.MatchString(c.Param("id")) {
			handler.notFound(c)
			return
		}
		authenticated(c)
	}
}

// list is ProjectViewSet.list, the lightweight values() projection.
func (handler *Handler) list(c *gin.Context, user *auth.User) {
	if !handler.requireWorkspaceRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	slug := c.Param("slug")
	query := handler.db.WithContext(c.Request.Context()).Table("projects p").
		Select(`p.id, p.name, p.identifier, p.logo_props, p.archived_at, p.workspace_id,
			p.cycle_view, p.issue_views_view, p.module_view, p.page_view,
			p.intake_view AS inbox_view, p.guest_view_all_features, p.project_lead_id,
			p.network, p.created_at, p.updated_at, p.created_by_id, p.updated_by_id,
			(SELECT pm.role FROM project_members pm WHERE pm.project_id = p.id AND pm.member_id = ? AND pm.is_active = TRUE AND pm.deleted_at IS NULL LIMIT 1) AS member_role,
			(SELECT pup.sort_order FROM project_user_properties pup WHERE pup.user_id = ? AND pup.project_id = p.id AND pup.workspace_id = w.id AND pup.deleted_at IS NULL LIMIT 1) AS sort_order,
			(SELECT COUNT(*) FROM intake_issues ii WHERE ii.project_id = p.id AND ii.status = ? AND ii.deleted_at IS NULL) AS intake_count`,
			user.ID, user.ID, intakeIssueStatusPending).
		Joins("JOIN workspaces w ON w.id = p.workspace_id").
		Where("w.slug = ? AND p.deleted_at IS NULL", slug)

	query, ok := handler.applyVisibility(c, query, user, slug)
	if !ok {
		return
	}
	var rows []projectListRow
	if err := query.Scan(&rows).Error; err != nil {
		handler.internalError(c, err)
		return
	}
	response := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		response = append(response, projectListJSON(row))
	}
	drf.Respond(c, http.StatusOK, response)
}

// applyVisibility reproduces the guest and member narrowing both list routes
// apply on top of the workspace filter.
func (handler *Handler) applyVisibility(c *gin.Context, query *gorm.DB, user *auth.User, slug string) (*gorm.DB, bool) {
	role, err := handler.workspaceRole(c.Request.Context(), slug, user.ID)
	if err != nil {
		handler.internalError(c, err)
		return nil, false
	}
	memberOfProject := `EXISTS (SELECT 1 FROM project_members pm2 WHERE pm2.project_id = p.id AND pm2.member_id = ? AND pm2.is_active = TRUE AND pm2.deleted_at IS NULL)`
	if role == roleGuest {
		query = query.Where(memberOfProject, user.ID)
	}
	if role == roleMember {
		query = query.Where("("+memberOfProject+" OR p.network = 2)", user.ID)
	}
	return query, true
}

func (handler *Handler) retrieve(c *gin.Context, user *auth.User) {
	if !handler.requireWorkspaceRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	row, found, err := handler.projectRowByID(c.Request.Context(), c.Param("slug"), c.Param("id"), user.ID, true)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "Project does not exist"})
		return
	}
	members, err := handler.projectMemberIDsIncludingBots(c.Request.Context(), row.ID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !contains(members, user.ID) {
		if row.Network == networkSecret {
			c.JSON(http.StatusForbidden, gin.H{"error": "You do not have permission"})
			return
		}
		c.JSON(http.StatusConflict, gin.H{"error": "You are not a member of this project"})
		return
	}
	if handler.tasks != nil {
		err := handler.tasks.PublishRecentVisit(c.Request.Context(), "project", row.ID, user.ID, row.ID, c.Param("slug"))
		if err != nil {
			handler.internalError(c, err)
			return
		}
	}
	data, err := handler.projectJSON(c.Request.Context(), row)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, data)
}

func (handler *Handler) create(c *gin.Context, user *auth.User) {
	if !handler.requireWorkspaceRole(c, user, roleAdmin, roleMember) {
		return
	}
	slug := c.Param("slug")
	var workspace struct {
		ID       string `gorm:"column:id"`
		Timezone string `gorm:"column:timezone"`
	}
	err := handler.db.WithContext(c.Request.Context()).Table("workspaces").
		Where("slug = ? AND deleted_at IS NULL", slug).Take(&workspace).Error
	if err != nil {
		handler.notFound(c)
		return
	}
	body, raw, ok := handler.readBody(c)
	if !ok {
		return
	}
	fields, ok := handler.projectFields(c, body, false)
	if !ok {
		return
	}
	if !handler.validateProjectReferences(c, fields, workspace.ID, "") {
		return
	}
	projectID, err := newUUID()
	if err != nil {
		handler.internalError(c, err)
		return
	}
	now := handler.clock().UTC()
	project := Project{
		ID: projectID, CreatedAt: now, UpdatedAt: now, CreatedByID: &user.ID,
		WorkspaceID: workspace.ID, Network: 2, PageView: true, Timezone: workspace.Timezone,
		LogoProps: emptyJSON(),
	}
	fields.apply(&project)
	if !fields.hasTimezone {
		// Project.save copies the workspace timezone unless one was provided.
		project.Timezone = workspace.Timezone
	}
	project.Identifier = strings.ToUpper(strings.TrimSpace(project.Identifier))

	err = handler.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&project).Error; err != nil {
			return err
		}
		identifier := ProjectIdentifier{
			CreatedAt: now, UpdatedAt: now, CreatedByID: &user.ID,
			WorkspaceID: &workspace.ID, ProjectID: project.ID, Name: project.Identifier,
		}
		if err := tx.Create(&identifier).Error; err != nil {
			return err
		}
		if err := handler.addProjectAdmin(tx, project, user.ID, user.ID, now); err != nil {
			return err
		}
		if project.ProjectLeadID != nil && *project.ProjectLeadID != user.ID {
			if err := handler.addProjectAdmin(tx, project, *project.ProjectLeadID, user.ID, now); err != nil {
				return err
			}
		}
		return handler.createDefaultStates(tx, project, user.ID, now)
	})
	if err != nil {
		if isUniqueViolation(err) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "The payload is not valid"})
			return
		}
		handler.internalError(c, err)
		return
	}
	if handler.tasks != nil {
		err := handler.tasks.PublishModelActivity(c.Request.Context(), "project", project.ID, raw, nil, user.ID, slug, handler.origin())
		if err != nil {
			handler.internalError(c, err)
			return
		}
	}
	row, found, err := handler.projectRowByID(c.Request.Context(), slug, project.ID, user.ID, false)
	if err != nil || !found {
		handler.internalError(c, err)
		return
	}
	data, err := handler.projectJSON(c.Request.Context(), row)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusCreated, data)
}

func (handler *Handler) partialUpdate(c *gin.Context, user *auth.User) {
	slug, projectID := c.Param("slug"), c.Param("id")
	allowed, err := handler.workspaceOrProjectAdmin(c.Request.Context(), slug, projectID, user.ID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !allowed {
		c.JSON(http.StatusForbidden, gin.H{"error": "You don't have the required permissions."})
		return
	}
	var project Project
	err = handler.db.WithContext(c.Request.Context()).
		Where("id = ? AND workspace_id = (SELECT id FROM workspaces WHERE slug = ?) AND deleted_at IS NULL", projectID, slug).
		Take(&project).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		handler.notFound(c)
		return
	}
	if err != nil {
		handler.internalError(c, err)
		return
	}
	body, raw, ok := handler.readBody(c)
	if !ok {
		return
	}
	// Django reads inbox_view off the request and feeds it back in as
	// intake_view before validating, so the alias wins over the stored value.
	intakeView := project.IntakeView
	if value, exists := body["inbox_view"]; exists {
		if err := json.Unmarshal(value, &intakeView); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"intake_view": []string{"Must be a valid boolean."}})
			return
		}
	}
	snapshot, err := handler.projectDetailJSON(c.Request.Context(), project)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	currentInstance := string(encoded)

	if project.ArchivedAt != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Archived projects cannot be updated"})
		return
	}
	fields, ok := handler.projectFields(c, body, true)
	if !ok {
		return
	}
	if !handler.validateProjectReferences(c, fields, project.WorkspaceID, project.ID) {
		return
	}
	updated := project
	fields.apply(&updated)
	updated.IntakeView = intakeView
	updated.Identifier = strings.ToUpper(strings.TrimSpace(updated.Identifier))
	updated.UpdatedAt = handler.clock().UTC()
	updated.UpdatedByID = &user.ID

	err = handler.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(&updated).Error; err != nil {
			return err
		}
		if !intakeView {
			return nil
		}
		var count int64
		err := tx.Model(&Intake{}).Where("project_id = ? AND is_default = TRUE AND deleted_at IS NULL", updated.ID).Count(&count).Error
		if err != nil || count > 0 {
			return err
		}
		intakeID, err := newUUID()
		if err != nil {
			return err
		}
		return tx.Create(&Intake{
			ID: intakeID, CreatedAt: updated.UpdatedAt, UpdatedAt: updated.UpdatedAt, CreatedByID: &user.ID,
			ProjectID: updated.ID, WorkspaceID: updated.WorkspaceID,
			Name: updated.Name + " Intake", IsDefault: true,
			ViewProps: emptyJSON(), LogoProps: emptyJSON(),
		}).Error
	})
	if err != nil {
		if isUniqueViolation(err) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "The payload is not valid"})
			return
		}
		handler.internalError(c, err)
		return
	}
	if handler.tasks != nil {
		err := handler.tasks.PublishModelActivity(c.Request.Context(), "project", updated.ID, raw, &currentInstance, user.ID, slug, handler.origin())
		if err != nil {
			handler.internalError(c, err)
			return
		}
	}
	row, found, err := handler.projectRowByID(c.Request.Context(), slug, updated.ID, user.ID, false)
	if err != nil || !found {
		handler.internalError(c, err)
		return
	}
	data, err := handler.projectJSON(c.Request.Context(), row)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, data)
}

func (handler *Handler) destroy(c *gin.Context, user *auth.User) {
	slug, projectID := c.Param("slug"), c.Param("id")
	allowed, err := handler.workspaceOrProjectAdmin(c.Request.Context(), slug, projectID, user.ID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !allowed {
		c.JSON(http.StatusForbidden, gin.H{"error": "You don't have the required permissions."})
		return
	}
	var project Project
	err = handler.db.WithContext(c.Request.Context()).
		Where("id = ? AND workspace_id = (SELECT id FROM workspaces WHERE slug = ?) AND deleted_at IS NULL", projectID, slug).
		Take(&project).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		handler.notFound(c)
		return
	}
	if err != nil {
		handler.internalError(c, err)
		return
	}
	now := handler.clock().UTC()
	err = handler.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&Project{}).Where("id = ?", project.ID).Updates(map[string]any{
			"deleted_at": now, "updated_at": now, "updated_by_id": user.ID,
		}).Error; err != nil {
			return err
		}
		// Django soft-deletes the queryset, which writes deleted_at only.
		if err := tx.Table("deploy_boards").
			Where("project_id = ? AND workspace_id = (SELECT id FROM workspaces WHERE slug = ?) AND deleted_at IS NULL", project.ID, slug).
			Update("deleted_at", now).Error; err != nil {
			return err
		}
		return tx.Table("user_favorites").
			Where("project_id = ? AND workspace_id = (SELECT id FROM workspaces WHERE slug = ?) AND deleted_at IS NULL", project.ID, slug).
			Update("deleted_at", now).Error
	})
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if handler.tasks != nil {
		if err := handler.tasks.PublishSoftDeleteRelatedObjects(c.Request.Context(), "db", "project", project.ID); err != nil {
			handler.internalError(c, err)
			return
		}
		err := handler.tasks.PublishWebhookActivity(c.Request.Context(), "project", "deleted", user.ID, slug, handler.origin(), project.ID)
		if err != nil {
			handler.internalError(c, err)
			return
		}
	}
	c.Status(http.StatusNoContent)
}

// projectRowByID builds the annotated row ProjectViewSet.get_queryset returns.
func (handler *Handler) projectRowByID(ctx context.Context, slug, projectID, userID string, excludeArchived bool) (projectRow, bool, error) {
	query := handler.db.WithContext(ctx).Table("projects p").
		Select(`p.*,
			EXISTS (SELECT 1 FROM user_favorites uf WHERE uf.user_id = ? AND uf.entity_identifier = p.id AND uf.entity_type = 'project' AND uf.project_id = p.id AND uf.deleted_at IS NULL) AS is_favorite,
			(SELECT pm.role FROM project_members pm WHERE pm.project_id = p.id AND pm.member_id = ? AND pm.is_active = TRUE AND pm.deleted_at IS NULL LIMIT 1) AS member_role,
			(SELECT db.anchor FROM deploy_boards db WHERE db.entity_name = 'project' AND db.entity_identifier = p.id AND db.workspace_id = w.id AND db.deleted_at IS NULL LIMIT 1) AS anchor,
			(SELECT pup.sort_order FROM project_user_properties pup WHERE pup.user_id = ? AND pup.project_id = p.id AND pup.workspace_id = w.id AND pup.deleted_at IS NULL LIMIT 1) AS sort_order`,
			userID, userID, userID).
		Joins("JOIN workspaces w ON w.id = p.workspace_id").
		Where("w.slug = ? AND p.id = ? AND p.deleted_at IS NULL", slug, projectID)
	if excludeArchived {
		query = query.Where("p.archived_at IS NULL")
	}
	var rows []projectRow
	if err := query.Limit(1).Scan(&rows).Error; err != nil {
		return projectRow{}, false, err
	}
	if len(rows) == 0 {
		return projectRow{}, false, nil
	}
	return rows[0], true, nil
}

// addProjectAdmin reproduces ProjectMember.save, which seeds a
// ProjectUserProperty ordered ahead of the member's existing projects.
func (handler *Handler) addProjectAdmin(tx *gorm.DB, project Project, memberID, actorID string, now time.Time) error {
	var minimum *float64
	err := tx.Table("project_user_properties").
		Where("workspace_id = ? AND user_id = ? AND deleted_at IS NULL", project.WorkspaceID, memberID).
		Select("MIN(sort_order)").Scan(&minimum).Error
	if err != nil {
		return err
	}
	sortOrder := 65535.0
	if minimum != nil {
		sortOrder = *minimum - 10000
	}
	propertyID, err := newUUID()
	if err != nil {
		return err
	}
	property := ProjectUserProperty{
		ID: propertyID, CreatedAt: now, UpdatedAt: now, CreatedByID: &actorID,
		ProjectID: project.ID, WorkspaceID: project.WorkspaceID, UserID: memberID,
		Filters: defaultFiltersJSON(), DisplayFilters: defaultDisplayFiltersJSON(),
		DisplayProperties: defaultDisplayPropertiesJSON(), RichFilters: emptyJSON(),
		Preferences: defaultPreferencesJSON(), SortOrder: sortOrder,
	}
	if err := tx.Create(&property).Error; err != nil {
		return err
	}
	memberRowID, err := newUUID()
	if err != nil {
		return err
	}
	return tx.Create(&ProjectMember{
		ID: memberRowID, CreatedAt: now, UpdatedAt: now, CreatedByID: &actorID,
		ProjectID: project.ID, WorkspaceID: project.WorkspaceID, MemberID: memberID,
		Role: roleAdmin, ViewProps: defaultPropsJSON(), DefaultProps: defaultPropsJSON(),
		Preferences: defaultPreferencesJSON(), SortOrder: 65535, IsActive: true,
	}).Error
}

// createDefaultStates bulk-inserts DEFAULT_STATES. Django uses bulk_create, so
// State.save never runs and slug stays empty while sequence keeps the literal.
func (handler *Handler) createDefaultStates(tx *gorm.DB, project Project, actorID string, now time.Time) error {
	states := make([]State, 0, len(defaultStates))
	for _, definition := range defaultStates {
		stateID, err := newUUID()
		if err != nil {
			return err
		}
		states = append(states, State{
			ID: stateID, CreatedAt: now, UpdatedAt: now, CreatedByID: &actorID,
			ProjectID: project.ID, WorkspaceID: project.WorkspaceID,
			Name: definition.Name, Color: definition.Color, Sequence: definition.Sequence,
			Group: definition.Group, Default: definition.Default,
		})
	}
	return tx.Create(&states).Error
}

func (handler *Handler) workspaceRole(ctx context.Context, slug, userID string) (int, error) {
	var role *int
	err := handler.db.WithContext(ctx).Table("workspace_members wm").
		Joins("JOIN workspaces w ON w.id = wm.workspace_id").
		Where("w.slug = ? AND wm.member_id = ? AND wm.is_active = TRUE AND wm.deleted_at IS NULL", slug, userID).
		Select("wm.role").Limit(1).Scan(&role).Error
	if err != nil {
		return 0, err
	}
	if role == nil {
		return 0, nil
	}
	return *role, nil
}

func (handler *Handler) requireWorkspaceRole(c *gin.Context, user *auth.User, allowed ...int) bool {
	role, err := handler.workspaceRole(c.Request.Context(), c.Param("slug"), user.ID)
	if err != nil {
		handler.internalError(c, err)
		return false
	}
	for _, candidate := range allowed {
		if role == candidate {
			return true
		}
	}
	c.JSON(http.StatusForbidden, gin.H{"error": "You don't have the required permissions."})
	return false
}

func (handler *Handler) workspaceOrProjectAdmin(ctx context.Context, slug, projectID, userID string) (bool, error) {
	var workspaceAdmins int64
	err := handler.db.WithContext(ctx).Table("workspace_members wm").
		Joins("JOIN workspaces w ON w.id = wm.workspace_id").
		Where("w.slug = ? AND wm.member_id = ? AND wm.role = ? AND wm.is_active = TRUE AND wm.deleted_at IS NULL", slug, userID, roleAdmin).
		Count(&workspaceAdmins).Error
	if err != nil {
		return false, err
	}
	if workspaceAdmins > 0 {
		return true, nil
	}
	var projectAdmins int64
	err = handler.db.WithContext(ctx).Table("project_members pm").
		Joins("JOIN workspaces w ON w.id = pm.workspace_id").
		Where("w.slug = ? AND pm.member_id = ? AND pm.project_id = ? AND pm.role = ? AND pm.is_active = TRUE AND pm.deleted_at IS NULL", slug, userID, projectID, roleAdmin).
		Count(&projectAdmins).Error
	return projectAdmins > 0, err
}

// projectMemberIDsIncludingBots backs the membership check in retrieve, which
// reads the prefetched member list before the bot filter is applied.
func (handler *Handler) projectMemberIDsIncludingBots(ctx context.Context, projectID string) ([]string, error) {
	members := make([]string, 0)
	err := handler.db.WithContext(ctx).Table("project_members").
		Where("project_id = ? AND is_active = TRUE AND deleted_at IS NULL", projectID).
		Pluck("member_id", &members).Error
	return members, err
}

func (handler *Handler) readBody(c *gin.Context) (map[string]json.RawMessage, any, bool) {
	var body map[string]json.RawMessage
	if err := c.ShouldBindJSON(&body); err != nil {
		handler.invalidDetail(c)
		return nil, nil, false
	}
	raw := make(map[string]any, len(body))
	for key, value := range body {
		var decoded any
		if json.Unmarshal(value, &decoded) == nil {
			raw[key] = decoded
		}
	}
	return body, raw, true
}

func (handler *Handler) origin() string {
	if handler.settings.WebURL != "" {
		return handler.settings.WebURL
	}
	return handler.settings.AppBaseURL
}

func (handler *Handler) notFound(c *gin.Context) {
	c.JSON(http.StatusNotFound, gin.H{"error": "The required object does not exist."})
}

func (handler *Handler) invalidDetail(c *gin.Context) {
	c.JSON(http.StatusBadRequest, gin.H{"error": "Please provide valid detail"})
}

func (handler *Handler) internalError(c *gin.Context, err error) {
	if err != nil {
		c.Error(err)
	}
	c.JSON(http.StatusInternalServerError, gin.H{"error": "Something went wrong please try again later"})
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func isUniqueViolation(err error) bool {
	lowered := strings.ToLower(err.Error())
	return strings.Contains(lowered, "duplicate key") || strings.Contains(lowered, "unique constraint")
}
