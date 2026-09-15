package worker

import (
	"context"
	"encoding/json"
	"log/slog"
	"reflect"
	"sort"
)

// ModelActivityTask is the first of the three links in the webhook chain: it works out what changed and fans one webhook_activity out per changed field.
const ModelActivityTask = "plane.bgtasks.webhook_task.model_activity"

// WebhookActivityPublisher is how this task hands each change on. The next link still runs on the Python worker, which is what picks the webhooks that want the event and delivers to them.
type WebhookActivityPublisher interface {
	PublishWebhookActivityChange(ctx context.Context, keywords map[string]any) error
}

// ModelActivityTasks turns a saved model into the changes a webhook should hear about.
type ModelActivityTasks struct {
	publisher WebhookActivityPublisher
	logger    *slog.Logger
}

func NewModelActivityTasks(publisher WebhookActivityPublisher, logger *slog.Logger) *ModelActivityTasks {
	return &ModelActivityTasks{publisher: publisher, logger: logger}
}

func (tasks *ModelActivityTasks) Register(consumer *Consumer) {
	consumer.Register(ModelActivityTask, tasks.modelActivity)
}

// modelActivity reproduces model_activity: compare what was asked for against what was there and report each field that really moved.
//
// Two things about the comparison decide what a webhook ever hears:
//
// A **create** is not a comparison at all. No previous state means one activity with the verb "created", no field named and no values — the next link fills in the whole object from the database.
//
// An **update** only looks at keys the previous state also had. A field the request introduced that the snapshot never carried is not a change as far as this is concerned, because the snapshot is what the model looked like and a key it lacks is a key the model does not have. So adding a field to a serializer makes its first write invisible to webhooks.
func (tasks *ModelActivityTasks) modelActivity(ctx context.Context, arguments []any, keywords map[string]any) error {
	modelName := stringArgument(arguments, keywords, 0, "model_name")
	modelID := stringArgument(arguments, keywords, 1, "model_id")
	requestedRaw, _ := argument(arguments, keywords, 2, "requested_data")
	currentRaw, _ := argument(arguments, keywords, 3, "current_instance")
	actorID := stringArgument(arguments, keywords, 4, "actor_id")
	slug := stringArgument(arguments, keywords, 5, "slug")
	origin := stringArgument(arguments, keywords, 6, "origin")

	current, present := decodeActivitySnapshot(currentRaw)
	if !present {
		return tasks.publish(ctx, modelName, "created", nil, nil, nil, actorID, slug, origin, modelID)
	}

	requested, _ := decodeActivitySnapshot(requestedRaw)
	// The order the fields go out in is fixed here. Python walks the request's own key order; a map has none, and a stable order is what makes the stream readable and the tests meaningful.
	fields := make([]string, 0, len(requested))
	for field := range requested {
		fields = append(fields, field)
	}
	sort.Strings(fields)

	for _, field := range fields {
		was, known := current[field]
		if !known {
			continue
		}
		now := requested[field]
		if reflect.DeepEqual(was, now) {
			continue
		}
		name := field
		err := tasks.publish(ctx, modelName, "updated", &name, was, now, actorID, slug, origin, modelID)
		if err != nil {
			return err
		}
	}
	return nil
}

func (tasks *ModelActivityTasks) publish(ctx context.Context, event, verb string, field *string, oldValue, newValue any, actorID, slug, origin, eventID string) error {
	if tasks.publisher == nil {
		return nil
	}
	return tasks.publisher.PublishWebhookActivityChange(ctx, map[string]any{
		"event": event, "verb": verb, "field": nullableString(field),
		"old_value": oldValue, "new_value": newValue,
		"actor_id": actorID, "slug": slug, "current_site": origin,
		"event_id": eventID, "old_identifier": nil, "new_identifier": nil,
	})
}

// decodeActivitySnapshot reads one of the two states. Django hands them over as json strings when they came from a serializer and as objects when they did not, and reports whether there was a state at all.
func decodeActivitySnapshot(value any) (map[string]any, bool) {
	switch typed := value.(type) {
	case nil:
		return nil, false
	case string:
		if typed == "" {
			return nil, false
		}
		snapshot := map[string]any{}
		if err := json.Unmarshal([]byte(typed), &snapshot); err != nil {
			return nil, false
		}
		return snapshot, true
	case map[string]any:
		return typed, true
	}
	return nil, false
}
