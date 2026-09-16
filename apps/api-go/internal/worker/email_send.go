package worker

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"time"

	redis "github.com/redis/go-redis/v9"
	"github.com/yldm-tech/pace/apps/api-go/internal/djangotemplate"
	"golang.org/x/net/html"
	"gorm.io/gorm"
)

// SendEmailNotificationTask renders and sends one person's batched email about one work item.
const SendEmailNotificationTask = "plane.bgtasks.email_notification_task.send_email_notification"

// emailLockTTL is how long the lock an email takes is held for, which is also how long two emails about the same batch are kept from both going out.
const emailLockTTL = 300 * time.Second

//go:embed templates/emails/notifications/issue-updates.html
var issueUpdatesTemplate string

// EmailSendTasks renders and sends the notification emails.
type EmailSendTasks struct {
	db       *gorm.DB
	redis    redis.UniversalClient
	config   ConfigurationReader
	settings EmailSettings
	mailer   Mailer
	template *djangotemplate.Template
	logger   *slog.Logger
	clock    func() time.Time
}

func NewEmailSendTasks(db *gorm.DB, client redis.UniversalClient, settings EmailSettings, config ConfigurationReader, mailer Mailer, logger *slog.Logger) (*EmailSendTasks, error) {
	parsed, err := djangotemplate.Parse(issueUpdatesTemplate)
	if err != nil {
		return nil, fmt.Errorf("parse the notification email template: %w", err)
	}
	return &EmailSendTasks{
		db: db, redis: client, config: config, settings: settings, mailer: mailer,
		template: parsed, logger: logger, clock: time.Now,
	}, nil
}

func (tasks *EmailSendTasks) Register(consumer *Consumer) {
	consumer.Register(SendEmailNotificationTask, tasks.sendEmailNotification)
}

// sendEmailNotification reproduces send_email_notification.
//
// **It sends nothing unless somebody has looked at the work item recently.** The links in the email are built from an origin the activity task parks in Redis for ten minutes, and with no origin there is nothing to link to, so the task returns. A change made by a client that sent no origin, or one whose email is queued more than ten minutes later, is silently not emailed. Reproduced rather than corrected.
//
// **A lock keeps a batch from being sent twice**, and it is named after every notification id in the batch — so two emails to the same person in one sweep take the same lock and only the first goes out.
func (tasks *EmailSendTasks) sendEmailNotification(ctx context.Context, arguments []any, keywords map[string]any) error {
	issueID := stringArgument(arguments, keywords, 0, "issue_id")
	notificationData := rawArgument(arguments, keywords, 1, "notification_data")
	receiverID := stringArgument(arguments, keywords, 2, "receiver_id")
	notificationIDs := stringListArgument(arguments, keywords, 3, "email_notification_ids")

	lockID := lockNameFor(issueID, receiverID, notificationIDs)

	taken, err := tasks.acquire(ctx, lockID)
	if err != nil {
		return err
	}
	if !taken {
		tasks.logger.Info("duplicate email received, skipping", "issue", issueID, "receiver", receiverID)
		return nil
	}
	// Every path out of here releases the lock, which is what upstream's except arms do.
	defer tasks.release(ctx, lockID)

	baseAPI, err := tasks.origin(ctx, issueID)
	if err != nil {
		return err
	}
	if baseAPI == "" {
		return nil
	}

	context, receiverEmail, err := tasks.emailContext(ctx, issueID, receiverID, baseAPI, notificationData)
	if err != nil {
		return err
	}
	if context == nil {
		return nil
	}

	body, err := tasks.template.Render(context)
	if err != nil {
		return err
	}
	settings, err := emailSettings(ctx, tasks.config, tasks.settings)
	if err != nil {
		return err
	}
	subject, _ := context["__subject"].(string)
	delete(context, "__subject")
	err = tasks.mailer.Send(ctx, settings, receiverEmail, subject, plainTextFromHTML(body), body)
	if err != nil {
		// Django logs the failure and still releases the lock, so the batch can be tried again by the next sweep — except that it has already been marked processed, so it will not be.
		tasks.logger.Error("the notification email could not be sent", "issue", issueID, "receiver", receiverID, "error", err)
		return nil
	}
	now := tasks.clock().UTC()
	return tasks.db.WithContext(ctx).Table("email_notification_logs").
		Where("id IN ?", notificationIDs).Update("sent_at", now).Error
}

// emailContext builds what the template is rendered against, and answers with nothing when the work item or the person is gone.
func (tasks *EmailSendTasks) emailContext(ctx context.Context, issueID, receiverID, baseAPI string, notificationData json.RawMessage) (map[string]any, string, error) {
	receiver, found, err := tasks.emailUser(ctx, receiverID)
	if err != nil || !found {
		return nil, "", err
	}
	issue, found, err := tasks.emailIssue(ctx, issueID)
	if err != nil || !found {
		return nil, "", err
	}

	grouped, err := tasks.createPayload(ctx, notificationData)
	if err != nil {
		return nil, "", err
	}

	actors := make([]string, 0, len(grouped))
	for actor := range grouped {
		actors = append(actors, actor)
	}
	// Python walks a dict, whose order is the order the sweep built it in. A map has none, so the order is fixed here.
	sort.Strings(actors)

	templateData := []any{}
	comments := []any{}
	for _, actorID := range actors {
		actor, found, err := tasks.emailUser(ctx, actorID)
		if err != nil {
			return nil, "", err
		}
		if !found {
			return nil, "", nil
		}
		detail := map[string]any{
			// The origin and the avatar are simply glued together, so somebody whose avatar is a full url elsewhere ends up with a broken one in the email. Reproduced rather than corrected.
			"avatar_url": baseAPI + actor.AvatarURL,
			"first_name": actor.FirstName,
			"last_name":  actor.LastName,
		}
		changes := grouped[actorID]

		if comment, present := changes["comment"]; present {
			delete(changes, "comment")
			comments = append(comments, map[string]any{"actor_comments": comment, "actor_detail": detail})
		}
		if mention, present := changes["mention"]; present {
			delete(changes, "mention")
			values, _ := mention.(map[string]any)
			if values != nil {
				values["new_value"], err = tasks.replaceMentions(ctx, values["new_value"])
				if err != nil {
					return nil, "", err
				}
				values["old_value"], err = tasks.replaceMentions(ctx, values["old_value"])
				if err != nil {
					return nil, "", err
				}
			}
			comments = append(comments, map[string]any{"actor_comments": mention, "actor_detail": detail})
		}

		activityTime, _ := changes["activity_time"].(string)
		delete(changes, "activity_time")
		if len(changes) == 0 {
			continue
		}
		templateData = append(templateData, map[string]any{
			"actor_detail": detail, "changes": changes,
			"issue_details": map[string]any{
				"name":       issue.Name,
				"identifier": fmt.Sprintf("%s-%d", issue.Identifier, issue.SequenceID),
			},
			"activity_time": formatEmailTime(activityTime),
		})
	}

	identifier := fmt.Sprintf("%s-%d", issue.Identifier, issue.SequenceID)
	issueURL := fmt.Sprintf("%s/%s/projects/%s/issues/%s", baseAPI, issue.WorkspaceSlug, issue.ProjectID, issue.ID)
	return map[string]any{
		"data": templateData, "summary": "Updates were made to the issue by",
		"actors_involved": len(actors),
		"issue": map[string]any{
			"issue_identifier": identifier, "name": issue.Name, "issue_url": issueURL,
		},
		"receiver":  map[string]any{"email": receiver.Email},
		"issue_url": issueURL,
		"project_url": fmt.Sprintf("%s/%s/projects/%s/issues/",
			baseAPI, issue.WorkspaceSlug, issue.ProjectID),
		"workspace": issue.WorkspaceSlug, "project": issue.ProjectName,
		"user_preference": fmt.Sprintf("%s/%s/settings/account/notifications/", baseAPI, issue.WorkspaceSlug),
		"comments":        comments, "entity_type": "issue",
		"__subject": identifier + " " + removeControlCharacters(issue.Name),
	}, receiver.Email, nil
}

// createPayload gathers what each person changed into one entry per field, keeping each value once.
func (tasks *EmailSendTasks) createPayload(ctx context.Context, notificationData json.RawMessage) (map[string]map[string]any, error) {
	byActor := map[string][]map[string]any{}
	if len(notificationData) > 0 {
		if err := json.Unmarshal(notificationData, &byActor); err != nil {
			return nil, err
		}
	}
	grouped := map[string]map[string]any{}
	for actorID, changes := range byActor {
		for _, change := range changes {
			activity, _ := change["issue_activity"].(map[string]any)
			if activity == nil {
				continue
			}
			fieldName := activityTextOrEmpty(activity["field"])
			// Both values are rendered as text first, which is why an absent one reads as the word rather than being skipped.
			oldValue := pythonText(activity["old_value"]).(string)
			newValue := pythonText(activity["new_value"]).(string)

			if _, present := grouped[actorID]; !present {
				grouped[actorID] = map[string]any{}
			}
			if oldValue != "" {
				appendOnce(grouped[actorID], fieldName, "old_value", oldValue)
			}
			if newValue != "" {
				appendOnce(grouped[actorID], fieldName, "new_value", newValue)
			}
			// Upstream tests a key on the wrong object here, so the time is rewritten by every change rather than kept from the first. The last one to arrive is what the email shows.
			if activityTime := activity["activity_time"]; activityTime != nil {
				grouped[actorID]["activity_time"] = normalizeActivityTime(activityTextOrEmpty(activityTime))
			}
		}
	}
	return grouped, nil
}

// appendOnce puts a value under a field's old or new list, skipping one that is already there.
func appendOnce(changes map[string]any, fieldName, side, value string) {
	field, _ := changes[fieldName].(map[string]any)
	if field == nil {
		field = map[string]any{}
		changes[fieldName] = field
	}
	values, _ := field[side].([]any)
	for _, existing := range values {
		if existing == value {
			return
		}
	}
	field[side] = append(values, value)
}

// replaceMentions rewrites every mention in a list of html values as the person's name with an at in front.
func (tasks *EmailSendTasks) replaceMentions(ctx context.Context, value any) (any, error) {
	values, ok := value.([]any)
	if !ok {
		return value, nil
	}
	rewritten := make([]any, 0, len(values))
	for _, entry := range values {
		text, ok := entry.(string)
		if !ok {
			rewritten = append(rewritten, entry)
			continue
		}
		replaced, err := tasks.replaceMentionsIn(ctx, text)
		if err != nil {
			return nil, err
		}
		rewritten = append(rewritten, replaced)
	}
	return rewritten, nil
}

var mentionTagPattern = regexp.MustCompile(`(?is)<mention-component[^>]*></mention-component>|<mention-component[^>]*/>`)

// replaceMentionsIn swaps each mention for the person's display name. A mention naming somebody who is gone is what Django reads with a get, which raises and loses the email.
func (tasks *EmailSendTasks) replaceMentionsIn(ctx context.Context, source string) (string, error) {
	var replaceErr error
	result := mentionTagPattern.ReplaceAllStringFunc(source, func(tag string) string {
		if replaceErr != nil {
			return tag
		}
		identifier := mentionIdentifier(tag)
		name, found, err := tasks.displayName(ctx, identifier)
		if err != nil {
			replaceErr = err
			return tag
		}
		if !found {
			replaceErr = fmt.Errorf("the email names somebody who is gone: %s", identifier)
			return tag
		}
		return "@" + name
	})
	return result, replaceErr
}

// mentionIdentifier reads the person out of one mention tag.
func mentionIdentifier(tag string) string {
	document, err := html.Parse(strings.NewReader(tag))
	if err != nil {
		return ""
	}
	var found string
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if found != "" {
			return
		}
		if node.Type == html.ElementNode && node.Data == "mention-component" {
			for _, attribute := range node.Attr {
				if attribute.Key == "entity_identifier" {
					found = attribute.Val
					return
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(document)
	return found
}

var controlCharacters = regexp.MustCompile(`[\x00-\x1F\x7F-\x9F]`)

// removeControlCharacters is what keeps a work item's name from breaking a subject line.
func removeControlCharacters(value string) string {
	return controlCharacters.ReplaceAllString(value, "")
}

// normalizeActivityTime is the first of the two formats the time passes through: the moment an activity was written, as a plain timestamp.
func normalizeActivityTime(value string) string {
	trimmed := strings.TrimSuffix(value, "Z")
	for _, layout := range []string{
		"2006-01-02T15:04:05.999999999", "2006-01-02T15:04:05", "2006-01-02T15:04",
		time.RFC3339Nano, time.RFC3339,
	} {
		if moment, err := time.Parse(layout, trimmed); err == nil {
			return moment.Format("2006-01-02 15:04:05")
		}
	}
	return trimmed
}

// formatEmailTime is the second: the time of day, as the email shows it.
func formatEmailTime(value string) string {
	moment, err := time.Parse("2006-01-02 15:04:05", value)
	if err != nil {
		return value
	}
	// Python's %H:%M %p is the twenty-four hour clock with an am or pm after it, which for the afternoon reads oddly and is what upstream sends.
	return moment.Format("15:04") + " " + strings.ToUpper(moment.Format("PM"))
}

func (tasks *EmailSendTasks) acquire(ctx context.Context, lockID string) (bool, error) {
	if tasks.redis == nil {
		return true, nil
	}
	return tasks.redis.SetNX(ctx, lockID, "true", emailLockTTL).Result()
}

func (tasks *EmailSendTasks) release(ctx context.Context, lockID string) {
	if tasks.redis == nil {
		return
	}
	if err := tasks.redis.Del(context.WithoutCancel(ctx), lockID).Err(); err != nil {
		tasks.logger.Warn("the email lock could not be released", "lock", lockID, "error", err)
	}
}

// origin is where the links in the email point, which the activity task parked in Redis for ten minutes.
func (tasks *EmailSendTasks) origin(ctx context.Context, issueID string) (string, error) {
	if tasks.redis == nil {
		return "", nil
	}
	value, err := tasks.redis.Get(ctx, issueID).Result()
	if err == redis.Nil {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return value, nil
}

// lockNameFor is the lock an email takes. It names every notification in the batch, sorted, which is why two emails to the same person in one sweep take the same lock and only the first goes out.
func lockNameFor(issueID, receiverID string, notificationIDs []string) string {
	sorted := append([]string(nil), notificationIDs...)
	sort.Strings(sorted)
	return "send_email_notif_" + issueID + "_" + receiverID + "_" + strings.Join(sorted, "_")
}
