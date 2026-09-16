package live

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// Server is the live service's HTTP surface. Everything it serves lives under the configured base path, which is "/live" unless the deployment says otherwise.
type Server struct {
	config Config
	api    *APIClient
	hub    *Hub
	relay  *Relay
	logger *slog.Logger
	engine *gin.Engine
	http   *http.Server
}

// NewServer wires the router. It does not listen; Run does that.
func NewServer(config Config, logger *slog.Logger) *Server {
	api := NewAPIClient(config.APIBaseURL)
	hub := NewHub(api, logger)
	server := &Server{
		config: config,
		api:    api,
		hub:    hub,
		logger: logger,
	}
	// A deployment with no Redis runs as a single node, and two people on one page then have to reach the same server for it to converge.
	relay, err := NewRelay(config.RedisAddress(), hub, logger)
	if err != nil {
		logger.Error("could not reach redis, running as a single node", "error", err)
	}
	hub.AttachRelay(relay)
	server.relay = relay
	server.engine = server.newRouter()
	return server
}

func (s *Server) newRouter() *gin.Engine {
	router := gin.New()
	// Express matches a route with or without its trailing slash and answers both the same way, where Gin would redirect. Every route is therefore registered under both spellings and the redirect is turned off.
	router.RedirectTrailingSlash = false
	router.Use(gin.Recovery())
	router.Use(securityHeaders())
	router.Use(crossOriginHeaders(s.config.CORSAllowedOrigins))

	group := router.Group(s.config.BasePath)
	s.registerRoutes(group)

	router.NoRoute(func(c *gin.Context) {
		c.JSON(http.StatusNotFound, gin.H{"message": "Not Found"})
	})
	return router
}

// registerRoutes mounts every route under the base path, each at both spellings of its trailing slash.
func (s *Server) registerRoutes(group *gin.RouterGroup) {
	get := func(path string, handler gin.HandlerFunc) {
		group.GET(path, handler)
		group.GET(strings.TrimSuffix(path, "/"), handler)
	}
	post := func(path string, handler gin.HandlerFunc) {
		group.POST(path, handler)
		group.POST(strings.TrimSuffix(path, "/"), handler)
	}
	get("/health/", s.health)
	get("/collaboration/", s.collaboration)
	post("/convert-document/", s.convertDocument)
}

// health answers the liveness probe.
func (s *Server) health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":    "OK",
		"timestamp": time.Now().UTC().Format("2006-01-02T15:04:05.000Z"),
		"version":   s.config.AppVersion,
	})
}

// Handler exposes the router, for tests and for embedding.
func (s *Server) Handler() http.Handler { return s.engine }

// Run listens until the context is cancelled, then shuts down without cutting off a request that is already being served.
func (s *Server) Run(ctx context.Context) error {
	s.http = &http.Server{Addr: ":" + s.config.Port, Handler: s.engine}

	errors := make(chan error, 1)
	go func() {
		s.logger.Info("live server listening", "port", s.config.Port, "base path", s.config.BasePath)
		if err := s.http.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errors <- err
		}
		close(errors)
	}()

	select {
	case err := <-errors:
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := s.http.Shutdown(shutdownCtx)
	s.hub.Stop()
	if closeErr := s.relay.Close(); closeErr != nil {
		s.logger.Warn("could not close the relay", "error", closeErr)
	}
	return err
}
