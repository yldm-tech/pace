package worker

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/yldm-tech/pace/apps/api/internal/storage"
	"gorm.io/gorm"
)

// The two nightly jobs that tidy up after people rather than after the system: one takes away the export links that have gone stale, the other closes and archives the work items nobody has touched.
const (
	DeleteOldExportLinksTask = "plane.bgtasks.exporter_expired_task.delete_old_s3_link"
	ArchiveAndCloseTask      = "plane.bgtasks.issue_automation_task.archive_and_close_old_issues"
)

// exportLinkLifetimeDays is how long an export link is good for before the sweep takes it.
const exportLinkLifetimeDays = 8

// daysPerMonth is what archive_in and close_in are multiplied by. They are named in months and counted in thirty-day blocks.
const daysPerMonth = 30

// ActivityPublisher is how the automation queues the activity rows its changes should produce.
type ActivityPublisher interface {
	PublishIssueActivity(ctx context.Context, keywords map[string]any) error
}

// NightlyTasks are the two daily jobs the beat schedules.
type NightlyTasks struct {
	db         *gorm.DB
	store      *storage.Store
	activities ActivityPublisher
	logger     *slog.Logger
	clock      func() time.Time
}

func NewNightlyTasks(db *gorm.DB, store *storage.Store, activities ActivityPublisher, logger *slog.Logger) *NightlyTasks {
	return &NightlyTasks{db: db, store: store, activities: activities, logger: logger, clock: time.Now}
}

func (tasks *NightlyTasks) Register(consumer *Consumer) {
	consumer.Register(DeleteOldExportLinksTask, tasks.deleteOldExportLinks)
	consumer.Register(ArchiveAndCloseTask, tasks.archiveAndClose)
}

// deleteOldExportLinks reproduces delete_old_s3_link: the spreadsheet goes out of the bucket and the row keeps everything but its url.
//
// The row is not deleted and neither is its key — only the url is cleared. So the history still says an export was made and what it was called; it just no longer offers a way to fetch it.
func (tasks *NightlyTasks) deleteOldExportLinks(ctx context.Context, _ []any, _ map[string]any) error {
	now := tasks.clock().UTC()
	cutoff := now.AddDate(0, 0, -exportLinkLifetimeDays)
	var rows []struct {
		ID  string `gorm:"column:id"`
		Key string `gorm:"column:key"`
	}
	err := tasks.db.WithContext(ctx).Table("exporter_histories").Select("id, key").
		Where("url IS NOT NULL AND created_at <= ? AND deleted_at IS NULL", cutoff).Scan(&rows).Error
	if err != nil {
		return err
	}
	for _, row := range rows {
		if row.Key != "" && tasks.store != nil {
			if err := tasks.store.RemoveObject(ctx, row.Key); err != nil {
				// Django lets a failed delete raise out of the loop and lose the rest. Logging and carrying on is a deliberate difference: one unreachable object should not keep every other expired link alive.
				tasks.logger.Warn("expired export could not be removed from the bucket",
					"export", row.ID, "object", row.Key, "error", err)
			}
		}
		err := tasks.db.WithContext(ctx).Table("exporter_histories").
			Where("id = ?", row.ID).Update("url", nil).Error
		if err != nil {
			return err
		}
	}
	tasks.logger.Info("expired export links swept", "count", len(rows))
	return nil
}

// archiveAndClose reproduces archive_and_close_old_issues, which is two sweeps sharing one schedule.
func (tasks *NightlyTasks) archiveAndClose(ctx context.Context, _ []any, _ map[string]any) error {
	if err := tasks.archiveOldIssues(ctx); err != nil {
		// Each half has its own except upstream, so one failing does not stop the other.
		tasks.logger.Error("archiving old work items failed", "error", err)
	}
	if err := tasks.closeOldIssues(ctx); err != nil {
		tasks.logger.Error("closing old work items failed", "error", err)
	}
	return nil
}

// archiveOldIssues archives what has been finished and left alone for as long as its project asks.
//
// A work item is only eligible when nothing it belongs to is still running: a cycle it is in must have ended, a module it is in must be past its target date, and it must not be sitting in intake waiting for a decision.
func (tasks *NightlyTasks) archiveOldIssues(ctx context.Context) error {
	now := tasks.clock().UTC()
	var projects []struct {
		ID          string  `gorm:"column:id"`
		ArchiveIn   int     `gorm:"column:archive_in"`
		CreatedByID *string `gorm:"column:created_by_id"`
	}
	err := tasks.db.WithContext(ctx).Table("projects").Select("id, archive_in, created_by_id").
		Where("archive_in > 0 AND deleted_at IS NULL").Scan(&projects).Error
	if err != nil {
		return err
	}
	archiveAt := now.Format("2006-01-02")

	for _, project := range projects {
		cutoff := now.AddDate(0, 0, -project.ArchiveIn*daysPerMonth)
		identifiers, err := tasks.staleWorkItems(ctx, project.ID, cutoff, now,
			[]string{"completed", "cancelled"})
		if err != nil {
			return err
		}
		if len(identifiers) == 0 {
			continue
		}
		err = tasks.db.WithContext(ctx).Table("issues").Where("id IN ?", uniqueIdentifiers(identifiers)).
			Update("archived_at", archiveAt).Error
		if err != nil {
			return err
		}
		requested, err := json.Marshal(map[string]any{"archived_at": archiveAt, "automation": true})
		if err != nil {
			return err
		}
		current, err := json.Marshal(map[string]any{"archived_at": nil})
		if err != nil {
			return err
		}
		for _, identifier := range identifiers {
			// The activity is attributed to whoever made the project, since nobody asked for this.
			err := tasks.publishAutomationActivity(ctx, project.CreatedByID, identifier, project.ID,
				string(requested), stringPointer(string(current)), now)
			if err != nil {
				return err
			}
		}
		tasks.logger.Info("old work items archived", "project", project.ID, "count", len(identifiers))
	}
	return nil
}

// closeOldIssues moves what is still open and untouched into the project's default state.
//
// A project with no default state takes **whatever cancelled state comes first anywhere in the installation** — the lookup is not scoped to the project or even to the workspace. Reproduced rather than corrected: correcting it would move work items into a state this port chose, which is a different outcome rather than a fixed one.
func (tasks *NightlyTasks) closeOldIssues(ctx context.Context) error {
	now := tasks.clock().UTC()
	var projects []struct {
		ID             string  `gorm:"column:id"`
		CloseIn        int     `gorm:"column:close_in"`
		DefaultStateID *string `gorm:"column:default_state_id"`
		CreatedByID    *string `gorm:"column:created_by_id"`
	}
	err := tasks.db.WithContext(ctx).Table("projects").
		Select("id, close_in, default_state_id, created_by_id").
		Where("close_in > 0 AND deleted_at IS NULL").Scan(&projects).Error
	if err != nil {
		return err
	}

	for _, project := range projects {
		cutoff := now.AddDate(0, 0, -project.CloseIn*daysPerMonth)
		identifiers, err := tasks.staleWorkItems(ctx, project.ID, cutoff, now,
			[]string{"backlog", "unstarted", "started"})
		if err != nil {
			return err
		}
		if len(identifiers) == 0 {
			continue
		}
		closeState := project.DefaultStateID
		if closeState == nil {
			var anyCancelled []string
			err := tasks.db.WithContext(ctx).Table("states").
				Where(`"group" = ? AND deleted_at IS NULL`, "cancelled").
				Order("created_at DESC").Limit(1).Pluck("id", &anyCancelled).Error
			if err != nil {
				return err
			}
			if len(anyCancelled) == 0 {
				// Nothing to move them to, and Django would write a null state.
				tasks.logger.Warn("no cancelled state exists, work items left open", "project", project.ID)
				continue
			}
			closeState = &anyCancelled[0]
		}
		err = tasks.db.WithContext(ctx).Table("issues").Where("id IN ?", uniqueIdentifiers(identifiers)).
			Update("state_id", *closeState).Error
		if err != nil {
			return err
		}
		requested, err := json.Marshal(map[string]any{"closed_to": *closeState})
		if err != nil {
			return err
		}
		for _, identifier := range identifiers {
			err := tasks.publishAutomationActivity(ctx, project.CreatedByID, identifier, project.ID,
				string(requested), nil, now)
			if err != nil {
				return err
			}
		}
		tasks.logger.Info("old work items closed", "project", project.ID, "count", len(identifiers), "state", *closeState)
	}
	return nil
}

// staleWorkItems is the eligibility rule both sweeps share, which is the same rule written against two different sets of state groups.
//
// Each of the three conditions is "it has none of these, or it has one that has finished" rather than "all of the ones it has have finished". That distinction only shows on a work item in more than one module: Django joins the links and matches the row, so one module past its target date is enough even while another is still running.
//
// It also answers with **one entry per matching link** rather than one per work item, because the queryset is not made distinct. A work item in three finished modules is archived once and gets three activity rows. Reproduced rather than corrected: the rows are what a reader of the history would see today.
func (tasks *NightlyTasks) staleWorkItems(ctx context.Context, projectID string, cutoff, now time.Time, groups []string) ([]string, error) {
	identifiers := []string{}
	err := tasks.db.WithContext(ctx).Table("issues i").
		Joins(`LEFT JOIN states s ON s.id = i.state_id`).
		Joins("JOIN projects p ON p.id = i.project_id").
		Joins("LEFT JOIN module_issues am ON am.issue_id = i.id AND am.deleted_at IS NULL").
		Joins("LEFT JOIN modules amo ON amo.id = am.module_id").
		Where(`i.deleted_at IS NULL AND s."group" IS DISTINCT FROM 'triage' AND i.archived_at IS NULL
			AND p.archived_at IS NULL AND i.is_draft = FALSE
			AND i.project_id = ? AND i.updated_at <= ? AND s."group" IN ?`,
			projectID, cutoff, groups).
		// A cycle it is in has to have ended, or it is in none.
		Where(`(NOT EXISTS (SELECT 1 FROM cycle_issues ac WHERE ac.issue_id = i.id AND ac.deleted_at IS NULL)
			OR EXISTS (SELECT 1 FROM cycle_issues ac JOIN cycles acy ON acy.id = ac.cycle_id
				WHERE ac.issue_id = i.id AND ac.deleted_at IS NULL AND acy.end_date < ?))`, now).
		// A module it is in has to be past its target date, or it is in none. This one is the join rather than a subquery, because it is what decides how many entries a work item contributes.
		Where("(am.id IS NULL OR amo.target_date < ?)", now).
		// And it must not be waiting in intake: accepted, declined and duplicate are decided, and having no intake row at all counts as decided too.
		Where(`(NOT EXISTS (SELECT 1 FROM intake_issues ai WHERE ai.issue_id = i.id AND ai.deleted_at IS NULL)
			OR EXISTS (SELECT 1 FROM intake_issues ai WHERE ai.issue_id = i.id AND ai.deleted_at IS NULL
				AND ai.status IN (1, -1, 2)))`).
		Pluck("i.id", &identifiers).Error
	return identifiers, err
}

// uniqueIdentifiers is what the bulk update needs, since writing the same row twice is pointless even where the activity rows are not deduplicated.
func uniqueIdentifiers(values []string) []string {
	seen := map[string]bool{}
	unique := make([]string, 0, len(values))
	for _, value := range values {
		if seen[value] {
			continue
		}
		seen[value] = true
		unique = append(unique, value)
	}
	return unique
}

// publishAutomationActivity queues the activity row a sweep's change should produce, which still runs on the Python worker.
func (tasks *NightlyTasks) publishAutomationActivity(ctx context.Context, actorID *string, issueID, projectID, requested string, current *string, now time.Time) error {
	if tasks.activities == nil {
		return nil
	}
	keywords := map[string]any{
		"type": "issue.activity.updated", "requested_data": requested,
		"current_instance": nullableString(current),
		"issue_id":         issueID, "actor_id": nullableString(actorID),
		"project_id": projectID, "subscriber": false,
		"epoch": now.Unix(), "notification": true,
	}
	return tasks.activities.PublishIssueActivity(ctx, keywords)
}

func nullableString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func stringPointer(value string) *string { return &value }
