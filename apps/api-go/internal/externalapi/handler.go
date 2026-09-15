package externalapi

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"context"
	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api-go/internal/drf"
	"gorm.io/gorm"

	"github.com/yldm-tech/pace/apps/api-go/internal/storage"
)

// Settings is what the external API needs from the environment.
type Settings struct {
	// RateLimit is API_KEY_RATE_LIMIT, written the way DRF writes a throttle rate: a count, a slash, and a period.
	RateLimit string
	// FileSizeLimit is settings.FILE_SIZE_LIMIT, the cap every reserved upload is clamped to.
	FileSizeLimit int64
}

// Handler serves plane.api.
type Handler struct {
	db       *gorm.DB
	settings Settings
	now      func() time.Time
	throttle *throttle
	assets   *storage.Store
	tasks    TaskPublisher
}

// TaskPublisher is the slice of the Celery publisher this app needs. The metadata task still runs on the Python worker.
type TaskPublisher interface {
	PublishAssetObjectMetadata(ctx context.Context, assetID string) error
	PublishModelActivity(ctx context.Context, modelName, modelID string, requestedData any, currentInstance *string, actorID, slug, origin string) error
	PublishWebhookActivity(ctx context.Context, event, verb string, actorID, slug, currentSite, eventID string) error
	PublishCrawlLinkTitle(ctx context.Context, linkID, url string) error
	PublishIssueActivity(ctx context.Context, keywords map[string]any) error
	PublishSoftDeleteRelatedObjects(ctx context.Context, appLabel, modelName, instanceID string) error
}

func NewHandler(database *gorm.DB, settings Settings) *Handler {
	return &Handler{db: database, settings: settings, throttle: newThrottle()}
}

// SetAssets gives the handler somewhere to put uploaded bytes. A misconfigured bucket leaves it nil, and the asset routes answer 500 rather than reserving a row nothing can upload against.
func (handler *Handler) SetAssets(store *storage.Store) {
	handler.assets = store
}

// SetTasks gives the handler the queue the metadata task is published to.
func (handler *Handler) SetTasks(publisher TaskPublisher) {
	handler.tasks = publisher
}

func (handler *Handler) clock() time.Time {
	if handler.now != nil {
		return handler.now()
	}
	return time.Now()
}

// Register mounts every external API route.
func (handler *Handler) Register(router gin.IRouter) {
	router.GET("/api/v1/users/me/", handler.authenticated(handler.currentUser))
	handler.registerStateRoutes(router)
	handler.registerProjectRoutes(router)
	handler.registerMemberRoutes(router)
	handler.registerLabelRoutes(router)
	handler.registerStickyRoutes(router)
	handler.registerInviteRoutes(router)
	handler.registerIntakeRoutes(router)
	handler.registerAssetRoutes(router)
	handler.registerUserAssetRoutes(router)
	handler.registerCycleRoutes(router)
	handler.registerModuleRoutes(router)
	handler.registerProjectDetailRoutes(router)
	handler.registerIssueLinkRoutes(router)
	handler.registerIssueCommentRoutes(router)
	handler.registerIssueActivityRoutes(router)
	handler.registerIssueRelationRoutes(router)
	handler.registerIssueSearchRoutes(router)
	handler.registerIssueAttachmentRoutes(router)
	handler.registerIssueRoutes(router)
}

// serverError is the catch-all the base view maps an unrecognised failure to. Every message the external API answers with is its own: the session API's wording appears nowhere here.
func (handler *Handler) serverError(c *gin.Context, err error) {
	slog.Error("external api request failed", "path", c.Request.URL.Path, "error", err)
	c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
		"error": "Something went wrong please try again later",
	})
}

// notFound is what the base view maps a missing object to, which is not the session API's wording either.
func notFound(c *gin.Context) {
	c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "The requested resource does not exist."})
}

// invalidPayload is what an integrity failure maps to.
func invalidPayload(c *gin.Context) {
	c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "The payload is not valid"})
}

// throttle is DRF's SimpleRateThrottle over the api_key scope: a sliding window of timestamps per key, kept in memory.
//
// Django keeps the window in its cache, which is Redis in a deployment, so two processes share one budget. This keeps it per process, which is a deliberate difference while both halves run side by side: sharing the counter would mean the two APIs throttling each other.
type throttle struct {
	mutex   sync.Mutex
	history map[string][]time.Time
}

func newThrottle() *throttle {
	return &throttle{history: map[string][]time.Time{}}
}

// allowRequest applies the key's own limit, falling back to the configured one.
//
// The headers are set only when the request is **allowed**, which is upstream's doing: a refused request carries no remaining count.
func (handler *Handler) allowRequest(c *gin.Context, key string, token APIToken) bool {
	rate := token.AllowedRateLimit
	if strings.TrimSpace(rate) == "" {
		rate = handler.settings.RateLimit
	}
	count, window, ok := parseRate(rate)
	if !ok {
		// A rate nobody can parse is no limit at all, which is what DRF does with an empty one.
		return true
	}

	now := handler.clock()
	handler.throttle.mutex.Lock()
	defer handler.throttle.mutex.Unlock()

	history := handler.throttle.history["api_key:"+key]
	kept := history[:0]
	for _, moment := range history {
		if moment.After(now.Add(-window)) {
			kept = append(kept, moment)
		}
	}
	if len(kept) >= count {
		handler.throttle.history["api_key:"+key] = kept
		c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
			"detail": "Request was throttled. Expected available in " + strconv.Itoa(int(window.Seconds())) + " seconds.",
		})
		return false
	}
	kept = append(kept, now)
	handler.throttle.history["api_key:"+key] = kept

	c.Header("X-RateLimit-Remaining", strconv.Itoa(count-len(kept)))
	c.Header("X-RateLimit-Reset", strconv.FormatInt(now.Add(window).Unix(), 10))
	return true
}

// parseRate reads DRF's throttle notation: a number, a slash, and a period whose **first letter** is all that is looked at.
func parseRate(rate string) (int, time.Duration, bool) {
	parts := strings.SplitN(rate, "/", 2)
	if len(parts) != 2 {
		return 0, 0, false
	}
	count, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil || count <= 0 {
		return 0, 0, false
	}
	period := strings.TrimSpace(parts[1])
	if period == "" {
		return 0, 0, false
	}
	switch period[0] {
	case 's':
		return count, time.Second, true
	case 'm':
		return count, time.Minute, true
	case 'h':
		return count, time.Hour, true
	case 'd':
		return count, 24 * time.Hour, true
	}
	return 0, 0, false
}

// requestedFields is the fields parameter, which narrows what a serializer renders. An empty list means everything.
func requestedFields(c *gin.Context) []string {
	fields := []string{}
	for _, field := range strings.Split(c.Query("fields"), ",") {
		if field != "" {
			fields = append(fields, field)
		}
	}
	return fields
}

// narrow keeps only the fields that were asked for, and keeps everything when none were.
func narrow(data gin.H, fields []string) gin.H {
	if len(fields) == 0 {
		return data
	}
	narrowed := gin.H{}
	for _, field := range fields {
		if value, present := data[field]; present {
			narrowed[field] = value
		}
	}
	return narrowed
}

// decodeJSON reads a jsonb column into the shape a response should carry, keeping a blob's own numbers.
func decodeJSON(value []byte) any {
	return drf.DecodeJSON(value)
}
