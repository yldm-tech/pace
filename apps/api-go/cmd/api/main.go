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
	"github.com/yldm-tech/pace/apps/api-go/internal/server"
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

	authSettings := auth.Settings{
		SecretKey: cfg.Auth.SecretKey, SecretKeyFallbacks: cfg.Auth.SecretKeyFallbacks,
		WebURL: cfg.Auth.WebURL, AppBaseURL: cfg.Auth.AppBaseURL, SpaceBaseURL: cfg.Auth.SpaceBaseURL,
		SpaceBasePath: cfg.Auth.SpaceBasePath, SessionCookieName: cfg.Auth.SessionCookieName,
		SessionCookieDomain: cfg.Auth.SessionCookieDomain, SessionCookieSecure: cfg.Auth.SessionCookieSecure,
		SessionCookieAge: cfg.Auth.SessionCookieAge, SessionSaveEveryRequest: cfg.Auth.SessionSaveEveryRequest,
		CSRFCookieName: cfg.Auth.CSRFCookieName, CSRFCookieDomain: cfg.Auth.CSRFCookieDomain,
		CSRFCookieSecure: cfg.Auth.CSRFCookieSecure, CSRFCookieAge: cfg.Auth.CSRFCookieAge,
		CSRFTrustedOrigins: cfg.Auth.CSRFTrustedOrigins, AuthenticationRateLimit: cfg.Auth.AuthenticationRateLimit,
		Environment: authenticationEnvironment(),
	}
	httpServer := &http.Server{
		Addr: cfg.Address,
		Handler: server.NewRouter(server.Dependencies{
			Database: connection.GORM, CORSOrigins: cfg.CORSOrigins, AuthSettings: &authSettings,
			AuthSkipEnvironmentConfig: cfg.Auth.SkipEnvironmentConfig,
			AuthRedis:                 redisClient,
			AuthMagicStore:            auth.NewRedisMagicStore(redisClient), AuthTaskPublisher: auth.NewCeleryPublisher(cfg.Auth.AMQPURL),
			AuthRateLimiter: authLimiter,
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
