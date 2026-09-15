package server

import (
	"net/http"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const Version = "0.1.0"

type Dependencies struct {
	Database    *gorm.DB
	CORSOrigins []string
}

func NewRouter(dependencies Dependencies) *gin.Engine {
	router := gin.New()
	router.Use(gin.Logger(), gin.Recovery())
	router.Use(cors.New(cors.Config{
		AllowOrigins:     dependencies.CORSOrigins,
		AllowMethods:     []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		AllowCredentials: true,
	}))

	router.GET("/", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"name": "pace-api", "status": "ok", "health": "/api/health", "version": "/api/version"})
	})
	router.GET("/api/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	router.GET("/api/version", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"name": "pace-api", "runtime": "go", "version": Version})
	})
	router.GET("/api/health/db", databaseHealth(dependencies.Database, "unavailable"))
	router.GET("/ready", databaseHealth(dependencies.Database, "not_ready"))
	router.GET("/metrics", func(c *gin.Context) {
		c.Data(http.StatusOK, "text/plain; version=0.0.4", []byte("# HELP pace_api_up API availability\n# TYPE pace_api_up gauge\npace_api_up 1\n"))
	})
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
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	}
}
