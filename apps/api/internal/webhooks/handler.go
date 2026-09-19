// Package webhooks serves the seven routes under `webhooks/` and `webhook-logs/`: where a workspace wants to be told about things, and which things.
//
// It is part of the session API rather than a third application — same cookie, same error vocabulary — but it is its own package because it shares nothing with the work-item surface `internal/project` owns. Its two models are its own, it answers no project-scoped path, and the three allowlists that decide which urls a workspace may be told to call are read by nothing else. The delivery side of the same chain lives in `internal/worker`; this is only the registration surface.
//
// The small kit below — authenticated, the role constants, workspaceRole, requireWorkspaceRole, invalidDetail, internalError, isUniqueViolation and newUUID — is copied from `internal/project` rather than imported from it. That is this repo's pattern for a handler package and not an oversight: `authenticated`, `internalError`, `newUUID` and `decodeJSON` each already exist five times over in internal/space, internal/workspace, internal/externalapi, internal/project and internal/user, and the alternative — exporting them from a 37k-line package — would couple the two packages together for the sake of ninety lines. Keep the copies byte-identical so a reader diffing the two sees no difference.
package webhooks

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api/internal/auth"
	"gorm.io/gorm"
)

// roleAdmin is WorkspaceMember.Role's administrator value. Every route here is admin only, so it is the only one of the three this package has a use for.
const roleAdmin = 20

type Settings struct {
	// The three webhook settings, which together decide which urls a workspace may be told to call.
	WebhookAllowedIPs        []netip.Prefix
	WebhookAllowedHosts      []string
	WebhookDisallowedDomains []string
}

type Handler struct {
	db       *gorm.DB
	sessions *auth.SessionManager
	settings Settings
	clock    func() time.Time
}

func NewHandler(db *gorm.DB, sessions *auth.SessionManager, settings Settings) *Handler {
	return &Handler{db: db, sessions: sessions, settings: settings, clock: time.Now}
}

func (handler *Handler) Register(router gin.IRouter) {
	handler.registerWebhookRoutes(router)
}

func (handler *Handler) authenticated(next func(*gin.Context, *auth.User)) gin.HandlerFunc {
	return func(c *gin.Context) {
		if handler.sessions == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"detail": "Authentication credentials were not provided."})
			return
		}
		user, _, err := handler.sessions.Authenticate(c.Request.Context(), c.Request, c.Writer)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"detail": "Authentication credentials were not provided."})
			return
		}
		next(c, user)
	}
}

func (handler *Handler) workspaceRole(ctx context.Context, slug, userID string) (int, error) {
	var role *int
	err := handler.db.WithContext(ctx).Table("workspace_members wm").
		Joins("JOIN workspaces w ON w.id = wm.workspace_id").
		Where("w.slug = ? AND wm.member_id = ? AND wm.is_active = TRUE AND wm.deleted_at IS NULL", slug, userID).
		Select("wm.role").Limit(1).Scan(&role).Error
	if err != nil {
		return 0, err
	}
	if role == nil {
		return 0, nil
	}
	return *role, nil
}

func (handler *Handler) requireWorkspaceRole(c *gin.Context, user *auth.User, allowed ...int) bool {
	role, err := handler.workspaceRole(c.Request.Context(), c.Param("slug"), user.ID)
	if err != nil {
		handler.internalError(c, err)
		return false
	}
	for _, candidate := range allowed {
		if role == candidate {
			return true
		}
	}
	c.JSON(http.StatusForbidden, gin.H{"error": "You don't have the required permissions."})
	return false
}

func (handler *Handler) invalidDetail(c *gin.Context) {
	c.JSON(http.StatusBadRequest, gin.H{"error": "Please provide valid detail"})
}

func (handler *Handler) internalError(c *gin.Context, err error) {
	if err != nil {
		c.Error(err)
	}
	c.JSON(http.StatusInternalServerError, gin.H{"error": "Something went wrong please try again later"})
}

func isUniqueViolation(err error) bool {
	lowered := strings.ToLower(err.Error())
	return strings.Contains(lowered, "duplicate key") || strings.Contains(lowered, "unique constraint")
}

func newUUID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", value[0:4], value[4:6], value[6:8], value[8:10], value[10:16]), nil
}
