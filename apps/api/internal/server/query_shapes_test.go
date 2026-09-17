package server

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestNoOrdinalGroupBy covers a difference between two GORM methods that look alike.
//
// Order passes its argument through as written, so Order("1") is the ordinal Postgres expects. Group quotes its argument as an identifier, so Group("1") arrives as GROUP BY "1" and the query dies with `column "1" does not exist`. Six queries were written with both on the same line, and every one of them answered 500: the activity graph, the dashboard, the completed-issues graph, the workspace default analytics and two project analytics.
//
// Grouping by the select alias is what replaced them -- Postgres allows an output name in GROUP BY, and it says what the query means rather than counting columns.
func TestNoOrdinalGroupBy(t *testing.T) {
	// No leading dot: a chained call often begins the line.
	ordinal := regexp.MustCompile(`\bGroup\("\d+"\)`)
	if offenders := walk(t, ordinal); len(offenders) > 0 {
		t.Errorf("Group quotes its argument, so an ordinal becomes a column name:\n  %s", strings.Join(offenders, "\n  "))
	}
}

// TestNoScanIntoAPointerSliceElement is the Scan half of the Pluck rule next door.
//
// Scan honours a pointer destination on its own -- *float64 takes a null aggregate fine -- but not as the element of a slice, where it falls back to the bare type. []*float64 therefore fails on exactly the null it was written to carry: SUM over an empty set.
func TestNoScanIntoAPointerSliceElement(t *testing.T) {
	// A Scan whose destination was declared []*T.
	scanPointer := regexp.MustCompile(`\.Scan\(&(\w+)\)`)
	declared := regexp.MustCompile(`var\s+(\w+)\s+\[\]\*\w+`)

	var offenders []string
	root := filepath.Join("..", "..")
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		names := map[string]bool{}
		for _, match := range declared.FindAllStringSubmatch(string(contents), -1) {
			names[match[1]] = true
		}
		if len(names) == 0 {
			return nil
		}
		for _, line := range strings.Split(string(contents), "\n") {
			if match := scanPointer.FindStringSubmatch(line); match != nil && names[match[1]] {
				offenders = append(offenders, path+": "+strings.TrimSpace(line))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(offenders) > 0 {
		t.Errorf("Scan does not honour a pointer as a slice element; use []sql.Null* :\n  %s", strings.Join(offenders, "\n  "))
	}
}

func walk(t *testing.T, pattern *regexp.Regexp) []string {
	t.Helper()
	var offenders []string
	root := filepath.Join("..", "..")
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, line := range strings.Split(string(contents), "\n") {
			if pattern.MatchString(line) {
				offenders = append(offenders, path+": "+strings.TrimSpace(line))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	return offenders
}
