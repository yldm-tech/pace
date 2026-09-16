package migrate

import (
	"context"
	"database/sql"
	"strings"
	"testing"
)

// TestEveryMigrationInThePlanHasItsStatements is the guard against a half-regenerated tree.
//
// The plan and the sql directory are written by the same run of the generator, so they cannot normally disagree — but a merge that takes one side of a conflict and not the other would produce a plan naming a file that is not there, and the failure would otherwise be a migration refusing to apply at deploy time rather than a test failing here.
func TestEveryMigrationInThePlanHasItsStatements(t *testing.T) {
	plan, err := Plan()
	if err != nil {
		t.Fatal(err)
	}
	if len(plan) < 150 {
		t.Fatalf("the plan has only %d migrations, which cannot be the whole app", len(plan))
	}
	for _, migration := range plan {
		if _, err := statements(migration); err != nil {
			t.Errorf("%s: %v", migration.Key(), err)
		}
	}
}

// TestNothingInThePlanRepeats catches a generator that walked the graph twice, which would apply a migration a second time and fail on whatever it had already done.
func TestNothingInThePlanRepeats(t *testing.T) {
	plan, err := Plan()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, migration := range plan {
		if seen[migration.Key()] {
			t.Errorf("%s appears in the plan twice", migration.Key())
		}
		seen[migration.Key()] = true
	}
}

// TestAMigrationCarryingUnportedCodeIsRefused pins the refusal, which is the whole safety of shipping this before all of the Python is ported.
func TestAMigrationCarryingUnportedCodeIsRefused(t *testing.T) {
	migration := Migration{App: "db", Name: "0035_auto_20230704_2225", Coded: []string{"not_a_real_operation"}}
	_, err := operationsFor(migration)
	if err == nil {
		t.Fatal("an operation with no Go counterpart should be refused")
	}
	if !strings.Contains(err.Error(), "not_a_real_operation") {
		t.Fatalf("the refusal should name the operation, and says %q", err)
	}
}

// TestARunSQLNeedsNoPort records that RunSQL is in the plan's third column but is not something to port: its statements were recorded like any other operation's.
func TestARunSQLNeedsNoPort(t *testing.T) {
	migration := Migration{App: "db", Name: "whatever", Coded: []string{RunSQLOperation}}
	resolved, err := operationsFor(migration)
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved) != 0 {
		t.Fatalf("a RunSQL should resolve to nothing to run, and resolved to %d", len(resolved))
	}
}

// TestPortedOperationsAreNamedByMigration guards the registry key.
//
// The Python function names repeat across migrations, so a registry keyed on the function alone would run one migration's code for another's. This fails if a key stops carrying the migration it belongs to.
func TestPortedOperationsAreNamedByMigration(t *testing.T) {
	register("testapp", "0001_example", "an_operation", func(context.Context, *sql.Tx) error { return nil })
	t.Cleanup(func() { delete(registry, "testapp.0001_example.an_operation") })

	resolved, err := operationsFor(Migration{App: "testapp", Name: "0001_example", Coded: []string{"an_operation"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved) != 1 || resolved[0].Name != "testapp.0001_example.an_operation" {
		t.Fatalf("resolved to %+v", resolved)
	}
	// The same function name under a different migration is a different operation, and is not ported.
	if _, err := operationsFor(Migration{App: "testapp", Name: "0002_other", Coded: []string{"an_operation"}}); err == nil {
		t.Fatal("the same function name under another migration should not resolve")
	}
}
