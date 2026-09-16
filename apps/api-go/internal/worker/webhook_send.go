package worker

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"sort"
	"strconv"
	"time"

	"github.com/yldm-tech/pace/apps/api-go/internal/httpsafe"
	"gorm.io/gorm"
)

// WebhookSendTask is the last link in the webhook chain: it takes one webhook and one event and delivers it.
const WebhookSendTask = "plane.bgtasks.webhook_task.webhook_send_task"

// The delivery's own settings. The backoff is what Celery computes from retry_backoff and retry_backoff_max, which are both ten minutes — so every wait is a uniformly random stretch between none at all and ten minutes, rather than a doubling one.
const (
	webhookTimeout     = 30 * time.Second
	webhookMaxRetries  = 5
	webhookBackoffCeil = 600 * time.Second
)

// WebhookDeactivationPublisher queues the email that tells somebody their webhook has been switched off.
type WebhookDeactivationPublisher interface {
	PublishWebhookDeactivation(ctx context.Context, webhookID, receiverID, currentSite, reason string) error
}

// WebhookTasks delivers one event to one webhook.
type WebhookTasks struct {
	db        *gorm.DB
	settings  httpsafe.Settings
	emails    WebhookDeactivationPublisher
	logger    *slog.Logger
	clock     func() time.Time
	backoff   func(attempt int) time.Duration
	scheduler func(delay time.Duration, run func())
}

func NewWebhookTasks(db *gorm.DB, settings httpsafe.Settings, emails WebhookDeactivationPublisher, logger *slog.Logger) *WebhookTasks {
	tasks := &WebhookTasks{db: db, settings: settings, emails: emails, logger: logger, clock: time.Now}
	tasks.backoff = defaultWebhookBackoff
	tasks.scheduler = func(delay time.Duration, run func()) { time.AfterFunc(delay, run) }
	return tasks
}

func (tasks *WebhookTasks) Register(consumer *Consumer) {
	consumer.Register(WebhookSendTask, tasks.webhookSend)
}

// defaultWebhookBackoff is Celery's exponential backoff with full jitter, which for these settings is not exponential at all: the doubling is capped at the same ten minutes it starts from, so every wait is a uniformly random stretch of up to ten minutes.
func defaultWebhookBackoff(int) time.Duration {
	return time.Duration(rand.Int63n(int64(webhookBackoffCeil) + 1))
}

// webhookVerbs is how the http method a change was made with is named in the payload.
var webhookVerbs = map[string]string{
	"POST": "create", "PATCH": "update", "PUT": "update", "DELETE": "delete",
}

type webhookRow struct {
	ID          string  `gorm:"column:id"`
	URL         string  `gorm:"column:url"`
	SecretKey   string  `gorm:"column:secret_key"`
	WorkspaceID string  `gorm:"column:workspace_id"`
	CreatedByID *string `gorm:"column:created_by_id"`
}

// webhookSend reproduces webhook_send_task: build the payload, sign it, deliver it to an address that was checked first, and keep a log of what happened either way.
//
// Nothing here fails the delivery in the Celery sense except a network error, and that is what the retry is for. A webhook whose url turns out to point somewhere internal is logged and dropped, not retried and not switched off — the cause may be a transient dns answer, and switching a customer's webhook off for that would be worse than missing the event.
func (tasks *WebhookTasks) webhookSend(ctx context.Context, arguments []any, keywords map[string]any) error {
	return tasks.deliver(ctx, webhookDelivery{
		webhookID: stringArgument(arguments, keywords, 0, "webhook_id"),
		slug:      stringArgument(arguments, keywords, 1, "slug"),
		event:     stringArgument(arguments, keywords, 2, "event"),
		// The two payload halves are spliced in as they arrived rather than decoded and rebuilt, which keeps the bytes — and so the key order — the same as the side that serialized them.
		eventData:   rawArgument(arguments, keywords, 3, "event_data"),
		action:      stringArgument(arguments, keywords, 4, "action"),
		currentSite: stringArgument(arguments, keywords, 5, "current_site"),
		activity:    rawArgument(arguments, keywords, 6, "activity"),
	}, 0)
}

type webhookDelivery struct {
	webhookID   string
	slug        string
	event       string
	eventData   json.RawMessage
	action      string
	currentSite string
	activity    json.RawMessage
}

func (tasks *WebhookTasks) deliver(ctx context.Context, delivery webhookDelivery, attempt int) error {
	var rows []webhookRow
	err := tasks.db.WithContext(ctx).Table("webhooks w").
		Select("w.id, w.url, w.secret_key, w.workspace_id, w.created_by_id").
		Joins("JOIN workspaces ws ON ws.id = w.workspace_id").
		Where("w.id = ? AND ws.slug = ? AND w.deleted_at IS NULL", delivery.webhookID, delivery.slug).
		Limit(1).Scan(&rows).Error
	if err != nil || len(rows) == 0 {
		// A webhook that is gone reaches the task's own except before anything is sent, and nothing is logged.
		return nil
	}
	webhook := rows[0]

	verb := delivery.action
	if mapped, known := webhookVerbs[delivery.action]; known {
		verb = mapped
	}
	body, err := webhookPayload(delivery, webhook, verb)
	if err != nil {
		return err
	}

	deliveryID, err := newTaskUUID()
	if err != nil {
		return err
	}
	headers := map[string]string{
		"Content-Type":     "application/json",
		"User-Agent":       "Autopilot",
		"X-Plane-Delivery": deliveryID,
		"X-Plane-Event":    delivery.event,
	}
	if webhook.SecretKey != "" {
		signature := hmac.New(sha256.New, []byte(webhook.SecretKey))
		signature.Write(body)
		headers["X-Plane-Signature"] = hex.EncodeToString(signature.Sum(nil))
	}

	response, err := httpsafe.Post(ctx, webhook.URL, tasks.settings, headers, body, webhookTimeout)
	var rejected httpsafe.RejectedError
	switch {
	case err == nil:
		return tasks.log(ctx, webhook, verb, headers, body, strconv.Itoa(response.StatusCode),
			renderHeaders(response.Headers), response.Body, attempt, delivery.event)

	case errors.As(err, &rejected):
		// Not retryable and not grounds for switching the webhook off: the target was refused, which may be a transient answer from dns.
		tasks.logger.Warn("webhook url refused", "webhook", webhook.ID, "error", rejected.Reason)
		return tasks.log(ctx, webhook, verb, headers, body, "400", "",
			"Webhook URL rejected: "+rejected.Reason, attempt, delivery.event)

	default:
		logErr := tasks.log(ctx, webhook, verb, headers, body, "500", "", err.Error(), attempt, delivery.event)
		if logErr != nil {
			return logErr
		}
		if attempt >= webhookMaxRetries {
			return tasks.deactivate(ctx, webhook, delivery.currentSite, err)
		}
		tasks.retry(ctx, delivery, attempt+1)
		return nil
	}
}

// retry waits and goes again.
//
// Celery does this by publishing a new message with an eta and holding it in the worker until then; with the default acknowledgement settings that is lost if the worker stops, and so is this. The difference is where the waiting happens, not whether it survives.
func (tasks *WebhookTasks) retry(ctx context.Context, delivery webhookDelivery, attempt int) {
	delay := tasks.backoff(attempt)
	tasks.logger.Info("webhook delivery will be retried", "webhook", delivery.webhookID,
		"attempt", attempt, "in", delay.String())
	tasks.scheduler(delay, func() {
		// The request's own context is finished by the time this runs, so the retry gets a fresh one.
		if err := tasks.deliver(context.WithoutCancel(ctx), delivery, attempt); err != nil {
			tasks.logger.Error("webhook retry failed", "webhook", delivery.webhookID, "error", err)
		}
	})
}

// deactivate switches a webhook off after it has failed often enough, and tells whoever made it.
func (tasks *WebhookTasks) deactivate(ctx context.Context, webhook webhookRow, currentSite string, reason error) error {
	err := tasks.db.WithContext(ctx).Table("webhooks").
		Where("id = ?", webhook.ID).Update("is_active", false).Error
	if err != nil {
		return err
	}
	tasks.logger.Warn("webhook switched off after repeated failures", "webhook", webhook.ID, "error", reason)
	if tasks.emails == nil || webhook.CreatedByID == nil {
		return nil
	}
	return tasks.emails.PublishWebhookDeactivation(ctx, webhook.ID, *webhook.CreatedByID, currentSite, reason.Error())
}

// log keeps what was sent and what came back. Every column is text, including the status, because that is how the table is shaped.
func (tasks *WebhookTasks) log(ctx context.Context, webhook webhookRow, verb string, headers map[string]string, body []byte, status, responseHeaders, responseBody string, attempt int, event string) error {
	identifier, err := newTaskUUID()
	if err != nil {
		return err
	}
	now := tasks.clock().UTC()
	return tasks.db.WithContext(ctx).Table("webhook_logs").Create(map[string]any{
		"id": identifier, "created_at": now, "updated_at": now,
		"created_by_id": nil, "updated_by_id": nil,
		"workspace_id": webhook.WorkspaceID, "webhook": webhook.ID,
		"event_type": event, "request_method": verb,
		"request_headers": renderHeaderMap(headers), "request_body": string(body),
		"response_status": status, "response_headers": responseHeaders, "response_body": responseBody,
		"retry_count": attempt,
	}).Error
}

// webhookPayload builds the body. The two halves that came from a serializer are spliced in as they arrived, so the bytes a receiver sees carry the same key order the sender produced.
func webhookPayload(delivery webhookDelivery, webhook webhookRow, verb string) ([]byte, error) {
	data := delivery.eventData
	if len(data) == 0 {
		data = json.RawMessage("null")
	}
	activity := delivery.activity
	if len(activity) == 0 {
		activity = json.RawMessage("null")
	}
	event, err := json.Marshal(delivery.event)
	if err != nil {
		return nil, err
	}
	action, err := json.Marshal(verb)
	if err != nil {
		return nil, err
	}
	slug, err := json.Marshal(delivery.slug)
	if err != nil {
		return nil, err
	}
	// Written by hand rather than through a map, so the keys keep the order the payload is documented in.
	return []byte(fmt.Sprintf(
		`{"event": %s, "action": %s, "webhook_id": %q, "workspace_id": %q, "workspace_slug": %s, "data": %s, "activity": %s}`,
		event, action, webhook.ID, webhook.WorkspaceID, slug, data, activity)), nil
}

// renderHeaderMap is what str() of a python dict looks like, which is what the log column holds today.
func renderHeaderMap(headers map[string]string) string {
	keys := []string{"Content-Type", "User-Agent", "X-Plane-Delivery", "X-Plane-Event", "X-Plane-Signature"}
	rendered := "{"
	first := true
	for _, key := range keys {
		value, present := headers[key]
		if !present {
			continue
		}
		if !first {
			rendered += ", "
		}
		first = false
		rendered += "'" + key + "': '" + value + "'"
	}
	return rendered + "}"
}

// renderHeaders is what str() of the response's headers looks like.
func renderHeaders(headers map[string][]string) string {
	keys := make([]string, 0, len(headers))
	for key := range headers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	rendered := "{"
	for index, key := range keys {
		if index > 0 {
			rendered += ", "
		}
		rendered += "'" + key + "': '" + joinHeaderValues(headers[key]) + "'"
	}
	return rendered + "}"
}

func joinHeaderValues(values []string) string {
	joined := ""
	for index, value := range values {
		if index > 0 {
			joined += ", "
		}
		joined += value
	}
	return joined
}

// rawArgument reads a task argument back as the json it arrived as, so a payload half can be spliced in without being rebuilt.
func rawArgument(arguments []any, keywords map[string]any, index int, name string) json.RawMessage {
	value, ok := argument(arguments, keywords, index, name)
	if !ok || value == nil {
		return nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return encoded
}
