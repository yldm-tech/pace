package beat

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// StaticEntry is one entry of celery.py's beat_schedule.
type StaticEntry struct {
	Name string
	Task string
	// Crontab fields, when the entry uses a crontab schedule.
	Minute, Hour, DayOfMonth, MonthOfYear, DayOfWeek string
	// IntervalMinutes is set instead when the entry uses schedule(run_every=...).
	IntervalMinutes int
}

// StaticSchedule is plane/celery.py's beat_schedule. DatabaseScheduler syncs
// these into the database on startup, so the Go beat has to do the same or an
// entry that was never edited through the admin would disappear.
//
// The metrics push interval is configurable, so it is filled in at sync time.
func StaticSchedule(metricsPushIntervalMinutes int) []StaticEntry {
	if metricsPushIntervalMinutes <= 0 || metricsPushIntervalMinutes > 10_000_000 {
		metricsPushIntervalMinutes = 360
	}
	return []StaticEntry{
		{
			Name:   "check-every-five-minutes-to-send-email-notifications",
			Task:   "plane.bgtasks.email_notification_task.stack_email_notification",
			Minute: "*/5", Hour: "*", DayOfMonth: "*", MonthOfYear: "*", DayOfWeek: "*",
		},
		{
			Name:            "push-instance-metrics",
			Task:            "plane.license.bgtasks.telemetry_metrics.push_instance_metrics",
			IntervalMinutes: metricsPushIntervalMinutes,
		},
		{
			Name:   "check-every-day-to-delete-hard-delete",
			Task:   "plane.bgtasks.deletion_task.hard_delete",
			Minute: "0", Hour: "0", DayOfMonth: "*", MonthOfYear: "*", DayOfWeek: "*",
		},
		{
			Name:   "check-every-day-to-archive-and-close",
			Task:   "plane.bgtasks.issue_automation_task.archive_and_close_old_issues",
			Minute: "0", Hour: "1", DayOfMonth: "*", MonthOfYear: "*", DayOfWeek: "*",
		},
		{
			Name:   "check-every-day-to-delete_exporter_history",
			Task:   "plane.bgtasks.exporter_expired_task.delete_old_s3_link",
			Minute: "30", Hour: "1", DayOfMonth: "*", MonthOfYear: "*", DayOfWeek: "*",
		},
		{
			Name:   "check-every-day-to-delete-file-asset",
			Task:   "plane.bgtasks.file_asset_task.delete_unuploaded_file_asset",
			Minute: "0", Hour: "2", DayOfMonth: "*", MonthOfYear: "*", DayOfWeek: "*",
		},
		{
			Name:   "check-every-day-to-delete-api-logs",
			Task:   "plane.bgtasks.cleanup_task.delete_api_logs",
			Minute: "30", Hour: "2", DayOfMonth: "*", MonthOfYear: "*", DayOfWeek: "*",
		},
		{
			Name:   "check-every-day-to-delete-email-notification-logs",
			Task:   "plane.bgtasks.cleanup_task.delete_email_notification_logs",
			Minute: "45", Hour: "2", DayOfMonth: "*", MonthOfYear: "*", DayOfWeek: "*",
		},
		{
			Name:   "check-every-day-to-delete-page-versions",
			Task:   "plane.bgtasks.cleanup_task.delete_page_versions",
			Minute: "0", Hour: "3", DayOfMonth: "*", MonthOfYear: "*", DayOfWeek: "*",
		},
		{
			Name:   "check-every-day-to-delete-issue-description-versions",
			Task:   "plane.bgtasks.cleanup_task.delete_issue_description_versions",
			Minute: "15", Hour: "3", DayOfMonth: "*", MonthOfYear: "*", DayOfWeek: "*",
		},
		{
			Name:   "check-every-day-to-delete-webhook-logs",
			Task:   "plane.bgtasks.cleanup_task.delete_webhook_logs",
			Minute: "30", Hour: "3", DayOfMonth: "*", MonthOfYear: "*", DayOfWeek: "*",
		},
		{
			Name:   "check-every-day-to-delete-exporter-history",
			Task:   "plane.bgtasks.exporter_expired_task.delete_old_s3_link",
			Minute: "45", Hour: "3", DayOfMonth: "*", MonthOfYear: "*", DayOfWeek: "*",
		},
	}
}

// SyncStatic reproduces DatabaseScheduler.setup_schedule: each static entry is
// written into django_celery_beat_periodictask keyed by name, creating the
// schedule row it points at. An entry an operator has since retimed through the
// admin keeps its own schedule only if it was renamed, which matches Django,
// where update_or_create overwrites the schedule on every startup.
func (store *Store) SyncStatic(ctx context.Context, entries []StaticEntry, now time.Time) error {
	return store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, entry := range entries {
			var crontabID, intervalID *int64
			if entry.IntervalMinutes > 0 {
				identifier, err := ensureInterval(tx, int64(entry.IntervalMinutes))
				if err != nil {
					return err
				}
				intervalID = &identifier
			} else {
				identifier, err := ensureCrontab(tx, entry)
				if err != nil {
					return err
				}
				crontabID = &identifier
			}
			if err := upsertPeriodicTask(tx, entry, crontabID, intervalID, now); err != nil {
				return err
			}
		}
		// django_celery_beat bumps this row whenever a schedule changes.
		return touchLastChange(tx, now)
	})
}

func ensureCrontab(tx *gorm.DB, entry StaticEntry) (int64, error) {
	var existing struct {
		ID int64 `gorm:"column:id"`
	}
	err := tx.Table(crontabScheduleTable).
		Where("minute = ? AND hour = ? AND day_of_month = ? AND month_of_year = ? AND day_of_week = ? AND timezone = ?",
			entry.Minute, entry.Hour, entry.DayOfMonth, entry.MonthOfYear, entry.DayOfWeek, "UTC").
		Limit(1).Take(&existing).Error
	if err == nil {
		return existing.ID, nil
	}
	row := map[string]any{
		"minute": entry.Minute, "hour": entry.Hour, "day_of_month": entry.DayOfMonth,
		"month_of_year": entry.MonthOfYear, "day_of_week": entry.DayOfWeek, "timezone": "UTC",
	}
	if err := tx.Table(crontabScheduleTable).Create(row).Error; err != nil {
		return 0, fmt.Errorf("create crontab schedule for %s: %w", entry.Name, err)
	}
	if err := tx.Table(crontabScheduleTable).
		Where("minute = ? AND hour = ? AND day_of_month = ? AND month_of_year = ? AND day_of_week = ? AND timezone = ?",
			entry.Minute, entry.Hour, entry.DayOfMonth, entry.MonthOfYear, entry.DayOfWeek, "UTC").
		Limit(1).Take(&existing).Error; err != nil {
		return 0, err
	}
	return existing.ID, nil
}

func ensureInterval(tx *gorm.DB, minutes int64) (int64, error) {
	var existing struct {
		ID int64 `gorm:"column:id"`
	}
	err := tx.Table(intervalScheduleTable).
		Where("every = ? AND period = ?", minutes, "minutes").Limit(1).Take(&existing).Error
	if err == nil {
		return existing.ID, nil
	}
	if err := tx.Table(intervalScheduleTable).
		Create(map[string]any{"every": minutes, "period": "minutes"}).Error; err != nil {
		return 0, fmt.Errorf("create interval schedule: %w", err)
	}
	if err := tx.Table(intervalScheduleTable).
		Where("every = ? AND period = ?", minutes, "minutes").Limit(1).Take(&existing).Error; err != nil {
		return 0, err
	}
	return existing.ID, nil
}

func upsertPeriodicTask(tx *gorm.DB, entry StaticEntry, crontabID, intervalID *int64, now time.Time) error {
	updates := map[string]any{
		"task": entry.Task, "crontab_id": crontabID, "interval_id": intervalID,
		"solar_id": nil, "clocked_id": nil,
		"args": "[]", "kwargs": "{}", "headers": "{}",
		"queue": nil, "exchange": nil, "routing_key": nil, "priority": nil,
		"expire_seconds": nil, "date_changed": now,
	}
	result := tx.Table(periodicTaskTable).Where("name = ?", entry.Name).Updates(updates)
	if result.Error != nil {
		return fmt.Errorf("update periodic task %s: %w", entry.Name, result.Error)
	}
	if result.RowsAffected > 0 {
		return nil
	}
	insert := map[string]any{
		"name": entry.Name, "enabled": true, "one_off": false, "total_run_count": 0,
		"description": "",
	}
	for key, value := range updates {
		insert[key] = value
	}
	if err := tx.Table(periodicTaskTable).Create(insert).Error; err != nil {
		return fmt.Errorf("create periodic task %s: %w", entry.Name, err)
	}
	return nil
}

func touchLastChange(tx *gorm.DB, now time.Time) error {
	result := tx.Table(periodicTasksTable).Where("ident = ?", 1).Update("last_update", now)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected > 0 {
		return nil
	}
	return tx.Table(periodicTasksTable).
		Create(map[string]any{"ident": 1, "last_update": now}).Error
}
