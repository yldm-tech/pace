package beat

import (
	"context"
	"log/slog"
	"time"
)

// Publisher sends a task to the broker. The API's Celery publisher satisfies
// this, so the beat reuses the same queue routing: a task the Go worker
// implements goes to the Go queue, everything else to the Python one.
type Publisher interface {
	PublishRaw(ctx context.Context, taskName string, arguments []any, keywords map[string]any) error
}

// Scheduler is the Go replacement for celery beat running django_celery_beat's
// DatabaseScheduler. It must not run alongside the Python beat, or every
// periodic task would be queued twice.
type Scheduler struct {
	store     *Store
	publisher Publisher
	logger    *slog.Logger
	clock     func() time.Time
	// tick is how often the schedule is re-read and evaluated. Celery's beat
	// sleeps until the next due entry; polling at a fixed interval is simpler
	// and, at one minute granularity, indistinguishable.
	tick time.Duration
}

func NewScheduler(store *Store, publisher Publisher, logger *slog.Logger) *Scheduler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Scheduler{store: store, publisher: publisher, logger: logger, clock: time.Now, tick: 5 * time.Second}
}

// Run evaluates the schedule until the context is cancelled.
func (scheduler *Scheduler) Run(ctx context.Context) error {
	var lastChange time.Time
	var reported bool
	ticker := time.NewTicker(scheduler.tick)
	defer ticker.Stop()
	for {
		changed, err := scheduler.store.LastChange(ctx)
		if err != nil {
			scheduler.logger.Error("read the schedule change marker", "error", err)
		} else if changed.After(lastChange) {
			lastChange = changed
			scheduler.logger.Info("schedule changed, reloading", "last_update", changed)
			reported = false
		}
		if err := scheduler.runDue(ctx, !reported); err != nil {
			scheduler.logger.Error("evaluate the schedule", "error", err)
		}
		reported = true
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// runDue publishes every entry whose time has come and records the run.
func (scheduler *Scheduler) runDue(ctx context.Context, reportUnsupported bool) error {
	entries, unsupported, err := scheduler.store.Entries(ctx)
	if err != nil {
		return err
	}
	if reportUnsupported {
		for _, reason := range unsupported {
			// Logged rather than skipped quietly: a schedule nobody runs is
			// worse than a noisy log.
			scheduler.logger.Error("periodic task cannot be scheduled", "reason", reason)
		}
	}
	now := scheduler.clock().UTC()
	for _, entry := range entries {
		if !entry.due(now) {
			continue
		}
		arguments, keywords := decodedArgs(entry)
		if err := scheduler.publisher.PublishRaw(ctx, entry.Task, arguments, keywords); err != nil {
			scheduler.logger.Error("publish a periodic task", "task", entry.Task, "name", entry.Name, "error", err)
			continue
		}
		if err := scheduler.store.MarkRun(ctx, entry, now); err != nil {
			// The task is already queued, so this only risks a duplicate on the
			// next tick; log it rather than dropping the run.
			scheduler.logger.Error("record a periodic task run", "name", entry.Name, "error", err)
			continue
		}
		scheduler.logger.Info("queued a periodic task", "task", entry.Task, "name", entry.Name)
	}
	return nil
}
