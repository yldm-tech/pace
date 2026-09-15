package project

import (
	"strings"
	"time"

	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
)

// IssueRelation is the db.IssueRelation table.
type IssueRelation struct {
	ID             string     `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt      time.Time  `gorm:"column:created_at"`
	UpdatedAt      time.Time  `gorm:"column:updated_at"`
	CreatedByID    *string    `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID    *string    `gorm:"column:updated_by_id;type:uuid"`
	DeletedAt      *time.Time `gorm:"column:deleted_at"`
	ProjectID      string     `gorm:"column:project_id;type:uuid"`
	WorkspaceID    string     `gorm:"column:workspace_id;type:uuid"`
	IssueID        string     `gorm:"column:issue_id;type:uuid"`
	RelatedIssueID string     `gorm:"column:related_issue_id;type:uuid"`
	RelationType   string     `gorm:"column:relation_type"`
}

func (IssueRelation) TableName() string { return "issue_relations" }

// storedRelationType is get_actual_relation: the six directions a caller may ask for collapse onto the three stored types, because a relation is recorded once and read from whichever end the caller is standing at. A type it does not name is stored verbatim, which is what lets duplicate and relates_to through unchanged.
func storedRelationType(requested string) string {
	stored := map[string]string{
		"start_after":    "start_before",
		"finish_after":   "finish_before",
		"blocking":       "blocked_by",
		"blocked_by":     "blocked_by",
		"start_before":   "start_before",
		"finish_before":  "finish_before",
		"implemented_by": "implemented_by",
		"implements":     "implemented_by",
	}
	if actual, named := stored[requested]; named {
		return actual
	}
	return requested
}

// relationIsReversed says whether a requested type is the reading from the far end, in which case the row is written with the two issues swapped.
func relationIsReversed(requested string) bool {
	switch requested {
	case "blocking", "start_after", "finish_after":
		return true
	}
	return false
}

// FileAsset is the db.FileAsset table. Only the columns the attachment routes touch are mapped, which is all of them: IssueAttachmentSerializer is fields = "__all__".
type FileAsset struct {
	ID               string         `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt        time.Time      `gorm:"column:created_at"`
	UpdatedAt        time.Time      `gorm:"column:updated_at"`
	CreatedByID      *string        `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID      *string        `gorm:"column:updated_by_id;type:uuid"`
	DeletedAt        *time.Time     `gorm:"column:deleted_at"`
	Attributes       auth.JSONValue `gorm:"column:attributes;type:jsonb"`
	Asset            string         `gorm:"column:asset"`
	UserID           *string        `gorm:"column:user_id;type:uuid"`
	WorkspaceID      *string        `gorm:"column:workspace_id;type:uuid"`
	DraftIssueID     *string        `gorm:"column:draft_issue_id;type:uuid"`
	ProjectID        *string        `gorm:"column:project_id;type:uuid"`
	IssueID          *string        `gorm:"column:issue_id;type:uuid"`
	CommentID        *string        `gorm:"column:comment_id;type:uuid"`
	PageID           *string        `gorm:"column:page_id;type:uuid"`
	EntityType       *string        `gorm:"column:entity_type"`
	EntityIdentifier *string        `gorm:"column:entity_identifier"`
	IsDeleted        bool           `gorm:"column:is_deleted"`
	IsArchived       bool           `gorm:"column:is_archived"`
	ExternalID       *string        `gorm:"column:external_id"`
	ExternalSource   *string        `gorm:"column:external_source"`
	Size             float64        `gorm:"column:size"`
	IsUploaded       bool           `gorm:"column:is_uploaded"`
	StorageMetadata  auth.JSONValue `gorm:"column:storage_metadata;type:jsonb"`
}

func (FileAsset) TableName() string { return "file_assets" }

// HasStorageMetadata says whether the row already carries what the metadata task would fetch. Django's default is an empty object, so both null and {} count as missing.
func (asset FileAsset) HasStorageMetadata() bool {
	text := strings.TrimSpace(string(asset.StorageMetadata))
	return text != "" && text != "null" && text != "{}"
}
