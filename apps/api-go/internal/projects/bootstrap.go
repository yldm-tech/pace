// Package projects holds what happens when a project is made, which both APIs do and neither does for itself.
//
// Creating a project is more than a row. The workflow states come from one list, the creator is made an administrator, and every membership seeds a per-person ordering so a new project appears at the top of that person's list. All three happen in the model rather than in a view, so both APIs have to do them the same way.
package projects

import (
	"time"

	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"gorm.io/gorm"
)

// State is a workflow state as the seeding writes one.
type State struct {
	ID          string    `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt   time.Time `gorm:"column:created_at"`
	UpdatedAt   time.Time `gorm:"column:updated_at"`
	CreatedByID *string   `gorm:"column:created_by_id;type:uuid"`
	ProjectID   string    `gorm:"column:project_id;type:uuid"`
	WorkspaceID string    `gorm:"column:workspace_id;type:uuid"`
	Name        string    `gorm:"column:name"`
	Color       string    `gorm:"column:color"`
	Sequence    float64   `gorm:"column:sequence"`
	Group       string    `gorm:"column:group"`
	Default     bool      `gorm:"column:default"`
	Slug        string    `gorm:"column:slug"`
}

func (State) TableName() string { return "states" }

// DefaultState is one row of DEFAULT_STATES.
type DefaultState struct {
	Name     string
	Color    string
	Sequence float64
	Group    string
	Default  bool
}

// DefaultStates is DEFAULT_STATES from plane/db/models/state.py, which every new project starts with.
var DefaultStates = []DefaultState{
	{Name: "Backlog", Color: "#60646C", Sequence: 15000, Group: "backlog", Default: true},
	{Name: "Todo", Color: "#60646C", Sequence: 25000, Group: "unstarted"},
	{Name: "In Progress", Color: "#F59E0B", Sequence: 35000, Group: "started"},
	{Name: "Done", Color: "#46A758", Sequence: 45000, Group: "completed"},
	{Name: "Cancelled", Color: "#9AA4BC", Sequence: 55000, Group: "cancelled"},
	{Name: "Triage", Color: "#4E5355", Sequence: 65000, Group: "triage"},
}

// CreateDefaultStates writes the six states a project starts with.
//
// Django uses bulk_create, so State.save never runs: the slug stays empty and the sequence keeps the literal it was given rather than being spaced out.
func CreateDefaultStates(tx *gorm.DB, projectID, workspaceID, actorID string, now time.Time, identifiers func() (string, error)) error {
	states := make([]State, 0, len(DefaultStates))
	for _, definition := range DefaultStates {
		stateID, err := identifiers()
		if err != nil {
			return err
		}
		states = append(states, State{
			ID: stateID, CreatedAt: now, UpdatedAt: now, CreatedByID: &actorID,
			ProjectID: projectID, WorkspaceID: workspaceID,
			Name: definition.Name, Color: definition.Color, Sequence: definition.Sequence,
			Group: definition.Group, Default: definition.Default,
		})
	}
	return tx.Create(&states).Error
}

// MemberProperty is the per-person ordering a membership seeds.
type MemberProperty struct {
	ID                string         `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt         time.Time      `gorm:"column:created_at"`
	UpdatedAt         time.Time      `gorm:"column:updated_at"`
	CreatedByID       *string        `gorm:"column:created_by_id;type:uuid"`
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

func (MemberProperty) TableName() string { return "project_user_properties" }

// Member is a project membership as the bootstrap writes one.
type Member struct {
	ID           string         `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt    time.Time      `gorm:"column:created_at"`
	UpdatedAt    time.Time      `gorm:"column:updated_at"`
	CreatedByID  *string        `gorm:"column:created_by_id;type:uuid"`
	ProjectID    string         `gorm:"column:project_id;type:uuid"`
	WorkspaceID  string         `gorm:"column:workspace_id;type:uuid"`
	MemberID     string         `gorm:"column:member_id;type:uuid"`
	Role         int            `gorm:"column:role"`
	ViewProps    auth.JSONValue `gorm:"column:view_props;type:jsonb"`
	DefaultProps auth.JSONValue `gorm:"column:default_props;type:jsonb"`
	Preferences  auth.JSONValue `gorm:"column:preferences;type:jsonb"`
	SortOrder    float64        `gorm:"column:sort_order"`
	IsActive     bool           `gorm:"column:is_active"`
}

func (Member) TableName() string { return "project_members" }

// AddMember is ProjectMember.save on the adding path: the membership itself, and the per-person ordering it seeds ahead of everything that person already has.
func AddMember(tx *gorm.DB, projectID, workspaceID, memberID, actorID string, role int, now time.Time, identifiers func() (string, error)) error {
	var minimum *float64
	err := tx.Table("project_user_properties").
		Where("workspace_id = ? AND user_id = ? AND deleted_at IS NULL", workspaceID, memberID).
		Select("MIN(sort_order)").Scan(&minimum).Error
	if err != nil {
		return err
	}
	sortOrder := 65535.0
	if minimum != nil {
		sortOrder = *minimum - 10000
	}
	propertyID, err := identifiers()
	if err != nil {
		return err
	}
	property := MemberProperty{
		ID: propertyID, CreatedAt: now, UpdatedAt: now, CreatedByID: &actorID,
		ProjectID: projectID, WorkspaceID: workspaceID, UserID: memberID,
		Filters: DefaultFiltersJSON(), DisplayFilters: DefaultDisplayFiltersJSON(),
		DisplayProperties: DefaultDisplayPropertiesJSON(), RichFilters: EmptyJSON(),
		Preferences: DefaultPreferencesJSON(), SortOrder: sortOrder,
	}
	if err := tx.Create(&property).Error; err != nil {
		return err
	}
	memberRowID, err := identifiers()
	if err != nil {
		return err
	}
	// The membership's own sort order is the column default rather than the one just computed, which is the per-person ordering.
	return tx.Create(&Member{
		ID: memberRowID, CreatedAt: now, UpdatedAt: now, CreatedByID: &actorID,
		ProjectID: projectID, WorkspaceID: workspaceID, MemberID: memberID,
		Role: role, ViewProps: DefaultPropsJSON(), DefaultProps: DefaultPropsJSON(),
		Preferences: DefaultPreferencesJSON(), SortOrder: 65535, IsActive: true,
	}).Error
}

// The column defaults the two models declare, which apply to every row either API writes.

func EmptyJSON() auth.JSONValue { return auth.JSONValue([]byte(`{}`)) }

func DefaultPropsJSON() auth.JSONValue {
	return auth.JSONValue([]byte(`{"filters":{"priority":null,"state":null,"state_group":null,"assignees":null,"created_by":null,"labels":null,"start_date":null,"target_date":null,"subscriber":null},"display_filters":{"group_by":null,"order_by":"-created_at","type":null,"sub_issue":true,"show_empty_groups":true,"layout":"list","calendar_date_range":""}}`))
}

func DefaultPreferencesJSON() auth.JSONValue {
	return auth.JSONValue([]byte(`{"pages":{"block_display":true},"navigation":{"default_tab":"work_items","hide_in_more_menu":[]}}`))
}

func DefaultFiltersJSON() auth.JSONValue {
	return auth.JSONValue([]byte(`{"priority":null,"state":null,"state_group":null,"assignees":null,"created_by":null,"labels":null,"start_date":null,"target_date":null,"subscriber":null}`))
}

func DefaultDisplayFiltersJSON() auth.JSONValue {
	return auth.JSONValue([]byte(`{"group_by":null,"order_by":"-created_at","type":null,"sub_issue":true,"show_empty_groups":true,"layout":"list","calendar_date_range":""}`))
}

func DefaultDisplayPropertiesJSON() auth.JSONValue {
	return auth.JSONValue([]byte(`{"assignee":true,"attachment_count":true,"created_on":true,"due_date":true,"estimate":true,"key":true,"labels":true,"link":true,"priority":true,"start_date":true,"state":true,"sub_issue_count":true,"updated_on":true}`))
}
