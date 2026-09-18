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

	"github.com/yldm-tech/pace/apps/api/internal/auth"
	"github.com/yldm-tech/pace/apps/api/internal/config"
	"github.com/yldm-tech/pace/apps/api/internal/database"
	"github.com/yldm-tech/pace/apps/api/internal/instances"
	"github.com/yldm-tech/pace/apps/api/internal/server"
	"github.com/yldm-tech/pace/apps/api/internal/worker"
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
	// Where the tasks go. Every one of the forty-six is implemented here, so with nothing set they go to the Go worker's own queue — the fallback used to be the queue the Python worker consumed, and since that worker was removed nothing consumes it, so a publisher that fell back was publishing into a queue with no consumer and every background task silently never ran.
	taskPublisher := auth.NewCeleryPublisher(cfg.Auth.AMQPURL)
	taskPublisher.RouteToGoWorker(workerQueue(), worker.MigratedTaskNames())

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
		// None of these four is a load-shedding budget. A request slow enough to reach one of them is cut off mid-flight, so each is set above the slowest legitimate request of its class rather than at it; what they buy is that a client which stopped talking can no longer hold a connection, and its goroutine, open for as long as the process lives.
		//
		// ReadTimeout and WriteTimeout are the same five minutes because Go arms the write deadline when the request headers finish rather than when the handler writes, so both have to cover the same upload. The v1 asset routes still take the bytes through the API (internal/project/legacy_asset_handler.go), capped at FILE_SIZE_LIMIT -- five megabytes by default, and the proxy enforces the same cap -- and five minutes is five megabytes at about a hundred and forty kilobits a second, slower than a link that could finish the upload at all. Five minutes also sits far above the slowest handler that is not an upload: the assistant routes and the console's model listing each wait on one outbound call, capped at thirty seconds by the client that makes it. The collaboration websockets are not served by this process at all -- internal/live has its own server on its own port -- so no deadline here can cut one off.
		//
		// IdleTimeout only ever closes a keep-alive connection between requests, and three minutes is above the window the proxy in front of this reuses one for, so the proxy is the side that closes and no request is dispatched onto a connection this process is shutting.
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       5 * time.Minute,
		WriteTimeout:      5 * time.Minute,
		IdleTimeout:       3 * time.Minute,
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
			From:     envOrDefault("EMAIL_FROM", "Pace <support@yldm.ai>"),
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

// workerQueue is worker.Queue, named locally so the three commands read the same.
func workerQueue() string { return worker.Queue() }
