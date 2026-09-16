package manage

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/yldm-tech/pace/apps/api-go/internal/issues"
	"gorm.io/gorm"
)

// updateDeletedWorkspaceSlug is manage.py update_deleted_workspace_slug: free a slug a soft-deleted workspace is still holding, by appending the moment it was deleted.
func updateDeletedWorkspaceSlug(ctx context.Context, env Environment, arguments []string) error {
	db, err := database(env)
	if err != nil {
		return err
	}
	values := positional(arguments)
	if len(values) == 0 {
		return commandError("Error: the workspace slug is required")
	}
	slug := values[0]
	dryRun := hasFlag(arguments, "dry-run")

	var workspace struct {
		Name      string     `gorm:"column:name"`
		Slug      string     `gorm:"column:slug"`
		DeletedAt *time.Time `gorm:"column:deleted_at"`
	}
	// all_objects rather than the default manager, since the whole point is a workspace that has been deleted.
	err = db.WithContext(ctx).Table("workspaces").Where("slug = ?", slug).
		Select("name, slug, deleted_at").Take(&workspace).Error
	if err != nil {
		write(env, "Workspace with slug '%s' not found.", slug)
		return nil
	}
	if workspace.DeletedAt == nil {
		write(env, "Workspace '%s' (slug: %s) is not deleted.", workspace.Name, workspace.Slug)
		return nil
	}
	if alreadyStamped(workspace.Slug) {
		write(env, "Workspace '%s' (slug: %s) already has a timestamp appended.", workspace.Name, workspace.Slug)
		return nil
	}

	updated := workspace.Slug + "__" + strconv.FormatInt(workspace.DeletedAt.Unix(), 10)
	if dryRun {
		write(env, "Would update workspace '%s' slug from '%s' to '%s'", workspace.Name, workspace.Slug, updated)
		return nil
	}
	err = db.WithContext(ctx).Table("workspaces").Where("slug = ?", slug).
		Update("slug", updated).Error
	if err != nil {
		write(env, "Error updating workspace '%s': %s", workspace.Name, err)
		return nil
	}
	// The message is written after the change, and the object it reads the old slug off has already been changed — so both halves of "from X to Y" say the new slug. Reproduced rather than corrected.
	write(env, "Updated workspace '%s' slug from '%s' to '%s'", workspace.Name, updated, updated)
	return nil
}

// alreadyStamped says whether the slug already ends in the epoch seconds this would append.
func alreadyStamped(slug string) bool {
	if !strings.Contains(slug, "__") {
		return false
	}
	parts := strings.Split(slug, "__")
	tail := parts[len(parts)-1]
	if tail == "" {
		return false
	}
	for _, character := range tail {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

// copyIssueCommentToDescription is manage.py copy_issue_comment_to_description: give every comment that has none its own description row.
func copyIssueCommentToDescription(ctx context.Context, env Environment, _ []string) error {
	db, err := database(env)
	if err != nil {
		return err
	}
	const batchSize = 500
	for {
		var comments []struct {
			ID              string    `gorm:"column:id"`
			CreatedAt       time.Time `gorm:"column:created_at"`
			UpdatedAt       time.Time `gorm:"column:updated_at"`
			CommentJSON     []byte    `gorm:"column:comment_json"`
			CommentHTML     string    `gorm:"column:comment_html"`
			CommentStripped *string   `gorm:"column:comment_stripped"`
			ProjectID       *string   `gorm:"column:project_id"`
			WorkspaceID     *string   `gorm:"column:workspace_id"`
			CreatedByID     *string   `gorm:"column:created_by_id"`
			UpdatedByID     *string   `gorm:"column:updated_by_id"`
		}
		err := db.WithContext(ctx).Table("issue_comments").
			Where("description_id IS NULL AND deleted_at IS NULL").
			Order("created_at").Limit(batchSize).
			Select("id, created_at, updated_at, comment_json, comment_html, comment_stripped, project_id, workspace_id, created_by_id, updated_by_id").
			Scan(&comments).Error
		if err != nil {
			return err
		}
		if len(comments) == 0 {
			break
		}
		err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			for _, comment := range comments {
				descriptionID := uuid.NewString()
				// The description keeps the comment's own timestamps and author rather than the moment it was copied.
				err := tx.Table("descriptions").Create(map[string]any{
					"id": descriptionID, "created_at": comment.CreatedAt, "updated_at": comment.UpdatedAt,
					"created_by_id": comment.CreatedByID, "updated_by_id": comment.UpdatedByID,
					"project_id": comment.ProjectID, "workspace_id": comment.WorkspaceID,
					"description_json": jsonOrEmpty(comment.CommentJSON), "description_html": comment.CommentHTML,
					"description_stripped": comment.CommentStripped,
				}).Error
				if err != nil {
					return err
				}
				// bulk_update writes the one column it names, so the comment's own updated_at stays where it was.
				err = tx.Table("issue_comments").Where("id = ?", comment.ID).
					Update("description_id", descriptionID).Error
				if err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	write(env, "Successfully Copied IssueComment to Description")
	return nil
}

func jsonOrEmpty(value []byte) string {
	if len(value) == 0 {
		return "{}"
	}
	return string(value)
}

// fixDuplicateSequences is manage.py fix_duplicate_sequences: give every work item after the first its own number, when two ended up sharing one.
//
// The workspace slug is asked for on the terminal rather than taken as an argument, which is upstream's shape and is kept.
func fixDuplicateSequences(ctx context.Context, env Environment, arguments []string) error {
	db, err := database(env)
	if err != nil {
		return err
	}
	slug, err := env.Prompt("Workspace slug: ")
	if err != nil {
		return err
	}
	if slug == "" {
		return commandError("Workspace slug is required")
	}
	values := positional(arguments)
	if len(values) == 0 || values[0] == "" {
		return commandError("Issue identifier is required")
	}

	parts := strings.Split(values[0], "-")
	if len(parts) != 2 {
		return commandError("Invalid issue identifier format")
	}
	sequence, err := strconv.Atoi(parts[1])
	if err != nil {
		return commandError("Invalid integer string")
	}

	var project struct {
		ID string `gorm:"column:id"`
	}
	err = db.WithContext(ctx).Table("projects p").
		Joins("JOIN workspaces w ON w.id = p.workspace_id").
		Where("UPPER(p.identifier) = UPPER(?) AND w.slug = ? AND p.deleted_at IS NULL", parts[0], slug).
		Select("p.id").Take(&project).Error
	if err != nil {
		return commandError("Project matching query does not exist.")
	}

	var duplicates []string
	err = db.WithContext(ctx).Table("issues").
		Where("project_id = ? AND sequence_id = ? AND deleted_at IS NULL", project.ID, sequence).
		Order("created_at DESC").Pluck("id", &duplicates).Error
	if err != nil {
		return commandError("%s", err)
	}
	if len(duplicates) <= 1 {
		return commandError("No duplicate issues found with the given identifier")
	}
	write(env, "%d issues found with identifier %s", len(duplicates), values[0])

	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// One transaction per project at a time, which is what keeps two runs from handing out the same number.
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", issues.AdvisoryLockKey(project.ID)).Error; err != nil {
			return err
		}
		var largest *int
		err := tx.Table("issue_sequences").Where("project_id = ? AND deleted_at IS NULL", project.ID).
			Select("MAX(sequence)").Scan(&largest).Error
		if err != nil {
			return err
		}
		last := 0
		if largest != nil {
			last = *largest
		}
		// The first of the duplicates keeps the number; everything after it is moved past the end.
		for index, issueID := range duplicates[1:] {
			updated := last + index + 1
			if err := tx.Table("issues").Where("id = ?", issueID).Update("sequence_id", updated).Error; err != nil {
				return err
			}
			err := tx.Table("issue_sequences").
				Where("issue_id = ? AND project_id = ? AND deleted_at IS NULL", issueID, project.ID).
				Update("sequence", updated).Error
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return commandError("%s", err)
	}
	write(env, "Sequence IDs updated successfully")
	return nil
}
