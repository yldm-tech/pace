package migrate

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"

	_ "github.com/lib/pq"
)

// TestGoBuildsTheSameSchemaAsDjango applies the plan to an empty database and compares what it built against what Django builds.
//
// It is opt-in because it creates a database and drops it again. The comparison is against testdata/schema.tsv, which is the same query run against a database Django migrated — so this fails if the recorded statements stop reproducing Django's schema, which is the only thing that makes replaying them safe.
//
// The coded operations run for real — every one of them is ported now, so the stubbing below finds nothing to stub. It is kept because it is what lets this test still mean something while a newly added migration is being worked on. Whether those operations are right is a different question, answered by tools/check_migration_operations.py; what this settles is that everything Django does to the shape of the database is in the recorded files.
func TestGoBuildsTheSameSchemaAsDjango(t *testing.T) {
	databaseURL := os.Getenv("MIGRATE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set MIGRATE_TEST_DATABASE_URL to an empty, disposable database")
	}
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	applied, err := Applied(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) > 0 {
		t.Fatalf("the database already has %d migrations applied, and this needs an empty one", len(applied))
	}

	withStubbedOperations(t)
	ran, err := Apply(ctx, db, nil)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := Plan()
	if err != nil {
		t.Fatal(err)
	}
	if ran != len(plan) {
		t.Fatalf("applied %d migrations out of %d", ran, len(plan))
	}

	want, err := os.ReadFile("testdata/schema.tsv")
	if err != nil {
		t.Fatal(err)
	}
	got := describeSchema(t, db)
	if got != strings.TrimSpace(string(want)) {
		t.Error("the schema Go builds is not the schema Django builds")
		reportFirstDifference(t, strings.TrimSpace(string(want)), got)
	}
}

// withStubbedOperations registers a do-nothing counterpart for every operation that has no Go port yet, and puts the registry back afterwards.
func withStubbedOperations(t *testing.T) {
	t.Helper()
	missing, err := Unported("")
	if err != nil {
		t.Fatal(err)
	}
	restore := make([]string, 0, len(missing))
	for _, key := range missing {
		registry[key] = Operation{Name: key, Run: func(context.Context, *sql.Tx) error { return nil }}
		restore = append(restore, key)
	}
	t.Cleanup(func() {
		for _, key := range restore {
			delete(registry, key)
		}
	})
}

// describeSchema runs testdata/schema_query.sql and returns its rows as lines.
func describeSchema(t *testing.T, db *sql.DB) string {
	t.Helper()
	query, err := os.ReadFile("testdata/schema_query.sql")
	if err != nil {
		t.Fatal(err)
	}
	rows, err := db.Query(string(query))
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var lines []string
	for rows.Next() {
		var kind, detail string
		if err := rows.Scan(&kind, &detail); err != nil {
			t.Fatal(err)
		}
		lines = append(lines, kind+"\t"+detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return strings.Join(lines, "\n")
}

// reportFirstDifference prints the lines one side has and the other does not, which is what a reader needs and a whole-file diff is not.
func reportFirstDifference(t *testing.T, want, got string) {
	t.Helper()
	inWant := map[string]bool{}
	for _, line := range strings.Split(want, "\n") {
		inWant[line] = true
	}
	inGot := map[string]bool{}
	for _, line := range strings.Split(got, "\n") {
		inGot[line] = true
	}
	reported := 0
	for _, line := range strings.Split(want, "\n") {
		if !inGot[line] {
			t.Errorf("Django has it and Go does not: %s", line)
			if reported++; reported > 20 {
				t.Error("...")
				break
			}
		}
	}
	reported = 0
	for _, line := range strings.Split(got, "\n") {
		if !inWant[line] {
			t.Errorf("Go has it and Django does not: %s", line)
			if reported++; reported > 20 {
				t.Error("...")
				break
			}
		}
	}
}
