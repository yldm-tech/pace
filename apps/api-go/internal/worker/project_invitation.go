package worker

import (
	"context"
	"log/slog"
	"time"

	"gorm.io/gorm"
)

// ProjectInvitationTask emails somebody an invitation to a project.
//
// Nothing in this edition queues it. Project invitations go out through project_add_user_email_task instead, and this one is left over — it is handled so a message carrying its name does not sit unconsumed, and so an installation with an older release still in flight is not left with a dead letter.
const ProjectInvitationTask = "plane.bgtasks.project_invitation_task.project_invitation"

// ProjectInvitationTasks sends that email.
type ProjectInvitationTasks struct {
	db       *gorm.DB
	settings EmailSettings
	config   ConfigurationReader
	mailer   Mailer
	logger   *slog.Logger
	clock    func() time.Time
}

func NewProjectInvitationTasks(db *gorm.DB, settings EmailSettings, config ConfigurationReader, mailer Mailer, logger *slog.Logger) *ProjectInvitationTasks {
	return &ProjectInvitationTasks{db: db, settings: settings, config: config, mailer: mailer, logger: logger, clock: time.Now}
}

func (tasks *ProjectInvitationTasks) Register(consumer *Consumer) {
	consumer.Register(ProjectInvitationTask, tasks.projectInvitation)
}

// projectInvitation reproduces project_invitation.
//
// It writes the plain text of the email onto the invitation before sending it, which is the one side effect anybody would miss: the row carries a copy of what was sent. A project or an invitation that is not there ends the task quietly; anything else is logged and swallowed.
func (tasks *ProjectInvitationTasks) projectInvitation(ctx context.Context, arguments []any, keywords map[string]any) error {
	email := stringArgument(arguments, keywords, 0, "email")
	projectID := stringArgument(arguments, keywords, 1, "project_id")
	token := stringArgument(arguments, keywords, 2, "token")
	currentSite := stringArgument(arguments, keywords, 3, "current_site")
	invitor := stringArgument(arguments, keywords, 4, "invitor")

	var actor struct {
		FirstName   string `gorm:"column:first_name"`
		DisplayName string `gorm:"column:display_name"`
		Email       string `gorm:"column:email"`
	}
	err := tasks.db.WithContext(ctx).Table("users").Where("email = ?", invitor).
		Select("first_name, display_name, email").Take(&actor).Error
	if err != nil {
		// The invitor's own lookup is not guarded upstream, so it is an error rather than a quiet return.
		tasks.logger.Warn("a project invitation named an invitor who is not there", "invitor", invitor, "error", err)
		return nil
	}

	var project struct {
		Name string `gorm:"column:name"`
		Slug string `gorm:"column:slug"`
	}
	err = tasks.db.WithContext(ctx).Table("projects p").
		Joins("JOIN workspaces w ON w.id = p.workspace_id").
		Where("p.id = ?", projectID).Select("p.name, w.slug").Take(&project).Error
	if err != nil {
		// A project that is not there ends the task without a word, which is what the DoesNotExist branch does.
		return nil
	}

	var invite struct {
		ID string `gorm:"column:id"`
	}
	err = tasks.db.WithContext(ctx).Table("project_member_invites").
		Where("token = ? AND email = ? AND deleted_at IS NULL", token, email).
		Select("id").Take(&invite).Error
	if err != nil {
		return nil
	}

	link := currentSite + "/project-invitations/?invitation_id=" + invite.ID +
		"&email=" + email + "&slug=" + project.Slug + "&project_id=" + projectID
	html, err := renderEmail("emails/invitations/project_invitation.html", map[string]any{
		"email": email, "first_name": actor.FirstName, "project_name": project.Name,
		"invitation_url": link, "current_site": currentSite,
	})
	if err != nil {
		tasks.logger.Warn("a project invitation could not be rendered", "error", err)
		return nil
	}
	text := plainTextFromHTML(html)

	// The invitation keeps a copy of what was sent, and it is written before the send rather than after — so a message that never goes out still leaves the copy behind.
	err = tasks.db.WithContext(ctx).Table("project_member_invites").Where("id = ?", invite.ID).
		Updates(map[string]any{"message": text, "updated_at": tasks.clock().UTC()}).Error
	if err != nil {
		tasks.logger.Warn("a project invitation could not be recorded", "error", err)
		return nil
	}

	settings, err := emailSettings(ctx, tasks.config, tasks.settings)
	if err != nil {
		tasks.logger.Warn("a project invitation could not read the mail settings", "error", err)
		return nil
	}
	subject := invitationSubject(actor.FirstName, actor.DisplayName, actor.Email, project.Name)
	if err := tasks.mailer.Send(ctx, settings, email, subject, text, html); err != nil {
		tasks.logger.Warn("a project invitation could not be sent", "email", email, "error", err)
	}
	return nil
}

// invitationSubject names the invitor by whichever of their three names is set first, which is what the f-string's or-chain picks.
func invitationSubject(firstName, displayName, email, projectName string) string {
	name := firstName
	if name == "" {
		name = displayName
	}
	if name == "" {
		name = email
	}
	return name + " invited you to join " + projectName + " on Plane"
}
