package user

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"github.com/yldm-tech/pace/apps/api-go/internal/drf"
	"github.com/yldm-tech/pace/apps/api-go/internal/storage"
	"github.com/yldm-tech/pace/apps/api-go/internal/uploads"
	"gorm.io/gorm"
)

func (handler *Handler) registerAssetRoutes(router gin.IRouter) {
	const base = "/api/assets/v2/user-assets/"
	router.POST(base, handler.authenticated(handler.userAssetReserve))
	router.PATCH(base+":asset/", handler.authenticated(handler.userAssetMarkUploaded))
	router.DELETE(base+":asset/", handler.authenticated(handler.userAssetDestroy))
}

// SetStorage wires the object store these routes sign against. A nil store means object storage is not configured, which they refuse rather than half-complete.
func (handler *Handler) SetStorage(store *storage.Store) { handler.storage = store }

// newUUID is the identifier a new row takes.
func newUUID() (string, error) {
	identifier, err := uuid.NewRandom()
	if err != nil {
		return "", err
	}
	return identifier.String(), nil
}

// FileAsset is the db.FileAsset table as the profile routes see it.
type FileAsset struct {
	ID              string     `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt       time.Time  `gorm:"column:created_at"`
	UpdatedAt       time.Time  `gorm:"column:updated_at"`
	DeletedAt       *time.Time `gorm:"column:deleted_at"`
	Attributes      []byte     `gorm:"column:attributes;type:jsonb"`
	Asset           string     `gorm:"column:asset"`
	UserID          *string    `gorm:"column:user_id;type:uuid"`
	EntityType      *string    `gorm:"column:entity_type"`
	IsUploaded      bool       `gorm:"column:is_uploaded"`
	Size            float64    `gorm:"column:size"`
	StorageMetadata []byte     `gorm:"column:storage_metadata;type:jsonb"`
}

func (FileAsset) TableName() string { return "file_assets" }

// profileImageTypes is the list a profile image is held to.
var profileImageTypes = map[string]bool{
	"image/jpeg": true, "image/png": true, "image/webp": true, "image/jpg": true, "image/gif": true,
}

// userAssetReserve makes a row for a profile image and hands back somewhere to put the bytes.
//
// The key has **no workspace in front of it**, unlike every other asset the application writes, because a profile image belongs to a person rather than to a workspace.
func (handler *Handler) userAssetReserve(c *gin.Context, user *auth.User) {
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
	if entityType != "USER_AVATAR" && entityType != "USER_COVER" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid entity type.", "status": false})
		return
	}
	if !profileImageTypes[fileType] {
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
	assetKey := strings.ReplaceAll(key, "-", "") + "-" + name
	attributes, err := json.Marshal(map[string]any{"name": name, "type": fileType, "size": size})
	if err != nil {
		handler.internalError(c, err)
		return
	}
	now := handler.clock().UTC()
	err = handler.db.WithContext(c.Request.Context()).Table("file_assets").Create(map[string]any{
		"id": identifier, "created_at": now, "updated_at": now, "created_by_id": user.ID,
		"attributes": auth.JSONValue(attributes), "asset": assetKey, "size": size,
		"user_id": user.ID, "entity_type": entityType,
		"is_deleted": false, "is_archived": false, "is_uploaded": false,
	}).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	target, err := handler.storage.PresignedUpload(c.Request.Context(), assetKey, fileType, int64(size))
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

// userAssetMarkUploaded marks the bytes present and **puts the image on the person**, replacing whatever was there and marking the one it replaces deleted. The url the person used to carry is cleared, so an uploaded image always wins over a linked one.
func (handler *Handler) userAssetMarkUploaded(c *gin.Context, user *auth.User) {
	asset, found, err := handler.userAsset(c, user)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		handler.notFound(c)
		return
	}
	var payload map[string]json.RawMessage
	_ = c.ShouldBindJSON(&payload)
	updates := map[string]any{"is_uploaded": true}
	if raw, given := payload["attributes"]; given && json.Valid(raw) {
		updates["attributes"] = auth.JSONValue(append([]byte(nil), raw...))
	}
	now := handler.clock().UTC()
	column, urlColumn := profileAssetColumns(asset)
	err = handler.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Table("file_assets").Where("id = ?", asset.ID).Updates(updates).Error; err != nil {
			return err
		}
		if column == "" {
			return nil
		}
		var previous []*string
		if err := tx.Table("users").Where("id = ?", user.ID).Limit(1).Pluck(column, &previous).Error; err != nil {
			return err
		}
		if len(previous) > 0 && previous[0] != nil {
			err := tx.Table("file_assets").Where("id = ?", *previous[0]).
				Updates(map[string]any{"is_deleted": true, "deleted_at": now}).Error
			if err != nil {
				return err
			}
		}
		return tx.Table("users").Where("id = ?", user.ID).
			Updates(map[string]any{urlColumn: "", column: asset.ID}).Error
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
	if err := handler.invalidateProfile(c.Request.Context()); err != nil {
		handler.internalError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// userAssetDestroy marks a profile image deleted and takes it off the person.
func (handler *Handler) userAssetDestroy(c *gin.Context, user *auth.User) {
	asset, found, err := handler.userAsset(c, user)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		handler.notFound(c)
		return
	}
	now := handler.clock().UTC()
	column, _ := profileAssetColumns(asset)
	err = handler.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if column != "" {
			if err := tx.Table("users").Where("id = ?", user.ID).Update(column, nil).Error; err != nil {
				return err
			}
		}
		return tx.Table("file_assets").Where("id = ?", asset.ID).
			Updates(map[string]any{"is_deleted": true, "deleted_at": now}).Error
	})
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if err := handler.invalidateProfile(c.Request.Context()); err != nil {
		handler.internalError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// profileAssetColumns names the two columns an entity writes: the asset it points at, and the url it used to carry.
func profileAssetColumns(asset FileAsset) (string, string) {
	if asset.EntityType == nil {
		return "", ""
	}
	switch *asset.EntityType {
	case "USER_AVATAR":
		return "avatar_asset_id", "avatar"
	case "USER_COVER":
		return "cover_image_asset_id", "cover_image"
	}
	return "", ""
}

// userAsset reads a profile image, which is scoped to the person rather than to a workspace.
func (handler *Handler) userAsset(c *gin.Context, user *auth.User) (FileAsset, bool, error) {
	var assets []FileAsset
	err := handler.db.WithContext(c.Request.Context()).Table("file_assets").
		Where("id = ? AND user_id = ? AND deleted_at IS NULL", c.Param("asset"), user.ID).
		Limit(1).Scan(&assets).Error
	if err != nil || len(assets) == 0 {
		return FileAsset{}, false, err
	}
	return assets[0], true, nil
}

// invalidateProfile drops the two cached views of the person, which is what the decorator on these routes does.
func (handler *Handler) invalidateProfile(ctx context.Context) error {
	if handler.redis == nil {
		return nil
	}
	invalidator := auth.NewRedisCacheInvalidator(handler.redis)
	for _, pattern := range []string{"*/api/users/me/*", "*/api/users/me/settings/*"} {
		if err := invalidator.InvalidatePattern(ctx, pattern); err != nil {
			return err
		}
	}
	return nil
}

var errNoAssetStore = &assetError{"object storage is not configured"}

type assetError struct{ message string }

func (err *assetError) Error() string { return err.message }
