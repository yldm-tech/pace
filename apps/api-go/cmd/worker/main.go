// Command worker runs the Go Celery worker.
//
// It consumes only the queue named by PACE_WORKER_QUEUE, which the Go API
// publishes the migrated task names to. The Python worker keeps consuming the
// shared "celery" queue, so the two run side by side without competing.
package main

import (
	"context"
	"log/slog"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"github.com/yldm-tech/pace/apps/api-go/internal/config"
	"github.com/yldm-tech/pace/apps/api-go/internal/database"
	"github.com/yldm-tech/pace/apps/api-go/internal/httpsafe"
	"github.com/yldm-tech/pace/apps/api-go/internal/storage"
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

	maintenance := worker.NewMaintenanceTasks(db, worker.RetentionSettings{
		APIActivityLogDays: retentionDays("API_ACTIVITY_LOG_RETENTION_DAYS", 14),
		EmailLogDays:       retentionDays("EMAIL_LOG_RETENTION_DAYS", 7),
		WebhookLogDays:     retentionDays("WEBHOOK_LOG_RETENTION_DAYS", 14),
	}, logger)

	deletions, err := worker.NewDeletionTasks(db, logger)
	if err != nil {
		logger.Error("load relation graph", "error", err)
		os.Exit(1)
	}
	// settings reads this one with a bare int() and a default of 60.
	deletions.SetHardDeleteAfterDays(retentionDays("HARD_DELETE_AFTER_DAYS", worker.HardDeleteAfterDays))

	versions := worker.NewVersionTasks(db, logger)

	// A misconfigured bucket leaves the store nil, and the one task that needs it logs and returns rather than failing the delivery.
	assetStore, err := storage.New(storage.Settings{
		AccessKey:        settings.Auth.AWSAccessKeyID,
		SecretKey:        settings.Auth.AWSSecretAccessKey,
		Region:           settings.Auth.AWSRegion,
		Bucket:           settings.Auth.AWSBucketName,
		Endpoint:         settings.Auth.AWSEndpointURL,
		UseMinio:         settings.Auth.UseMinio,
		MinioEndpointSSL: settings.Auth.MinioEndpointSSL,
		SignedURLExpiry:  settings.Auth.SignedURLExpiration,
		// On MinIO the download link is signed against the web host rather than the internal one, which is what makes it followable from a browser.
		PublicEndpoint: settings.Auth.WebURL,
	})
	if err != nil {
		logger.Warn("object storage is not configured, asset metadata will be skipped", "error", err)
		assetStore = nil
	}
	// A nil *storage.Store has to be left out of the interface rather than put into it, or the task would see a non-nil interface holding nothing.
	var exportStore worker.ExportStore
	if assetStore != nil {
		exportStore = assetStore
	}
	exports := worker.NewExportTasks(db, logger, exportStore, settings.Auth.UseMinio)

	allowedIPs, _ := httpsafe.ParseAllowedIPs(os.Getenv("WEBHOOK_ALLOWED_IPS"))
	links := worker.NewLinkTasks(db, httpsafe.Settings{
		AllowedIPs:   allowedIPs,
		AllowedHosts: httpsafe.ParseAllowedHosts(os.Getenv("WEBHOOK_ALLOWED_HOSTS")),
	}, logger)

	// The automation queues issue_activity, which still runs on the Python worker; publishing it from here is how a sweep's changes still show up in a work item's history.
	activityPublisher := auth.NewCeleryPublisher(settings.Auth.AMQPURL)
	nightly := worker.NewNightlyTasks(db, assetStore, activityPublisher, logger)
	// webhook_activity, the next link in the chain, still runs on the Python worker.
	modelActivity := worker.NewModelActivityTasks(activityPublisher, logger)
	// The activity task parks the request origin in Redis for the notification emails to read, and hands the rows it wrote to notifications, which still runs on the Python worker.
	redisClient, err := auth.OpenRedis(ctx, settings.Auth.RedisURL)
	if err != nil {
		logger.Warn("redis is not reachable, the request origin will not be parked", "error", err)
		redisClient = nil
	} else {
		defer redisClient.Close()
	}
	issueActivity := worker.NewIssueActivityTasks(db, redisClient, activityPublisher, logger)
	notifications := worker.NewNotificationTasks(db, logger)
	webhooks := worker.NewWebhookTasks(db, httpsafe.Settings{
		AllowedIPs:   allowedIPs,
		AllowedHosts: httpsafe.ParseAllowedHosts(os.Getenv("WEBHOOK_ALLOWED_HOSTS")),
	}, activityPublisher, activityPublisher, logger)
	emailStack := worker.NewEmailStackTasks(db, activityPublisher, logger)
	// The environment is only the fallback; get_email_configuration reads the instance configuration rows first.
	emailDefaults := worker.EmailSettings{
		Host:     os.Getenv("EMAIL_HOST"),
		User:     os.Getenv("EMAIL_HOST_USER"),
		Password: os.Getenv("EMAIL_HOST_PASSWORD"),
		Port:     envOrDefault("EMAIL_PORT", "587"),
		UseTLS:   envOrDefault("EMAIL_USE_TLS", "1"),
		UseSSL:   envOrDefault("EMAIL_USE_SSL", "0"),
		From:     envOrDefault("EMAIL_FROM", "Team Plane <team@mailer.plane.so>"),
	}
	emailSend, err := worker.NewEmailSendTasks(db, redisClient, emailDefaults, repository, nil, logger)
	if err != nil {
		logger.Error("prepare the notification email", "error", err)
		os.Exit(1)
	}

	// A nil *storage.Store has to be left out of the interface rather than put into it.
	var copyStore worker.AssetCopyStore
	if assetStore != nil {
		copyStore = assetStore
	}
	copyAssets := worker.NewCopyAssetTasks(db, copyStore, liveURL(), logger)
	// The version backfill asks for its own next batch after a countdown, which is the one place a task here queues another with a delay.
	versionSync := worker.NewVersionSyncTasks(db, activityPublisher, logger)

	// The analytics export builds its spreadsheet out of the same chart the analytics endpoint draws, and mails it with the same settings the notification emails use.
	analyticExports := worker.NewAnalyticExportTasks(db, emailDefaults, repository, worker.SMTPMailer{}, logger)

	assets := worker.NewAssetTasks(db, assetStore, logger)
	assets.SetUnuploadedAssetDeleteDays(retentionDays("UNUPLOADED_ASSET_DELETE_DAYS", worker.DefaultUnuploadedAssetDeleteDays))

	consumer := worker.NewConsumer(settings.Auth.AMQPURL, os.Getenv("PACE_WORKER_QUEUE"), logger)
	tasks.Register(consumer)
	maintenance.Register(consumer)
	deletions.Register(consumer)
	versions.Register(consumer)
	assets.Register(consumer)
	links.Register(consumer)
	nightly.Register(consumer)
	modelActivity.Register(consumer)
	issueActivity.Register(consumer)
	notifications.Register(consumer)
	webhooks.Register(consumer)
	emailStack.Register(consumer)
	emailSend.Register(consumer)
	worker.NewAPILogTasks(db, logger).Register(consumer)
	exports.Register(consumer)
	analyticExports.Register(consumer)
	copyAssets.Register(consumer)
	versionSync.Register(consumer)
	worker.NewDummyDataTasks(db, logger).Register(consumer)
	worker.NewWorkspaceSeedTasks(db, settings.Auth.WebURL, logger).Register(consumer)
	worker.NewProjectInvitationTasks(db, emailDefaults, repository, worker.SMTPMailer{}, logger).Register(consumer)
	logger.Info("worker starting", "tasks", strings.Join(consumer.TaskNames(), ","))

	for {
		err := consumer.Run(ctx)
		if ctx.Err() != nil {
			// A task held until its eta is still running in the background; give it the chance to finish what it started.
			consumer.Wait()
			logger.Info("worker stopped")
			return
		}
		logger.Error("consumer stopped, reconnecting", "error", err)
		select {
		case <-ctx.Done():
			consumer.Wait()
			return
		case <-time.After(5 * time.Second):
		}
	}
}

// retentionDays mirrors settings._retention_days: the default is used when the
// variable is unset, unparseable, or negative. Zero is a valid window and is
// kept, because it means "everything older than now".
func retentionDays(name string, fallback int) int {
	raw, present := os.LookupEnv(name)
	if !present {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return fallback
	}
	return value
}

func envOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

// liveURL is settings.LIVE_URL: the base url joined with the base path, and nothing at all when the base url is not a url.
func liveURL() string {
	base := strings.TrimSpace(os.Getenv("LIVE_BASE_URL"))
	parsed, err := url.Parse(base)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	path := envOrDefault("LIVE_BASE_PATH", "/live/")
	joined, err := parsed.Parse(path)
	if err != nil {
		return ""
	}
	return joined.String()
}
