package beat

import (
	"context"
	"os"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// TestBeatStoreAgainstDjangoSchema is opt-in because it writes to the
// configured database. Every write is rolled back, and Go never creates or
// migrates tables. It is the check that the django_celery_beat table and column
// names this package hardcodes are the real ones.
func TestBeatStoreAgainstDjangoSchema(t *testing.T) {
	databaseURL := os.Getenv("BEAT_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set BEAT_TEST_DATABASE_URL to a disposable database with the Django schema")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	database, err := gorm.Open(postgres.Open(databaseURL), &gorm.Config{})
	if err != nil {
		t.Fatalf("open integration database: %v", err)
	}
	sqlDatabase, err := database.DB()
	if err != nil {
		t.Fatalf("access integration database: %v", err)
	}
	t.Cleanup(func() { _ = sqlDatabase.Close() })

	transaction := database.WithContext(ctx).Begin()
	if transaction.Error != nil {
		t.Fatalf("begin integration transaction: %v", transaction.Error)
	}
	t.Cleanup(func() { _ = transaction.Rollback().Error })

	store := NewStore(transaction)
	now := time.Date(2026, time.September, 15, 12, 0, 0, 0, time.UTC)

	// Syncing the static schedule is what DatabaseScheduler does on startup.
	schedule := StaticSchedule(360)
	if err := store.SyncStatic(ctx, schedule, now); err != nil {
		t.Fatalf("sync the static schedule into the Django tables: %v", err)
	}
	// Running it twice must be a no-op rather than a duplicate, because beat
	// restarts.
	if err := store.SyncStatic(ctx, schedule, now); err != nil {
		t.Fatalf("re-sync the static schedule: %v", err)
	}

	var count int64
	err = transaction.Table(periodicTaskTable).Where("name IN ?", scheduleNames(schedule)).Count(&count).Error
	if err != nil {
		t.Fatalf("count periodic tasks: %v", err)
	}
	if count != int64(len(schedule)) {
		t.Fatalf("synced %d periodic tasks, want %d", count, len(schedule))
	}

	entries, unsupported, err := store.Entries(ctx)
	if err != nil {
		t.Fatalf("read the schedule back: %v", err)
	}
	for _, reason := range unsupported {
		t.Errorf("a synced entry could not be scheduled: %s", reason)
	}
	byName := map[string]Entry{}
	for _, entry := range entries {
		byName[entry.Name] = entry
	}
	hardDelete, known := byName["check-every-day-to-delete-hard-delete"]
	if !known {
		t.Fatal("the hard delete entry did not come back from the database")
	}
	if hardDelete.Crontab == nil {
		t.Fatal("the hard delete entry lost its crontab schedule")
	}
	if hardDelete.Task != "plane.bgtasks.deletion_task.hard_delete" {
		t.Fatalf("hard delete task = %q", hardDelete.Task)
	}
	metrics, known := byName["push-instance-metrics"]
	if !known || metrics.IntervalEvery == nil || *metrics.IntervalEvery != 360 {
		t.Fatalf("metrics entry = %+v, want a 360 minute interval", metrics)
	}

	// Recording a run has to write the columns Django's sync writes.
	if err := store.MarkRun(ctx, hardDelete, now); err != nil {
		t.Fatalf("record a run: %v", err)
	}
	var recorded struct {
		LastRunAt     *time.Time `gorm:"column:last_run_at"`
		TotalRunCount int64      `gorm:"column:total_run_count"`
	}
	err = transaction.Table(periodicTaskTable).Where("id = ?", hardDelete.ID).Take(&recorded).Error
	if err != nil {
		t.Fatalf("read the recorded run: %v", err)
	}
	if recorded.LastRunAt == nil || !recorded.LastRunAt.UTC().Equal(now) {
		t.Fatalf("last_run_at = %v, want %v", recorded.LastRunAt, now)
	}
	if recorded.TotalRunCount != hardDelete.TotalRunCount+1 {
		t.Fatalf("total_run_count = %d, want %d", recorded.TotalRunCount, hardDelete.TotalRunCount+1)
	}

	changed, err := store.LastChange(ctx)
	if err != nil {
		t.Fatalf("read the change marker: %v", err)
	}
	if changed.IsZero() {
		t.Fatal("syncing the schedule should have stamped the change marker")
	}
}

func scheduleNames(entries []StaticEntry) []string {
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name)
	}
	return names
}
