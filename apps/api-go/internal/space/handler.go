// Package space serves the published half of a project: what a person sees who has a link and no account.
//
// Everything here is reached through an **anchor** rather than through a session. The anchor is the whole of the credential, so the first thing every route does is turn one into a deploy board and refuse the request when it cannot.
package space

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"github.com/yldm-tech/pace/apps/api-go/internal/storage"
	"gorm.io/gorm"
)

// Handler serves plane.space.
type Handler struct {
	db       *gorm.DB
	sessions *auth.SessionManager
	storage  *storage.Store
	// fileSizeLimit is settings.FILE_SIZE_LIMIT, the cap every reserved upload is clamped to.
	fileSizeLimit int64
	now           func() time.Time
}

// SetFileSizeLimit gives the handler the cap an upload is clamped to.
func (handler *Handler) SetFileSizeLimit(limit int64) { handler.fileSizeLimit = limit }

func NewHandler(database *gorm.DB) *Handler {
	return &Handler{db: database}
}

// SetSessions gives the handler the session manager the write routes need. Reading a published board needs no account; writing on one does.
func (handler *Handler) SetSessions(sessions *auth.SessionManager) { handler.sessions = sessions }

// authenticated is the session wrapper the write routes carry. The read routes carry none at all.
func (handler *Handler) authenticated(next func(*gin.Context, *auth.User)) gin.HandlerFunc {
	return func(c *gin.Context) {
		if handler.sessions == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"detail": "Authentication credentials were not provided."})
			return
		}
		user, _, err := handler.sessions.Authenticate(c.Request.Context(), c.Request, c.Writer)
		if err != nil || user == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"detail": "Authentication credentials were not provided."})
			return
		}
		// The comment serializer reports whether the caller is in the project, so who is asking has to reach the body builder.
		c.Set("space.caller", user.ID)
		next(c, user)
	}
}

// newUUID is the identifier a new row takes.
func newUUID() (string, error) {
	identifier, err := uuid.NewRandom()
	if err != nil {
		return "", err
	}
	return identifier.String(), nil
}

func (handler *Handler) clock() time.Time {
	if handler.now != nil {
		return handler.now()
	}
	return time.Now()
}

// Register mounts the space routes.
func (handler *Handler) Register(router gin.IRouter) {
	handler.registerProjectRoutes(router)
	handler.registerReactionRoutes(router)
	handler.registerCommentRoutes(router)
	handler.registerIntakeRoutes(router)
	handler.registerAssetRoutes(router)
	handler.registerIssueRoutes(router)
}

func (handler *Handler) serverError(c *gin.Context, err error) {
	slog.Error("space request failed", "path", c.Request.URL.Path, "error", err)
	c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
		"error": "Something went wrong please try again later",
	})
}

// notFound is what the base view maps a missing object to.
func notFound(c *gin.Context) {
	c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "The requested resource does not exist."})
}

// DeployBoard is the row an anchor names.
type DeployBoard struct {
	ID                 string     `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt          time.Time  `gorm:"column:created_at"`
	UpdatedAt          time.Time  `gorm:"column:updated_at"`
	CreatedByID        *string    `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID        *string    `gorm:"column:updated_by_id;type:uuid"`
	DeletedAt          *time.Time `gorm:"column:deleted_at"`
	ProjectID          *string    `gorm:"column:project_id;type:uuid"`
	WorkspaceID        string     `gorm:"column:workspace_id;type:uuid"`
	EntityIdentifier   *string    `gorm:"column:entity_identifier;type:uuid"`
	EntityName         string     `gorm:"column:entity_name"`
	Anchor             string     `gorm:"column:anchor"`
	IsCommentsEnabled  bool       `gorm:"column:is_comments_enabled"`
	IsReactionsEnabled bool       `gorm:"column:is_reactions_enabled"`
	IsVotesEnabled     bool       `gorm:"column:is_votes_enabled"`
	ViewProps          []byte     `gorm:"column:view_props;type:jsonb"`
	IsActivityEnabled  bool       `gorm:"column:is_activity_enabled"`
	IsDisabled         bool       `gorm:"column:is_disabled"`
	IntakeID           *string    `gorm:"column:intake_id;type:uuid"`
}

func (DeployBoard) TableName() string { return "deploy_boards" }

// boardByAnchor turns an anchor into the board it names.
//
// Most routes here look it up **without** asking what it is published as, so an anchor belonging to a page or a view resolves the same way a project's does and the project columns it carries are read from it regardless.
func (handler *Handler) boardByAnchor(c *gin.Context) (DeployBoard, bool, error) {
	var boards []DeployBoard
	err := handler.db.WithContext(c.Request.Context()).Table("deploy_boards").
		Where("anchor = ? AND deleted_at IS NULL", c.Param("anchor")).
		Limit(1).Scan(&boards).Error
	if err != nil || len(boards) == 0 {
		return DeployBoard{}, false, err
	}
	return boards[0], true, nil
}

// projectBoardByAnchor is the stricter lookup the settings and the metadata use, which asks that the anchor be a **project's**.
func (handler *Handler) projectBoardByAnchor(c *gin.Context) (DeployBoard, bool, error) {
	var boards []DeployBoard
	err := handler.db.WithContext(c.Request.Context()).Table("deploy_boards").
		Where("anchor = ? AND entity_name = 'project' AND deleted_at IS NULL", c.Param("anchor")).
		Limit(1).Scan(&boards).Error
	if err != nil || len(boards) == 0 {
		return DeployBoard{}, false, err
	}
	return boards[0], true, nil
}

// invalidAnchor is what the routes that look an anchor up loosely answer when there is no such board.
func invalidAnchor(c *gin.Context) {
	c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "Invalid anchor"})
}
