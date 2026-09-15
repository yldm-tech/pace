// Command beat runs the Go replacement for Celery beat.
//
// It must not run alongside the Python beat-worker: both would evaluate the
// same schedule and every periodic task would be queued twice. The compose file
// therefore ships this service commented out, and switching over means stopping
// beat-worker in the same change.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"github.com/yldm-tech/pace/apps/api-go/internal/beat"
	"github.com/yldm-tech/pace/apps/api-go/internal/config"
	"github.com/yldm-tech/pace/apps/api-go/internal/database"
	"github.com/yldm-tech/pace/apps/api-go/internal/worker"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	settings, err := config.Load()
	if err != nil {
		logger.Error("load configuration", "error", err)
		os.Exit(1)
	}
	connection, err := database.Open(ctx, settings)
	if err != nil {
		logger.Error("open database", "error", err)
		os.Exit(1)
	}
	defer connection.SQL.Close()

	store := beat.NewStore(connection.GORM)
	// DatabaseScheduler writes the static schedule into the tables on startup,
	// so an entry nobody has touched through the admin still exists.
	staticSchedule := beat.StaticSchedule(metricsPushInterval())
	if err := store.SyncStatic(ctx, staticSchedule, time.Now().UTC()); err != nil {
		logger.Error("sync the static schedule", "error", err)
		os.Exit(1)
	}
	logger.Info("static schedule synced", "entries", len(staticSchedule))

	publisher := auth.NewCeleryPublisher(settings.Auth.AMQPURL)
	publisher.RouteToGoWorker(os.Getenv("PACE_WORKER_QUEUE"), worker.MigratedTaskNames())

	scheduler := beat.NewScheduler(store, publisher, logger)
	logger.Info("beat starting")
	if err := scheduler.Run(ctx); err != nil && ctx.Err() == nil {
		logger.Error("beat stopped", "error", err)
		os.Exit(1)
	}
	logger.Info("beat stopped")
}

// metricsPushInterval mirrors celery.py's _get_metrics_push_interval_minutes.
func metricsPushInterval() int {
	value, err := strconv.Atoi(os.Getenv("METRICS_PUSH_INTERVAL_MINUTES"))
	if err != nil || value <= 0 || value > 10_000_000 {
		return 360
	}
	return value
}
