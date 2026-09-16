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

// operationsFor resolves the coded operations of one migration, keyed by the function name the recorded file's RUN markers use.
//
// Anything missing is reported by name, all of them at once, because a caller fixing this wants the list rather than one at a time.
func operationsFor(migration Migration) (map[string]Operation, error) {
	resolved := map[string]Operation{}
	var missing []string
	for _, function := range migration.Coded {
		if function == RunSQLOperation {
			continue
		}
		key := migration.Key() + "." + function
		operation, ported := registry[key]
		if !ported {
			missing = append(missing, function)
			continue
		}
		resolved[key] = operation
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("these operations carry Python that has not been ported: %s", strings.Join(missing, ", "))
	}
	return resolved, nil
}

// Unported is every operation that still has no Go counterpart, named "app.migration.function".
//
// The target scopes it the way ApplyThrough scopes the plan: asking about a run that stops at db.0035 should not report operations in db.0120 that the run will never reach. An empty target asks about the whole plan.
//
// It is what the command reports when it refuses, and what the guard test in this package counts.
func Unported(target string) ([]string, error) {
	plan, err := Plan()
	if err != nil {
		return nil, err
	}
	if target != "" {
		cut := -1
		for index, migration := range plan {
			if migration.Key() == target {
				cut = index
				break
			}
		}
		if cut < 0 {
			return nil, fmt.Errorf("%s is not in the plan", target)
		}
		plan = plan[:cut+1]
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
