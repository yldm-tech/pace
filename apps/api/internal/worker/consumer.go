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
	"hash/fnv"
	"log/slog"
	"sync"
	"sync/atomic"
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
	// prefetch bounds how many unacknowledged messages the broker hands over, and with it the number of lanes the tasks are run on: a message the broker has not handed over has nothing to run it, so more lanes than this would sit idle.
	prefetch int
	// running counts the tasks held until their eta, so a shutdown can wait for them.
	running sync.WaitGroup
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
	lanes := consumer.startLanes(ctx)
	// Closing the lanes before the channel and the connection lets the tasks still running finish and acknowledge.
	defer lanes.stop()
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
			ready, dispatch := consumer.prepare(delivery)
			if !dispatch {
				continue
			}
			if !lanes.send(ctx, ready) {
				return ctx.Err()
			}
		}
	}
}

// task is one delivery with its body already read, which happens before it is handed to a lane so the lane can be chosen by what the task works on.
type task struct {
	delivery  amqp.Delivery
	logger    *slog.Logger
	handler   Handler
	arguments []any
	keywords  map[string]any
	name      string
}

// prepare reads the delivery and says whether it is for a lane to run. A task that cannot be run at all is rejected here.
func (consumer *Consumer) prepare(delivery amqp.Delivery) (task, bool) {
	taskName, _ := delivery.Headers["task"].(string)
	taskID, _ := delivery.Headers["id"].(string)
	logger := consumer.logger.With("task", taskName, "task_id", taskID)

	handler, known := consumer.handlers[taskName]
	if !known {
		// Rejecting without requeue keeps a misrouted task from looping; the
		// log is the signal that routing needs fixing.
		logger.Error("unregistered task on the Go queue")
		_ = delivery.Reject(false)
		return task{}, false
	}
	arguments, keywords, err := decodeBody(delivery.Body)
	if err != nil {
		logger.Error("decode task body", "error", err)
		_ = delivery.Reject(false)
		return task{}, false
	}
	return task{delivery: delivery, logger: logger, handler: handler, arguments: arguments, keywords: keywords, name: taskName}, true
}

func (consumer *Consumer) handle(ctx context.Context, ready task) {
	delivery, logger, handler := ready.delivery, ready.logger, ready.handler
	arguments, keywords := ready.arguments, ready.keywords
	if wait := etaDelay(delivery.Headers["eta"], time.Now()); wait > 0 {
		// Celery holds an eta task in the worker rather than at the broker, and acknowledges it on receipt — so a restart loses it. This does the same, and waits out of the way of the messages behind it.
		logger.Info("task held until its eta", "wait", wait)
		_ = delivery.Ack(false)
		consumer.running.Add(1)
		go func() {
			defer consumer.running.Done()
			timer := time.NewTimer(wait)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
			}
			consumer.run(ctx, logger, handler, arguments, keywords)
		}()
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

// lanePool runs the tasks on a fixed set of goroutines instead of one after another on the consuming one. A lane is picked by the thing the task works on, so two tasks touching the same thing land on the same lane and keep the order the broker delivered them in — which matters because several of the handlers read a row and write it back without a lock: the version tasks fold an edit into the version before it, and the activity task appends to a work item's history. Tasks naming different things, and the nightly sweep that names nothing, now run alongside each other rather than behind whatever is at the head of the queue.
type lanePool struct {
	lanes    []chan task
	running  sync.WaitGroup
	draining atomic.Bool
}

func (consumer *Consumer) startLanes(ctx context.Context) *lanePool {
	count := consumer.prefetch
	if count < 1 {
		count = 1
	}
	pool := &lanePool{lanes: make([]chan task, count)}
	for index := range pool.lanes {
		// A lane holds as many tasks as the broker will hand over unacknowledged, so handing one over never blocks the consuming goroutine: an activity task publishes the notification for the same work item, and that notification lands on the lane its activity is still running on.
		lane := make(chan task, count)
		pool.lanes[index] = lane
		pool.running.Add(1)
		go func() {
			defer pool.running.Done()
			for ready := range lane {
				if pool.draining.Load() {
					// Run is on its way out, either because the process is stopping or because the connection dropped, so this one is handed back instead of being run against a context that is already cancelled. Requeueing is what the broker does with an unacknowledged message anyway.
					_ = ready.delivery.Nack(false, true)
					continue
				}
				consumer.handle(ctx, ready)
			}
		}()
	}
	return pool
}

// send hands a task to its lane, waiting if that lane is somehow full, which is what keeps two tasks on one thing in the order the broker delivered them.
func (pool *lanePool) send(ctx context.Context, ready task) bool {
	lane := pool.lanes[laneFor(ready, len(pool.lanes))]
	select {
	case lane <- ready:
		return true
	case <-ctx.Done():
		return false
	}
}

// stop waits for the tasks already running, so a shutdown does not cut one off mid-write, and gives back the ones that had not started.
func (pool *lanePool) stop() {
	pool.draining.Store(true)
	for _, lane := range pool.lanes {
		close(lane)
	}
	pool.running.Wait()
}

// laneKeywords are the keywords that name the thing a task works on, in the order they are looked for. The identifier rather than the task name is what a lane is keyed on, so every kind of work on one work item stays in order, not just repeats of the same task.
var laneKeywords = []string{"issue_id", "page_id", "model_id", "entity_identifier", "event_id", "webhook_id", "user_id"}

// laneFor picks the lane a task belongs to. A task that names nothing this recognises — a sweep, a backfill batch, an export — is keyed on its own name instead, so two of one kind never overlap even though different kinds do.
func laneFor(ready task, lanes int) int {
	key := ready.name
	for _, name := range laneKeywords {
		if value, ok := ready.keywords[name].(string); ok && value != "" {
			key = value
			break
		}
	}
	digest := fnv.New32a()
	_, _ = digest.Write([]byte(key))
	return int(digest.Sum32() % uint32(lanes))
}

// run is the handler call on its own, for the held tasks that have already been acknowledged.
func (consumer *Consumer) run(ctx context.Context, logger *slog.Logger, handler Handler, arguments []any, keywords map[string]any) {
	started := time.Now()
	if err := handler(ctx, arguments, keywords); err != nil {
		logger.Error("task failed", "error", err, "duration", time.Since(started))
		return
	}
	logger.Info("task completed", "duration", time.Since(started))
}

// etaDelay reads the eta header and says how long to wait. A moment already past, or a header in a shape this does not know, is no wait at all.
func etaDelay(header any, now time.Time) time.Duration {
	text, ok := header.(string)
	if !ok || text == "" {
		return 0
	}
	moment, err := time.Parse(time.RFC3339Nano, text)
	if err != nil {
		return 0
	}
	if wait := moment.Sub(now); wait > 0 {
		return wait
	}
	return 0
}

// Wait blocks until every held task has run, so a shutdown does not cut one off mid-write.
func (consumer *Consumer) Wait() {
	consumer.running.Wait()
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
