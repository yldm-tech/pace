package project

import (
	"time"

	"github.com/lib/pq"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
)

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

type IssueLink struct {
	ID          string         `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt   time.Time      `gorm:"column:created_at"`
	UpdatedAt   time.Time      `gorm:"column:updated_at"`
	CreatedByID *string        `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID *string        `gorm:"column:updated_by_id;type:uuid"`
	DeletedAt   *time.Time     `gorm:"column:deleted_at"`
	ProjectID   string         `gorm:"column:project_id;type:uuid"`
	WorkspaceID string         `gorm:"column:workspace_id;type:uuid"`
	IssueID     string         `gorm:"column:issue_id;type:uuid"`
	Title       *string        `gorm:"column:title"`
	URL         string         `gorm:"column:url"`
	Metadata    auth.JSONValue `gorm:"column:metadata;type:jsonb"`
}

func (IssueLink) TableName() string { return "issue_links" }

type IssueComment struct {
	ID              string         `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt       time.Time      `gorm:"column:created_at"`
	UpdatedAt       time.Time      `gorm:"column:updated_at"`
	CreatedByID     *string        `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID     *string        `gorm:"column:updated_by_id;type:uuid"`
	DeletedAt       *time.Time     `gorm:"column:deleted_at"`
	ProjectID       string         `gorm:"column:project_id;type:uuid"`
	WorkspaceID     string         `gorm:"column:workspace_id;type:uuid"`
	IssueID         string         `gorm:"column:issue_id;type:uuid"`
	ActorID         *string        `gorm:"column:actor_id;type:uuid"`
	ParentID        *string        `gorm:"column:parent_id;type:uuid"`
	DescriptionID   *string        `gorm:"column:description_id;type:uuid"`
	CommentStripped string         `gorm:"column:comment_stripped"`
	CommentJSON     auth.JSONValue `gorm:"column:comment_json;type:jsonb"`
	CommentHTML     string         `gorm:"column:comment_html"`
	Attachments     pq.StringArray `gorm:"column:attachments;type:text[]"`
	Access          string         `gorm:"column:access"`
	ExternalSource  *string        `gorm:"column:external_source"`
	ExternalID      *string        `gorm:"column:external_id"`
	EditedAt        *time.Time     `gorm:"column:edited_at"`
}

func (IssueComment) TableName() string { return "issue_comments" }

// Description is the row IssueComment.save keeps in step with the comment.
type Description struct {
	ID                  string         `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt           time.Time      `gorm:"column:created_at"`
	UpdatedAt           time.Time      `gorm:"column:updated_at"`
	CreatedByID         *string        `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID         *string        `gorm:"column:updated_by_id;type:uuid"`
	DeletedAt           *time.Time     `gorm:"column:deleted_at"`
	WorkspaceID         string         `gorm:"column:workspace_id;type:uuid"`
	ProjectID           *string        `gorm:"column:project_id;type:uuid"`
	DescriptionJSON     auth.JSONValue `gorm:"column:description_json;type:jsonb"`
	DescriptionHTML     string         `gorm:"column:description_html"`
	DescriptionStripped *string        `gorm:"column:description_stripped"`
}

func (Description) TableName() string { return "descriptions" }

type CommentReaction struct {
	ID          string     `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt   time.Time  `gorm:"column:created_at"`
	UpdatedAt   time.Time  `gorm:"column:updated_at"`
	CreatedByID *string    `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID *string    `gorm:"column:updated_by_id;type:uuid"`
	DeletedAt   *time.Time `gorm:"column:deleted_at"`
	ProjectID   string     `gorm:"column:project_id;type:uuid"`
	WorkspaceID string     `gorm:"column:workspace_id;type:uuid"`
	CommentID   string     `gorm:"column:comment_id;type:uuid"`
	ActorID     string     `gorm:"column:actor_id;type:uuid"`
	Reaction    string     `gorm:"column:reaction"`
}

func (CommentReaction) TableName() string { return "comment_reactions" }
