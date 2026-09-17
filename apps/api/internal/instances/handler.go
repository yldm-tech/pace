// Package instances is the admin console's API: the admin screens an operator signs into to configure the installation itself.
//
// It is its own app rather than part of the workspace API, and its own idea of who may call it. Every route here asks whether the caller is an instance administrator, which has nothing to do with being an administrator of any workspace — and the two sign-in routes are form posts that answer with a redirect rather than json, because the admin console is a separate front end that reads its errors out of the query string.
package instances

import (
	"context"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api/internal/auth"
	"github.com/yldm-tech/pace/apps/api/internal/drf"
	"gorm.io/gorm"
)

// Settings is what the admin console needs to know about the installation around it.
type Settings struct {
	// The three base urls the config response hands the front end, and the one every redirect from here is built on.
	AdminBaseURL  string
	AdminBasePath string
	SpaceBaseURL  string
	AppBaseURL    string
	WebURL        string

	InstanceChangelogURL string
	IsSelfManaged        bool
	FileSizeLimit        float64
	SecretKey            string
	// SkipEnvironmentConfig is SKIP_ENV_VAR: with it set the configuration rows are authoritative, and without it they are ignored entirely.
	SkipEnvironmentConfig bool
	// The allowlists the language-model endpoint is checked against before this process connects to it. They are the webhook ones: an address typed into the console is still an address this process dials, and a deployment that has already said which private ranges are reachable should not have to say it twice.
	LLMAllowedIPs   []netip.Prefix
	LLMAllowedHosts []string
	// Environment is the fallback every configuration value falls back to, read the way get_configuration_value reads it.
	Environment map[string]string
}

// Mailer sends the one message the credentials check sends.
type Mailer interface {
	Send(ctx context.Context, to, subject, text string) error
}

// Handler serves the admin console's API.
type Handler struct {
	db       *gorm.DB
	sessions *auth.SessionManager
	settings Settings
	mailer   Mailer
	clock    func() time.Time
}

func NewHandler(db *gorm.DB, sessions *auth.SessionManager, settings Settings) *Handler {
	return &Handler{db: db, sessions: sessions, settings: settings, clock: time.Now}
}

func (handler *Handler) SetMailer(mailer Mailer) { handler.mailer = mailer }

// RegisterRoutes mounts every route the admin console calls.
func (handler *Handler) RegisterRoutes(router gin.IRouter) {
	const base = "/api/instances/"
	router.GET(base, handler.instanceRead)
	router.PATCH(base, handler.admin(handler.instanceUpdate))

	router.GET(base+"admins/", handler.admin(handler.adminList))
	router.POST(base+"admins/", handler.admin(handler.adminCreate))
	router.DELETE(base+"admins/:pk/", handler.admin(handler.adminDelete))
	router.GET(base+"admins/me/", handler.admin(handler.adminMe))
	router.GET(base+"admins/session/", handler.adminSession)
	router.POST(base+"admins/sign-in/", handler.adminSignIn)
	router.POST(base+"admins/sign-up/", handler.adminSignUp)
	router.POST(base+"admins/sign-out/", handler.adminSignOut)
	router.POST(base+"admins/sign-up-screen-visited/", handler.signUpScreenVisited)

	router.GET(base+"configurations/", handler.admin(handler.configurationList))
	router.POST(base+"configurations/llm-models/", handler.admin(handler.llmModels))
	router.PATCH(base+"configurations/", handler.admin(handler.configurationUpdate))
	router.DELETE(base+"configurations/disable-email-feature/", handler.admin(handler.disableEmailFeature))
	router.POST(base+"email-credentials-check/", handler.admin(handler.emailCredentialsCheck))

	router.GET(base+"workspace-slug-check/", handler.admin(handler.workspaceSlugCheck))
	router.GET(base+"workspaces/", handler.admin(handler.workspaceList))
	router.POST(base+"workspaces/", handler.admin(handler.workspaceCreate))
}

// Instance is the registration row every one of these routes reads first.
type Instance struct {
	ID                         string     `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt                  time.Time  `gorm:"column:created_at"`
	UpdatedAt                  time.Time  `gorm:"column:updated_at"`
	CreatedByID                *string    `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID                *string    `gorm:"column:updated_by_id;type:uuid"`
	DeletedAt                  *time.Time `gorm:"column:deleted_at"`
	InstanceName               string     `gorm:"column:instance_name"`
	WhitelistEmails            *string    `gorm:"column:whitelist_emails"`
	InstanceID                 string     `gorm:"column:instance_id"`
	CurrentVersion             string     `gorm:"column:current_version"`
	LatestVersion              *string    `gorm:"column:latest_version"`
	Edition                    string     `gorm:"column:edition"`
	Domain                     string     `gorm:"column:domain"`
	LastCheckedAt              time.Time  `gorm:"column:last_checked_at"`
	Namespace                  *string    `gorm:"column:namespace"`
	IsTelemetryEnabled         bool       `gorm:"column:is_telemetry_enabled"`
	IsSupportRequired          bool       `gorm:"column:is_support_required"`
	IsSetupDone                bool       `gorm:"column:is_setup_done"`
	IsSignupScreenVisited      bool       `gorm:"column:is_signup_screen_visited"`
	IsVerified                 bool       `gorm:"column:is_verified"`
	IsTest                     bool       `gorm:"column:is_test"`
	IsCurrentVersionDeprecated bool       `gorm:"column:is_current_version_deprecated"`
}

func (Instance) TableName() string { return "instances" }

// instance reads the registration. Instance.Meta orders newest first, so .first() is the newest of them rather than the oldest.
func (handler *Handler) instance(ctx context.Context) (*Instance, error) {
	var instance Instance
	err := handler.db.WithContext(ctx).Table("instances").Where("deleted_at IS NULL").
		Order("created_at DESC").Limit(1).Take(&instance).Error
	if err != nil {
		return nil, err
	}
	return &instance, nil
}

// admin wraps a route in the check every one of them makes: the caller is signed in, and they are an administrator of this installation with a role of at least fifteen.
//
// The role check is `role >= 15`, which is not the role the console hands out — that is twenty. So a row written by hand with fifteen also passes, and nothing in the product ever writes one.
func (handler *Handler) admin(next func(*gin.Context, *auth.User, *Instance)) gin.HandlerFunc {
	return func(c *gin.Context) {
		if handler.sessions == nil {
			c.JSON(http.StatusForbidden, gin.H{"detail": "You do not have permission to perform this action."})
			return
		}
		user, _, err := handler.sessions.Authenticate(c.Request.Context(), c.Request, c.Writer)
		if err != nil || user == nil {
			// Not being signed in is refused with the same 403 as not being an administrator, because the permission answers false either way rather than raising.
			c.JSON(http.StatusForbidden, gin.H{"detail": "You do not have permission to perform this action."})
			return
		}
		instance, err := handler.instance(c.Request.Context())
		if err != nil {
			// The permission reads the instance and compares against it, so no registration means nobody is an administrator.
			c.JSON(http.StatusForbidden, gin.H{"detail": "You do not have permission to perform this action."})
			return
		}
		var count int64
		err = handler.db.WithContext(c.Request.Context()).Table("instance_admins").
			Where("role >= 15 AND instance_id = ? AND user_id = ? AND deleted_at IS NULL", instance.ID, user.ID).
			Count(&count).Error
		if err != nil || count == 0 {
			c.JSON(http.StatusForbidden, gin.H{"detail": "You do not have permission to perform this action."})
			return
		}
		next(c, user, instance)
	}
}

// adminBaseURL is base_host(is_admin=True): the console's own origin with its base path, or the web origin with that path when the console has no origin of its own.
func (handler *Handler) adminBaseURL() string {
	path := handler.settings.AdminBasePath
	if path == "" {
		path = "/admin/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if !strings.HasSuffix(path, "/") {
		path += "/"
	}
	if handler.settings.AdminBaseURL != "" {
		return handler.settings.AdminBaseURL + path
	}
	origin := handler.settings.WebURL
	if origin == "" {
		origin = handler.settings.AppBaseURL
	}
	return origin + path
}

// respond answers with DRF's rendering, so a float and a datetime read the way the console already expects.
func (handler *Handler) respond(c *gin.Context, status int, payload any) {
	drf.Respond(c, status, payload)
}

func (handler *Handler) internalError(c *gin.Context, err error) {
	_ = err
	c.JSON(http.StatusInternalServerError, gin.H{"error": "Something went wrong please try again later"})
}
