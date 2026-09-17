package worker

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"gorm.io/gorm"
)

// Beat task names, matching plane.bgtasks.cleanup_task.
const (
	DeleteAPILogsTask                  = "plane.bgtasks.cleanup_task.delete_api_logs"
	DeleteEmailNotificationLogsTask    = "plane.bgtasks.cleanup_task.delete_email_notification_logs"
	DeletePageVersionsTask             = "plane.bgtasks.cleanup_task.delete_page_versions"
	DeleteIssueDescriptionVersionsTask = "plane.bgtasks.cleanup_task.delete_issue_description_versions"
	DeleteWebhookLogsTask              = "plane.bgtasks.cleanup_task.delete_webhook_logs"
	RecentVisitedTask                  = "plane.bgtasks.recent_visited_task.recent_visited_task"
)

// cleanupBatchSize is cleanup_task.BATCH_SIZE.
const cleanupBatchSize = 500

// versionsKeptPerParent is the 20 rows the version cleanups keep per page or
// per issue.
const versionsKeptPerParent = 20

// RetentionSettings mirrors the retention windows in plane/settings/common.py.
type RetentionSettings struct {
	APIActivityLogDays int
	EmailLogDays       int
	WebhookLogDays     int
}

// DefaultRetentionSettings carries the defaults Django falls back to.
func DefaultRetentionSettings() RetentionSettings {
	return RetentionSettings{APIActivityLogDays: 14, EmailLogDays: 7, WebhookLogDays: 14}
}

// MaintenanceTasks owns the periodic database cleanups and the recent-visit
// bookkeeping. They touch only PostgreSQL, so they carry no template or mail
// dependency.
type MaintenanceTasks struct {
	db        *gorm.DB
	retention RetentionSettings
	logger    *slog.Logger
	clock     func() time.Time
}

func NewMaintenanceTasks(db *gorm.DB, retention RetentionSettings, logger *slog.Logger) *MaintenanceTasks {
	if logger == nil {
		logger = slog.Default()
	}
	return &MaintenanceTasks{db: db, retention: retention, logger: logger, clock: time.Now}
}

func (tasks *MaintenanceTasks) Register(consumer *Consumer) {
	consumer.Register(DeleteAPILogsTask, tasks.deleteAPILogs)
	consumer.Register(DeleteEmailNotificationLogsTask, tasks.deleteEmailNotificationLogs)
	consumer.Register(DeletePageVersionsTask, tasks.deletePageVersions)
	consumer.Register(DeleteIssueDescriptionVersionsTask, tasks.deleteIssueDescriptionVersions)
	consumer.Register(DeleteWebhookLogsTask, tasks.deleteWebhookLogs)
	consumer.Register(RecentVisitedTask, tasks.recentVisited)
}

func (tasks *MaintenanceTasks) deleteAPILogs(ctx context.Context, _ []any, _ map[string]any) error {
	cutoff := tasks.clock().UTC().AddDate(0, 0, -tasks.retention.APIActivityLogDays)
	return tasks.deleteInBatches(ctx, "API Activity Log", "api_activity_logs",
		tasks.db.WithContext(ctx).Table("api_activity_logs").Where("created_at <= ?", cutoff))
}

func (tasks *MaintenanceTasks) deleteEmailNotificationLogs(ctx context.Context, _ []any, _ map[string]any) error {
	cutoff := tasks.clock().UTC().AddDate(0, 0, -tasks.retention.EmailLogDays)
	return tasks.deleteInBatches(ctx, "Email Notification Log", "email_notification_logs",
		tasks.db.WithContext(ctx).Table("email_notification_logs").Where("sent_at <= ?", cutoff))
}

func (tasks *MaintenanceTasks) deleteWebhookLogs(ctx context.Context, _ []any, _ map[string]any) error {
	cutoff := tasks.clock().UTC().AddDate(0, 0, -tasks.retention.WebhookLogDays)
	return tasks.deleteInBatches(ctx, "Webhook Log", "webhook_logs",
		tasks.db.WithContext(ctx).Table("webhook_logs").Where("created_at <= ?", cutoff).Order("created_at"))
}

func (tasks *MaintenanceTasks) deletePageVersions(ctx context.Context, _ []any, _ map[string]any) error {
	return tasks.deleteExcessVersions(ctx, "Page Version", "page_versions", "page_id")
}

func (tasks *MaintenanceTasks) deleteIssueDescriptionVersions(ctx context.Context, _ []any, _ map[string]any) error {
	return tasks.deleteExcessVersions(ctx, "Issue Description Version", "issue_description_versions", "issue_id")
}

// deleteExcessVersions keeps the newest versionsKeptPerParent rows per parent,
// which is the ROW_NUMBER window Django builds.
func (tasks *MaintenanceTasks) deleteExcessVersions(ctx context.Context, label, table, parentColumn string) error {
	query := tasks.db.WithContext(ctx).Table(table).Where(
		fmt.Sprintf(`id IN (
			SELECT id FROM (
				SELECT id, ROW_NUMBER() OVER (PARTITION BY %s ORDER BY created_at DESC) AS row_num
				FROM %s
			) ranked WHERE ranked.row_num > ?
		)`, parentColumn, table), versionsKeptPerParent)
	return tasks.deleteInBatches(ctx, label, table, query)
}

// deleteInBatches reproduces process_cleanup_task: collect primary keys, delete
// them BATCH_SIZE at a time with a hard delete, and log a failed batch instead
// of aborting the run so one bad batch cannot block the rest.
func (tasks *MaintenanceTasks) deleteInBatches(ctx context.Context, label, table string, selector *gorm.DB) error {
	var identifiers []string
	if err := selector.Pluck("id", &identifiers).Error; err != nil {
		return fmt.Errorf("select %s rows: %w", label, err)
	}
	logger := tasks.logger.With("cleanup", label)
	logger.Info("cleanup task starting", "candidates", len(identifiers))

	deleted, batches := int64(0), 0
	for start := 0; start < len(identifiers); start += cleanupBatchSize {
		end := start + cleanupBatchSize
		if end > len(identifiers) {
			end = len(identifiers)
		}
		batches++
		// all_objects is a plain manager in Django, so these are hard deletes.
		result := tasks.db.WithContext(ctx).Table(table).Where("id IN ?", identifiers[start:end]).Delete(nil)
		if result.Error != nil {
			logger.Error("cleanup batch failed", "error", result.Error)
			continue
		}
		deleted += result.RowsAffected
	}
	logger.Info("cleanup task completed", "total_records_deleted", deleted, "total_batches", batches)
	return nil
}

// recentVisited reproduces recent_visited_task: refresh the timestamp when the
// entity was already visited, otherwise trim the list to twenty and insert.
func (tasks *MaintenanceTasks) recentVisited(ctx context.Context, arguments []any, keywords map[string]any) error {
	entityName := stringArgument(arguments, keywords, 0, "entity_name")
	entityIdentifier := stringArgument(arguments, keywords, 1, "entity_identifier")
	userID := stringArgument(arguments, keywords, 2, "user_id")
	projectID := stringArgument(arguments, keywords, 3, "project_id")
	slug := stringArgument(arguments, keywords, 4, "slug")

	var workspaceID string
	err := tasks.db.WithContext(ctx).Table("workspaces").Where("slug = ?", slug).
		Limit(1).Pluck("id", &workspaceID).Error
	if err != nil || workspaceID == "" {
		// Django lets Workspace.DoesNotExist reach its own except and returns.
		return nil
	}
	now := tasks.clock().UTC()

	var existing struct {
		ID string `gorm:"column:id"`
	}
	err = tasks.db.WithContext(ctx).Table("user_recent_visits").
		Where(`entity_name = ? AND entity_identifier = ? AND user_id = ? AND project_id = ?
			AND workspace_id = ? AND deleted_at IS NULL`,
			entityName, entityIdentifier, userID, nullableID(projectID), workspaceID).
		Order("created_at DESC").Limit(1).Take(&existing).Error
	if err == nil && existing.ID != "" {
		// save(update_fields=["visited_at"]) leaves updated_at alone.
		return tasks.db.WithContext(ctx).Table("user_recent_visits").
			Where("id = ?", existing.ID).Update("visited_at", now).Error
	}

	var count int64
	err = tasks.db.WithContext(ctx).Table("user_recent_visits").
		Where("user_id = ? AND workspace_id = ? AND deleted_at IS NULL", userID, workspaceID).
		Count(&count).Error
	if err != nil {
		return err
	}
	// Django tests for exactly twenty, so a list that somehow grew past the cap
	// is left alone. That is reproduced rather than corrected.
	if count == versionsKeptPerParent {
		var oldest struct {
			ID string `gorm:"column:id"`
		}
		err := tasks.db.WithContext(ctx).Table("user_recent_visits").
			Where("user_id = ? AND workspace_id = ? AND deleted_at IS NULL", userID, workspaceID).
			Order("created_at").Limit(1).Take(&oldest).Error
		if err == nil && oldest.ID != "" {
			// The model's delete() is a soft delete.
			err = tasks.db.WithContext(ctx).Table("user_recent_visits").
				Where("id = ?", oldest.ID).Update("deleted_at", now).Error
			if err != nil {
				return err
			}
		}
	}
	visitID, err := newTaskUUID()
	if err != nil {
		return err
	}
	return tasks.db.WithContext(ctx).Table("user_recent_visits").Create(map[string]any{
		"id": visitID, "created_at": now, "updated_at": now,
		"entity_name": entityName, "entity_identifier": nullableID(entityIdentifier),
		"user_id": userID, "visited_at": now, "project_id": nullableID(projectID),
		"workspace_id": workspaceID, "created_by_id": userID, "updated_by_id": userID,
	}).Error
}

// nullableID keeps an empty identifier out of a uuid column.
func nullableID(value string) any {
	if value == "" {
		return nil
	}
	return value
}
