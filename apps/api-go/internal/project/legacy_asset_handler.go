package project

import (
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"github.com/yldm-tech/pace/apps/api-go/internal/drf"
	"github.com/yldm-tech/pace/apps/api-go/internal/uploads"
)

// The v1 asset routes, which are the ones that still upload *through* the API.
//
// Everything the editor writes today goes through the v2 routes, where the browser is handed a signed policy and posts straight at the bucket. These take the bytes themselves — a multipart form reaches the API, the API writes the object, and only then is the row recorded. They are still here because older clients still call them.
func (handler *Handler) registerLegacyAssetRoutes(router gin.IRouter) {
	router.POST("/api/workspaces/:slug/file-assets/", handler.authenticated(handler.workspaceAssetUpload))
	router.GET("/api/workspaces/file-assets/:workspace/:key/", handler.authenticated(handler.workspaceAssetRead))
	router.DELETE("/api/workspaces/file-assets/:workspace/:key/", handler.authenticated(handler.workspaceAssetDelete))
	router.POST("/api/workspaces/file-assets/:workspace/:key/restore/", handler.authenticated(handler.workspaceAssetRestore))

	router.POST("/api/users/file-assets/", handler.authenticated(handler.userAssetUpload))
	router.GET("/api/users/file-assets/:key/", handler.authenticated(handler.userAssetRead))
	router.DELETE("/api/users/file-assets/:key/", handler.authenticated(handler.userAssetDelete))
}

// LegacyAsset is the db.FileAsset row these routes read and write. It is the same table the v2 routes use; only the way the bytes get there differs.
type LegacyAsset struct {
	ID               string     `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt        time.Time  `gorm:"column:created_at"`
	UpdatedAt        time.Time  `gorm:"column:updated_at"`
	CreatedByID      *string    `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID      *string    `gorm:"column:updated_by_id;type:uuid"`
	DeletedAt        *time.Time `gorm:"column:deleted_at"`
	Attributes       []byte     `gorm:"column:attributes;type:jsonb"`
	Asset            string     `gorm:"column:asset"`
	EntityType       *string    `gorm:"column:entity_type"`
	EntityIdentifier *string    `gorm:"column:entity_identifier"`
	IsDeleted        bool       `gorm:"column:is_deleted"`
	IsArchived       bool       `gorm:"column:is_archived"`
	ExternalID       *string    `gorm:"column:external_id"`
	ExternalSource   *string    `gorm:"column:external_source"`
	Size             *int64     `gorm:"column:size"`
	IsUploaded       bool       `gorm:"column:is_uploaded"`
	StorageMetadata  []byte     `gorm:"column:storage_metadata;type:jsonb"`
	UserID           *string    `gorm:"column:user_id;type:uuid"`
	WorkspaceID      *string    `gorm:"column:workspace_id;type:uuid"`
	DraftIssueID     *string    `gorm:"column:draft_issue_id;type:uuid"`
	ProjectID        *string    `gorm:"column:project_id;type:uuid"`
	IssueID          *string    `gorm:"column:issue_id;type:uuid"`
	CommentID        *string    `gorm:"column:comment_id;type:uuid"`
	PageID           *string    `gorm:"column:page_id;type:uuid"`
}

func (LegacyAsset) TableName() string { return "file_assets" }

// workspaceAssetUpload takes a file for a workspace.
func (handler *Handler) workspaceAssetUpload(c *gin.Context, user *auth.User) {
	if !handler.requireWorkspaceRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	var workspaceIDs []string
	err := handler.db.WithContext(c.Request.Context()).Table("workspaces").
		Where("slug = ? AND deleted_at IS NULL", c.Param("slug")).Limit(1).Pluck("id", &workspaceIDs).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if len(workspaceIDs) == 0 {
		handler.notFound(c)
		return
	}
	handler.storeLegacyAsset(c, user, &workspaceIDs[0])
}

// userAssetUpload takes a file that belongs to nobody but the person uploading it.
func (handler *Handler) userAssetUpload(c *gin.Context, user *auth.User) {
	handler.storeLegacyAsset(c, user, nil)
}

// storeLegacyAsset is the half both uploads share: read the file, put it in the bucket, record the row.
//
// The order matters and is upstream's. The object is written first and the row second, so an upload that fails at the row leaves an orphan in the bucket that nothing will ever point at. There is no sweep for those, because the sweep that exists looks for rows without objects rather than objects without rows.
func (handler *Handler) storeLegacyAsset(c *gin.Context, user *auth.User, workspaceID *string) {
	file, header, err := c.Request.FormFile("asset")
	if err != nil {
		// The serializer refuses a body with no file the way it refuses any missing required field.
		c.JSON(http.StatusBadRequest, gin.H{"asset": []string{"No file was submitted."}})
		return
	}
	defer file.Close()

	if handler.settings.FileSizeLimit > 0 && header.Size > handler.settings.FileSizeLimit {
		// The model's own validator, which names five megabytes whatever the limit really is.
		c.JSON(http.StatusBadRequest, gin.H{"asset": []string{"File too large. Size should not exceed 5 MB."}})
		return
	}
	payload, err := io.ReadAll(io.LimitReader(file, handler.settings.FileSizeLimit+1))
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if handler.settings.FileSizeLimit > 0 && int64(len(payload)) > handler.settings.FileSizeLimit {
		c.JSON(http.StatusBadRequest, gin.H{"asset": []string{"File too large. Size should not exceed 5 MB."}})
		return
	}

	key := legacyUploadPath(workspaceID, header.Filename)
	if handler.storage == nil {
		handler.internalError(c, errors.New("object storage is not configured"))
		return
	}
	err = handler.storage.PutObject(c.Request.Context(), key, headerContentType(header), payload, false)
	if err != nil {
		handler.internalError(c, err)
		return
	}

	now := handler.clock().UTC()
	row := LegacyAsset{
		ID: uuid.NewString(), CreatedAt: now, UpdatedAt: now,
		Asset: key, Size: int64Pointer(int64(len(payload))),
		WorkspaceID: workspaceID,
		Attributes:  legacyAttributes(c),
	}
	columns := map[string]any{
		"id": row.ID, "created_at": now, "updated_at": now,
		// The serializer names created_by read-only and BaseModel.save fills it from the request, so the row really does remember who uploaded it — unlike everything written from a worker.
		"created_by_id": user.ID, "updated_by_id": nil,
		"asset": key, "size": len(payload), "attributes": string(row.Attributes),
		"is_deleted": false, "is_archived": false, "is_uploaded": false,
		"workspace_id": workspaceID,
	}
	// Every other column the body names is written through, which is how an editor attaches an upload to a work item in the same call.
	for name, value := range legacyWritableColumns(c) {
		columns[name] = value
	}
	if err := handler.db.WithContext(c.Request.Context()).Table("file_assets").Create(columns).Error; err != nil {
		handler.internalError(c, err)
		return
	}
	stored, err := handler.legacyAssetByID(c, row.ID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusCreated, legacyAssetJSON(*stored))
}

// legacyUploadPath is get_upload_path: a workspace's file is filed under the workspace, and anybody else's under a "user-" prefix. A filename that sanitises to nothing is replaced by a bare uuid.
func legacyUploadPath(workspaceID *string, filename string) string {
	cleaned := uploads.SanitizeFilename(filename)
	if cleaned == "" {
		cleaned = strings.ReplaceAll(uuid.NewString(), "-", "")
	}
	unique := strings.ReplaceAll(uuid.NewString(), "-", "")
	if workspaceID != nil {
		return *workspaceID + "/" + unique + "-" + cleaned
	}
	return "user-" + unique + "-" + cleaned
}

// headerContentType is what the browser said the file was, and octet-stream when it said nothing.
func headerContentType(header *multipart.FileHeader) string {
	if header.Header != nil {
		if value := header.Header.Get("Content-Type"); value != "" {
			return value
		}
	}
	if extension := path.Ext(header.Filename); extension != "" {
		return "application/octet-stream"
	}
	return "application/octet-stream"
}

// legacyAttributes reads the attributes field off the form, which arrives as json in a text field.
func legacyAttributes(c *gin.Context) []byte {
	raw := c.Request.FormValue("attributes")
	if raw == "" {
		return []byte("{}")
	}
	var decoded any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return []byte("{}")
	}
	return []byte(raw)
}

// legacyWritableFields are the columns beyond the file itself that the form may set, which is every field the serializer leaves writable.
var legacyWritableFields = map[string]string{
	"entity_type": "entity_type", "entity_identifier": "entity_identifier",
	"external_id": "external_id", "external_source": "external_source",
	"user": "user_id", "draft_issue": "draft_issue_id", "project": "project_id",
	"issue": "issue_id", "comment": "comment_id", "page": "page_id",
}

func legacyWritableColumns(c *gin.Context) map[string]any {
	columns := map[string]any{}
	for field, column := range legacyWritableFields {
		value := c.Request.FormValue(field)
		if value == "" {
			continue
		}
		columns[column] = value
	}
	if value := c.Request.FormValue("is_archived"); value != "" {
		columns["is_archived"] = value == "true" || value == "True" || value == "1"
	}
	return columns
}

// workspaceAssetRead looks a file up by the key a client already holds.
//
// The key is the workspace id and the rest of the path joined back together, and the lookup names nothing else — so any member of any workspace can read the row of an asset in another, as long as they know its key. That is the route as it stands.
func (handler *Handler) workspaceAssetRead(c *gin.Context, user *auth.User) {
	key := c.Param("workspace") + "/" + c.Param("key")
	var rows []LegacyAsset
	err := handler.db.WithContext(c.Request.Context()).Table("file_assets").
		Where("asset = ? AND deleted_at IS NULL", key).Scan(&rows).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if len(rows) == 0 {
		// Not finding it is a 200 with a flag rather than a 404, which is what the client reads.
		drf.Respond(c, http.StatusOK, gin.H{"error": "Asset key does not exist", "status": false})
		return
	}
	results := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		results = append(results, legacyAssetJSON(row))
	}
	drf.Respond(c, http.StatusOK, gin.H{"data": results, "status": true})
}

// userAssetRead is the same lookup narrowed to the caller's own uploads, and it answers one object rather than a list.
//
// The narrowing and the shape are both worth noticing: the list is filtered but the serializer is handed the *queryset* without `many`, so what comes back is a serialized queryset rather than a serialized row. Django renders that as the first row's fields, which is what this answers.
func (handler *Handler) userAssetRead(c *gin.Context, user *auth.User) {
	var rows []LegacyAsset
	err := handler.db.WithContext(c.Request.Context()).Table("file_assets").
		Where("asset = ? AND created_by_id = ? AND deleted_at IS NULL", c.Param("key"), user.ID).
		Order("created_at DESC").Scan(&rows).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if len(rows) == 0 {
		drf.Respond(c, http.StatusOK, gin.H{"error": "Asset key does not exist", "status": false})
		return
	}
	drf.Respond(c, http.StatusOK, gin.H{"data": legacyAssetJSON(rows[0]), "status": true})
}

// workspaceAssetDelete marks a file deleted without taking it out of the bucket.
//
// It is `is_deleted`, not `deleted_at`: the row stays where it is and the object stays in the bucket, which is what the restore route depends on.
func (handler *Handler) workspaceAssetDelete(c *gin.Context, user *auth.User) {
	handler.setLegacyAssetDeleted(c, c.Param("workspace")+"/"+c.Param("key"), nil, true)
}

// workspaceAssetRestore undoes that.
func (handler *Handler) workspaceAssetRestore(c *gin.Context, user *auth.User) {
	handler.setLegacyAssetDeleted(c, c.Param("workspace")+"/"+c.Param("key"), nil, false)
}

// userAssetDelete marks one of the caller's own files deleted.
func (handler *Handler) userAssetDelete(c *gin.Context, user *auth.User) {
	handler.setLegacyAssetDeleted(c, c.Param("key"), &user.ID, true)
}

func (handler *Handler) setLegacyAssetDeleted(c *gin.Context, key string, owner *string, deleted bool) {
	query := handler.db.WithContext(c.Request.Context()).Table("file_assets").
		Where("asset = ? AND deleted_at IS NULL", key)
	if owner != nil {
		query = query.Where("created_by_id = ?", *owner)
	}
	var rows []string
	if err := query.Order("created_at DESC").Limit(1).Pluck("id", &rows).Error; err != nil {
		handler.internalError(c, err)
		return
	}
	if len(rows) == 0 {
		// Django's unguarded get raises, which the base view turns into a 404.
		handler.notFound(c)
		return
	}
	// save(update_fields=["is_deleted"]) writes the one column, so updated_at is left where it was.
	err := handler.db.WithContext(c.Request.Context()).Table("file_assets").
		Where("id = ?", rows[0]).Update("is_deleted", deleted).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (handler *Handler) legacyAssetByID(c *gin.Context, id string) (*LegacyAsset, error) {
	var row LegacyAsset
	err := handler.db.WithContext(c.Request.Context()).Table("file_assets").Where("id = ?", id).Take(&row).Error
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// legacyAssetJSON is FileAssetSerializer: every column the model has, in the order DRF lists them.
func legacyAssetJSON(row LegacyAsset) gin.H {
	return gin.H{
		"id": row.ID, "created_at": drf.Time(row.CreatedAt), "updated_at": drf.Time(row.UpdatedAt),
		"deleted_at": drf.At(row.DeletedAt),
		"attributes": decodeJSON(row.Attributes), "asset": row.Asset,
		"entity_type": row.EntityType, "entity_identifier": row.EntityIdentifier,
		"is_deleted": row.IsDeleted, "is_archived": row.IsArchived,
		"external_id": row.ExternalID, "external_source": row.ExternalSource,
		"size": row.Size, "is_uploaded": row.IsUploaded,
		"storage_metadata": decodeJSON(row.StorageMetadata),
		"created_by":       row.CreatedByID, "updated_by": row.UpdatedByID,
		"user": row.UserID, "workspace": row.WorkspaceID, "draft_issue": row.DraftIssueID,
		"project": row.ProjectID, "issue": row.IssueID, "comment": row.CommentID, "page": row.PageID,
	}
}

func int64Pointer(value int64) *int64 { return &value }
