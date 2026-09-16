package server

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestNoPluckIntoAPointerSlice covers a trap in this version of GORM.
//
// Pluck does not honour a pointer element type: given *[]*string it still scans each row into a string, so a null column ends the request with
//
//	sql: Scan error on column index 0, name "avatar_asset_id": converting NULL to string is unsupported
//
// and a 500. []*string reads as the careful choice, which is what makes it dangerous -- it is written exactly where the author knew the column was nullable. Four of them had shipped: the first avatar anybody sets, the first workspace logo, the first project cover, and a project with no estimate.
//
// []sql.NullString works, and so does Row().Scan into a sql.Null*. Verified against a live database rather than assumed.
func TestNoPluckIntoAPointerSlice(t *testing.T) {
	// A Pluck whose destination is a variable declared as []*T, in either declaration form.
	pointerSlice := regexp.MustCompile(`(?m)(var\s+(\w+)\s+\[\]\*\w+|(\w+)\s*:=\s*\[\]\*\w+\{)`)
	pluck := regexp.MustCompile(`\.Pluck\([^,]+,\s*&(\w+)\)`)

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
		declared := map[string]bool{}
		for _, match := range pointerSlice.FindAllStringSubmatch(string(contents), -1) {
			for _, name := range match[2:] {
				if name != "" {
					declared[name] = true
				}
			}
		}
		if len(declared) == 0 {
			return nil
		}
		for _, line := range strings.Split(string(contents), "\n") {
			match := pluck.FindStringSubmatch(line)
			if match != nil && declared[match[1]] {
				offenders = append(offenders, path+": "+strings.TrimSpace(line))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(offenders) > 0 {
		t.Errorf("Pluck scans into the element type itself, so []*T cannot carry a NULL; use []sql.NullString or Row().Scan:\n  %s", strings.Join(offenders, "\n  "))
	}
}
