package server

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// profileOnlyColumns are fields Django keeps on Profile rather than on User. They live in the profiles table, keyed by user_id, and asking the users table for one ends the request with `column ... does not exist`.
//
// The list is deliberately short: only the ones a handler is plausibly tempted to read straight off the user, taken from the columns profiles has and users does not.
var profileOnlyColumns = []string{
	"last_workspace_id",
	"onboarding_step",
	"is_onboarded",
	"is_tour_completed",
	"use_case",
	"role",
	"theme",
	"billing_address",
	"has_billing_address",
	"company_name",
}

// TestNoProfileColumnIsReadOffTheUsersTable caught /api/users/last-visited-workspace/, which answered 500 on every request because it plucked last_workspace_id from users.
func TestNoProfileColumnIsReadOffTheUsersTable(t *testing.T) {
	usersTable := regexp.MustCompile(`Table\("users"\)`)

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
		lines := strings.Split(string(contents), "\n")
		for index, line := range lines {
			if !usersTable.MatchString(line) {
				continue
			}
			// A GORM chain runs over several lines; the column usually appears within the next few.
			window := strings.Join(lines[index:min(index+5, len(lines))], "\n")
			for _, column := range profileOnlyColumns {
				if strings.Contains(window, `"`+column+`"`) {
					offenders = append(offenders, path+":"+column)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(offenders) > 0 {
		t.Errorf("these columns are on profiles, keyed by user_id, not on users:\n  %s", strings.Join(offenders, "\n  "))
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
