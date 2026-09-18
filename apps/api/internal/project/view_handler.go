package project

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api/internal/access"
	"github.com/yldm-tech/pace/apps/api/internal/auth"
	"github.com/yldm-tech/pace/apps/api/internal/drf"
	"gorm.io/gorm"
)

// newIssueView is a view with every NOT NULL column already holding what Django's model default would have put there.
//
// The six jsonb columns are backed by []byte fields, and a nil one is written as null rather than as an empty object — which is why creating any view at all failed on filters. The two display objects are not empty: their defaults are the whole shape the editor expects.
func newIssueView() IssueView {
	const displayFilters = `{"group_by": null, "order_by": "-created_at", "type": null, "sub_issue": true, "show_empty_groups": true, "layout": "list", "calendar_date_range": ""}`
	const displayProperties = `{"assignee": true, "attachment_count": true, "created_on": true, "due_date": true, "estimate": true, "key": true, "labels": true, "link": true, "priority": true, "start_date": true, "state": true, "sub_issue_count": true, "updated_on": true}`
	return IssueView{
		Query: []byte("{}"), Filters: []byte("{}"), RichFilters: []byte("{}"), LogoProps: []byte("{}"),
		DisplayFilters: []byte(displayFilters), DisplayProperties: []byte(displayProperties),
	}
}

func (handler *Handler) registerViewRoutes(router gin.IRouter) {
	router.GET("/api/workspaces/:slug/projects/:id/views/", handler.authenticated(handler.viewList))
	router.POST("/api/workspaces/:slug/projects/:id/views/", handler.authenticated(handler.viewCreate))
	router.GET("/api/workspaces/:slug/projects/:id/views/:view/", handler.authenticated(handler.viewRetrieve))
	router.PATCH("/api/workspaces/:slug/projects/:id/views/:view/", handler.authenticated(handler.viewUpdate))
	router.PUT("/api/workspaces/:slug/projects/:id/views/:view/", handler.authenticated(handler.fullUpdate("view", handler.viewUpdate)))
	router.DELETE("/api/workspaces/:slug/projects/:id/views/:view/", handler.authenticated(handler.viewDestroy))
	router.GET("/api/workspaces/:slug/projects/:id/user-favorite-views/", handler.authenticated(handler.viewFavoriteList))
	router.POST("/api/workspaces/:slug/projects/:id/user-favorite-views/", handler.authenticated(handler.viewFavoriteCreate))
	router.DELETE("/api/workspaces/:slug/projects/:id/user-favorite-views/:view/", handler.authenticated(handler.viewFavoriteDestroy))
}

// IssueView is the db.IssueView table, a saved set of filters over a project's issues.
type IssueView struct {
	ID                string     `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt         time.Time  `gorm:"column:created_at"`
	UpdatedAt         time.Time  `gorm:"column:updated_at"`
	CreatedByID       *string    `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID       *string    `gorm:"column:updated_by_id;type:uuid"`
	DeletedAt         *time.Time `gorm:"column:deleted_at"`
	ProjectID         *string    `gorm:"column:project_id;type:uuid"`
	WorkspaceID       string     `gorm:"column:workspace_id;type:uuid"`
	Name              string     `gorm:"column:name"`
	Description       string     `gorm:"column:description"`
	Query             []byte     `gorm:"column:query;type:jsonb"`
	Filters           []byte     `gorm:"column:filters;type:jsonb"`
	DisplayFilters    []byte     `gorm:"column:display_filters;type:jsonb"`
	DisplayProperties []byte     `gorm:"column:display_properties;type:jsonb"`
	RichFilters       []byte     `gorm:"column:rich_filters;type:jsonb"`
	Access            int        `gorm:"column:access"`
	SortOrder         float64    `gorm:"column:sort_order"`
	LogoProps         []byte     `gorm:"column:logo_props;type:jsonb"`
	OwnedByID         string     `gorm:"column:owned_by_id;type:uuid"`
	IsLocked          bool       `gorm:"column:is_locked"`
	ArchivedAt        *time.Time `gorm:"column:archived_at"`
}

func (IssueView) TableName() string { return "issue_views" }

// viewRow is the view with the favourite flag the list annotates on.
type viewRow struct {
	IssueView
	IsFavorite bool `gorm:"column:is_favorite"`
}

// viewList returns the project's views: the caller's own, plus every public one.
//
// A guest sees only their own unless the project has been opened up with guest_view_all_features, which is a second narrowing on top of the access rule rather than a replacement for it.
func (handler *Handler) viewList(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	slug, projectID := c.Param("slug"), c.Param("id")

	restricted, err := handler.viewsRestrictedToOwner(c, user, slug, projectID)
	if err != nil {
		handler.internalError(c, err)
		return
	}

	query := handler.viewScope(c, user, slug, projectID)
	if restricted {
		query = query.Where("v.owned_by_id = ?", user.ID)
	}
	var rows []viewRow
	if err := query.Order("is_favorite DESC, v.name").Scan(&rows).Error; err != nil {
		handler.internalError(c, err)
		return
	}

	fields := requestedFields(c.Query("fields"))
	results := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		results = append(results, narrowToFields(viewJSON(row), fields))
	}
	drf.Respond(c, http.StatusOK, results)
}

// viewScope is the queryset every view route reads through: the project's live views the caller is allowed to see.
func (handler *Handler) viewScope(c *gin.Context, user *auth.User, slug, projectID string) *gorm.DB {
	return handler.db.WithContext(c.Request.Context()).Table("issue_views v").
		Select(`v.*, EXISTS (SELECT 1 FROM user_favorites uf
			JOIN workspaces uw ON uw.id = uf.workspace_id
			WHERE uf.user_id = ? AND uf.entity_identifier = v.id AND uf.entity_type = 'view'
			AND uf.project_id = ? AND uw.slug = ? AND uf.deleted_at IS NULL) AS is_favorite`, user.ID, projectID, slug).
		Joins("JOIN workspaces w ON w.id = v.workspace_id").
		Joins("JOIN projects p ON p.id = v.project_id AND p.archived_at IS NULL").
		Joins(access.MemberJoin("v", "project_id"), user.ID).
		Where("w.slug = ? AND v.project_id = ? AND v.deleted_at IS NULL", slug, projectID).
		// Access 1 is public and 0 is private, and a private view is visible to the person who made it.
		Where("v.owned_by_id = ? OR v.access = 1", user.ID)
}

// viewsRestrictedToOwner reports whether this caller only gets to see their own views, which is true of a guest in a project that has not been opened up.
func (handler *Handler) viewsRestrictedToOwner(c *gin.Context, user *auth.User, slug, projectID string) (bool, error) {
	var project Project
	err := handler.db.WithContext(c.Request.Context()).Where("id = ?", projectID).Take(&project).Error
	if err != nil {
		return false, err
	}
	if project.GuestViewAllFeatures {
		return false, nil
	}
	var guests int64
	err = handler.db.WithContext(c.Request.Context()).Table("project_members pm").
		Joins("JOIN workspaces w ON w.id = pm.workspace_id").
		Where("w.slug = ? AND pm.project_id = ? AND pm.member_id = ? AND pm.role = ? AND pm.is_active = TRUE",
			slug, projectID, user.ID, roleGuest).
		Count(&guests).Error
	return guests > 0, err
}

// viewRetrieve returns one view.
//
// The guest rule is checked after the view is read rather than folded into the query, so a guest asking for someone else's view is told no instead of being told it does not exist.
func (handler *Handler) viewRetrieve(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	slug, projectID, viewID := c.Param("slug"), c.Param("id"), c.Param("view")

	var rows []viewRow
	err := handler.viewScope(c, user, slug, projectID).Where("v.id = ?", viewID).Limit(1).Scan(&rows).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	restricted, err := handler.viewsRestrictedToOwner(c, user, slug, projectID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if len(rows) == 0 {
		// Django reads the owner off the result of .first() with no guard, so a view the caller cannot see raises rather than answering 404 — but only for a caller the guest rule applies to, since the expression short-circuits before it gets there.
		if restricted {
			handler.internalError(c, errors.New("project views: the view does not exist"))
			return
		}
		drf.Respond(c, http.StatusOK, gin.H{})
		return
	}
	if restricted && rows[0].OwnedByID != user.ID {
		c.JSON(http.StatusForbidden, gin.H{"error": "You are not allowed to view this issue"})
		return
	}
	if handler.tasks != nil {
		err = handler.tasks.PublishRecentVisit(c.Request.Context(), "view", viewID, user.ID, projectID, slug)
		if err != nil {
			handler.internalError(c, err)
			return
		}
	}
	drf.Respond(c, http.StatusOK, viewJSON(rows[0]))
}

// viewCreate saves a new view. Its sort order is placed after every other view in the project, and its query is derived from the filters rather than accepted from the caller.
func (handler *Handler) viewCreate(c *gin.Context, user *auth.User) {
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

	view := newIssueView()
	view.ProjectID = &projectID
	view.WorkspaceID = project.WorkspaceID
	view.OwnedByID = user.ID
	view.CreatedByID = &user.ID
	view.UpdatedByID = &user.ID
	applyViewPayload(&view, payload)

	identifier, err := newUUID()
	if err != nil {
		handler.internalError(c, err)
		return
	}
	now := handler.clock().UTC()
	view.ID, view.CreatedAt, view.UpdatedAt = identifier, now, now
	view.SortOrder, err = handler.nextViewSortOrder(c, projectID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	view.Query, err = viewQueryJSON(view.Filters, now)
	if err != nil {
		handler.internalError(c, err)
		return
	}

	if err := handler.db.WithContext(c.Request.Context()).Create(&view).Error; err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusCreated, viewJSON(viewRow{IssueView: view}))
}

// viewUpdate edits a view. A locked view refuses, and so does one the caller does not own — the owner rule is a plain 400 rather than a permission failure.
func (handler *Handler) viewUpdate(c *gin.Context, user *auth.User) {
	slug, projectID, viewID := c.Param("slug"), c.Param("id"), c.Param("view")

	var payload map[string]any
	if err := c.ShouldBindJSON(&payload); err != nil {
		handler.invalidDetail(c)
		return
	}

	var view IssueView
	err := handler.db.WithContext(c.Request.Context()).Table("issue_views v").Select("v.*").
		Joins("JOIN workspaces w ON w.id = v.workspace_id").
		Where("w.slug = ? AND v.project_id = ? AND v.id = ? AND v.deleted_at IS NULL", slug, projectID, viewID).
		Take(&view).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "The required object does not exist."})
		return
	}
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !handler.requireViewCreatorOrRoles(c, user, view.CreatedByID) {
		return
	}
	if view.IsLocked {
		c.JSON(http.StatusBadRequest, gin.H{"error": "view is locked"})
		return
	}
	if view.OwnedByID != user.ID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Only the owner of the view can update the view"})
		return
	}

	applyViewPayload(&view, payload)
	now := handler.clock().UTC()
	view.UpdatedAt, view.UpdatedByID = now, &user.ID
	// The model recomputes the query on every save from whatever filters the instance now holds, so an update that does not mention filters clears the stored query along with them.
	view.Query, err = viewQueryJSON(view.Filters, now)
	if err != nil {
		handler.internalError(c, err)
		return
	}

	err = handler.db.WithContext(c.Request.Context()).Model(&IssueView{}).Where("id = ?", view.ID).
		Updates(map[string]any{
			"name": view.Name, "description": view.Description,
			"filters": view.Filters, "display_filters": view.DisplayFilters,
			"display_properties": view.DisplayProperties, "rich_filters": view.RichFilters,
			"logo_props": view.LogoProps, "sort_order": view.SortOrder, "query": view.Query,
			"updated_at": view.UpdatedAt, "updated_by_id": view.UpdatedByID,
		}).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, viewJSON(viewRow{IssueView: view}))
}

// viewDestroy removes a view, its favourites and its recent-visit rows. Only an admin or the owner may, and anyone else is told so with a 400.
func (handler *Handler) viewDestroy(c *gin.Context, user *auth.User) {
	slug, projectID, viewID := c.Param("slug"), c.Param("id"), c.Param("view")

	var view IssueView
	err := handler.db.WithContext(c.Request.Context()).Table("issue_views v").Select("v.*").
		Joins("JOIN workspaces w ON w.id = v.workspace_id").
		Where("w.slug = ? AND v.project_id = ? AND v.id = ? AND v.deleted_at IS NULL", slug, projectID, viewID).
		Take(&view).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "The required object does not exist."})
		return
	}
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !handler.requireViewCreatorOrRoles(c, user, view.CreatedByID, roleAdmin) {
		return
	}

	member, _, err := handler.activeProjectMember(c.Request.Context(), slug, projectID, user.ID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if member.Role != roleAdmin && view.OwnedByID != user.ID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Only admin or owner can delete the view"})
		return
	}

	now := handler.clock().UTC()
	database := handler.db.WithContext(c.Request.Context())
	if err := database.Model(&IssueView{}).Where("id = ?", view.ID).Update("deleted_at", now).Error; err != nil {
		handler.internalError(c, err)
		return
	}
	err = database.Model(&UserFavorite{}).
		Where(`project_id = ? AND entity_type = 'view' AND entity_identifier = ? AND deleted_at IS NULL
			AND workspace_id = (SELECT id FROM workspaces WHERE slug = ?)`, projectID, viewID, slug).
		Update("deleted_at", now).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	// The recent visits go for good rather than being marked deleted, which is what delete(soft=False) does.
	err = database.Exec(`DELETE FROM user_recent_visits
		WHERE project_id = ? AND entity_name = 'view' AND entity_identifier = ?
		AND workspace_id = (SELECT id FROM workspaces WHERE slug = ?)`, projectID, viewID, slug).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (handler *Handler) viewFavoriteList(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember) {
		return
	}
	handler.internalError(c, errFavouriteListHasNoSerializer)
}

func (handler *Handler) viewFavoriteCreate(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember) {
		return
	}
	var request struct {
		View *string `json:"view"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		handler.invalidDetail(c)
		return
	}
	if !handler.createFavorite(c, user, "view", request.View) {
		return
	}
	c.Status(http.StatusNoContent)
}

func (handler *Handler) viewFavoriteDestroy(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember) {
		return
	}
	handler.destroyFavorite(c, user, "view", c.Param("view"))
}

// applyViewPayload writes the writable fields. The query, the owner, the access and the lock are not among them: the serializer marks all four read-only, so a caller naming them changes nothing.
func applyViewPayload(view *IssueView, payload map[string]any) {
	if value, ok := payload["name"].(string); ok {
		view.Name = value
	}
	if value, ok := payload["description"].(string); ok {
		view.Description = value
	}
	if value, ok := payload["sort_order"].(float64); ok {
		view.SortOrder = value
	}
	for key, target := range map[string]*[]byte{
		"filters":            &view.Filters,
		"display_filters":    &view.DisplayFilters,
		"display_properties": &view.DisplayProperties,
		"rich_filters":       &view.RichFilters,
		"logo_props":         &view.LogoProps,
	} {
		value, present := payload[key]
		if !present {
			continue
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			continue
		}
		*target = encoded
	}
}

// nextViewSortOrder places a new view after every other view in the project. A project with none leaves the model default alone.
func (handler *Handler) nextViewSortOrder(c *gin.Context, projectID string) (float64, error) {
	// MAX over no rows is one row holding null rather than no rows at all, and neither a float64 nor a *float64 survives Pluck scanning that. A nullable scan target is what does.
	var largest sql.NullFloat64
	err := handler.db.WithContext(c.Request.Context()).Table("issue_views").
		Where("project_id = ? AND deleted_at IS NULL", projectID).
		Select("MAX(sort_order)").Row().Scan(&largest)
	if err != nil {
		return 0, err
	}
	if !largest.Valid {
		return defaultViewSortOrder, nil
	}
	return largest.Float64 + 10000, nil
}

const defaultViewSortOrder = 65535

// viewJSON is IssueViewSerializer over one row.
func viewJSON(row viewRow) gin.H {
	return gin.H{
		"id": row.ID, "created_at": row.CreatedAt, "updated_at": row.UpdatedAt,
		"created_by": row.CreatedByID, "updated_by": row.UpdatedByID, "deleted_at": row.DeletedAt,
		"name": row.Name, "description": row.Description,
		"query": decodeJSON(row.Query), "filters": decodeJSON(row.Filters),
		"display_filters": decodeJSON(row.DisplayFilters), "display_properties": decodeJSON(row.DisplayProperties),
		"rich_filters": decodeJSON(row.RichFilters), "logo_props": decodeJSON(row.LogoProps),
		"access": row.Access, "sort_order": row.SortOrder, "is_locked": row.IsLocked,
		"archived_at": row.ArchivedAt, "owned_by": row.OwnedByID,
		"project": row.ProjectID, "workspace": row.WorkspaceID,
		"is_favorite": row.IsFavorite,
	}
}

// requestedFields is the fields query parameter, which DynamicBaseSerializer uses to narrow what it renders. An empty entry is dropped, so a trailing comma does not ask for a field named "".
func requestedFields(raw string) []string {
	fields := []string{}
	for _, field := range strings.Split(raw, ",") {
		if field != "" {
			fields = append(fields, field)
		}
	}
	return fields
}

// narrowToFields keeps only what was asked for, and keeps everything when nothing was.
func narrowToFields(data gin.H, fields []string) gin.H {
	if len(fields) == 0 {
		return data
	}
	narrowed := gin.H{}
	for _, field := range fields {
		if value, present := data[field]; present {
			narrowed[field] = value
		}
	}
	return narrowed
}

// requireViewCreatorOrRoles is allow_permission with creator=True: the caller must be in the workspace at all, and then passes as the object's creator, as the holder of one of the named project roles, or as a workspace admin who is also in the project.
//
// The workspace check comes first and refuses on its own, before any role is looked at, which is what the decorator does when it is given a model to check the creator against.
func (handler *Handler) requireViewCreatorOrRoles(c *gin.Context, user *auth.User, createdBy *string, roles ...int) bool {
	slug, projectID := c.Param("slug"), c.Param("id")
	workspaceRole, inWorkspace, err := handler.workspaceMemberRole(c.Request.Context(), slug, user.ID)
	if err != nil {
		handler.internalError(c, err)
		return false
	}
	if !inWorkspace {
		c.JSON(http.StatusForbidden, gin.H{"error": "You don't have the required permissions."})
		return false
	}
	if createdBy != nil && *createdBy == user.ID {
		return true
	}
	member, found, err := handler.activeProjectMember(c.Request.Context(), slug, projectID, user.ID)
	if err != nil {
		handler.internalError(c, err)
		return false
	}
	if found {
		for _, role := range roles {
			if member.Role == role {
				return true
			}
		}
		if workspaceRole == roleAdmin {
			return true
		}
	}
	c.JSON(http.StatusForbidden, gin.H{"error": "You don't have the required permissions."})
	return false
}
