package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"golang.org/x/net/html"
	"gorm.io/gorm"
)

// NotificationsTask turns the history a change produced into the notifications people see and the emails they are sent.
const NotificationsTask = "plane.bgtasks.notification_task.notifications"

// silentActivityTypes are the thirteen the task looks at and does nothing about. Being added to a cycle or a module, a reaction, a vote and anything to do with a draft all pass through without a notification.
var silentActivityTypes = map[string]bool{
	"cycle.activity.created": true, "cycle.activity.deleted": true,
	"module.activity.created": true, "module.activity.deleted": true,
	"issue_reaction.activity.created": true, "issue_reaction.activity.deleted": true,
	"comment_reaction.activity.created": true, "comment_reaction.activity.deleted": true,
	"issue_vote.activity.created": true, "issue_vote.activity.deleted": true,
	"issue_draft.activity.created": true, "issue_draft.activity.updated": true,
	"issue_draft.activity.deleted": true,
}

// errNotificationsAbandoned marks what Django lets reach the task's own except, which prints the error and returns — so nothing at all is written, not the notifications and not the email logs. Every place it is raised says which lookup did it.
var errNotificationsAbandoned = errors.New("notifications: the batch was abandoned")

// NotificationTasks writes what people see and what they are sent.
type NotificationTasks struct {
	db     *gorm.DB
	logger *slog.Logger
	clock  func() time.Time
}

func NewNotificationTasks(db *gorm.DB, logger *slog.Logger) *NotificationTasks {
	return &NotificationTasks{db: db, logger: logger, clock: time.Now}
}

func (tasks *NotificationTasks) Register(consumer *Consumer) {
	consumer.Register(NotificationsTask, tasks.notifications)
}

// notificationRow is one thing somebody will see in their inbox.
type notificationRow struct {
	ID               string
	WorkspaceID      string
	ProjectID        string
	Sender           string
	TriggeredByID    string
	ReceiverID       string
	EntityIdentifier string
	Title            string
	Message          any
	Data             map[string]any
}

// emailLogRow is one thing somebody will be sent.
type emailLogRow struct {
	ID               string
	TriggeredByID    string
	ReceiverID       string
	EntityIdentifier string
	Data             map[string]any
}

// notifications reproduces the task. It reads the history a change produced and decides, for each person who cares about the work item, whether they hear about it and whether they are emailed.
func (tasks *NotificationTasks) notifications(ctx context.Context, arguments []any, keywords map[string]any) error {
	activityType := stringArgument(arguments, keywords, 0, "type")
	issueID := stringArgument(arguments, keywords, 1, "issue_id")
	projectID := stringArgument(arguments, keywords, 2, "project_id")
	actorID := stringArgument(arguments, keywords, 3, "actor_id")
	subscribeActor := boolArgumentWithDefault(arguments, keywords, 4, "subscriber", true)
	activitiesRaw := stringArgument(arguments, keywords, 5, "issue_activities_created")
	requestedData := stringArgument(arguments, keywords, 6, "requested_data")
	currentInstance := stringArgument(arguments, keywords, 7, "current_instance")

	if silentActivityTypes[activityType] {
		return nil
	}
	var activities []map[string]any
	if activitiesRaw != "" {
		if err := json.Unmarshal([]byte(activitiesRaw), &activities); err != nil {
			tasks.logger.Warn("notifications: the history is not json", "issue", issueID)
			return nil
		}
	}

	err := tasks.build(ctx, notificationRequest{
		activityType: activityType, issueID: issueID, projectID: projectID, actorID: actorID,
		subscribeActor: subscribeActor, activities: activities,
		requestedData: requestedData, currentInstance: currentInstance,
	})
	if errors.Is(err, errNotificationsAbandoned) {
		tasks.logger.Warn("notifications abandoned", "type", activityType, "issue", issueID, "error", err)
		return nil
	}
	return err
}

type notificationRequest struct {
	activityType    string
	issueID         string
	projectID       string
	actorID         string
	subscribeActor  bool
	activities      []map[string]any
	requestedData   string
	currentInstance string
}

func (tasks *NotificationTasks) build(ctx context.Context, request notificationRequest) error {
	now := tasks.clock().UTC()

	members, err := tasks.activeProjectMembers(ctx, request.projectID)
	if err != nil {
		return err
	}

	// A mention is somebody named in the description. New ones are narrowed to the project's own people; removed ones are not, because they are only used to take the record away.
	requestedMentions := mentionsIn(request.requestedData)
	currentMentions := mentionsIn(request.currentInstance)
	newMentions := narrowTo(difference(requestedMentions, currentMentions), members)
	removedMentions := difference(currentMentions, requestedMentions)

	commentMentions := []string{}
	allCommentMentions := []string{}
	for _, activity := range request.activities {
		if activity["issue_comment"] == nil {
			continue
		}
		newValue := activityTextOrEmpty(activity["new_value"])
		oldValue := activity["old_value"]
		allCommentMentions = append(allCommentMentions, extractMentions(newValue)...)
		fresh := extractMentions(newValue)
		if oldValue != nil {
			fresh = difference(fresh, extractMentions(activityTextOrEmpty(oldValue)))
		}
		commentMentions = append(commentMentions, fresh...)
		// The narrowing happens inside the loop rather than after it, which is upstream's shape and makes no difference to the result.
		commentMentions = narrowTo(commentMentions, members)
	}

	issue, found, err := tasks.notificationIssue(ctx, request.issueID)
	if err != nil {
		return err
	}
	if !found {
		// Django reads the work item's name off None, which raises.
		return fmt.Errorf("%w: the work item is gone", errNotificationsAbandoned)
	}

	project, found, err := tasks.notificationProject(ctx, request.projectID)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("%w: the project is gone", errNotificationsAbandoned)
	}

	excluded := map[string]bool{request.actorID: true}
	for _, mention := range append(append([]string{}, newMentions...), commentMentions...) {
		excluded[mention] = true
	}
	subscribers, err := tasks.issueSubscribers(ctx, request.projectID, request.issueID, members, excluded)
	if err != nil {
		return err
	}

	if request.subscribeActor {
		// get_or_create, and its failure is swallowed on its own rather than losing the batch.
		if err := tasks.subscribeOne(ctx, project, request.issueID, request.actorID, now); err != nil {
			tasks.logger.Warn("the actor could not be subscribed", "issue", request.issueID, "error", err)
		}
	}

	assignees, err := tasks.issueAssignees(ctx, request.projectID, request.issueID, members)
	if err != nil {
		return err
	}

	notifications := []notificationRow{}
	emails := []emailLogRow{}

	// lastSubscriber and lastActivity are what the two mention loops below read by accident. They are the loop variables this loop leaves behind, and upstream reads them after it has finished.
	lastSubscriber := ""
	var lastActivityInLoop map[string]any

	for _, subscriber := range subscribers {
		sender := "in_app:issue_activities:subscribed"
		switch {
		case issue.CreatedByID != nil && *issue.CreatedByID == subscriber:
			sender = "in_app:issue_activities:created"
		case assignees[subscriber] && (issue.CreatedByID == nil || !assignees[*issue.CreatedByID]):
			sender = "in_app:issue_activities:assigned"
		}

		preference, found, err := tasks.notificationPreference(ctx, subscriber)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("%w: %s has no notification preference", errNotificationsAbandoned, subscriber)
		}

		lastSubscriber = subscriber
		for _, activity := range request.activities {
			detail, _ := activity["issue_detail"].(map[string]any)
			if activityTextOrEmpty(detail["id"]) != request.issueID {
				continue
			}
			if activityTextOrEmpty(activity["field"]) == "description" {
				continue
			}
			lastActivityInLoop = activity

			field := activityTextOrEmpty(activity["field"])
			sendEmail := preference.PropertyChange
			switch {
			case field == "state" && preference.StateChange:
				sendEmail = true
			case field == "comment" && preference.Comment:
				sendEmail = true
			}

			commentText := ""
			if activity["issue_comment"] != nil {
				commentText, err = tasks.commentStripped(ctx, activityTextOrEmpty(activity["issue_comment"]),
					request.issueID, request.projectID, project.WorkspaceID)
				if err != nil {
					return err
				}
			}

			identifier, err := newTaskUUID()
			if err != nil {
				return err
			}
			notifications = append(notifications, notificationRow{
				ID: identifier, WorkspaceID: project.WorkspaceID, ProjectID: request.projectID,
				Sender: sender, TriggeredByID: request.actorID, ReceiverID: subscriber,
				EntityIdentifier: request.issueID,
				Title:            activityTextOrEmpty(activity["comment"]),
				Data: map[string]any{
					"issue":          issueData(issue, project, false),
					"issue_activity": activityData(activity, commentText, "", false),
				},
			})
			if !sendEmail {
				continue
			}
			logID, err := newTaskUUID()
			if err != nil {
				return err
			}
			emails = append(emails, emailLogRow{
				ID: logID, TriggeredByID: request.actorID, ReceiverID: subscriber,
				EntityIdentifier: request.issueID,
				Data: map[string]any{
					"issue":          issueData(issue, project, true),
					"issue_activity": activityData(activity, commentText, "", true),
				},
			})
		}
	}

	// The people named in the description and in the comments become subscribers whether or not they hear about this particular change.
	if err := tasks.subscribeMany(ctx, project, request.issueID, requestedMentions, now); err != nil {
		return err
	}
	if err := tasks.subscribeMany(ctx, project, request.issueID, allCommentMentions, now); err != nil {
		return err
	}

	lastActivity, err := tasks.lastIssueActivity(ctx, request.issueID)
	if err != nil {
		return err
	}
	actorName, found, err := tasks.displayName(ctx, request.actorID)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("%w: the actor is gone", errNotificationsAbandoned)
	}

	for _, mention := range commentMentions {
		if mention == request.actorID {
			continue
		}
		preference, found, err := tasks.notificationPreference(ctx, mention)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("%w: %s has no notification preference", errNotificationsAbandoned, mention)
		}
		for _, activity := range request.activities {
			if preference.Mention {
				logID, err := newTaskUUID()
				if err != nil {
					return err
				}
				emails = append(emails, emailLogRow{
					ID: logID, TriggeredByID: request.actorID, ReceiverID: mention,
					EntityIdentifier: request.issueID,
					Data: map[string]any{
						"issue":          issueData(issue, project, true),
						"issue_activity": activityData(activity, "", "mention", true),
					},
				})
			}
			row, err := tasks.mentionNotification(project, issue, request,
				actorName+" has mentioned you in a comment in issue "+issue.Name, mention, activity)
			if err != nil {
				return err
			}
			notifications = append(notifications, row)
		}
	}

	for _, mention := range newMentions {
		if mention == request.actorID {
			continue
		}
		preference, found, err := tasks.notificationPreference(ctx, mention)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("%w: %s has no notification preference", errNotificationsAbandoned, mention)
		}
		collapsed := lastActivity != nil && lastActivity.Field != nil && *lastActivity.Field == "description" &&
			lastActivity.ActorID != nil && *lastActivity.ActorID == request.actorID
		if collapsed {
			// This branch reads the two identifiers off the subscriber loop's last activity. When that loop never ran there is no such variable and Django raises a NameError, which loses everything.
			if lastActivityInLoop == nil {
				return fmt.Errorf("%w: the mention branch reads a variable the subscriber loop never bound", errNotificationsAbandoned)
			}
			identifier, err := newTaskUUID()
			if err != nil {
				return err
			}
			notifications = append(notifications, notificationRow{
				ID: identifier, WorkspaceID: project.WorkspaceID, ProjectID: request.projectID,
				Sender: "in_app:issue_activities:mentioned", TriggeredByID: request.actorID, ReceiverID: mention,
				EntityIdentifier: request.issueID,
				Message:          "You have been mentioned in the issue " + issue.Name,
				Data: map[string]any{
					"issue":          issueData(issue, project, true),
					"issue_activity": lastActivityData(lastActivity, lastActivityInLoop, false),
				},
			})
			if preference.Mention {
				// The receiver here is the subscriber loop's leftover rather than the person mentioned, which is what sends this email to the wrong reader.
				receiver, err := tasks.leftoverReceiver(lastSubscriber)
				if err != nil {
					return err
				}
				logID, err := newTaskUUID()
				if err != nil {
					return err
				}
				emails = append(emails, emailLogRow{
					ID: logID, TriggeredByID: request.actorID, ReceiverID: receiver,
					EntityIdentifier: request.issueID,
					Data: map[string]any{
						"issue":          issueData(issue, project, false),
						"issue_activity": lastActivityData(lastActivity, lastActivityInLoop, true),
					},
				})
			}
			continue
		}
		for _, activity := range request.activities {
			if preference.Mention {
				receiver, err := tasks.leftoverReceiver(lastSubscriber)
				if err != nil {
					return err
				}
				logID, err := newTaskUUID()
				if err != nil {
					return err
				}
				emails = append(emails, emailLogRow{
					ID: logID, TriggeredByID: request.actorID, ReceiverID: receiver,
					EntityIdentifier: request.issueID,
					Data: map[string]any{
						"issue":          issueData(issue, project, false),
						"issue_activity": activityData(activity, "", "mention", true),
					},
				})
			}
			row, err := tasks.mentionNotification(project, issue, request,
				"You have been mentioned in the issue "+issue.Name, mention, activity)
			if err != nil {
				return err
			}
			notifications = append(notifications, row)
		}
	}

	if err := tasks.recordMentions(ctx, project, request.issueID, newMentions, removedMentions, now); err != nil {
		return err
	}
	if err := tasks.writeNotifications(ctx, notifications, now); err != nil {
		return err
	}
	return tasks.writeEmailLogs(ctx, emails, now)
}

// leftoverReceiver is the variable the two mention loops read by accident. When the subscriber loop ran it is the last subscriber's id; when it did not, upstream still holds the task's own `subscriber` flag there and writing a boolean into a uuid column loses the whole batch.
func (tasks *NotificationTasks) leftoverReceiver(lastSubscriber string) (string, error) {
	if lastSubscriber == "" {
		return "", fmt.Errorf("%w: the mention email names the subscriber flag rather than a person", errNotificationsAbandoned)
	}
	return lastSubscriber, nil
}

// mentionNotification is create_mention_notification, which unlike the subscriber's carries a message rather than a title.
func (tasks *NotificationTasks) mentionNotification(project notificationProject, issue notificationIssue, request notificationRequest, message, mention string, activity map[string]any) (notificationRow, error) {
	identifier, err := newTaskUUID()
	if err != nil {
		return notificationRow{}, err
	}
	return notificationRow{
		ID: identifier, WorkspaceID: project.WorkspaceID, ProjectID: request.projectID,
		Sender: "in_app:issue_activities:mentioned", TriggeredByID: request.actorID, ReceiverID: mention,
		EntityIdentifier: request.issueID, Message: message,
		Data: map[string]any{
			"issue":          issueData(issue, project, false),
			"issue_activity": activityData(activity, "", "", false),
		},
	}, nil
}

// issueData is the work item as a notification describes it. The email's copy carries two more fields than the inbox's, which is what lets the email link back into the app.
func issueData(issue notificationIssue, project notificationProject, forEmail bool) map[string]any {
	data := map[string]any{
		"id": issue.ID, "name": issue.Name, "identifier": project.Identifier,
		"sequence_id": issue.SequenceID, "state_name": issue.StateName, "state_group": issue.StateGroup,
	}
	if forEmail {
		data["project_id"] = project.ID
		data["workspace_slug"] = project.WorkspaceSlug
	}
	return data
}

// activityData is the line of history as a notification describes it.
//
// Every value is rendered as text the way Python's str() renders it, so a value that was absent reads as the word "None" rather than as nothing — except the two identifiers, which are null when they are empty.
func activityData(activity map[string]any, commentText, fieldOverride string, forEmail bool) map[string]any {
	field := activityTextOrEmpty(activity["field"])
	if fieldOverride != "" {
		field = fieldOverride
	}
	data := map[string]any{
		"id": pythonText(activity["id"]), "verb": pythonText(activity["verb"]),
		"field":     pythonTextOf(field, activity["field"], fieldOverride != ""),
		"actor":     pythonText(activity["actor_id"]),
		"new_value": pythonText(activity["new_value"]), "old_value": pythonText(activity["old_value"]),
		"old_identifier": identifierOrNil(activity["old_identifier"]),
		"new_identifier": identifierOrNil(activity["new_identifier"]),
	}
	if forEmail {
		data["activity_time"] = activity["created_at"]
	} else {
		// The inbox's copy carries the comment's plain text; the email's does not.
		data["issue_comment"] = commentText
	}
	return data
}

// lastActivityData is the one place a notification describes a row read back out of the table rather than one handed over, and it mixes the two: the values come from that row and the two identifiers come from the subscriber loop's leftover activity.
func lastActivityData(last *lastActivityRow, leftover map[string]any, forEmail bool) map[string]any {
	field := "mention"
	if !forEmail {
		field = pythonText(last.Field).(string)
	}
	data := map[string]any{
		"id": last.ID, "verb": last.Verb, "field": field,
		"actor":     pythonText(last.ActorID),
		"new_value": pythonText(last.NewValue), "old_value": pythonText(last.OldValue),
		"old_identifier": identifierOrNil(leftover["old_identifier"]),
		"new_identifier": identifierOrNil(leftover["new_identifier"]),
	}
	if forEmail {
		data["activity_time"] = last.CreatedAt.Format(time.RFC3339Nano)
	}
	return data
}

// pythonText renders a value the way str() does, which turns an absent one into the word rather than into nothing.
func pythonText(value any) any {
	if value == nil {
		return "None"
	}
	return activityTextOrEmpty(value)
}

func pythonTextOf(rendered string, original any, overridden bool) any {
	if overridden {
		return rendered
	}
	return pythonText(original)
}

// identifierOrNil keeps an identifier null when it is empty, which is the one place the rendering stops short of the word.
func identifierOrNil(value any) any {
	if value == nil {
		return nil
	}
	rendered := activityTextOrEmpty(value)
	if rendered == "" {
		return nil
	}
	return rendered
}

// mentionsIn reads the people named in a description out of one of the two snapshots.
func mentionsIn(snapshot string) []string {
	if snapshot == "" {
		return nil
	}
	decoded := map[string]any{}
	if err := json.Unmarshal([]byte(snapshot), &decoded); err != nil {
		return nil
	}
	return extractMentions(activityTextOrEmpty(decoded["description_html"]))
}

// extractMentions finds every person named in a piece of html, deduplicated.
func extractMentions(source string) []string {
	if source == "" {
		return nil
	}
	document, err := html.Parse(strings.NewReader(source))
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	mentions := []string{}
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == "mention-component" {
			attributes := map[string]string{}
			for _, attribute := range node.Attr {
				attributes[attribute.Key] = attribute.Val
			}
			if attributes["entity_name"] == "user_mention" {
				identifier := attributes["entity_identifier"]
				if identifier != "" && !seen[identifier] {
					seen[identifier] = true
					mentions = append(mentions, identifier)
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(document)
	// A set has no order in Python either, so fixing one here changes nothing a reader can see and makes the result the same twice running.
	sort.Strings(mentions)
	return mentions
}

func difference(left, right []string) []string {
	excluded := map[string]bool{}
	for _, value := range right {
		excluded[value] = true
	}
	result := []string{}
	for _, value := range left {
		if !excluded[value] {
			result = append(result, value)
		}
	}
	return result
}

func narrowTo(values []string, allowed map[string]bool) []string {
	result := []string{}
	for _, value := range values {
		if allowed[value] {
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}
