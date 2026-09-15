// Package worker runs the Go half of Plane's Celery workload.
//
// It consumes Celery protocol v2 messages from a queue of its own rather than
// the shared "celery" queue, so the Python worker keeps receiving every task
// that has not been migrated. A message whose task name is not registered here
// is rejected without requeueing and logged, which surfaces a routing mistake
// instead of silently dropping work.
package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// DefaultQueue is the queue the Go worker consumes. Publishers route only the
// migrated task names here.
const DefaultQueue = "pace-go"

// Handler runs one task. Arguments and keywords carry the Celery protocol v2
// body, so a task can be invoked either way, exactly as Django calls it.
type Handler func(ctx context.Context, arguments []any, keywords map[string]any) error

type Consumer struct {
	brokerURL string
	queue     string
	handlers  map[string]Handler
	logger    *slog.Logger
	// prefetch bounds how many unacknowledged messages the broker hands over.
	prefetch int
}

func NewConsumer(brokerURL, queue string, logger *slog.Logger) *Consumer {
	if queue == "" {
		queue = DefaultQueue
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Consumer{
		brokerURL: brokerURL,
		queue:     queue,
		handlers:  make(map[string]Handler),
		logger:    logger,
		prefetch:  4,
	}
}

func (consumer *Consumer) Register(taskName string, handler Handler) {
	consumer.handlers[taskName] = handler
}

// TaskNames lists what this worker can run, which the publisher side uses to
// decide what it may route to the Go queue.
func (consumer *Consumer) TaskNames() []string {
	names := make([]string, 0, len(consumer.handlers))
	for name := range consumer.handlers {
		names = append(names, name)
	}
	return names
}

// Run consumes until the context is cancelled or the connection drops. The
// caller is expected to restart it; the process supervisor handles backoff.
func (consumer *Consumer) Run(ctx context.Context) error {
	connection, err := amqp.Dial(consumer.brokerURL)
	if err != nil {
		return fmt.Errorf("connect to Celery broker: %w", err)
	}
	defer connection.Close()
	channel, err := connection.Channel()
	if err != nil {
		return fmt.Errorf("open Celery broker channel: %w", err)
	}
	defer channel.Close()
	if err := channel.Qos(consumer.prefetch, 0, false); err != nil {
		return fmt.Errorf("set broker prefetch: %w", err)
	}
	// Celery declares its queues durable; match that so a restart keeps work.
	if _, err := channel.QueueDeclare(consumer.queue, true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare queue %s: %w", consumer.queue, err)
	}
	deliveries, err := channel.ConsumeWithContext(ctx, consumer.queue, "", false, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("consume queue %s: %w", consumer.queue, err)
	}
	closed := connection.NotifyClose(make(chan *amqp.Error, 1))
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case reason := <-closed:
			if reason == nil {
				return nil
			}
			return fmt.Errorf("broker connection closed: %w", reason)
		case delivery, ok := <-deliveries:
			if !ok {
				return nil
			}
			consumer.handle(ctx, delivery)
		}
	}
}

func (consumer *Consumer) handle(ctx context.Context, delivery amqp.Delivery) {
	taskName, _ := delivery.Headers["task"].(string)
	taskID, _ := delivery.Headers["id"].(string)
	logger := consumer.logger.With("task", taskName, "task_id", taskID)

	handler, known := consumer.handlers[taskName]
	if !known {
		// Rejecting without requeue keeps a misrouted task from looping; the
		// log is the signal that routing needs fixing.
		logger.Error("unregistered task on the Go queue")
		_ = delivery.Reject(false)
		return
	}
	arguments, keywords, err := decodeBody(delivery.Body)
	if err != nil {
		logger.Error("decode task body", "error", err)
		_ = delivery.Reject(false)
		return
	}
	started := time.Now()
	if err := handler(ctx, arguments, keywords); err != nil {
		// Django's tasks swallow their own errors and return, so a failure here
		// is logged and acknowledged rather than retried, matching that.
		logger.Error("task failed", "error", err, "duration", time.Since(started))
		_ = delivery.Ack(false)
		return
	}
	logger.Info("task completed", "duration", time.Since(started))
	_ = delivery.Ack(false)
}

// decodeBody reads Celery's protocol v2 body: [args, kwargs, embed].
func decodeBody(body []byte) ([]any, map[string]any, error) {
	var envelope []json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, nil, fmt.Errorf("task body is not a protocol v2 envelope: %w", err)
	}
	if len(envelope) < 2 {
		return nil, nil, errors.New("task body is missing its arguments or keywords")
	}
	var arguments []any
	if err := json.Unmarshal(envelope[0], &arguments); err != nil {
		return nil, nil, fmt.Errorf("decode task arguments: %w", err)
	}
	keywords := map[string]any{}
	if err := json.Unmarshal(envelope[1], &keywords); err != nil {
		return nil, nil, fmt.Errorf("decode task keywords: %w", err)
	}
	return arguments, keywords, nil
}

// argument reads a positional argument, falling back to the keyword of the same
// name, so a task behaves the same however Django called it.
func argument(arguments []any, keywords map[string]any, index int, name string) (any, bool) {
	if index < len(arguments) {
		return arguments[index], true
	}
	value, ok := keywords[name]
	return value, ok
}

func stringArgument(arguments []any, keywords map[string]any, index int, name string) string {
	value, ok := argument(arguments, keywords, index, name)
	if !ok || value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	return fmt.Sprint(value)
}
