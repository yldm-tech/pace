package worker

import (
	"context"
	"fmt"
	"time"
)

// HardDeleteTask is plane.bgtasks.deletion_task.hard_delete, which the beat
// schedule runs daily at UTC 00:00.
const HardDeleteTask = "plane.bgtasks.deletion_task.hard_delete"

// hardDeleteLeadModels is the explicit list hard_delete walks before it sweeps
// every remaining model. The order is Django's, and it matters: each of these
// cascades into the ones below it, so a later sweep finds less to do.
var hardDeleteLeadModels = []string{
	"db.workspace", "db.project", "db.cycle", "db.module", "db.issue", "db.page",
	"db.issueview", "db.label", "db.state", "db.issueactivity", "db.issuecomment",
	"db.issuelink", "db.issuereaction", "db.userfavorite", "db.moduleissue",
	"db.cycleissue", "db.estimate", "db.estimatepoint",
}

// HardDeleteAfterDays is the settings default for HARD_DELETE_AFTER_DAYS.
const HardDeleteAfterDays = 60

func (tasks *DeletionTasks) hardDelete(ctx context.Context, _ []any, _ map[string]any) error {
	cutoff := tasks.clock().UTC().AddDate(0, 0, -tasks.hardDeleteAfterDays)
	logger := tasks.logger.With("task", "hard_delete", "cutoff", cutoff)
	logger.Info("hard delete starting")

	swept := map[string]bool{}
	total := int64(0)
	for _, key := range hardDeleteLeadModels {
		deleted, err := tasks.hardDeleteModel(ctx, key, cutoff)
		if err != nil {
			// Django would abort here; log and continue so one model cannot
			// block the rest of the nightly sweep.
			logger.Error("hard delete failed for a model", "model", key, "error", err)
		}
		swept[key] = true
		total += deleted
	}
	// Django then walks every remaining model that has a deleted_at column.
	for key, model := range tasks.graph.Models {
		if swept[key] || !model.SoftDeletes {
			continue
		}
		deleted, err := tasks.hardDeleteModel(ctx, key, cutoff)
		if err != nil {
			logger.Error("hard delete failed for a model", "model", key, "error", err)
		}
		total += deleted
	}
	logger.Info("hard delete completed", "rows_deleted", total)
	return nil
}

func (tasks *DeletionTasks) hardDeleteModel(ctx context.Context, modelKey string, cutoff time.Time) (int64, error) {
	model, known := tasks.graph.Models[modelKey]
	if !known {
		return 0, fmt.Errorf("unknown model %s", modelKey)
	}
	if !model.SoftDeletes {
		return 0, nil
	}
	var identifiers []string
	err := tasks.db.WithContext(ctx).Table(model.Table).
		Where("deleted_at < ?", cutoff).Pluck(model.PrimaryKey, &identifiers).Error
	if err != nil {
		return 0, fmt.Errorf("select expired %s rows: %w", modelKey, err)
	}
	if len(identifiers) == 0 {
		return 0, nil
	}
	return tasks.collectAndDelete(ctx, modelKey, identifiers, 0)
}

// collectAndDelete reproduces Django's deletion Collector: cascade into the
// related rows first, null the SET_NULL references, then delete the rows
// themselves. Unlike the soft-delete cascade this walks every related row, not
// just the live ones, because the Collector goes through the model's plain base
// manager rather than the soft-deleting one.
func (tasks *DeletionTasks) collectAndDelete(ctx context.Context, modelKey string, identifiers []string, depth int) (int64, error) {
	if depth > tasks.maxDepth {
		return 0, fmt.Errorf("hard delete cascade for %s exceeded the depth limit", modelKey)
	}
	model, known := tasks.graph.Models[modelKey]
	if !known {
		return 0, fmt.Errorf("unknown model %s", modelKey)
	}
	total := int64(0)
	for _, relation := range model.Relations {
		switch relation.OnDelete {
		case "DO_NOTHING":
			// Django's Collector leaves these rows pointing at a row that is
			// about to disappear. Both such relations in this schema are
			// nullable foreign keys that still carry a database constraint, so
			// the delete can fail at commit exactly as it does on Django.
			continue
		case "SET_NULL":
			err := tasks.db.WithContext(ctx).Table(relation.RelatedTable).
				Where(relation.RelatedColumn+" IN ?", identifiers).
				Update(relation.RelatedColumn, nil).Error
			if err != nil {
				return total, fmt.Errorf("clear %s.%s: %w", relation.RelatedTable, relation.RelatedColumn, err)
			}
		default:
			related, known := tasks.graph.Models[relation.RelatedModel]
			if !known {
				return total, fmt.Errorf("relation %s points at unknown model %s", relation.Accessor, relation.RelatedModel)
			}
			var childIdentifiers []string
			err := tasks.db.WithContext(ctx).Table(relation.RelatedTable).
				Where(relation.RelatedColumn+" IN ?", identifiers).
				Pluck(related.PrimaryKey, &childIdentifiers).Error
			if err != nil {
				return total, fmt.Errorf("collect %s rows: %w", relation.RelatedModel, err)
			}
			if len(childIdentifiers) == 0 {
				continue
			}
			deleted, err := tasks.collectAndDelete(ctx, relation.RelatedModel, childIdentifiers, depth+1)
			total += deleted
			if err != nil {
				return total, err
			}
		}
	}
	result := tasks.db.WithContext(ctx).Table(model.Table).
		Where(model.PrimaryKey+" IN ?", identifiers).Delete(nil)
	if result.Error != nil {
		return total, fmt.Errorf("delete %s rows: %w", modelKey, result.Error)
	}
	return total + result.RowsAffected, nil
}
