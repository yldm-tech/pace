package project

import (
	"time"

	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
)

// The models below mirror the existing Django tables. The Go API runs no
// migrations; PostgreSQL stays owned by the Django schema.
type Project struct {
	ID                    string         `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt             time.Time      `gorm:"column:created_at"`
	UpdatedAt             time.Time      `gorm:"column:updated_at"`
	CreatedByID           *string        `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID           *string        `gorm:"column:updated_by_id;type:uuid"`
	DeletedAt             *time.Time     `gorm:"column:deleted_at"`
	Name                  string         `gorm:"column:name"`
	Description           string         `gorm:"column:description"`
	DescriptionText       auth.JSONValue `gorm:"column:description_text;type:jsonb"`
	DescriptionHTML       auth.JSONValue `gorm:"column:description_html;type:jsonb"`
	Network               int            `gorm:"column:network"`
	WorkspaceID           string         `gorm:"column:workspace_id;type:uuid"`
	Identifier            string         `gorm:"column:identifier"`
	DefaultAssigneeID     *string        `gorm:"column:default_assignee_id;type:uuid"`
	ProjectLeadID         *string        `gorm:"column:project_lead_id;type:uuid"`
	Emoji                 *string        `gorm:"column:emoji"`
	IconProp              auth.JSONValue `gorm:"column:icon_prop;type:jsonb"`
	ModuleView            bool           `gorm:"column:module_view"`
	CycleView             bool           `gorm:"column:cycle_view"`
	IssueViewsView        bool           `gorm:"column:issue_views_view"`
	PageView              bool           `gorm:"column:page_view"`
	IntakeView            bool           `gorm:"column:intake_view"`
	IsTimeTrackingEnabled bool           `gorm:"column:is_time_tracking_enabled"`
	IsIssueTypeEnabled    bool           `gorm:"column:is_issue_type_enabled"`
	GuestViewAllFeatures  bool           `gorm:"column:guest_view_all_features"`
	CoverImage            *string        `gorm:"column:cover_image"`
	CoverImageAssetID     *string        `gorm:"column:cover_image_asset_id;type:uuid"`
	EstimateID            *string        `gorm:"column:estimate_id;type:uuid"`
	ArchiveIn             int            `gorm:"column:archive_in"`
	CloseIn               int            `gorm:"column:close_in"`
	LogoProps             auth.JSONValue `gorm:"column:logo_props;type:jsonb"`
	DefaultStateID        *string        `gorm:"column:default_state_id;type:uuid"`
	ArchivedAt            *time.Time     `gorm:"column:archived_at"`
	Timezone              string         `gorm:"column:timezone"`
	ExternalSource        *string        `gorm:"column:external_source"`
	ExternalID            *string        `gorm:"column:external_id"`
}

func (Project) TableName() string { return "projects" }

type ProjectMember struct {
	ID           string         `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt    time.Time      `gorm:"column:created_at"`
	UpdatedAt    time.Time      `gorm:"column:updated_at"`
	CreatedByID  *string        `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID  *string        `gorm:"column:updated_by_id;type:uuid"`
	DeletedAt    *time.Time     `gorm:"column:deleted_at"`
	ProjectID    string         `gorm:"column:project_id;type:uuid"`
	WorkspaceID  string         `gorm:"column:workspace_id;type:uuid"`
	MemberID     string         `gorm:"column:member_id;type:uuid"`
	Comment      *string        `gorm:"column:comment"`
	Role         int            `gorm:"column:role"`
	ViewProps    auth.JSONValue `gorm:"column:view_props;type:jsonb"`
	DefaultProps auth.JSONValue `gorm:"column:default_props;type:jsonb"`
	Preferences  auth.JSONValue `gorm:"column:preferences;type:jsonb"`
	SortOrder    float64        `gorm:"column:sort_order"`
	IsActive     bool           `gorm:"column:is_active"`
}

func (ProjectMember) TableName() string { return "project_members" }

type ProjectIdentifier struct {
	ID          string     `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt   time.Time  `gorm:"column:created_at"`
	UpdatedAt   time.Time  `gorm:"column:updated_at"`
	CreatedByID *string    `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID *string    `gorm:"column:updated_by_id;type:uuid"`
	DeletedAt   *time.Time `gorm:"column:deleted_at"`
	WorkspaceID *string    `gorm:"column:workspace_id;type:uuid"`
	ProjectID   string     `gorm:"column:project_id;type:uuid"`
	Name        string     `gorm:"column:name"`
}

func (ProjectIdentifier) TableName() string { return "project_identifiers" }

type ProjectUserProperty struct {
	ID                string         `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt         time.Time      `gorm:"column:created_at"`
	UpdatedAt         time.Time      `gorm:"column:updated_at"`
	CreatedByID       *string        `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID       *string        `gorm:"column:updated_by_id;type:uuid"`
	DeletedAt         *time.Time     `gorm:"column:deleted_at"`
	ProjectID         string         `gorm:"column:project_id;type:uuid"`
	WorkspaceID       string         `gorm:"column:workspace_id;type:uuid"`
	UserID            string         `gorm:"column:user_id;type:uuid"`
	Filters           auth.JSONValue `gorm:"column:filters;type:jsonb"`
	DisplayFilters    auth.JSONValue `gorm:"column:display_filters;type:jsonb"`
	DisplayProperties auth.JSONValue `gorm:"column:display_properties;type:jsonb"`
	RichFilters       auth.JSONValue `gorm:"column:rich_filters;type:jsonb"`
	Preferences       auth.JSONValue `gorm:"column:preferences;type:jsonb"`
	SortOrder         float64        `gorm:"column:sort_order"`
}

func (ProjectUserProperty) TableName() string { return "project_user_properties" }

type State struct {
	ID          string     `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt   time.Time  `gorm:"column:created_at"`
	UpdatedAt   time.Time  `gorm:"column:updated_at"`
	CreatedByID *string    `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID *string    `gorm:"column:updated_by_id;type:uuid"`
	DeletedAt   *time.Time `gorm:"column:deleted_at"`
	ProjectID   string     `gorm:"column:project_id;type:uuid"`
	WorkspaceID string     `gorm:"column:workspace_id;type:uuid"`
	Name        string     `gorm:"column:name"`
	Description string     `gorm:"column:description"`
	Color       string     `gorm:"column:color"`
	Slug        string     `gorm:"column:slug"`
	Sequence    float64    `gorm:"column:sequence"`
	Group       string     `gorm:"column:group"`
	IsTriage    bool       `gorm:"column:is_triage"`
	Default     bool       `gorm:"column:default"`
	ExternalID  *string    `gorm:"column:external_id"`
	ExternalSrc *string    `gorm:"column:external_source"`
}

func (State) TableName() string { return "states" }

type Intake struct {
	ID          string         `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt   time.Time      `gorm:"column:created_at"`
	UpdatedAt   time.Time      `gorm:"column:updated_at"`
	CreatedByID *string        `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID *string        `gorm:"column:updated_by_id;type:uuid"`
	DeletedAt   *time.Time     `gorm:"column:deleted_at"`
	ProjectID   string         `gorm:"column:project_id;type:uuid"`
	WorkspaceID string         `gorm:"column:workspace_id;type:uuid"`
	Name        string         `gorm:"column:name"`
	Description string         `gorm:"column:description"`
	IsDefault   bool           `gorm:"column:is_default"`
	ViewProps   auth.JSONValue `gorm:"column:view_props;type:jsonb"`
	LogoProps   auth.JSONValue `gorm:"column:logo_props;type:jsonb"`
}

func (Intake) TableName() string { return "intakes" }

// projectRow carries the annotations ProjectViewSet.get_queryset adds.
type projectRow struct {
	Project
	IsFavorite bool     `gorm:"column:is_favorite"`
	MemberRole *int     `gorm:"column:member_role"`
	Anchor     *string  `gorm:"column:anchor"`
	SortOrder  *float64 `gorm:"column:sort_order"`
}

// projectListRow carries the lighter annotations the values() list route uses.
type projectListRow struct {
	ID                   string         `gorm:"column:id"`
	Name                 string         `gorm:"column:name"`
	Identifier           string         `gorm:"column:identifier"`
	SortOrder            *float64       `gorm:"column:sort_order"`
	LogoProps            auth.JSONValue `gorm:"column:logo_props"`
	MemberRole           *int           `gorm:"column:member_role"`
	IntakeCount          int            `gorm:"column:intake_count"`
	ArchivedAt           *time.Time     `gorm:"column:archived_at"`
	WorkspaceID          string         `gorm:"column:workspace_id"`
	CycleView            bool           `gorm:"column:cycle_view"`
	IssueViewsView       bool           `gorm:"column:issue_views_view"`
	ModuleView           bool           `gorm:"column:module_view"`
	PageView             bool           `gorm:"column:page_view"`
	InboxView            bool           `gorm:"column:inbox_view"`
	GuestViewAllFeatures bool           `gorm:"column:guest_view_all_features"`
	ProjectLeadID        *string        `gorm:"column:project_lead_id"`
	Network              int            `gorm:"column:network"`
	CreatedAt            time.Time      `gorm:"column:created_at"`
	UpdatedAt            time.Time      `gorm:"column:updated_at"`
	CreatedByID          *string        `gorm:"column:created_by_id"`
	UpdatedByID          *string        `gorm:"column:updated_by_id"`
}
