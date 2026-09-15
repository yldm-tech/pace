package project

import (
	"time"

	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
)

// Cycle is the db.Cycle table.
type Cycle struct {
	ID               string         `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt        time.Time      `gorm:"column:created_at"`
	UpdatedAt        time.Time      `gorm:"column:updated_at"`
	CreatedByID      *string        `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID      *string        `gorm:"column:updated_by_id;type:uuid"`
	DeletedAt        *time.Time     `gorm:"column:deleted_at"`
	ProjectID        string         `gorm:"column:project_id;type:uuid"`
	WorkspaceID      string         `gorm:"column:workspace_id;type:uuid"`
	Name             string         `gorm:"column:name"`
	Description      string         `gorm:"column:description"`
	StartDate        *time.Time     `gorm:"column:start_date"`
	EndDate          *time.Time     `gorm:"column:end_date"`
	OwnedByID        string         `gorm:"column:owned_by_id;type:uuid"`
	ViewProps        auth.JSONValue `gorm:"column:view_props;type:jsonb"`
	SortOrder        float64        `gorm:"column:sort_order"`
	ExternalSource   *string        `gorm:"column:external_source"`
	ExternalID       *string        `gorm:"column:external_id"`
	ProgressSnapshot auth.JSONValue `gorm:"column:progress_snapshot;type:jsonb"`
	ArchivedAt       *time.Time     `gorm:"column:archived_at"`
	LogoProps        auth.JSONValue `gorm:"column:logo_props;type:jsonb"`
	Timezone         string         `gorm:"column:timezone"`
	Version          int            `gorm:"column:version"`
}

func (Cycle) TableName() string { return "cycles" }

// CycleUserProperties is one member's saved view of one cycle.
type CycleUserProperties struct {
	ID                string         `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt         time.Time      `gorm:"column:created_at"`
	UpdatedAt         time.Time      `gorm:"column:updated_at"`
	CreatedByID       *string        `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID       *string        `gorm:"column:updated_by_id;type:uuid"`
	DeletedAt         *time.Time     `gorm:"column:deleted_at"`
	ProjectID         string         `gorm:"column:project_id;type:uuid"`
	WorkspaceID       string         `gorm:"column:workspace_id;type:uuid"`
	CycleID           string         `gorm:"column:cycle_id;type:uuid"`
	UserID            string         `gorm:"column:user_id;type:uuid"`
	Filters           auth.JSONValue `gorm:"column:filters;type:jsonb"`
	DisplayFilters    auth.JSONValue `gorm:"column:display_filters;type:jsonb"`
	DisplayProperties auth.JSONValue `gorm:"column:display_properties;type:jsonb"`
	RichFilters       auth.JSONValue `gorm:"column:rich_filters;type:jsonb"`
}

func (CycleUserProperties) TableName() string { return "cycle_user_properties" }

// UserFavorite is the one table every kind of favourite shares, keyed by an entity type and the entity's id.
type UserFavorite struct {
	ID               string     `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt        time.Time  `gorm:"column:created_at"`
	UpdatedAt        time.Time  `gorm:"column:updated_at"`
	CreatedByID      *string    `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID      *string    `gorm:"column:updated_by_id;type:uuid"`
	DeletedAt        *time.Time `gorm:"column:deleted_at"`
	ProjectID        *string    `gorm:"column:project_id;type:uuid"`
	WorkspaceID      string     `gorm:"column:workspace_id;type:uuid"`
	UserID           string     `gorm:"column:user_id;type:uuid"`
	EntityType       string     `gorm:"column:entity_type"`
	EntityIdentifier *string    `gorm:"column:entity_identifier;type:uuid"`
	Name             *string    `gorm:"column:name"`
	IsFolder         bool       `gorm:"column:is_folder"`
	Sequence         float64    `gorm:"column:sequence"`
	ParentID         *string    `gorm:"column:parent_id;type:uuid"`
}

func (UserFavorite) TableName() string { return "user_favorites" }
