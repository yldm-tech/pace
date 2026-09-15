package project

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"github.com/yldm-tech/pace/apps/api-go/internal/drf"
	"github.com/yldm-tech/pace/apps/api-go/internal/htmlsanitizer"
	"gorm.io/gorm"
)

func (handler *Handler) registerPageDescriptionRoutes(router gin.IRouter) {
	base := "/api/workspaces/:slug/projects/:id/pages/:page/"
	router.GET(base+"description/", handler.authenticated(handler.pageDescriptionRetrieve))
	router.PATCH(base+"description/", handler.authenticated(handler.pageDescriptionUpdate))
	router.GET(base+"versions/", handler.authenticated(handler.pageVersionList))
	router.GET(base+"versions/:version/", handler.authenticated(handler.pageVersionRetrieve))
	router.POST(base+"duplicate/", handler.authenticated(handler.pageDuplicate))
}

// PageVersion is the db.PageVersion table, one row per saved state of a page's description.
type PageVersion struct {
	ID                string     `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt         time.Time  `gorm:"column:created_at"`
	UpdatedAt         time.Time  `gorm:"column:updated_at"`
	CreatedByID       *string    `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID       *string    `gorm:"column:updated_by_id;type:uuid"`
	DeletedAt         *time.Time `gorm:"column:deleted_at"`
	WorkspaceID       string     `gorm:"column:workspace_id;type:uuid"`
	PageID            string     `gorm:"column:page_id;type:uuid"`
	LastSavedAt       time.Time  `gorm:"column:last_saved_at"`
	OwnedByID         *string    `gorm:"column:owned_by_id;type:uuid"`
	DescriptionBinary []byte     `gorm:"column:description_binary"`
	DescriptionHTML   *string    `gorm:"column:description_html"`
	DescriptionJSON   []byte     `gorm:"column:description_json;type:jsonb"`
	DescriptionStripd *string    `gorm:"column:description_stripped"`
	SubPagesData      []byte     `gorm:"column:sub_pages_data;type:jsonb"`
}

func (PageVersion) TableName() string { return "page_versions" }

// pageDescriptionRetrieve streams the page's collaborative document back as a file.
//
// The body is the raw binary column with no encoding applied, and a page that has none answers an empty file rather than a null or a 404.
func (handler *Handler) pageDescriptionRetrieve(c *gin.Context, user *auth.User) {
	if _, allowed := handler.requirePageAccess(c, user, c.Param("page")); !allowed {
		return
	}
	page, found, err := handler.readablePage(c, user)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		// Django's unguarded .get raises DoesNotExist.
		c.JSON(http.StatusNotFound, gin.H{"error": "The required object does not exist."})
		return
	}
	c.Header("Content-Disposition", `attachment; filename="page_description.bin"`)
	c.Data(http.StatusOK, "application/octet-stream", page.DescriptionBinary)
}

// pageDescriptionUpdate saves what the editor produced: the collaborative binary, the rendered HTML and the document tree.
//
// A locked or archived page refuses with a numbered error code rather than a message, which is the only place in the migrated surface that shape appears.
func (handler *Handler) pageDescriptionUpdate(c *gin.Context, user *auth.User) {
	if _, allowed := handler.requirePageAccess(c, user, c.Param("page")); !allowed {
		return
	}
	page, found, err := handler.readablePage(c, user)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "The required object does not exist."})
		return
	}
	if page.IsLocked {
		c.JSON(http.StatusBadRequest, gin.H{"error_code": errorCodePageLocked, "error_message": "PAGE_LOCKED"})
		return
	}
	if page.ArchivedAt != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error_code": errorCodePageArchived, "error_message": "PAGE_ARCHIVED"})
		return
	}

	var payload map[string]any
	if err := c.ShouldBindJSON(&payload); err != nil {
		handler.invalidDetail(c)
		return
	}

	updates := map[string]any{}
	if value, present := payload["description_binary"]; present {
		encoded, ok := value.(string)
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"description_binary": []string{"Not a valid string."}})
			return
		}
		// An empty string is stored as it stands: the validator lets it through before it ever decodes.
		if encoded == "" {
			updates["description_binary"] = []byte{}
		} else {
			decoded, err := base64.StdEncoding.DecodeString(encoded)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"description_binary": []string{"Failed to decode base64 data"}})
				return
			}
			if message := validateBinaryDocument(decoded); message != "" {
				c.JSON(http.StatusBadRequest, gin.H{"description_binary": []string{"Invalid binary data: " + message}})
				return
			}
			updates["description_binary"] = decoded
		}
	}
	if value, present := payload["description_html"]; present {
		html, ok := value.(string)
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"description_html": []string{"Not a valid string."}})
			return
		}
		if html != "" {
			valid, message, cleaned := htmlsanitizer.ValidateHTMLContent(html)
			if !valid {
				c.JSON(http.StatusBadRequest, gin.H{"description_html": []string{message}})
				return
			}
			if cleaned != nil {
				html = *cleaned
			}
		}
		updates["description_html"] = html
	}
	if value, present := payload["description_json"]; present {
		updates["description_json"] = rawJSONOr(value, "null")
	}

	previous := page.DescriptionHTML
	if len(updates) > 0 {
		now := handler.clock().UTC()
		updates["updated_at"] = now
		updates["updated_by_id"] = user.ID
		err = handler.db.WithContext(c.Request.Context()).Model(&Page{}).Where("id = ?", page.ID).Updates(updates).Error
		if err != nil {
			handler.internalError(c, err)
			return
		}
	}

	// The transaction is captured only for a description that is actually there, while the version is recorded whatever the request carried.
	if html, ok := payload["description_html"].(string); ok && html != "" {
		if err := handler.publishPageTransaction(c, html, &previous, page.ID); err != nil {
			handler.internalError(c, err)
			return
		}
	}
	snapshot, err := json.Marshal(map[string]any{"description_html": previous})
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if handler.tasks != nil {
		err = handler.tasks.PublishTrackPageVersion(c.Request.Context(), page.ID, string(snapshot), user.ID)
		if err != nil {
			handler.internalError(c, err)
			return
		}
	}
	drf.Respond(c, http.StatusOK, gin.H{"message": "Updated successfully"})
}

// errorCodePageLocked and errorCodePageArchived are the two page entries in ERROR_CODES.
const (
	errorCodePageLocked   = 4701
	errorCodePageArchived = 4702
)

// suspiciousBinaryPatterns is what a collaborative document must not begin with. Only the first two hundred bytes are looked at, decoded as text with undecodable runs dropped.
var suspiciousBinaryPatterns = []string{"<html", "<!doctype", "<script", "javascript:", "data:", "<iframe"}

// maxDocumentSize is the ten megabytes both validators share.
const maxDocumentSize = 10 * 1024 * 1024

// validateBinaryDocument is validate_binary_data, returning the message it would have raised with or the empty string.
func validateBinaryDocument(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	if len(data) > maxDocumentSize {
		return "Binary data exceeds maximum size limit (10MB)"
	}
	if len(data) < 4 {
		return "Binary data too short to be valid document format"
	}
	// Python decodes the whole document first, dropping what it cannot read, and only then takes two hundred **characters**. Cutting two hundred bytes instead would shrink the window whenever the document holds anything multibyte.
	decoded := []rune(strings.ToValidUTF8(string(data), ""))
	if len(decoded) > 200 {
		decoded = decoded[:200]
	}
	text := strings.ToLower(string(decoded))
	for _, pattern := range suspiciousBinaryPatterns {
		if strings.Contains(text, pattern) {
			return "Binary data contains suspicious content patterns"
		}
	}
	return ""
}

// pageVersionList returns the saved states of a page, without the documents themselves.
func (handler *Handler) pageVersionList(c *gin.Context, user *auth.User) {
	if _, allowed := handler.requirePageAccess(c, user, c.Param("page")); !allowed {
		return
	}
	var versions []PageVersion
	err := handler.pageVersionScope(c).Where("v.page_id = ?", c.Param("page")).Scan(&versions).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	results := make([]gin.H, 0, len(versions))
	for _, version := range versions {
		results = append(results, pageVersionJSON(version, false))
	}
	drf.Respond(c, http.StatusOK, results)
}

// pageVersionRetrieve returns one saved state, with the document in it.
func (handler *Handler) pageVersionRetrieve(c *gin.Context, user *auth.User) {
	if _, allowed := handler.requirePageAccess(c, user, c.Param("page")); !allowed {
		return
	}
	var versions []PageVersion
	err := handler.pageVersionScope(c).
		Where("v.page_id = ? AND v.id = ?", c.Param("page"), c.Param("version")).
		Limit(1).Scan(&versions).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if len(versions) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "The required object does not exist."})
		return
	}
	drf.Respond(c, http.StatusOK, pageVersionJSON(versions[0], true))
}

// pageVersionScope narrows the versions to a page that is linked to this project by a live link, which is what stops a version being read through a project the page has been taken out of.
func (handler *Handler) pageVersionScope(c *gin.Context) *gorm.DB {
	return handler.db.WithContext(c.Request.Context()).Table("page_versions v").Select("DISTINCT v.*").
		Joins("JOIN workspaces w ON w.id = v.workspace_id").
		Joins("JOIN project_pages pp ON pp.page_id = v.page_id AND pp.project_id = ? AND pp.deleted_at IS NULL", c.Param("id")).
		Where("w.slug = ? AND v.deleted_at IS NULL", c.Param("slug"))
}

// pageVersionJSON is PageVersionSerializer, or the detail one when the document is included.
func pageVersionJSON(version PageVersion, detailed bool) gin.H {
	data := gin.H{
		"id": version.ID, "workspace": version.WorkspaceID, "page": version.PageID,
		"last_saved_at": version.LastSavedAt, "owned_by": version.OwnedByID,
		"created_at": version.CreatedAt, "updated_at": version.UpdatedAt,
		"created_by": version.CreatedByID, "updated_by": version.UpdatedByID,
	}
	if detailed {
		data["description_binary"] = version.DescriptionBinary
		data["description_html"] = version.DescriptionHTML
		data["description_json"] = decodeJSON(version.DescriptionJSON)
		data["description_stripped"] = version.DescriptionStripd
		data["sub_pages_data"] = decodeJSON(version.SubPagesData)
	}
	return data
}

// pageDuplicate copies a page into every project the original sits in.
//
// The copy carries the original's description but **not** its collaborative binary, which is cleared — so the duplicate opens from the rendered HTML and the editor rebuilds the document.
func (handler *Handler) pageDuplicate(c *gin.Context, user *auth.User) {
	page, allowed := handler.requirePageAccess(c, user, c.Param("page"))
	if !allowed {
		return
	}
	slug, projectID := c.Param("slug"), c.Param("id")
	if page.Access == pagePrivateAccess && page.OwnedByID != user.ID {
		c.JSON(http.StatusForbidden, gin.H{"error": "Permission denied"})
		return
	}

	var projectIDs []string
	err := handler.db.WithContext(c.Request.Context()).Table("project_pages").
		Where("page_id = ?", page.ID).Pluck("project_id", &projectIDs).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}

	now := handler.clock().UTC()
	copied := page
	identifier, err := newUUID()
	if err != nil {
		handler.internalError(c, err)
		return
	}
	copied.ID = identifier
	copied.Name = page.Name + " (Copy)"
	copied.DescriptionBinary = nil
	copied.OwnedByID = user.ID
	copied.CreatedByID, copied.UpdatedByID = &user.ID, &user.ID
	copied.CreatedAt, copied.UpdatedAt = now, now

	err = handler.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&copied).Error; err != nil {
			return err
		}
		for _, target := range projectIDs {
			linkID, err := newUUID()
			if err != nil {
				return err
			}
			link := ProjectPage{
				ID: linkID, CreatedAt: now, UpdatedAt: now,
				CreatedByID: copied.CreatedByID, UpdatedByID: copied.UpdatedByID,
				WorkspaceID: copied.WorkspaceID, ProjectID: target, PageID: copied.ID,
			}
			if err := tx.Create(&link).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		handler.internalError(c, err)
		return
	}

	if err := handler.publishPageTransaction(c, copied.DescriptionHTML, nil, copied.ID); err != nil {
		handler.internalError(c, err)
		return
	}
	if handler.tasks != nil {
		// The asset copy is told the project the **loop variable** was left holding, which is the last of the page's projects rather than the one the request named. A page in one project is unaffected; a page in several is not.
		target := projectID
		if len(projectIDs) > 0 {
			target = projectIDs[len(projectIDs)-1]
		}
		err = handler.tasks.PublishCopyDescriptionAssets(c.Request.Context(), "PAGE", copied.ID, target, slug, user.ID)
		if err != nil {
			handler.internalError(c, err)
			return
		}
	}

	var links []string
	err = handler.db.WithContext(c.Request.Context()).Table("project_pages").
		Where("page_id = ?", copied.ID).Distinct().Pluck("project_id", &links).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	// The response is read back through a queryset that annotates only the project ids, so the labels come back empty whatever the original carried.
	drf.Respond(c, http.StatusCreated, pageJSON(pageRow{Page: copied, ProjectIDs: pq.StringArray(links)}, true))
}

// readablePage reads the page the description routes act on: linked to this project by a live link, and either the caller's own or public.
func (handler *Handler) readablePage(c *gin.Context, user *auth.User) (Page, bool, error) {
	var pages []Page
	err := handler.db.WithContext(c.Request.Context()).Table("pages p").Select("p.*").
		Joins("JOIN workspaces w ON w.id = p.workspace_id").
		Joins("JOIN project_pages pp ON pp.page_id = p.id AND pp.project_id = ? AND pp.deleted_at IS NULL", c.Param("id")).
		Where("w.slug = ? AND p.id = ? AND p.deleted_at IS NULL", c.Param("slug"), c.Param("page")).
		Where("p.owned_by_id = ? OR p.access = ?", user.ID, pagePublicAccess).
		Limit(1).Scan(&pages).Error
	if err != nil {
		return Page{}, false, err
	}
	if len(pages) == 0 {
		return Page{}, false, nil
	}
	return pages[0], true, nil
}
