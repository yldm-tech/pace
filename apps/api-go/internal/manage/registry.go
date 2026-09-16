package manage

import (
	"context"
	"crypto/rand"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/yldm-tech/pace/apps/api-go/internal/worker"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Mail is how test_email reaches a mail server, and how the settings it uses are resolved. A binary that does not set it answers that mail is not configured.
var Mail func() (defaults worker.EmailSettings, reader worker.ConfigurationReader, mailer worker.Mailer)

// Queue is how the two backfill commands hand their work to the worker. A binary that does not set it answers that the queue is not configured.
var Queue func() worker.DelayedPublisher

// testEmail is manage.py test_email: one message to prove the mail settings work.
func testEmail(ctx context.Context, env Environment, arguments []string) error {
	values := positional(arguments)
	if len(values) == 0 || values[0] == "" {
		return commandError("Receiver email is required")
	}
	if Mail == nil {
		write(env, "Error: Email could not be delivered due to no mail settings being configured")
		return nil
	}
	defaults, reader, mailer := Mail()
	write(env, "Trying to send test email...")
	if err := worker.SendTestEmail(ctx, defaults, reader, mailer, values[0]); err != nil {
		write(env, "Error: Email could not be delivered due to %s", err)
		return nil
	}
	write(env, "Email successfully sent")
	return nil
}

// syncIssueVersion is manage.py sync_issue_version: start the backfill that gives every existing work item a version row.
//
// Both backfill commands ask for their two numbers on the terminal rather than taking them as arguments, which is upstream's shape. The batch size is handed over as the text it was typed as, and the countdown as a number — a difference that is upstream's too.
func syncIssueVersion(ctx context.Context, env Environment, _ []string) error {
	return startBackfill(ctx, env, worker.ScheduleIssueVersionTask, "Successfully created issue version task")
}

// syncIssueDescriptionVersion is manage.py sync_issue_description_version: the same backfill over the description columns.
func syncIssueDescriptionVersion(ctx context.Context, env Environment, _ []string) error {
	return startBackfill(ctx, env, worker.ScheduleIssueDescriptionVersionTask, "Successfully created issue description version task")
}

func startBackfill(ctx context.Context, env Environment, taskName, done string) error {
	batchSize, err := env.Prompt("Enter the batch size: ")
	if err != nil {
		return err
	}
	countdown, err := env.Prompt("Enter the batch countdown: ")
	if err != nil {
		return err
	}
	if Queue == nil {
		return commandError("No task queue is configured")
	}
	publisher := Queue()
	if publisher == nil {
		return commandError("No task queue is configured")
	}
	// The batch size goes over as the text it was typed as and the countdown as a number, which is what the command does with them.
	err = publisher.PublishAfter(ctx, taskName, map[string]any{
		"batch_size": batchSize, "countdown": asNumber(countdown),
	}, 0)
	if err != nil {
		return err
	}
	write(env, "%s", done)
	return nil
}

// asNumber is int(): a countdown that is not a number at all is an error the command does not survive, which here is a countdown of zero.
func asNumber(value string) int {
	number := 0
	for _, character := range value {
		if character < '0' || character > '9' {
			return 0
		}
		number = number*10 + int(character-'0')
	}
	return number
}

// createDummyData is manage.py create_dummy_data: a whole workspace of made-up work, asked for on the terminal one answer at a time.
//
// The workspace is made here and the projects are queued, one message per project. Everything that goes wrong is reported and swallowed, which is what the command's own try does — including a half-made workspace, since the workspace is written before the first project is asked about.
func createDummyData(ctx context.Context, env Environment, _ []string) error {
	db, err := database(env)
	if err != nil {
		return err
	}
	name, err := env.Prompt("Workspace Name: ")
	if err != nil {
		return err
	}
	slug, err := env.Prompt("Workspace slug: ")
	if err != nil {
		return err
	}
	if slug == "" {
		write(env, "Command errored out Workspace slug is required")
		return nil
	}
	var existing int64
	if err := db.WithContext(ctx).Table("workspaces").Where("slug = ? AND deleted_at IS NULL", slug).Count(&existing).Error; err != nil {
		return err
	}
	if existing > 0 {
		write(env, "Command errored out Workspace already exists")
		return nil
	}

	creator, err := env.Prompt("Your email: ")
	if err != nil {
		return err
	}
	var owners []string
	if creator != "" {
		if err := db.WithContext(ctx).Table("users").Where("email = ?", creator).Limit(1).Pluck("id", &owners).Error; err != nil {
			return err
		}
	}
	if len(owners) == 0 {
		write(env, "Command errored out User email is required and should have signed in plane")
		return nil
	}

	memberLine, err := env.Prompt("Enter Member emails (comma separated): ")
	if err != nil {
		return err
	}
	members := []string{}
	if memberLine != "" {
		// The line is split on commas and nothing is trimmed, so a space after a comma stays part of the address — which is what then fails to match anybody.
		members = strings.Split(memberLine, ",")
	}

	if err := createSeedWorkspace(ctx, db, name, slug, owners[0], members); err != nil {
		write(env, "Command errored out %s", err)
		return nil
	}

	projectCount, err := promptNumber(env, "Number of projects to be created: ")
	if err != nil {
		return err
	}
	if Queue == nil || Queue() == nil {
		write(env, "Command errored out No task queue is configured")
		return nil
	}
	publisher := Queue()
	for index := 0; index < projectCount; index++ {
		write(env, "Please provide the following details for project %d:", index+1)
		counts := map[string]any{"slug": slug, "email": creator, "members": members}
		// The five are asked in the order the command asks them in.
		for _, question := range []struct{ label, key string }{
			{"Number of issues to be created: ", "issue_count"},
			{"Number of cycles to be created: ", "cycle_count"},
			{"Number of modules to be created: ", "module_count"},
			{"Number of pages to be created: ", "pages_count"},
			{"Number of intake issues to be created: ", "intake_issue_count"},
		} {
			value, err := promptNumber(env, question.label)
			if err != nil {
				return err
			}
			counts[question.key] = value
		}
		if err := publisher.PublishAfter(ctx, worker.CreateDummyDataTask, counts, 0); err != nil {
			write(env, "Command errored out %s", err)
			return nil
		}
	}
	write(env, "Data is pushed to the queue")
	return nil
}

func promptNumber(env Environment, label string) (int, error) {
	value, err := env.Prompt(label)
	if err != nil {
		return 0, err
	}
	return asNumber(value), nil
}

// createSeedWorkspace writes the workspace and its first two memberships: the person who asked, and anybody they named who has already signed in.
func createSeedWorkspace(ctx context.Context, db *gorm.DB, name, slug, ownerID string, members []string) error {
	now := time.Now().UTC()
	workspaceID := uuid.NewString()
	err := db.WithContext(ctx).Table("workspaces").Create(map[string]any{
		"id": workspaceID, "created_at": now, "updated_at": now,
		"created_by_id": nil, "updated_by_id": nil,
		"name": name, "slug": slug, "owner_id": ownerID, "organization_size": nil,
		"timezone": "UTC", "background_color": randomHexColor(),
	}).Error
	if err != nil {
		return err
	}
	err = db.WithContext(ctx).Table("workspace_members").Create(map[string]any{
		"id": uuid.NewString(), "created_at": now, "updated_at": now,
		"created_by_id": nil, "updated_by_id": nil,
		"workspace_id": workspaceID, "member_id": ownerID, "role": 20, "is_active": true,
		"view_props": "{}", "default_props": "{}", "issue_props": "{}", "company_role": nil,
		"getting_started_checklist": "{}", "tips": "{}", "explored_features": "{}",
	}).Error
	if err != nil {
		return err
	}
	if len(members) == 0 {
		return nil
	}
	var memberIDs []string
	if err := db.WithContext(ctx).Table("users").Where("email IN ?", members).Pluck("id", &memberIDs).Error; err != nil {
		return err
	}
	rows := make([]map[string]any, 0, len(memberIDs))
	for _, memberID := range memberIDs {
		rows = append(rows, map[string]any{
			"id": uuid.NewString(), "created_at": now, "updated_at": now,
			"created_by_id": nil, "updated_by_id": nil,
			"workspace_id": workspaceID, "member_id": memberID, "role": 20, "is_active": true,
			"view_props": "{}", "default_props": "{}", "issue_props": "{}", "company_role": nil,
			"getting_started_checklist": "{}", "tips": "{}", "explored_features": "{}",
		})
	}
	if len(rows) == 0 {
		return nil
	}
	// ignore_conflicts, so the person who asked is not written twice when they name themselves.
	return db.WithContext(ctx).Table("workspace_members").
		Clauses(clause.OnConflict{DoNothing: true}).Create(rows).Error
}

func allCommands() []Command {
	return []Command{
		{Name: "activate_user", Help: "Make the user with the given email active", Usage: "activate_user <email>", Run: activateUser},
		{Name: "clear_cache", Help: "Clear Cache before starting the server to remove stale values", Usage: "clear_cache [--key KEY]", Run: clearCache},
		{Name: "copy_issue_comment_to_description", Help: "Create Description records for existing IssueComment", Usage: "copy_issue_comment_to_description", Run: copyIssueCommentToDescription},
		{Name: "configure_instance", Help: "Configure instance variables", Usage: "configure_instance", Run: configureInstance},
		{Name: "create_bucket", Help: "Create the default bucket for the instance", Usage: "create_bucket", Run: createBucket},
		{Name: "create_dummy_data", Help: "Create dump issues, cycles etc. for a project in a given workspace", Usage: "create_dummy_data", Run: createDummyData},
		{Name: "create_instance_admin", Help: "Add a new instance admin", Usage: "create_instance_admin <admin_email>", Run: createInstanceAdmin},
		{Name: "create_project_member", Help: "Add a member to a project. If present in the workspace", Usage: "create_project_member --project_id ID --user_email EMAIL [--role ROLE]", Run: createProjectMember},
		{Name: "fix_duplicate_sequences", Help: "Fix duplicate sequences", Usage: "fix_duplicate_sequences <issue_identifier>", Run: fixDuplicateSequences},
		{Name: "register_instance", Help: "Check if instance is registered else register", Usage: "register_instance <machine_signature>", Run: registerInstance},
		{Name: "reactivate_workspace_member", Help: "Reactivate a workspace member given a workspace slug and user email", Usage: "reactivate_workspace_member <slug> <email>", Run: reactivateWorkspaceMember},
		{Name: "reset_password", Help: "Reset password of the user with the given email", Usage: "reset_password <email>", Run: resetPassword},
		{Name: "sync_issue_description_version", Help: "Creates IssueDescriptionVersion records for existing Issues in batches", Usage: "sync_issue_description_version", Run: syncIssueDescriptionVersion},
		{Name: "sync_issue_version", Help: "Creates IssueVersion records for existing Issues in batches", Usage: "sync_issue_version", Run: syncIssueVersion},
		{Name: "test_email", Help: "Send a test email to prove the mail settings work", Usage: "test_email <to_email>", Run: testEmail},
		{Name: "update_bucket", Help: "Keep the objects already in the bucket readable", Usage: "update_bucket", Run: updateBucket},
		{Name: "update_deleted_workspace_slug", Help: "Updates the slug of a soft-deleted workspace by appending the epoch timestamp", Usage: "update_deleted_workspace_slug <slug> [--dry-run]", Run: updateDeletedWorkspaceSlug},
		{Name: "wait_for_db", Help: "Pause execution until the database is available", Usage: "wait_for_db", Run: waitForDB},
		{Name: "wait_for_migrations", Help: "Wait for database migrations to complete before starting the worker or beat", Usage: "wait_for_migrations", Run: waitForMigrations},
	}
}

// pollInterval is how often the two waiting commands ask again. It is a variable so a test does not have to wait out a real second.
var pollInterval = struct {
	database   time.Duration
	migrations time.Duration
}{database: time.Second, migrations: 10 * time.Second}

// randomHexColor is get_random_color, the column default a workspace gets when nobody picks one.
func randomHexColor() string {
	value := make([]byte, 3)
	if _, err := rand.Read(value); err != nil {
		return "#3f76ff"
	}
	return fmt.Sprintf("#%x", value)
}
