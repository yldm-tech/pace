package manage

import (
	"bufio"
	"context"
	_ "embed"
	"strings"
	"time"

	"gorm.io/gorm"
)

// waitForDB is manage.py wait_for_db: hold until the database answers.
//
// Upstream's loop is not what its name suggests. `connections["default"]` hands back a lazy wrapper without touching the server, so the loop never spins and the command reports success against a database that is not there. This one really asks, which is the one place here that does something upstream does not — a command whose whole job is to wait is not worth reproducing as a command that does not.
func waitForDB(ctx context.Context, env Environment, _ []string) error {
	write(env, "Waiting for database...")
	db, err := database(env)
	if err != nil {
		return err
	}
	for {
		if err := pingDatabase(ctx, db); err == nil {
			write(env, "Database available!")
			return nil
		}
		write(env, "Database unavailable, waititng 1 second...")
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pollInterval.database):
		}
	}
}

func pingDatabase(ctx context.Context, db *gorm.DB) error {
	pool, err := db.DB()
	if err != nil {
		return err
	}
	return pool.PingContext(ctx)
}

//go:embed migrations.tsv
var shippedMigrations string

// waitForMigrations is manage.py wait_for_migrations: hold until the schema is up to date, which is what gates the worker and the beat on a deploy.
//
// Django asks its own migration executor whether anything is pending. There is no executor here, so the question is asked of the database: is every migration the Django app ships recorded in django_migrations? The list is generated from that app and CI keeps it in step, so a migration added there cannot be forgotten here.
func waitForMigrations(ctx context.Context, env Environment, _ []string) error {
	db, err := database(env)
	if err != nil {
		return err
	}
	expected := parseShippedMigrations()
	for {
		pending, err := pendingMigrations(ctx, db, expected)
		if err == nil && len(pending) == 0 {
			write(env, "No migrations Pending. Starting processes ...")
			return nil
		}
		write(env, "Waiting for database migrations to complete...")
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pollInterval.migrations):
		}
	}
}

// migrationName is one migration, named the way django_migrations names it.
type migrationName struct{ App, Name string }

func parseShippedMigrations() []migrationName {
	names := make([]migrationName, 0, 128)
	scanner := bufio.NewScanner(strings.NewReader(shippedMigrations))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		app, name, found := strings.Cut(line, "\t")
		if !found {
			continue
		}
		names = append(names, migrationName{App: app, Name: name})
	}
	return names
}

// pendingMigrations is every shipped migration the table does not record. A missing table is every migration pending, which is what a database that has never been migrated looks like.
func pendingMigrations(ctx context.Context, db *gorm.DB, expected []migrationName) ([]migrationName, error) {
	var applied []struct {
		App  string `gorm:"column:app"`
		Name string `gorm:"column:name"`
	}
	if err := db.WithContext(ctx).Table("django_migrations").Select("app, name").Scan(&applied).Error; err != nil {
		return expected, err
	}
	present := map[migrationName]bool{}
	for _, row := range applied {
		present[migrationName{App: row.App, Name: row.Name}] = true
	}
	pending := make([]migrationName, 0)
	for _, name := range expected {
		if !present[name] {
			pending = append(pending, name)
		}
	}
	return pending, nil
}

// clearCache is manage.py clear_cache: empty the cache, or drop one key from it.
//
// django-redis writes a key as ":1:<name>" — the empty key prefix, the version, and the name — and clearing everything flushes the whole database rather than scanning for a pattern. Both are reproduced. A failure is reported and swallowed, so the command answers zero either way.
func clearCache(ctx context.Context, env Environment, arguments []string) error {
	if env.Redis == nil {
		write(env, "Failed to clear cache")
		return nil
	}
	if key, given := flagValue(arguments, "key"); given && key != "" {
		if err := env.Redis.Del(ctx, ":1:"+key).Err(); err != nil {
			write(env, "Failed to clear cache")
			return nil
		}
		write(env, "Cache Cleared for key: %s", key)
		return nil
	}
	if err := env.Redis.FlushDB(ctx).Err(); err != nil {
		write(env, "Failed to clear cache")
		return nil
	}
	write(env, "Cache Cleared")
	return nil
}
