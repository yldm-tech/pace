// Package externalapi serves plane.api, the key-authenticated REST surface that
// integrations call. It is a different app from the session-authenticated one:
// a different authentication scheme, a different rate limit, a different error
// vocabulary and a different serializer set.
package externalapi

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api/internal/auth"
	"gorm.io/gorm"
)

// APIToken is the db.APIToken table: a key an integration presents instead of a session.
type APIToken struct {
	ID               string     `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt        time.Time  `gorm:"column:created_at"`
	UpdatedAt        time.Time  `gorm:"column:updated_at"`
	DeletedAt        *time.Time `gorm:"column:deleted_at"`
	Label            string     `gorm:"column:label"`
	Description      string     `gorm:"column:description"`
	IsActive         bool       `gorm:"column:is_active"`
	LastUsed         *time.Time `gorm:"column:last_used"`
	Token            string     `gorm:"column:token"`
	UserID           string     `gorm:"column:user_id;type:uuid"`
	UserType         int        `gorm:"column:user_type"`
	WorkspaceID      *string    `gorm:"column:workspace_id;type:uuid"`
	ExpiredAt        *time.Time `gorm:"column:expired_at"`
	IsService        bool       `gorm:"column:is_service"`
	AllowedRateLimit string     `gorm:"column:allowed_rate_limit"`
}

func (APIToken) TableName() string { return "api_tokens" }

// apiKeyHeader is the header the key is read from. It is not Authorization: the external API has its own.
const apiKeyHeader = "X-Api-Key"

// authenticated wraps a handler in the key check.
//
// A request with **no** key at all is not refused by the authenticator — it returns nothing and lets the permission class answer, which is still a 401 but with DRF's own wording rather than the authenticator's. A key that is present and wrong gets the authenticator's message instead. Two ways to be unauthenticated, two bodies.
func (handler *Handler) authenticated(next func(*gin.Context, *auth.User, APIToken)) gin.HandlerFunc {
	return func(c *gin.Context) {
		key := c.GetHeader(apiKeyHeader)
		if strings.TrimSpace(key) == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"detail": "Authentication credentials were not provided.",
			})
			return
		}
		token, user, err := handler.tokenFor(c, key)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"detail": "Given API token is not valid"})
			return
		}
		if err != nil {
			handler.serverError(c, err)
			return
		}
		if !handler.allowRequest(c, key, token) {
			return
		}
		// The last-used stamp is written on every authenticated call, which makes every request a write even when the route only reads.
		now := handler.clock().UTC()
		err = handler.db.WithContext(c.Request.Context()).Model(&APIToken{}).
			Where("id = ?", token.ID).Update("last_used", now).Error
		if err != nil {
			handler.serverError(c, err)
			return
		}
		next(c, user, token)
	}
}

// tokenFor reads the key's row and the person behind it, refusing one that has expired, been switched off, or belongs to a deactivated account.
func (handler *Handler) tokenFor(c *gin.Context, key string) (APIToken, *auth.User, error) {
	var token APIToken
	now := handler.clock().UTC()
	err := handler.db.WithContext(c.Request.Context()).Table("api_tokens t").Select("t.*").
		Joins("JOIN users u ON u.id = t.user_id AND u.is_active = TRUE").
		Where("t.token = ? AND t.is_active = TRUE AND t.deleted_at IS NULL", key).
		Where("t.expired_at > ? OR t.expired_at IS NULL", now).
		Take(&token).Error
	if err != nil {
		return APIToken{}, nil, err
	}
	var user auth.User
	if err := handler.db.WithContext(c.Request.Context()).Where("id = ?", token.UserID).Take(&user).Error; err != nil {
		return APIToken{}, nil, err
	}
	return token, &user, nil
}
