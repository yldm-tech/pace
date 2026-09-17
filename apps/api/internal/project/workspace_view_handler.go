package project

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api/internal/auth"
	"github.com/yldm-tech/pace/apps/api/internal/drf"
	"gorm.io/gorm"
)

func (handler *Handler) registerWorkspaceViewRoutes(router gin.IRouter) {
	router.GET("/api/workspaces/:slug/views/", handler.authenticated(handler.workspaceViewList))
	router.POST("/api/workspaces/:slug/views/", handler.authenticated(handler.workspaceViewCreate))
	router.GET("/api/workspaces/:slug/views/:view/", handler.authenticated(handler.workspaceViewRetrieve))
	router.PATCH("/api/workspaces/:slug/views/:view/", handler.authenticated(handler.workspaceViewUpdate))
	router.PUT("/api/workspaces/:slug/views/:view/", handler.authenticated(handler.fullUpdate("view", handler.workspaceViewUpdate)))
	router.DELETE("/api/workspaces/:slug/views/:view/", handler.authenticated(handler.workspaceViewDestroy))
}

// workspaceViewList returns the workspace's own views: the ones belonging to no project.
//
// It is the project list's twin and differs in three ways. There is no favourite flag, because nothing annotates one here. The guest narrowing has no escape hatch — a workspace guest sees only their own views whatever the projects are configured to allow. And the order is the caller's to choose, from an allowlist of three fields.
func (handler *Handler) workspaceViewList(c *gin.Context, user *auth.User) {
	if !handler.requireWorkspaceRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	slug := c.Param("slug")

	role, _, err := handler.workspaceMemberRole(c.Request.Context(), slug, user.ID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	query := handler.workspaceViewScope(c, user, slug)
	if role == roleGuest {
		query = query.Where("v.owned_by_id = ?", user.ID)
	}

	var rows []viewRow
	err = query.Order(orderClause("v.", sanitizeOrderBy(c.Query("order_by"), viewOrderByAllowlist, "-created_at"))).
		Scan(&rows).Error
	if err != nil {
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

// workspaceViewScope is the queryset the workspace routes read through: the workspace's project-less views the caller is allowed to see.
func (handler *Handler) workspaceViewScope(c *gin.Context, user *auth.User, slug string) *gorm.DB {
	return handler.db.WithContext(c.Request.Context()).Table("issue_views v").Select("v.*").
		Joins("JOIN workspaces w ON w.id = v.workspace_id").
		Where("w.slug = ? AND v.project_id IS NULL AND v.deleted_at IS NULL", slug).
		Where("v.owned_by_id = ? OR v.access = 1", user.ID)
}

// viewOrderByAllowlist is VIEW_ORDER_BY_ALLOWLIST: the three fields a caller may order the list by.
var viewOrderByAllowlist = map[string]string{"created_at": "created_at", "updated_at": "updated_at", "name": "name"}

// sanitizeOrderBy is plane.utils.order_queryset.sanitize_order_by. It strips at most one leading dash, so a doubled one is rejected rather than reaching the ORM.
// orderClause turns what sanitizeOrderBy returns into SQL. Its result is Django's ordering syntax, where a leading minus means descending -- "-created_at" -- and that is not something a database understands: concatenating it after a table alias produces `p.-created_at`, which Postgres rejects with `syntax error at or near "-"` and the request answers 500.
func orderClause(alias, order string) string {
	column := strings.TrimPrefix(order, "-")
	if strings.HasPrefix(order, "-") {
		return alias + column + " DESC"
	}
	return alias + column + " ASC"
}

// sanitizeOrderBy maps the order_by query parameter onto the column it is allowed to produce.
//
// The return value is read out of the allowlist rather than built from the parameter, so the string that reaches the query is always one this file wrote. That is what makes it safe, and it is also what makes the safety visible: nothing derived from the request survives the lookup.
func sanitizeOrderBy(value string, allowed map[string]string, fallback string) string {
	if value == "" {
		return fallback
	}
	descending := strings.HasPrefix(value, "-")
	bare := strings.TrimPrefix(value, "-")
	column, permitted := allowed[bare]
	if !permitted || strings.HasPrefix(bare, "-") {
		return fallback
	}
	if descending {
		return "-" + column
	}
	return column
}

// workspaceViewRetrieve returns one view.
//
// It carries no permission decorator at all, so the viewset's own default applies and any signed-in user reaches it. The queryset still hides a private view they do not own — and then the serializer is handed the nothing that comes back, which renders as an empty body rather than a 404.
func (handler *Handler) workspaceViewRetrieve(c *gin.Context, user *auth.User) {
	slug, viewID := c.Param("slug"), c.Param("view")

	var rows []viewRow
	err := handler.workspaceViewScope(c, user, slug).Where("v.id = ?", viewID).Limit(1).Scan(&rows).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if handler.tasks != nil {
		// The visit is recorded whether or not the view was found, and with no project to record it against.
		if err := handler.tasks.PublishRecentVisit(c.Request.Context(), "view", viewID, user.ID, "", slug); err != nil {
			handler.internalError(c, err)
			return
		}
	}
	if len(rows) == 0 {
		drf.Respond(c, http.StatusOK, gin.H{})
		return
	}
	drf.Respond(c, http.StatusOK, viewJSON(rows[0]))
}

// workspaceViewCreate saves a new workspace view. Its sort order is placed after every other project-less view in the workspace.
func (handler *Handler) workspaceViewCreate(c *gin.Context, user *auth.User) {
	if !handler.requireWorkspaceRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	slug := c.Param("slug")

	var payload map[string]any
	if err := c.ShouldBindJSON(&payload); err != nil {
		handler.invalidDetail(c)
		return
	}
	if name, ok := payload["name"].(string); !ok || strings.TrimSpace(name) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"name": []string{"This field is required."}})
		return
	}

	var workspaceIDs []string
	err := handler.db.WithContext(c.Request.Context()).Table("workspaces").
		Where("slug = ?", slug).Limit(1).Pluck("id", &workspaceIDs).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if len(workspaceIDs) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "The required object does not exist."})
		return
	}

	view := newIssueView()
	view.WorkspaceID = workspaceIDs[0]
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
	view.SortOrder, err = handler.nextWorkspaceViewSortOrder(c, view.WorkspaceID)
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

// workspaceViewUpdate edits a workspace view, refusing a locked one and anyone who is not the owner.
func (handler *Handler) workspaceViewUpdate(c *gin.Context, user *auth.User) {
	slug, viewID := c.Param("slug"), c.Param("view")

	var payload map[string]any
	if err := c.ShouldBindJSON(&payload); err != nil {
		handler.invalidDetail(c)
		return
	}

	view, found, err := handler.workspaceViewByID(c, slug, viewID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "The required object does not exist."})
		return
	}
	if !handler.requireWorkspaceViewCreatorOrRoles(c, user, view.CreatedByID) {
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

// workspaceViewDestroy removes a workspace view and its favourites.
//
// Unlike the project one it leaves the recent visits alone, so a deleted workspace view keeps showing up in the recent list until something else clears it.
func (handler *Handler) workspaceViewDestroy(c *gin.Context, user *auth.User) {
	slug, viewID := c.Param("slug"), c.Param("view")

	view, found, err := handler.workspaceViewByID(c, slug, viewID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "The required object does not exist."})
		return
	}
	if !handler.requireWorkspaceViewCreatorOrRoles(c, user, view.CreatedByID, roleAdmin) {
		return
	}

	role, _, err := handler.workspaceMemberRole(c.Request.Context(), slug, user.ID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if role != roleAdmin && view.OwnedByID != user.ID {
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
		Where(`entity_type = 'view' AND entity_identifier = ? AND project_id IS NULL AND deleted_at IS NULL
			AND workspace_id = (SELECT id FROM workspaces WHERE slug = ?)`, viewID, slug).
		Update("deleted_at", now).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (handler *Handler) workspaceViewByID(c *gin.Context, slug, viewID string) (IssueView, bool, error) {
	var view IssueView
	err := handler.db.WithContext(c.Request.Context()).Table("issue_views v").Select("v.*").
		Joins("JOIN workspaces w ON w.id = v.workspace_id").
		Where("w.slug = ? AND v.id = ? AND v.deleted_at IS NULL", slug, viewID).
		Take(&view).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return IssueView{}, false, nil
	}
	if err != nil {
		return IssueView{}, false, err
	}
	return view, true, nil
}

// nextWorkspaceViewSortOrder places a new view after every other project-less view in the workspace.
func (handler *Handler) nextWorkspaceViewSortOrder(c *gin.Context, workspaceID string) (float64, error) {
	// MAX over no rows is one row holding null rather than no rows at all, and neither a float64 nor a *float64 survives Pluck scanning that. A nullable scan target is what does.
	var largest sql.NullFloat64
	err := handler.db.WithContext(c.Request.Context()).Table("issue_views").
		Where("workspace_id = ? AND project_id IS NULL AND deleted_at IS NULL", workspaceID).
		Select("MAX(sort_order)").Row().Scan(&largest)
	if err != nil {
		return 0, err
	}
	if !largest.Valid {
		return defaultViewSortOrder, nil
	}
	return largest.Float64 + 10000, nil
}

// requireWorkspaceViewCreatorOrRoles is allow_permission at the workspace level with creator=True: the caller must be in the workspace at all, and then passes as the object's creator or as the holder of one of the named workspace roles.
//
// The workspace-admin fallback the project version has is not here: at this level the allowed roles are checked against workspace membership directly, so there is nothing to fall back to.
func (handler *Handler) requireWorkspaceViewCreatorOrRoles(c *gin.Context, user *auth.User, createdBy *string, roles ...int) bool {
	role, inWorkspace, err := handler.workspaceMemberRole(c.Request.Context(), c.Param("slug"), user.ID)
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
	for _, allowed := range roles {
		if role == allowed {
			return true
		}
	}
	c.JSON(http.StatusForbidden, gin.H{"error": "You don't have the required permissions."})
	return false
}
