// Package space serves the published half of a project: what a person sees who has a link and no account.
//
// Everything here is reached through an **anchor** rather than through a session. The anchor is the whole of the credential, so the first thing every route does is turn one into a deploy board and refuse the request when it cannot.
package space

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Handler serves plane.space.
type Handler struct {
	db  *gorm.DB
	now func() time.Time
}

func NewHandler(database *gorm.DB) *Handler {
	return &Handler{db: database}
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
