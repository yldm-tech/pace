package space

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"github.com/yldm-tech/pace/apps/api-go/internal/drf"
	"github.com/yldm-tech/pace/apps/api-go/internal/storage"
	"github.com/yldm-tech/pace/apps/api-go/internal/uploads"
)

func (handler *Handler) registerAssetRoutes(router gin.IRouter) {
	const base = "/api/public/assets/v2/anchor/:anchor/"
	// The read is the only asset route here without a session: a published board's images are public and everything that writes one is not.
	router.GET(base+":asset/", handler.assetDownload)
	router.POST(base, handler.authenticated(handler.assetReserve))
	router.PATCH(base+":asset/", handler.authenticated(handler.assetMarkUploaded))
	router.DELETE(base+":asset/", handler.authenticated(handler.assetDestroy))
	router.POST(base+"restore/:asset/", handler.authenticated(handler.assetRestore))
	router.POST(base+":asset/bulk/", handler.authenticated(handler.assetBulk))
}

// SetStorage wires the object store these routes sign against.
func (handler *Handler) SetStorage(store *storage.Store) { handler.storage = store }

// FileAsset is the db.FileAsset table as the published board sees one.
type FileAsset struct {
	ID              string     `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt       time.Time  `gorm:"column:created_at"`
	UpdatedAt       time.Time  `gorm:"column:updated_at"`
	DeletedAt       *time.Time `gorm:"column:deleted_at"`
	Attributes      []byte     `gorm:"column:attributes;type:jsonb"`
	Asset           string     `gorm:"column:asset"`
	WorkspaceID     *string    `gorm:"column:workspace_id;type:uuid"`
	ProjectID       *string    `gorm:"column:project_id;type:uuid"`
	CommentID       *string    `gorm:"column:comment_id;type:uuid"`
	EntityType      *string    `gorm:"column:entity_type"`
	IsUploaded      bool       `gorm:"column:is_uploaded"`
	Size            float64    `gorm:"column:size"`
	StorageMetadata []byte     `gorm:"column:storage_metadata;type:jsonb"`
}

func (FileAsset) TableName() string { return "file_assets" }

// spaceAssetEntityTypes is what an upload may name itself here. The list is the whole of FileAsset's, which is wider than what the read will serve.
var spaceAssetEntityTypes = map[string]bool{
	"ISSUE_ATTACHMENT": true, "ISSUE_DESCRIPTION": true, "COMMENT_DESCRIPTION": true,
	"PAGE_DESCRIPTION": true, "USER_COVER": true, "USER_AVATAR": true,
	"WORKSPACE_LOGO": true, "PROJECT_COVER": true,
	"DRAFT_ISSUE_ATTACHMENT": true, "DRAFT_ISSUE_DESCRIPTION": true,
}

var spaceAssetImageTypes = map[string]bool{
	"image/jpeg": true, "image/png": true, "image/webp": true, "image/jpg": true, "image/gif": true,
}

// assetDownload serves an asset the board holds, and only one of the **two** entity types a reader could have put there — a work item's description or a comment's. Anything else is out of reach here whatever the board is published as.
func (handler *Handler) assetDownload(c *gin.Context) {
	board, found, err := handler.boardByAnchor(c)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "Requested resource could not be found."})
		return
	}
	var assets []FileAsset
	err = handler.db.WithContext(c.Request.Context()).Table("file_assets").
		Where(`id = ? AND workspace_id = ? AND project_id = ?
			AND entity_type IN ('ISSUE_DESCRIPTION','COMMENT_DESCRIPTION') AND deleted_at IS NULL`,
			c.Param("asset"), board.WorkspaceID, board.ProjectID).
		Limit(1).Scan(&assets).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if len(assets) == 0 {
		notFound(c)
		return
	}
	asset := assets[0]
	if !asset.IsUploaded {
		c.JSON(http.StatusNotFound, gin.H{"error": "The requested asset could not be found."})
		return
	}
	if handler.storage == nil {
		handler.serverError(c, errNoAssetStore)
		return
	}
	// A type a browser would execute is served as an attachment rather than inline, which is what stops an uploaded SVG running as script on the board's own origin.
	disposition := "inline"
	if scriptCapableMimeTypes[assetMimeType(asset)] {
		disposition = "attachment"
	}
	url, err := handler.storage.ForRequest(c.Request).PresignedDownload(c.Request.Context(), asset.Asset, disposition, "")
	if err != nil {
		handler.serverError(c, err)
		return
	}
	c.Redirect(http.StatusFound, url)
}

// assetReserve makes a row for something a reader is uploading into a comment.
//
// Whatever entity the payload names, the identifier it carries is written to **comment_id** and to nothing else — so an upload calling itself a work item description still lands on a comment, or on no comment at all.
func (handler *Handler) assetReserve(c *gin.Context, user *auth.User) {
	board, found, err := handler.boardByAnchor(c)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "Project is not published"})
		return
	}
	var payload map[string]any
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid entity type.", "status": false})
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
	size := float64(handler.fileSizeLimit)
	if value, present := payload["size"].(float64); present {
		size = value
	}
	entityType, _ := payload["entity_type"].(string)
	if !spaceAssetEntityTypes[entityType] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid entity type.", "status": false})
		return
	}
	if !spaceAssetImageTypes[fileType] {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":  "Invalid file type. Only JPEG, PNG, WebP, JPG and GIF files are allowed.",
			"status": false,
		})
		return
	}
	// The size is clamped at both ends here, unlike every other reserve, so an upload of nothing still carries a policy the bucket accepts.
	if limit := float64(handler.fileSizeLimit); size > limit {
		size = limit
	}
	if size < 1 {
		size = 1
	}
	if handler.storage == nil {
		handler.serverError(c, errNoAssetStore)
		return
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
	assetKey := board.WorkspaceID + "/" + strings.ReplaceAll(key, "-", "") + "-" + name
	attributes, err := json.Marshal(map[string]any{"name": name, "type": fileType, "size": size})
	if err != nil {
		handler.serverError(c, err)
		return
	}
	now := handler.clock().UTC()
	row := map[string]any{
		"id": identifier, "created_at": now, "updated_at": now, "created_by_id": user.ID,
		"attributes": auth.JSONValue(attributes), "asset": assetKey, "size": size,
		"workspace_id": board.WorkspaceID, "project_id": board.ProjectID, "entity_type": entityType,
		"is_deleted": false, "is_archived": false, "is_uploaded": false,
	}
	if entityIdentifier, present := payload["entity_identifier"].(string); present && entityIdentifier != "" {
		row["comment_id"] = entityIdentifier
	}
	if err := handler.db.WithContext(c.Request.Context()).Table("file_assets").Create(row).Error; err != nil {
		handler.serverError(c, err)
		return
	}
	target, err := handler.storage.ForRequest(c.Request).PresignedUpload(c.Request.Context(), assetKey, fileType, int64(size))
	if err != nil {
		handler.serverError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, gin.H{
		"upload_data": gin.H{"url": target.URL, "fields": target.Fields},
		"asset_id":    identifier,
		"asset_url":   "/api/assets/v2/static/" + identifier + "/",
	})
}

func (handler *Handler) assetMarkUploaded(c *gin.Context, user *auth.User) {
	board, asset, ok := handler.boardAsset(c, false)
	if !ok {
		return
	}
	_ = board
	var payload map[string]json.RawMessage
	_ = c.ShouldBindJSON(&payload)
	updates := map[string]any{"is_uploaded": true}
	if raw, given := payload["attributes"]; given && json.Valid(raw) {
		updates["attributes"] = auth.JSONValue(append([]byte(nil), raw...))
	}
	err := handler.db.WithContext(c.Request.Context()).Table("file_assets").
		Where("id = ?", asset.ID).Updates(updates).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (handler *Handler) assetDestroy(c *gin.Context, user *auth.User) {
	_, asset, ok := handler.boardAsset(c, true)
	if !ok {
		return
	}
	now := handler.clock().UTC()
	err := handler.db.WithContext(c.Request.Context()).Table("file_assets").Where("id = ?", asset.ID).
		Updates(map[string]any{"is_deleted": true, "deleted_at": now}).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// assetRestore brings a deleted asset back, reading through the manager that shows deleted rows.
func (handler *Handler) assetRestore(c *gin.Context, user *auth.User) {
	board, found, err := handler.projectBoardByAnchor(c)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "Project is not published"})
		return
	}
	var assets []FileAsset
	err = handler.db.WithContext(c.Request.Context()).Table("file_assets").
		Where("id = ? AND workspace_id = ? AND project_id = ?", c.Param("asset"), board.WorkspaceID, board.ProjectID).
		Limit(1).Scan(&assets).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if len(assets) == 0 {
		notFound(c)
		return
	}
	err = handler.db.WithContext(c.Request.Context()).Table("file_assets").Where("id = ?", assets[0].ID).
		Updates(map[string]any{"is_deleted": false, "deleted_at": nil}).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// assetBulk claims uploads for a comment, and **only** for a comment: the entity is read off the first asset the query finds and anything that is not a comment description is left where it is.
func (handler *Handler) assetBulk(c *gin.Context, user *auth.User) {
	board, found, err := handler.projectBoardByAnchor(c)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "Project is not published"})
		return
	}
	var request struct {
		AssetIDs []string `json:"asset_ids"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No asset ids provided."})
		return
	}
	if len(request.AssetIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No asset ids provided."})
		return
	}
	var assets []FileAsset
	err = handler.db.WithContext(c.Request.Context()).Table("file_assets").
		Where("id IN ? AND workspace_id = ? AND project_id = ? AND deleted_at IS NULL",
			request.AssetIDs, board.WorkspaceID, board.ProjectID).
		Order("created_at").Scan(&assets).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if len(assets) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "The requested asset could not be found."})
		return
	}
	if stringOrNil(assets[0].EntityType) == "COMMENT_DESCRIPTION" {
		identifiers := make([]string, 0, len(assets))
		for _, asset := range assets {
			identifiers = append(identifiers, asset.ID)
		}
		err := handler.db.WithContext(c.Request.Context()).Table("file_assets").
			Where("id IN ?", identifiers).Update("comment_id", c.Param("asset")).Error
		if err != nil {
			handler.serverError(c, err)
			return
		}
	}
	c.Status(http.StatusNoContent)
}

// boardAsset reads the asset a write route names, scoped to the board's project.
//
// The delete asks that the board be a **project's** and the update does not, which is the one difference between the two lookups.
func (handler *Handler) boardAsset(c *gin.Context, projectBoard bool) (DeployBoard, FileAsset, bool) {
	var board DeployBoard
	var found bool
	var err error
	if projectBoard {
		board, found, err = handler.projectBoardByAnchor(c)
	} else {
		board, found, err = handler.boardByAnchor(c)
	}
	if err != nil {
		handler.serverError(c, err)
		return DeployBoard{}, FileAsset{}, false
	}
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "Project is not published"})
		return DeployBoard{}, FileAsset{}, false
	}
	var assets []FileAsset
	err = handler.db.WithContext(c.Request.Context()).Table("file_assets").
		Where("id = ? AND workspace_id = ? AND project_id = ? AND deleted_at IS NULL",
			c.Param("asset"), board.WorkspaceID, board.ProjectID).
		Limit(1).Scan(&assets).Error
	if err != nil {
		handler.serverError(c, err)
		return DeployBoard{}, FileAsset{}, false
	}
	if len(assets) == 0 {
		notFound(c)
		return DeployBoard{}, FileAsset{}, false
	}
	return board, assets[0], true
}

// scriptCapableMimeTypes is settings.SCRIPT_CAPABLE_MIME_TYPES: the seven types a browser would execute if it rendered them inline.
var scriptCapableMimeTypes = map[string]bool{
	"image/svg+xml": true, "text/javascript": true, "application/javascript": true,
	"text/html": true, "application/xhtml+xml": true, "text/xml": true, "application/xml": true,
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

var errNoAssetStore = &spaceError{"object storage is not configured"}
