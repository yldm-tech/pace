package workspace

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api/internal/auth"
	"github.com/yldm-tech/pace/apps/api/internal/drf"
	"github.com/yldm-tech/pace/apps/api/internal/uploads"
	"gorm.io/gorm"
)

func (handler *Handler) registerProjectAssetRoutes(router gin.IRouter) {
	const base = "/api/assets/v2/workspaces/:slug/projects/:id/"
	router.POST(base, handler.authenticated(handler.projectAssetReserve))
	router.GET(base+":asset/", handler.authenticated(handler.projectAssetDownload))
	router.PATCH(base+":asset/", handler.authenticated(handler.projectAssetMarkUploaded))
	router.DELETE(base+":asset/", handler.authenticated(handler.projectAssetDestroy))
	router.GET(base+"download/:asset/", handler.authenticated(handler.projectAssetAttachment))
	router.POST(base+":asset/bulk/", handler.authenticated(handler.projectAssetBulk))
	router.POST("/api/assets/v2/workspaces/:slug/duplicate-assets/:asset/", handler.authenticated(handler.duplicateAsset))
}

// projectEntityColumn is the project endpoint's own entity table. It differs from the workspace one by a single line: a draft issue description writes a column here and writes none there.
func projectEntityColumn(entityType string) string {
	if entityType == "DRAFT_ISSUE_DESCRIPTION" {
		return "draft_issue_id"
	}
	return entityColumn(entityType)
}

// projectAssetReserve makes a row inside a project and hands back somewhere to put the bytes.
//
// Unlike the workspace route it asks nothing of the entity beyond being one: a workspace logo reserved here is written with the project's id beside it and nobody objects.
func (handler *Handler) projectAssetReserve(c *gin.Context, user *auth.User) {
	if !handler.requireProjectMember(c, user) {
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
	err := handler.db.WithContext(c.Request.Context()).Table("workspaces").
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
		"workspace_id": workspaceIDs[0], "project_id": c.Param("id"), "entity_type": entityType,
		"is_deleted": false, "is_archived": false, "is_uploaded": false,
	}
	if column := projectEntityColumn(entityType); column != "" {
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

// projectAssetMarkUploaded marks the bytes present. Unlike the workspace route it moves the asset onto **nothing**: a project cover reserved here stays unattached until the bulk route claims it.
func (handler *Handler) projectAssetMarkUploaded(c *gin.Context, user *auth.User) {
	asset, ok := handler.projectAsset(c, user, false)
	if !ok {
		return
	}
	var payload map[string]json.RawMessage
	_ = c.ShouldBindJSON(&payload)
	updates := map[string]any{"is_uploaded": true}
	if raw, given := payload["attributes"]; given && json.Valid(raw) {
		updates["attributes"] = auth.JSONValue(append([]byte(nil), raw...))
	}
	err := handler.db.WithContext(c.Request.Context()).Table("file_assets").
		Where("id = ?", asset.ID).Updates(updates).Error
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
	c.Status(http.StatusNoContent)
}

// projectAssetDestroy marks an asset deleted and takes it off nothing, which is the other half of the same asymmetry.
func (handler *Handler) projectAssetDestroy(c *gin.Context, user *auth.User) {
	asset, ok := handler.projectAsset(c, user, false)
	if !ok {
		return
	}
	now := handler.clock().UTC()
	err := handler.db.WithContext(c.Request.Context()).Table("file_assets").Where("id = ?", asset.ID).
		Updates(map[string]any{"is_deleted": true, "deleted_at": now}).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (handler *Handler) projectAssetDownload(c *gin.Context, user *auth.User) {
	asset, ok := handler.projectAsset(c, user, false)
	if !ok {
		return
	}
	if !asset.IsUploaded {
		c.JSON(http.StatusNotFound, gin.H{"error": "The requested asset could not be found."})
		return
	}
	handler.redirectToAsset(c, asset, "attachment")
}

func (handler *Handler) projectAssetAttachment(c *gin.Context, user *auth.User) {
	asset, ok := handler.projectAsset(c, user, true)
	if !ok {
		return
	}
	handler.redirectToAsset(c, asset, "attachment")
}

// projectAssetBulk claims freshly uploaded assets for an entity.
//
// The entity it acts on is read off the **first** asset the query finds, and then applied to all of them — so a call naming two assets of different kinds treats both as whatever the first one is. The scope is the caller's own uploads that are either unattached or already in this project, which is what lets a cover uploaded before the project existed be claimed afterwards.
func (handler *Handler) projectAssetBulk(c *gin.Context, user *auth.User) {
	if !handler.requireProjectMember(c, user) {
		return
	}
	var request struct {
		AssetIDs []string `json:"asset_ids"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		handler.invalidDetail(c)
		return
	}
	if len(request.AssetIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No asset ids provided."})
		return
	}
	projectID, entityID := c.Param("id"), c.Param("asset")
	var assets []FileAsset
	err := handler.db.WithContext(c.Request.Context()).Table("file_assets fa").Select("fa.*").
		Joins("JOIN workspaces w ON w.id = fa.workspace_id").
		Where("fa.id IN ? AND w.slug = ? AND fa.created_by_id = ? AND fa.deleted_at IS NULL",
			request.AssetIDs, c.Param("slug"), user.ID).
		Where("fa.project_id = ? OR fa.project_id IS NULL", projectID).
		Order("fa.created_at").Scan(&assets).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if len(assets) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "The requested asset could not be found."})
		return
	}

	identifiers := make([]string, 0, len(assets))
	for _, asset := range assets {
		identifiers = append(identifiers, asset.ID)
	}
	now := handler.clock().UTC()
	err = handler.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		switch stringOrNil(assets[0].EntityType) {
		case "PROJECT_COVER":
			err := tx.Table("file_assets").Where("id IN ?", identifiers).
				Update("project_id", projectID).Error
			if err != nil {
				return err
			}
			// Every asset in the list is put on the project in turn, so the last one wins.
			for range assets {
				err := tx.Table("projects").Where("id = ?", projectID).
					Updates(map[string]any{"cover_image_asset_id": identifiers[len(identifiers)-1], "updated_at": now}).Error
				if err != nil {
					return err
				}
			}
			return nil
		case "ISSUE_DESCRIPTION":
			return tx.Table("file_assets").Where("id IN ?", identifiers).
				Updates(map[string]any{"issue_id": entityID, "project_id": projectID}).Error
		case "COMMENT_DESCRIPTION":
			return tx.Table("file_assets").Where("id IN ?", identifiers).
				Update("comment_id", entityID).Error
		case "PAGE_DESCRIPTION":
			return tx.Table("file_assets").Where("id IN ?", identifiers).
				Update("page_id", entityID).Error
		case "DRAFT_ISSUE_DESCRIPTION":
			return tx.Table("file_assets").Where("id IN ?", identifiers).
				Update("draft_issue_id", entityID).Error
		}
		return nil
	})
	if err != nil {
		// An entity that has been deleted since the upload is a foreign key failure, which Django swallows for three of the five kinds.
		if isForeignKeyViolation(err) {
			c.Status(http.StatusNoContent)
			return
		}
		handler.internalError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// duplicateAsset copies an asset's bytes into a new key and writes a row beside it.
//
// The copy is the bucket's own: the bytes never pass through here. The new row is marked uploaded **after** it is written, in a second statement, which is what Django does.
func (handler *Handler) duplicateAsset(c *gin.Context, user *auth.User) {
	role, err := handler.workspaceRole(c.Request.Context(), c.Param("slug"), user.ID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if role == 0 {
		forbidden(c)
		return
	}
	var request struct {
		ProjectID  *string `json:"project_id"`
		EntityID   *string `json:"entity_id"`
		EntityType *string `json:"entity_type"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		handler.invalidDetail(c)
		return
	}
	if request.EntityType == nil || !assetEntityTypes[*request.EntityType] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid entity type or entity id"})
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
	if request.ProjectID != nil && *request.ProjectID != "" {
		var count int64
		err := handler.db.WithContext(c.Request.Context()).Table("projects").
			Where("id = ? AND workspace_id = ?", *request.ProjectID, workspaceIDs[0]).Count(&count).Error
		if err != nil {
			handler.internalError(c, err)
			return
		}
		if count == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "Project not found"})
			return
		}
	}

	var originals []FileAsset
	// The source has to be in the same workspace as the destination, which is what stops an asset being copied out of another one.
	err = handler.db.WithContext(c.Request.Context()).Table("file_assets").
		Where("id = ? AND is_uploaded = TRUE AND workspace_id = ? AND deleted_at IS NULL",
			c.Param("asset"), workspaceIDs[0]).
		Limit(1).Scan(&originals).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if len(originals) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Asset not found"})
		return
	}
	original := originals[0]
	if handler.storage == nil {
		handler.internalError(c, errNoAssetStore)
		return
	}

	attributes := map[string]any{}
	_ = json.Unmarshal(original.Attributes, &attributes)
	rawName, _ := attributes["name"].(string)
	name := uploads.SanitizeFilename(rawName)
	if name == "" {
		name = "unnamed"
	}
	key, err := newUUID()
	if err != nil {
		handler.internalError(c, err)
		return
	}
	destination := workspaceIDs[0] + "/" + strings.ReplaceAll(key, "-", "") + "-" + name
	identifier, err := newUUID()
	if err != nil {
		handler.internalError(c, err)
		return
	}
	// The copy keeps the **unsanitized** name in its attributes even though the key it is written under is sanitized.
	copied, err := json.Marshal(map[string]any{
		"name": attributes["name"], "type": attributes["type"], "size": attributes["size"],
	})
	if err != nil {
		handler.internalError(c, err)
		return
	}
	now := handler.clock().UTC()
	row := map[string]any{
		"id": identifier, "created_at": now, "updated_at": now, "created_by_id": user.ID,
		"attributes": auth.JSONValue(copied), "asset": destination, "size": original.Size,
		"workspace_id": workspaceIDs[0], "entity_type": *request.EntityType,
		"storage_metadata": auth.JSONValue(original.StorageMetadata),
		"is_deleted":       false, "is_archived": false, "is_uploaded": false,
	}
	if request.ProjectID != nil && *request.ProjectID != "" {
		row["project_id"] = *request.ProjectID
	}
	if column := projectEntityColumn(*request.EntityType); column != "" && request.EntityID != nil && *request.EntityID != "" {
		row[column] = *request.EntityID
	}
	if err := handler.db.WithContext(c.Request.Context()).Table("file_assets").Create(row).Error; err != nil {
		handler.internalError(c, err)
		return
	}
	if err := handler.storage.CopyObject(c.Request.Context(), original.Asset, destination); err != nil {
		handler.internalError(c, err)
		return
	}
	err = handler.db.WithContext(c.Request.Context()).Table("file_assets").
		Where("id = ?", identifier).Update("is_uploaded", true).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, gin.H{"asset_id": identifier})
}

// projectAsset reads an asset of the project the url names, having asked that the caller be a member of it.
func (handler *Handler) projectAsset(c *gin.Context, user *auth.User, uploadedOnly bool) (FileAsset, bool) {
	if !handler.requireProjectMember(c, user) {
		return FileAsset{}, false
	}
	query := handler.db.WithContext(c.Request.Context()).Table("file_assets fa").Select("fa.*").
		Joins("JOIN workspaces w ON w.id = fa.workspace_id").
		Where("fa.id = ? AND w.slug = ? AND fa.project_id = ? AND fa.deleted_at IS NULL",
			c.Param("asset"), c.Param("slug"), c.Param("id"))
	if uploadedOnly {
		query = query.Where("fa.is_uploaded = TRUE")
	}
	var assets []FileAsset
	if err := query.Limit(1).Scan(&assets).Error; err != nil {
		handler.internalError(c, err)
		return FileAsset{}, false
	}
	if len(assets) == 0 {
		if uploadedOnly {
			c.JSON(http.StatusNotFound, gin.H{"error": "The requested asset could not be found."})
			return FileAsset{}, false
		}
		handler.notFound(c)
		return FileAsset{}, false
	}
	return assets[0], true
}

// requireProjectMember is the project branch of allow_permission: an active membership of the project named in the url, at any role.
//
// The membership's own soft delete is filtered because this query is the whole gate: it reads project_members through its own manager and carries no other check that the project or the workspace still exists.
func (handler *Handler) requireProjectMember(c *gin.Context, user *auth.User) bool {
	var count int64
	err := handler.db.WithContext(c.Request.Context()).Table("project_members pm").
		Joins("JOIN workspaces w ON w.id = pm.workspace_id").
		Where("w.slug = ? AND pm.project_id = ? AND pm.member_id = ? AND pm.is_active = TRUE AND pm.deleted_at IS NULL",
			c.Param("slug"), c.Param("id"), user.ID).Count(&count).Error
	if err != nil {
		handler.internalError(c, err)
		return false
	}
	if count == 0 {
		forbidden(c)
		return false
	}
	return true
}
