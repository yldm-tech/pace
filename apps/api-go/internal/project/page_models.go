package project

import "time"

// Page is the db.Page table. A page belongs to a workspace and reaches its projects through a link table, so the same page can sit in more than one.
type Page struct {
	ID                 string     `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt          time.Time  `gorm:"column:created_at"`
	UpdatedAt          time.Time  `gorm:"column:updated_at"`
	CreatedByID        *string    `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID        *string    `gorm:"column:updated_by_id;type:uuid"`
	DeletedAt          *time.Time `gorm:"column:deleted_at"`
	WorkspaceID        string     `gorm:"column:workspace_id;type:uuid"`
	Name               string     `gorm:"column:name"`
	DescriptionJSON    []byte     `gorm:"column:description_json;type:jsonb"`
	DescriptionBinary  []byte     `gorm:"column:description_binary"`
	DescriptionHTML    string     `gorm:"column:description_html"`
	DescriptionStrippd *string    `gorm:"column:description_stripped"`
	OwnedByID          string     `gorm:"column:owned_by_id;type:uuid"`
	Access             int        `gorm:"column:access"`
	Color              string     `gorm:"column:color"`
	ParentID           *string    `gorm:"column:parent_id;type:uuid"`
	// ArchivedAt is a date rather than a datetime, which is why archiving reports a day and not an instant.
	ArchivedAt  *time.Time `gorm:"column:archived_at"`
	IsLocked    bool       `gorm:"column:is_locked"`
	ViewProps   []byte     `gorm:"column:view_props;type:jsonb"`
	LogoProps   []byte     `gorm:"column:logo_props;type:jsonb"`
	IsGlobal    bool       `gorm:"column:is_global"`
	MovedToPage *string    `gorm:"column:moved_to_page;type:uuid"`
}

func (Page) TableName() string { return "pages" }

// ProjectPage is the link between a page and a project.
type ProjectPage struct {
	ID          string     `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt   time.Time  `gorm:"column:created_at"`
	UpdatedAt   time.Time  `gorm:"column:updated_at"`
	CreatedByID *string    `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID *string    `gorm:"column:updated_by_id;type:uuid"`
	DeletedAt   *time.Time `gorm:"column:deleted_at"`
	WorkspaceID string     `gorm:"column:workspace_id;type:uuid"`
	ProjectID   string     `gorm:"column:project_id;type:uuid"`
	PageID      string     `gorm:"column:page_id;type:uuid"`
}

func (ProjectPage) TableName() string { return "project_pages" }

// PageLabel is the link between a page and a label.
type PageLabel struct {
	ID          string     `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt   time.Time  `gorm:"column:created_at"`
	UpdatedAt   time.Time  `gorm:"column:updated_at"`
	CreatedByID *string    `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID *string    `gorm:"column:updated_by_id;type:uuid"`
	DeletedAt   *time.Time `gorm:"column:deleted_at"`
	WorkspaceID string     `gorm:"column:workspace_id;type:uuid"`
	ProjectID   *string    `gorm:"column:project_id;type:uuid"`
	PageID      string     `gorm:"column:page_id;type:uuid"`
	LabelID     string     `gorm:"column:label_id;type:uuid"`
}

func (PageLabel) TableName() string { return "page_labels" }
