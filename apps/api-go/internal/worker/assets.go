package worker

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/yldm-tech/pace/apps/api-go/internal/storage"
	"gorm.io/gorm"
)

// The two tasks that look after an uploaded file: one reads the object's headers back out of the bucket, the other sweeps up the rows whose upload never arrived.
const (
	AssetObjectMetadataTask       = "plane.bgtasks.storage_metadata_task.get_asset_object_metadata"
	DeleteUnuploadedFileAssetTask = "plane.bgtasks.file_asset_task.delete_unuploaded_file_asset"
)

// DefaultUnuploadedAssetDeleteDays is UNUPLOADED_ASSET_DELETE_DAYS, which the task reads with a bare int().
const DefaultUnuploadedAssetDeleteDays = 7

// AssetTasks needs the object store as well as the database, which is what sets it apart from the other task groups.
type AssetTasks struct {
	db      *gorm.DB
	store   *storage.Store
	logger  *slog.Logger
	clock   func() time.Time
	sweepIn int
}

func NewAssetTasks(db *gorm.DB, store *storage.Store, logger *slog.Logger) *AssetTasks {
	return &AssetTasks{db: db, store: store, logger: logger, clock: time.Now, sweepIn: DefaultUnuploadedAssetDeleteDays}
}

// SetUnuploadedAssetDeleteDays takes the configured window. Django reads it with a bare int() and no guard, so a negative value would put the cutoff in the future and sweep away every asset waiting to be uploaded; that is refused here for the same reason the hard delete's window is.
func (tasks *AssetTasks) SetUnuploadedAssetDeleteDays(days int) {
	if days < 0 {
		tasks.logger.Error("UNUPLOADED_ASSET_DELETE_DAYS is negative, keeping the default",
			"configured", days, "using", tasks.sweepIn)
		return
	}
	tasks.sweepIn = days
}

func (tasks *AssetTasks) Register(consumer *Consumer) {
	consumer.Register(AssetObjectMetadataTask, tasks.assetObjectMetadata)
	consumer.Register(DeleteUnuploadedFileAssetTask, tasks.deleteUnuploadedFileAssets)
}

// assetObjectMetadata reproduces get_asset_object_metadata: read the object's headers and keep them beside the row.
//
// A bucket that refuses the read is not an error here. Django logs it and writes **null** over whatever was there, so an object that has since gone takes its metadata with it.
func (tasks *AssetTasks) assetObjectMetadata(ctx context.Context, arguments []any, keywords map[string]any) error {
	assetID := stringArgument(arguments, keywords, 0, "asset_id")
	var asset struct {
		Asset string `gorm:"column:asset"`
	}
	err := tasks.db.WithContext(ctx).Table("file_assets").Select("asset").
		Where("id = ? AND deleted_at IS NULL", assetID).Take(&asset).Error
	if err != nil {
		// FileAsset.DoesNotExist reaches its own except and the task returns.
		return nil
	}
	if tasks.store == nil {
		// Without a configured bucket there is nothing to read, and Django would raise into its own except.
		tasks.logger.Warn("asset metadata skipped: object storage is not configured", "asset", assetID)
		return nil
	}

	var encoded any
	metadata, err := tasks.store.StatObject(ctx, asset.Asset)
	if err != nil {
		tasks.logger.Warn("asset metadata could not be read", "asset", assetID, "object", asset.Asset, "error", err)
	} else {
		rendered, err := json.Marshal(metadata)
		if err != nil {
			return err
		}
		encoded = string(rendered)
	}
	// save(update_fields=["storage_metadata"]) leaves updated_at where it was.
	return tasks.db.WithContext(ctx).Table("file_assets").Where("id = ?", assetID).
		Update("storage_metadata", encoded).Error
}

// deleteUnuploadedFileAssets reproduces delete_unuploaded_file_asset, which takes away the rows whose upload was started and never finished.
//
// It is a queryset delete, so it is a soft one: the rows stay and are marked. Nothing goes to the bucket either, because there is nothing there to remove — the upload never happened.
func (tasks *AssetTasks) deleteUnuploadedFileAssets(ctx context.Context, _ []any, _ map[string]any) error {
	now := tasks.clock().UTC()
	cutoff := now.AddDate(0, 0, -tasks.sweepIn)
	result := tasks.db.WithContext(ctx).Table("file_assets").
		Where("created_at < ? AND is_uploaded = FALSE AND deleted_at IS NULL", cutoff).
		Update("deleted_at", now)
	if result.Error != nil {
		return result.Error
	}
	tasks.logger.Info("unuploaded assets swept", "deleted", result.RowsAffected, "older_than_days", tasks.sweepIn)
	return nil
}
