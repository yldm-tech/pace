package project

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// intakeRelatedName is what Django calls the reverse accessor on both foreign keys into IntakeIssue:
//
//	intake = models.ForeignKey("db.Intake", related_name="issue_intake", ...)
//	issue  = models.ForeignKey("db.Issue",  related_name="issue_intake", ...)
//
// It is not a table. The table is db_table = "intake_issues", renamed from inbox_issues by migration 0085. Reading the related_name as a table name put `FROM issue_intake` into ten queries across the app, and every endpoint that touched one answered 500 with `relation "issue_intake" does not exist` -- the notifications list, global search, the intake lists and the project counts among them.
//
// The name is still right in three other places, which is why it cannot simply be spelled out of existence: it is the Django lookup path in a filter key, and it is the JSON key the serializer renders, because DRF names that field after the accessor too.
func TestNoQueryTreatsTheIntakeAccessorAsATable(t *testing.T) {
	// The shapes a table name appears in: a FROM or JOIN, GORM's Table(), and the TableName a model declares.
	asTable := regexp.MustCompile(`(?i)(FROM|JOIN)\s+issue_intake\b|Table\("issue_intake|TableName\(\)[^"]*"issue_intake"`)

	root := filepath.Join("..", "..")
	var offenders []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, line := range strings.Split(string(contents), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "//") {
				continue
			}
			if asTable.MatchString(line) {
				offenders = append(offenders, path+": "+strings.TrimSpace(line))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(offenders) > 0 {
		t.Errorf("issue_intake is a related_name, not a table; the table is intake_issues:\n  %s", strings.Join(offenders, "\n  "))
	}
}
