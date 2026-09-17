package workspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api/internal/auth"
	"github.com/yldm-tech/pace/apps/api/internal/drf"
	"github.com/yldm-tech/pace/apps/api/internal/storage"
	"github.com/yldm-tech/pace/apps/api/internal/uploads"
	"gorm.io/gorm"
)

func (handler *Handler) registerAssetRoutes(router gin.IRouter) {
	const base = "/api/assets/v2/workspaces/:slug/"
	router.POST(base, handler.authenticated(handler.assetReserve))
	router.GET(base+":asset/", handler.authenticated(handler.assetDownload))
	router.PATCH(base+":asset/", handler.authenticated(handler.assetMarkUploaded))
	router.DELETE(base+":asset/", handler.authenticated(handler.assetDestroy))
	router.GET(base+"check/:asset/", handler.authenticated(handler.assetCheck))
	router.GET(base+"download/:asset/", handler.authenticated(handler.assetAttachment))
	router.POST(base+"restore/:asset/", handler.authenticated(handler.assetRestore))
	// The static route is the one asset route with no authentication at all: it is what every avatar and logo url points at.
	router.GET("/api/assets/v2/static/:asset/", handler.staticAsset)
}

// SetStorage wires the object store the asset routes sign against. A nil store means object storage is not configured, which those routes refuse rather than half-complete.
func (handler *Handler) SetStorage(store *storage.Store) { handler.storage = store }

// forbidden is what the permission class answers when the caller is not in the workspace at all.
func forbidden(c *gin.Context) {
	c.JSON(http.StatusForbidden, gin.H{"detail": "You do not have permission to perform this action."})
}

// FileAsset is the db.FileAsset table as the workspace routes see it.
type FileAsset struct {
	ID              string     `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt       time.Time  `gorm:"column:created_at"`
	UpdatedAt       time.Time  `gorm:"column:updated_at"`
	CreatedByID     *string    `gorm:"column:created_by_id;type:uuid"`
	DeletedAt       *time.Time `gorm:"column:deleted_at"`
	Attributes      []byte     `gorm:"column:attributes;type:jsonb"`
	Asset           string     `gorm:"column:asset"`
	UserID          *string    `gorm:"column:user_id;type:uuid"`
	WorkspaceID     *string    `gorm:"column:workspace_id;type:uuid"`
	ProjectID       *string    `gorm:"column:project_id;type:uuid"`
	IssueID         *string    `gorm:"column:issue_id;type:uuid"`
	CommentID       *string    `gorm:"column:comment_id;type:uuid"`
	PageID          *string    `gorm:"column:page_id;type:uuid"`
	EntityType      *string    `gorm:"column:entity_type"`
	IsDeleted       bool       `gorm:"column:is_deleted"`
	IsUploaded      bool       `gorm:"column:is_uploaded"`
	Size            float64    `gorm:"column:size"`
	StorageMetadata []byte     `gorm:"column:storage_metadata;type:jsonb"`
}

func (FileAsset) TableName() string { return "file_assets" }

// assetEntityTypes is FileAsset.EntityTypeContext, which is what an upload has to name itself as.
var assetEntityTypes = map[string]bool{
	"ISSUE_ATTACHMENT": true, "ISSUE_DESCRIPTION": true, "COMMENT_DESCRIPTION": true,
	"PAGE_DESCRIPTION": true, "USER_COVER": true, "USER_AVATAR": true,
	"WORKSPACE_LOGO": true, "PROJECT_COVER": true,
	"DRAFT_ISSUE_ATTACHMENT": true, "DRAFT_ISSUE_DESCRIPTION": true,
}

// assetImageTypes is the list this endpoint holds an upload to, which is images alone however it names itself — so an ISSUE_ATTACHMENT reserved here cannot be a pdf, while the same entity reserved through the project route can.
var assetImageTypes = map[string]bool{
	"image/jpeg": true, "image/png": true, "image/webp": true, "image/jpg": true, "image/gif": true,
}

// scriptCapableMimeTypes is settings.SCRIPT_CAPABLE_MIME_TYPES: the seven types a browser would execute if it rendered them inline. The static route serves one of those as an attachment rather than inline, which is what stops an uploaded SVG running as script on the application's own origin.
var scriptCapableMimeTypes = map[string]bool{
	"image/svg+xml": true, "text/javascript": true, "application/javascript": true,
	"text/html": true, "application/xhtml+xml": true, "text/xml": true, "application/xml": true,
}

// entityColumn is the column an entity identifier is written to, which depends on what the asset says it is. An entity nobody recognises writes no column at all and the identifier is dropped.
func entityColumn(entityType string) string {
	switch entityType {
	case "WORKSPACE_LOGO":
		return "workspace_id"
	case "PROJECT_COVER":
		return "project_id"
	case "USER_AVATAR", "USER_COVER":
		return "user_id"
	case "ISSUE_ATTACHMENT", "ISSUE_DESCRIPTION":
		return "issue_id"
	case "PAGE_DESCRIPTION":
		return "page_id"
	case "COMMENT_DESCRIPTION":
		return "comment_id"
	}
	return ""
}

// assetReserve makes a row and hands back somewhere to put the bytes.
//
// A workspace logo is the one entity with a role of its own: only a workspace admin may reserve one, whatever the route's own permission allows.
func (handler *Handler) assetReserve(c *gin.Context, user *auth.User) {
	role, err := handler.workspaceRole(c.Request.Context(), c.Param("slug"), user.ID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if role == 0 {
		forbidden(c)
		return
	}
	var payload map[string]any
	if err := c.ShouldBindJSON(&payload); err != nil {
		handler.invalidDetail(c)
		return
	}
	rawName, _ := payload["name"].(string)
	name := uploads.SanitizeFilename(rawName)
	if name == "" {
		name = "unnamed"
	}
	fileType := "image/jpeg"
	if value, present := payload["type"].(string); present {
		fileType = value
	}
	size := float64(handler.settings.FileSizeLimit)
	if value, present := payload["size"].(float64); present {
		size = value
	}
	entityType, _ := payload["entity_type"].(string)
	if !assetEntityTypes[entityType] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid entity type.", "status": false})
		return
	}
	if entityType == "WORKSPACE_LOGO" && role != roleAdmin {
		c.JSON(http.StatusForbidden, gin.H{"error": "Only workspace admins can upload a workspace logo."})
		return
	}
	if !assetImageTypes[fileType] {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":  "Invalid file type. Only JPEG, PNG, WebP, JPG and GIF files are allowed.",
			"status": false,
		})
		return
	}
	if limit := float64(handler.settings.FileSizeLimit); size > limit {
		size = limit
	}
	if handler.storage == nil {
		handler.internalError(c, errNoAssetStore)
		return
	}

	var workspaceIDs []string
	err = handler.db.WithContext(c.Request.Context()).Table("workspaces").
		Where("slug = ?", c.Param("slug")).Limit(1).Pluck("id", &workspaceIDs).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if len(workspaceIDs) == 0 {
		handler.notFound(c)
		return
	}
	identifier, err := newUUID()
	if err != nil {
		handler.internalError(c, err)
		return
	}
	key, err := newUUID()
	if err != nil {
		handler.internalError(c, err)
		return
	}
	assetKey := workspaceIDs[0] + "/" + strings.ReplaceAll(key, "-", "") + "-" + name
	attributes, err := json.Marshal(map[string]any{"name": name, "type": fileType, "size": size})
	if err != nil {
		handler.internalError(c, err)
		return
	}
	now := handler.clock().UTC()
	row := map[string]any{
		"id": identifier, "created_at": now, "updated_at": now, "created_by_id": user.ID,
		"attributes": auth.JSONValue(attributes), "asset": assetKey, "size": size,
		"workspace_id": workspaceIDs[0], "entity_type": entityType,
		"is_deleted": false, "is_archived": false, "is_uploaded": false,
	}
	// The identifier is written to whichever column the entity names, and an entity that names none drops it.
	if column := entityColumn(entityType); column != "" {
		if entityIdentifier, present := payload["entity_identifier"].(string); present && entityIdentifier != "" {
			row[column] = entityIdentifier
		}
	}
	if err := handler.db.WithContext(c.Request.Context()).Table("file_assets").Create(row).Error; err != nil {
		handler.internalError(c, err)
		return
	}
	target, err := handler.storage.ForRequest(c.Request).PresignedUpload(c.Request.Context(), assetKey, fileType, int64(size))
	if err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, gin.H{
		"upload_data": gin.H{"url": target.URL, "fields": target.Fields},
		"asset_id":    identifier,
		"asset_url":   "/api/assets/v2/static/" + identifier + "/",
	})
}

// assetMarkUploaded is the callback once the bytes are in place. It also **moves the asset onto its entity** — a workspace logo or a project cover replaces whatever was there, and the one it replaces is marked deleted.
func (handler *Handler) assetMarkUploaded(c *gin.Context, user *auth.User) {
	asset, ok := handler.assetForWriting(c, user)
	if !ok {
		return
	}
	var payload map[string]json.RawMessage
	_ = c.ShouldBindJSON(&payload)
	updates := map[string]any{"is_uploaded": true}
	if raw, given := payload["attributes"]; given && json.Valid(raw) {
		updates["attributes"] = auth.JSONValue(append([]byte(nil), raw...))
	}
	now := handler.clock().UTC()
	err := handler.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Table("file_assets").Where("id = ?", asset.ID).Updates(updates).Error; err != nil {
			return err
		}
		return handler.attachAssetToEntity(tx, asset, now)
	})
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if len(asset.StorageMetadata) == 0 || string(asset.StorageMetadata) == "null" {
		if handler.tasks != nil {
			if err := handler.tasks.PublishAssetObjectMetadata(c.Request.Context(), asset.ID); err != nil {
				handler.internalError(c, err)
				return
			}
		}
	}
	if err := handler.invalidateWorkspaceListings(c.Request.Context(), asset); err != nil {
		handler.internalError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// attachAssetToEntity is entity_asset_save, which only two entities have: a workspace logo and a project cover. Both clear the url the entity used to carry, so an uploaded image always wins over a linked one.
func (handler *Handler) attachAssetToEntity(tx *gorm.DB, asset FileAsset, now time.Time) error {
	switch stringOrNil(asset.EntityType) {
	case "WORKSPACE_LOGO":
		if asset.WorkspaceID == nil {
			return nil
		}
		// []sql.NullString rather than []*string: Pluck does not honour a pointer element type, so a null column ends the request with `converting NULL to string is unsupported` -- which is every first time, before the row has ever been set.
		var previous []sql.NullString
		err := tx.Table("workspaces").Where("id = ?", *asset.WorkspaceID).Limit(1).Pluck("logo_asset_id", &previous).Error
		if err != nil || len(previous) == 0 {
			return err
		}
		if previous[0].Valid {
			if err := markAssetDeleted(tx, previous[0].String, now); err != nil {
				return err
			}
		}
		return tx.Table("workspaces").Where("id = ?", *asset.WorkspaceID).
			Updates(map[string]any{"logo": "", "logo_asset_id": asset.ID, "updated_at": now}).Error
	case "PROJECT_COVER":
		if asset.ProjectID == nil {
			return nil
		}
		var previous []sql.NullString
		err := tx.Table("projects").Where("id = ?", *asset.ProjectID).Limit(1).Pluck("cover_image_asset_id", &previous).Error
		if err != nil || len(previous) == 0 {
			return err
		}
		if previous[0].Valid {
			if err := markAssetDeleted(tx, previous[0].String, now); err != nil {
				return err
			}
		}
		return tx.Table("projects").Where("id = ?", *asset.ProjectID).
			Updates(map[string]any{"cover_image": "", "cover_image_asset_id": asset.ID, "updated_at": now}).Error
	}
	return nil
}

// markAssetDeleted flags an asset as gone. It writes both columns, so a query that reads either one agrees.
func markAssetDeleted(tx *gorm.DB, assetID string, now time.Time) error {
	return tx.Table("file_assets").Where("id = ?", assetID).
		Updates(map[string]any{"is_deleted": true, "deleted_at": now}).Error
}

// assetDestroy marks an asset deleted and takes it off whatever was wearing it.
func (handler *Handler) assetDestroy(c *gin.Context, user *auth.User) {
	asset, ok := handler.assetForWriting(c, user)
	if !ok {
		return
	}
	now := handler.clock().UTC()
	err := handler.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		switch stringOrNil(asset.EntityType) {
		case "WORKSPACE_LOGO":
			if asset.WorkspaceID != nil {
				err := tx.Table("workspaces").Where("id = ?", *asset.WorkspaceID).
					Updates(map[string]any{"logo_asset_id": nil, "updated_at": now}).Error
				if err != nil {
					return err
				}
			}
		case "PROJECT_COVER":
			if asset.ProjectID != nil {
				err := tx.Table("projects").Where("id = ?", *asset.ProjectID).
					Updates(map[string]any{"cover_image_asset_id": nil, "updated_at": now}).Error
				if err != nil {
					return err
				}
			}
		}
		return markAssetDeleted(tx, asset.ID, now)
	})
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if err := handler.invalidateWorkspaceListings(c.Request.Context(), asset); err != nil {
		handler.internalError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// assetDownload answers with a **redirect** to the bytes, always as an attachment.
func (handler *Handler) assetDownload(c *gin.Context, user *auth.User) {
	asset, ok := handler.assetForWriting(c, user)
	if !ok {
		return
	}
	if !asset.IsUploaded {
		c.JSON(http.StatusNotFound, gin.H{"error": "The requested asset could not be found."})
		return
	}
	handler.redirectToAsset(c, asset, "attachment")
}

// assetAttachment is the download route, which differs from the one above only in reading an uploaded asset directly rather than checking the flag afterwards — so an asset that is not uploaded is a 404 either way.
func (handler *Handler) assetAttachment(c *gin.Context, user *auth.User) {
	role, err := handler.workspaceRole(c.Request.Context(), c.Param("slug"), user.ID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if role == 0 {
		forbidden(c)
		return
	}
	asset, found, err := handler.assetByID(c, true)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "The requested asset could not be found."})
		return
	}
	handler.redirectToAsset(c, asset, "attachment")
}

// assetCheck reports whether an asset is there, and answers **200 with false** rather than a 404 when it is not.
func (handler *Handler) assetCheck(c *gin.Context, user *auth.User) {
	role, err := handler.workspaceRole(c.Request.Context(), c.Param("slug"), user.ID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if role == 0 {
		forbidden(c)
		return
	}
	var count int64
	err = handler.db.WithContext(c.Request.Context()).Table("file_assets fa").
		Joins("JOIN workspaces w ON w.id = fa.workspace_id").
		Where("fa.id = ? AND w.slug = ? AND fa.deleted_at IS NULL", c.Param("asset"), c.Param("slug")).
		Count(&count).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, gin.H{"exists": count > 0})
}

// assetRestore brings a deleted asset back. It reads through the manager that shows deleted rows, which is the only route here that does.
func (handler *Handler) assetRestore(c *gin.Context, user *auth.User) {
	role, err := handler.workspaceRole(c.Request.Context(), c.Param("slug"), user.ID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if role == 0 {
		forbidden(c)
		return
	}
	var assets []FileAsset
	err = handler.db.WithContext(c.Request.Context()).Table("file_assets fa").Select("fa.*").
		Joins("JOIN workspaces w ON w.id = fa.workspace_id").
		Where("fa.id = ? AND w.slug = ?", c.Param("asset"), c.Param("slug")).
		Limit(1).Scan(&assets).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if len(assets) == 0 {
		handler.notFound(c)
		return
	}
	err = handler.db.WithContext(c.Request.Context()).Table("file_assets").Where("id = ?", assets[0].ID).
		Updates(map[string]any{"is_deleted": false, "deleted_at": nil}).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// staticAsset is what every avatar and logo url points at. It asks for no session at all, and answers a redirect to the bytes.
//
// Only the four entities a person or a workspace wears are served here; anything else is refused, which is what keeps a work item's attachment off an unauthenticated route. A type a browser would execute is served as an attachment rather than inline.
func (handler *Handler) staticAsset(c *gin.Context) {
	var assets []FileAsset
	err := handler.db.WithContext(c.Request.Context()).Table("file_assets").
		Where("id = ?", c.Param("asset")).Limit(1).Scan(&assets).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if len(assets) == 0 {
		handler.notFound(c)
		return
	}
	asset := assets[0]
	if !asset.IsUploaded {
		c.JSON(http.StatusNotFound, gin.H{"error": "The requested asset could not be found."})
		return
	}
	switch stringOrNil(asset.EntityType) {
	case "USER_AVATAR", "USER_COVER", "WORKSPACE_LOGO", "PROJECT_COVER":
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid entity type.", "status": false})
		return
	}
	disposition := "inline"
	if scriptCapableMimeTypes[assetMimeType(asset)] {
		disposition = "attachment"
	}
	if handler.storage == nil {
		handler.internalError(c, errNoAssetStore)
		return
	}
	// The static route signs without a filename, so the browser keeps the name the object has in the bucket.
	url, err := handler.storage.ForRequest(c.Request).PresignedDownload(c.Request.Context(), asset.Asset, disposition, "")
	if err != nil {
		handler.internalError(c, err)
		return
	}
	c.Redirect(http.StatusFound, url)
}

func (handler *Handler) redirectToAsset(c *gin.Context, asset FileAsset, disposition string) {
	if handler.storage == nil {
		handler.internalError(c, errNoAssetStore)
		return
	}
	attributes := map[string]any{}
	_ = json.Unmarshal(asset.Attributes, &attributes)
	filename, _ := attributes["name"].(string)
	url, err := handler.storage.ForRequest(c.Request).PresignedDownload(c.Request.Context(), asset.Asset, disposition, filename)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	c.Redirect(http.StatusFound, url)
}

// assetForWriting reads the asset the three detail routes act on and applies the project check.
//
// The route is authorised at the **workspace** level, so an asset bound to a project needs a membership of that project as well — otherwise a workspace guest could reach a project they are not in.
func (handler *Handler) assetForWriting(c *gin.Context, user *auth.User) (FileAsset, bool) {
	role, err := handler.workspaceRole(c.Request.Context(), c.Param("slug"), user.ID)
	if err != nil {
		handler.internalError(c, err)
		return FileAsset{}, false
	}
	if role == 0 {
		forbidden(c)
		return FileAsset{}, false
	}
	asset, found, err := handler.assetByID(c, false)
	if err != nil {
		handler.internalError(c, err)
		return FileAsset{}, false
	}
	if !found {
		handler.notFound(c)
		return FileAsset{}, false
	}
	if asset.ProjectID != nil {
		var member int64
		err := handler.db.WithContext(c.Request.Context()).Table("project_members").
			Where("member_id = ? AND workspace_id = ? AND project_id = ? AND is_active = TRUE",
				user.ID, stringOrNil(asset.WorkspaceID), *asset.ProjectID).Count(&member).Error
		if err != nil {
			handler.internalError(c, err)
			return FileAsset{}, false
		}
		if member == 0 {
			c.JSON(http.StatusForbidden, gin.H{"error": "You don't have access to this asset."})
			return FileAsset{}, false
		}
	}
	return asset, true
}

func (handler *Handler) assetByID(c *gin.Context, uploadedOnly bool) (FileAsset, bool, error) {
	query := handler.db.WithContext(c.Request.Context()).Table("file_assets fa").Select("fa.*").
		Joins("JOIN workspaces w ON w.id = fa.workspace_id").
		Where("fa.id = ? AND w.slug = ? AND fa.deleted_at IS NULL", c.Param("asset"), c.Param("slug"))
	if uploadedOnly {
		query = query.Where("fa.is_uploaded = TRUE")
	}
	var assets []FileAsset
	if err := query.Limit(1).Scan(&assets).Error; err != nil || len(assets) == 0 {
		return FileAsset{}, false, err
	}
	return assets[0], true, nil
}

// invalidateWorkspaceListings drops the three cached listings a workspace logo appears in. A project cover appears in none of them, so nothing is dropped for one.
func (handler *Handler) invalidateWorkspaceListings(ctx context.Context, asset FileAsset) error {
	if handler.cache == nil || stringOrNil(asset.EntityType) != "WORKSPACE_LOGO" {
		return nil
	}
	for _, pattern := range []string{"*/api/workspaces/*", "*/api/users/me/workspaces/*", "*/api/instances/*"} {
		if err := handler.cache.InvalidatePattern(ctx, pattern); err != nil {
			return err
		}
	}
	return nil
}

// assetMimeType is the type an asset says it is, cut down to the media type alone and lowercased.
func assetMimeType(asset FileAsset) string {
	attributes := map[string]any{}
	_ = json.Unmarshal(asset.Attributes, &attributes)
	value, _ := attributes["type"].(string)
	return strings.ToLower(strings.TrimSpace(strings.Split(value, ";")[0]))
}

func stringOrNil(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

var errNoAssetStore = &assetError{"object storage is not configured"}

type assetError struct{ message string }

func (err *assetError) Error() string { return err.message }
