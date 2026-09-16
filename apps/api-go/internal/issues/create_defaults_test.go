package issues

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCreateDefaultsCoverTheRequiredColumns is the guard for the one insert the static checks cannot see.
//
// A work item is written from a map built at runtime out of the request, so nothing reading the source can tell which columns it ends up with. What can be checked is that every NOT NULL column with no database default is accounted for: either PrepareCreate sets it, or CreateDefaults has a value for it.
//
// Leaving one out is not a compile error and not a test failure anywhere else. It is a 500 the first time somebody creates a work item without that field — priority was exactly that, and only turned up because a request finally omitted it.
func TestCreateDefaultsCoverTheRequiredColumns(t *testing.T) {
	covered := map[string]bool{}
	for column := range CreateDefaults {
		covered[column] = true
	}
	for _, column := range CreateAssigned {
		covered[column] = true
	}

	required := requiredIssueColumns(t)
	if len(required) < 8 {
		t.Fatalf("only %d required columns found for issues, so this guard is not reading the schema", len(required))
	}
	for _, column := range required {
		if !covered[column] {
			t.Errorf("issues.%s is NOT NULL with no database default and PrepareCreate neither sets it nor defaults it", column)
		}
	}
}

// requiredIssueColumns reads the columns of issues that an insert has to name, out of the schema recorded from a fully migrated database.
func requiredIssueColumns(t *testing.T) []string {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join("..", "migrate", "testdata", "schema.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	var columns []string
	for _, line := range strings.Split(string(contents), "\n") {
		kind, detail, found := strings.Cut(line, "\t")
		if !found || kind != "column" {
			continue
		}
		qualified, rest, found := strings.Cut(detail, " ")
		if !found || !strings.HasPrefix(qualified, "issues.") {
			continue
		}
		if !strings.Contains(rest, "null=NO") || strings.Contains(rest, "default=") {
			continue
		}
		columns = append(columns, strings.TrimPrefix(qualified, "issues."))
	}
	return columns
}
