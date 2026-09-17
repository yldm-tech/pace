package worker

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"gorm.io/gorm"
)

// The five tasks that backfill a work item's version history. Nothing in the running application queues any of them; they are what the two management commands start.
const (
	IssueVersionTask                    = "plane.bgtasks.issue_version_sync.issue_task"
	SyncIssueVersionTask                = "plane.bgtasks.issue_version_sync.sync_issue_version"
	ScheduleIssueVersionTask            = "plane.bgtasks.issue_version_sync.schedule_issue_version"
	SyncIssueDescriptionVersionTask     = "plane.bgtasks.issue_description_version_sync.sync_issue_description_version"
	ScheduleIssueDescriptionVersionTask = "plane.bgtasks.issue_description_version_sync.schedule_issue_description_version"
)

// versionSyncDefaults are the arguments the tasks fall back to when they are queued without them.
const (
	defaultVersionBatchSize = 5000
	defaultVersionCountdown = 300
)

// projectAdminRole is ROLE.ADMIN, the role the owner falls back to when a work item names nobody.
const projectAdminRole = 20

// DelayedPublisher queues a task to run later, which is how each batch asks for the next one.
type DelayedPublisher interface {
	PublishAfter(ctx context.Context, taskName string, keywords map[string]any, delay time.Duration) error
}

// VersionSyncTasks backfill the version history.
type VersionSyncTasks struct {
	db        *gorm.DB
	publisher DelayedPublisher
	logger    *slog.Logger
	clock     func() time.Time
}

func NewVersionSyncTasks(db *gorm.DB, publisher DelayedPublisher, logger *slog.Logger) *VersionSyncTasks {
	return &VersionSyncTasks{db: db, publisher: publisher, logger: logger, clock: time.Now}
}

func (tasks *VersionSyncTasks) Register(consumer *Consumer) {
	consumer.Register(IssueVersionTask, tasks.issueVersion)
	consumer.Register(SyncIssueVersionTask, tasks.syncIssueVersion)
	consumer.Register(ScheduleIssueVersionTask, tasks.scheduleIssueVersion)
	consumer.Register(SyncIssueDescriptionVersionTask, tasks.syncIssueDescriptionVersion)
	consumer.Register(ScheduleIssueDescriptionVersionTask, tasks.scheduleIssueDescriptionVersion)
}

// scheduleIssueVersion and its twin queue the first batch and nothing else.
func (tasks *VersionSyncTasks) scheduleIssueVersion(ctx context.Context, arguments []any, keywords map[string]any) error {
	return tasks.startSync(ctx, SyncIssueVersionTask, arguments, keywords)
}

func (tasks *VersionSyncTasks) scheduleIssueDescriptionVersion(ctx context.Context, arguments []any, keywords map[string]any) error {
	return tasks.startSync(ctx, SyncIssueDescriptionVersionTask, arguments, keywords)
}

func (tasks *VersionSyncTasks) startSync(ctx context.Context, taskName string, arguments []any, keywords map[string]any) error {
	if tasks.publisher == nil {
		return nil
	}
	batchSize := intArgument(arguments, keywords, 0, "batch_size", defaultVersionBatchSize)
	countdown := intArgument(arguments, keywords, 1, "countdown", defaultVersionCountdown)
	// The first batch is queued without a delay; only the ones after it wait. The offset is left out, which is how it takes its own default of zero.
	return tasks.publisher.PublishAfter(ctx, taskName, map[string]any{
		"batch_size": batchSize, "countdown": countdown,
	}, 0)
}

// syncIssueVersion writes one batch of version rows and asks for the next.
//
// The whole batch is one transaction, and a failure anywhere in it logs and returns without asking for the next — so the backfill stops where it broke rather than skipping past it.
func (tasks *VersionSyncTasks) syncIssueVersion(ctx context.Context, arguments []any, keywords map[string]any) error {
	batchSize := intArgument(arguments, keywords, 0, "batch_size", defaultVersionBatchSize)
	offset := intArgument(arguments, keywords, 1, "offset", 0)
	countdown := intArgument(arguments, keywords, 2, "countdown", defaultVersionCountdown)

	return tasks.runBatch(ctx, SyncIssueVersionTask, batchSize, offset, countdown, func(tx *gorm.DB, issues []syncIssue) error {
		ids := make([]string, 0, len(issues))
		for _, issue := range issues {
			ids = append(ids, issue.ID)
		}
		related, err := loadVersionRelated(ctx, tx, ids)
		if err != nil {
			return err
		}
		rows := make([]map[string]any, 0, len(issues))
		for _, issue := range issues {
			owner, err := tasks.ownerOf(ctx, tx, issue)
			if err != nil {
				return err
			}
			if issue.WorkspaceID == nil || issue.ProjectID == nil {
				tasks.logger.Warn("a work item with no workspace or project is left out of the backfill", "id", issue.ID)
				continue
			}
			if owner == nil {
				tasks.logger.Warn("a work item with nobody to own its version is left out of the backfill", "id", issue.ID)
				continue
			}
			rows = append(rows, tasks.versionRow(issue, owner, related))
		}
		if len(rows) == 0 {
			return nil
		}
		return tx.Table("issue_versions").CreateInBatches(rows, 1000).Error
	})
}

// syncIssueDescriptionVersion is the same backfill over the description columns rather than the work item's own.
func (tasks *VersionSyncTasks) syncIssueDescriptionVersion(ctx context.Context, arguments []any, keywords map[string]any) error {
	batchSize := intArgument(arguments, keywords, 0, "batch_size", defaultVersionBatchSize)
	offset := intArgument(arguments, keywords, 1, "offset", 0)
	countdown := intArgument(arguments, keywords, 2, "countdown", defaultVersionCountdown)

	return tasks.runBatch(ctx, SyncIssueDescriptionVersionTask, batchSize, offset, countdown, func(tx *gorm.DB, issues []syncIssue) error {
		rows := make([]map[string]any, 0, len(issues))
		for _, issue := range issues {
			if issue.WorkspaceID == nil || issue.ProjectID == nil {
				tasks.logger.Warn("a work item with no workspace or project is left out of the backfill", "id", issue.ID)
				continue
			}
			owner, err := tasks.ownerOf(ctx, tx, issue)
			if err != nil {
				return err
			}
			if owner == nil {
				tasks.logger.Warn("a work item with nobody to own its version is left out of the backfill", "id", issue.ID)
				continue
			}
			now := tasks.clock().UTC()
			rows = append(rows, map[string]any{
				"id": uuid.NewString(), "created_at": now, "updated_at": now,
				"workspace_id": *issue.WorkspaceID, "project_id": *issue.ProjectID,
				"created_by_id": issue.CreatedByID, "updated_by_id": issue.UpdatedByID,
				"owned_by_id": *owner, "last_saved_at": now, "issue_id": issue.ID,
				"description_binary": issue.DescriptionBinary, "description_html": issue.DescriptionHTML,
				"description_stripped": issue.DescriptionStripped,
				"description_json":     jsonOrEmpty(issue.DescriptionJSON),
			})
		}
		if len(rows) == 0 {
			return nil
		}
		// The description backfill bulk-creates without a batch size, where the other one asks for a thousand at a time.
		return tx.Table("issue_description_versions").Create(rows).Error
	})
}

// syncIssue is one work item as a backfill batch reads it.
type syncIssue struct {
	ID                  string     `gorm:"column:id"`
	WorkspaceID         *string    `gorm:"column:workspace_id"`
	ProjectID           *string    `gorm:"column:project_id"`
	CreatedByID         *string    `gorm:"column:created_by_id"`
	UpdatedByID         *string    `gorm:"column:updated_by_id"`
	ParentID            *string    `gorm:"column:parent_id"`
	StateID             *string    `gorm:"column:state_id"`
	EstimatePointID     *string    `gorm:"column:estimate_point_id"`
	TypeID              *string    `gorm:"column:type_id"`
	Name                string     `gorm:"column:name"`
	Priority            string     `gorm:"column:priority"`
	StartDate           *time.Time `gorm:"column:start_date"`
	TargetDate          *time.Time `gorm:"column:target_date"`
	SequenceID          int        `gorm:"column:sequence_id"`
	SortOrder           float64    `gorm:"column:sort_order"`
	CompletedAt         *time.Time `gorm:"column:completed_at"`
	ArchivedAt          *time.Time `gorm:"column:archived_at"`
	IsDraft             bool       `gorm:"column:is_draft"`
	ExternalSource      *string    `gorm:"column:external_source"`
	ExternalID          *string    `gorm:"column:external_id"`
	DescriptionBinary   []byte     `gorm:"column:description_binary"`
	DescriptionHTML     string     `gorm:"column:description_html"`
	DescriptionStripped *string    `gorm:"column:description_stripped"`
	DescriptionJSON     []byte     `gorm:"column:description_json"`
}

// runBatch is the shape both backfills share: count, read a slice, write it, and queue the next slice when there is one.
func (tasks *VersionSyncTasks) runBatch(ctx context.Context, taskName string, batchSize, offset, countdown int, write func(*gorm.DB, []syncIssue) error) error {
	err := tasks.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var total int64
		if err := tx.Table("issues").Where("deleted_at IS NULL").Count(&total).Error; err != nil {
			return err
		}
		if total == 0 {
			return nil
		}
		end := offset + batchSize
		if end > int(total) {
			end = int(total)
		}
		var issues []syncIssue
		err := tx.Table("issues").Where("deleted_at IS NULL").Order("created_at").
			Offset(offset).Limit(end - offset).Scan(&issues).Error
		if err != nil {
			return err
		}
		if len(issues) == 0 {
			return nil
		}
		if err := write(tx, issues); err != nil {
			return err
		}
		if end < int(total) && tasks.publisher != nil {
			return tasks.publisher.PublishAfter(ctx, taskName, map[string]any{
				"batch_size": batchSize, "offset": end, "countdown": countdown,
			}, time.Duration(countdown)*time.Second)
		}
		return nil
	})
	if err != nil {
		// The task swallows its own failure, so a backfill that breaks halfway simply stops rather than raising.
		tasks.logger.Warn("a version backfill batch failed", "task", taskName, "offset", offset, "error", err)
	}
	return nil
}

// versionRelated is the four things a version row carries that do not live on the work item.
type versionRelated struct {
	cycles     map[string]string
	assignees  map[string][]string
	labels     map[string][]string
	modules    map[string][]string
	activities map[string]string
}

// loadVersionRelated reads all four for a batch. None of them filters the through table's own deleted_at, so a soft-deleted assignment is still written into the version.
func loadVersionRelated(ctx context.Context, db *gorm.DB, issueIDs []string) (versionRelated, error) {
	related := versionRelated{
		cycles: map[string]string{}, assignees: map[string][]string{},
		labels: map[string][]string{}, modules: map[string][]string{}, activities: map[string]string{},
	}
	if len(issueIDs) == 0 {
		return related, nil
	}
	type pair struct {
		IssueID string `gorm:"column:issue_id"`
		Value   string `gorm:"column:value"`
	}

	var cycles []pair
	// Only one cycle is kept per work item, which is what a dict comprehension over the rows leaves behind: the last one read wins.
	if err := db.WithContext(ctx).Table("cycle_issues").Where("issue_id IN ?", issueIDs).
		Select("issue_id, cycle_id AS value").Scan(&cycles).Error; err != nil {
		return related, err
	}
	for _, row := range cycles {
		related.cycles[row.IssueID] = row.Value
	}

	lists := []struct {
		table  string
		column string
		target map[string][]string
	}{
		{"issue_assignees", "assignee_id", related.assignees},
		{"issue_labels", "label_id", related.labels},
		{"module_issues", "module_id", related.modules},
	}
	for _, list := range lists {
		var rows []pair
		err := db.WithContext(ctx).Table(list.table).Where("issue_id IN ?", issueIDs).
			Select("issue_id, " + list.column + " AS value").Order("issue_id").Scan(&rows).Error
		if err != nil {
			return related, err
		}
		for _, row := range rows {
			list.target[row.IssueID] = append(list.target[row.IssueID], row.Value)
		}
	}

	var activities []pair
	// The newest activity per work item, which groupby picks off a query ordered by work item and then by time descending.
	err := db.WithContext(ctx).Table("issue_activities").Where("issue_id IN ?", issueIDs).
		Select("DISTINCT ON (issue_id) issue_id, id AS value").
		Order("issue_id, created_at DESC").Scan(&activities).Error
	if err != nil {
		return related, err
	}
	for _, row := range activities {
		related.activities[row.IssueID] = row.Value
	}
	return related, nil
}

// versionRow is create_issue_version: the work item's own columns plus the four lists.
func (tasks *VersionSyncTasks) versionRow(issue syncIssue, owner *string, related versionRelated) map[string]any {
	now := tasks.clock().UTC()
	var cycle any
	if value, found := related.cycles[issue.ID]; found {
		cycle = value
	}
	var activity any
	if value, found := related.activities[issue.ID]; found {
		activity = value
	}
	return map[string]any{
		"id": uuid.NewString(), "created_at": now, "updated_at": now,
		"workspace_id": *issue.WorkspaceID, "project_id": *issue.ProjectID,
		"created_by_id": issue.CreatedByID, "updated_by_id": issue.UpdatedByID,
		"owned_by_id": *owner, "last_saved_at": now, "activity_id": activity,
		// A work item has neither of these, so getattr falls back to an empty object for both.
		"properties": "{}", "meta": "{}",
		"issue_id": issue.ID, "parent": issue.ParentID, "state": issue.StateID,
		"estimate_point": issue.EstimatePointID, "name": issue.Name, "priority": issue.Priority,
		"start_date": issue.StartDate, "target_date": issue.TargetDate,
		"assignees":   pq.StringArray(orEmptyList(related.assignees[issue.ID])),
		"sequence_id": issue.SequenceID,
		"labels":      pq.StringArray(orEmptyList(related.labels[issue.ID])),
		"sort_order":  issue.SortOrder, "completed_at": issue.CompletedAt,
		"archived_at": issue.ArchivedAt, "is_draft": issue.IsDraft,
		"external_source": issue.ExternalSource, "external_id": issue.ExternalID,
		"type":    issue.TypeID,
		"cycle":   cycle,
		"modules": pq.StringArray(orEmptyList(related.modules[issue.ID])),
	}
}

func orEmptyList(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

// ownerOf is get_owner_id: whoever last touched the work item, then whoever made it, and failing both any admin of its project.
func (tasks *VersionSyncTasks) ownerOf(ctx context.Context, db *gorm.DB, issue syncIssue) (*string, error) {
	if issue.UpdatedByID != nil {
		return issue.UpdatedByID, nil
	}
	if issue.CreatedByID != nil {
		return issue.CreatedByID, nil
	}
	if issue.ProjectID == nil {
		return nil, nil
	}
	var members []string
	// .first() over a queryset with the model's own ordering, which is newest membership first.
	err := db.WithContext(ctx).Table("project_members").
		Where("project_id = ? AND role = ? AND deleted_at IS NULL", *issue.ProjectID, projectAdminRole).
		Order("created_at DESC").Limit(1).Pluck("member_id", &members).Error
	if err != nil {
		return nil, err
	}
	if len(members) == 0 {
		return nil, nil
	}
	return &members[0], nil
}

// issueVersion reproduces issue_task, which keeps one version row per person per ten minutes rather than one per edit.
//
// Nothing in this edition queues it. It is handled so a message carrying its name does not sit unconsumed, and because its second branch is worth writing down: when there is no recent version to fold the change into, it calls IssueVersion.log_issue_version, and that **always fails**. log_issue_version creates the row without a project, ProjectBaseModel.save then reads project.workspace off it, and an unsaved row with no project raises RelatedObjectDoesNotExist — which log_issue_version's own except swallows before anything reaches the database. So the first edit after a gap records nothing at all, and only an edit that lands within ten minutes of an existing version is ever written down. Reproduced rather than corrected.
func (tasks *VersionSyncTasks) issueVersion(ctx context.Context, arguments []any, keywords map[string]any) error {
	updated := stringArgument(arguments, keywords, 0, "updated_issue")
	issueID := stringArgument(arguments, keywords, 1, "issue_id")
	userID := stringArgument(arguments, keywords, 2, "user_id")

	changed := map[string]any{}
	if updated != "" {
		if err := json.Unmarshal([]byte(updated), &changed); err != nil {
			tasks.logger.Warn("a work item version arrived in a shape this does not know", "issue", issueID)
			return nil
		}
	}
	if len(changed) == 0 {
		return nil
	}

	var latest struct {
		ID          string    `gorm:"column:id"`
		OwnedByID   string    `gorm:"column:owned_by_id"`
		LastSavedAt time.Time `gorm:"column:last_saved_at"`
	}
	err := tasks.db.WithContext(ctx).Table("issue_versions").
		Where("issue_id = ? AND deleted_at IS NULL", issueID).
		Select("id, owned_by_id, last_saved_at").Order("last_saved_at DESC").Limit(1).Take(&latest).Error
	if err != nil {
		// No version to fold into means log_issue_version, which never writes anything.
		return nil
	}
	if latest.OwnedByID != userID || tasks.clock().UTC().Sub(latest.LastSavedAt.UTC()) > 600*time.Second {
		return nil
	}

	columns := map[string]any{"last_saved_at": tasks.clock().UTC()}
	for name, value := range changed {
		if !issueVersionColumns[name] {
			// save(update_fields=...) raises on a name the model does not have, which the task's own except swallows.
			tasks.logger.Warn("a work item version named a column it does not have", "column", name)
			return nil
		}
		columns[name] = value
	}
	if err := tasks.db.WithContext(ctx).Table("issue_versions").Where("id = ?", latest.ID).Updates(columns).Error; err != nil {
		tasks.logger.Warn("a work item version could not be updated", "issue", issueID, "error", err)
	}
	return nil
}

// issueVersionColumns are the columns a version row carries that an edit may name. A name outside this list is what makes save(update_fields=...) raise.
var issueVersionColumns = map[string]bool{
	"parent": true, "state": true, "estimate_point": true, "name": true, "priority": true,
	"start_date": true, "target_date": true, "assignees": true, "sequence_id": true,
	"labels": true, "sort_order": true, "completed_at": true, "archived_at": true,
	"is_draft": true, "external_source": true, "external_id": true, "type": true,
	"cycle": true, "modules": true, "properties": true, "meta": true,
}

// intArgument reads a whole number from either side of the call. A value that arrived as text is read as one too, which is what int() on the management command's input already made of it.
func intArgument(arguments []any, keywords map[string]any, index int, name string, fallback int) int {
	value, found := argument(arguments, keywords, index, name)
	if !found {
		return fallback
	}
	if number := statusCodeOf(value); number != 0 {
		return number
	}
	// A zero really is zero rather than an unreadable value, so it is told apart from one by the shape it arrived in.
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	case string:
		if typed == "0" {
			return 0
		}
	}
	return fallback
}
