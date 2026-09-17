package worker

import (
	"context"
	"encoding/json"
)

// WebhookActivityTask is the middle link in the webhook chain: it picks the webhooks that asked about an event and hands each one a copy.
const WebhookActivityTask = "plane.bgtasks.webhook_task.webhook_activity"

// WebhookSendPublisher queues one delivery.
type WebhookSendPublisher interface {
	PublishWebhookSend(ctx context.Context, keywords map[string]any) error
}

// webhookEventColumns says which switch on a webhook an event answers to. An event with no switch here reaches every active webhook in the workspace.
var webhookEventColumns = map[string]string{
	"project": "project", "issue": "issue",
	"module": "module", "module_issue": "module",
	"cycle": "cycle", "cycle_issue": "cycle",
	"issue_comment": "issue_comment",
}

func (tasks *WebhookTasks) registerActivity(consumer *Consumer) {
	consumer.Register(WebhookActivityTask, tasks.webhookActivity)
}

// webhookActivity reproduces webhook_activity.
//
// **A delete carries only an id.** Every other verb reads the object back out of the database and sends it in full; a delete cannot, because the row is gone, so the payload is `{"id": ...}` and nothing else.
//
// **An intake work item reaches every webhook.** There is no switch for it, and the filter only narrows for the seven events that have one — so a webhook that asked for nothing at all still hears about one.
func (tasks *WebhookTasks) webhookActivity(ctx context.Context, arguments []any, keywords map[string]any) error {
	event := stringArgument(arguments, keywords, 0, "event")
	verb := stringArgument(arguments, keywords, 1, "verb")
	fieldName := stringArgument(arguments, keywords, 2, "field")
	oldValue, _ := argument(arguments, keywords, 3, "old_value")
	newValue, _ := argument(arguments, keywords, 4, "new_value")
	actorID := stringArgument(arguments, keywords, 5, "actor_id")
	slug := stringArgument(arguments, keywords, 6, "slug")
	currentSite := stringArgument(arguments, keywords, 7, "current_site")
	eventID := stringArgument(arguments, keywords, 8, "event_id")
	oldIdentifier, _ := argument(arguments, keywords, 9, "old_identifier")
	newIdentifier, _ := argument(arguments, keywords, 10, "new_identifier")

	query := tasks.db.WithContext(ctx).Table("webhooks w").
		Joins("JOIN workspaces ws ON ws.id = w.workspace_id").
		Where("ws.slug = ? AND w.is_active = TRUE AND w.deleted_at IS NULL", slug)
	if column, narrows := webhookEventColumns[event]; narrows {
		query = query.Where("w." + column + " = TRUE")
	}
	identifiers := []string{}
	if err := query.Order("w.created_at DESC").Pluck("w.id", &identifiers).Error; err != nil {
		return err
	}
	if len(identifiers) == 0 {
		return nil
	}

	var eventData json.RawMessage
	if verb == "deleted" {
		encoded, err := orderedJSON([]field{{"id", eventID}})
		if err != nil {
			return err
		}
		eventData = encoded
	} else {
		data, found, err := tasks.modelData(ctx, event, eventID)
		if err != nil {
			return err
		}
		if !found {
			// Django reads this with a get and lets the DoesNotExist end the task, so no webhook hears about it.
			tasks.logger.Warn("webhook event object is gone", "event", event, "id", eventID)
			return nil
		}
		eventData = data
	}

	actor, found, err := tasks.modelData(ctx, "user", actorID)
	if err != nil {
		return err
	}
	if !found {
		tasks.logger.Warn("webhook actor is gone", "actor", actorID)
		return nil
	}
	activity, err := orderedJSON([]field{
		{"field", nullableActivityField(fieldName)},
		{"new_value", newValue}, {"old_value", oldValue},
		{"actor", actor},
		{"old_identifier", oldIdentifier}, {"new_identifier", newIdentifier},
	})
	if err != nil {
		return err
	}

	for _, webhookID := range identifiers {
		err := tasks.sends.PublishWebhookSend(ctx, map[string]any{
			"webhook_id": webhookID, "slug": slug, "event": event,
			"event_data": eventData, "action": verb,
			"current_site": currentSite, "activity": activity,
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// nullableActivityField keeps a change with no field named as null rather than as an empty string, which is what a create sends.
func nullableActivityField(name string) any {
	if name == "" {
		return nil
	}
	return name
}
