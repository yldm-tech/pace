package manage

import (
	"context"
	"fmt"
	"strings"

	"github.com/yldm-tech/pace/apps/api-go/internal/migrate"
)

// runMigrate is manage.py migrate: bring the database up to the schema the Django app describes.
//
// It applies what internal/migrate replays — the statements recorded from Django's own migrate — in Django's order, into Django's ledger. All fifty-eight of the RunPython operations are ported, so nothing is refused any more; the check is kept because a migration added to the Python app arrives here with no counterpart, and refusing it by name beats applying half of it.
func runMigrate(ctx context.Context, env Environment, arguments []string) error {
	db, err := database(env)
	if err != nil {
		return err
	}
	pool, err := db.DB()
	if err != nil {
		return err
	}

	// `migrate <app> <name>` stops at that migration, the way Django's does. With no arguments it runs the whole plan.
	target := ""
	if len(arguments) >= 2 {
		target = arguments[0] + "." + arguments[1]
	} else if len(arguments) == 1 {
		return fmt.Errorf("migrate takes either no arguments or an app and a migration name, and was given %q", arguments[0])
	}

	unported, err := migrate.Unported(target)
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

	applied, err := migrate.ApplyThrough(ctx, pool, target, nil)
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
