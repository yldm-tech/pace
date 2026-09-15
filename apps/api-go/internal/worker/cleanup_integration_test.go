package worker

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// TestMaintenanceTasksAgainstDjangoSchema is opt-in because it writes to the
// configured database. Every write is rolled back, and Go never creates or
// migrates tables.
func TestMaintenanceTasksAgainstDjangoSchema(t *testing.T) {
	databaseURL := os.Getenv("WORKER_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set WORKER_TEST_DATABASE_URL to a disposable database with the Django schema")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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

	now := time.Date(2026, time.September, 15, 12, 0, 0, 0, time.UTC)
	tasks := NewMaintenanceTasks(transaction, DefaultRetentionSettings(), nil)
	tasks.clock = func() time.Time { return now }

	// The cleanup queries have to match the real columns; an empty database is
	// enough to prove the SQL is valid against the Django schema.
	for name, run := range map[string]Handler{
		"api logs":                   tasks.deleteAPILogs,
		"email notification logs":    tasks.deleteEmailNotificationLogs,
		"webhook logs":               tasks.deleteWebhookLogs,
		"page versions":              tasks.deletePageVersions,
		"issue description versions": tasks.deleteIssueDescriptionVersions,
	} {
		if err := run(ctx, nil, nil); err != nil {
			t.Fatalf("%s cleanup against Django schema: %v", name, err)
		}
	}

	// Rows past the retention window go, rows inside it stay.
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	expired, err := newTaskUUID()
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := newTaskUUID()
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		id        string
		createdAt time.Time
	}{
		{id: expired, createdAt: now.AddDate(0, 0, -30)},
		{id: fresh, createdAt: now.AddDate(0, 0, -1)},
	} {
		err := transaction.Exec(
			`INSERT INTO api_activity_logs (id, created_at, updated_at, token_identifier, path, method, response_code, ip_address, user_agent)
			 VALUES (?, ?, ?, ?, ?, 'GET', 200, NULL, NULL)`,
			row.id, row.createdAt, row.createdAt, "integration-"+suffix, "/api/test/",
		).Error
		if err != nil {
			t.Fatalf("insert api activity log through Django schema: %v", err)
		}
	}
	if err := tasks.deleteAPILogs(ctx, nil, nil); err != nil {
		t.Fatalf("delete api logs: %v", err)
	}
	var remaining []string
	err = transaction.Table("api_activity_logs").
		Where("token_identifier = ?", "integration-"+suffix).Pluck("id", &remaining).Error
	if err != nil {
		t.Fatalf("read remaining api logs: %v", err)
	}
	if len(remaining) != 1 || remaining[0] != fresh {
		t.Fatalf("remaining api logs = %#v, want only the fresh row", remaining)
	}
}
