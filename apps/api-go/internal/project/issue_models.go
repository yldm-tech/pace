package project

import "time"

// The issue-adjacent tables these routes touch. The Issue row itself is not
// modelled yet; these routes only need its identifier.
type IssueReaction struct {
	ID          string     `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt   time.Time  `gorm:"column:created_at"`
	UpdatedAt   time.Time  `gorm:"column:updated_at"`
	CreatedByID *string    `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID *string    `gorm:"column:updated_by_id;type:uuid"`
	DeletedAt   *time.Time `gorm:"column:deleted_at"`
	ProjectID   string     `gorm:"column:project_id;type:uuid"`
	WorkspaceID string     `gorm:"column:workspace_id;type:uuid"`
	IssueID     string     `gorm:"column:issue_id;type:uuid"`
	ActorID     string     `gorm:"column:actor_id;type:uuid"`
	Reaction    string     `gorm:"column:reaction"`
}

func (IssueReaction) TableName() string { return "issue_reactions" }

type IssueSubscriber struct {
	ID           string     `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt    time.Time  `gorm:"column:created_at"`
	UpdatedAt    time.Time  `gorm:"column:updated_at"`
	CreatedByID  *string    `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID  *string    `gorm:"column:updated_by_id;type:uuid"`
	DeletedAt    *time.Time `gorm:"column:deleted_at"`
	ProjectID    string     `gorm:"column:project_id;type:uuid"`
	WorkspaceID  string     `gorm:"column:workspace_id;type:uuid"`
	IssueID      string     `gorm:"column:issue_id;type:uuid"`
	SubscriberID string     `gorm:"column:subscriber_id;type:uuid"`
}

func (IssueSubscriber) TableName() string { return "issue_subscribers" }
