// Command worker runs the Go Celery worker.
//
// It consumes only the queue named by PACE_WORKER_QUEUE, which the Go API
// publishes the migrated task names to. The Python worker keeps consuming the
// shared "celery" queue, so the two run side by side without competing.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
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
	db := connection.GORM

	repository := auth.NewGORMRepository(db, settings.Auth.SkipEnvironmentConfig, settings.Auth.SecretKey)
	tasks := worker.NewEmailTasks(db, worker.EmailSettings{
		Host:     os.Getenv("EMAIL_HOST"),
		User:     os.Getenv("EMAIL_HOST_USER"),
		Password: os.Getenv("EMAIL_HOST_PASSWORD"),
		Port:     envOrDefault("EMAIL_PORT", "587"),
		UseTLS:   envOrDefault("EMAIL_USE_TLS", "1"),
		UseSSL:   envOrDefault("EMAIL_USE_SSL", "0"),
		From:     envOrDefault("EMAIL_FROM", "Team Plane <team@mailer.plane.so>"),
	}, repository, nil, logger)

	consumer := worker.NewConsumer(settings.Auth.AMQPURL, os.Getenv("PACE_WORKER_QUEUE"), logger)
	tasks.Register(consumer)
	logger.Info("worker starting", "tasks", strings.Join(consumer.TaskNames(), ","))

	for {
		err := consumer.Run(ctx)
		if ctx.Err() != nil {
			logger.Info("worker stopped")
			return
		}
		logger.Error("consumer stopped, reconnecting", "error", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}

func envOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
