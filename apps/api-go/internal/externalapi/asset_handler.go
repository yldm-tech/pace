package externalapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"github.com/yldm-tech/pace/apps/api-go/internal/drf"
	"github.com/yldm-tech/pace/apps/api-go/internal/uploads"
)

func (handler *Handler) registerAssetRoutes(router gin.IRouter) {
	router.POST("/api/v1/workspaces/:slug/assets/", handler.authenticated(handler.assetReserve))
	router.GET("/api/v1/workspaces/:slug/assets/:asset/", handler.authenticated(handler.assetDownload))
	router.PATCH("/api/v1/workspaces/:slug/assets/:asset/", handler.authenticated(handler.assetMarkUploaded))
}

// FileAsset is the db.FileAsset table as the external API sees it.
type FileAsset struct {
	ID              string     `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt       time.Time  `gorm:"column:created_at"`
	UpdatedAt       time.Time  `gorm:"column:updated_at"`
	CreatedByID     *string    `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID     *string    `gorm:"column:updated_by_id;type:uuid"`
	DeletedAt       *time.Time `gorm:"column:deleted_at"`
	Attributes      []byte     `gorm:"column:attributes;type:jsonb"`
	Asset           string     `gorm:"column:asset"`
	WorkspaceID     *string    `gorm:"column:workspace_id;type:uuid"`
	ProjectID       *string    `gorm:"column:project_id;type:uuid"`
	EntityType      *string    `gorm:"column:entity_type"`
	IsDeleted       bool       `gorm:"column:is_deleted"`
	ExternalID      *string    `gorm:"column:external_id"`
	ExternalSource  *string    `gorm:"column:external_source"`
	Size            float64    `gorm:"column:size"`
	IsUploaded      bool       `gorm:"column:is_uploaded"`
	StorageMetadata []byte     `gorm:"column:storage_metadata;type:jsonb"`
	// NOT NULL with no database default. A column the struct has no field for is one GORM does not write, which is a null rather than the false Django would have put there.
	IsArchived bool `gorm:"column:is_archived"`
}

func (FileAsset) TableName() string { return "file_assets" }

// scriptCapableMimeTypes is settings.SCRIPT_CAPABLE_MIME_TYPES: the seven types a browser would execute if it rendered them inline.
//
// An asset of one of these is served as an **attachment** rather than inline, which is what stops an uploaded SVG or HTML file running as script on the workspace's own origin.
var scriptCapableMimeTypes = map[string]bool{
	"image/svg+xml": true, "text/javascript": true, "application/javascript": true,
	"text/html": true, "application/xhtml+xml": true, "text/xml": true, "application/xml": true,
}

// assetReserve makes a row and hands back somewhere to put the bytes.
//
// It answers **200** rather than 201, and the row exists before the upload does — an integration that never finishes the upload leaves a row behind that no route cleans up.
func (handler *Handler) assetReserve(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireWorkspaceUser(c, user) {
		return
	}
	if handler.assets == nil {
		handler.serverError(c, errNoAssetStore)
		return
	}
	slug := c.Param("slug")
	var payload map[string]any
	if err := c.ShouldBindJSON(&payload); err != nil {
		invalidPayload(c)
		return
	}

	rawName, _ := payload["name"].(string)
	name := uploads.SanitizeFilename(rawName)
	fileType, _ := payload["type"].(string)
	// The size defaults to the cap rather than to zero, so an integration that omits it reserves the largest allowed upload.
	size := float64(handler.settings.FileSizeLimit)
	if value, present := payload["size"]; present {
		parsed, ok := assetSize(value)
		if !ok {
			handler.serverError(c, errBadAssetSize)
			return
		}
		size = parsed
	}
	// The guard tests the size for truthiness, so a size of zero is refused along with a missing name.
	if name == "" || size == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Name and size are required fields.", "status": false})
		return
	}
	if fileType == "" || !uploads.AttachmentMimeTypes[fileType] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid file type.", "status": false})
		return
	}
	if limit := float64(handler.settings.FileSizeLimit); size > limit {
		size = limit
	}

	var workspaceIDs []string
	err := handler.db.WithContext(c.Request.Context()).Table("workspaces").
		Where("slug = ?", slug).Limit(1).Pluck("id", &workspaceIDs).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if len(workspaceIDs) == 0 {
		notFound(c)
		return
	}

	externalID, _ := payload["external_id"].(string)
	externalSource, _ := payload["external_source"].(string)
	if externalID != "" && externalSource != "" {
		var existing []FileAsset
		err := handler.db.WithContext(c.Request.Context()).Table("file_assets fa").Select("fa.*").
			Joins("JOIN workspaces w ON w.id = fa.workspace_id").
			Where("w.slug = ? AND fa.external_source = ? AND fa.external_id = ? AND fa.is_deleted = FALSE",
				slug, externalSource, externalID).
			Order("fa.created_at").Limit(1).Scan(&existing).Error
		if err != nil {
			handler.serverError(c, err)
			return
		}
		if len(existing) > 0 {
			// The conflict says "message" rather than "error", which is the only place in this app that key appears.
			c.JSON(http.StatusConflict, gin.H{
				"message":   "Asset with same external id and source already exists",
				"asset_id":  existing[0].ID,
				"asset_url": assetURL(existing[0]),
			})
			return
		}
	}

	identifier, err := newUUID()
	if err != nil {
		handler.serverError(c, err)
		return
	}
	key, err := newUUID()
	if err != nil {
		handler.serverError(c, err)
		return
	}
	assetKey := workspaceIDs[0] + "/" + strings.ReplaceAll(key, "-", "") + "-" + name
	now := handler.clock().UTC()
	attributes, err := assetAttributes(name, fileType, size)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	entityType := "ISSUE_ATTACHMENT"
	asset := FileAsset{
		ID: identifier, CreatedAt: now, UpdatedAt: now, CreatedByID: &user.ID,
		Attributes: attributes, Asset: assetKey, Size: size,
		WorkspaceID: &workspaceIDs[0], EntityType: &entityType,
	}
	if value, ok := payload["project_id"].(string); ok && value != "" {
		asset.ProjectID = &value
	}
	if externalID != "" {
		asset.ExternalID = &externalID
	}
	if externalSource != "" {
		asset.ExternalSource = &externalSource
	}
	if err := handler.db.WithContext(c.Request.Context()).Create(&asset).Error; err != nil {
		handler.serverError(c, err)
		return
	}

	target, err := handler.assets.PresignedUpload(c.Request.Context(), assetKey, fileType, int64(size))
	if err != nil {
		handler.serverError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, gin.H{
		"upload_data": gin.H{"url": target.URL, "fields": target.Fields},
		"asset_id":    asset.ID,
		"asset_url":   assetURL(asset),
	})
}

// assetDownload hands back a link to the bytes, refusing one whose upload never finished.
func (handler *Handler) assetDownload(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireWorkspaceUser(c, user) {
		return
	}
	if handler.assets == nil {
		handler.serverError(c, errNoAssetStore)
		return
	}
	var workspaceIDs []string
	err := handler.db.WithContext(c.Request.Context()).Table("workspaces").
		Where("slug = ?", c.Param("slug")).Limit(1).Pluck("id", &workspaceIDs).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if len(workspaceIDs) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Workspace not found"})
		return
	}
	var assets []FileAsset
	err = handler.db.WithContext(c.Request.Context()).Table("file_assets").
		Where("id = ? AND workspace_id = ? AND is_deleted = FALSE", c.Param("asset"), workspaceIDs[0]).
		Limit(1).Scan(&assets).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if len(assets) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Asset not found"})
		return
	}
	asset := assets[0]
	if !asset.IsUploaded {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Asset not yet uploaded"})
		return
	}

	attributes, _ := decodeJSON(asset.Attributes).(map[string]any)
	stored, _ := attributes["type"].(string)
	// The type is cut at the first semicolon and lowercased before the list is consulted, so "Text/HTML; charset=utf-8" is recognised.
	mime := strings.ToLower(strings.TrimSpace(strings.Split(stored, ";")[0]))
	disposition := "inline"
	if scriptCapableMimeTypes[mime] {
		disposition = "attachment"
	}
	filename, _ := attributes["name"].(string)
	url, err := handler.assets.PresignedDownload(c.Request.Context(), asset.Asset, disposition, filename)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	name, _ := attributes["name"].(string)
	assetType, _ := attributes["type"].(string)
	drf.Respond(c, http.StatusOK, gin.H{
		"asset_id": asset.ID, "asset_url": url, "asset_name": name, "asset_type": assetType,
	})
}

// assetMarkUploaded is the callback an integration makes once the bytes are in place.
//
// The metadata task is queued **before** the flag is written and regardless of whether the request actually turned it on, so a repeated call with no body queues it again for an asset that already has metadata.
func (handler *Handler) assetMarkUploaded(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireWorkspaceUser(c, user) {
		return
	}
	var payload map[string]any
	if err := c.ShouldBindJSON(&payload); err != nil {
		invalidPayload(c)
		return
	}
	var assets []FileAsset
	err := handler.db.WithContext(c.Request.Context()).Table("file_assets fa").Select("fa.*").
		Joins("JOIN workspaces w ON w.id = fa.workspace_id").
		Where("w.slug = ? AND fa.id = ? AND fa.is_deleted = FALSE", c.Param("slug"), c.Param("asset")).
		Limit(1).Scan(&assets).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if len(assets) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Asset not found"})
		return
	}
	asset := assets[0]
	uploaded := asset.IsUploaded
	if value, ok := payload["is_uploaded"].(bool); ok {
		uploaded = value
	}
	if len(asset.StorageMetadata) == 0 || string(asset.StorageMetadata) == "null" {
		if handler.tasks != nil {
			if err := handler.tasks.PublishAssetObjectMetadata(c.Request.Context(), asset.ID); err != nil {
				handler.serverError(c, err)
				return
			}
		}
	}
	// Only the flag is written: the save names one field, so nothing else the request carried lands.
	err = handler.db.WithContext(c.Request.Context()).Table("file_assets").
		Where("id = ?", asset.ID).Update("is_uploaded", uploaded).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// assetURL is the path the frontend fetches an asset through.
func assetURL(asset FileAsset) string {
	return "/api/assets/v2/static/" + asset.ID + "/"
}

// assetSize reads the size, which Django casts with int() — so a string of digits works and anything else raises.
func assetSize(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return float64(int64(typed)), true
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		if err != nil {
			return 0, false
		}
		return float64(parsed), true
	}
	return 0, false
}

var errNoAssetStore = &orderError{"the asset store is not configured"}
var errBadAssetSize = &orderError{"the size is not a number"}

// assetAttributes is the attributes column: the three things the upload was reserved with.
func assetAttributes(name, fileType string, size float64) ([]byte, error) {
	return json.Marshal(map[string]any{"name": name, "type": fileType, "size": size})
}
