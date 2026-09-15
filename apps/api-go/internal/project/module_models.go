package project

import (
	"time"

	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
)

// Module is the db.Module table. Its two description columns are both JSON, unlike an issue's, whose html column is text.
type Module struct {
	ID              string         `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt       time.Time      `gorm:"column:created_at"`
	UpdatedAt       time.Time      `gorm:"column:updated_at"`
	CreatedByID     *string        `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID     *string        `gorm:"column:updated_by_id;type:uuid"`
	DeletedAt       *time.Time     `gorm:"column:deleted_at"`
	ProjectID       string         `gorm:"column:project_id;type:uuid"`
	WorkspaceID     string         `gorm:"column:workspace_id;type:uuid"`
	Name            string         `gorm:"column:name"`
	Description     string         `gorm:"column:description"`
	DescriptionText auth.JSONValue `gorm:"column:description_text;type:jsonb"`
	DescriptionHTML auth.JSONValue `gorm:"column:description_html;type:jsonb"`
	StartDate       *time.Time     `gorm:"column:start_date"`
	TargetDate      *time.Time     `gorm:"column:target_date"`
	Status          string         `gorm:"column:status"`
	LeadID          *string        `gorm:"column:lead_id;type:uuid"`
	ViewProps       auth.JSONValue `gorm:"column:view_props;type:jsonb"`
	SortOrder       float64        `gorm:"column:sort_order"`
	ExternalSource  *string        `gorm:"column:external_source"`
	ExternalID      *string        `gorm:"column:external_id"`
	ArchivedAt      *time.Time     `gorm:"column:archived_at"`
	LogoProps       auth.JSONValue `gorm:"column:logo_props;type:jsonb"`
}

func (Module) TableName() string { return "modules" }

// ModuleMember is the m2m between a module and the people working on it.
type ModuleMember struct {
	ID          string     `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt   time.Time  `gorm:"column:created_at"`
	UpdatedAt   time.Time  `gorm:"column:updated_at"`
	CreatedByID *string    `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID *string    `gorm:"column:updated_by_id;type:uuid"`
	DeletedAt   *time.Time `gorm:"column:deleted_at"`
	ProjectID   string     `gorm:"column:project_id;type:uuid"`
	WorkspaceID string     `gorm:"column:workspace_id;type:uuid"`
	ModuleID    string     `gorm:"column:module_id;type:uuid"`
	MemberID    string     `gorm:"column:member_id;type:uuid"`
}

func (ModuleMember) TableName() string { return "module_members" }

// ModuleIssue links a module to an issue.
type ModuleIssue struct {
	ID          string     `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt   time.Time  `gorm:"column:created_at"`
	UpdatedAt   time.Time  `gorm:"column:updated_at"`
	CreatedByID *string    `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID *string    `gorm:"column:updated_by_id;type:uuid"`
	DeletedAt   *time.Time `gorm:"column:deleted_at"`
	ProjectID   string     `gorm:"column:project_id;type:uuid"`
	WorkspaceID string     `gorm:"column:workspace_id;type:uuid"`
	ModuleID    string     `gorm:"column:module_id;type:uuid"`
	IssueID     string     `gorm:"column:issue_id;type:uuid"`
}

func (ModuleIssue) TableName() string { return "module_issues" }

// ModuleLink is a link pinned to a module.
type ModuleLink struct {
	ID          string         `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt   time.Time      `gorm:"column:created_at"`
	UpdatedAt   time.Time      `gorm:"column:updated_at"`
	CreatedByID *string        `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID *string        `gorm:"column:updated_by_id;type:uuid"`
	DeletedAt   *time.Time     `gorm:"column:deleted_at"`
	ProjectID   string         `gorm:"column:project_id;type:uuid"`
	WorkspaceID string         `gorm:"column:workspace_id;type:uuid"`
	ModuleID    string         `gorm:"column:module_id;type:uuid"`
	Title       *string        `gorm:"column:title"`
	URL         string         `gorm:"column:url"`
	Metadata    auth.JSONValue `gorm:"column:metadata;type:jsonb"`
}

func (ModuleLink) TableName() string { return "module_links" }

// ModuleUserProperties is one member's saved view of one module.
type ModuleUserProperties struct {
	ID                string         `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt         time.Time      `gorm:"column:created_at"`
	UpdatedAt         time.Time      `gorm:"column:updated_at"`
	CreatedByID       *string        `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID       *string        `gorm:"column:updated_by_id;type:uuid"`
	DeletedAt         *time.Time     `gorm:"column:deleted_at"`
	ProjectID         string         `gorm:"column:project_id;type:uuid"`
	WorkspaceID       string         `gorm:"column:workspace_id;type:uuid"`
	ModuleID          string         `gorm:"column:module_id;type:uuid"`
	UserID            string         `gorm:"column:user_id;type:uuid"`
	Filters           auth.JSONValue `gorm:"column:filters;type:jsonb"`
	DisplayFilters    auth.JSONValue `gorm:"column:display_filters;type:jsonb"`
	DisplayProperties auth.JSONValue `gorm:"column:display_properties;type:jsonb"`
	RichFilters       auth.JSONValue `gorm:"column:rich_filters;type:jsonb"`
}

func (ModuleUserProperties) TableName() string { return "module_user_properties" }
