package project

import (
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api/internal/auth"
	"github.com/yldm-tech/pace/apps/api/internal/drf"
	"github.com/yldm-tech/pace/apps/api/internal/pagination"
)

func (handler *Handler) registerProjectDetailRoutes(router gin.IRouter) {
	router.GET("/api/workspaces/:slug/projects/details/", handler.authenticated(handler.projectListDetail))
	router.POST("/api/workspaces/:slug/projects/:id/archive/", handler.authenticated(handler.projectArchive))
	router.DELETE("/api/workspaces/:slug/projects/:id/archive/", handler.authenticated(handler.projectUnarchive))
	router.GET("/api/workspaces/:slug/project-identifiers/", handler.authenticated(handler.projectIdentifierCheck))
	router.DELETE("/api/workspaces/:slug/project-identifiers/", handler.authenticated(handler.projectIdentifierDestroy))
}

// projectListDetail is the project list ordered for a sidebar: by the caller's own place in it and then by name.
//
// It paginates only when **both** per_page and cursor are given, and answers a plain list otherwise — so a caller who sends one of the two gets every project rather than a page.
func (handler *Handler) projectListDetail(c *gin.Context, user *auth.User) {
	if !handler.requireWorkspaceRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	slug := c.Param("slug")
	query := handler.projectListQuery(c, user, slug)
	query, ok := handler.applyVisibility(c, query, user, slug)
	if !ok {
		return
	}
	var rows []projectListRow
	if err := query.Scan(&rows).Error; err != nil {
		handler.internalError(c, err)
		return
	}
	// The ordering is the caller's own sort order and then the name, and a project the caller has no ordering for sorts last.
	sort.SliceStable(rows, func(left, right int) bool {
		first, second := rows[left].SortOrder, rows[right].SortOrder
		switch {
		case first == nil && second == nil:
			return rows[left].Name < rows[right].Name
		case first == nil:
			return false
		case second == nil:
			return true
		case *first != *second:
			return *first < *second
		}
		return rows[left].Name < rows[right].Name
	})

	fields := []string{}
	for _, field := range strings.Split(c.Query("fields"), ",") {
		if field != "" {
			fields = append(fields, field)
		}
	}
	results := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		results = append(results, narrowFields(projectListJSON(row), fields))
	}

	if c.Query("per_page") == "" || c.Query("cursor") == "" {
		drf.Respond(c, http.StatusOK, results)
		return
	}
	perPage, err := pagination.PerPage(c.Query("per_page"), pagination.DefaultPerPage, pagination.DefaultPerPage)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	cursor, err := pagination.ParseOffsetCursor(c.Query("cursor"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "Invalid cursor parameter."})
		return
	}
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

// narrowFields keeps only the fields the caller asked for, and everything when they asked for none.
func narrowFields(data gin.H, fields []string) gin.H {
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

// projectArchive puts a project away and **removes every favourite pointing at it**, for everybody rather than for the caller alone.
func (handler *Handler) projectArchive(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember) {
		return
	}
	slug, projectID := c.Param("slug"), c.Param("id")
	var projects []Project
	err := handler.db.WithContext(c.Request.Context()).Table("projects p").Select("p.*").
		Joins("JOIN workspaces w ON w.id = p.workspace_id").
		Where("p.id = ? AND w.slug = ? AND p.deleted_at IS NULL", projectID, slug).
		Limit(1).Scan(&projects).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if len(projects) == 0 {
		handler.notFound(c)
		return
	}
	now := handler.clock().UTC()
	err = handler.db.WithContext(c.Request.Context()).Exec(
		`UPDATE projects SET archived_at = ?, updated_at = ? WHERE id = ?`, now, now, projectID).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	// The favourites are soft deleted through the queryset, which is what delete() on a queryset does here.
	err = handler.db.WithContext(c.Request.Context()).Table("user_favorites uf").
		Where(`uf.project_id = ? AND uf.deleted_at IS NULL
			AND uf.workspace_id = (SELECT id FROM workspaces WHERE slug = ?)`, projectID, slug).
		Update("deleted_at", now).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, gin.H{"archived_at": now})
}

// projectUnarchive brings a project back. It does not bring the favourites back with it.
func (handler *Handler) projectUnarchive(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember) {
		return
	}
	now := handler.clock().UTC()
	result := handler.db.WithContext(c.Request.Context()).Exec(
		`UPDATE projects SET archived_at = NULL, updated_at = ?
		WHERE id = ? AND workspace_id = (SELECT id FROM workspaces WHERE slug = ?) AND deleted_at IS NULL`,
		now, c.Param("id"), c.Param("slug"))
	if result.Error != nil {
		handler.internalError(c, result.Error)
		return
	}
	if result.RowsAffected == 0 {
		handler.notFound(c)
		return
	}
	c.Status(http.StatusNoContent)
}

// projectIdentifierCheck answers whether an identifier is taken, reporting a **count** and the rows behind it.
//
// The name is upper-cased and trimmed before the lookup, so the check agrees with what a create would write.
func (handler *Handler) projectIdentifierCheck(c *gin.Context, user *auth.User) {
	if !handler.requireWorkspaceRole(c, user, roleAdmin, roleMember) {
		return
	}
	name := strings.ToUpper(strings.TrimSpace(c.Query("name")))
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Name is required"})
		return
	}
	var rows []struct {
		ID        string  `gorm:"column:id"`
		Name      string  `gorm:"column:name"`
		ProjectID *string `gorm:"column:project_id"`
	}
	err := handler.db.WithContext(c.Request.Context()).Table("project_identifiers pi").
		Select("pi.id, pi.name, pi.project_id").
		Joins("JOIN workspaces w ON w.id = pi.workspace_id").
		Where("pi.name = ? AND w.slug = ?", name, c.Param("slug")).
		Scan(&rows).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	identifiers := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		identifiers = append(identifiers, gin.H{"id": row.ID, "name": row.Name, "project": row.ProjectID})
	}
	drf.Respond(c, http.StatusOK, gin.H{"exists": len(identifiers), "identifiers": identifiers})
}

// projectIdentifierDestroy frees an identifier, and refuses while a project still carries it.
//
// The row goes for good rather than being soft deleted, which is what lets the name be taken again.
func (handler *Handler) projectIdentifierDestroy(c *gin.Context, user *auth.User) {
	if !handler.requireWorkspaceRole(c, user, roleAdmin, roleMember) {
		return
	}
	var request struct {
		Name string `json:"name"`
	}
	_ = c.ShouldBindJSON(&request)
	name := strings.ToUpper(strings.TrimSpace(request.Name))
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Name is required"})
		return
	}
	var count int64
	err := handler.db.WithContext(c.Request.Context()).Table("projects p").
		Joins("JOIN workspaces w ON w.id = p.workspace_id").
		Where("p.identifier = ? AND w.slug = ? AND p.deleted_at IS NULL", name, c.Param("slug")).
		Count(&count).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if count > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Cannot delete an identifier of an existing project"})
		return
	}
	err = handler.db.WithContext(c.Request.Context()).Exec(
		`DELETE FROM project_identifiers WHERE name = ? AND workspace_id = (SELECT id FROM workspaces WHERE slug = ?)`,
		name, c.Param("slug")).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
