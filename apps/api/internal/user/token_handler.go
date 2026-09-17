package user

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/yldm-tech/pace/apps/api/internal/auth"
	"github.com/yldm-tech/pace/apps/api/internal/drf"
)

func (handler *Handler) registerTokenRoutes(router gin.IRouter) {
	const base = "/api/users/api-tokens/"
	router.GET(base, handler.authenticated(handler.tokenList))
	router.POST(base, handler.authenticated(handler.tokenCreate))
	router.GET(base+":token/", handler.authenticated(handler.tokenRetrieve))
	router.PATCH(base+":token/", handler.authenticated(handler.tokenUpdate))
	router.DELETE(base+":token/", handler.authenticated(handler.tokenDestroy))
}

// APIToken is a key somebody made for the external API.
// defaultAllowedRateLimit is the model's default for api_tokens.allowed_rate_limit.
var defaultAllowedRateLimit = "60/min"

type APIToken struct {
	ID               string     `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt        time.Time  `gorm:"column:created_at"`
	UpdatedAt        time.Time  `gorm:"column:updated_at"`
	CreatedByID      *string    `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID      *string    `gorm:"column:updated_by_id;type:uuid"`
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
	AllowedRateLimit *string    `gorm:"column:allowed_rate_limit"`
}

func (APIToken) TableName() string { return "api_tokens" }

// tokenCreate makes a key. It is the **only** route that reports the key itself: every read afterwards leaves it out, so a key not written down when it was made cannot be recovered.
//
// An unnamed key is given a label of thirty-two hexadecimal characters, the same shape as the key's own suffix.
func (handler *Handler) tokenCreate(c *gin.Context, user *auth.User) {
	var payload map[string]json.RawMessage
	_ = c.ShouldBindJSON(&payload)

	label := hexIdentifier()
	if raw, given := payload["label"]; given {
		var value string
		if json.Unmarshal(raw, &value) == nil && value != "" {
			label = value
		}
	}
	description := ""
	if raw, given := payload["description"]; given {
		_ = json.Unmarshal(raw, &description)
	}
	var expiry *time.Time
	if raw, given := payload["expired_at"]; given && string(raw) != "null" {
		var value string
		if json.Unmarshal(raw, &value) == nil && value != "" {
			parsed, ok := parseTokenExpiry(value)
			if !ok {
				c.JSON(http.StatusBadRequest, gin.H{
					"expired_at": []string{"Datetime has wrong format. Use one of these formats instead: YYYY-MM-DDThh:mm[:ss[.uuuuuu]][+HH:MM|-HH:MM|Z]."},
				})
				return
			}
			expiry = &parsed
		}
	}

	identifier, err := newUUID()
	if err != nil {
		handler.internalError(c, err)
		return
	}
	now := handler.clock().UTC()
	token := APIToken{
		ID: identifier, CreatedAt: now, UpdatedAt: now, CreatedByID: &user.ID, UpdatedByID: &user.ID,
		Label: label, Description: description, IsActive: true,
		// The key is a fixed prefix and thirty-two hexadecimal characters, which is what makes one recognisable in a log.
		Token:  "plane_api_" + hexIdentifier(),
		UserID: user.ID, ExpiredAt: expiry,
		// NOT NULL with no database default, and the field is a pointer, so leaving it out writes a null. Django's default is what the recorded migration set the column to when it added it.
		AllowedRateLimit: &defaultAllowedRateLimit,
	}
	// A bot's key is marked as one, which is what tells the external API it is not a person.
	if user.IsBot {
		token.UserType = 1
	}
	if err := handler.db.WithContext(c.Request.Context()).Create(&token).Error; err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusCreated, tokenJSON(token, true))
}

// tokenList returns the caller's own keys, without the keys themselves.
//
// A **service** key is hidden from every route here: those belong to the installation rather than to a person, and nobody reaches one through this API.
func (handler *Handler) tokenList(c *gin.Context, user *auth.User) {
	var tokens []APIToken
	err := handler.db.WithContext(c.Request.Context()).Table("api_tokens").
		Where("user_id = ? AND is_service = FALSE AND deleted_at IS NULL", user.ID).
		Order("created_at DESC").Scan(&tokens).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	results := make([]gin.H, 0, len(tokens))
	for _, token := range tokens {
		results = append(results, tokenJSON(token, false))
	}
	drf.Respond(c, http.StatusOK, results)
}

func (handler *Handler) tokenRetrieve(c *gin.Context, user *auth.User) {
	token, found, err := handler.tokenByID(c, user)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		handler.notFound(c)
		return
	}
	drf.Respond(c, http.StatusOK, tokenJSON(token, false))
}

// tokenUpdate edits a key's label, description or expiry — and answers with the **key itself**, which the reads do not.
func (handler *Handler) tokenUpdate(c *gin.Context, user *auth.User) {
	token, found, err := handler.tokenByID(c, user)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		handler.notFound(c)
		return
	}
	var payload map[string]json.RawMessage
	if err := c.ShouldBindJSON(&payload); err != nil {
		handler.invalidDetail(c)
		return
	}
	updates := map[string]any{}
	if raw, given := payload["label"]; given {
		var value string
		if json.Unmarshal(raw, &value) != nil {
			c.JSON(http.StatusBadRequest, gin.H{"label": []string{"Not a valid string."}})
			return
		}
		updates["label"] = value
		token.Label = value
	}
	if raw, given := payload["description"]; given {
		var value string
		if json.Unmarshal(raw, &value) != nil {
			c.JSON(http.StatusBadRequest, gin.H{"description": []string{"Not a valid string."}})
			return
		}
		updates["description"] = value
		token.Description = value
	}
	if raw, given := payload["is_active"]; given {
		var value bool
		if json.Unmarshal(raw, &value) != nil {
			c.JSON(http.StatusBadRequest, gin.H{"is_active": []string{"Must be a valid boolean."}})
			return
		}
		updates["is_active"] = value
		token.IsActive = value
	}
	if raw, given := payload["expired_at"]; given {
		if string(raw) == "null" {
			updates["expired_at"] = nil
			token.ExpiredAt = nil
		} else {
			var value string
			parsed, ok := time.Time{}, false
			if json.Unmarshal(raw, &value) == nil {
				parsed, ok = parseTokenExpiry(value)
			}
			if !ok {
				c.JSON(http.StatusBadRequest, gin.H{
					"expired_at": []string{"Datetime has wrong format. Use one of these formats instead: YYYY-MM-DDThh:mm[:ss[.uuuuuu]][+HH:MM|-HH:MM|Z]."},
				})
				return
			}
			updates["expired_at"] = parsed
			token.ExpiredAt = &parsed
		}
	}
	if len(updates) > 0 {
		now := handler.clock().UTC()
		updates["updated_at"] = now
		updates["updated_by_id"] = user.ID
		token.UpdatedAt = now
		err := handler.db.WithContext(c.Request.Context()).Table("api_tokens").
			Where("id = ?", token.ID).Updates(updates).Error
		if err != nil {
			handler.internalError(c, err)
			return
		}
	}
	drf.Respond(c, http.StatusOK, tokenJSON(token, true))
}

func (handler *Handler) tokenDestroy(c *gin.Context, user *auth.User) {
	token, found, err := handler.tokenByID(c, user)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		handler.notFound(c)
		return
	}
	now := handler.clock().UTC()
	err = handler.db.WithContext(c.Request.Context()).Table("api_tokens").Where("id = ?", token.ID).
		Updates(map[string]any{"deleted_at": now, "updated_at": now}).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (handler *Handler) tokenByID(c *gin.Context, user *auth.User) (APIToken, bool, error) {
	var tokens []APIToken
	err := handler.db.WithContext(c.Request.Context()).Table("api_tokens").
		Where("user_id = ? AND id = ? AND is_service = FALSE AND deleted_at IS NULL", user.ID, c.Param("token")).
		Limit(1).Scan(&tokens).Error
	if err != nil || len(tokens) == 0 {
		return APIToken{}, false, err
	}
	return tokens[0], true, nil
}

// hexIdentifier is uuid4().hex: thirty-two hexadecimal characters with no dashes.
func hexIdentifier() string {
	value, err := uuid.NewRandom()
	if err != nil {
		return ""
	}
	return strings.ReplaceAll(value.String(), "-", "")
}

// parseTokenExpiry reads what DRF's DateTimeField reads, which takes a day on its own as midnight.
func parseTokenExpiry(value string) (time.Time, bool) {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

// tokenJSON is APITokenSerializer when the key is shown and APITokenReadSerializer when it is not — the same seventeen fields but one.
func tokenJSON(token APIToken, withToken bool) gin.H {
	body := gin.H{
		"id": token.ID, "created_at": token.CreatedAt, "updated_at": token.UpdatedAt,
		"deleted_at": token.DeletedAt, "label": token.Label, "description": token.Description,
		"is_active": token.IsActive, "last_used": token.LastUsed,
		"user_type": token.UserType, "expired_at": token.ExpiredAt, "is_service": token.IsService,
		"allowed_rate_limit": token.AllowedRateLimit,
		"created_by":         token.CreatedByID, "updated_by": token.UpdatedByID,
		"user": token.UserID, "workspace": token.WorkspaceID,
	}
	if withToken {
		body["token"] = token.Token
	}
	return body
}
