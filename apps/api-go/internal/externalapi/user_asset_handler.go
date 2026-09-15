package externalapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"github.com/yldm-tech/pace/apps/api-go/internal/drf"
	"github.com/yldm-tech/pace/apps/api-go/internal/uploads"
	"gorm.io/gorm"
)

func (handler *Handler) registerUserAssetRoutes(router gin.IRouter) {
	const base = "/api/v1/assets/user-assets/"
	router.POST(base, handler.authenticated(handler.userAssetReserve))
	router.PATCH(base+":asset/", handler.authenticated(handler.userAssetMarkUploaded))
	router.DELETE(base+":asset/", handler.authenticated(handler.userAssetDestroy))
	// The server variant is the same endpoint asking the storage for server credentials, and the two routes under an id behave exactly like the ones above.
	router.POST(base+"server/", handler.authenticated(handler.userServerAssetReserve))
	router.PATCH(base+":asset/server/", handler.authenticated(handler.userAssetMarkUploaded))
	router.DELETE(base+":asset/server/", handler.authenticated(handler.userAssetDestroy))
}

// userAvatarTypes is the list a profile image is held to, which is narrower than the one a work item attachment uses.
var userAvatarTypes = map[string]bool{
	"image/jpeg": true, "image/png": true, "image/webp": true, "image/jpg": true, "image/gif": true,
}

// userAssetEntityTypes is what a profile image can be.
var userAssetEntityTypes = map[string]bool{"USER_AVATAR": true, "USER_COVER": true}

// userAssetReserve makes a row for a profile image and hands back somewhere to put the bytes.
//
// Nothing here is scoped to a workspace: a profile image belongs to the person rather than to a workspace, and the asset key is the bare name with no workspace in front of it.
func (handler *Handler) userAssetReserve(c *gin.Context, user *auth.User, _ APIToken) {
	handler.reserveUserAsset(c, user, false)
}

// userServerAssetReserve is the same endpoint asking the storage for **server** credentials, which it does by passing an argument the storage class does not take.
//
// That is a TypeError, so this route has never answered anything but a 500. It is reproduced rather than quietly fixed: a route that starts working is a change no caller asked for, and the error it raises is the only behaviour it has ever had.
func (handler *Handler) userServerAssetReserve(c *gin.Context, user *auth.User, _ APIToken) {
	handler.reserveUserAsset(c, user, true)
}

func (handler *Handler) reserveUserAsset(c *gin.Context, user *auth.User, server bool) {
	var payload map[string]any
	if err := c.ShouldBindJSON(&payload); err != nil {
		invalidPayload(c)
		return
	}
	rawName, _ := payload["name"].(string)
	name := uploads.SanitizeFilename(rawName)
	if name == "" {
		// A name that sanitizes away is stored as "unnamed" rather than refused.
		name = "unnamed"
	}
	fileType := "image/jpeg"
	if value, present := payload["type"].(string); present {
		fileType = value
	}
	size := float64(handler.settings.FileSizeLimit)
	if value, present := payload["size"]; present {
		parsed, ok := assetSize(value)
		if !ok {
			handler.serverError(c, errBadAssetSize)
			return
		}
		size = parsed
	}
	entityType, _ := payload["entity_type"].(string)
	if !userAssetEntityTypes[entityType] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid entity type.", "status": false})
		return
	}
	if !userAvatarTypes[fileType] {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":  "Invalid file type. Only JPEG and PNG files are allowed.",
			"status": false,
		})
		return
	}
	if limit := float64(handler.settings.FileSizeLimit); size > limit {
		size = limit
	}
	if server {
		// The storage is asked for credentials it has no argument for, which is where the request ends.
		handler.serverError(c, errServerStorageTakesNoFlag)
		return
	}
	if handler.assets == nil {
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
	// The key has no workspace in front of it, unlike every other asset this API writes.
	assetKey := strings.ReplaceAll(key, "-", "") + "-" + name
	attributes, err := assetAttributes(name, fileType, size)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	now := handler.clock().UTC()
	asset := FileAsset{
		ID: identifier, CreatedAt: now, UpdatedAt: now, CreatedByID: &user.ID,
		Attributes: attributes, Asset: assetKey, Size: size, EntityType: &entityType,
	}
	err = handler.db.WithContext(c.Request.Context()).Table("file_assets").Create(map[string]any{
		"id": asset.ID, "created_at": now, "updated_at": now, "created_by_id": user.ID,
		"attributes": attributes, "asset": assetKey, "size": size,
		"entity_type": entityType, "user_id": user.ID,
		"is_deleted": false, "is_archived": false, "is_uploaded": false,
	}).Error
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
	})
}

var errServerStorageTakesNoFlag = &orderError{"the storage class takes no server flag, which is where this route ends"}

// userAssetMarkUploaded is the callback once the bytes are in place. It also rewrites the attributes from the payload, which the workspace asset route does not.
func (handler *Handler) userAssetMarkUploaded(c *gin.Context, user *auth.User, _ APIToken) {
	asset, found, err := handler.userAssetByID(c, user)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !found {
		notFound(c)
		return
	}
	updates := map[string]any{"is_uploaded": true}
	var payload map[string]json.RawMessage
	if err := c.ShouldBindJSON(&payload); err == nil {
		if raw, given := payload["attributes"]; given && json.Valid(raw) {
			updates["attributes"] = jsonValue(raw)
		}
	}
	err = handler.db.WithContext(c.Request.Context()).Table("file_assets").
		Where("id = ?", asset.ID).Updates(updates).Error
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

// userAssetDestroy marks a profile image deleted and takes it off the person it belonged to.
//
// The row is marked deleted but keeps its deleted_at semantics: is_deleted goes true and deleted_at is stamped, which is not the soft delete the manager reads — a deleted profile image is still visible to any query that filters only on deleted_at.
func (handler *Handler) userAssetDestroy(c *gin.Context, user *auth.User, _ APIToken) {
	asset, found, err := handler.userAssetByID(c, user)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !found {
		notFound(c)
		return
	}
	now := handler.clock().UTC()
	column := ""
	switch stringOrNil(asset.EntityType) {
	case "USER_AVATAR":
		column = "avatar_asset_id"
	case "USER_COVER":
		column = "cover_image_asset_id"
	}
	err = handler.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if column != "" {
			err := tx.Table("users").Where("id = ?", user.ID).Updates(map[string]any{column: nil}).Error
			if err != nil {
				return err
			}
		}
		return tx.Table("file_assets").Where("id = ?", asset.ID).
			Updates(map[string]any{"is_deleted": true, "deleted_at": now}).Error
	})
	if err != nil {
		handler.serverError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// userAssetByID reads a profile image, which is scoped to the person rather than to a workspace.
func (handler *Handler) userAssetByID(c *gin.Context, user *auth.User) (FileAsset, bool, error) {
	var assets []FileAsset
	err := handler.db.WithContext(c.Request.Context()).Table("file_assets").
		Where("id = ? AND user_id = ?", c.Param("asset"), user.ID).
		Limit(1).Scan(&assets).Error
	if err != nil || len(assets) == 0 {
		return FileAsset{}, false, err
	}
	return assets[0], true, nil
}
