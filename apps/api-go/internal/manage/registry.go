package manage

import (
	"context"
	"time"

	"github.com/yldm-tech/pace/apps/api-go/internal/worker"
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

// createDummyData is manage.py create_dummy_data, which is not here yet.
//
// It queues plane.bgtasks.dummy_data_task, and that task has not been ported. Rather than half-run — making the workspace and then queueing something nothing will consume — it says so and stops.
func createDummyData(_ context.Context, _ Environment, _ []string) error {
	return commandError("create_dummy_data is not available yet: it queues plane.bgtasks.dummy_data_task, which has not been ported.")
}

func allCommands() []Command {
	return []Command{
		{Name: "activate_user", Help: "Make the user with the given email active", Usage: "activate_user <email>", Run: activateUser},
		{Name: "clear_cache", Help: "Clear Cache before starting the server to remove stale values", Usage: "clear_cache [--key KEY]", Run: clearCache},
		{Name: "copy_issue_comment_to_description", Help: "Create Description records for existing IssueComment", Usage: "copy_issue_comment_to_description", Run: copyIssueCommentToDescription},
		{Name: "create_bucket", Help: "Create the default bucket for the instance", Usage: "create_bucket", Run: createBucket},
		{Name: "create_dummy_data", Help: "Create dump issues, cycles etc. for a project in a given workspace", Usage: "create_dummy_data", Run: createDummyData},
		{Name: "create_instance_admin", Help: "Add a new instance admin", Usage: "create_instance_admin <admin_email>", Run: createInstanceAdmin},
		{Name: "create_project_member", Help: "Add a member to a project. If present in the workspace", Usage: "create_project_member --project_id ID --user_email EMAIL [--role ROLE]", Run: createProjectMember},
		{Name: "fix_duplicate_sequences", Help: "Fix duplicate sequences", Usage: "fix_duplicate_sequences <issue_identifier>", Run: fixDuplicateSequences},
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
