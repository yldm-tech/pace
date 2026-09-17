package project

import (
	"time"

	"github.com/yldm-tech/pace/apps/api/internal/issues"
	"gorm.io/gorm"
)

// applyIssueSavePath is Issue.save's non-adding path, which runs whenever a serializer saves a work item and changes columns the request never mentioned. It lives in internal/issues because the external API updates work items through the same model.
func applyIssueSavePath(tx *gorm.DB, issue Issue, updates map[string]any, now time.Time) error {
	return issues.ApplyUpdate(tx, updates, issue.ProjectID, issue.StateID, issue.DescriptionHTML, now)
}
