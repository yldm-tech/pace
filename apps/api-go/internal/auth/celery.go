package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

const magicLinkTaskName = "plane.bgtasks.magic_link_code_task.magic_link"
const forgotPasswordTaskName = "plane.bgtasks.forgot_password_task.forgot_password"
const userActivationTaskName = "plane.bgtasks.user_activation_email_task.user_activation_email"
const emailUpdateCodeTaskName = "plane.bgtasks.user_email_update_task.send_email_update_magic_code"
const emailUpdateConfirmationTaskName = "plane.bgtasks.user_email_update_task.send_email_update_confirmation"
const userDeactivationTaskName = "plane.bgtasks.user_deactivation_email_task.user_deactivation_email"
const workspaceSeedTaskName = "plane.bgtasks.workspace_seed_task.workspace_seed"
const workspaceInvitationTaskName = "plane.bgtasks.workspace_invitation_task.workspace_invitation"
const softDeleteRelatedObjectsTaskName = "plane.bgtasks.deletion_task.soft_delete_related_objects"
const modelActivityTaskName = "plane.bgtasks.webhook_task.model_activity"
const webhookActivityTaskName = "plane.bgtasks.webhook_task.webhook_activity"
const recentVisitedTaskName = "plane.bgtasks.recent_visited_task.recent_visited_task"
const projectAddUserEmailTaskName = "plane.bgtasks.project_add_user_email_task.project_add_user_email"

type CeleryPublisher struct{ brokerURL string }

func NewCeleryPublisher(brokerURL string) *CeleryPublisher {
	return &CeleryPublisher{brokerURL: brokerURL}
}

func (publisher *CeleryPublisher) PublishMagicLink(ctx context.Context, email, key, token string) error {
	return publisher.publish(ctx, magicLinkTaskName, []any{email, key, token})
}

func (publisher *CeleryPublisher) PublishForgotPassword(ctx context.Context, firstName, email, uid, token, currentSite string) error {
	return publisher.publish(ctx, forgotPasswordTaskName, []any{firstName, email, uid, token, currentSite})
}

func (publisher *CeleryPublisher) PublishUserActivation(ctx context.Context, currentSite, userID string) error {
	return publisher.publish(ctx, userActivationTaskName, []any{currentSite, userID})
}

func (publisher *CeleryPublisher) PublishEmailUpdateCode(ctx context.Context, email, token string) error {
	return publisher.publish(ctx, emailUpdateCodeTaskName, []any{email, token})
}

func (publisher *CeleryPublisher) PublishEmailUpdateConfirmation(ctx context.Context, email string) error {
	return publisher.publish(ctx, emailUpdateConfirmationTaskName, []any{email})
}

func (publisher *CeleryPublisher) PublishUserDeactivation(ctx context.Context, currentSite, userID string) error {
	return publisher.publish(ctx, userDeactivationTaskName, []any{currentSite, userID})
}

func (publisher *CeleryPublisher) PublishWorkspaceSeed(ctx context.Context, workspaceID string) error {
	return publisher.publish(ctx, workspaceSeedTaskName, []any{workspaceID})
}

func (publisher *CeleryPublisher) PublishWorkspaceInvitation(ctx context.Context, email, workspaceID, token, currentSite, inviter string) error {
	return publisher.publish(ctx, workspaceInvitationTaskName, []any{email, workspaceID, token, currentSite, inviter})
}

func (publisher *CeleryPublisher) PublishSoftDeleteRelatedObjects(ctx context.Context, appLabel, modelName, instanceID string) error {
	return publisher.publish(ctx, softDeleteRelatedObjectsTaskName, []any{appLabel, modelName, instanceID, nil})
}

// PublishModelActivity mirrors model_activity.delay, which Django calls with
// keyword arguments after creating or updating a tracked model. requestedData
// and currentInstance carry the JSON Django compares to build the activity.
func (publisher *CeleryPublisher) PublishModelActivity(ctx context.Context, modelName, modelID string, requestedData any, currentInstance *string, actorID, slug, origin string) error {
	var instance any
	if currentInstance != nil {
		instance = *currentInstance
	}
	return publisher.publishKeywords(ctx, modelActivityTaskName, map[string]any{
		"model_name": modelName, "model_id": modelID, "requested_data": requestedData,
		"current_instance": instance, "actor_id": actorID, "slug": slug, "origin": origin,
	})
}

// PublishWebhookActivity mirrors webhook_activity.delay.
func (publisher *CeleryPublisher) PublishWebhookActivity(ctx context.Context, event, verb string, actorID, slug, currentSite, eventID string) error {
	return publisher.publishKeywords(ctx, webhookActivityTaskName, map[string]any{
		"event": event, "verb": verb, "field": nil, "old_value": nil, "new_value": nil,
		"actor_id": actorID, "slug": slug, "current_site": currentSite, "event_id": eventID,
		"old_identifier": nil, "new_identifier": nil,
	})
}

// PublishRecentVisit mirrors recent_visited_task.delay.
func (publisher *CeleryPublisher) PublishRecentVisit(ctx context.Context, entityName, entityIdentifier, userID, projectID, slug string) error {
	return publisher.publishKeywords(ctx, recentVisitedTaskName, map[string]any{
		"entity_name": entityName, "entity_identifier": entityIdentifier,
		"user_id": userID, "project_id": projectID, "slug": slug,
	})
}

// PublishProjectAddUserEmail mirrors project_add_user_email.delay, which Django
// calls positionally.
func (publisher *CeleryPublisher) PublishProjectAddUserEmail(ctx context.Context, currentSite, projectMemberID, invitorID string) error {
	return publisher.publish(ctx, projectAddUserEmailTaskName, []any{currentSite, projectMemberID, invitorID})
}

func (publisher *CeleryPublisher) publish(ctx context.Context, taskName string, arguments []any) error {
	return publisher.send(ctx, taskName, arguments, nil)
}

func (publisher *CeleryPublisher) publishKeywords(ctx context.Context, taskName string, keywords map[string]any) error {
	return publisher.send(ctx, taskName, nil, keywords)
}

func (publisher *CeleryPublisher) send(ctx context.Context, taskName string, arguments []any, keywords map[string]any) error {
	message, err := celeryMessage(taskName, arguments, keywords)
	if err != nil {
		return err
	}
	connection, err := amqp.Dial(publisher.brokerURL)
	if err != nil {
		return fmt.Errorf("connect to Celery broker: %w", err)
	}
	defer connection.Close()
	channel, err := connection.Channel()
	if err != nil {
		return fmt.Errorf("open Celery broker channel: %w", err)
	}
	defer channel.Close()
	if err := channel.PublishWithContext(ctx, "", "celery", false, false, message); err != nil {
		return fmt.Errorf("publish Celery task: %w", err)
	}
	return nil
}

func celeryMessage(taskName string, arguments []any, keywords map[string]any) (amqp.Publishing, error) {
	taskID, err := randomUUID()
	if err != nil {
		return amqp.Publishing{}, err
	}
	if arguments == nil {
		arguments = []any{}
	}
	if keywords == nil {
		keywords = map[string]any{}
	}
	body, err := json.Marshal([]any{
		arguments,
		keywords,
		map[string]any{"callbacks": nil, "errbacks": nil, "chain": nil, "chord": nil},
	})
	if err != nil {
		return amqp.Publishing{}, fmt.Errorf("encode Celery task: %w", err)
	}
	return amqp.Publishing{
		Headers: amqp.Table{
			"lang": "py", "task": taskName, "id": taskID, "shadow": nil,
			"eta": nil, "expires": nil, "group": nil, "group_index": nil,
			"retries": int32(0), "timelimit": []any{nil, nil}, "root_id": taskID,
			"parent_id": nil, "argsrepr": fmt.Sprint(arguments), "kwargsrepr": keywordsRepr(keywords),
			"origin": "pace-api-go", "ignore_result": false,
		},
		ContentType: "application/json", ContentEncoding: "utf-8", DeliveryMode: amqp.Persistent,
		CorrelationId: taskID, Timestamp: time.Now().UTC(), Body: body,
	}, nil
}

// keywordsRepr renders the kwargsrepr header Celery uses for logging only; the
// broker never parses it, so a stable ordering is enough.
func keywordsRepr(keywords map[string]any) string {
	if len(keywords) == 0 {
		return "{}"
	}
	names := make([]string, 0, len(keywords))
	for name := range keywords {
		names = append(names, name)
	}
	sort.Strings(names)
	var builder strings.Builder
	builder.WriteString("{")
	for index, name := range names {
		if index > 0 {
			builder.WriteString(", ")
		}
		fmt.Fprintf(&builder, "'%s': %v", name, keywords[name])
	}
	builder.WriteString("}")
	return builder.String()
}
