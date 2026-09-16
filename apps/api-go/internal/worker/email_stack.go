package worker

import (
	"context"
	"encoding/json"
	"log/slog"
	"sort"
	"time"

	"gorm.io/gorm"
)

// StackEmailNotificationTask gathers the notifications nobody has been emailed about yet and turns them into one email per person per work item.
const StackEmailNotificationTask = "plane.bgtasks.email_notification_task.stack_email_notification"

// EmailNotificationPublisher queues one of those emails.
type EmailNotificationPublisher interface {
	PublishSendEmailNotification(ctx context.Context, keywords map[string]any) error
}

// EmailStackTasks is the five-minute sweep that batches notifications into emails.
type EmailStackTasks struct {
	db     *gorm.DB
	sends  EmailNotificationPublisher
	logger *slog.Logger
	clock  func() time.Time
}

func NewEmailStackTasks(db *gorm.DB, sends EmailNotificationPublisher, logger *slog.Logger) *EmailStackTasks {
	return &EmailStackTasks{db: db, sends: sends, logger: logger, clock: time.Now}
}

func (tasks *EmailStackTasks) Register(consumer *Consumer) {
	consumer.Register(StackEmailNotificationTask, tasks.stackEmailNotification)
}

// stackEmailNotification reproduces stack_email_notification: read everything unsent, group it by who is to be told and about what, and queue one email per pair.
//
// **Every email a person is queued carries all of that person's ids**, not just the ones about the work item it is for. The list is built across the whole of a person's notifications and then handed to each of their emails, so the first one sent marks the rest as sent too — and the lock each email takes is built from that same list, so two emails to the same person in one sweep take the same lock and only one of them goes. Reproduced rather than corrected: this is why a person with changes on three work items gets one email rather than three.
func (tasks *EmailStackTasks) stackEmailNotification(ctx context.Context, _ []any, _ map[string]any) error {
	var rows []struct {
		ID               string `gorm:"column:id"`
		ReceiverID       string `gorm:"column:receiver_id"`
		TriggeredByID    string `gorm:"column:triggered_by_id"`
		EntityIdentifier string `gorm:"column:entity_identifier"`
		Data             []byte `gorm:"column:data"`
	}
	err := tasks.db.WithContext(ctx).Table("email_notification_logs").
		Select("id, receiver_id, triggered_by_id, entity_identifier, data").
		Where("processed_at IS NULL AND deleted_at IS NULL").
		Order("receiver_id").Scan(&rows).Error
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}

	// Grouped by who is to be told, then by what happened, then by who did it.
	byReceiver := map[string][]int{}
	receivers := []string{}
	for index, row := range rows {
		if _, seen := byReceiver[row.ReceiverID]; !seen {
			receivers = append(receivers, row.ReceiverID)
		}
		byReceiver[row.ReceiverID] = append(byReceiver[row.ReceiverID], index)
	}
	// Python takes a set of the receivers, which has no order. Fixing one makes a sweep do the same thing twice.
	sort.Strings(receivers)

	processed := make([]string, 0, len(rows))
	for _, receiver := range receivers {
		indexes := byReceiver[receiver]

		entities := []string{}
		byEntity := map[string]map[string][]json.RawMessage{}
		// This is the list that ends up on every one of the person's emails.
		notificationIDs := make([]string, 0, len(indexes))
		for _, index := range indexes {
			row := rows[index]
			if _, seen := byEntity[row.EntityIdentifier]; !seen {
				entities = append(entities, row.EntityIdentifier)
				byEntity[row.EntityIdentifier] = map[string][]json.RawMessage{}
			}
			byEntity[row.EntityIdentifier][row.TriggeredByID] = append(
				byEntity[row.EntityIdentifier][row.TriggeredByID], rawJSONOrNull(row.Data))
			notificationIDs = append(notificationIDs, row.ID)
			processed = append(processed, row.ID)
		}

		for _, entity := range entities {
			data, err := orderedActorData(byEntity[entity])
			if err != nil {
				return err
			}
			err = tasks.sends.PublishSendEmailNotification(ctx, map[string]any{
				"issue_id": entity, "notification_data": data,
				"receiver_id": receiver, "email_notification_ids": notificationIDs,
			})
			if err != nil {
				return err
			}
		}
	}

	now := tasks.clock().UTC()
	err = tasks.db.WithContext(ctx).Table("email_notification_logs").
		Where("id IN ?", processed).Update("processed_at", now).Error
	if err != nil {
		return err
	}
	tasks.logger.Info("email notifications stacked", "logs", len(processed), "receivers", len(receivers))
	return nil
}

// orderedActorData writes the per-actor lists with their keys in a fixed order, since a map has none and the email is rendered from this.
func orderedActorData(byActor map[string][]json.RawMessage) (json.RawMessage, error) {
	actors := make([]string, 0, len(byActor))
	for actor := range byActor {
		actors = append(actors, actor)
	}
	sort.Strings(actors)
	fields := make([]field, 0, len(actors))
	for _, actor := range actors {
		fields = append(fields, field{actor, orderedList(byActor[actor])})
	}
	return orderedJSON(fields)
}

// rawJSONOrNull keeps a json column as it is stored, and a null one as null rather than as an empty object — this one really can be absent.
func rawJSONOrNull(value []byte) json.RawMessage {
	if len(value) == 0 {
		return json.RawMessage("null")
	}
	return json.RawMessage(value)
}
