package user

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	redis "github.com/redis/go-redis/v9"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"github.com/yldm-tech/pace/apps/api-go/internal/drf"
	"github.com/yldm-tech/pace/apps/api-go/internal/storage"
	"gorm.io/gorm"
)

type Handler struct {
	db       *gorm.DB
	storage  *storage.Store
	sessions *auth.SessionManager
	users    auth.Repository
	settings Settings
	clock    func() time.Time
	redis    redis.UniversalClient
	tasks    *auth.CeleryPublisher
}

type Settings struct {
	AppBaseURL string
	// FileSizeLimit is settings.FILE_SIZE_LIMIT, the cap every reserved upload is clamped to.
	FileSizeLimit int64
}

func NewHandler(db *gorm.DB, sessions *auth.SessionManager, users auth.Repository, settings Settings) *Handler {
	return &Handler{db: db, sessions: sessions, users: users, settings: settings, clock: time.Now}
}

func (handler *Handler) SetRedis(client redis.UniversalClient)    { handler.redis = client }
func (handler *Handler) SetTasks(publisher *auth.CeleryPublisher) { handler.tasks = publisher }

func (handler *Handler) Register(router gin.IRouter) {
	handler.registerAssetRoutes(router)
	handler.registerTokenRoutes(router)
	router.GET("/api/users/me/", handler.authenticated(handler.me))
	router.PATCH("/api/users/me/", handler.authenticated(handler.updateMe))
	router.DELETE("/api/users/me/", handler.authenticated(handler.deactivate))
	router.GET("/api/users/session/", handler.session)
	router.GET("/api/users/me/settings/", handler.authenticated(handler.settingsEndpoint))
	router.POST("/api/users/me/email/generate-code/", handler.authenticated(handler.generateEmailCode))
	router.PATCH("/api/users/me/email/", handler.authenticated(handler.updateEmail))
	router.GET("/api/users/me/profile/", handler.authenticated(handler.profileGet))
	router.PATCH("/api/users/me/profile/", handler.authenticated(handler.profilePatch))
	router.GET("/api/users/me/accounts/", handler.authenticated(handler.accountsGet))
	router.GET("/api/users/me/accounts/:id/", handler.authenticated(handler.accountGet))
	router.DELETE("/api/users/me/accounts/:id/", handler.authenticated(handler.accountDelete))
	router.GET("/api/users/me/instance-admin/", handler.authenticated(handler.instanceAdmin))
	router.PATCH("/api/users/me/onboard/", handler.authenticated(handler.onboard))
	router.PATCH("/api/users/me/tour-completed/", handler.authenticated(handler.tourCompleted))
}

func (handler *Handler) authenticated(next func(*gin.Context, *auth.User)) gin.HandlerFunc {
	return func(c *gin.Context) {
		user, _, err := handler.sessions.Authenticate(c.Request.Context(), c.Request, c.Writer)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"detail": "Authentication credentials were not provided."})
			return
		}
		next(c, user)
	}
}

func (handler *Handler) session(c *gin.Context) {
	user, _, err := handler.sessions.Authenticate(c.Request.Context(), c.Request, c.Writer)
	if err != nil {
		drf.Respond(c, http.StatusOK, gin.H{"is_authenticated": false})
		return
	}
	drf.Respond(c, http.StatusOK, gin.H{"is_authenticated": true, "user": userMe(user)})
}

func (handler *Handler) me(c *gin.Context, user *auth.User) {
	c.Header("Cache-Control", "private, max-age=12")
	c.Header("Vary", "Cookie")
	drf.Respond(c, http.StatusOK, userMe(user))
}

// liteMe is the external API's view of the caller. It is served by internal/externalapi rather than here, because that path authenticates with a key rather than a session — routing it through this package refused every integration that called it. The function stays because the shape is this package's to own.
func (handler *Handler) liteMe(c *gin.Context, user *auth.User) {
	drf.Respond(c, http.StatusOK, gin.H{
		"id": user.ID, "first_name": user.FirstName, "last_name": user.LastName, "email": user.Email,
		"avatar": user.Avatar, "avatar_url": avatarURL(user), "display_name": user.DisplayName,
	})
}

func (handler *Handler) settingsEndpoint(c *gin.Context, user *auth.User) {
	var profile Profile
	if err := handler.db.WithContext(c.Request.Context()).Where("user_id = ?", user.ID).Take(&profile).Error; err != nil {
		handler.notFound(c)
		return
	}
	var inviteCount int64
	handler.db.WithContext(c.Request.Context()).Table("workspace_member_invites").Where("email = ? AND deleted_at IS NULL", user.Email).Count(&inviteCount)
	type workspace struct {
		ID   string `gorm:"column:id"`
		Slug string `gorm:"column:slug"`
		Name string `gorm:"column:name"`
	}
	var current workspace
	if profile.LastWorkspaceID != nil {
		handler.db.WithContext(c.Request.Context()).Table("workspaces w").
			Select("w.id, w.slug, w.name").Joins("JOIN workspace_members wm ON wm.workspace_id = w.id").
			Where("w.id = ? AND wm.member_id = ? AND wm.is_active = ? AND wm.deleted_at IS NULL", *profile.LastWorkspaceID, user.ID, true).Take(&current)
	}
	response := gin.H{"last_workspace_id": nil, "last_workspace_slug": nil, "fallback_workspace_id": nil, "fallback_workspace_slug": nil, "invites": inviteCount}
	if current.ID != "" {
		response["last_workspace_id"], response["last_workspace_slug"] = current.ID, current.Slug
		response["fallback_workspace_id"], response["fallback_workspace_slug"] = current.ID, current.Slug
	} else {
		var fallback workspace
		handler.db.WithContext(c.Request.Context()).Table("workspaces w").Select("w.id, w.slug").
			Joins("JOIN workspace_members wm ON wm.workspace_id = w.id").
			Where("wm.member_id = ? AND wm.is_active = ? AND wm.deleted_at IS NULL", user.ID, true).
			Order("w.created_at ASC").Take(&fallback)
		if fallback.ID != "" {
			response["fallback_workspace_id"], response["fallback_workspace_slug"] = fallback.ID, fallback.Slug
		}
	}
	c.Header("Cache-Control", "private, max-age=12")
	c.Header("Vary", "Cookie")
	drf.Respond(c, http.StatusOK, gin.H{"id": user.ID, "email": user.Email, "workspace": response})
}

func (handler *Handler) updateMe(c *gin.Context, user *auth.User) {
	var fields map[string]any
	if err := c.ShouldBindJSON(&fields); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Please provide valid detail"})
		return
	}
	updates := make(map[string]any)
	for _, key := range []string{"display_name", "first_name", "last_name", "avatar", "cover_image", "is_password_expired", "is_password_reset_required", "is_email_valid", "user_timezone"} {
		if value, exists := fields[key]; exists {
			updates[key] = value
		}
	}
	if first, ok := updates["first_name"].(string); ok && strings.Contains(first, "://") {
		c.JSON(http.StatusBadRequest, gin.H{"first_name": []string{"First name cannot contain a URL."}})
		return
	}
	if last, ok := updates["last_name"].(string); ok && strings.Contains(last, "://") {
		c.JSON(http.StatusBadRequest, gin.H{"last_name": []string{"Last name cannot contain a URL."}})
		return
	}
	if len(updates) > 0 {
		updates["updated_at"] = handler.clock().UTC()
		if err := handler.db.WithContext(c.Request.Context()).Model(&auth.User{}).Where("id = ?", user.ID).Updates(updates).Error; err != nil {
			handler.internalError(c, err)
			return
		}
		refreshed, err := handler.users.FindUserByID(c.Request.Context(), user.ID)
		if err == nil {
			user = refreshed
		}
	}
	drf.Respond(c, http.StatusOK, fullUser(user))
}

func (handler *Handler) profileGet(c *gin.Context, user *auth.User) {
	var profile Profile
	if err := handler.db.WithContext(c.Request.Context()).Where("user_id = ?", user.ID).Take(&profile).Error; err != nil {
		handler.notFound(c)
		return
	}
	c.Header("Cache-Control", "private, max-age=12")
	c.Header("Vary", "Cookie")
	drf.Respond(c, http.StatusOK, profileJSON(profile))
}

func (handler *Handler) profilePatch(c *gin.Context, user *auth.User) {
	var fields map[string]any
	if err := c.ShouldBindJSON(&fields); err != nil {
		handler.invalidDetail(c)
		return
	}
	allowed := map[string]bool{"theme": true, "is_app_rail_docked": true, "is_tour_completed": true, "onboarding_step": true, "use_case": true, "role": true, "is_onboarded": true, "last_workspace_id": true, "billing_address_country": true, "billing_address": true, "has_billing_address": true, "company_name": true, "notification_view_mode": true, "is_smooth_cursor_enabled": true, "is_mobile_onboarded": true, "mobile_onboarding_step": true, "mobile_timezone_auto_set": true, "language": true, "start_of_the_week": true, "goals": true, "background_color": true, "is_navigation_tour_completed": true, "has_marketing_email_consent": true, "is_subscribed_to_changelog": true, "product_tour": true}
	updates := make(map[string]any)
	for key, value := range fields {
		if !allowed[key] {
			continue
		}
		if _, ok := map[string]bool{"theme": true, "onboarding_step": true, "billing_address": true, "mobile_onboarding_step": true, "goals": true, "product_tour": true}[key]; ok {
			encoded, err := json.Marshal(value)
			if err != nil {
				handler.invalidDetail(c)
				return
			}
			updates[key] = auth.JSONValue(encoded)
		} else {
			updates[key] = value
		}
	}
	if len(updates) == 0 {
		handler.invalidDetail(c)
		return
	}
	updates["updated_at"] = handler.clock().UTC()
	if err := handler.db.WithContext(c.Request.Context()).Model(&Profile{}).Where("user_id = ?", user.ID).Updates(updates).Error; err != nil {
		handler.internalError(c, err)
		return
	}
	var profile Profile
	if err := handler.db.WithContext(c.Request.Context()).Where("user_id = ?", user.ID).Take(&profile).Error; err != nil {
		handler.notFound(c)
		return
	}
	drf.Respond(c, http.StatusOK, profileJSON(profile))
}

func (handler *Handler) accountsGet(c *gin.Context, user *auth.User) {
	var accounts []Account
	if err := handler.db.WithContext(c.Request.Context()).Where("user_id = ?", user.ID).Find(&accounts).Error; err != nil {
		handler.internalError(c, err)
		return
	}
	result := make([]gin.H, 0, len(accounts))
	for _, account := range accounts {
		result = append(result, accountJSON(account))
	}
	drf.Respond(c, http.StatusOK, result)
}

func (handler *Handler) accountGet(c *gin.Context, user *auth.User) {
	var account Account
	if err := handler.db.WithContext(c.Request.Context()).Where("id = ? AND user_id = ?", c.Param("id"), user.ID).Take(&account).Error; err != nil {
		handler.notFound(c)
		return
	}
	drf.Respond(c, http.StatusOK, accountJSON(account))
}

func (handler *Handler) accountDelete(c *gin.Context, user *auth.User) {
	result := handler.db.WithContext(c.Request.Context()).Where("id = ? AND user_id = ?", c.Param("id"), user.ID).Delete(&Account{})
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

func (handler *Handler) instanceAdmin(c *gin.Context, user *auth.User) {
	var count int64
	err := handler.db.WithContext(c.Request.Context()).Table("instance_admins ia").Joins("JOIN instances i ON i.id = ia.instance_id").Where("ia.user_id = ? AND i.deleted_at IS NULL", user.ID).Count(&count).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, gin.H{"is_instance_admin": count > 0})
}

func (handler *Handler) onboard(c *gin.Context, user *auth.User) {
	var request struct {
		IsOnboarded bool `json:"is_onboarded"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		handler.invalidDetail(c)
		return
	}
	if err := handler.db.WithContext(c.Request.Context()).Model(&Profile{}).Where("user_id = ?", user.ID).Update("is_onboarded", request.IsOnboarded).Error; err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, gin.H{"message": "Updated successfully"})
}

func (handler *Handler) tourCompleted(c *gin.Context, user *auth.User) {
	var request struct {
		IsTourCompleted bool `json:"is_tour_completed"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		handler.invalidDetail(c)
		return
	}
	if err := handler.db.WithContext(c.Request.Context()).Model(&Profile{}).Where("user_id = ?", user.ID).Update("is_tour_completed", request.IsTourCompleted).Error; err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, gin.H{"message": "Updated successfully"})
}

func (handler *Handler) generateEmailCode(c *gin.Context, user *auth.User) {
	if handler.redis == nil {
		handler.internalError(c, errors.New("email verification store is unavailable"))
		return
	}
	var request struct {
		Email string `json:"email"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		handler.invalidDetail(c)
		return
	}
	email := strings.ToLower(strings.TrimSpace(request.Email))
	if !validEmail(email) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid email format"})
		return
	}
	if email == strings.ToLower(user.Email) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "New email must be different from current email"})
		return
	}
	if existing, err := handler.users.FindUserByEmail(c.Request.Context(), email); err == nil && existing != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "An account with this email already exists"})
		return
	} else if err != nil && !errors.Is(err, auth.ErrNotFound) {
		handler.internalError(c, err)
		return
	}
	key := "magic_email_update_" + user.ID + "_" + email
	countKey := key + ":requests"
	count, err := handler.redis.Incr(c.Request.Context(), countKey).Result()
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if count == 1 {
		handler.redis.Expire(c.Request.Context(), countKey, time.Hour)
	}
	if count > 3 {
		c.JSON(http.StatusTooManyRequests, gin.H{"detail": "Request was throttled."})
		return
	}
	bytes := make([]byte, 4)
	if _, err := rand.Read(bytes); err != nil {
		handler.internalError(c, err)
		return
	}
	token := fmt.Sprintf("%06d", uint32(bytes[0])<<24|uint32(bytes[1])<<16|uint32(bytes[2])<<8|uint32(bytes[3]))
	token = token[len(token)-6:]
	if err := handler.redis.Set(c.Request.Context(), key, token, 10*time.Minute).Err(); err != nil {
		handler.internalError(c, err)
		return
	}
	if handler.tasks != nil {
		if err := handler.tasks.PublishEmailUpdateCode(c.Request.Context(), email, token); err != nil {
			handler.internalError(c, err)
			return
		}
	}
	drf.Respond(c, http.StatusOK, gin.H{"message": "Verification code sent to email"})
}

func (handler *Handler) updateEmail(c *gin.Context, user *auth.User) {
	if handler.redis == nil {
		handler.internalError(c, errors.New("email verification store is unavailable"))
		return
	}
	var request struct {
		Email string `json:"email"`
		Code  string `json:"code"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		handler.invalidDetail(c)
		return
	}
	email := strings.ToLower(strings.TrimSpace(request.Email))
	if !validEmail(email) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid email format"})
		return
	}
	if email == strings.ToLower(user.Email) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "New email must be different from current email"})
		return
	}
	if request.Code == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Verification code is required"})
		return
	}
	key := "magic_email_update_" + user.ID + "_" + email
	stored, err := handler.redis.Get(c.Request.Context(), key).Result()
	if errors.Is(err, redis.Nil) || stored == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Verification code has expired or is invalid"})
		return
	}
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if stored != request.Code {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid verification code"})
		return
	}
	if existing, err := handler.users.FindUserByEmail(c.Request.Context(), email); err == nil && existing != nil && existing.ID != user.ID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "An account with this email already exists"})
		return
	} else if err != nil && !errors.Is(err, auth.ErrNotFound) {
		handler.internalError(c, err)
		return
	}
	oldEmail := user.Email
	if err := handler.db.WithContext(c.Request.Context()).Model(&auth.User{}).Where("id = ?", user.ID).Updates(map[string]any{"email": email, "is_email_verified": false, "updated_at": handler.clock().UTC()}).Error; err != nil {
		handler.internalError(c, err)
		return
	}
	_ = handler.redis.Del(c.Request.Context(), key).Err()
	_ = handler.sessions.Logout(c.Request.Context(), c.Request)
	handler.sessions.DeleteCookie(c.Writer)
	if handler.tasks != nil {
		_ = handler.tasks.PublishEmailUpdateConfirmation(c.Request.Context(), email)
		_ = handler.tasks.PublishEmailUpdateConfirmation(c.Request.Context(), oldEmail)
	}
	refreshed, _ := handler.users.FindUserByID(c.Request.Context(), user.ID)
	if refreshed == nil {
		refreshed = user
		refreshed.Email = email
	}
	drf.Respond(c, http.StatusOK, userMe(refreshed))
}

func (handler *Handler) deactivate(c *gin.Context, user *auth.User) {
	ctx := c.Request.Context()
	var adminCount int64
	if err := handler.db.WithContext(ctx).Table("instance_admins").Where("user_id = ?", user.ID).Count(&adminCount).Error; err != nil {
		handler.internalError(c, err)
		return
	}
	if adminCount > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "You cannot deactivate your account since you are an instance admin"})
		return
	}
	for _, membershipScope := range []struct {
		table string
		key   string
		error string
	}{
		{table: "project_members", key: "project_id", error: "You cannot deactivate account as you are the only admin in some projects."},
		{table: "workspace_members", key: "workspace_id", error: "You cannot deactivate account as you are the only admin in some workspaces."},
	} {
		var memberships []struct {
			ID      string `gorm:"column:id"`
			Role    int16  `gorm:"column:role"`
			ScopeID string `gorm:"column:scope_id"`
		}
		if err := handler.db.WithContext(ctx).Table(membershipScope.table).Select("id, role, "+membershipScope.key+" AS scope_id").Where("member_id = ? AND is_active = ? AND deleted_at IS NULL", user.ID, true).Find(&memberships).Error; err != nil {
			handler.internalError(c, err)
			return
		}
		if len(memberships) == 0 {
			continue
		}
		for _, membership := range memberships {
			var total, otherAdmins int64
			if err := handler.db.WithContext(ctx).Table(membershipScope.table).Where(membershipScope.key+" = ? AND is_active = ? AND deleted_at IS NULL", membership.ScopeID, true).Count(&total).Error; err != nil {
				handler.internalError(c, err)
				return
			}
			if err := handler.db.WithContext(ctx).Table(membershipScope.table).Where(membershipScope.key+" = ? AND member_id <> ? AND role = ? AND is_active = ? AND deleted_at IS NULL", membership.ScopeID, user.ID, 20, true).Count(&otherAdmins).Error; err != nil {
				handler.internalError(c, err)
				return
			}
			if otherAdmins == 0 && total > 1 {
				c.JSON(http.StatusBadRequest, gin.H{"error": membershipScope.error})
				return
			}
		}
		if err := handler.db.WithContext(ctx).Table(membershipScope.table).Where("member_id = ? AND is_active = ? AND deleted_at IS NULL", user.ID, true).Update("is_active", false).Error; err != nil {
			handler.internalError(c, err)
			return
		}
	}
	now := handler.clock().UTC()
	password, err := auth.HashPassword(randomPassword())
	if err != nil {
		handler.internalError(c, err)
		return
	}
	err = handler.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Table("workspace_member_invites").Where("email = ? AND deleted_at IS NULL", user.Email).Update("deleted_at", now).Error; err != nil {
			return err
		}
		if err := tx.Exec("DELETE FROM sessions WHERE user_id = ?", user.ID).Error; err != nil {
			return err
		}
		if err := tx.Model(&Profile{}).Where("user_id = ?", user.ID).Updates(map[string]any{"last_workspace_id": nil, "is_tour_completed": false, "is_onboarded": false, "onboarding_step": auth.JSONValue(`{"workspace_join":false,"profile_complete":false,"workspace_create":false,"workspace_invite":false}`), "updated_at": now}).Error; err != nil {
			return err
		}
		return tx.Model(&auth.User{}).Where("id = ?", user.ID).Updates(map[string]any{"password": password, "is_password_autoset": true, "is_active": false, "last_logout_ip": clientIP(c), "last_logout_time": now, "updated_at": now}).Error
	})
	if err != nil {
		handler.internalError(c, err)
		return
	}
	handler.sessions.DeleteCookie(c.Writer)
	c.Status(http.StatusNoContent)
}

func userMe(user *auth.User) gin.H {
	return gin.H{"id": user.ID, "avatar": user.Avatar, "cover_image": user.CoverImage, "avatar_url": avatarURL(user), "cover_image_url": coverURL(user), "date_joined": user.DateJoined, "display_name": user.DisplayName, "email": user.Email, "first_name": user.FirstName, "last_name": user.LastName, "is_active": user.IsActive, "is_bot": user.IsBot, "is_email_verified": user.IsEmailVerified, "user_timezone": user.UserTimezone, "username": user.Username, "is_password_autoset": user.IsPasswordAutoset, "last_login_medium": user.LastLoginMedium, "last_login_time": user.LastLoginTime}
}

func fullUser(user *auth.User) gin.H {
	return gin.H{"id": user.ID, "last_login": user.LastLogin, "username": user.Username, "mobile_number": user.MobileNumber, "email": user.Email, "display_name": user.DisplayName, "first_name": user.FirstName, "last_name": user.LastName, "avatar": user.Avatar, "avatar_asset": user.AvatarAssetID, "cover_image": user.CoverImage, "cover_image_asset": user.CoverImageAssetID, "date_joined": user.DateJoined, "created_at": user.CreatedAt, "updated_at": user.UpdatedAt, "last_location": user.LastLocation, "created_location": user.CreatedLocation, "is_superuser": user.IsSuperuser, "is_managed": user.IsManaged, "is_password_expired": user.IsPasswordExpired, "is_active": user.IsActive, "is_staff": user.IsStaff, "is_email_verified": user.IsEmailVerified, "is_password_autoset": user.IsPasswordAutoset, "is_password_reset_required": user.IsPasswordResetRequired, "token": user.Token, "last_active": user.LastActive, "last_login_time": user.LastLoginTime, "last_logout_time": user.LastLogoutTime, "last_login_ip": user.LastLoginIP, "last_logout_ip": user.LastLogoutIP, "last_login_medium": user.LastLoginMedium, "last_login_uagent": user.LastLoginUserAgent, "token_updated_at": user.TokenUpdatedAt, "is_bot": user.IsBot, "bot_type": user.BotType, "user_timezone": user.UserTimezone, "is_email_valid": user.IsEmailValid, "masked_at": user.MaskedAt}
}

func profileJSON(profile Profile) gin.H {
	return gin.H{"id": profile.ID, "user": profile.UserID, "theme": decodeJSON(profile.Theme), "is_app_rail_docked": profile.IsAppRailDocked, "is_tour_completed": profile.IsTourCompleted, "onboarding_step": decodeJSON(profile.OnboardingStep), "use_case": profile.UseCase, "role": profile.Role, "is_onboarded": profile.IsOnboarded, "last_workspace_id": profile.LastWorkspaceID, "billing_address_country": profile.BillingAddressCountry, "billing_address": decodeJSON(profile.BillingAddress), "has_billing_address": profile.HasBillingAddress, "company_name": profile.CompanyName, "notification_view_mode": profile.NotificationViewMode, "is_smooth_cursor_enabled": profile.IsSmoothCursorEnabled, "is_mobile_onboarded": profile.IsMobileOnboarded, "mobile_onboarding_step": decodeJSON(profile.MobileOnboardingStep), "mobile_timezone_auto_set": profile.MobileTimezoneAutoSet, "language": profile.Language, "start_of_the_week": profile.StartOfTheWeek, "goals": decodeJSON(profile.Goals), "background_color": profile.BackgroundColor, "is_navigation_tour_completed": profile.IsNavigationTourComplete, "has_marketing_email_consent": profile.HasMarketingConsent, "is_subscribed_to_changelog": profile.IsSubscribedToChangelog, "product_tour": decodeJSON(profile.ProductTour), "created_at": profile.CreatedAt, "updated_at": profile.UpdatedAt}
}

func accountJSON(account Account) gin.H {
	return gin.H{"id": account.ID, "user": account.UserID, "provider_account_id": account.ProviderAccountID, "provider": account.Provider, "access_token": account.AccessToken, "access_token_expired_at": account.AccessTokenExpiredAt, "refresh_token": account.RefreshToken, "refresh_token_expired_at": account.RefreshTokenExpiredAt, "last_connected_at": account.LastConnectedAt, "id_token": account.IDToken, "metadata": decodeJSON(account.Metadata), "created_at": account.CreatedAt, "updated_at": account.UpdatedAt}
}

func decodeJSON(value auth.JSONValue) any {
	return drf.DecodeJSON([]byte(value))
}

func avatarURL(user *auth.User) any {
	if user.AvatarAssetID != nil {
		return "/api/assets/v2/static/" + *user.AvatarAssetID + "/"
	}
	if user.Avatar != "" {
		return user.Avatar
	}
	return nil
}

func coverURL(user *auth.User) any {
	if user.CoverImageAssetID != nil {
		return "/api/assets/v2/static/" + *user.CoverImageAssetID + "/"
	}
	if user.CoverImage != nil && *user.CoverImage != "" {
		return *user.CoverImage
	}
	return nil
}

func randomPassword() string {
	value := make([]byte, 24)
	_, _ = rand.Read(value)
	return hex.EncodeToString(value)
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

func validEmail(value string) bool {
	parts := strings.Split(value, "@")
	return len(value) <= 320 && len(parts) == 2 && parts[0] != "" && strings.Contains(parts[1], ".")
}

func clientIP(c *gin.Context) string {
	if forwarded := c.GetHeader("X-Forwarded-For"); forwarded != "" {
		return strings.TrimSpace(strings.Split(forwarded, ",")[0])
	}
	host, _, err := strings.Cut(c.Request.RemoteAddr, ":")
	if err {
		return host
	}
	return c.Request.RemoteAddr
}
