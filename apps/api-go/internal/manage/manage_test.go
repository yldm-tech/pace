package manage

import (
	"strings"
	"testing"
)

// Every manage.py command has a subcommand here under the same name, so a runbook written against manage.py still reads true.
func TestEveryCommandKeepsItsName(t *testing.T) {
	want := []string{
		"activate_user", "clear_cache", "copy_issue_comment_to_description", "create_bucket",
		"create_dummy_data", "create_instance_admin", "create_project_member",
		"fix_duplicate_sequences", "reactivate_workspace_member", "reset_password",
		"sync_issue_description_version", "sync_issue_version", "test_email",
		"update_bucket", "update_deleted_workspace_slug", "wait_for_db", "wait_for_migrations",
	}
	commands := Registry()
	for _, name := range want {
		if _, known := commands[name]; !known {
			t.Errorf("%s has no subcommand", name)
		}
	}
	if len(commands) != len(want) {
		t.Errorf("there are %d subcommands rather than %d: %v", len(commands), len(want), Names())
	}
	for name, command := range commands {
		if command.Help == "" || command.Usage == "" || command.Run == nil {
			t.Errorf("%s is missing its help, usage or body", name)
		}
	}
}

// The arguments are read the way argparse reads them: a flag written apart from its value, or joined to it with an equals sign, and the positionals left over.
func TestTheArgumentsAreReadTheWayArgparseReadsThem(t *testing.T) {
	arguments := []string{"--project_id", "one", "--user_email=ada@example.test", "--role", "15"}
	if value, _ := flagValue(arguments, "project_id"); value != "one" {
		t.Errorf("the project is %q", value)
	}
	if value, _ := flagValue(arguments, "user_email"); value != "ada@example.test" {
		t.Errorf("the email is %q", value)
	}
	if got := projectMemberRole(arguments); got != 15 {
		t.Errorf("the role is %d", got)
	}
	// Without --role the default is twenty, which is admin — so the command makes an administrator unless it is told otherwise.
	if got := projectMemberRole(nil); got != 20 {
		t.Errorf("the default role is %d", got)
	}
	if got := positional([]string{"acme", "ada@example.test", "--dry-run"}); strings.Join(got, ",") != "acme,ada@example.test" {
		t.Errorf("the positionals are %v", got)
	}
	if !hasFlag([]string{"acme", "--dry-run"}, "dry-run") {
		t.Error("the dry run switch was not seen")
	}
}

// A slug that already ends in the epoch seconds is left alone, so running the command twice does not stack timestamps.
func TestASlugThatIsAlreadyStamped(t *testing.T) {
	if !alreadyStamped("acme__1758000000") {
		t.Error("a stamped slug reads as unstamped")
	}
	for _, slug := range []string{"acme", "acme__", "acme__beta", "acme-1758000000"} {
		if alreadyStamped(slug) {
			t.Errorf("%q reads as stamped", slug)
		}
	}
}

// The migrations the Django app ships are read out of the embedded list, which CI keeps in step with that app.
func TestTheShippedMigrations(t *testing.T) {
	names := parseShippedMigrations()
	if len(names) < 100 {
		t.Fatalf("only %d migrations are embedded", len(names))
	}
	seen := map[migrationName]bool{}
	for _, name := range names {
		if name.App == "" || name.Name == "" {
			t.Errorf("a migration is missing its app or its name: %#v", name)
		}
		if seen[name] {
			t.Errorf("%s.%s is listed twice", name.App, name.Name)
		}
		seen[name] = true
	}
	if !seen[migrationName{App: "db", Name: "0001_initial"}] {
		t.Error("the first migration is not in the list")
	}
}

// A role is reported by the label beside its number, which is what get_role_display gives.
func TestTheRoleNames(t *testing.T) {
	cases := map[int]string{20: "Admin", 15: "Member", 5: "Guest", 99: ""}
	for role, want := range cases {
		if got := workspaceRoleName(role); got != want {
			t.Errorf("role %d reads as %q rather than %q", role, got, want)
		}
	}
}
