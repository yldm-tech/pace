package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrNotFound = errors.New("not found")

type User struct {
	ID                      string     `gorm:"column:id;type:uuid;primaryKey"`
	Password                string     `gorm:"column:password"`
	LastLogin               *time.Time `gorm:"column:last_login"`
	Username                string     `gorm:"column:username"`
	MobileNumber            *string    `gorm:"column:mobile_number"`
	Email                   string     `gorm:"column:email"`
	DisplayName             string     `gorm:"column:display_name"`
	FirstName               string     `gorm:"column:first_name"`
	LastName                string     `gorm:"column:last_name"`
	Avatar                  string     `gorm:"column:avatar"`
	AvatarAssetID           *string    `gorm:"column:avatar_asset_id;type:uuid"`
	CoverImage              *string    `gorm:"column:cover_image"`
	CoverImageAssetID       *string    `gorm:"column:cover_image_asset_id;type:uuid"`
	DateJoined              time.Time  `gorm:"column:date_joined"`
	CreatedAt               time.Time  `gorm:"column:created_at"`
	UpdatedAt               time.Time  `gorm:"column:updated_at"`
	LastLocation            string     `gorm:"column:last_location"`
	CreatedLocation         string     `gorm:"column:created_location"`
	IsSuperuser             bool       `gorm:"column:is_superuser"`
	IsManaged               bool       `gorm:"column:is_managed"`
	IsPasswordExpired       bool       `gorm:"column:is_password_expired"`
	IsActive                bool       `gorm:"column:is_active"`
	IsStaff                 bool       `gorm:"column:is_staff"`
	IsEmailVerified         bool       `gorm:"column:is_email_verified"`
	IsPasswordAutoset       bool       `gorm:"column:is_password_autoset"`
	IsPasswordResetRequired bool       `gorm:"column:is_password_reset_required"`
	Token                   string     `gorm:"column:token"`
	LastActive              *time.Time `gorm:"column:last_active"`
	LastLoginTime           *time.Time `gorm:"column:last_login_time"`
	LastLogoutTime          *time.Time `gorm:"column:last_logout_time"`
	LastLoginIP             string     `gorm:"column:last_login_ip"`
	LastLogoutIP            string     `gorm:"column:last_logout_ip"`
	LastLoginMedium         string     `gorm:"column:last_login_medium"`
	LastLoginUserAgent      string     `gorm:"column:last_login_uagent"`
	TokenUpdatedAt          *time.Time `gorm:"column:token_updated_at"`
	IsBot                   bool       `gorm:"column:is_bot"`
	BotType                 *string    `gorm:"column:bot_type"`
	UserTimezone            string     `gorm:"column:user_timezone"`
	IsEmailValid            bool       `gorm:"column:is_email_valid"`
	MaskedAt                *time.Time `gorm:"column:masked_at"`
}

func (User) TableName() string { return "users" }

type Profile struct {
	ID                       string    `gorm:"column:id;type:uuid;primaryKey"`
	UserID                   string    `gorm:"column:user_id;type:uuid"`
	Theme                    JSONValue `gorm:"column:theme;type:jsonb"`
	IsAppRailDocked          bool      `gorm:"column:is_app_rail_docked"`
	IsTourCompleted          bool      `gorm:"column:is_tour_completed"`
	OnboardingStep           JSONValue `gorm:"column:onboarding_step;type:jsonb"`
	UseCase                  *string   `gorm:"column:use_case"`
	Role                     *string   `gorm:"column:role"`
	IsOnboarded              bool      `gorm:"column:is_onboarded"`
	LastWorkspaceID          *string   `gorm:"column:last_workspace_id;type:uuid"`
	BillingAddressCountry    string    `gorm:"column:billing_address_country"`
	BillingAddress           JSONValue `gorm:"column:billing_address;type:jsonb"`
	HasBillingAddress        bool      `gorm:"column:has_billing_address"`
	CompanyName              string    `gorm:"column:company_name"`
	NotificationViewMode     string    `gorm:"column:notification_view_mode"`
	IsSmoothCursorEnabled    bool      `gorm:"column:is_smooth_cursor_enabled"`
	IsMobileOnboarded        bool      `gorm:"column:is_mobile_onboarded"`
	MobileOnboardingStep     JSONValue `gorm:"column:mobile_onboarding_step;type:jsonb"`
	MobileTimezoneAutoSet    bool      `gorm:"column:mobile_timezone_auto_set"`
	Language                 string    `gorm:"column:language"`
	StartOfTheWeek           int16     `gorm:"column:start_of_the_week"`
	Goals                    JSONValue `gorm:"column:goals;type:jsonb"`
	BackgroundColor          string    `gorm:"column:background_color"`
	IsNavigationTourComplete bool      `gorm:"column:is_navigation_tour_completed"`
	HasMarketingConsent      bool      `gorm:"column:has_marketing_email_consent"`
	IsSubscribedToChangelog  bool      `gorm:"column:is_subscribed_to_changelog"`
	ProductTour              JSONValue `gorm:"column:product_tour;type:jsonb"`
	CreatedAt                time.Time `gorm:"column:created_at"`
	UpdatedAt                time.Time `gorm:"column:updated_at"`
}

func (Profile) TableName() string { return "profiles" }

type UserNotificationPreference struct {
	ID             string     `gorm:"column:id;type:uuid;primaryKey"`
	UserID         string     `gorm:"column:user_id;type:uuid"`
	WorkspaceID    *string    `gorm:"column:workspace_id;type:uuid"`
	ProjectID      *string    `gorm:"column:project_id;type:uuid"`
	PropertyChange bool       `gorm:"column:property_change"`
	StateChange    bool       `gorm:"column:state_change"`
	Comment        bool       `gorm:"column:comment"`
	Mention        bool       `gorm:"column:mention"`
	IssueCompleted bool       `gorm:"column:issue_completed"`
	CreatedAt      time.Time  `gorm:"column:created_at"`
	UpdatedAt      time.Time  `gorm:"column:updated_at"`
	DeletedAt      *time.Time `gorm:"column:deleted_at"`
	CreatedByID    *string    `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID    *string    `gorm:"column:updated_by_id;type:uuid"`
}

func (UserNotificationPreference) TableName() string { return "user_notification_preferences" }

type Repository interface {
	InstanceConfigured(ctx context.Context) (bool, error)
	ConfigurationValue(ctx context.Context, key, fallback string) (string, error)
	SignupAllowed(ctx context.Context, email string) (bool, error)
	FindUserByEmail(ctx context.Context, email string) (*User, error)
	FindUserByID(ctx context.Context, id string) (*User, error)
	CreateUser(ctx context.Context, email, encodedPassword string, passwordAutoset, emailVerified bool) (*User, error)
	CreateOAuthUser(ctx context.Context, identity OAuthIdentity, encodedPassword string) (*User, error)
	SyncOAuthUser(ctx context.Context, userID string, identity OAuthIdentity, at time.Time) error
	UpsertOAuthAccount(ctx context.Context, userID string, identity OAuthIdentity, tokens OAuthTokens, at time.Time) error
	ProcessAcceptedInvitations(ctx context.Context, user *User, at time.Time) error
	RecordAuthentication(ctx context.Context, user *User, medium, ipAddress, userAgent string, at time.Time) error
	RecordSessionLogin(ctx context.Context, userID string, at time.Time) error
	RecordLogout(ctx context.Context, userID, ipAddress string, at time.Time) error
	UpdatePassword(ctx context.Context, user *User, encodedPassword string, at time.Time) error
}

type GORMRepository struct {
	db                    *gorm.DB
	skipEnvironmentConfig bool
	secretKey             string
	cache                 CacheInvalidator
	avatarStore           AvatarStore
}

func NewGORMRepository(db *gorm.DB, skipEnvironmentConfig bool, secretKey string) *GORMRepository {
	return &GORMRepository{db: db, skipEnvironmentConfig: skipEnvironmentConfig, secretKey: secretKey}
}

func (repository *GORMRepository) SetCacheInvalidator(invalidator CacheInvalidator) {
	repository.cache = invalidator
}

func (repository *GORMRepository) SetAvatarStore(store AvatarStore) {
	repository.avatarStore = store
}

func (repository *GORMRepository) InstanceConfigured(ctx context.Context) (bool, error) {
	var count int64
	err := repository.db.WithContext(ctx).Table("instances").Where("is_setup_done = ? AND deleted_at IS NULL", true).Count(&count).Error
	return count > 0, err
}

func (repository *GORMRepository) ConfigurationValue(ctx context.Context, key, fallback string) (string, error) {
	if !repository.skipEnvironmentConfig {
		return fallback, nil
	}
	var result struct {
		Value       *string `gorm:"column:value"`
		IsEncrypted bool    `gorm:"column:is_encrypted"`
	}
	err := repository.db.WithContext(ctx).Table("instance_configurations").Select("value, is_encrypted").Where("key = ? AND deleted_at IS NULL", key).Take(&result).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return fallback, nil
	}
	if err != nil {
		return "", err
	}
	if result.Value == nil {
		return "", nil
	}
	if result.IsEncrypted {
		decrypted, err := decryptDjangoConfiguration(*result.Value, repository.secretKey)
		if err != nil {
			return "", fmt.Errorf("decrypt configuration %s: %w", key, err)
		}
		return decrypted, nil
	}
	return *result.Value, nil
}

func (repository *GORMRepository) SignupAllowed(ctx context.Context, email string) (bool, error) {
	var count int64
	err := repository.db.WithContext(ctx).Table("workspace_member_invites").
		Where("LOWER(email) = ? AND deleted_at IS NULL", strings.ToLower(email)).Count(&count).Error
	return count > 0, err
}

func (repository *GORMRepository) FindUserByEmail(ctx context.Context, email string) (*User, error) {
	var user User
	err := repository.db.WithContext(ctx).Where("LOWER(email) = ?", strings.ToLower(email)).Take(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &user, err
}

func (repository *GORMRepository) FindUserByID(ctx context.Context, id string) (*User, error) {
	var user User
	err := repository.db.WithContext(ctx).Where("id = ?", id).Take(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &user, err
}

func (repository *GORMRepository) CreateUser(ctx context.Context, email, encodedPassword string, passwordAutoset, emailVerified bool) (*User, error) {
	return repository.createUser(ctx, email, encodedPassword, passwordAutoset, emailVerified, "", "", "")
}

func (repository *GORMRepository) CreateOAuthUser(ctx context.Context, identity OAuthIdentity, encodedPassword string) (*User, error) {
	user, err := repository.createUser(ctx, identity.Email, encodedPassword, true, true, identity.FirstName, identity.LastName, identity.Avatar)
	if err != nil || repository.avatarStore == nil || identity.Avatar == "" {
		return user, err
	}
	asset, err := repository.avatarStore.Upload(ctx, identity.Provider, identity.Avatar)
	if err != nil {
		return nil, err
	}
	if asset == nil {
		return user, nil
	}
	assetID, err := repository.createAvatarAsset(ctx, user.ID, asset)
	if err != nil {
		return nil, err
	}
	user.AvatarAssetID = &assetID
	return user, repository.db.WithContext(ctx).Model(&User{}).Where("id = ?", user.ID).Update("avatar_asset_id", assetID).Error
}

// NewUserRecords builds the three rows a new account is: the user, the profile the preferences moved to, and the notification preference.
//
// It is one function because a second caller writing these by hand got them wrong in four separate ways — naming columns that had moved to the profile, naming audit columns the profile does not have, and leaving out nine of the user's and ten of the profile's NOT NULL columns. Every default here is Django's, taken from what the recorded migrations set the column to when they added it.
func NewUserRecords(email, encodedPassword, firstName, lastName, avatar string, passwordAutoset, emailVerified bool, now time.Time) (*User, *Profile, *UserNotificationPreference, error) {
	userID, err := randomUUID()
	if err != nil {
		return nil, nil, nil, err
	}
	username, err := randomHex(16)
	if err != nil {
		return nil, nil, nil, err
	}
	color, err := randomHex(3)
	if err != nil {
		return nil, nil, nil, err
	}
	profileID, err := randomUUID()
	if err != nil {
		return nil, nil, nil, err
	}
	preferenceID, err := randomUUID()
	if err != nil {
		return nil, nil, nil, err
	}
	user := &User{
		ID: userID, Password: encodedPassword, Username: username, Email: strings.ToLower(strings.TrimSpace(email)),
		DisplayName: strings.Split(email, "@")[0], FirstName: firstName, LastName: lastName, Avatar: avatar, DateJoined: now,
		CreatedAt: now, UpdatedAt: now, LastLocation: "", CreatedLocation: "", IsActive: true,
		IsEmailVerified: emailVerified, IsPasswordAutoset: passwordAutoset, Token: "", LastActive: &now,
		LastLoginIP: "", LastLogoutIP: "", LastLoginMedium: "email", LastLoginUserAgent: "", UserTimezone: "UTC",
	}
	profile := &Profile{
		ID: profileID, UserID: userID, Theme: JSONValue(`{}`), IsAppRailDocked: true,
		OnboardingStep:        JSONValue(`{"profile_complete":false,"workspace_create":false,"workspace_invite":false,"workspace_join":false}`),
		BillingAddressCountry: "INDIA", CompanyName: "", NotificationViewMode: "full",
		MobileOnboardingStep: JSONValue(`{"profile_complete":false,"workspace_create":false,"workspace_join":false}`),
		Language:             "en", StartOfTheWeek: 0, Goals: JSONValue(`{}`), BackgroundColor: "#" + color,
		ProductTour: JSONValue(`{"work_items":false,"cycles":false,"modules":false,"intake":false,"pages":false}`),
		CreatedAt:   now, UpdatedAt: now,
	}
	preference := &UserNotificationPreference{
		ID: preferenceID, UserID: userID, PropertyChange: true, StateChange: true, Comment: true,
		Mention: true, IssueCompleted: true, CreatedAt: now, UpdatedAt: now,
	}
	return user, profile, preference, nil
}

func (repository *GORMRepository) createUser(ctx context.Context, email, encodedPassword string, passwordAutoset, emailVerified bool, firstName, lastName, avatar string) (*User, error) {
	user, profile, preference, err := NewUserRecords(email, encodedPassword, firstName, lastName, avatar, passwordAutoset, emailVerified, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	err = repository.db.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		if err := transaction.Create(user).Error; err != nil {
			return err
		}
		if err := transaction.Create(profile).Error; err != nil {
			return err
		}
		return transaction.Create(preference).Error
	})
	if err != nil {
		return nil, err
	}
	return user, nil
}

func (repository *GORMRepository) SyncOAuthUser(ctx context.Context, userID string, identity OAuthIdentity, at time.Time) error {
	var oldAsset struct {
		AvatarAssetID *string `gorm:"column:avatar_asset_id"`
	}
	if err := repository.db.WithContext(ctx).Table("users").Select("avatar_asset_id").Where("id = ?", userID).Take(&oldAsset).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	var newAssetID *string
	if repository.avatarStore != nil && identity.Avatar != "" {
		asset, err := repository.avatarStore.Upload(ctx, identity.Provider, identity.Avatar)
		if err != nil {
			return err
		}
		if asset != nil {
			id, err := repository.createAvatarAsset(ctx, userID, asset)
			if err != nil {
				return err
			}
			newAssetID = &id
		}
	}
	updates := map[string]any{
		"first_name": identity.FirstName, "last_name": identity.LastName, "avatar": identity.Avatar,
		"avatar_asset_id": newAssetID, "updated_at": at,
	}
	if err := repository.db.WithContext(ctx).Model(&User{}).Where("id = ?", userID).Updates(updates).Error; err != nil {
		return err
	}
	if oldAsset.AvatarAssetID != nil && (newAssetID == nil || *oldAsset.AvatarAssetID != *newAssetID) {
		if repository.avatarStore != nil {
			var oldAssetRow struct {
				Asset string `gorm:"column:asset"`
			}
			if err := repository.db.WithContext(ctx).Table("file_assets").Select("asset").Where("id = ?", *oldAsset.AvatarAssetID).Take(&oldAssetRow).Error; err == nil {
				_ = repository.avatarStore.Delete(ctx, oldAssetRow.Asset)
			}
		}
		_ = repository.db.WithContext(ctx).Table("file_assets").Where("id = ? AND deleted_at IS NULL", *oldAsset.AvatarAssetID).Update("deleted_at", at).Error
	}
	return nil
}

type fileAsset struct {
	ID              string    `gorm:"column:id;type:uuid;primaryKey"`
	Attributes      JSONValue `gorm:"column:attributes;type:jsonb"`
	Asset           string    `gorm:"column:asset"`
	UserID          *string   `gorm:"column:user_id;type:uuid"`
	EntityType      string    `gorm:"column:entity_type"`
	Size            float64   `gorm:"column:size"`
	IsUploaded      bool      `gorm:"column:is_uploaded"`
	StorageMetadata JSONValue `gorm:"column:storage_metadata;type:jsonb"`
	CreatedAt       time.Time `gorm:"column:created_at"`
	UpdatedAt       time.Time `gorm:"column:updated_at"`
	CreatedByID     *string   `gorm:"column:created_by_id;type:uuid"`
}

func (fileAsset) TableName() string { return "file_assets" }

func (repository *GORMRepository) createAvatarAsset(ctx context.Context, userID string, upload *AvatarUpload) (string, error) {
	id, err := randomUUID()
	if err != nil {
		return "", err
	}
	at := time.Now().UTC()
	asset := &fileAsset{
		ID: id, Attributes: JSONValue(fmt.Sprintf(`{"name":%q,"type":%q,"size":%d}`, upload.AttributeName, upload.ContentType, upload.Size)),
		Asset: upload.ObjectName, UserID: &userID, EntityType: "USER_AVATAR", Size: float64(upload.Size),
		IsUploaded: true, StorageMetadata: upload.StorageMetadata, CreatedAt: at, UpdatedAt: at, CreatedByID: &userID,
	}
	return id, repository.db.WithContext(ctx).Create(asset).Error
}

type OAuthAccount struct {
	ID                    string     `gorm:"column:id;type:uuid;primaryKey"`
	UserID                string     `gorm:"column:user_id;type:uuid"`
	ProviderAccountID     string     `gorm:"column:provider_account_id"`
	Provider              string     `gorm:"column:provider"`
	AccessToken           string     `gorm:"column:access_token"`
	AccessTokenExpiredAt  *time.Time `gorm:"column:access_token_expired_at"`
	RefreshToken          *string    `gorm:"column:refresh_token"`
	RefreshTokenExpiredAt *time.Time `gorm:"column:refresh_token_expired_at"`
	LastConnectedAt       time.Time  `gorm:"column:last_connected_at"`
	IDToken               string     `gorm:"column:id_token"`
	Metadata              JSONValue  `gorm:"column:metadata;type:jsonb"`
	CreatedAt             time.Time  `gorm:"column:created_at"`
	UpdatedAt             time.Time  `gorm:"column:updated_at"`
}

func (OAuthAccount) TableName() string { return "accounts" }

func (repository *GORMRepository) UpsertOAuthAccount(ctx context.Context, userID string, identity OAuthIdentity, tokens OAuthTokens, at time.Time) error {
	var account OAuthAccount
	err := repository.db.WithContext(ctx).Where(
		"user_id = ? AND provider = ? AND provider_account_id = ?", userID, identity.Provider, identity.ProviderID,
	).Take(&account).Error
	if err == nil {
		return repository.db.WithContext(ctx).Model(&account).Updates(map[string]any{
			"access_token": tokens.AccessToken, "refresh_token": tokens.RefreshToken,
			"access_token_expired_at":  tokens.AccessTokenExpiredAt,
			"refresh_token_expired_at": tokens.RefreshTokenExpiredAt,
			"last_connected_at":        at, "id_token": tokens.IDToken, "updated_at": at,
		}).Error
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	id, err := randomUUID()
	if err != nil {
		return err
	}
	account = OAuthAccount{
		ID: id, UserID: userID, Provider: identity.Provider, ProviderAccountID: identity.ProviderID,
		AccessToken: tokens.AccessToken, RefreshToken: tokens.RefreshToken,
		AccessTokenExpiredAt: tokens.AccessTokenExpiredAt, RefreshTokenExpiredAt: tokens.RefreshTokenExpiredAt,
		LastConnectedAt: at, IDToken: tokens.IDToken, Metadata: JSONValue(`{}`), CreatedAt: at, UpdatedAt: at,
	}
	return repository.db.WithContext(ctx).Create(&account).Error
}

func (repository *GORMRepository) RecordAuthentication(ctx context.Context, user *User, medium, ipAddress, userAgent string, at time.Time) error {
	token, err := randomHex(32)
	if err != nil {
		return err
	}
	updates := map[string]any{
		"is_active":         true,
		"last_active":       at,
		"last_login_time":   at,
		"last_login_ip":     ipAddress,
		"last_login_medium": medium,
		"last_login_uagent": userAgent,
		"token":             token,
		"token_updated_at":  at,
		"updated_at":        at,
	}
	if err := repository.db.WithContext(ctx).Model(&User{}).Where("id = ?", user.ID).Updates(updates).Error; err != nil {
		return err
	}
	user.IsActive = true
	user.LastActive = &at
	user.LastLoginTime = &at
	user.LastLoginIP = ipAddress
	user.LastLoginMedium = medium
	user.LastLoginUserAgent = userAgent
	user.Token = token
	user.TokenUpdatedAt = &at
	return nil
}

func (repository *GORMRepository) RecordSessionLogin(ctx context.Context, userID string, at time.Time) error {
	return repository.db.WithContext(ctx).Model(&User{}).Where("id = ?", userID).Update("last_login", at).Error
}

func (repository *GORMRepository) RecordLogout(ctx context.Context, userID, ipAddress string, at time.Time) error {
	token, err := randomHex(32)
	if err != nil {
		return err
	}
	return repository.db.WithContext(ctx).Model(&User{}).Where("id = ?", userID).Updates(map[string]any{
		"last_logout_ip": ipAddress, "last_logout_time": at, "updated_at": at,
		"token":            gorm.Expr("CASE WHEN token_updated_at IS NOT NULL THEN ? ELSE token END", token),
		"token_updated_at": gorm.Expr("CASE WHEN token_updated_at IS NOT NULL THEN ? ELSE token_updated_at END", at),
	}).Error
}

func (repository *GORMRepository) UpdatePassword(ctx context.Context, user *User, encodedPassword string, at time.Time) error {
	updates := map[string]any{"password": encodedPassword, "is_password_autoset": false, "updated_at": at}
	if user.TokenUpdatedAt != nil {
		token, err := randomHex(32)
		if err != nil {
			return err
		}
		updates["token"] = token
		updates["token_updated_at"] = at
		user.Token = token
		user.TokenUpdatedAt = &at
	}
	if err := repository.db.WithContext(ctx).Model(&User{}).Where("id = ?", user.ID).Updates(updates).Error; err != nil {
		return err
	}
	user.Password = encodedPassword
	user.IsPasswordAutoset = false
	user.UpdatedAt = at
	return nil
}

type workspaceInvite struct {
	ID          string  `gorm:"column:id"`
	WorkspaceID string  `gorm:"column:workspace_id"`
	Role        int16   `gorm:"column:role"`
	CreatedByID *string `gorm:"column:created_by_id"`
}

type projectInvite struct {
	ID          string  `gorm:"column:id"`
	WorkspaceID string  `gorm:"column:workspace_id"`
	ProjectID   string  `gorm:"column:project_id"`
	Role        int16   `gorm:"column:role"`
	CreatedByID *string `gorm:"column:created_by_id"`
}

type workspaceMember struct {
	ID                      string     `gorm:"column:id;type:uuid;primaryKey"`
	WorkspaceID             string     `gorm:"column:workspace_id;type:uuid"`
	MemberID                string     `gorm:"column:member_id;type:uuid"`
	Role                    int16      `gorm:"column:role"`
	CompanyRole             *string    `gorm:"column:company_role"`
	ViewProps               JSONValue  `gorm:"column:view_props;type:jsonb"`
	DefaultProps            JSONValue  `gorm:"column:default_props;type:jsonb"`
	IssueProps              JSONValue  `gorm:"column:issue_props;type:jsonb"`
	IsActive                bool       `gorm:"column:is_active"`
	GettingStartedChecklist JSONValue  `gorm:"column:getting_started_checklist;type:jsonb"`
	Tips                    JSONValue  `gorm:"column:tips;type:jsonb"`
	ExploredFeatures        JSONValue  `gorm:"column:explored_features;type:jsonb"`
	CreatedAt               time.Time  `gorm:"column:created_at"`
	UpdatedAt               time.Time  `gorm:"column:updated_at"`
	DeletedAt               *time.Time `gorm:"column:deleted_at"`
	CreatedByID             *string    `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID             *string    `gorm:"column:updated_by_id;type:uuid"`
}

func (workspaceMember) TableName() string { return "workspace_members" }

type projectMember struct {
	ID           string     `gorm:"column:id;type:uuid;primaryKey"`
	WorkspaceID  string     `gorm:"column:workspace_id;type:uuid"`
	ProjectID    string     `gorm:"column:project_id;type:uuid"`
	MemberID     string     `gorm:"column:member_id;type:uuid"`
	Comment      *string    `gorm:"column:comment"`
	Role         int16      `gorm:"column:role"`
	ViewProps    JSONValue  `gorm:"column:view_props;type:jsonb"`
	DefaultProps JSONValue  `gorm:"column:default_props;type:jsonb"`
	Preferences  JSONValue  `gorm:"column:preferences;type:jsonb"`
	SortOrder    float64    `gorm:"column:sort_order"`
	IsActive     bool       `gorm:"column:is_active"`
	CreatedAt    time.Time  `gorm:"column:created_at"`
	UpdatedAt    time.Time  `gorm:"column:updated_at"`
	DeletedAt    *time.Time `gorm:"column:deleted_at"`
	CreatedByID  *string    `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID  *string    `gorm:"column:updated_by_id;type:uuid"`
}

func (projectMember) TableName() string { return "project_members" }

const workspaceMemberDefaultProps = `{"filters":{"priority":null,"state":null,"state_group":null,"assignees":null,"created_by":null,"labels":null,"start_date":null,"target_date":null,"subscriber":null},"display_filters":{"group_by":null,"order_by":"-created_at","type":null,"sub_issue":true,"show_empty_groups":true,"layout":"list","calendar_date_range":""},"display_properties":{"assignee":true,"attachment_count":true,"created_on":true,"due_date":true,"estimate":true,"key":true,"labels":true,"link":true,"priority":true,"start_date":true,"state":true,"sub_issue_count":true,"updated_on":true}}`
const projectMemberDefaultProps = `{"filters":{"priority":null,"state":null,"state_group":null,"assignees":null,"created_by":null,"labels":null,"start_date":null,"target_date":null,"subscriber":null},"display_filters":{"group_by":null,"order_by":"-created_at","type":null,"sub_issue":true,"show_empty_groups":true,"layout":"list","calendar_date_range":""}}`

func (repository *GORMRepository) ProcessAcceptedInvitations(ctx context.Context, user *User, at time.Time) error {
	var workspaceSlugs []string
	return repository.db.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		var workspaceInvites []workspaceInvite
		if err := transaction.Table("workspace_member_invites").
			Where("LOWER(email) = ? AND accepted = ? AND deleted_at IS NULL", strings.ToLower(user.Email), true).
			Find(&workspaceInvites).Error; err != nil {
			return err
		}
		var projectInvites []projectInvite
		if err := transaction.Table("project_member_invites").
			Where("LOWER(email) = ? AND accepted = ? AND deleted_at IS NULL", strings.ToLower(user.Email), true).
			Find(&projectInvites).Error; err != nil {
			return err
		}
		for _, invite := range workspaceInvites {
			var workspace struct {
				Slug string `gorm:"column:slug"`
			}
			if err := transaction.Table("workspaces").Select("slug").Where("id = ?", invite.WorkspaceID).Take(&workspace).Error; err == nil && workspace.Slug != "" {
				workspaceSlugs = append(workspaceSlugs, workspace.Slug)
			}
			if err := repository.createWorkspaceMember(transaction, user.ID, invite.WorkspaceID, invite.Role, nil, at); err != nil {
				return err
			}
		}
		for _, invite := range projectInvites {
			var workspace struct {
				Slug string `gorm:"column:slug"`
			}
			if err := transaction.Table("workspaces").Select("slug").Where("id = ?", invite.WorkspaceID).Take(&workspace).Error; err == nil && workspace.Slug != "" {
				workspaceSlugs = append(workspaceSlugs, workspace.Slug)
			}
			role := invite.Role
			if role != 5 && role != 15 {
				role = 15
			}
			if err := repository.createWorkspaceMember(transaction, user.ID, invite.WorkspaceID, role, invite.CreatedByID, at); err != nil {
				return err
			}
			id, err := randomUUID()
			if err != nil {
				return err
			}
			member := projectMember{
				ID: id, WorkspaceID: invite.WorkspaceID, ProjectID: invite.ProjectID, MemberID: user.ID,
				Role: role, ViewProps: JSONValue(projectMemberDefaultProps), DefaultProps: JSONValue(projectMemberDefaultProps),
				Preferences: JSONValue(`{"pages":{"block_display":true},"navigation":{"default_tab":"work_items","hide_in_more_menu":[]}}`),
				SortOrder:   65535, IsActive: true, CreatedAt: at, UpdatedAt: at, CreatedByID: invite.CreatedByID,
			}
			if err := transaction.Clauses(clause.OnConflict{DoNothing: true}).Create(&member).Error; err != nil {
				return err
			}
		}
		if err := transaction.Table("workspace_member_invites").Where("LOWER(email) = ? AND accepted = ? AND deleted_at IS NULL", strings.ToLower(user.Email), true).Update("deleted_at", at).Error; err != nil {
			return err
		}
		if err := transaction.Table("project_member_invites").Where("LOWER(email) = ? AND accepted = ? AND deleted_at IS NULL", strings.ToLower(user.Email), true).Update("deleted_at", at).Error; err != nil {
			return err
		}
		if repository.cache != nil {
			seen := make(map[string]struct{}, len(workspaceSlugs))
			for _, slug := range workspaceSlugs {
				if _, exists := seen[slug]; exists {
					continue
				}
				seen[slug] = struct{}{}
				if err := repository.cache.InvalidatePattern(ctx, "*/api/workspaces/"+slug+"/members/*"); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func (repository *GORMRepository) createWorkspaceMember(transaction *gorm.DB, userID, workspaceID string, role int16, createdByID *string, at time.Time) error {
	id, err := randomUUID()
	if err != nil {
		return err
	}
	member := workspaceMember{
		ID: id, WorkspaceID: workspaceID, MemberID: userID, Role: role,
		ViewProps: JSONValue(workspaceMemberDefaultProps), DefaultProps: JSONValue(workspaceMemberDefaultProps),
		IssueProps: JSONValue(`{"subscribed":true,"assigned":true,"created":true,"all_issues":true}`),
		IsActive:   true, GettingStartedChecklist: JSONValue(`{}`), Tips: JSONValue(`{}`), ExploredFeatures: JSONValue(`{}`),
		CreatedAt: at, UpdatedAt: at, CreatedByID: createdByID,
	}
	return transaction.Clauses(clause.OnConflict{DoNothing: true}).Create(&member).Error
}

type SessionRecord struct {
	SessionKey  string         `gorm:"column:session_key;primaryKey"`
	SessionData string         `gorm:"column:session_data"`
	ExpireDate  time.Time      `gorm:"column:expire_date"`
	DeviceInfo  map[string]any `gorm:"column:device_info;serializer:json"`
	UserID      *string        `gorm:"column:user_id"`
}

func (SessionRecord) TableName() string { return "sessions" }

type SessionRepository interface {
	Load(ctx context.Context, key string, now time.Time) (*SessionRecord, error)
	Save(ctx context.Context, record *SessionRecord) error
	Delete(ctx context.Context, key string) error
}

type GORMSessionRepository struct{ db *gorm.DB }

func NewGORMSessionRepository(db *gorm.DB) *GORMSessionRepository {
	return &GORMSessionRepository{db: db}
}

func (repository *GORMSessionRepository) Load(ctx context.Context, key string, now time.Time) (*SessionRecord, error) {
	var record SessionRecord
	err := repository.db.WithContext(ctx).Where("session_key = ? AND expire_date > ?", key, now).Take(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &record, err
}

func (repository *GORMSessionRepository) Save(ctx context.Context, record *SessionRecord) error {
	return repository.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "session_key"}},
		DoUpdates: clause.AssignmentColumns([]string{"session_data", "expire_date", "device_info", "user_id"}),
	}).Create(record).Error
}

func (repository *GORMSessionRepository) Delete(ctx context.Context, key string) error {
	return repository.db.WithContext(ctx).Where("session_key = ?", key).Delete(&SessionRecord{}).Error
}

func randomUUID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate UUID: %w", err)
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		value[0:4], value[4:6], value[6:8], value[8:10], value[10:16]), nil
}

func randomHex(byteLength int) (string, error) {
	value := make([]byte, byteLength)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate random value: %w", err)
	}
	return hex.EncodeToString(value), nil
}
