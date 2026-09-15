package workspace

import (
	"time"

	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
)

// Workspace and its related records intentionally model the existing Django
// tables. The Go API does not run migrations; PostgreSQL remains owned by the
// Django schema until the corresponding database module is migrated.
type Workspace struct {
	ID               string     `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt        time.Time  `gorm:"column:created_at"`
	UpdatedAt        time.Time  `gorm:"column:updated_at"`
	CreatedByID      *string    `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID      *string    `gorm:"column:updated_by_id;type:uuid"`
	DeletedAt        *time.Time `gorm:"column:deleted_at"`
	Name             string     `gorm:"column:name"`
	Logo             *string    `gorm:"column:logo"`
	LogoAssetID      *string    `gorm:"column:logo_asset_id;type:uuid"`
	OwnerID          string     `gorm:"column:owner_id;type:uuid"`
	Slug             string     `gorm:"column:slug"`
	OrganizationSize *string    `gorm:"column:organization_size"`
	Timezone         string     `gorm:"column:timezone"`
	BackgroundColor  string     `gorm:"column:background_color"`
}

func (Workspace) TableName() string { return "workspaces" }

type WorkspaceMember struct {
	ID                      string         `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt               time.Time      `gorm:"column:created_at"`
	UpdatedAt               time.Time      `gorm:"column:updated_at"`
	CreatedByID             *string        `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID             *string        `gorm:"column:updated_by_id;type:uuid"`
	DeletedAt               *time.Time     `gorm:"column:deleted_at"`
	WorkspaceID             string         `gorm:"column:workspace_id;type:uuid"`
	MemberID                string         `gorm:"column:member_id;type:uuid"`
	Role                    int            `gorm:"column:role"`
	CompanyRole             *string        `gorm:"column:company_role"`
	ViewProps               auth.JSONValue `gorm:"column:view_props;type:jsonb"`
	DefaultProps            auth.JSONValue `gorm:"column:default_props;type:jsonb"`
	IssueProps              auth.JSONValue `gorm:"column:issue_props;type:jsonb"`
	IsActive                bool           `gorm:"column:is_active"`
	GettingStartedChecklist auth.JSONValue `gorm:"column:getting_started_checklist;type:jsonb"`
	Tips                    auth.JSONValue `gorm:"column:tips;type:jsonb"`
	ExploredFeatures        auth.JSONValue `gorm:"column:explored_features;type:jsonb"`
}

func (WorkspaceMember) TableName() string { return "workspace_members" }

type WorkspaceInvite struct {
	ID          string     `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt   time.Time  `gorm:"column:created_at"`
	UpdatedAt   time.Time  `gorm:"column:updated_at"`
	CreatedByID *string    `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID *string    `gorm:"column:updated_by_id;type:uuid"`
	DeletedAt   *time.Time `gorm:"column:deleted_at"`
	WorkspaceID string     `gorm:"column:workspace_id;type:uuid"`
	Email       string     `gorm:"column:email"`
	Accepted    bool       `gorm:"column:accepted"`
	Token       string     `gorm:"column:token"`
	Message     *string    `gorm:"column:message"`
	RespondedAt *time.Time `gorm:"column:responded_at"`
	Role        int        `gorm:"column:role"`
}

func (WorkspaceInvite) TableName() string { return "workspace_member_invites" }

type WorkspaceTheme struct {
	ID          string         `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt   time.Time      `gorm:"column:created_at"`
	UpdatedAt   time.Time      `gorm:"column:updated_at"`
	CreatedByID *string        `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID *string        `gorm:"column:updated_by_id;type:uuid"`
	DeletedAt   *time.Time     `gorm:"column:deleted_at"`
	WorkspaceID string         `gorm:"column:workspace_id;type:uuid"`
	Name        string         `gorm:"column:name"`
	ActorID     string         `gorm:"column:actor_id;type:uuid"`
	Colors      auth.JSONValue `gorm:"column:colors;type:jsonb"`
}

func (WorkspaceTheme) TableName() string { return "workspace_themes" }

type WorkspaceUserProperties struct {
	ID                          string         `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt                   time.Time      `gorm:"column:created_at"`
	UpdatedAt                   time.Time      `gorm:"column:updated_at"`
	CreatedByID                 *string        `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID                 *string        `gorm:"column:updated_by_id;type:uuid"`
	DeletedAt                   *time.Time     `gorm:"column:deleted_at"`
	WorkspaceID                 string         `gorm:"column:workspace_id;type:uuid"`
	UserID                      string         `gorm:"column:user_id;type:uuid"`
	Filters                     auth.JSONValue `gorm:"column:filters;type:jsonb"`
	DisplayFilters              auth.JSONValue `gorm:"column:display_filters;type:jsonb"`
	DisplayProperties           auth.JSONValue `gorm:"column:display_properties;type:jsonb"`
	RichFilters                 auth.JSONValue `gorm:"column:rich_filters;type:jsonb"`
	NavigationProjectLimit      int            `gorm:"column:navigation_project_limit"`
	NavigationControlPreference string         `gorm:"column:navigation_control_preference"`
}

func (WorkspaceUserProperties) TableName() string { return "workspace_user_properties" }

type WorkspaceUserPreference struct {
	ID          string     `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt   time.Time  `gorm:"column:created_at"`
	UpdatedAt   time.Time  `gorm:"column:updated_at"`
	CreatedByID *string    `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID *string    `gorm:"column:updated_by_id;type:uuid"`
	DeletedAt   *time.Time `gorm:"column:deleted_at"`
	WorkspaceID string     `gorm:"column:workspace_id;type:uuid"`
	UserID      string     `gorm:"column:user_id;type:uuid"`
	Key         string     `gorm:"column:key"`
	IsPinned    bool       `gorm:"column:is_pinned"`
	SortOrder   float64    `gorm:"column:sort_order"`
}

func (WorkspaceUserPreference) TableName() string { return "workspace_user_preferences" }

type WorkspaceHomePreference struct {
	ID          string         `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt   time.Time      `gorm:"column:created_at"`
	UpdatedAt   time.Time      `gorm:"column:updated_at"`
	CreatedByID *string        `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID *string        `gorm:"column:updated_by_id;type:uuid"`
	DeletedAt   *time.Time     `gorm:"column:deleted_at"`
	WorkspaceID string         `gorm:"column:workspace_id;type:uuid"`
	UserID      string         `gorm:"column:user_id;type:uuid"`
	Key         string         `gorm:"column:key"`
	IsEnabled   bool           `gorm:"column:is_enabled"`
	Config      auth.JSONValue `gorm:"column:config;type:jsonb"`
	SortOrder   float64        `gorm:"column:sort_order"`
}

func (WorkspaceHomePreference) TableName() string { return "workspace_home_preferences" }

type workspaceRow struct {
	Workspace
	TotalMembers int `gorm:"column:total_members"`
	Role         int `gorm:"column:role"`
}
