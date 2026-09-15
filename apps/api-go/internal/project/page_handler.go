package project

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"github.com/yldm-tech/pace/apps/api-go/internal/drf"
	"gorm.io/gorm"
)

func (handler *Handler) registerPageRoutes(router gin.IRouter) {
	base := "/api/workspaces/:slug/projects/:id/"
	router.GET(base+"pages-summary/", handler.authenticated(handler.pageSummary))
	router.GET(base+"pages/", handler.authenticated(handler.pageList))
	router.POST(base+"pages/", handler.authenticated(handler.pageCreate))
	router.GET(base+"pages/:page/", handler.authenticated(handler.pageRetrieve))
	router.PATCH(base+"pages/:page/", handler.authenticated(handler.pageUpdate))
	router.DELETE(base+"pages/:page/", handler.authenticated(handler.pageDestroy))
	router.POST(base+"pages/:page/lock/", handler.authenticated(handler.pageLock))
	router.DELETE(base+"pages/:page/lock/", handler.authenticated(handler.pageUnlock))
	router.POST(base+"pages/:page/access/", handler.authenticated(handler.pageSetAccess))
	router.POST(base+"pages/:page/archive/", handler.authenticated(handler.pageArchive))
	router.DELETE(base+"pages/:page/archive/", handler.authenticated(handler.pageUnarchive))
	router.POST(base+"favorite-pages/:page/", handler.authenticated(handler.pageFavoriteCreate))
	router.DELETE(base+"favorite-pages/:page/", handler.authenticated(handler.pageFavoriteDestroy))
}

// pageRow is the page with everything the list annotates onto it.
type pageRow struct {
	Page
	IsFavorite bool           `gorm:"column:is_favorite"`
	LabelIDs   pq.StringArray `gorm:"column:label_ids;type:uuid[]"`
	ProjectIDs pq.StringArray `gorm:"column:project_ids;type:uuid[]"`
}

// pageAccess is the access column, which reads the opposite way round from every other one in the codebase: zero is public and one is private.
const (
	pagePublicAccess  = 0
	pagePrivateAccess = 1
)

// requirePageAccess is ProjectPagePermission.
//
// It is unusual in two ways. The role rules are keyed on the **HTTP method** rather than on the action, so locking a page needs a member and unlocking it needs an admin — the two halves of one button, behind different permissions, because one is a POST and the other a DELETE. And a private page is readable by **its owner alone**: the community implementation of the private-page hook returns false for everyone else, whatever their role.
func (handler *Handler) requirePageAccess(c *gin.Context, user *auth.User, pageID string) (Page, bool) {
	slug, projectID := c.Param("slug"), c.Param("id")

	member, found, err := handler.activeProjectMember(c.Request.Context(), slug, projectID, user.ID)
	if err != nil {
		handler.internalError(c, err)
		return Page{}, false
	}
	if !found {
		c.JSON(http.StatusForbidden, gin.H{"error": "You don't have the required permissions."})
		return Page{}, false
	}

	page := Page{}
	if pageID != "" {
		// Both halves of the link condition sit on the same relation, so a page whose link to this project was removed is refused rather than found.
		var pages []Page
		err := handler.db.WithContext(c.Request.Context()).Table("pages p").Select("p.*").
			Joins("JOIN workspaces w ON w.id = p.workspace_id").
			Joins("JOIN project_pages pp ON pp.page_id = p.id AND pp.project_id = ? AND pp.deleted_at IS NULL", projectID).
			Where("w.slug = ? AND p.id = ? AND p.deleted_at IS NULL", slug, pageID).
			Limit(1).Scan(&pages).Error
		if err != nil {
			handler.internalError(c, err)
			return Page{}, false
		}
		if len(pages) == 0 {
			c.JSON(http.StatusForbidden, gin.H{"error": "You don't have the required permissions."})
			return Page{}, false
		}
		page = pages[0]
		if page.OwnedByID == user.ID {
			return page, true
		}
		if page.Access == pagePrivateAccess {
			c.JSON(http.StatusForbidden, gin.H{"error": "You don't have the required permissions."})
			return Page{}, false
		}
	}

	if !pageMethodAllows(c.Request.Method, member.Role) {
		c.JSON(http.StatusForbidden, gin.H{"error": "You don't have the required permissions."})
		return Page{}, false
	}
	return page, true
}

// pageMethodAllows is the method table the permission class carries. A guest may only read.
func pageMethodAllows(method string, role int) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch:
		return role == roleAdmin || role == roleMember
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return role == roleAdmin || role == roleMember || role == roleGuest
	case http.MethodDelete:
		return role == roleAdmin
	}
	return false
}

// pageList returns the project's top-level pages.
//
// Only pages with no parent appear: a child page is reached through its parent rather than through the list. A guest sees only their own unless the project has been opened up.
func (handler *Handler) pageList(c *gin.Context, user *auth.User) {
	if _, allowed := handler.requirePageAccess(c, user, ""); !allowed {
		return
	}
	slug, projectID := c.Param("slug"), c.Param("id")

	restricted, err := handler.viewsRestrictedToOwner(c, user, slug, projectID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	query := handler.pageScope(c, user, slug, projectID)
	if restricted {
		query = query.Where("p.owned_by_id = ?", user.ID)
	}

	var rows []pageRow
	err = query.Order("is_favorite DESC, p." + sanitizeOrderBy(c.Query("order_by"), pageOrderByAllowlist, "-created_at") + ", p.id").
		Scan(&rows).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	results := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		results = append(results, pageJSON(row, false))
	}
	drf.Respond(c, http.StatusOK, results)
}

// pageOrderByAllowlist is PAGE_ORDER_BY_ALLOWLIST. Django resolves an order field at call time, so an unknown one raises and a relation path walks the ORM — which is what the allowlist is there to stop.
var pageOrderByAllowlist = map[string]bool{"created_at": true, "updated_at": true, "name": true}

// pageScope is the queryset every page route reads through.
func (handler *Handler) pageScope(c *gin.Context, user *auth.User, slug, projectID string) *gorm.DB {
	return handler.db.WithContext(c.Request.Context()).Table("pages p").
		Select(pageAnnotations(), user.ID, slug, projectID).
		Joins("JOIN workspaces w ON w.id = p.workspace_id").
		Joins("JOIN project_pages pp ON pp.page_id = p.id AND pp.deleted_at IS NULL").
		Joins("JOIN projects pr ON pr.id = pp.project_id AND pr.archived_at IS NULL").
		Joins("JOIN project_members pm ON pm.project_id = pp.project_id AND pm.member_id = ? AND pm.is_active = TRUE", user.ID).
		Where("w.slug = ? AND p.deleted_at IS NULL AND p.parent_id IS NULL", slug).
		Where("p.owned_by_id = ? OR p.access = ?", user.ID, pagePublicAccess).
		// The project filter is an annotation that is then filtered on, so the page has to be linked to *this* project while still being reachable through any of them.
		Where(`EXISTS (SELECT 1 FROM project_pages fp WHERE fp.page_id = p.id AND fp.project_id = ?)`, projectID).
		Group("p.id")
}

// pageAnnotations is the select the list builds.
func pageAnnotations() string {
	return `p.*,
		EXISTS (SELECT 1 FROM user_favorites uf
			JOIN workspaces uw ON uw.id = uf.workspace_id
			WHERE uf.user_id = ? AND uf.entity_identifier = p.id AND uf.entity_type = 'page'
			AND uw.slug = ? AND uf.deleted_at IS NULL) AS is_favorite,
		EXISTS (SELECT 1 FROM project_pages ap WHERE ap.page_id = p.id AND ap.project_id = ?) AS project,
		COALESCE((SELECT ARRAY_AGG(DISTINCT pl.label_id) FROM page_labels pl
			WHERE pl.page_id = p.id AND pl.label_id IS NOT NULL), '{}') AS label_ids,
		COALESCE((SELECT ARRAY_AGG(DISTINCT lp.project_id) FROM project_pages lp
			WHERE lp.page_id = p.id), '{}') AS project_ids`
}

// pageRetrieve returns one page, with the ids of every issue embedded in it.
func (handler *Handler) pageRetrieve(c *gin.Context, user *auth.User) {
	if _, allowed := handler.requirePageAccess(c, user, c.Param("page")); !allowed {
		return
	}
	slug, projectID, pageID := c.Param("slug"), c.Param("id"), c.Param("page")

	var rows []pageRow
	err := handler.pageScope(c, user, slug, projectID).Where("p.id = ?", pageID).Limit(1).Scan(&rows).Error
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
		// Django reads the owner off the result of .first() before checking whether it found anything, so a page the queryset hides raises for a caller the guest rule applies to and answers 404 for everyone else.
		if restricted {
			handler.internalError(c, errors.New("pages: the page does not exist"))
			return
		}
		c.JSON(http.StatusNotFound, gin.H{"error": "Page not found"})
		return
	}
	if restricted && rows[0].OwnedByID != user.ID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "You are not allowed to view this page"})
		return
	}

	var issueIDs []string
	err = handler.db.WithContext(c.Request.Context()).Table("page_logs").
		Where("page_id = ? AND entity_name = 'issue' AND deleted_at IS NULL", pageID).
		Pluck("entity_identifier", &issueIDs).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}

	data := pageJSON(rows[0], true)
	data["issue_ids"] = issueIDs
	if c.DefaultQuery("track_visit", "true") == "true" && handler.tasks != nil {
		err = handler.tasks.PublishRecentVisit(c.Request.Context(), "page", pageID, user.ID, projectID, slug)
		if err != nil {
			handler.internalError(c, err)
			return
		}
	}
	drf.Respond(c, http.StatusOK, data)
}

// pageCreate saves a new page and links it to the project it was created in.
func (handler *Handler) pageCreate(c *gin.Context, user *auth.User) {
	if _, allowed := handler.requirePageAccess(c, user, ""); !allowed {
		return
	}
	slug, projectID := c.Param("slug"), c.Param("id")

	var payload map[string]any
	if err := c.ShouldBindJSON(&payload); err != nil {
		handler.invalidDetail(c)
		return
	}

	var project Project
	if err := handler.db.WithContext(c.Request.Context()).Where("id = ?", projectID).Take(&project).Error; err != nil {
		handler.internalError(c, err)
		return
	}

	now := handler.clock().UTC()
	page := Page{
		WorkspaceID: project.WorkspaceID, OwnedByID: user.ID,
		CreatedByID: &user.ID, UpdatedByID: &user.ID, CreatedAt: now, UpdatedAt: now,
		Access: pagePublicAccess, DescriptionHTML: "<p></p>",
	}
	applyPagePayload(&page, payload)
	// The three description fields are read out of the context rather than validated, so whatever the caller sent is stored as it stands.
	page.DescriptionJSON = rawJSONOr(payload["description_json"], "{}")
	page.DescriptionHTML = stringOr(payload["description_html"], "<p></p>")
	if binary, ok := payload["description_binary"].(string); ok {
		page.DescriptionBinary = []byte(binary)
	}

	identifier, err := newUUID()
	if err != nil {
		handler.internalError(c, err)
		return
	}
	page.ID = identifier

	err = handler.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&page).Error; err != nil {
			return err
		}
		linkID, err := newUUID()
		if err != nil {
			return err
		}
		link := ProjectPage{
			ID: linkID, CreatedAt: now, UpdatedAt: now,
			CreatedByID: page.CreatedByID, UpdatedByID: page.UpdatedByID,
			WorkspaceID: page.WorkspaceID, ProjectID: projectID, PageID: page.ID,
		}
		if err := tx.Create(&link).Error; err != nil {
			return err
		}
		return handler.writePageLabels(tx, page, payload, now)
	})
	if err != nil {
		handler.internalError(c, err)
		return
	}

	if err := handler.publishPageTransaction(c, page.DescriptionHTML, nil, page.ID); err != nil {
		handler.internalError(c, err)
		return
	}

	// The response is read back through the list's queryset, so it carries the annotations a plain save would not have.
	var rows []pageRow
	err = handler.pageScope(c, user, slug, projectID).Where("p.id = ?", page.ID).Limit(1).Scan(&rows).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if len(rows) == 0 {
		// A page created with a parent is not in the list's queryset, and Django's .get raises rather than answering.
		handler.internalError(c, errors.New("pages: the created page is not in the list queryset"))
		return
	}
	drf.Respond(c, http.StatusCreated, pageJSON(rows[0], true))
}

// pageUpdate edits a page. A locked page refuses, and the access column moves only for its owner.
func (handler *Handler) pageUpdate(c *gin.Context, user *auth.User) {
	page, allowed := handler.requirePageAccess(c, user, c.Param("page"))
	if !allowed {
		return
	}
	slug, projectID := c.Param("slug"), c.Param("id")

	var payload map[string]any
	if err := c.ShouldBindJSON(&payload); err != nil {
		handler.invalidDetail(c)
		return
	}
	if page.IsLocked {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Page is locked"})
		return
	}
	if parent, ok := payload["parent"].(string); ok && parent != "" {
		var parents []string
		err := handler.db.WithContext(c.Request.Context()).Table("pages p").
			Joins("JOIN workspaces w ON w.id = p.workspace_id").
			Joins("JOIN project_pages pp ON pp.page_id = p.id AND pp.project_id = ? AND pp.deleted_at IS NULL", projectID).
			Where("w.slug = ? AND p.id = ? AND p.deleted_at IS NULL", slug, parent).
			Limit(1).Pluck("p.id", &parents).Error
		if err != nil {
			handler.internalError(c, err)
			return
		}
		if len(parents) == 0 {
			// Every failure inside the handler's try block answers the same message, including this one, which has nothing to do with ownership.
			c.JSON(http.StatusBadRequest, gin.H{"error": "Access cannot be updated since this page is owned by someone else"})
			return
		}
	}
	if access, ok := payload["access"]; ok && intOr(access, page.Access) != page.Access && page.OwnedByID != user.ID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Access cannot be updated since this page is owned by someone else"})
		return
	}

	previous := page.DescriptionHTML
	applyPagePayload(&page, payload)
	if html, ok := payload["description_html"].(string); ok {
		page.DescriptionHTML = html
	}
	now := handler.clock().UTC()
	page.UpdatedAt, page.UpdatedByID = now, &user.ID

	err := handler.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		err := tx.Model(&Page{}).Where("id = ?", page.ID).Updates(map[string]any{
			"name": page.Name, "color": page.Color, "access": page.Access,
			"parent_id": page.ParentID, "view_props": page.ViewProps, "logo_props": page.LogoProps,
			"description_html": page.DescriptionHTML,
			"updated_at":       page.UpdatedAt, "updated_by_id": page.UpdatedByID,
		}).Error
		if err != nil {
			return err
		}
		return handler.writePageLabels(tx, page, payload, now)
	})
	if err != nil {
		handler.internalError(c, err)
		return
	}

	// The transaction is captured only when the request actually carried a description, so an edit that renames the page leaves no version behind.
	if html, ok := payload["description_html"].(string); ok && html != "" {
		if err := handler.publishPageTransaction(c, html, &previous, page.ID); err != nil {
			handler.internalError(c, err)
			return
		}
	}

	labels, projects, err := handler.pageRelations(c, page.ID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, pageJSON(pageRow{Page: page, LabelIDs: labels, ProjectIDs: projects}, true))
}

// pageLock and pageUnlock are the two halves of one button, and they sit behind different permissions: locking is a POST and needs a member, unlocking is a DELETE and needs an admin.
func (handler *Handler) pageLock(c *gin.Context, user *auth.User) {
	handler.setPageLock(c, user, true)
}

func (handler *Handler) pageUnlock(c *gin.Context, user *auth.User) {
	handler.setPageLock(c, user, false)
}

func (handler *Handler) setPageLock(c *gin.Context, user *auth.User, locked bool) {
	page, allowed := handler.requirePageAccess(c, user, c.Param("page"))
	if !allowed {
		return
	}
	now := handler.clock().UTC()
	err := handler.db.WithContext(c.Request.Context()).Model(&Page{}).Where("id = ?", page.ID).
		Updates(map[string]any{"is_locked": locked, "updated_at": now, "updated_by_id": user.ID}).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// pageSetAccess moves a page between public and private. Only its owner may, and a request naming no access at all is read as a move to public.
func (handler *Handler) pageSetAccess(c *gin.Context, user *auth.User) {
	page, allowed := handler.requirePageAccess(c, user, c.Param("page"))
	if !allowed {
		return
	}
	var payload map[string]any
	if err := c.ShouldBindJSON(&payload); err != nil {
		handler.invalidDetail(c)
		return
	}
	// The guard reads the request twice with different defaults: it refuses only when the caller named an access that differs from the one the page has, while the value written falls back to public.
	if access, ok := payload["access"]; ok && intOr(access, page.Access) != page.Access && page.OwnedByID != user.ID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Access cannot be updated since this page is owned by someone else"})
		return
	}
	now := handler.clock().UTC()
	err := handler.db.WithContext(c.Request.Context()).Model(&Page{}).Where("id = ?", page.ID).
		Updates(map[string]any{
			"access": intOr(payload["access"], pagePublicAccess), "updated_at": now, "updated_by_id": user.ID,
		}).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// pageArchive archives a page and everything under it, and clears every member's favourite of it.
func (handler *Handler) pageArchive(c *gin.Context, user *auth.User) {
	page, allowed := handler.requirePageAccess(c, user, c.Param("page"))
	if !allowed {
		return
	}
	if !handler.requirePageOwnerOrAdmin(c, user, page, "Only the owner or admin can archive the page") {
		return
	}
	now := handler.clock().UTC()
	slug, projectID := c.Param("slug"), c.Param("id")

	err := handler.db.WithContext(c.Request.Context()).Model(&UserFavorite{}).
		Where(`entity_type = 'page' AND entity_identifier = ? AND project_id = ? AND deleted_at IS NULL
			AND workspace_id = (SELECT id FROM workspaces WHERE slug = ?)`, page.ID, projectID, slug).
		Update("deleted_at", now).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if err := handler.setPageTreeArchived(c, page.ID, &now); err != nil {
		handler.internalError(c, err)
		return
	}
	// The body carries a naive local timestamp, because the handler reads the clock again with datetime.now() rather than reusing the aware one it stored.
	drf.Respond(c, http.StatusOK, gin.H{"archived_at": naiveTimestamp(now)})
}

// pageUnarchive lifts the archive from a page and everything under it.
func (handler *Handler) pageUnarchive(c *gin.Context, user *auth.User) {
	page, allowed := handler.requirePageAccess(c, user, c.Param("page"))
	if !allowed {
		return
	}
	if !handler.requirePageOwnerOrAdmin(c, user, page, "Only the owner or admin can un archive the page") {
		return
	}
	// A page whose parent is still archived is cut loose from it rather than left dangling under something invisible.
	if page.ParentID != nil {
		var archived []time.Time
		err := handler.db.WithContext(c.Request.Context()).Table("pages").
			Where("id = ? AND archived_at IS NOT NULL", *page.ParentID).
			Pluck("archived_at", &archived).Error
		if err != nil {
			handler.internalError(c, err)
			return
		}
		if len(archived) > 0 {
			err = handler.db.WithContext(c.Request.Context()).Model(&Page{}).
				Where("id = ?", page.ID).Update("parent_id", nil).Error
			if err != nil {
				handler.internalError(c, err)
				return
			}
		}
	}
	if err := handler.setPageTreeArchived(c, page.ID, nil); err != nil {
		handler.internalError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// setPageTreeArchived walks the page and its descendants in one recursive statement, the way the raw SQL does. It does not filter deleted rows, so a soft-deleted child is archived along with the rest.
func (handler *Handler) setPageTreeArchived(c *gin.Context, pageID string, archivedAt *time.Time) error {
	return handler.db.WithContext(c.Request.Context()).Exec(`
		WITH RECURSIVE descendants AS (
			SELECT id FROM pages WHERE id = ?
			UNION ALL
			SELECT pages.id FROM pages, descendants WHERE pages.parent_id = descendants.id
		)
		UPDATE pages SET archived_at = ? WHERE id IN (SELECT id FROM descendants)`, pageID, archivedAt).Error
}

// pageDestroy removes an archived page, cuts its children loose and clears what pointed at it.
func (handler *Handler) pageDestroy(c *gin.Context, user *auth.User) {
	page, allowed := handler.requirePageAccess(c, user, c.Param("page"))
	if !allowed {
		return
	}
	slug, projectID := c.Param("slug"), c.Param("id")

	if page.ArchivedAt == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "The page should be archived before deleting"})
		return
	}
	member, _, err := handler.activeProjectMember(c.Request.Context(), slug, projectID, user.ID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if page.OwnedByID != user.ID && member.Role != roleAdmin {
		c.JSON(http.StatusForbidden, gin.H{"error": "Only admin or owner can delete the page"})
		return
	}

	now := handler.clock().UTC()
	database := handler.db.WithContext(c.Request.Context())
	err = database.Exec(`UPDATE pages SET parent_id = NULL WHERE parent_id = ? AND deleted_at IS NULL
		AND EXISTS (SELECT 1 FROM project_pages pp WHERE pp.page_id = pages.id AND pp.project_id = ? AND pp.deleted_at IS NULL)
		AND workspace_id = (SELECT id FROM workspaces WHERE slug = ?)`, page.ID, projectID, slug).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if err := database.Model(&Page{}).Where("id = ?", page.ID).Update("deleted_at", now).Error; err != nil {
		handler.internalError(c, err)
		return
	}
	err = database.Model(&UserFavorite{}).
		Where(`entity_type = 'page' AND entity_identifier = ? AND project_id = ? AND deleted_at IS NULL
			AND workspace_id = (SELECT id FROM workspaces WHERE slug = ?)`, page.ID, projectID, slug).
		Update("deleted_at", now).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	err = database.Exec(`DELETE FROM user_recent_visits
		WHERE project_id = ? AND entity_name = 'page' AND entity_identifier = ?
		AND workspace_id = (SELECT id FROM workspaces WHERE slug = ?)`, projectID, page.ID, slug).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// pageSummary counts what the project's page list would show, split three ways.
func (handler *Handler) pageSummary(c *gin.Context, user *auth.User) {
	if _, allowed := handler.requirePageAccess(c, user, ""); !allowed {
		return
	}
	slug, projectID := c.Param("slug"), c.Param("id")

	restricted, err := handler.viewsRestrictedToOwner(c, user, slug, projectID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	query := handler.db.WithContext(c.Request.Context()).Table("pages p").
		Joins("JOIN workspaces w ON w.id = p.workspace_id").
		Joins("JOIN project_pages pp ON pp.page_id = p.id").
		Joins("JOIN projects pr ON pr.id = pp.project_id AND pr.archived_at IS NULL").
		Joins("JOIN project_members pm ON pm.project_id = pp.project_id AND pm.member_id = ? AND pm.is_active = TRUE", user.ID).
		Where("w.slug = ? AND p.deleted_at IS NULL AND p.parent_id IS NULL", slug).
		Where("p.owned_by_id = ? OR p.access = ?", user.ID, pagePublicAccess).
		Where(`EXISTS (SELECT 1 FROM project_pages fp WHERE fp.page_id = p.id AND fp.project_id = ?)`, projectID)
	if restricted {
		query = query.Where("p.owned_by_id = ?", user.ID)
	}

	var stats struct {
		Public   int64 `gorm:"column:public_pages"`
		Private  int64 `gorm:"column:private_pages"`
		Archived int64 `gorm:"column:archived_pages"`
	}
	// The summary's queryset has no project-page link filter on deleted_at, unlike the list's, so a page whose link was removed is still counted here.
	err = query.Select(`COUNT(DISTINCT p.id) FILTER (WHERE p.access = 0 AND p.archived_at IS NULL) AS public_pages,
		COUNT(DISTINCT p.id) FILTER (WHERE p.access = 1 AND p.archived_at IS NULL) AS private_pages,
		COUNT(DISTINCT p.id) FILTER (WHERE p.archived_at IS NOT NULL) AS archived_pages`).
		Scan(&stats).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, gin.H{
		"public_pages": stats.Public, "private_pages": stats.Private, "archived_pages": stats.Archived,
	})
}

func (handler *Handler) pageFavoriteCreate(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember) {
		return
	}
	pageID := c.Param("page")
	if !handler.createFavorite(c, user, "page", &pageID) {
		return
	}
	c.Status(http.StatusNoContent)
}

func (handler *Handler) pageFavoriteDestroy(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember) {
		return
	}
	handler.destroyFavorite(c, user, "page", c.Param("page"))
}

// requirePageOwnerOrAdmin is the archive rule: anyone below an admin who is not the owner is refused. A caller who is not a project member at all passes it, because the check only refuses someone it found.
func (handler *Handler) requirePageOwnerOrAdmin(c *gin.Context, user *auth.User, page Page, message string) bool {
	member, found, err := handler.activeProjectMember(c.Request.Context(), c.Param("slug"), c.Param("id"), user.ID)
	if err != nil {
		handler.internalError(c, err)
		return false
	}
	if found && member.Role <= roleMember && page.OwnedByID != user.ID {
		c.JSON(http.StatusBadRequest, gin.H{"error": message})
		return false
	}
	return true
}

// writePageLabels replaces the page's labels when the request named any. Naming none leaves them alone; naming an empty list clears them.
func (handler *Handler) writePageLabels(tx *gorm.DB, page Page, payload map[string]any, now time.Time) error {
	value, present := payload["labels"]
	if !present {
		return nil
	}
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	if err := tx.Where("page_id = ?", page.ID).Delete(&PageLabel{}).Error; err != nil {
		return err
	}
	rows := make([]PageLabel, 0, len(items))
	for _, item := range items {
		label, ok := item.(string)
		if !ok {
			continue
		}
		identifier, err := newUUID()
		if err != nil {
			return err
		}
		rows = append(rows, PageLabel{
			ID: identifier, CreatedAt: now, UpdatedAt: now,
			CreatedByID: page.CreatedByID, UpdatedByID: page.UpdatedByID,
			WorkspaceID: page.WorkspaceID, PageID: page.ID, LabelID: label,
		})
	}
	if len(rows) == 0 {
		return nil
	}
	return tx.Create(&rows).Error
}

// pageRelations reads back the two id lists the serializer reports.
func (handler *Handler) pageRelations(c *gin.Context, pageID string) (pq.StringArray, pq.StringArray, error) {
	var labels []string
	err := handler.db.WithContext(c.Request.Context()).Table("page_labels").
		Where("page_id = ? AND deleted_at IS NULL", pageID).Distinct().Pluck("label_id", &labels).Error
	if err != nil {
		return nil, nil, err
	}
	var projects []string
	err = handler.db.WithContext(c.Request.Context()).Table("project_pages").
		Where("page_id = ?", pageID).Distinct().Pluck("project_id", &projects).Error
	if err != nil {
		return nil, nil, err
	}
	return labels, projects, nil
}

// publishPageTransaction queues page_transaction, which is what turns a description change into a version and rewrites the embedded issue links.
func (handler *Handler) publishPageTransaction(c *gin.Context, newHTML string, oldHTML *string, pageID string) error {
	if handler.tasks == nil {
		return nil
	}
	return handler.tasks.PublishPageTransaction(c.Request.Context(), newHTML, oldHTML, pageID)
}

// applyPagePayload writes the writable fields. The workspace and the owner are read-only on the serializer, so a caller naming them changes nothing.
func applyPagePayload(page *Page, payload map[string]any) {
	if value, ok := payload["name"].(string); ok {
		page.Name = value
	}
	if value, ok := payload["color"].(string); ok {
		page.Color = value
	}
	if value, ok := payload["access"]; ok {
		page.Access = intOr(value, page.Access)
	}
	if value, present := payload["parent"]; present {
		if parent, ok := value.(string); ok && parent != "" {
			page.ParentID = &parent
		} else {
			page.ParentID = nil
		}
	}
	for key, target := range map[string]*[]byte{"view_props": &page.ViewProps, "logo_props": &page.LogoProps} {
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

// pageJSON is PageSerializer, or PageDetailSerializer when the description is included.
func pageJSON(row pageRow, detailed bool) gin.H {
	data := gin.H{
		"id": row.ID, "name": row.Name, "owned_by": row.OwnedByID, "access": row.Access,
		"color": row.Color, "parent": row.ParentID, "is_favorite": row.IsFavorite,
		"is_locked": row.IsLocked, "archived_at": dateOnly(row.ArchivedAt),
		"workspace": row.WorkspaceID, "created_at": row.CreatedAt, "updated_at": row.UpdatedAt,
		"created_by": row.CreatedByID, "updated_by": row.UpdatedByID,
		"view_props": decodeJSON(row.ViewProps), "logo_props": decodeJSON(row.LogoProps),
		"label_ids": stringsOrEmpty(row.LabelIDs), "project_ids": stringsOrEmpty(row.ProjectIDs),
	}
	if detailed {
		data["description_html"] = row.DescriptionHTML
	}
	return data
}

// naiveTimestamp is what str(datetime.now()) prints: a local wall clock with no zone and a space where the T would be.
func naiveTimestamp(value time.Time) string {
	local := value.Local()
	if local.Nanosecond() == 0 {
		return local.Format("2006-01-02 15:04:05")
	}
	return local.Format("2006-01-02 15:04:05.000000")
}

// intOr reads a number out of a decoded body, where every number arrives as a float.
func intOr(value any, fallback int) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	}
	return fallback
}

// stringOr reads a string out of a decoded body.
func stringOr(value any, fallback string) string {
	if text, ok := value.(string); ok {
		return text
	}
	return fallback
}

// rawJSONOr re-encodes a decoded value for a jsonb column.
func rawJSONOr(value any, fallback string) []byte {
	if value == nil {
		return []byte(fallback)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return []byte(fallback)
	}
	return encoded
}
