// Package migrate applies the Django app's migrations from Go.
//
// Django owns the schema, and this package does not try to redescribe it. What every migration does to a database was recorded from Django itself — a real migrate, with the statements captured as they were executed — and is replayed here in the same order, against the same ledger table. The generator is apps/api-go/tools/generate_migration_sql.py, and CI regenerates its output and diffs it, so a migration added to the Python app cannot be forgotten here.
//
// The part that could not be recorded is RunPython. What those operations do depends on the rows already in the database, and the database they were recorded against was empty, so they are ported to Go by hand and registered in operations.go. A migration carrying one that has not been ported is refused by name rather than half applied.
package migrate

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"log/slog"
	"path"
	"strings"
	"time"
)

//go:embed plan.tsv ledger.sql signal_rows.sql sql
var files embed.FS

// ledger is Django's own table, and stays Django's own table. Both sides read it: this package to know what is applied, and internal/manage's wait_for_migrations to know whether anything is outstanding.
const ledger = "django_migrations"

// unrenderable is what the generator writes where a statement carried bound parameters it could not fold in. Nothing in the recorded output has it, and a file that grows one is refused rather than replayed wrongly.
const unrenderable = "-- UNRENDERABLE PARAMETERS"

// Migration is one row of the plan: a migration, the names of the operations in it that carry code rather than schema changes, and whether Django wraps it in a transaction.
type Migration struct {
	App   string
	Name  string
	Coded []string
	// Atomic is Django's own Migration.atomic. A handful are false, and they have to be: CREATE INDEX CONCURRENTLY is refused inside a transaction block, so a migration adding one cannot be wrapped in it.
	Atomic bool
}

// Key is how the ledger names this migration, and how a caller names it in a message.
func (m Migration) Key() string { return m.App + "." + m.Name }

// Plan is every migration the Django app applies, in the order Django applies it.
func Plan() ([]Migration, error) {
	contents, err := files.ReadFile("plan.tsv")
	if err != nil {
		return nil, err
	}
	var plan []Migration
	for _, line := range strings.Split(string(contents), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 4 {
			return nil, fmt.Errorf("malformed plan row %q", line)
		}
		migration := Migration{App: fields[0], Name: fields[1], Atomic: fields[3] == "atomic"}
		if fields[3] != "atomic" && fields[3] != "non-atomic" {
			return nil, fmt.Errorf("plan row %q does not say whether it is atomic", line)
		}
		if fields[2] != "" {
			migration.Coded = strings.Split(fields[2], ";")
		}
		plan = append(plan, migration)
	}
	if len(plan) == 0 {
		return nil, fmt.Errorf("the plan is empty, which cannot be the whole app")
	}
	return plan, nil
}

// statements reads the recorded statements of one migration, one to a line.
func statements(migration Migration) ([]string, error) {
	contents, err := files.ReadFile(path.Join("sql", migration.App, migration.Name+".sql"))
	if err != nil {
		return nil, fmt.Errorf("no recorded sql for %s: %w", migration.Key(), err)
	}
	if strings.Contains(string(contents), unrenderable) {
		return nil, fmt.Errorf("%s carries a statement the generator could not render, so it cannot be replayed", migration.Key())
	}
	var found []string
	for _, line := range strings.Split(string(contents), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "--") {
			continue
		}
		found = append(found, trimmed)
	}
	return found, nil
}

// Applied is the set of migrations the ledger already records, keyed the way Key does.
//
// A database with no ledger at all is a database nothing has ever migrated, which is not an error — it is the ordinary state of a fresh install, and ensureLedger creates the table before anything is applied.
func Applied(ctx context.Context, db *sql.DB) (map[string]bool, error) {
	var exists bool
	if err := db.QueryRowContext(ctx, `SELECT to_regclass($1) IS NOT NULL`, ledger).Scan(&exists); err != nil {
		return nil, fmt.Errorf("look for the migration ledger: %w", err)
	}
	applied := map[string]bool{}
	if !exists {
		return applied, nil
	}
	rows, err := db.QueryContext(ctx, `SELECT app, name FROM `+ledger)
	if err != nil {
		return nil, fmt.Errorf("read the migration ledger: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var app, name string
		if err := rows.Scan(&app, &name); err != nil {
			return nil, err
		}
		applied[app+"."+name] = true
	}
	return applied, rows.Err()
}

// Apply brings the database up to the plan, and reports how many migrations it ran.
//
// Each migration is one transaction: its statements, then whatever code it carries, then the ledger row. Postgres does DDL transactionally, so a migration that fails part way leaves the database as it was and the ledger without its row — which is what Django's own non-atomic=False default does too.
func Apply(ctx context.Context, db *sql.DB, logger *slog.Logger) (int, error) {
	plan, err := Plan()
	if err != nil {
		return 0, err
	}
	if err := ensureLedger(ctx, db); err != nil {
		return 0, err
	}
	applied, err := Applied(ctx, db)
	if err != nil {
		return 0, err
	}
	ran := 0
	for _, migration := range plan {
		if applied[migration.Key()] {
			continue
		}
		if err := applyOne(ctx, db, migration); err != nil {
			return ran, fmt.Errorf("apply %s: %w", migration.Key(), err)
		}
		ran++
		if logger != nil {
			logger.Info("applied a migration", "migration", migration.Key())
		}
	}
	if err := applySignalRows(ctx, db, logger); err != nil {
		return ran, err
	}
	return ran, nil
}

// ensureLedger creates django_migrations if it is not there.
//
// No migration creates it. Django's MigrationRecorder does, before the first migration runs, so it belongs to none of them and the recorded statements do not carry it. Its definition was recorded from the same run as everything else rather than written out from memory.
func ensureLedger(ctx context.Context, db *sql.DB) error {
	var exists bool
	if err := db.QueryRowContext(ctx, `SELECT to_regclass($1) IS NOT NULL`, ledger).Scan(&exists); err != nil {
		return fmt.Errorf("look for the migration ledger: %w", err)
	}
	if exists {
		return nil
	}
	contents, err := files.ReadFile("ledger.sql")
	if err != nil {
		return err
	}
	for _, line := range strings.Split(string(contents), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "--") {
			continue
		}
		if _, err := db.ExecContext(ctx, trimmed); err != nil {
			return fmt.Errorf("create the migration ledger: %w", err)
		}
	}
	return nil
}

func applyOne(ctx context.Context, db *sql.DB, migration Migration) error {
	recorded, err := statements(migration)
	if err != nil {
		return err
	}
	// Resolved before anything is executed, so a migration carrying an unported operation is refused rather than half applied.
	coded, err := operationsFor(migration)
	if err != nil {
		return err
	}
	if !migration.Atomic {
		return applyOutsideTransaction(ctx, db, migration, recorded, coded)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, statement := range recorded {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("%s: %w", statement, err)
		}
	}
	for _, operation := range coded {
		if err := operation.Run(ctx, tx); err != nil {
			return fmt.Errorf("%s: %w", operation.Name, err)
		}
	}
	if err := recordApplied(ctx, tx, migration); err != nil {
		return err
	}
	return tx.Commit()
}

// applyOutsideTransaction runs a migration Django marks atomic = False.
//
// Such a migration carries a statement Postgres refuses inside a transaction block — CREATE INDEX CONCURRENTLY is the only reason any of them are marked — so there is no transaction to roll back and a failure part way leaves the database part way. That is exactly what Django does with the same migration, and it is why so few of them are marked.
//
// A coded operation still needs a transaction handle, so it gets one of its own. Nothing in the plan pairs a concurrent index with ported code, but the shape is written out rather than left to chance.
func applyOutsideTransaction(ctx context.Context, db *sql.DB, migration Migration, recorded []string, coded []Operation) error {
	for _, statement := range recorded {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("%s: %w", statement, err)
		}
	}
	for _, operation := range coded {
		if err := runInOwnTransaction(ctx, db, operation); err != nil {
			return err
		}
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := recordApplied(ctx, tx, migration); err != nil {
		return err
	}
	return tx.Commit()
}

func runInOwnTransaction(ctx context.Context, db *sql.DB, operation Operation) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := operation.Run(ctx, tx); err != nil {
		return fmt.Errorf("%s: %w", operation.Name, err)
	}
	return tx.Commit()
}

func recordApplied(ctx context.Context, tx *sql.Tx, migration Migration) error {
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO `+ledger+` (app, name, applied) VALUES ($1, $2, $3)`,
		migration.App, migration.Name, time.Now().UTC()); err != nil {
		return fmt.Errorf("record in the ledger: %w", err)
	}
	return nil
}

// applySignalRows fills in django_content_type and auth_permission.
//
// No migration writes those. Django hangs handlers off post_migrate that fill them in as models appear, and a database built only from the recorded statements would have the tables and none of the rows. Nothing in Plane reads either one — there is no GenericForeignKey in the app, and its permission classes are DRF's rather than Django's — but a database this builds and a database Django builds have to be the same thing, or comparing them stops meaning anything.
//
// It runs when the table is empty, which is the only state it can safely fill: the recorded rows carry their own ids, so inserting them over rows that are already there would collide.
func applySignalRows(ctx context.Context, db *sql.DB, logger *slog.Logger) error {
	var present int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM django_content_type`).Scan(&present); err != nil {
		return fmt.Errorf("count content types: %w", err)
	}
	if present > 0 {
		return nil
	}
	contents, err := files.ReadFile("signal_rows.sql")
	if err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	inserted := 0
	for _, line := range strings.Split(string(contents), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "--") {
			continue
		}
		if _, err := tx.ExecContext(ctx, trimmed); err != nil {
			return fmt.Errorf("%s: %w", trimmed, err)
		}
		inserted++
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if logger != nil {
		logger.Info("filled in the rows post_migrate would have written", "statements", inserted)
	}
	return nil
}
