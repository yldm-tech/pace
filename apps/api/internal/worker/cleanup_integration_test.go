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

// TestSoftDeleteCascadeAgainstDjangoSchema exercises the generated relation
// graph against the real tables: every table and column it names has to exist,
// and a workspace delete has to reach the rows hanging off it.
func TestSoftDeleteCascadeAgainstDjangoSchema(t *testing.T) {
	databaseURL := os.Getenv("WORKER_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set WORKER_TEST_DATABASE_URL to a disposable database with the Django schema")
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

	graph, err := loadRelationGraph()
	if err != nil {
		t.Fatal(err)
	}
	// Every column the graph names must exist, or the cascade would fail at
	// runtime on a table nobody happened to exercise.
	for key, model := range graph.Models {
		var count int64
		err := transaction.Raw(
			`SELECT COUNT(*) FROM information_schema.columns WHERE table_name = ? AND column_name = ?`,
			model.Table, model.PrimaryKey).Scan(&count).Error
		if err != nil {
			t.Fatalf("inspect %s: %v", key, err)
		}
		if count == 0 {
			t.Errorf("%s names table %q primary key %q, which does not exist", key, model.Table, model.PrimaryKey)
		}
		for _, relation := range model.Relations {
			var relationCount int64
			err := transaction.Raw(
				`SELECT COUNT(*) FROM information_schema.columns WHERE table_name = ? AND column_name = ?`,
				relation.RelatedTable, relation.RelatedColumn).Scan(&relationCount).Error
			if err != nil {
				t.Fatalf("inspect relation %s of %s: %v", relation.Accessor, key, err)
			}
			if relationCount == 0 {
				t.Errorf("%s relation %s names %s.%s, which does not exist",
					key, relation.Accessor, relation.RelatedTable, relation.RelatedColumn)
			}
		}
	}

	now := time.Date(2026, time.September, 15, 12, 0, 0, 0, time.UTC)
	tasks, err := NewDeletionTasks(transaction, nil)
	if err != nil {
		t.Fatal(err)
	}
	tasks.clock = func() time.Time { return now }

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	workspaceID, err := newTaskUUID()
	if err != nil {
		t.Fatal(err)
	}
	userID, err := newTaskUUID()
	if err != nil {
		t.Fatal(err)
	}
	err = transaction.Exec(
		`INSERT INTO users (id, password, is_superuser, username, email, is_staff, is_active, date_joined,
			created_at, updated_at, is_bot, is_password_autoset, is_email_verified, display_name,
			first_name, last_name, mobile_number, avatar, cover_image, last_location, created_location,
			is_managed, is_onboarded, is_password_expired, token, user_timezone, masked_at, is_active_mobile, is_active_desktop)
		 VALUES (?, '!', FALSE, ?, ?, FALSE, TRUE, ?, ?, ?, FALSE, TRUE, FALSE, ?, '', '', NULL, '', NULL, '', '',
			FALSE, FALSE, FALSE, '', 'UTC', NULL, TRUE, TRUE)`,
		userID, "cascade-"+suffix, "cascade-"+suffix+"@pace.invalid", now, now, now, "cascade",
	).Error
	if err != nil {
		t.Skipf("user columns differ from this fixture, skipping the cascade half: %v", err)
	}
	err = transaction.Exec(
		`INSERT INTO workspaces (id, created_at, updated_at, created_by_id, name, owner_id, slug, timezone, background_color)
		 VALUES (?, ?, ?, ?, ?, ?, ?, 'UTC', '#3f76ff')`,
		workspaceID, now, now, userID, "Cascade WS "+suffix, userID, "cascade-"+suffix,
	).Error
	if err != nil {
		t.Fatalf("insert workspace: %v", err)
	}
	themeID, err := newTaskUUID()
	if err != nil {
		t.Fatal(err)
	}
	err = transaction.Exec(
		`INSERT INTO workspace_themes (id, created_at, updated_at, created_by_id, workspace_id, name, actor_id, colors)
		 VALUES (?, ?, ?, ?, ?, ?, ?, '{}')`,
		themeID, now, now, userID, workspaceID, "Cascade Theme", userID,
	).Error
	if err != nil {
		t.Fatalf("insert workspace theme: %v", err)
	}

	err = tasks.softDeleteRelatedObjects(ctx, []any{"db", "workspace", workspaceID}, nil)
	if err != nil {
		t.Fatalf("cascade a workspace delete: %v", err)
	}

	var theme struct {
		DeletedAt   *time.Time `gorm:"column:deleted_at"`
		CreatedByID *string    `gorm:"column:created_by_id"`
	}
	if err := transaction.Table("workspace_themes").Where("id = ?", themeID).Take(&theme).Error; err != nil {
		t.Fatalf("read the cascaded theme: %v", err)
	}
	if theme.DeletedAt == nil {
		t.Fatal("the theme should have been soft deleted with its workspace")
	}
	// BaseModel.save blanks the audit columns when there is no current user, and
	// a worker never has one. This asserts the Go worker reproduces that rather
	// than quietly preserving the value.
	if theme.CreatedByID != nil {
		t.Fatalf("theme created_by = %v, want NULL the way BaseModel.save leaves it", *theme.CreatedByID)
	}

	var workspace struct {
		DeletedAt *time.Time `gorm:"column:deleted_at"`
	}
	if err := transaction.Table("workspaces").Where("id = ?", workspaceID).Take(&workspace).Error; err != nil {
		t.Fatalf("read the workspace: %v", err)
	}
	if workspace.DeletedAt == nil {
		t.Fatal("the workspace itself should have been soft deleted")
	}
}

// TestHardDeleteAgainstDjangoSchema proves the collector cascade satisfies the
// real foreign keys: Django creates them without ON DELETE actions, so a delete
// that forgets a child row fails on the constraint.
func TestHardDeleteAgainstDjangoSchema(t *testing.T) {
	databaseURL := os.Getenv("WORKER_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set WORKER_TEST_DATABASE_URL to a disposable database with the Django schema")
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

	now := time.Date(2026, time.September, 15, 12, 0, 0, 0, time.UTC)
	tasks, err := NewDeletionTasks(transaction, nil)
	if err != nil {
		t.Fatal(err)
	}
	tasks.clock = func() time.Time { return now }

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	userID, err := newTaskUUID()
	if err != nil {
		t.Fatal(err)
	}
	err = transaction.Exec(
		`INSERT INTO users (id, password, is_superuser, username, email, is_staff, is_active, date_joined,
			created_at, updated_at, is_bot, is_password_autoset, is_email_verified, display_name,
			first_name, last_name, mobile_number, avatar, cover_image, last_location, created_location,
			is_managed, is_onboarded, is_password_expired, token, user_timezone, masked_at, is_active_mobile, is_active_desktop)
		 VALUES (?, '!', FALSE, ?, ?, FALSE, TRUE, ?, ?, ?, FALSE, TRUE, FALSE, ?, '', '', NULL, '', NULL, '', '',
			FALSE, FALSE, FALSE, '', 'UTC', NULL, TRUE, TRUE)`,
		userID, "harddel-"+suffix, "harddel-"+suffix+"@pace.invalid", now, now, now, "harddel",
	).Error
	if err != nil {
		t.Skipf("user columns differ from this fixture: %v", err)
	}

	// A workspace soft-deleted well past the window, with a theme hanging off
	// it. The theme's foreign key has no ON DELETE action, so deleting the
	// workspace without collecting the theme first would fail.
	workspaceID, err := newTaskUUID()
	if err != nil {
		t.Fatal(err)
	}
	expired := now.AddDate(0, 0, -HardDeleteAfterDays-5)
	err = transaction.Exec(
		`INSERT INTO workspaces (id, created_at, updated_at, deleted_at, created_by_id, name, owner_id, slug, timezone, background_color)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'UTC', '#3f76ff')`,
		workspaceID, now, now, expired, userID, "Hard WS "+suffix, userID, "harddel-"+suffix,
	).Error
	if err != nil {
		t.Fatalf("insert workspace: %v", err)
	}
	themeID, err := newTaskUUID()
	if err != nil {
		t.Fatal(err)
	}
	err = transaction.Exec(
		`INSERT INTO workspace_themes (id, created_at, updated_at, created_by_id, workspace_id, name, actor_id, colors)
		 VALUES (?, ?, ?, ?, ?, ?, ?, '{}')`,
		themeID, now, now, userID, workspaceID, "Hard Theme", userID,
	).Error
	if err != nil {
		t.Fatalf("insert workspace theme: %v", err)
	}

	deleted, err := tasks.hardDeleteModel(ctx, "db.workspace", now.AddDate(0, 0, -HardDeleteAfterDays))
	if err != nil {
		t.Fatalf("hard delete the workspace: %v", err)
	}
	if deleted < 2 {
		t.Fatalf("hard delete removed %d rows, want the workspace and its theme", deleted)
	}
	// The constraint is deferred, so force it to be checked now rather than at
	// a commit this test never reaches.
	if err := transaction.Exec("SET CONSTRAINTS ALL IMMEDIATE").Error; err != nil {
		t.Fatalf("a foreign key was left dangling by the cascade: %v", err)
	}
	for _, check := range []struct {
		table string
		id    string
	}{{table: "workspaces", id: workspaceID}, {table: "workspace_themes", id: themeID}} {
		var count int64
		if err := transaction.Table(check.table).Where("id = ?", check.id).Count(&count).Error; err != nil {
			t.Fatalf("count %s: %v", check.table, err)
		}
		if count != 0 {
			t.Errorf("%s row survived the hard delete", check.table)
		}
	}
}
