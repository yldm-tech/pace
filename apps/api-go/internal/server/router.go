package server

import (
	"net/http"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	redis "github.com/redis/go-redis/v9"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"github.com/yldm-tech/pace/apps/api-go/internal/drf"
	"github.com/yldm-tech/pace/apps/api-go/internal/externalapi"
	projectapi "github.com/yldm-tech/pace/apps/api-go/internal/project"
	spaceapi "github.com/yldm-tech/pace/apps/api-go/internal/space"
	"github.com/yldm-tech/pace/apps/api-go/internal/storage"
	userapi "github.com/yldm-tech/pace/apps/api-go/internal/user"
	workspaceapi "github.com/yldm-tech/pace/apps/api-go/internal/workspace"
	"gorm.io/gorm"
)

const Version = "0.1.0"

type Dependencies struct {
	Database                  *gorm.DB
	CORSOrigins               []string
	AuthSettings              *auth.Settings
	AuthSkipEnvironmentConfig bool
	AuthMagicStore            auth.MagicStore
	AuthTaskPublisher         auth.TaskPublisher
	AuthRateLimiter           auth.RateLimiter
	AuthRedis                 redis.UniversalClient
	AuthAvatarStore           auth.AvatarStore
}

func NewRouter(dependencies Dependencies) *gin.Engine {
	router := gin.New()
	router.Use(gin.Logger(), gin.Recovery())
	router.Use(cors.New(cors.Config{
		AllowOrigins:     dependencies.CORSOrigins,
		AllowMethods:     []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions},
		AllowHeaders:     []string{"Accept", "Authorization", "Content-Type", "Origin", "X-API-Key", "X-CSRFToken"},
		AllowCredentials: true,
	}))
	// Every request made with an api key is recorded, the same way Django's logging middleware records one. It runs before anything else so a request refused by authentication is recorded too.
	if publisher, ok := dependencies.AuthTaskPublisher.(*auth.CeleryPublisher); ok && dependencies.AuthSettings != nil {
		router.Use(apiActivityLog(publisher, dependencies.AuthSettings.SecretKey))
	}

	router.GET("/", func(c *gin.Context) {
		drf.Respond(c, http.StatusOK, gin.H{"name": "pace-api", "status": "ok", "health": "/api/health", "version": "/api/version"})
	})
	router.GET("/api/health", func(c *gin.Context) {
		drf.Respond(c, http.StatusOK, gin.H{"status": "ok"})
	})
	router.GET("/api/version", func(c *gin.Context) {
		drf.Respond(c, http.StatusOK, gin.H{"name": "pace-api", "runtime": "go", "version": Version})
	})
	router.GET("/api/health/db", databaseHealth(dependencies.Database, "unavailable"))
	router.GET("/ready", databaseHealth(dependencies.Database, "not_ready"))
	router.GET("/metrics", func(c *gin.Context) {
		c.Data(http.StatusOK, "text/plain; version=0.0.4", []byte("# HELP pace_api_up API availability\n# TYPE pace_api_up gauge\npace_api_up 1\n"))
	})
	if dependencies.Database != nil && dependencies.AuthSettings != nil {
		repository := auth.NewGORMRepository(
			dependencies.Database,
			dependencies.AuthSkipEnvironmentConfig,
			dependencies.AuthSettings.SecretKey,
		)
		repository.SetCacheInvalidator(auth.NewRedisCacheInvalidator(dependencies.AuthRedis))
		repository.SetAvatarStore(dependencies.AuthAvatarStore)
		sessions, err := auth.NewSessionManager(
			auth.NewGORMSessionRepository(dependencies.Database),
			repository,
			*dependencies.AuthSettings,
		)
		if err != nil {
			panic(err)
		}
		options := make([]auth.HandlerOption, 0, 2)
		if dependencies.AuthMagicStore != nil && dependencies.AuthTaskPublisher != nil {
			options = append(options, auth.WithMagic(dependencies.AuthMagicStore, dependencies.AuthTaskPublisher))
		}
		if dependencies.AuthRateLimiter != nil {
			options = append(options, auth.WithRateLimiter(dependencies.AuthRateLimiter))
		}
		if dependencies.AuthRedis != nil {
			options = append(options, auth.WithCacheInvalidator(auth.NewRedisCacheInvalidator(dependencies.AuthRedis)))
		}
		auth.NewHandler(repository, sessions, *dependencies.AuthSettings, options...).Register(router)
		userHandler := userapi.NewHandler(dependencies.Database, sessions, repository, userapi.Settings{
			AppBaseURL:    dependencies.AuthSettings.AppBaseURL,
			FileSizeLimit: dependencies.AuthSettings.FileSizeLimit,
		})
		if dependencies.AuthRedis != nil {
			userHandler.SetRedis(dependencies.AuthRedis)
		}
		if publisher, ok := dependencies.AuthTaskPublisher.(*auth.CeleryPublisher); ok {
			userHandler.SetTasks(publisher)
		}
		userHandler.Register(router)
		// A misconfigured bucket leaves the store nil, and every route that signs against it answers 500 rather than reserving a row nothing can upload against.
		attachmentStore, err := storage.New(storage.Settings{
			AccessKey:        dependencies.AuthSettings.AWSAccessKeyID,
			SecretKey:        dependencies.AuthSettings.AWSSecretAccessKey,
			Region:           dependencies.AuthSettings.AWSRegion,
			Bucket:           dependencies.AuthSettings.AWSBucketName,
			Endpoint:         dependencies.AuthSettings.AWSEndpointURL,
			UseMinio:         dependencies.AuthSettings.UseMinio,
			MinioEndpointSSL: dependencies.AuthSettings.MinioEndpointSSL,
			SignedURLExpiry:  dependencies.AuthSettings.SignedURLExpiration,
		})
		workspaceHandler := workspaceapi.NewHandler(dependencies.Database, sessions, repository, workspaceapi.Settings{
			SecretKey:             dependencies.AuthSettings.SecretKey,
			AppBaseURL:            dependencies.AuthSettings.AppBaseURL,
			SkipEnvironmentConfig: dependencies.AuthSkipEnvironmentConfig,
			FileSizeLimit:         dependencies.AuthSettings.FileSizeLimit,
		})
		if err == nil {
			workspaceHandler.SetStorage(attachmentStore)
			userHandler.SetStorage(attachmentStore)
		}
		if publisher, ok := dependencies.AuthTaskPublisher.(*auth.CeleryPublisher); ok {
			workspaceHandler.SetTasks(publisher)
		}
		if dependencies.AuthRedis != nil {
			workspaceHandler.SetCache(auth.NewRedisCacheInvalidator(dependencies.AuthRedis))
		}
		workspaceHandler.Register(router)
		projectHandler := projectapi.NewHandler(dependencies.Database, sessions, projectapi.Settings{
			AppBaseURL:               dependencies.AuthSettings.AppBaseURL,
			WebURL:                   dependencies.AuthSettings.WebURL,
			FileSizeLimit:            dependencies.AuthSettings.FileSizeLimit,
			WebhookAllowedIPs:        dependencies.AuthSettings.WebhookAllowedIPs,
			WebhookAllowedHosts:      dependencies.AuthSettings.WebhookAllowedHosts,
			WebhookDisallowedDomains: dependencies.AuthSettings.WebhookDisallowedDomains,
		})
		if err == nil {
			projectHandler.SetStorage(attachmentStore)
		}
		if publisher, ok := dependencies.AuthTaskPublisher.(*auth.CeleryPublisher); ok {
			projectHandler.SetTasks(publisher)
		}
		if dependencies.AuthRedis != nil {
			projectHandler.SetCache(auth.NewRedisCacheInvalidator(dependencies.AuthRedis))
		}
		projectHandler.Register(router)
		spaceHandler := spaceapi.NewHandler(dependencies.Database)
		spaceHandler.SetSessions(sessions)
		spaceHandler.SetFileSizeLimit(dependencies.AuthSettings.FileSizeLimit)
		if err == nil {
			spaceHandler.SetStorage(attachmentStore)
		}
		spaceHandler.Register(router)
		externalHandler := externalapi.NewHandler(dependencies.Database, externalapi.Settings{
			RateLimit:     dependencies.AuthSettings.APIKeyRateLimit,
			FileSizeLimit: dependencies.AuthSettings.FileSizeLimit,
		})
		// The two APIs share one bucket, so they share one store.
		if err == nil {
			externalHandler.SetAssets(attachmentStore)
		}
		if publisher, ok := dependencies.AuthTaskPublisher.(*auth.CeleryPublisher); ok {
			externalHandler.SetTasks(publisher)
		}
		externalHandler.Register(router)
	}
	return router
}

func databaseHealth(db *gorm.DB, failureStatus string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if db == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": failureStatus})
			return
		}
		sqlDB, err := db.DB()
		if err != nil || sqlDB.PingContext(c.Request.Context()) != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": failureStatus})
			return
		}
		drf.Respond(c, http.StatusOK, gin.H{"status": "ok"})
	}
}
