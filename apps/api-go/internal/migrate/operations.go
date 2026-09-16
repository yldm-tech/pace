package migrate

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
)

// Operation is one RunPython the Django app carries, ported.
//
// It gets the migration's own transaction, so what it does and the ledger row that records it stand or fall together.
type Operation struct {
	// Name is the Python function's name, which is what plan.tsv records and what a message about an unported operation says.
	Name string
	Run  func(ctx context.Context, tx *sql.Tx) error
}

// registry holds every ported operation, keyed by "app.name.function".
//
// The key carries the migration as well as the function because the names repeat: three different migrations define a function called update_pages or similar, and they do different things. Keying on the function alone would silently run one migration's code for another's.
var registry = map[string]Operation{}

// register adds an operation. It panics on a duplicate, which can only be a copy-and-paste in this package.
func register(app, migration, function string, run func(ctx context.Context, tx *sql.Tx) error) {
	key := app + "." + migration + "." + function
	if _, taken := registry[key]; taken {
		panic("migrate: " + key + " is registered twice")
	}
	registry[key] = Operation{Name: key, Run: run}
}

// RunSQLOperation is the name plan.tsv gives a RunSQL. Its statements were recorded like any other, so there is nothing to port and nothing to look up.
const RunSQLOperation = "RunSQL"

// operationsFor resolves the coded operations of one migration, in the order the migration runs them.
//
// Anything missing is reported by name, all of them at once, because a caller fixing this wants the list rather than one at a time.
func operationsFor(migration Migration) ([]Operation, error) {
	var resolved []Operation
	var missing []string
	for _, function := range migration.Coded {
		if function == RunSQLOperation {
			continue
		}
		operation, ported := registry[migration.Key()+"."+function]
		if !ported {
			missing = append(missing, function)
			continue
		}
		resolved = append(resolved, operation)
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("these operations carry Python that has not been ported: %s", strings.Join(missing, ", "))
	}
	return resolved, nil
}

// Unported is every operation in the plan that still has no Go counterpart, named "app.migration.function".
//
// It is what the command reports when it refuses, and what the guard test in this package counts.
func Unported() ([]string, error) {
	plan, err := Plan()
	if err != nil {
		return nil, err
	}
	var missing []string
	for _, migration := range plan {
		for _, function := range migration.Coded {
			if function == RunSQLOperation {
				continue
			}
			if _, ported := registry[migration.Key()+"."+function]; !ported {
				missing = append(missing, migration.Key()+"."+function)
			}
		}
	}
	sort.Strings(missing)
	return missing, nil
}
