package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"github.com/yldm-tech/pace/apps/api-go/internal/config"
	"github.com/yldm-tech/pace/apps/api-go/internal/database"
	"github.com/yldm-tech/pace/apps/api-go/internal/instances"
	"github.com/yldm-tech/pace/apps/api-go/internal/server"
	"github.com/yldm-tech/pace/apps/api-go/internal/worker"
	"gorm.io/gorm"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	rootContext, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	connection, err := database.Open(rootContext, cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer connection.SQL.Close()
	redisClient, err := auth.OpenRedis(rootContext, cfg.Auth.RedisURL)
	if err != nil {
		log.Fatal(err)
	}
	defer redisClient.Close()
	authLimiter, err := auth.NewRedisRateLimiter(redisClient, cfg.Auth.AuthenticationRateLimit)
	if err != nil {
		log.Fatal(err)
	}
	avatarStore, err := auth.NewS3AvatarStore(rootContext, auth.AvatarStoreSettings{
		AccessKey: cfg.Auth.AWSAccessKeyID, SecretKey: cfg.Auth.AWSSecretAccessKey,
		Region: cfg.Auth.AWSRegion, Bucket: cfg.Auth.AWSBucketName, Endpoint: cfg.Auth.AWSEndpointURL,
		UseMinio: cfg.Auth.UseMinio, MinioEndpointSSL: cfg.Auth.MinioEndpointSSL, MaxSize: cfg.Auth.FileSizeLimit,
	})
	if err != nil {
		log.Printf("OAuth avatar storage unavailable: %v", err)
	}

	authSettings := auth.Settings{
		SecretKey: cfg.Auth.SecretKey, SecretKeyFallbacks: cfg.Auth.SecretKeyFallbacks,
		WebURL: cfg.Auth.WebURL, AppBaseURL: cfg.Auth.AppBaseURL, SpaceBaseURL: cfg.Auth.SpaceBaseURL,
		SpaceBasePath: cfg.Auth.SpaceBasePath, SessionCookieName: cfg.Auth.SessionCookieName,
		SessionCookieDomain: cfg.Auth.SessionCookieDomain, SessionCookieSecure: cfg.Auth.SessionCookieSecure,
		SessionCookieAge: cfg.Auth.SessionCookieAge, SessionSaveEveryRequest: cfg.Auth.SessionSaveEveryRequest,
		CSRFCookieName: cfg.Auth.CSRFCookieName, CSRFCookieDomain: cfg.Auth.CSRFCookieDomain,
		CSRFCookieSecure: cfg.Auth.CSRFCookieSecure, CSRFCookieAge: cfg.Auth.CSRFCookieAge,
		CSRFTrustedOrigins: cfg.Auth.CSRFTrustedOrigins, AuthenticationRateLimit: cfg.Auth.AuthenticationRateLimit,
		Environment:    authenticationEnvironment(),
		AWSAccessKeyID: cfg.Auth.AWSAccessKeyID, AWSSecretAccessKey: cfg.Auth.AWSSecretAccessKey,
		AWSRegion: cfg.Auth.AWSRegion, AWSBucketName: cfg.Auth.AWSBucketName, AWSEndpointURL: cfg.Auth.AWSEndpointURL,
		UseMinio: cfg.Auth.UseMinio, MinioEndpointSSL: cfg.Auth.MinioEndpointSSL, FileSizeLimit: cfg.Auth.FileSizeLimit,
		SignedURLExpiration: cfg.Auth.SignedURLExpiration,
		WebhookAllowedIPs:   cfg.Auth.WebhookAllowedIPs, WebhookAllowedHosts: cfg.Auth.WebhookAllowedHosts,
		WebhookDisallowedDomains: cfg.Auth.WebhookDisallowedDomains,
		APIKeyRateLimit:          cfg.Auth.APIKeyRateLimit,
	}
	// Tasks the Go worker implements go to its own queue; everything else keeps
	// going to the queue the Python worker consumes. Leaving PACE_WORKER_QUEUE
	// unset routes everything to Python, which is the rollback path.
	taskPublisher := auth.NewCeleryPublisher(cfg.Auth.AMQPURL)
	taskPublisher.RouteToGoWorker(os.Getenv("PACE_WORKER_QUEUE"), worker.MigratedTaskNames())

	httpServer := &http.Server{
		Addr: cfg.Address,
		Handler: server.NewRouter(server.Dependencies{
			Database: connection.GORM, CORSOrigins: cfg.CORSOrigins, AuthSettings: &authSettings,
			AuthSkipEnvironmentConfig: cfg.Auth.SkipEnvironmentConfig,
			AuthRedis:                 redisClient,
			AuthAvatarStore:           avatarStore,
			AuthMagicStore:            auth.NewRedisMagicStore(redisClient), AuthTaskPublisher: taskPublisher,
			AuthRateLimiter: authLimiter,
			InstanceSettings: server.InstanceSettings{
				AdminBaseURL:         cfg.Auth.AdminBaseURL,
				AdminBasePath:        cfg.Auth.AdminBasePath,
				InstanceChangelogURL: cfg.Auth.InstanceChangelogURL,
				IsSelfManaged:        cfg.Auth.IsSelfManaged,
			},
			InstanceMailer: instanceMailer(connection.GORM, cfg),
			LLMBaseURL:     os.Getenv("LLM_BASE_URL"),
		}),
		ReadHeaderTimeout: 10 * time.Second,
	}

	serverErrors := make(chan error, 1)
	go func() {
		log.Printf("Pace Go API listening on %s", cfg.Address)
		serverErrors <- httpServer.ListenAndServe()
	}()

	select {
	case <-rootContext.Done():
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Printf("HTTP server stopped unexpectedly: %v", err)
		}
	}

	shutdownContext, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := httpServer.Shutdown(shutdownContext); err != nil {
		log.Printf("graceful shutdown failed: %v", err)
	}
}

func authenticationEnvironment() map[string]string {
	values := make(map[string]string)
	for _, key := range []string{
		"ENABLE_EMAIL_PASSWORD", "ENABLE_SIGNUP", "EMAIL_HOST", "ENABLE_MAGIC_LINK_LOGIN",
		"GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET", "ENABLE_GOOGLE_SYNC",
		"GITHUB_CLIENT_ID", "GITHUB_CLIENT_SECRET", "GITHUB_ORGANIZATION_ID", "ENABLE_GITHUB_SYNC",
		"GITLAB_CLIENT_ID", "GITLAB_CLIENT_SECRET", "GITLAB_HOST", "ENABLE_GITLAB_SYNC",
		"GITEA_CLIENT_ID", "GITEA_CLIENT_SECRET", "GITEA_HOST", "ENABLE_GITEA_SYNC",
	} {
		if value, exists := os.LookupEnv(key); exists {
			values[key] = value
		}
	}
	return values
}

// instanceMailer is how the admin console's credential check sends its one message: the same settings and the same client the worker's emails use.
func instanceMailer(db *gorm.DB, cfg config.Config) instances.Mailer {
	return &consoleMailer{
		reader: auth.NewGORMRepository(db, cfg.Auth.SkipEnvironmentConfig, cfg.Auth.SecretKey),
		defaults: worker.EmailSettings{
			Host:     os.Getenv("EMAIL_HOST"),
			User:     os.Getenv("EMAIL_HOST_USER"),
			Password: os.Getenv("EMAIL_HOST_PASSWORD"),
			Port:     envOrDefault("EMAIL_PORT", "587"),
			UseTLS:   envOrDefault("EMAIL_USE_TLS", "1"),
			UseSSL:   envOrDefault("EMAIL_USE_SSL", "0"),
			From:     envOrDefault("EMAIL_FROM", "Team Plane <team@mailer.plane.so>"),
		},
	}
}

// consoleMailer sends a plain text message with the instance's own mail settings.
type consoleMailer struct {
	reader   worker.ConfigurationReader
	defaults worker.EmailSettings
}

func (mailer *consoleMailer) Send(ctx context.Context, to, subject, text string) error {
	return worker.SendPlainEmail(ctx, mailer.defaults, mailer.reader, worker.SMTPMailer{}, to, subject, text)
}

func envOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
