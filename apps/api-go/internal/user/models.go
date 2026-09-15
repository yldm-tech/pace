package user

import (
	"time"

	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
)

type Profile struct {
	ID                       string         `gorm:"column:id;type:uuid;primaryKey"`
	UserID                   string         `gorm:"column:user_id;type:uuid"`
	Theme                    auth.JSONValue `gorm:"column:theme;type:jsonb"`
	IsAppRailDocked          bool           `gorm:"column:is_app_rail_docked"`
	IsTourCompleted          bool           `gorm:"column:is_tour_completed"`
	OnboardingStep           auth.JSONValue `gorm:"column:onboarding_step;type:jsonb"`
	UseCase                  *string        `gorm:"column:use_case"`
	Role                     *string        `gorm:"column:role"`
	IsOnboarded              bool           `gorm:"column:is_onboarded"`
	LastWorkspaceID          *string        `gorm:"column:last_workspace_id;type:uuid"`
	BillingAddressCountry    string         `gorm:"column:billing_address_country"`
	BillingAddress           auth.JSONValue `gorm:"column:billing_address;type:jsonb"`
	HasBillingAddress        bool           `gorm:"column:has_billing_address"`
	CompanyName              string         `gorm:"column:company_name"`
	NotificationViewMode     string         `gorm:"column:notification_view_mode"`
	IsSmoothCursorEnabled    bool           `gorm:"column:is_smooth_cursor_enabled"`
	IsMobileOnboarded        bool           `gorm:"column:is_mobile_onboarded"`
	MobileOnboardingStep     auth.JSONValue `gorm:"column:mobile_onboarding_step;type:jsonb"`
	MobileTimezoneAutoSet    bool           `gorm:"column:mobile_timezone_auto_set"`
	Language                 string         `gorm:"column:language"`
	StartOfTheWeek           int16          `gorm:"column:start_of_the_week"`
	Goals                    auth.JSONValue `gorm:"column:goals;type:jsonb"`
	BackgroundColor          string         `gorm:"column:background_color"`
	IsNavigationTourComplete bool           `gorm:"column:is_navigation_tour_completed"`
	HasMarketingConsent      bool           `gorm:"column:has_marketing_email_consent"`
	IsSubscribedToChangelog  bool           `gorm:"column:is_subscribed_to_changelog"`
	ProductTour              auth.JSONValue `gorm:"column:product_tour;type:jsonb"`
	CreatedAt                time.Time      `gorm:"column:created_at"`
	UpdatedAt                time.Time      `gorm:"column:updated_at"`
}

func (Profile) TableName() string { return "profiles" }

type Account struct {
	ID                    string         `gorm:"column:id;type:uuid;primaryKey"`
	UserID                string         `gorm:"column:user_id;type:uuid"`
	ProviderAccountID     string         `gorm:"column:provider_account_id"`
	Provider              string         `gorm:"column:provider"`
	AccessToken           string         `gorm:"column:access_token"`
	AccessTokenExpiredAt  *time.Time     `gorm:"column:access_token_expired_at"`
	RefreshToken          *string        `gorm:"column:refresh_token"`
	RefreshTokenExpiredAt *time.Time     `gorm:"column:refresh_token_expired_at"`
	LastConnectedAt       time.Time      `gorm:"column:last_connected_at"`
	IDToken               string         `gorm:"column:id_token"`
	Metadata              auth.JSONValue `gorm:"column:metadata;type:jsonb"`
	CreatedAt             time.Time      `gorm:"column:created_at"`
	UpdatedAt             time.Time      `gorm:"column:updated_at"`
}

func (Account) TableName() string { return "accounts" }
