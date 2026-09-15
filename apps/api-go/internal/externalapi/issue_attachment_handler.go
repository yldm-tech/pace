package externalapi

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"github.com/yldm-tech/pace/apps/api-go/internal/drf"
	"github.com/yldm-tech/pace/apps/api-go/internal/uploads"
)

func (handler *Handler) registerIssueAttachmentRoutes(router gin.IRouter) {
	base := "/api/v1/workspaces/:slug/projects/:project/"
	// The two spellings do not carry the same path here: the older one says issue-attachments and the newer one attachments.
	for _, path := range []string{
		base + "issues/:issue/issue-attachments/",
		base + "work-items/:issue/attachments/",
	} {
		router.GET(path, handler.authenticated(handler.issueAttachmentList))
		router.POST(path, handler.authenticated(handler.issueAttachmentReserve))
		router.GET(path+":asset/", handler.authenticated(handler.issueAttachmentDownload))
		router.PATCH(path+":asset/", handler.authenticated(handler.issueAttachmentMarkUploaded))
		router.DELETE(path+":asset/", handler.authenticated(handler.issueAttachmentDestroy))
	}
}

// issueAttachmentList returns a work item's attachments, showing only what finished uploading.
func (handler *Handler) issueAttachmentList(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireIssuePermission(c, user, false, false) {
		return
	}
	var assets []FileAsset
	err := handler.db.WithContext(c.Request.Context()).Table("file_assets fa").Select("fa.*").
		Joins("JOIN workspaces w ON w.id = fa.workspace_id").
		Where(`w.slug = ? AND fa.project_id = ? AND fa.issue_id = ?
			AND fa.entity_type = 'ISSUE_ATTACHMENT' AND fa.is_deleted = FALSE AND fa.is_uploaded = TRUE`,
			c.Param("slug"), c.Param("project"), c.Param("issue")).
		Order("fa.created_at DESC").Scan(&assets).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	results := make([]gin.H, 0, len(assets))
	for _, asset := range assets {
		results = append(results, issueAttachmentJSON(asset))
	}
	drf.Respond(c, http.StatusOK, results)
}

// issueAttachmentReserve makes a row and hands back somewhere to put the bytes.
//
// It refuses a missing name or size with "Invalid request." where the workspace asset route says "Name and size are required fields." — the same guard, two messages.
func (handler *Handler) issueAttachmentReserve(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireIssuePermission(c, user, true, true) {
		return
	}
	if handler.assets == nil {
		handler.serverError(c, errNoAssetStore)
		return
	}
	slug, projectID, issueID := c.Param("slug"), c.Param("project"), c.Param("issue")
	var payload map[string]any
	if err := c.ShouldBindJSON(&payload); err != nil {
		invalidPayload(c)
		return
	}
	rawName, _ := payload["name"].(string)
	name := uploads.SanitizeFilename(rawName)
	fileType, _ := payload["type"].(string)
	// Unlike the workspace asset route this one has **no default**: an absent size is nothing, and nothing is refused.
	var size float64
	if value, present := payload["size"]; present {
		parsed, ok := assetSize(value)
		if !ok {
			handler.serverError(c, errBadAssetSize)
			return
		}
		size = parsed
	}
	if name == "" || size == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request.", "status": false})
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
			Where(`w.slug = ? AND fa.project_id = ? AND fa.issue_id = ?
				AND fa.external_source = ? AND fa.external_id = ? AND fa.entity_type = 'ISSUE_ATTACHMENT'`,
				slug, projectID, issueID, externalSource, externalID).
			Order("fa.created_at").Limit(1).Scan(&existing).Error
		if err != nil {
			handler.serverError(c, err)
			return
		}
		if len(existing) > 0 {
			// The message says "Issue with the same external id" although it is the attachment that clashed.
			c.JSON(http.StatusConflict, gin.H{
				"error": "Issue with the same external id and external source already exists",
				"id":    existing[0].ID,
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
		WorkspaceID: &workspaceIDs[0], ProjectID: &projectID, EntityType: &entityType,
	}
	if externalID != "" {
		asset.ExternalID = &externalID
	}
	if externalSource != "" {
		asset.ExternalSource = &externalSource
	}
	err = handler.db.WithContext(c.Request.Context()).Table("file_assets").
		Create(attachmentInsert(asset, issueID)).Error
	if err != nil {
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
		"attachment":  issueAttachmentJSON(asset),
	})
}

// attachmentInsert writes the row with the issue link the model column carries.
func attachmentInsert(asset FileAsset, issueID string) map[string]any {
	return map[string]any{
		"id": asset.ID, "created_at": asset.CreatedAt, "updated_at": asset.UpdatedAt,
		"created_by_id": asset.CreatedByID, "attributes": asset.Attributes, "asset": asset.Asset,
		"size": asset.Size, "workspace_id": asset.WorkspaceID, "project_id": asset.ProjectID,
		"issue_id": issueID, "entity_type": asset.EntityType,
		"external_id": asset.ExternalID, "external_source": asset.ExternalSource,
		"is_deleted": false, "is_archived": false, "is_uploaded": false,
	}
}

// issueAttachmentDownload redirects to the bytes.
//
// It answers a **redirect** rather than a body, which is the only route in this API that does. The disposition is always an attachment, so nothing uploaded here is ever rendered inline — unlike the workspace asset route, which decides per type.
func (handler *Handler) issueAttachmentDownload(c *gin.Context, user *auth.User, _ APIToken) {
	// The permission check here passes no issue and no roles, so it asks only that the caller is an active member of the project — any role will do.
	if !handler.requireIssuePermission(c, user, false, false) {
		return
	}
	if handler.assets == nil {
		handler.serverError(c, errNoAssetStore)
		return
	}
	asset, found, err := handler.attachmentByID(c)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !found {
		notFound(c)
		return
	}
	if !asset.IsUploaded {
		c.JSON(http.StatusBadRequest, gin.H{"error": "The asset is not uploaded.", "status": false})
		return
	}
	attributes, _ := decodeJSON(asset.Attributes).(map[string]any)
	filename, _ := attributes["name"].(string)
	url, err := handler.assets.PresignedDownload(c.Request.Context(), asset.Asset, "attachment", filename)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	c.Redirect(http.StatusFound, url)
}

// issueAttachmentMarkUploaded is the callback once the bytes are in place.
func (handler *Handler) issueAttachmentMarkUploaded(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireIssuePermission(c, user, true, true) {
		return
	}
	asset, found, err := handler.attachmentByID(c)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !found {
		notFound(c)
		return
	}
	err = handler.db.WithContext(c.Request.Context()).Table("file_assets").
		Where("id = ?", asset.ID).Update("is_uploaded", true).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if len(asset.StorageMetadata) == 0 || string(asset.StorageMetadata) == "null" {
		if handler.tasks != nil {
			if err := handler.tasks.PublishAssetObjectMetadata(c.Request.Context(), asset.ID); err != nil {
				handler.serverError(c, err)
				return
			}
		}
	}
	c.Status(http.StatusNoContent)
}

// issueAttachmentDestroy marks an attachment deleted.
//
// It queues the **metadata** task on the way out, for an attachment that is going away — which is upstream's, and harmless rather than useful.
func (handler *Handler) issueAttachmentDestroy(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireIssuePermission(c, user, true, true) {
		return
	}
	asset, found, err := handler.attachmentByID(c)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !found {
		notFound(c)
		return
	}
	now := handler.clock().UTC()
	err = handler.db.WithContext(c.Request.Context()).Table("file_assets").Where("id = ?", asset.ID).
		Updates(map[string]any{"is_deleted": true, "deleted_at": now}).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if len(asset.StorageMetadata) == 0 || string(asset.StorageMetadata) == "null" {
		if handler.tasks != nil {
			if err := handler.tasks.PublishAssetObjectMetadata(c.Request.Context(), asset.ID); err != nil {
				handler.serverError(c, err)
				return
			}
		}
	}
	c.Status(http.StatusNoContent)
}

// attachmentByID reads the row, scoped to the workspace and the project but **not** to the work item in the url.
func (handler *Handler) attachmentByID(c *gin.Context) (FileAsset, bool, error) {
	var assets []FileAsset
	err := handler.db.WithContext(c.Request.Context()).Table("file_assets fa").Select("fa.*").
		Joins("JOIN workspaces w ON w.id = fa.workspace_id").
		Where("w.slug = ? AND fa.project_id = ? AND fa.id = ?",
			c.Param("slug"), c.Param("project"), c.Param("asset")).
		Limit(1).Scan(&assets).Error
	if err != nil || len(assets) == 0 {
		return FileAsset{}, false, err
	}
	return assets[0], true, nil
}

// requireIssuePermission is user_has_issue_permission.
//
// The person who **raised** the work item passes whatever their role, which is what lets a guest manage the attachments on their own item. A call that passes no roles asks only for membership.
func (handler *Handler) requireIssuePermission(c *gin.Context, user *auth.User, withRoles, allowCreator bool) bool {
	if allowCreator {
		var creators []string
		err := handler.db.WithContext(c.Request.Context()).Table("issues i").
			Joins("JOIN workspaces w ON w.id = i.workspace_id").
			Where("w.slug = ? AND i.project_id = ? AND i.id = ?",
				c.Param("slug"), c.Param("project"), c.Param("issue")).
			Limit(1).Pluck("i.created_by_id", &creators).Error
		if err != nil {
			handler.serverError(c, err)
			return false
		}
		if len(creators) > 0 && creators[0] == user.ID {
			return true
		}
	}
	role, member, err := handler.projectRole(c, user)
	if err != nil {
		handler.serverError(c, err)
		return false
	}
	if member && (!withRoles || role == roleAdmin || role == roleMember || role == roleGuest) {
		return true
	}
	c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
		"error": "You are not allowed to upload this attachment",
	})
	return false
}

// issueAttachmentJSON is the external API's IssueAttachmentSerializer.
func issueAttachmentJSON(asset FileAsset) gin.H {
	return gin.H{
		"id": asset.ID, "created_at": asset.CreatedAt, "updated_at": asset.UpdatedAt,
		"created_by": asset.CreatedByID, "updated_by": asset.UpdatedByID, "deleted_at": asset.DeletedAt,
		"attributes": decodeJSON(asset.Attributes), "asset": asset.Asset, "size": asset.Size,
		"is_uploaded": asset.IsUploaded, "storage_metadata": decodeJSON(asset.StorageMetadata),
		"external_source": asset.ExternalSource, "external_id": asset.ExternalID,
		"entity_type": asset.EntityType,
		"project":     asset.ProjectID, "workspace": asset.WorkspaceID,
	}
}
