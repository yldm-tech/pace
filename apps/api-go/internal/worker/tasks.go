package worker

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log/slog"

	"gorm.io/gorm"
)

// Task names as Celery sees them, matching plane.bgtasks.
const (
	MagicLinkTask               = "plane.bgtasks.magic_link_code_task.magic_link"
	ForgotPasswordTask          = "plane.bgtasks.forgot_password_task.forgot_password"
	UserActivationTask          = "plane.bgtasks.user_activation_email_task.user_activation_email"
	UserDeactivationTask        = "plane.bgtasks.user_deactivation_email_task.user_deactivation_email"
	EmailUpdateCodeTask         = "plane.bgtasks.user_email_update_task.send_email_update_magic_code"
	EmailUpdateConfirmationTask = "plane.bgtasks.user_email_update_task.send_email_update_confirmation"
	WorkspaceInvitationTask     = "plane.bgtasks.workspace_invitation_task.workspace_invitation"
	ProjectAddUserEmailTask     = "plane.bgtasks.project_add_user_email_task.project_add_user_email"
)

// EmailTasks owns the tasks that render a template and send it.
type EmailTasks struct {
	db       *gorm.DB
	settings EmailSettings
	config   ConfigurationReader
	mailer   Mailer
	logger   *slog.Logger
}

func NewEmailTasks(db *gorm.DB, defaults EmailSettings, config ConfigurationReader, mailer Mailer, logger *slog.Logger) *EmailTasks {
	if mailer == nil {
		mailer = SMTPMailer{}
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &EmailTasks{db: db, settings: defaults, config: config, mailer: mailer, logger: logger}
}

// Register wires every migrated email task onto the consumer.
func (tasks *EmailTasks) Register(consumer *Consumer) {
	consumer.Register(MagicLinkTask, tasks.magicLink)
	consumer.Register(ForgotPasswordTask, tasks.forgotPassword)
	consumer.Register(UserActivationTask, tasks.userActivation)
	consumer.Register(UserDeactivationTask, tasks.userDeactivation)
	consumer.Register(EmailUpdateCodeTask, tasks.emailUpdateCode)
	consumer.Register(EmailUpdateConfirmationTask, tasks.emailUpdateConfirmation)
	consumer.Register(WorkspaceInvitationTask, tasks.workspaceInvitation)
	consumer.Register(ProjectAddUserEmailTask, tasks.projectAddUser)
	consumer.Register(WebhookDeactivationTask, tasks.webhookDeactivation)
}

func (tasks *EmailTasks) magicLink(ctx context.Context, arguments []any, keywords map[string]any) error {
	email := stringArgument(arguments, keywords, 0, "email")
	token := stringArgument(arguments, keywords, 2, "token")
	return tasks.send(ctx, email,
		fmt.Sprintf("Your unique Plane login code is %s", token),
		"emails/auth/magic_signin.html",
		map[string]any{"code": token, "email": email})
}

func (tasks *EmailTasks) forgotPassword(ctx context.Context, arguments []any, keywords map[string]any) error {
	firstName := stringArgument(arguments, keywords, 0, "first_name")
	email := stringArgument(arguments, keywords, 1, "email")
	uidb64 := stringArgument(arguments, keywords, 2, "uidb64")
	token := stringArgument(arguments, keywords, 3, "token")
	currentSite := stringArgument(arguments, keywords, 4, "current_site")
	absoluteURL := currentSite + "/accounts/reset-password/?uidb64=" + uidb64 + "&token=" + token + "&email=" + email
	return tasks.send(ctx, email,
		"A new password to your Plane account has been requested",
		"emails/auth/forgot_password.html",
		map[string]any{"first_name": firstName, "forgot_password_url": absoluteURL, "email": email})
}

func (tasks *EmailTasks) userActivation(ctx context.Context, arguments []any, keywords map[string]any) error {
	currentSite := stringArgument(arguments, keywords, 0, "current_site")
	userID := stringArgument(arguments, keywords, 1, "user_id")
	user, found, err := tasks.user(ctx, userID)
	if err != nil || !found {
		return err
	}
	return tasks.send(ctx, user.Email,
		fmt.Sprintf("%s has been activated on Plane", user.label()),
		"emails/user/user_activation.html",
		map[string]any{"email": user.Email, "profile_url": currentSite + "/profile"})
}

func (tasks *EmailTasks) userDeactivation(ctx context.Context, arguments []any, keywords map[string]any) error {
	currentSite := stringArgument(arguments, keywords, 0, "current_site")
	userID := stringArgument(arguments, keywords, 1, "user_id")
	user, found, err := tasks.user(ctx, userID)
	if err != nil || !found {
		return err
	}
	return tasks.send(ctx, user.Email,
		fmt.Sprintf("%s has been deactivated on Plane", user.label()),
		"emails/user/user_deactivation.html",
		map[string]any{"email": user.Email, "login_url": currentSite + "/login"})
}

func (tasks *EmailTasks) emailUpdateCode(ctx context.Context, arguments []any, keywords map[string]any) error {
	email := stringArgument(arguments, keywords, 0, "email")
	token := stringArgument(arguments, keywords, 1, "token")
	// Django reuses the magic sign-in template here with a different subject.
	return tasks.send(ctx, email,
		"Verify your new email address",
		"emails/auth/magic_signin.html",
		map[string]any{"code": token, "email": email})
}

func (tasks *EmailTasks) emailUpdateConfirmation(ctx context.Context, arguments []any, keywords map[string]any) error {
	email := stringArgument(arguments, keywords, 0, "email")
	return tasks.send(ctx, email,
		"Plane email address successfully updated",
		"emails/user/email_updated.html",
		map[string]any{"email": email})
}

func (tasks *EmailTasks) workspaceInvitation(ctx context.Context, arguments []any, keywords map[string]any) error {
	email := stringArgument(arguments, keywords, 0, "email")
	workspaceID := stringArgument(arguments, keywords, 1, "workspace_id")
	token := stringArgument(arguments, keywords, 2, "token")
	currentSite := stringArgument(arguments, keywords, 3, "current_site")
	inviter := stringArgument(arguments, keywords, 4, "inviter")

	invitor, found, err := tasks.userByEmail(ctx, inviter)
	if err != nil || !found {
		return err
	}
	var workspace struct {
		Name string `gorm:"column:name"`
		Slug string `gorm:"column:slug"`
	}
	err = tasks.db.WithContext(ctx).Table("workspaces").Where("id = ?", workspaceID).Take(&workspace).Error
	if err != nil {
		// Django swallows Workspace.DoesNotExist and returns.
		return nil
	}
	var invite struct {
		ID string `gorm:"column:id"`
	}
	err = tasks.db.WithContext(ctx).Table("workspace_member_invites").
		Where("token = ? AND email = ?", token, email).Take(&invite).Error
	if err != nil {
		return nil
	}
	absoluteURL := currentSite + "/workspace-invitations/?invitation_id=" + invite.ID + "&slug=" + workspace.Slug + "&token=" + token
	context := map[string]any{
		"email": email, "first_name": invitor.label(),
		"workspace_name": workspace.Name, "abs_url": absoluteURL,
	}
	body, text, err := tasks.render("emails/invitations/workspace_invitation.html", context)
	if err != nil {
		return err
	}
	// Django stores the plain text on the invite before sending.
	err = tasks.db.WithContext(ctx).Table("workspace_member_invites").
		Where("id = ?", invite.ID).Update("message", text).Error
	if err != nil {
		return err
	}
	settings, err := emailSettings(ctx, tasks.config, tasks.settings)
	if err != nil {
		return err
	}
	subject := fmt.Sprintf("%s has invited you to join them in %s on Plane", invitor.label(), workspace.Name)
	return tasks.mailer.Send(ctx, settings, email, subject, text, body)
}

func (tasks *EmailTasks) projectAddUser(ctx context.Context, arguments []any, keywords map[string]any) error {
	currentSite := stringArgument(arguments, keywords, 0, "current_site")
	projectMemberID := stringArgument(arguments, keywords, 1, "project_member_id")
	invitorID := stringArgument(arguments, keywords, 2, "invitor_id")

	invitor, found, err := tasks.user(ctx, invitorID)
	if err != nil || !found {
		return err
	}
	var row struct {
		ProjectID     string `gorm:"column:project_id"`
		ProjectName   string `gorm:"column:project_name"`
		WorkspaceName string `gorm:"column:workspace_name"`
		WorkspaceSlug string `gorm:"column:workspace_slug"`
		MemberEmail   string `gorm:"column:member_email"`
	}
	err = tasks.db.WithContext(ctx).Table("project_members pm").
		Joins("JOIN projects p ON p.id = pm.project_id").
		Joins("JOIN workspaces w ON w.id = pm.workspace_id").
		Joins("JOIN users u ON u.id = pm.member_id").
		Where("pm.id = ?", projectMemberID).
		Select("pm.project_id, p.name AS project_name, w.name AS workspace_name, w.slug AS workspace_slug, u.email AS member_email").
		Take(&row).Error
	if err != nil {
		return nil
	}
	projectURL := fmt.Sprintf("%s/%s/projects/%s/issues", currentSite, row.WorkspaceSlug, row.ProjectID)
	return tasks.send(ctx, row.MemberEmail,
		"You have been invited to a Plane project",
		"emails/notifications/project_addition.html",
		map[string]any{
			"project_name": row.ProjectName, "workspace_name": row.WorkspaceName,
			"email": row.MemberEmail, "inviter_first_name": invitor.FirstName,
			"project_url": projectURL,
		})
}

func (tasks *EmailTasks) render(templateName string, context map[string]any) (string, string, error) {
	body, err := renderEmail(templateName, context)
	if err != nil {
		return "", "", err
	}
	return body, plainTextFromHTML(body), nil
}

func (tasks *EmailTasks) send(ctx context.Context, to, subject, templateName string, context map[string]any) error {
	if to == "" {
		return fmt.Errorf("task has no recipient")
	}
	body, text, err := tasks.render(templateName, context)
	if err != nil {
		return err
	}
	settings, err := emailSettings(ctx, tasks.config, tasks.settings)
	if err != nil {
		return err
	}
	return tasks.mailer.Send(ctx, settings, to, subject, text, body)
}

type taskUser struct {
	ID          string `gorm:"column:id"`
	Email       string `gorm:"column:email"`
	FirstName   string `gorm:"column:first_name"`
	DisplayName string `gorm:"column:display_name"`
}

// label is Django's `first_name or display_name or email` fallback.
func (user taskUser) label() string {
	if user.FirstName != "" {
		return user.FirstName
	}
	if user.DisplayName != "" {
		return user.DisplayName
	}
	return user.Email
}

func (tasks *EmailTasks) user(ctx context.Context, userID string) (taskUser, bool, error) {
	var user taskUser
	err := tasks.db.WithContext(ctx).Table("users").Where("id = ?", userID).Take(&user).Error
	if err != nil {
		// Django logs and returns when the user is gone.
		return taskUser{}, false, nil
	}
	return user, true, nil
}

func (tasks *EmailTasks) userByEmail(ctx context.Context, email string) (taskUser, bool, error) {
	var user taskUser
	err := tasks.db.WithContext(ctx).Table("users").Where("email = ?", email).Take(&user).Error
	if err != nil {
		return taskUser{}, false, nil
	}
	return user, true, nil
}

func base64Encode(value string) string {
	return base64.StdEncoding.EncodeToString([]byte(value))
}

// MigratedTaskNames lists the Celery tasks the Go worker implements. The API
// routes exactly these to the Go queue, so the two lists cannot drift.
func MigratedTaskNames() []string {
	return []string{
		MagicLinkTask, ForgotPasswordTask, UserActivationTask, UserDeactivationTask,
		EmailUpdateCodeTask, EmailUpdateConfirmationTask, WorkspaceInvitationTask,
		ProjectAddUserEmailTask, WebhookDeactivationTask,
		DeleteAPILogsTask, DeleteEmailNotificationLogsTask, DeletePageVersionsTask,
		DeleteIssueDescriptionVersionsTask, DeleteWebhookLogsTask, RecentVisitedTask,
		SoftDeleteRelatedObjectsTask, HardDeleteTask,
		PageTransactionTask, TrackPageVersionTask, IssueDescriptionVersionTask,
		AssetObjectMetadataTask, DeleteUnuploadedFileAssetTask, CrawlLinkTitleTask,
		DeleteOldExportLinksTask, ArchiveAndCloseTask, ModelActivityTask, IssueActivityTask, NotificationsTask, WebhookSendTask,
	}
}

// newTaskUUID mints a v4 identifier for rows the worker inserts.
func newTaskUUID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", value[0:4], value[4:6], value[6:8], value[8:10], value[10:16]), nil
}

// WebhookDeactivationTask is send_webhook_deactivation_email, queued when a
// webhook has failed often enough to be switched off.
const WebhookDeactivationTask = "plane.bgtasks.webhook_task.send_webhook_deactivation_email"

func (tasks *EmailTasks) webhookDeactivation(ctx context.Context, arguments []any, keywords map[string]any) error {
	webhookID := stringArgument(arguments, keywords, 0, "webhook_id")
	receiverID := stringArgument(arguments, keywords, 1, "receiver_id")
	currentSite := stringArgument(arguments, keywords, 2, "current_site")

	receiver, found, err := tasks.user(ctx, receiverID)
	if err != nil || !found {
		return err
	}
	var webhook struct {
		ID            string `gorm:"column:id"`
		URL           string `gorm:"column:url"`
		WorkspaceSlug string `gorm:"column:workspace_slug"`
	}
	err = tasks.db.WithContext(ctx).Table("webhooks w").
		Joins("JOIN workspaces ws ON ws.id = w.workspace_id").
		Where("w.id = ?", webhookID).
		Select("w.id, w.url, ws.slug AS workspace_slug").Take(&webhook).Error
	if err != nil {
		// Django lets the lookup failure fall into its own except and returns.
		return nil
	}
	return tasks.send(ctx, receiver.Email,
		"Webhook Deactivated",
		"emails/notifications/webhook-deactivate.html",
		map[string]any{
			"email":   receiver.Email,
			"message": fmt.Sprintf("Webhook %s has been deactivated due to failed requests.", webhook.URL),
			"webhook_url": fmt.Sprintf("%s/%s/settings/webhooks/%s",
				currentSite, webhook.WorkspaceSlug, webhook.ID),
		})
}
