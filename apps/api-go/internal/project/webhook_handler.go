package project

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"github.com/yldm-tech/pace/apps/api-go/internal/drf"
	"github.com/yldm-tech/pace/apps/api-go/internal/httpsafe"
	"gorm.io/gorm"
)

func (handler *Handler) registerWebhookRoutes(router gin.IRouter) {
	router.GET("/api/workspaces/:slug/webhooks/", handler.authenticated(handler.webhookList))
	router.POST("/api/workspaces/:slug/webhooks/", handler.authenticated(handler.webhookCreate))
	router.GET("/api/workspaces/:slug/webhooks/:webhook/", handler.authenticated(handler.webhookRetrieve))
	router.PATCH("/api/workspaces/:slug/webhooks/:webhook/", handler.authenticated(handler.webhookUpdate))
	router.DELETE("/api/workspaces/:slug/webhooks/:webhook/", handler.authenticated(handler.webhookDestroy))
	router.POST("/api/workspaces/:slug/webhooks/:webhook/regenerate/", handler.authenticated(handler.webhookRegenerate))
	router.GET("/api/workspaces/:slug/webhook-logs/:webhook/", handler.authenticated(handler.webhookLogs))
}

// Webhook is the db.Webhook table: where a workspace wants to be told about things, and which things.
type Webhook struct {
	ID           string     `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt    time.Time  `gorm:"column:created_at"`
	UpdatedAt    time.Time  `gorm:"column:updated_at"`
	CreatedByID  *string    `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID  *string    `gorm:"column:updated_by_id;type:uuid"`
	DeletedAt    *time.Time `gorm:"column:deleted_at"`
	WorkspaceID  string     `gorm:"column:workspace_id;type:uuid"`
	URL          string     `gorm:"column:url"`
	IsActive     bool       `gorm:"column:is_active"`
	SecretKey    string     `gorm:"column:secret_key"`
	Project      bool       `gorm:"column:project"`
	Issue        bool       `gorm:"column:issue"`
	Module       bool       `gorm:"column:module"`
	Cycle        bool       `gorm:"column:cycle"`
	IssueComment bool       `gorm:"column:issue_comment"`
}

func (Webhook) TableName() string { return "webhooks" }

// WebhookLog is the db.WebhookLog table: one row per delivery attempt.
type WebhookLog struct {
	ID              string     `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt       time.Time  `gorm:"column:created_at"`
	UpdatedAt       time.Time  `gorm:"column:updated_at"`
	CreatedByID     *string    `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID     *string    `gorm:"column:updated_by_id;type:uuid"`
	DeletedAt       *time.Time `gorm:"column:deleted_at"`
	WorkspaceID     string     `gorm:"column:workspace_id;type:uuid"`
	WebhookID       string     `gorm:"column:webhook_id;type:uuid"`
	EventType       *string    `gorm:"column:event_type"`
	RequestMethod   *string    `gorm:"column:request_method"`
	RequestHeaders  *string    `gorm:"column:request_headers"`
	RequestBody     *string    `gorm:"column:request_body"`
	ResponseStatus  *string    `gorm:"column:response_status"`
	ResponseHeaders *string    `gorm:"column:response_headers"`
	ResponseBody    *string    `gorm:"column:response_body"`
	Retryyount      int        `gorm:"column:retry_count"`
}

func (WebhookLog) TableName() string { return "webhook_logs" }

// webhookList returns the workspace's webhooks, without their secrets.
func (handler *Handler) webhookList(c *gin.Context, user *auth.User) {
	if !handler.requireWorkspaceRole(c, user, roleAdmin) {
		return
	}
	var webhooks []Webhook
	err := handler.db.WithContext(c.Request.Context()).Table("webhooks wh").Select("wh.*").
		Joins("JOIN workspaces w ON w.id = wh.workspace_id").
		Where("w.slug = ? AND wh.deleted_at IS NULL", c.Param("slug")).
		Scan(&webhooks).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	results := make([]gin.H, 0, len(webhooks))
	for _, webhook := range webhooks {
		results = append(results, webhookJSON(webhook, false))
	}
	drf.Respond(c, http.StatusOK, results)
}

func (handler *Handler) webhookRetrieve(c *gin.Context, user *auth.User) {
	if !handler.requireWorkspaceRole(c, user, roleAdmin) {
		return
	}
	webhook, found, err := handler.webhookByID(c)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "The required object does not exist."})
		return
	}
	drf.Respond(c, http.StatusOK, webhookJSON(webhook, false))
}

// webhookCreate registers a new webhook.
//
// The secret is shown **once**, here, and never again except on a regenerate. Everything else that renders a webhook drops it.
func (handler *Handler) webhookCreate(c *gin.Context, user *auth.User) {
	if !handler.requireWorkspaceRole(c, user, roleAdmin) {
		return
	}
	slug := c.Param("slug")
	var payload map[string]any
	if err := c.ShouldBindJSON(&payload); err != nil {
		handler.invalidDetail(c)
		return
	}
	target, ok := payload["url"].(string)
	if !ok || target == "" {
		c.JSON(http.StatusBadRequest, gin.H{"url": []string{"This field is required."}})
		return
	}
	if message, field := handler.validateWebhookURL(c, target); message != "" {
		c.JSON(http.StatusBadRequest, gin.H{field: message})
		return
	}

	var workspaceIDs []string
	err := handler.db.WithContext(c.Request.Context()).Table("workspaces").
		Where("slug = ?", slug).Limit(1).Pluck("id", &workspaceIDs).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if len(workspaceIDs) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "The required object does not exist."})
		return
	}

	identifier, err := newUUID()
	if err != nil {
		handler.internalError(c, err)
		return
	}
	secret, err := generateWebhookToken()
	if err != nil {
		handler.internalError(c, err)
		return
	}
	now := handler.clock().UTC()
	webhook := Webhook{
		ID: identifier, CreatedAt: now, UpdatedAt: now, CreatedByID: &user.ID, UpdatedByID: &user.ID,
		WorkspaceID: workspaceIDs[0], URL: target, IsActive: true, SecretKey: secret,
	}
	applyWebhookPayload(&webhook, payload)

	err = handler.db.WithContext(c.Request.Context()).Create(&webhook).Error
	if err != nil {
		if isUniqueViolation(err) {
			// The unique index on the workspace and the url is the only one this table has, so a repeat registration is a conflict rather than a bad request.
			c.JSON(http.StatusConflict, gin.H{"error": "URL already exists for the workspace"})
			return
		}
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusCreated, webhookJSON(webhook, true))
}

// webhookUpdate edits a webhook. The url is revalidated only when the request carries one.
func (handler *Handler) webhookUpdate(c *gin.Context, user *auth.User) {
	if !handler.requireWorkspaceRole(c, user, roleAdmin) {
		return
	}
	var payload map[string]any
	if err := c.ShouldBindJSON(&payload); err != nil {
		handler.invalidDetail(c)
		return
	}
	webhook, found, err := handler.webhookByID(c)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "The required object does not exist."})
		return
	}
	if target, ok := payload["url"].(string); ok && target != "" {
		if message, field := handler.validateWebhookURL(c, target); message != "" {
			c.JSON(http.StatusBadRequest, gin.H{field: message})
			return
		}
	}

	applyWebhookPayload(&webhook, payload)
	now := handler.clock().UTC()
	webhook.UpdatedAt, webhook.UpdatedByID = now, &user.ID
	err = handler.db.WithContext(c.Request.Context()).Model(&Webhook{}).Where("id = ?", webhook.ID).
		Updates(map[string]any{
			"url": webhook.URL, "is_active": webhook.IsActive,
			"project": webhook.Project, "issue": webhook.Issue, "module": webhook.Module,
			"cycle": webhook.Cycle, "issue_comment": webhook.IssueComment,
			"updated_at": webhook.UpdatedAt, "updated_by_id": webhook.UpdatedByID,
		}).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, webhookJSON(webhook, false))
}

func (handler *Handler) webhookDestroy(c *gin.Context, user *auth.User) {
	if !handler.requireWorkspaceRole(c, user, roleAdmin) {
		return
	}
	webhook, found, err := handler.webhookByID(c)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "The required object does not exist."})
		return
	}
	err = handler.db.WithContext(c.Request.Context()).Model(&Webhook{}).
		Where("id = ?", webhook.ID).Update("deleted_at", handler.clock().UTC()).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// webhookRegenerate issues a new secret and is the only other place one is shown.
func (handler *Handler) webhookRegenerate(c *gin.Context, user *auth.User) {
	if !handler.requireWorkspaceRole(c, user, roleAdmin) {
		return
	}
	webhook, found, err := handler.webhookByID(c)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "The required object does not exist."})
		return
	}
	secret, err := generateWebhookToken()
	if err != nil {
		handler.internalError(c, err)
		return
	}
	now := handler.clock().UTC()
	webhook.SecretKey, webhook.UpdatedAt, webhook.UpdatedByID = secret, now, &user.ID
	err = handler.db.WithContext(c.Request.Context()).Model(&Webhook{}).Where("id = ?", webhook.ID).
		Updates(map[string]any{"secret_key": secret, "updated_at": now, "updated_by_id": user.ID}).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, webhookJSON(webhook, true))
}

// webhookLogs returns the delivery attempts for one webhook.
func (handler *Handler) webhookLogs(c *gin.Context, user *auth.User) {
	if !handler.requireWorkspaceRole(c, user, roleAdmin) {
		return
	}
	var logs []WebhookLog
	err := handler.db.WithContext(c.Request.Context()).Table("webhook_logs wl").Select("wl.*").
		Joins("JOIN workspaces w ON w.id = wl.workspace_id").
		Where("w.slug = ? AND wl.webhook_id = ? AND wl.deleted_at IS NULL", c.Param("slug"), c.Param("webhook")).
		Scan(&logs).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	results := make([]gin.H, 0, len(logs))
	for _, entry := range logs {
		results = append(results, gin.H{
			"id": entry.ID, "created_at": entry.CreatedAt, "updated_at": entry.UpdatedAt,
			"created_by": entry.CreatedByID, "updated_by": entry.UpdatedByID, "deleted_at": entry.DeletedAt,
			"event_type": entry.EventType, "request_method": entry.RequestMethod,
			"request_headers": entry.RequestHeaders, "request_body": entry.RequestBody,
			"response_status": entry.ResponseStatus, "response_headers": entry.ResponseHeaders,
			"response_body": entry.ResponseBody, "retry_count": entry.Retryyount,
			"workspace": entry.WorkspaceID, "webhook": entry.WebhookID,
		})
	}
	drf.Respond(c, http.StatusOK, results)
}

func (handler *Handler) webhookByID(c *gin.Context) (Webhook, bool, error) {
	var webhook Webhook
	err := handler.db.WithContext(c.Request.Context()).Table("webhooks wh").Select("wh.*").
		Joins("JOIN workspaces w ON w.id = wh.workspace_id").
		Where("w.slug = ? AND wh.id = ? AND wh.deleted_at IS NULL", c.Param("slug"), c.Param("webhook")).
		Take(&webhook).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Webhook{}, false, nil
	}
	if err != nil {
		return Webhook{}, false, err
	}
	return webhook, true, nil
}

// validateWebhookURL is the three checks a webhook url has to pass, in the order the serializer runs them.
//
// The scheme and the local-name check come from the field's own validators; the SSRF resolve and the disallowed-domain check come from the serializer. They report different messages under different keys, which is worth keeping because the frontend shows them.
func (handler *Handler) validateWebhookURL(c *gin.Context, target string) (any, string) {
	parsed, err := url.Parse(target)
	if err != nil || parsed.Host == "" {
		return []string{"Enter a valid URL."}, "url"
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return []string{"Invalid schema. Only HTTP and HTTPS are allowed."}, "url"
	}
	// The local check looks at the whole netloc, so a port makes it pass — "localhost:8000" is not in the list.
	if parsed.Host == "localhost" || parsed.Host == "127.0.0.1" {
		return []string{"Local URLs are not allowed."}, "url"
	}

	settings := httpsafe.Settings{
		AllowedIPs:   handler.settings.WebhookAllowedIPs,
		AllowedHosts: handler.settings.WebhookAllowedHosts,
	}
	hostname := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if !httpsafe.HostAllowed(hostname, settings.AllowedHosts) {
		if _, err := httpsafe.ResolveAndValidate(parsed.Hostname(), settings.AllowedIPs, true); err != nil {
			return "Invalid or disallowed webhook URL.", "url"
		}
		// A host on the allowlist skips the disallowed-domain check too: it is already trusted for SSRF, and the loop-back guard would only get in the way of a sibling service sharing a parent domain.
		disallowed := append([]string{}, handler.settings.WebhookDisallowedDomains...)
		if requestHost := strings.ToLower(strings.TrimSuffix(strings.Split(c.Request.Host, ":")[0], ".")); requestHost != "" {
			disallowed = append(disallowed, requestHost)
		}
		for _, domain := range disallowed {
			if hostname == domain || strings.HasSuffix(hostname, "."+domain) {
				return "URL domain or its subdomain is not allowed.", "url"
			}
		}
	}
	return "", ""
}

// generateWebhookToken is db.models.webhook.generate_token: a fixed prefix and a hex uuid with no dashes.
func generateWebhookToken() (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	// The version and variant nibbles are set the way uuid4 sets them, since the token is a uuid's hex rather than arbitrary bytes.
	buffer[6] = (buffer[6] & 0x0f) | 0x40
	buffer[8] = (buffer[8] & 0x3f) | 0x80
	return "plane_wh_" + hex.EncodeToString(buffer), nil
}

// applyWebhookPayload writes the writable fields. The workspace and the secret are read-only, so a caller naming either changes nothing.
func applyWebhookPayload(webhook *Webhook, payload map[string]any) {
	if value, ok := payload["url"].(string); ok {
		webhook.URL = value
	}
	for key, target := range map[string]*bool{
		"is_active": &webhook.IsActive, "project": &webhook.Project, "issue": &webhook.Issue,
		"module": &webhook.Module, "cycle": &webhook.Cycle, "issue_comment": &webhook.IssueComment,
	} {
		if value, ok := payload[key].(bool); ok {
			*target = value
		}
	}
}

// webhookJSON is WebhookSerializer over one row.
//
// The fields= allowlists the views pass are dead twice over — DynamicBaseSerializer discards them and the filter never removes anything — so every route renders the whole model, and the **only** thing keeping the secret back is the context flag.
func webhookJSON(webhook Webhook, withSecret bool) gin.H {
	data := gin.H{
		"id": webhook.ID, "created_at": webhook.CreatedAt, "updated_at": webhook.UpdatedAt,
		"created_by": webhook.CreatedByID, "updated_by": webhook.UpdatedByID, "deleted_at": webhook.DeletedAt,
		"url": webhook.URL, "is_active": webhook.IsActive, "workspace": webhook.WorkspaceID,
		"project": webhook.Project, "issue": webhook.Issue, "module": webhook.Module,
		"cycle": webhook.Cycle, "issue_comment": webhook.IssueComment,
	}
	if withSecret {
		data["secret_key"] = webhook.SecretKey
	}
	return data
}
