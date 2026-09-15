package workspace

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	roleGuest  = 5
	roleMember = 15
	roleAdmin  = 20
)

var workspaceSlugPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
var workspaceURLPattern = regexp.MustCompile(`(?i)(https?://\S+|www\.[a-z0-9][a-z0-9.-]*|([a-z0-9][a-z0-9-]*\.)+[a-z]{2,6}|(([0-9]{1,3})\.){3}[0-9]{1,3})`)
var uuidPattern = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

var restrictedWorkspaceSlugs = map[string]struct{}{
	"404": {}, "accounts": {}, "api": {}, "create-workspace": {}, "god-mode": {},
	"installations": {}, "invitations": {}, "onboarding": {}, "profile": {}, "spaces": {},
	"workspace-invitations": {}, "password": {}, "flags": {}, "monitor": {}, "monitoring": {},
	"ingest": {}, "plane-pro": {}, "plane-ultimate": {}, "enterprise": {}, "plane-enterprise": {},
	"disco": {}, "silo": {}, "chat": {}, "calendar": {}, "drive": {}, "channels": {},
	"upgrade": {}, "billing": {}, "sign-in": {}, "sign-up": {}, "signin": {}, "signup": {},
	"config": {}, "live": {}, "admin": {}, "m": {}, "import": {}, "importers": {},
	"integrations": {}, "integration": {}, "configuration": {}, "initiatives": {}, "initiative": {},
	"workflow": {}, "workflows": {}, "epics": {}, "epic": {}, "story": {}, "mobile": {},
	"dashboard": {}, "desktop": {}, "onload": {}, "real-time": {}, "one": {}, "pages": {},
	"business": {}, "pro": {}, "settings": {}, "license": {}, "licenses": {}, "instances": {},
	"instance": {},
}

type Settings struct {
	SecretKey                string
	AppBaseURL               string
	SkipEnvironmentConfig    bool
	DisableWorkspaceCreation string
}

type TaskPublisher interface {
	PublishWorkspaceSeed(ctx context.Context, workspaceID string) error
	PublishWorkspaceInvitation(ctx context.Context, email, workspaceID, token, currentSite, inviter string) error
	PublishSoftDeleteRelatedObjects(ctx context.Context, appLabel, modelName, instanceID string) error
}

type invitationCandidate struct {
	Email string          `json:"email"`
	Role  json.RawMessage `json:"role"`
}

func (candidate invitationCandidate) role() (int, error) {
	if len(candidate.Role) == 0 {
		return roleGuest, nil
	}
	return integerValue(candidate.Role)
}

type Handler struct {
	db       *gorm.DB
	sessions *auth.SessionManager
	users    auth.Repository
	settings Settings
	tasks    TaskPublisher
	cache    auth.CacheInvalidator
	clock    func() time.Time
}

func NewHandler(db *gorm.DB, sessions *auth.SessionManager, users auth.Repository, settings Settings) *Handler {
	return &Handler{db: db, sessions: sessions, users: users, settings: settings, clock: time.Now}
}

func (handler *Handler) SetTasks(publisher TaskPublisher)           { handler.tasks = publisher }
func (handler *Handler) SetCache(invalidator auth.CacheInvalidator) { handler.cache = invalidator }

func (handler *Handler) Register(router gin.IRouter) {
	router.GET("/api/workspace-slug-check/", handler.authenticated(handler.slugCheck))
	router.GET("/api/workspaces/", handler.authenticated(handler.workspaceList))
	router.POST("/api/workspaces/", handler.authenticated(handler.workspaceCreate))
	router.GET("/api/workspaces/:slug/", handler.authenticated(handler.workspaceRetrieve))
	router.PUT("/api/workspaces/:slug/", handler.authenticated(handler.workspaceUpdate))
	router.PATCH("/api/workspaces/:slug/", handler.authenticated(handler.workspaceUpdate))
	router.DELETE("/api/workspaces/:slug/", handler.authenticated(handler.workspaceDelete))
	router.GET("/api/users/me/workspaces/", handler.authenticated(handler.userWorkspaces))

	router.GET("/api/workspaces/:slug/members/", handler.authenticated(handler.memberList))
	router.GET("/api/workspaces/:slug/members/:id/", handler.authenticatedUUID(handler.memberRetrieve))
	router.PATCH("/api/workspaces/:slug/members/:id/", handler.authenticatedUUID(handler.memberPatch))
	router.DELETE("/api/workspaces/:slug/members/:id/", handler.authenticatedUUID(handler.memberDelete))
	router.POST("/api/workspaces/:slug/members/leave/", handler.authenticated(handler.memberLeave))
	router.GET("/api/workspaces/:slug/workspace-members/me/", handler.authenticated(handler.memberMe))
	router.POST("/api/workspaces/:slug/workspace-views/", handler.authenticated(handler.memberViews))

	router.GET("/api/workspaces/:slug/invitations/", handler.authenticated(handler.invitationList))
	router.POST("/api/workspaces/:slug/invitations/", handler.authenticated(handler.invitationCreate))
	router.GET("/api/workspaces/:slug/invitations/:id/", handler.authenticatedUUID(handler.invitationRetrieve))
	router.PATCH("/api/workspaces/:slug/invitations/:id/", handler.authenticatedUUID(handler.invitationPatch))
	router.DELETE("/api/workspaces/:slug/invitations/:id/", handler.authenticatedUUID(handler.invitationDelete))
	router.GET("/api/users/me/workspaces/invitations/", handler.authenticated(handler.userInvitationList))
	router.POST("/api/users/me/workspaces/invitations/", handler.authenticated(handler.userInvitationAccept))

	// Invitation GET is deliberately public and never includes the acceptance token.
	router.GET("/api/workspaces/:slug/invitations/:id/join/", handler.uuidPath(handler.invitationPublic))
	router.POST("/api/workspaces/:slug/invitations/:id/join/", handler.uuidPath(handler.invitationJoin))
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

func (handler *Handler) authenticatedUUID(next func(*gin.Context, *auth.User)) gin.HandlerFunc {
	return handler.uuidPath(handler.authenticated(next))
}

func (handler *Handler) uuidPath(next gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !uuidPattern.MatchString(c.Param("id")) {
			handler.notFound(c)
			return
		}
		next(c)
	}
}

func (handler *Handler) slugCheck(c *gin.Context, _ *auth.User) {
	slug := c.Query("slug")
	if slug == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Workspace Slug is required"})
		return
	}
	var count int64
	err := handler.db.WithContext(c.Request.Context()).Model(&Workspace{}).
		Where("slug = ? AND deleted_at IS NULL", slug).Count(&count).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	_, restricted := restrictedWorkspaceSlugs[slug]
	c.JSON(http.StatusOK, gin.H{"status": count == 0 && !restricted})
}

func (handler *Handler) workspaceList(c *gin.Context, user *auth.User) {
	rows, err := handler.queryWorkspaces(c.Request.Context(), user.ID, "", c.Query("search"), c.Query("owner"))
	if err != nil {
		handler.internalError(c, err)
		return
	}
	result := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		result = append(result, handler.workspaceJSON(c.Request.Context(), row))
	}
	c.JSON(http.StatusOK, result)
}

func (handler *Handler) userWorkspaces(c *gin.Context, user *auth.User) {
	rows, err := handler.queryWorkspaces(c.Request.Context(), user.ID, "", c.Query("search"), c.Query("owner"))
	if err != nil {
		handler.internalError(c, err)
		return
	}
	result := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		data := handler.workspaceJSON(c.Request.Context(), row)
		result = append(result, data)
	}
	c.JSON(http.StatusOK, result)
}

func (handler *Handler) workspaceRetrieve(c *gin.Context, user *auth.User) {
	rows, err := handler.queryWorkspaces(c.Request.Context(), user.ID, c.Param("slug"), c.Query("search"), c.Query("owner"))
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if len(rows) == 0 {
		handler.notFound(c)
		return
	}
	c.JSON(http.StatusOK, handler.workspaceJSON(c.Request.Context(), rows[0]))
}

func (handler *Handler) workspaceCreate(c *gin.Context, user *auth.User) {
	disabled, err := handler.configurationValue(c.Request.Context(), "DISABLE_WORKSPACE_CREATION", handler.settings.DisableWorkspaceCreation)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if disabled == "1" || strings.EqualFold(disabled, "true") {
		c.JSON(http.StatusForbidden, gin.H{"error": "Workspace creation is not allowed"})
		return
	}
	var request struct {
		Name             string          `json:"name"`
		Slug             string          `json:"slug"`
		CompanyRole      string          `json:"company_role"`
		Logo             *string         `json:"logo"`
		LogoAssetID      *string         `json:"logo_asset"`
		OrganizationSize *string         `json:"organization_size"`
		Timezone         json.RawMessage `json:"timezone"`
		BackgroundColor  json.RawMessage `json:"background_color"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		handler.invalidDetail(c)
		return
	}
	request.Name = strings.TrimSpace(request.Name)
	request.Slug = strings.TrimSpace(request.Slug)
	if request.Name == "" || request.Slug == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Both name and slug are required"})
		return
	}
	if utf8.RuneCountInString(request.Name) > 80 || len(request.Slug) > 48 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "The maximum length for name is 80 and for slug is 48"})
		return
	}
	if containsURL(request.Name) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Name cannot contain a URL"})
		return
	}
	if !hasAlphanumeric(request.Name) {
		c.JSON(http.StatusBadRequest, gin.H{"name": []string{"Name must contain at least one letter or number"}})
		return
	}
	if !validWorkspaceSlug(request.Slug) {
		c.JSON(http.StatusBadRequest, gin.H{"slug": []string{"Slug can only contain letters, numbers, hyphens (-), and underscores (_)"}})
		return
	}
	if _, restricted := restrictedWorkspaceSlugs[request.Slug]; restricted {
		c.JSON(http.StatusBadRequest, gin.H{"slug": []string{"Slug is not valid"}})
		return
	}
	workspaceID, err := newUUID()
	if err != nil {
		handler.internalError(c, err)
		return
	}
	now := handler.clock().UTC()
	timezoneValue := "UTC"
	if len(request.Timezone) > 0 {
		if string(request.Timezone) == "null" {
			c.JSON(http.StatusBadRequest, gin.H{"timezone": []string{"This field may not be null."}})
			return
		}
		if err := json.Unmarshal(request.Timezone, &timezoneValue); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"timezone": []string{"Not a valid string."}})
			return
		}
		if timezoneValue == "" {
			c.JSON(http.StatusBadRequest, gin.H{"timezone": []string{"This field may not be blank."}})
			return
		}
	}
	if _, err := time.LoadLocation(timezoneValue); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"timezone": []string{fmt.Sprintf(`%q is not a valid choice.`, timezoneValue)}})
		return
	}
	if request.OrganizationSize != nil && utf8.RuneCountInString(*request.OrganizationSize) > 20 {
		c.JSON(http.StatusBadRequest, gin.H{"organization_size": []string{"Ensure this field has no more than 20 characters."}})
		return
	}
	backgroundColor := randomColor()
	if len(request.BackgroundColor) > 0 {
		if string(request.BackgroundColor) == "null" {
			c.JSON(http.StatusBadRequest, gin.H{"background_color": []string{"This field may not be null."}})
			return
		}
		if err := json.Unmarshal(request.BackgroundColor, &backgroundColor); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"background_color": []string{"Not a valid string."}})
			return
		}
		if backgroundColor == "" {
			c.JSON(http.StatusBadRequest, gin.H{"background_color": []string{"This field may not be blank."}})
			return
		}
	}
	if utf8.RuneCountInString(backgroundColor) > 255 {
		c.JSON(http.StatusBadRequest, gin.H{"background_color": []string{"Ensure this field has no more than 255 characters."}})
		return
	}
	if request.LogoAssetID != nil {
		if !uuidPattern.MatchString(*request.LogoAssetID) {
			c.JSON(http.StatusBadRequest, gin.H{"logo_asset": []string{fmt.Sprintf(`%q is not a valid UUID.`, *request.LogoAssetID)}})
			return
		}
		exists, err := handler.assetExists(c.Request.Context(), *request.LogoAssetID)
		if err != nil {
			handler.internalError(c, err)
			return
		}
		if !exists {
			c.JSON(http.StatusBadRequest, gin.H{"logo_asset": []string{fmt.Sprintf(`Invalid pk %q - object does not exist.`, *request.LogoAssetID)}})
			return
		}
	}
	workspace := Workspace{ID: workspaceID, CreatedAt: now, UpdatedAt: now, CreatedByID: &user.ID, Name: request.Name, Logo: request.Logo, LogoAssetID: request.LogoAssetID, OwnerID: user.ID, Slug: request.Slug, OrganizationSize: request.OrganizationSize, Timezone: timezoneValue, BackgroundColor: backgroundColor}
	memberID, err := newUUID()
	if err != nil {
		handler.internalError(c, err)
		return
	}
	companyRole := request.CompanyRole
	member := WorkspaceMember{ID: memberID, CreatedAt: now, UpdatedAt: now, CreatedByID: &user.ID, WorkspaceID: workspaceID, MemberID: user.ID, Role: roleAdmin, CompanyRole: &companyRole, IsActive: true, ViewProps: defaultPropsJSON(), DefaultProps: defaultPropsJSON(), IssueProps: issuePropsJSON(), GettingStartedChecklist: emptyJSON(), Tips: emptyJSON(), ExploredFeatures: emptyJSON()}
	err = handler.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&workspace).Error; err != nil {
			return err
		}
		return tx.Create(&member).Error
	})
	if err != nil {
		if isUniqueViolation(err) {
			c.JSON(http.StatusConflict, gin.H{"slug": "The workspace with the slug already exists"})
			return
		}
		handler.internalError(c, err)
		return
	}
	if handler.tasks != nil {
		if err := handler.tasks.PublishWorkspaceSeed(c.Request.Context(), workspaceID); err != nil {
			handler.internalError(c, err)
			return
		}
	}
	row := workspaceRow{Workspace: workspace, TotalMembers: 1, Role: roleAdmin}
	c.JSON(http.StatusCreated, handler.workspaceJSON(c.Request.Context(), row))
}

func (handler *Handler) workspaceUpdate(c *gin.Context, user *auth.User) {
	role, err := handler.workspaceRole(c.Request.Context(), c.Param("slug"), user.ID)
	if err != nil {
		handler.notFound(c)
		return
	}
	if role != roleAdmin {
		c.JSON(http.StatusForbidden, gin.H{"detail": "You do not have permission to perform this action."})
		return
	}
	var fields map[string]json.RawMessage
	if err := c.ShouldBindJSON(&fields); err != nil {
		handler.invalidDetail(c)
		return
	}
	updates := make(map[string]any)
	if c.Request.Method == http.MethodPut {
		missing := gin.H{}
		if _, ok := fields["name"]; !ok {
			missing["name"] = []string{"This field is required."}
		}
		if _, ok := fields["slug"]; !ok {
			missing["slug"] = []string{"This field is required."}
		}
		if len(missing) > 0 {
			c.JSON(http.StatusBadRequest, missing)
			return
		}
	}
	for key, raw := range fields {
		switch key {
		case "name":
			var value string
			if json.Unmarshal(raw, &value) != nil {
				c.JSON(http.StatusBadRequest, gin.H{"name": []string{"Not a valid string."}})
				return
			}
			value = strings.TrimSpace(value)
			if value == "" {
				c.JSON(http.StatusBadRequest, gin.H{"name": []string{"This field may not be blank."}})
				return
			}
			if utf8.RuneCountInString(value) > 80 {
				c.JSON(http.StatusBadRequest, gin.H{"name": []string{"Ensure this field has no more than 80 characters."}})
				return
			}
			if containsURL(value) {
				c.JSON(http.StatusBadRequest, gin.H{"name": []string{"Name must not contain URLs"}})
				return
			}
			if !hasAlphanumeric(value) {
				c.JSON(http.StatusBadRequest, gin.H{"name": []string{"Name must contain at least one letter or number"}})
				return
			}
			updates["name"] = value
		case "slug":
			var value string
			if json.Unmarshal(raw, &value) != nil {
				c.JSON(http.StatusBadRequest, gin.H{"slug": []string{"Not a valid string."}})
				return
			}
			value = strings.TrimSpace(value)
			if len(value) > 48 {
				c.JSON(http.StatusBadRequest, gin.H{"slug": []string{"Ensure this field has no more than 48 characters."}})
				return
			}
			if !validWorkspaceSlug(value) {
				c.JSON(http.StatusBadRequest, gin.H{"slug": []string{"Slug can only contain letters, numbers, hyphens (-), and underscores (_)"}})
				return
			}
			if _, restricted := restrictedWorkspaceSlugs[value]; restricted {
				c.JSON(http.StatusBadRequest, gin.H{"slug": []string{"Slug is not valid"}})
				return
			}
			updates["slug"] = value
		case "logo", "logo_asset", "organization_size", "timezone", "background_color":
			column := key
			if key == "logo_asset" {
				column = "logo_asset_id"
			}
			var value any
			if string(raw) == "null" {
				if key == "timezone" || key == "background_color" {
					c.JSON(http.StatusBadRequest, gin.H{key: []string{"This field may not be null."}})
					return
				}
				value = nil
			} else if err := json.Unmarshal(raw, &value); err != nil {
				handler.invalidDetail(c)
				return
			}
			if text, ok := value.(string); ok {
				if (key == "timezone" || key == "background_color") && text == "" {
					c.JSON(http.StatusBadRequest, gin.H{key: []string{"This field may not be blank."}})
					return
				}
				if key == "organization_size" && utf8.RuneCountInString(text) > 20 {
					c.JSON(http.StatusBadRequest, gin.H{key: []string{"Ensure this field has no more than 20 characters."}})
					return
				}
				if (key == "timezone" || key == "background_color") && utf8.RuneCountInString(text) > 255 {
					c.JSON(http.StatusBadRequest, gin.H{key: []string{"Ensure this field has no more than 255 characters."}})
					return
				}
				if key == "timezone" {
					if _, err := time.LoadLocation(text); err != nil {
						c.JSON(http.StatusBadRequest, gin.H{"timezone": []string{fmt.Sprintf(`%q is not a valid choice.`, text)}})
						return
					}
				}
				if key == "logo_asset" {
					if !uuidPattern.MatchString(text) {
						c.JSON(http.StatusBadRequest, gin.H{"logo_asset": []string{fmt.Sprintf(`%q is not a valid UUID.`, text)}})
						return
					}
					exists, err := handler.assetExists(c.Request.Context(), text)
					if err != nil {
						handler.internalError(c, err)
						return
					}
					if !exists {
						c.JSON(http.StatusBadRequest, gin.H{"logo_asset": []string{fmt.Sprintf(`Invalid pk %q - object does not exist.`, text)}})
						return
					}
				}
			}
			updates[column] = value
		}
	}
	if len(updates) > 0 {
		updates["updated_at"] = handler.clock().UTC()
		updates["updated_by_id"] = user.ID
		result := handler.db.WithContext(c.Request.Context()).Model(&Workspace{}).Where("slug = ? AND deleted_at IS NULL", c.Param("slug")).Updates(updates)
		if result.Error != nil {
			if isUniqueViolation(result.Error) {
				c.JSON(http.StatusConflict, gin.H{"slug": "The workspace with the slug already exists"})
				return
			}
			handler.internalError(c, result.Error)
			return
		}
		if result.RowsAffected == 0 {
			handler.notFound(c)
			return
		}
	}
	lookupSlug := c.Param("slug")
	if changedSlug, ok := updates["slug"].(string); ok {
		lookupSlug = changedSlug
	}
	rows, err := handler.queryWorkspaces(c.Request.Context(), user.ID, lookupSlug, "", "")
	if err != nil || len(rows) == 0 {
		handler.notFound(c)
		return
	}
	c.JSON(http.StatusOK, handler.workspaceJSON(c.Request.Context(), rows[0]))
}

func (handler *Handler) workspaceDelete(c *gin.Context, user *auth.User) {
	role, err := handler.workspaceRole(c.Request.Context(), c.Param("slug"), user.ID)
	if err != nil {
		handler.notFound(c)
		return
	}
	if role != roleAdmin {
		c.JSON(http.StatusForbidden, gin.H{"detail": "You do not have permission to perform this action."})
		return
	}
	var workspace Workspace
	if err := handler.db.WithContext(c.Request.Context()).Where("slug = ? AND deleted_at IS NULL", c.Param("slug")).Take(&workspace).Error; err != nil {
		handler.notFound(c)
		return
	}
	now := handler.clock().UTC()
	deletedSlug := fmt.Sprintf("%s__%d", workspace.Slug, now.Unix())
	err = handler.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&Workspace{}).Where("id = ?", workspace.ID).Updates(map[string]any{"deleted_at": now, "slug": deletedSlug, "updated_at": now, "updated_by_id": user.ID}).Error; err != nil {
			return err
		}
		return tx.Table("profiles").Where("last_workspace_id = ?", workspace.ID).Update("last_workspace_id", nil).Error
	})
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if handler.tasks != nil {
		if err := handler.tasks.PublishSoftDeleteRelatedObjects(c.Request.Context(), "db", "workspace", workspace.ID); err != nil {
			handler.internalError(c, err)
			return
		}
	}
	c.Status(http.StatusNoContent)
}

func (handler *Handler) queryWorkspaces(ctx context.Context, userID, slug, search, owner string) ([]workspaceRow, error) {
	query := handler.db.WithContext(ctx).Table("workspaces w").
		Select("w.*, COUNT(DISTINCT CASE WHEN wm_all.is_active = TRUE AND wm_all.deleted_at IS NULL AND u_all.is_bot = FALSE THEN wm_all.id END) AS total_members, wm_user.role AS role").
		Joins("JOIN workspace_members wm_user ON wm_user.workspace_id = w.id AND wm_user.member_id = ? AND wm_user.is_active = TRUE AND wm_user.deleted_at IS NULL", userID).
		Joins("LEFT JOIN workspace_members wm_all ON wm_all.workspace_id = w.id").
		Joins("LEFT JOIN users u_all ON u_all.id = wm_all.member_id").
		Where("w.deleted_at IS NULL").Group("w.id, wm_user.role").Order("w.name ASC")
	if slug != "" {
		query = query.Where("w.slug = ?", slug)
	}
	if search != "" {
		query = query.Where("w.name ILIKE ?", "%"+search+"%")
	}
	if owner != "" {
		query = query.Where("w.owner_id = ?", owner)
	}
	var rows []workspaceRow
	if err := query.Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (handler *Handler) workspaceJSON(ctx context.Context, row workspaceRow) gin.H {
	logoURL := any(nil)
	if row.LogoAssetID != nil {
		logoURL = "/api/assets/v2/static/" + *row.LogoAssetID + "/"
	} else if row.Logo != nil && *row.Logo != "" {
		logoURL = *row.Logo
	}
	data := gin.H{
		"id": row.ID, "created_at": row.CreatedAt, "updated_at": row.UpdatedAt,
		"created_by": row.CreatedByID, "updated_by": row.UpdatedByID, "deleted_at": row.DeletedAt,
		"name": row.Name, "logo": row.Logo, "logo_asset": row.LogoAssetID, "owner": row.OwnerID,
		"slug": row.Slug, "organization_size": row.OrganizationSize, "timezone": row.Timezone,
		"background_color": row.BackgroundColor, "logo_url": logoURL, "total_members": row.TotalMembers,
		"role": row.Role,
	}
	_ = ctx
	return data
}

func (handler *Handler) workspaceRole(ctx context.Context, slug, userID string) (int, error) {
	var membership struct {
		Role int `gorm:"column:role"`
	}
	err := handler.db.WithContext(ctx).Table("workspace_members wm").Select("wm.role").
		Joins("JOIN workspaces w ON w.id = wm.workspace_id").
		Where("w.slug = ? AND w.deleted_at IS NULL AND wm.member_id = ? AND wm.is_active = TRUE AND wm.deleted_at IS NULL", slug, userID).Take(&membership).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, auth.ErrNotFound
	}
	return membership.Role, err
}

func (handler *Handler) memberList(c *gin.Context, user *auth.User) {
	requesterRole, err := handler.workspaceRole(c.Request.Context(), c.Param("slug"), user.ID)
	if err != nil {
		handler.notFound(c)
		return
	}
	var workspaceID string
	if err := handler.db.WithContext(c.Request.Context()).Table("workspaces").Where("slug = ? AND deleted_at IS NULL", c.Param("slug")).Pluck("id", &workspaceID).Error; err != nil || workspaceID == "" {
		handler.notFound(c)
		return
	}
	var members []WorkspaceMember
	query := handler.db.WithContext(c.Request.Context()).Where("workspace_id = ? AND deleted_at IS NULL", workspaceID).Order("created_at DESC")
	if search := c.Query("search"); search != "" {
		query = query.Joins("JOIN users ON users.id = workspace_members.member_id").Where("users.display_name ILIKE ? OR users.first_name ILIKE ?", "%"+search+"%", "%"+search+"%")
	}
	if err := query.Find(&members).Error; err != nil {
		handler.internalError(c, err)
		return
	}
	result := make([]gin.H, 0, len(members))
	for _, member := range members {
		data, err := handler.memberJSON(c.Request.Context(), member, requesterRole > roleGuest)
		if err != nil {
			handler.internalError(c, err)
			return
		}
		result = append(result, data)
	}
	c.JSON(http.StatusOK, result)
}

func (handler *Handler) memberRetrieve(c *gin.Context, user *auth.User) {
	requesterRole, err := handler.workspaceRole(c.Request.Context(), c.Param("slug"), user.ID)
	if err != nil {
		handler.notFound(c)
		return
	}
	var member WorkspaceMember
	err = handler.db.WithContext(c.Request.Context()).Where("id = ? AND workspace_id = (SELECT id FROM workspaces WHERE slug = ? AND deleted_at IS NULL) AND deleted_at IS NULL", c.Param("id"), c.Param("slug")).Take(&member).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "Workspace member not found"})
		return
	}
	if err != nil {
		handler.internalError(c, err)
		return
	}
	data, err := handler.memberJSON(c.Request.Context(), member, requesterRole > roleGuest)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, data)
}

func (handler *Handler) memberPatch(c *gin.Context, user *auth.User) {
	requesterRole, err := handler.workspaceRole(c.Request.Context(), c.Param("slug"), user.ID)
	if err != nil {
		handler.notFound(c)
		return
	}
	if requesterRole != roleAdmin {
		c.JSON(http.StatusForbidden, gin.H{"detail": "You do not have permission to perform this action."})
		return
	}
	var member WorkspaceMember
	if err := handler.db.WithContext(c.Request.Context()).Where("id = ? AND workspace_id = (SELECT id FROM workspaces WHERE slug = ? AND deleted_at IS NULL) AND is_active = TRUE AND deleted_at IS NULL", c.Param("id"), c.Param("slug")).Take(&member).Error; err != nil {
		handler.notFound(c)
		return
	}
	if member.MemberID == user.ID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "You cannot update your own role"})
		return
	}
	var fields map[string]json.RawMessage
	if err := c.ShouldBindJSON(&fields); err != nil {
		handler.invalidDetail(c)
		return
	}
	updates := make(map[string]any)
	if raw, ok := fields["role"]; ok {
		role, err := integerValue(raw)
		if err != nil || (role != roleGuest && role != roleMember && role != roleAdmin) {
			handler.invalidDetail(c)
			return
		}
		updates["role"] = role
	}
	if raw, ok := fields["company_role"]; ok {
		if string(raw) == "null" {
			updates["company_role"] = nil
		} else {
			var role string
			if err := json.Unmarshal(raw, &role); err != nil {
				handler.invalidDetail(c)
				return
			}
			updates["company_role"] = role
		}
	}
	for _, field := range []string{"view_props", "default_props", "issue_props", "getting_started_checklist", "tips", "explored_features"} {
		if raw, ok := fields[field]; ok {
			if !json.Valid(raw) || string(raw) == "null" {
				handler.invalidDetail(c)
				return
			}
			updates[field] = auth.JSONValue(raw)
		}
	}
	if raw, ok := fields["is_active"]; ok {
		var active bool
		if err := json.Unmarshal(raw, &active); err != nil {
			handler.invalidDetail(c)
			return
		}
		updates["is_active"] = active
	}
	if len(updates) > 0 {
		updates["updated_at"] = handler.clock().UTC()
		updates["updated_by_id"] = user.ID
		err := handler.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
			if role, ok := updates["role"].(int); ok && role == roleGuest {
				if err := tx.Table("project_members pm").Where("pm.member_id = ? AND pm.workspace_id = ? AND pm.deleted_at IS NULL", member.MemberID, workspaceIDSubquery(c.Param("slug"))).Update("role", roleGuest).Error; err != nil {
					return err
				}
			}
			return tx.Model(&WorkspaceMember{}).Where("id = ?", member.ID).Updates(updates).Error
		})
		if err != nil {
			handler.internalError(c, err)
			return
		}
	}
	var refreshed WorkspaceMember
	if err := handler.db.WithContext(c.Request.Context()).Where("id = ?", member.ID).Take(&refreshed).Error; err != nil {
		handler.internalError(c, err)
		return
	}
	data, err := handler.fullMemberJSON(c.Request.Context(), refreshed, false)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, data)
}

func (handler *Handler) memberDelete(c *gin.Context, user *auth.User) {
	requesterRole, err := handler.workspaceRole(c.Request.Context(), c.Param("slug"), user.ID)
	if err != nil {
		handler.notFound(c)
		return
	}
	if requesterRole != roleAdmin {
		c.JSON(http.StatusForbidden, gin.H{"detail": "You do not have permission to perform this action."})
		return
	}
	var member WorkspaceMember
	if err := handler.db.WithContext(c.Request.Context()).Where("id = ? AND workspace_id = (SELECT id FROM workspaces WHERE slug = ? AND deleted_at IS NULL) AND member_id IN (SELECT id FROM users WHERE is_bot = FALSE) AND is_active = TRUE AND deleted_at IS NULL", c.Param("id"), c.Param("slug")).Take(&member).Error; err != nil {
		handler.notFound(c)
		return
	}
	var requester WorkspaceMember
	if err := handler.db.WithContext(c.Request.Context()).Where("workspace_id = ? AND member_id = ? AND is_active = TRUE AND deleted_at IS NULL", member.WorkspaceID, user.ID).Take(&requester).Error; err != nil {
		handler.notFound(c)
		return
	}
	if member.ID == requester.ID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "You cannot remove yourself from the workspace. Please use leave workspace"})
		return
	}
	if requester.Role < member.Role {
		c.JSON(http.StatusBadRequest, gin.H{"error": "You cannot remove a user having role higher than you"})
		return
	}
	// Django currently compares the project membership's member_id with the
	// workspace-member row id on this removal path. Keep that behavior until a
	// separate compatibility change fixes both implementations together.
	onlyAdmin, err := handler.onlyProjectAdmin(c.Request.Context(), member.WorkspaceID, member.ID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if onlyAdmin {
		c.JSON(http.StatusBadRequest, gin.H{"error": "User is a part of some projects where they are the only admin, they should either leave that project or promote another user to admin."})
		return
	}
	now := handler.clock().UTC()
	err = handler.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Table("project_members").Where("workspace_id = ? AND member_id = ? AND is_active = TRUE AND deleted_at IS NULL", member.WorkspaceID, member.MemberID).Updates(map[string]any{"is_active": false, "updated_at": now}).Error; err != nil {
			return err
		}
		return tx.Model(&WorkspaceMember{}).Where("id = ?", member.ID).Updates(map[string]any{"is_active": false, "updated_at": now, "updated_by_id": user.ID}).Error
	})
	if err != nil {
		handler.internalError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (handler *Handler) memberLeave(c *gin.Context, user *auth.User) {
	if err := handler.invalidateMemberCaches(c.Request.Context(), c.Param("slug"), user.ID); err != nil {
		handler.internalError(c, err)
		return
	}
	var member WorkspaceMember
	if err := handler.db.WithContext(c.Request.Context()).Where("workspace_id = (SELECT id FROM workspaces WHERE slug = ? AND deleted_at IS NULL) AND member_id = ? AND is_active = TRUE AND deleted_at IS NULL", c.Param("slug"), user.ID).Take(&member).Error; err != nil {
		handler.notFound(c)
		return
	}
	if member.Role == roleAdmin {
		var admins int64
		if err := handler.db.WithContext(c.Request.Context()).Model(&WorkspaceMember{}).Where("workspace_id = ? AND role = ? AND is_active = TRUE AND deleted_at IS NULL", member.WorkspaceID, roleAdmin).Count(&admins).Error; err != nil {
			handler.internalError(c, err)
			return
		}
		if admins <= 1 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "You cannot leave the workspace as you are the only admin of the workspace you will have to either delete the workspace or promote another user to admin."})
			return
		}
	}
	onlyAdmin, err := handler.onlyProjectAdmin(c.Request.Context(), member.WorkspaceID, user.ID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if onlyAdmin {
		c.JSON(http.StatusBadRequest, gin.H{"error": "You are a part of some projects where you are the only admin, you should either leave the project or promote another user to admin."})
		return
	}
	now := handler.clock().UTC()
	err = handler.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Table("project_members").Where("workspace_id = ? AND member_id = ? AND is_active = TRUE AND deleted_at IS NULL", member.WorkspaceID, user.ID).Updates(map[string]any{"is_active": false, "updated_at": now}).Error; err != nil {
			return err
		}
		return tx.Model(&WorkspaceMember{}).Where("id = ?", member.ID).Updates(map[string]any{"is_active": false, "updated_at": now, "updated_by_id": user.ID}).Error
	})
	if err != nil {
		handler.internalError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (handler *Handler) memberMe(c *gin.Context, user *auth.User) {
	var member WorkspaceMember
	if err := handler.db.WithContext(c.Request.Context()).Where("workspace_id = (SELECT id FROM workspaces WHERE slug = ? AND deleted_at IS NULL) AND member_id = ? AND is_active = TRUE AND deleted_at IS NULL", c.Param("slug"), user.ID).Take(&member).Error; err != nil {
		handler.notFound(c)
		return
	}
	var drafts int64
	if err := handler.db.WithContext(c.Request.Context()).Table("draft_issues").Where("workspace_id = ? AND created_by_id = ? AND deleted_at IS NULL", member.WorkspaceID, user.ID).Count(&drafts).Error; err != nil {
		handler.internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, memberJSONWithDraft(member, drafts))
}

func (handler *Handler) memberViews(c *gin.Context, user *auth.User) {
	var request struct {
		ViewProps json.RawMessage `json:"view_props"`
	}
	if err := c.ShouldBindJSON(&request); err != nil || !json.Valid(request.ViewProps) {
		handler.invalidDetail(c)
		return
	}
	result := handler.db.WithContext(c.Request.Context()).Model(&WorkspaceMember{}).Where("workspace_id = (SELECT id FROM workspaces WHERE slug = ? AND deleted_at IS NULL) AND member_id = ? AND is_active = TRUE AND deleted_at IS NULL", c.Param("slug"), user.ID).Updates(map[string]any{"view_props": auth.JSONValue(request.ViewProps), "updated_at": handler.clock().UTC(), "updated_by_id": user.ID})
	if result.Error != nil {
		handler.internalError(c, result.Error)
		return
	}
	if result.RowsAffected == 0 {
		handler.notFound(c)
		return
	}
	c.Status(http.StatusNoContent)
}

func (handler *Handler) memberJSON(ctx context.Context, member WorkspaceMember, admin bool) (gin.H, error) {
	var user auth.User
	if err := handler.db.WithContext(ctx).Where("id = ?", member.MemberID).Take(&user).Error; err != nil {
		return nil, err
	}
	data := gin.H{
		"id": member.ID, "member": liteUserJSON(user, admin), "role": member.Role,
	}
	return data, nil
}

func (handler *Handler) fullMemberJSON(ctx context.Context, member WorkspaceMember, admin bool) (gin.H, error) {
	var user auth.User
	if err := handler.db.WithContext(ctx).Where("id = ?", member.MemberID).Take(&user).Error; err != nil {
		return nil, err
	}
	data := memberJSONWithDraft(member, 0)
	delete(data, "draft_issue_count")
	data["member"] = liteUserJSON(user, admin)
	return data, nil
}

func memberJSONWithDraft(member WorkspaceMember, count int64) gin.H {
	return gin.H{
		"id": member.ID, "created_at": member.CreatedAt, "updated_at": member.UpdatedAt,
		"created_by": member.CreatedByID, "updated_by": member.UpdatedByID, "deleted_at": member.DeletedAt,
		"workspace": member.WorkspaceID, "member": member.MemberID, "role": member.Role, "company_role": member.CompanyRole,
		"view_props": decodeJSON(member.ViewProps), "default_props": decodeJSON(member.DefaultProps), "issue_props": decodeJSON(member.IssueProps),
		"is_active": member.IsActive, "getting_started_checklist": decodeJSON(member.GettingStartedChecklist), "tips": decodeJSON(member.Tips), "explored_features": decodeJSON(member.ExploredFeatures), "draft_issue_count": count,
	}
}

func liteUserJSON(user auth.User, admin bool) gin.H {
	avatar := any(nil)
	if user.AvatarAssetID != nil {
		avatar = "/api/assets/v2/static/" + *user.AvatarAssetID + "/"
	} else if user.Avatar != "" {
		avatar = user.Avatar
	}
	data := gin.H{"id": user.ID, "first_name": user.FirstName, "last_name": user.LastName, "avatar": user.Avatar, "avatar_url": avatar, "is_bot": user.IsBot, "display_name": user.DisplayName}
	if admin {
		data["email"] = user.Email
		data["last_login_medium"] = user.LastLoginMedium
	}
	return data
}

func (handler *Handler) onlyProjectAdmin(ctx context.Context, workspaceID, memberID string) (bool, error) {
	var count int64
	err := handler.db.WithContext(ctx).Table("projects p").Joins("JOIN project_members pm ON pm.project_id = p.id AND pm.deleted_at IS NULL").
		Where("p.workspace_id = ? AND p.deleted_at IS NULL", workspaceID).
		Group("p.id").Having("COUNT(pm.id) = 1 AND SUM(CASE WHEN pm.member_id = ? AND pm.role = ? THEN 1 ELSE 0 END) = 1", memberID, roleAdmin).Count(&count).Error
	return count > 0, err
}

func workspaceIDSubquery(slug string) any {
	return gorm.Expr("(SELECT id FROM workspaces WHERE slug = ? AND deleted_at IS NULL)", slug)
}

func (handler *Handler) invalidate(ctx context.Context, patterns ...string) error {
	if handler.cache == nil {
		return nil
	}
	for _, pattern := range patterns {
		if err := handler.cache.InvalidatePattern(ctx, pattern); err != nil {
			return err
		}
	}
	return nil
}

func (handler *Handler) invalidateMemberCaches(ctx context.Context, slug, userID string) error {
	return handler.invalidate(
		ctx,
		"*/api/workspaces/"+slug+"/members/*",
		"/api/users/me/settings/:"+userID,
		"*api/users/me/workspaces/*",
	)
}

func (handler *Handler) invalidateInvitationCaches(ctx context.Context, userID string) error {
	return handler.invalidate(ctx, "/api/workspaces/", "*/api/users/me/workspaces/:"+userID+"*")
}

func (handler *Handler) configurationValue(ctx context.Context, key, fallback string) (string, error) {
	if fallback == "" {
		fallback = os.Getenv(key)
	}
	if !handler.settings.SkipEnvironmentConfig || handler.users == nil {
		return fallback, nil
	}
	return handler.users.ConfigurationValue(ctx, key, fallback)
}

func (handler *Handler) assetExists(ctx context.Context, assetID string) (bool, error) {
	var count int64
	err := handler.db.WithContext(ctx).Table("file_assets").Where("id = ? AND deleted_at IS NULL", assetID).Count(&count).Error
	return count > 0, err
}

func (handler *Handler) notFound(c *gin.Context) {
	c.JSON(http.StatusNotFound, gin.H{"error": "The required object does not exist."})
}

func (handler *Handler) invalidDetail(c *gin.Context) {
	c.JSON(http.StatusBadRequest, gin.H{"error": "Please provide valid detail"})
}

func (handler *Handler) internalError(c *gin.Context, err error) {
	c.Error(err)
	c.JSON(http.StatusInternalServerError, gin.H{"error": "Something went wrong please try again later"})
}

func validWorkspaceSlug(value string) bool {
	return len(value) <= 48 && workspaceSlugPattern.MatchString(value)
}

func containsURL(value string) bool {
	return workspaceURLPattern.MatchString(value)
}

func hasAlphanumeric(value string) bool {
	for _, runeValue := range value {
		if unicode.IsLetter(runeValue) || unicode.IsNumber(runeValue) {
			return true
		}
	}
	return false
}

func integerValue(raw json.RawMessage) (int, error) {
	var value int
	if err := json.Unmarshal(raw, &value); err == nil {
		return value, nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return 0, err
	}
	return strconv.Atoi(text)
}

func decodeJSON(value auth.JSONValue) any {
	if len(value) == 0 {
		return nil
	}
	var decoded any
	if json.Unmarshal(value, &decoded) != nil {
		return nil
	}
	return decoded
}

func defaultPropsJSON() auth.JSONValue {
	value, _ := json.Marshal(map[string]any{
		"filters":            map[string]any{"priority": nil, "state": nil, "state_group": nil, "assignees": nil, "created_by": nil, "labels": nil, "start_date": nil, "target_date": nil, "subscriber": nil},
		"display_filters":    map[string]any{"group_by": nil, "order_by": "-created_at", "type": nil, "sub_issue": true, "show_empty_groups": true, "layout": "list", "calendar_date_range": ""},
		"display_properties": map[string]any{"assignee": true, "attachment_count": true, "created_on": true, "due_date": true, "estimate": true, "key": true, "labels": true, "link": true, "priority": true, "start_date": true, "state": true, "sub_issue_count": true, "updated_on": true},
	})
	return auth.JSONValue(value)
}

func issuePropsJSON() auth.JSONValue {
	value, _ := json.Marshal(map[string]bool{"subscribed": true, "assigned": true, "created": true, "all_issues": true})
	return auth.JSONValue(value)
}

func emptyJSON() auth.JSONValue { return auth.JSONValue([]byte(`{}`)) }

func randomColor() string {
	value := make([]byte, 3)
	if _, err := rand.Read(value); err != nil {
		return "#3f76ff"
	}
	return fmt.Sprintf("#%x", value)
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

func isUniqueViolation(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "duplicate key") || strings.Contains(strings.ToLower(err.Error()), "unique constraint")
}

func invitationToken(secret string, email any, now time.Time) (string, error) {
	header, _ := json.Marshal(map[string]string{"alg": "HS256", "typ": "JWT"})
	payload, _ := json.Marshal(map[string]any{"email": email, "timestamp": float64(now.UnixNano()) / 1e9})
	encode := func(value []byte) string { return base64.RawURLEncoding.EncodeToString(value) }
	unsigned := encode(header) + "." + encode(payload)
	hash := hmac.New(sha256.New, []byte(secret))
	_, _ = hash.Write([]byte(unsigned))
	return unsigned + "." + encode(hash.Sum(nil)), nil
}

func invitationPublicJSON(inv WorkspaceInvite, workspace workspaceRow) gin.H {
	return gin.H{"id": inv.ID, "email": inv.Email, "workspace": workspaceLiteJSON(workspace.Workspace), "role": inv.Role, "message": inv.Message, "accepted": inv.Accepted, "responded_at": inv.RespondedAt, "created_at": inv.CreatedAt, "updated_at": inv.UpdatedAt, "created_by": inv.CreatedByID}
}

func (handler *Handler) invitationJSON(ctx context.Context, inv WorkspaceInvite) (gin.H, error) {
	var workspace workspaceRow
	if err := handler.db.WithContext(ctx).Table("workspaces w").Select("w.*").Where("w.id = ?", inv.WorkspaceID).Scan(&workspace).Error; err != nil {
		return nil, err
	}
	return gin.H{"id": inv.ID, "created_at": inv.CreatedAt, "updated_at": inv.UpdatedAt, "created_by": inv.CreatedByID, "updated_by": inv.UpdatedByID, "deleted_at": inv.DeletedAt, "workspace": workspaceLiteJSON(workspace.Workspace), "email": inv.Email, "accepted": inv.Accepted, "token": inv.Token, "message": inv.Message, "responded_at": inv.RespondedAt, "role": inv.Role, "invite_link": "/workspace-invitations/?invitation_id=" + inv.ID + "&slug=" + workspace.Slug + "&token=" + inv.Token}, nil
}

func workspaceLiteJSON(workspace Workspace) gin.H {
	logoURL := any(nil)
	if workspace.LogoAssetID != nil {
		logoURL = "/api/assets/v2/static/" + *workspace.LogoAssetID + "/"
	} else if workspace.Logo != nil && *workspace.Logo != "" {
		logoURL = *workspace.Logo
	}
	return gin.H{"name": workspace.Name, "slug": workspace.Slug, "id": workspace.ID, "logo_url": logoURL}
}

func (handler *Handler) invitationAdminRole(ctx context.Context, slug, userID string) (int, error) {
	return handler.workspaceRole(ctx, slug, userID)
}

func (handler *Handler) invitationList(c *gin.Context, user *auth.User) {
	role, err := handler.invitationAdminRole(c.Request.Context(), c.Param("slug"), user.ID)
	if err != nil {
		handler.notFound(c)
		return
	}
	if role != roleAdmin && role != roleMember {
		c.JSON(http.StatusForbidden, gin.H{"detail": "You do not have permission to perform this action."})
		return
	}
	var invites []WorkspaceInvite
	if err := handler.db.WithContext(c.Request.Context()).Where("workspace_id = (SELECT id FROM workspaces WHERE slug = ? AND deleted_at IS NULL) AND deleted_at IS NULL", c.Param("slug")).Order("created_at DESC").Find(&invites).Error; err != nil {
		handler.internalError(c, err)
		return
	}
	result := make([]gin.H, 0, len(invites))
	for _, invite := range invites {
		data, err := handler.invitationJSON(c.Request.Context(), invite)
		if err != nil {
			handler.internalError(c, err)
			return
		}
		result = append(result, data)
	}
	c.JSON(http.StatusOK, result)
}

func (handler *Handler) invitationCreate(c *gin.Context, user *auth.User) {
	role, err := handler.invitationAdminRole(c.Request.Context(), c.Param("slug"), user.ID)
	if err != nil {
		handler.notFound(c)
		return
	}
	if role != roleAdmin && role != roleMember {
		c.JSON(http.StatusForbidden, gin.H{"detail": "You do not have permission to perform this action."})
		return
	}
	var request struct {
		Emails []invitationCandidate `json:"emails"`
	}
	if err := c.ShouldBindJSON(&request); err != nil || len(request.Emails) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Emails are required"})
		return
	}
	var workspace Workspace
	if err := handler.db.WithContext(c.Request.Context()).Where("slug = ? AND deleted_at IS NULL", c.Param("slug")).Take(&workspace).Error; err != nil {
		handler.notFound(c)
		return
	}
	for _, candidate := range request.Emails {
		parsed, parseError := mail.ParseAddress(strings.TrimSpace(candidate.Email))
		if parseError != nil || parsed.Address != strings.TrimSpace(candidate.Email) || !strings.Contains(candidate.Email, "@") {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid email - %s provided a valid email address is required to send the invite", candidate.Email)})
			return
		}
		candidateRole, roleError := candidate.role()
		if roleError != nil {
			handler.invalidDetail(c)
			return
		}
		if candidateRole > role {
			c.JSON(http.StatusBadRequest, gin.H{"error": "You cannot invite a user with higher role"})
			return
		}
	}
	var existing []WorkspaceMember
	emails := make([]string, 0, len(request.Emails))
	for _, candidate := range request.Emails {
		emails = append(emails, strings.ToLower(strings.TrimSpace(candidate.Email)))
	}
	if err := handler.db.WithContext(c.Request.Context()).Model(&WorkspaceMember{}).Joins("JOIN users u ON u.id = workspace_members.member_id").Where("workspace_members.workspace_id = ? AND workspace_members.is_active = TRUE AND workspace_members.deleted_at IS NULL AND LOWER(u.email) IN ?", workspace.ID, emails).Find(&existing).Error; err != nil {
		handler.internalError(c, err)
		return
	}
	if len(existing) > 0 {
		users := make([]gin.H, 0, len(existing))
		for _, existingMember := range existing {
			member, memberError := handler.fullMemberJSON(c.Request.Context(), existingMember, false)
			if memberError != nil {
				handler.internalError(c, memberError)
				return
			}
			users = append(users, member)
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "Some users are already member of workspace", "workspace_users": users})
		return
	}
	now := handler.clock().UTC()
	invitations := make([]WorkspaceInvite, 0, len(request.Emails))
	for _, candidate := range request.Emails {
		candidateEmail := strings.ToLower(strings.TrimSpace(candidate.Email))
		candidateRole, roleError := candidate.role()
		if roleError != nil {
			handler.invalidDetail(c)
			return
		}
		token, tokenErr := invitationToken(handler.settings.SecretKey, map[string]any{"email": candidateEmail, "role": candidateRole}, now)
		if tokenErr != nil {
			handler.internalError(c, tokenErr)
			return
		}
		id, idErr := newUUID()
		if idErr != nil {
			handler.internalError(c, idErr)
			return
		}
		invitations = append(invitations, WorkspaceInvite{ID: id, CreatedAt: now, UpdatedAt: now, CreatedByID: &user.ID, WorkspaceID: workspace.ID, Email: candidateEmail, Token: token, Role: candidateRole})
	}
	err = handler.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		for index := range invitations {
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&invitations[index]).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if handler.tasks != nil {
		for _, invite := range invitations {
			if err := handler.tasks.PublishWorkspaceInvitation(c.Request.Context(), invite.Email, workspace.ID, invite.Token, handler.appBaseURL(c), user.Email); err != nil {
				handler.internalError(c, err)
				return
			}
		}
	}
	c.JSON(http.StatusOK, gin.H{"message": "Emails sent successfully"})
}

func (handler *Handler) invitationRetrieve(c *gin.Context, user *auth.User) {
	role, err := handler.invitationAdminRole(c.Request.Context(), c.Param("slug"), user.ID)
	if err != nil {
		handler.notFound(c)
		return
	}
	if role != roleAdmin && role != roleMember {
		c.JSON(http.StatusForbidden, gin.H{"detail": "You do not have permission to perform this action."})
		return
	}
	var invite WorkspaceInvite
	if err := handler.db.WithContext(c.Request.Context()).Where("id = ? AND workspace_id = (SELECT id FROM workspaces WHERE slug = ? AND deleted_at IS NULL) AND deleted_at IS NULL", c.Param("id"), c.Param("slug")).Take(&invite).Error; err != nil {
		handler.notFound(c)
		return
	}
	data, err := handler.invitationJSON(c.Request.Context(), invite)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, data)
}

func (handler *Handler) invitationPatch(c *gin.Context, user *auth.User) {
	role, err := handler.invitationAdminRole(c.Request.Context(), c.Param("slug"), user.ID)
	if err != nil {
		handler.notFound(c)
		return
	}
	if role != roleAdmin && role != roleMember {
		c.JSON(http.StatusForbidden, gin.H{"detail": "You do not have permission to perform this action."})
		return
	}
	var fields map[string]json.RawMessage
	if err := c.ShouldBindJSON(&fields); err != nil {
		handler.invalidDetail(c)
		return
	}
	updates := map[string]any{"updated_at": handler.clock().UTC(), "updated_by_id": user.ID}
	if raw, ok := fields["role"]; ok {
		value, valueError := integerValue(raw)
		if valueError != nil || (value != roleGuest && value != roleMember && value != roleAdmin) {
			handler.invalidDetail(c)
			return
		}
		if value > role {
			c.JSON(http.StatusBadRequest, gin.H{"error": "You cannot invite a user with higher role"})
			return
		}
		updates["role"] = value
	}
	if raw, ok := fields["accepted"]; ok {
		var value bool
		if json.Unmarshal(raw, &value) != nil {
			handler.invalidDetail(c)
			return
		}
		updates["accepted"] = value
	}
	result := handler.db.WithContext(c.Request.Context()).Model(&WorkspaceInvite{}).Where("id = ? AND workspace_id = (SELECT id FROM workspaces WHERE slug = ? AND deleted_at IS NULL) AND deleted_at IS NULL", c.Param("id"), c.Param("slug")).Updates(updates)
	if result.Error != nil {
		handler.internalError(c, result.Error)
		return
	}
	if result.RowsAffected == 0 {
		handler.notFound(c)
		return
	}
	var invite WorkspaceInvite
	if err := handler.db.WithContext(c.Request.Context()).Where("id = ?", c.Param("id")).Take(&invite).Error; err != nil {
		handler.notFound(c)
		return
	}
	data, err := handler.invitationJSON(c.Request.Context(), invite)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, data)
}

func (handler *Handler) invitationDelete(c *gin.Context, user *auth.User) {
	role, err := handler.invitationAdminRole(c.Request.Context(), c.Param("slug"), user.ID)
	if err != nil {
		handler.notFound(c)
		return
	}
	if role != roleAdmin && role != roleMember {
		c.JSON(http.StatusForbidden, gin.H{"detail": "You do not have permission to perform this action."})
		return
	}
	now := handler.clock().UTC()
	result := handler.db.WithContext(c.Request.Context()).Model(&WorkspaceInvite{}).Where("id = ? AND workspace_id = (SELECT id FROM workspaces WHERE slug = ? AND deleted_at IS NULL) AND deleted_at IS NULL", c.Param("id"), c.Param("slug")).Updates(map[string]any{"deleted_at": now, "updated_at": now, "updated_by_id": user.ID})
	if result.Error != nil {
		handler.internalError(c, result.Error)
		return
	}
	if result.RowsAffected == 0 {
		handler.notFound(c)
		return
	}
	if handler.tasks != nil {
		if err := handler.tasks.PublishSoftDeleteRelatedObjects(c.Request.Context(), "db", "workspacememberinvite", c.Param("id")); err != nil {
			handler.internalError(c, err)
			return
		}
	}
	c.Status(http.StatusNoContent)
}

func (handler *Handler) userInvitationList(c *gin.Context, user *auth.User) {
	var invites []WorkspaceInvite
	if err := handler.db.WithContext(c.Request.Context()).Where("email = ? AND deleted_at IS NULL", user.Email).Order("created_at DESC").Find(&invites).Error; err != nil {
		handler.internalError(c, err)
		return
	}
	result := make([]gin.H, 0, len(invites))
	for _, invite := range invites {
		data, err := handler.invitationJSON(c.Request.Context(), invite)
		if err != nil {
			handler.internalError(c, err)
			return
		}
		result = append(result, data)
	}
	c.JSON(http.StatusOK, result)
}

func (handler *Handler) userInvitationAccept(c *gin.Context, user *auth.User) {
	if err := handler.invalidateInvitationCaches(c.Request.Context(), user.ID); err != nil {
		handler.internalError(c, err)
		return
	}
	var request struct {
		Invitations []string `json:"invitations"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		handler.invalidDetail(c)
		return
	}
	for _, invitationID := range request.Invitations {
		if !uuidPattern.MatchString(invitationID) {
			handler.invalidDetail(c)
			return
		}
	}
	var invites []WorkspaceInvite
	if err := handler.db.WithContext(c.Request.Context()).Where("id IN ? AND email = ? AND deleted_at IS NULL", request.Invitations, user.Email).Order("created_at DESC").Find(&invites).Error; err != nil {
		handler.internalError(c, err)
		return
	}
	for _, invite := range invites {
		var slug string
		if err := handler.db.WithContext(c.Request.Context()).Table("workspaces").Where("id = ? AND deleted_at IS NULL", invite.WorkspaceID).Pluck("slug", &slug).Error; err != nil {
			handler.internalError(c, err)
			return
		}
		if slug != "" {
			if err := handler.invalidate(c.Request.Context(), "*/api/workspaces/"+slug+"/members/*"); err != nil {
				handler.internalError(c, err)
				return
			}
		}
	}
	now := handler.clock().UTC()
	err := handler.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		for _, invite := range invites {
			var existing WorkspaceMember
			existingErr := tx.Where("workspace_id = ? AND member_id = ? AND deleted_at IS NULL", invite.WorkspaceID, user.ID).Take(&existing).Error
			if existingErr == nil {
				if err := tx.Table("workspace_members").Where("id = ?", existing.ID).Updates(map[string]any{"is_active": true, "role": invite.Role}).Error; err != nil {
					return err
				}
			} else if errors.Is(existingErr, gorm.ErrRecordNotFound) {
				id, err := newUUID()
				if err != nil {
					return err
				}
				member := WorkspaceMember{ID: id, CreatedAt: now, UpdatedAt: now, CreatedByID: &user.ID, WorkspaceID: invite.WorkspaceID, MemberID: user.ID, Role: invite.Role, IsActive: true, ViewProps: defaultPropsJSON(), DefaultProps: defaultPropsJSON(), IssueProps: issuePropsJSON(), GettingStartedChecklist: emptyJSON(), Tips: emptyJSON(), ExploredFeatures: emptyJSON()}
				if err := tx.Create(&member).Error; err != nil {
					return err
				}
			} else {
				return existingErr
			}
			if err := tx.Table("workspace_member_invites").Where("id = ?", invite.ID).Update("deleted_at", now).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		handler.internalError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (handler *Handler) invitationPublic(c *gin.Context) {
	var invite WorkspaceInvite
	if err := handler.db.WithContext(c.Request.Context()).Where("id = ? AND workspace_id = (SELECT id FROM workspaces WHERE slug = ? AND deleted_at IS NULL) AND deleted_at IS NULL", c.Param("id"), c.Param("slug")).Take(&invite).Error; err != nil {
		handler.notFound(c)
		return
	}
	var workspace workspaceRow
	if err := handler.db.WithContext(c.Request.Context()).Where("id = ?", invite.WorkspaceID).Take(&workspace.Workspace).Error; err != nil {
		handler.notFound(c)
		return
	}
	c.JSON(http.StatusOK, invitationPublicJSON(invite, workspace))
}

func (handler *Handler) invitationJoin(c *gin.Context) {
	if err := handler.invalidate(
		c.Request.Context(),
		"/api/workspaces/",
		"*/api/users/me/workspaces/*",
		"*/api/workspaces/"+c.Param("slug")+"/members/*",
		"*/api/users/me/settings/*",
	); err != nil {
		handler.internalError(c, err)
		return
	}
	var request struct {
		Token    string `json:"token"`
		Accepted bool   `json:"accepted"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		handler.invalidDetail(c)
		return
	}
	var invite WorkspaceInvite
	if err := handler.db.WithContext(c.Request.Context()).Where("id = ? AND workspace_id = (SELECT id FROM workspaces WHERE slug = ? AND deleted_at IS NULL) AND deleted_at IS NULL", c.Param("id"), c.Param("slug")).Take(&invite).Error; err != nil {
		handler.notFound(c)
		return
	}
	if request.Token == "" || request.Token != invite.Token {
		c.JSON(http.StatusForbidden, gin.H{"error": "You do not have permission to join the workspace"})
		return
	}
	if handler.sessions == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Authentication required to accept workspace invitation"})
		return
	}
	user, _, err := handler.sessions.Authenticate(c.Request.Context(), c.Request, c.Writer)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Authentication required to accept workspace invitation"})
		return
	}
	if !strings.EqualFold(user.Email, invite.Email) {
		c.JSON(http.StatusForbidden, gin.H{"error": "You do not have permission to accept this invitation"})
		return
	}
	if invite.RespondedAt != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "You have already responded to the invitation request"})
		return
	}
	now := handler.clock().UTC()
	err = handler.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&WorkspaceInvite{}).Where("id = ?", invite.ID).Updates(map[string]any{"accepted": request.Accepted, "responded_at": now, "updated_at": now, "updated_by_id": user.ID}).Error; err != nil {
			return err
		}
		if !request.Accepted {
			return nil
		}
		var member WorkspaceMember
		memberErr := tx.Where("workspace_id = ? AND member_id = ? AND deleted_at IS NULL", invite.WorkspaceID, user.ID).Take(&member).Error
		if memberErr == nil {
			if err := tx.Model(&member).Updates(map[string]any{"is_active": true, "role": invite.Role, "updated_at": now, "updated_by_id": user.ID}).Error; err != nil {
				return err
			}
		} else if errors.Is(memberErr, gorm.ErrRecordNotFound) {
			id, err := newUUID()
			if err != nil {
				return err
			}
			member = WorkspaceMember{ID: id, CreatedAt: now, UpdatedAt: now, CreatedByID: &user.ID, WorkspaceID: invite.WorkspaceID, MemberID: user.ID, Role: invite.Role, IsActive: true, ViewProps: defaultPropsJSON(), DefaultProps: defaultPropsJSON(), IssueProps: issuePropsJSON(), GettingStartedChecklist: emptyJSON(), Tips: emptyJSON(), ExploredFeatures: emptyJSON()}
			if err := tx.Create(&member).Error; err != nil {
				return err
			}
		} else {
			return memberErr
		}
		if err := tx.Model(&auth.Profile{}).Where("user_id = ?", user.ID).Updates(map[string]any{"last_workspace_id": invite.WorkspaceID, "updated_at": now}).Error; err != nil {
			return err
		}
		return tx.Model(&WorkspaceInvite{}).Where("id = ?", invite.ID).Updates(map[string]any{"deleted_at": now, "updated_at": now, "updated_by_id": user.ID}).Error
	})
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if request.Accepted && handler.tasks != nil {
		if err := handler.tasks.PublishSoftDeleteRelatedObjects(c.Request.Context(), "db", "workspacememberinvite", invite.ID); err != nil {
			handler.internalError(c, err)
			return
		}
	}
	if request.Accepted {
		c.JSON(http.StatusOK, gin.H{"message": "Workspace Invitation Accepted"})
	} else {
		c.JSON(http.StatusOK, gin.H{"message": "Workspace Invitation was not accepted"})
	}
}

func (handler *Handler) appBaseURL(c *gin.Context) string {
	if baseURL := strings.TrimRight(handler.settings.AppBaseURL, "/"); baseURL != "" {
		return baseURL
	}
	scheme := "http"
	if strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https") || c.Request.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + c.Request.Host
}
