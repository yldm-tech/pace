package manage

import (
	"context"
	"fmt"
	"strings"

	"github.com/yldm-tech/pace/apps/api-go/internal/migrate"
)

// runMigrate is manage.py migrate: bring the database up to the schema the Django app describes.
//
// It applies what internal/migrate replays — the statements recorded from Django's own migrate — in Django's order, into Django's ledger. What it will not do is guess. A migration carrying a RunPython that has no Go counterpart is refused before anything is applied, naming the operations, because half a migration is worse than none.
//
// On a database with rows in it that refusal is the whole point: those operations exist to move data that is already there. On an empty one they would do nothing either way, which is why a fresh install already comes out identical to Django's — but the command does not make that distinction, because "it happens to be empty" is not something worth betting a schema on.
func runMigrate(ctx context.Context, env Environment, _ []string) error {
	db, err := database(env)
	if err != nil {
		return err
	}
	pool, err := db.DB()
	if err != nil {
		return err
	}

	unported, err := migrate.Unported()
	if err != nil {
		return err
	}
	if len(unported) > 0 {
		write(env, fmt.Sprintf("%d migrations carry Python that has not been ported to Go:", len(unported)))
		for _, name := range unported {
			write(env, "  "+name)
		}
		return fmt.Errorf("refusing to migrate: %s", strings.Join(unported, ", "))
	}

	applied, err := migrate.Apply(ctx, pool, nil)
	if err != nil {
		return err
	}
	if applied == 0 {
		write(env, "No migrations to apply.")
		return nil
	}
	write(env, fmt.Sprintf("Applied %d migrations.", applied))
	return nil
}

// showMigrations is manage.py showmigrations: which of the shipped migrations the database records.
func showMigrations(ctx context.Context, env Environment, _ []string) error {
	db, err := database(env)
	if err != nil {
		return err
	}
	pool, err := db.DB()
	if err != nil {
		return err
	}
	plan, err := migrate.Plan()
	if err != nil {
		return err
	}
	applied, err := migrate.Applied(ctx, pool)
	if err != nil {
		return err
	}
	for _, migration := range plan {
		mark := " "
		if applied[migration.Key()] {
			mark = "X"
		}
		write(env, fmt.Sprintf("[%s]  %s", mark, migration.Key()))
	}
	return nil
}
