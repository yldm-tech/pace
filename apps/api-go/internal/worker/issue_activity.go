package worker

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sort"
	"time"

	redis "github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// IssueActivityTask is what writes a work item's history. Every change to a work item goes through it, which is why nothing else in the worker is queued as often.
const IssueActivityTask = "plane.bgtasks.issue_activities_task.issue_activity"

// originTTL is how long the request's origin is kept beside the work item, which the notification task reads to build the links in its emails.
const originTTL = 600 * time.Second

// NotificationPublisher hands the written activities on to the task that turns them into notifications, which still runs on the Python worker.
type NotificationPublisher interface {
	PublishNotifications(ctx context.Context, keywords map[string]any) error
}

// IssueActivityTasks writes the rows behind a work item's history.
type IssueActivityTasks struct {
	db            *gorm.DB
	redis         redis.UniversalClient
	notifications NotificationPublisher
	logger        *slog.Logger
	clock         func() time.Time
}

func NewIssueActivityTasks(db *gorm.DB, client redis.UniversalClient, notifications NotificationPublisher, logger *slog.Logger) *IssueActivityTasks {
	return &IssueActivityTasks{db: db, redis: client, notifications: notifications, logger: logger, clock: time.Now}
}

func (tasks *IssueActivityTasks) Register(consumer *Consumer) {
	consumer.Register(IssueActivityTask, tasks.issueActivity)
}

// activityRow is one line of a work item's history, in the shape the table wants it.
type activityRow struct {
	ID            string
	IssueID       *string
	ActorID       *string
	Verb          string
	Field         *string
	OldValue      *string
	NewValue      *string
	Comment       string
	OldIdentifier *string
	NewIdentifier *string
	// IssueCommentID is set only by the three comment activities, which is what lets a comment's history be read on its own.
	IssueCommentID *string
	ProjectID      string
	WorkspaceID    string
	Epoch          *float64
	CreatedAt      time.Time
}

// activityContext is what every tracker needs and none of them changes.
type activityContext struct {
	issueID     string
	projectID   string
	workspaceID string
	actorID     string
	epoch       *float64
	now         time.Time
}

// errActivityAbandoned marks the failures Django lets reach the task's own except, which throws away **every** activity the request would have written rather than the one that failed. Each place it is raised says why.
var errActivityAbandoned = errors.New("issue activity: the batch was abandoned")

// issueActivity reproduces the task's entry point.
//
// Three things happen before any history is written. A project id that is not a uuid ends the task silently. The origin is parked in Redis beside the work item for ten minutes, which is what lets the notification emails build their links. And the work item's updated_at is touched, so a change to something hanging off it still counts as touching it.
func (tasks *IssueActivityTasks) issueActivity(ctx context.Context, arguments []any, keywords map[string]any) error {
	activityType := stringArgument(arguments, keywords, 0, "type")
	requestedData := stringArgument(arguments, keywords, 1, "requested_data")
	currentInstance := stringArgument(arguments, keywords, 2, "current_instance")
	issueID := stringArgument(arguments, keywords, 3, "issue_id")
	actorID := stringArgument(arguments, keywords, 4, "actor_id")
	projectID := stringArgument(arguments, keywords, 5, "project_id")
	epoch := floatArgument(arguments, keywords, 6, "epoch")
	subscriber := boolArgumentWithDefault(arguments, keywords, 7, "subscriber", true)
	notify := boolArgument(arguments, keywords, 8, "notification")
	origin := stringArgument(arguments, keywords, 9, "origin")

	if !looksLikeUUID(projectID) {
		return nil
	}
	var project struct {
		WorkspaceID string `gorm:"column:workspace_id"`
	}
	err := tasks.db.WithContext(ctx).Table("projects").Select("workspace_id").
		Where("id = ?", projectID).Take(&project).Error
	if err != nil {
		// Project.DoesNotExist reaches the task's own except and the whole thing returns.
		return nil
	}

	now := tasks.clock().UTC()
	if issueID != "" {
		if origin != "" && tasks.redis != nil {
			if err := tasks.redis.Set(ctx, issueID, origin, originTTL).Err(); err != nil {
				tasks.logger.Warn("the request origin could not be parked", "issue", issueID, "error", err)
			}
		}
		// Touching the work item is wrapped in its own try upstream, so a failure here does not stop the history being written.
		err := tasks.touchIssue(ctx, issueID, now)
		if err != nil {
			tasks.logger.Warn("the work item could not be touched", "issue", issueID, "error", err)
		}
	}

	context := activityContext{
		issueID: issueID, projectID: projectID, workspaceID: project.WorkspaceID,
		actorID: actorID, epoch: epoch, now: now,
	}
	rows, err := tasks.dispatch(ctx, activityType, requestedData, currentInstance, context)
	if errors.Is(err, errActivityAbandoned) {
		tasks.logger.Warn("issue activity abandoned", "type", activityType, "issue", issueID, "error", err)
		return nil
	}
	if err != nil {
		return err
	}
	if len(rows) > 0 {
		if err := tasks.writeActivities(ctx, rows); err != nil {
			return err
		}
	}
	if !notify {
		return nil
	}
	return tasks.publishNotifications(ctx, activityType, issueID, actorID, projectID,
		subscriber, rows, requestedData, currentInstance)
}

// dispatch picks the handler for the activity type. A type with no handler writes nothing, which is what an unknown key in the mapper does.
func (tasks *IssueActivityTasks) dispatch(ctx context.Context, activityType, requestedData, currentInstance string, context activityContext) ([]activityRow, error) {
	switch activityType {
	case "issue.activity.created":
		return tasks.createIssueActivity(ctx, requestedData, currentInstance, context)
	case "issue.activity.updated":
		return tasks.updateIssueActivity(ctx, requestedData, currentInstance, context)
	case "issue.activity.deleted":
		return []activityRow{tasks.row(context, activityRow{
			Verb: "deleted", Field: text("issue"), Comment: "deleted the issue",
		})}, nil
	}
	return tasks.dispatchRelated(ctx, activityType, requestedData, currentInstance, context)
}

// createIssueActivity writes the one row that says a work item exists, and then the assignees it was raised with.
//
// The row is written on its own rather than with the batch, and then rewritten: its timestamp becomes the work item's own and its actor becomes whoever raised it. So the first line of a history is always attributed to the creator and dated to the creation, whoever queued the task and whenever it ran.
func (tasks *IssueActivityTasks) createIssueActivity(ctx context.Context, requestedData, currentInstance string, context activityContext) ([]activityRow, error) {
	var issue struct {
		CreatedAt   time.Time `gorm:"column:created_at"`
		CreatedByID *string   `gorm:"column:created_by_id"`
	}
	err := tasks.db.WithContext(ctx).Table("issues").Select("created_at, created_by_id").
		Where("id = ?", context.issueID).Take(&issue).Error
	if err != nil {
		// Issue.objects.get raises, which abandons the batch.
		return nil, errActivityAbandoned
	}
	created := tasks.row(context, activityRow{Verb: "created", Comment: "created the issue"})
	created.CreatedAt = issue.CreatedAt
	created.ActorID = issue.CreatedByID
	if err := tasks.writeActivities(ctx, []activityRow{created}); err != nil {
		return nil, err
	}

	requested, requestedPresent := decodeActivitySnapshot(anyString(requestedData))
	if !requestedPresent {
		// Django reads a key off None here, which raises and abandons the rest.
		return nil, errActivityAbandoned
	}
	if _, named := requested["assignee_ids"]; !named {
		return nil, nil
	}
	current, _ := decodeActivitySnapshot(anyString(currentInstance))
	return tasks.trackAssignees(ctx, requested, current, context)
}

// issueTrackers maps a field the request named onto what it means for the history. Four of them are named twice, because the external API sends the relation's own name where the session API sends the column's.
var issueTrackers = map[string]string{
	"name": "name", "parent_id": "parent", "priority": "priority", "state_id": "state",
	"description_html": "description", "target_date": "target_date", "start_date": "start_date",
	"label_ids": "labels", "assignee_ids": "assignees", "estimate_point": "estimate",
	"archived_at": "archived_at", "closed_to": "closed_to",
	"parent": "parent", "state": "state", "assignees": "assignees", "labels": "labels",
}

// updateIssueActivity walks the fields the request named and asks each one what changed.
//
// A payload that names both `state_id` and `state` is walked twice and writes the change twice, because both keys reach the same tracker. Reproduced rather than corrected.
func (tasks *IssueActivityTasks) updateIssueActivity(ctx context.Context, requestedData, currentInstance string, context activityContext) ([]activityRow, error) {
	requested, _ := decodeActivitySnapshot(anyString(requestedData))
	current, _ := decodeActivitySnapshot(anyString(currentInstance))

	keys := make([]string, 0, len(requested))
	for key := range requested {
		if _, tracked := issueTrackers[key]; tracked {
			keys = append(keys, key)
		}
	}
	// Python walks the request's own key order; a map has none, so the order is fixed here instead.
	sort.Strings(keys)

	rows := []activityRow{}
	for _, key := range keys {
		var produced []activityRow
		var err error
		switch issueTrackers[key] {
		case "name":
			produced = tasks.trackPlain(requested, current, context, "name", "name", "updated the name to")
		case "priority":
			produced = tasks.trackPlain(requested, current, context, "priority", "priority", "updated the priority to")
		case "target_date":
			produced = tasks.trackDate(requested, current, context, "target_date", "updated the target date to")
		case "start_date":
			// The trailing space is upstream's and the web client renders it, so it stays.
			produced = tasks.trackDate(requested, current, context, "start_date", "updated the start date to ")
		case "description":
			produced, err = tasks.trackDescription(ctx, requested, current, context)
		case "state":
			produced, err = tasks.trackState(ctx, requested, current, context)
		case "parent":
			produced, err = tasks.trackParent(ctx, requested, current, context)
		case "labels":
			produced, err = tasks.trackLabels(ctx, requested, current, context)
		case "assignees":
			produced, err = tasks.trackAssignees(ctx, requested, current, context)
		case "estimate":
			produced, err = tasks.trackEstimate(ctx, requested, current, context)
		case "archived_at":
			produced = tasks.trackArchivedAt(requested, current, context)
		case "closed_to":
			produced, err = tasks.trackClosedTo(ctx, requested, context)
		}
		if err != nil {
			return nil, err
		}
		rows = append(rows, produced...)
	}
	return rows, nil
}

// trackPlain is the shape four of the trackers share: compare one key and report both sides as they were written.
func (tasks *IssueActivityTasks) trackPlain(requested, current map[string]any, context activityContext, key, field, comment string) []activityRow {
	was, now := current[key], requested[key]
	if equalActivityValues(was, now) {
		return nil
	}
	return []activityRow{tasks.row(context, activityRow{
		Verb: "updated", Field: text(field), Comment: comment,
		OldValue: activityText(was), NewValue: activityText(now),
	})}
}

// trackDate is the same comparison with one difference: a date that was cleared is reported as the empty string rather than as nothing, which is what the history renders as "removed".
func (tasks *IssueActivityTasks) trackDate(requested, current map[string]any, context activityContext, key, comment string) []activityRow {
	was, now := current[key], requested[key]
	if equalActivityValues(was, now) {
		return nil
	}
	return []activityRow{tasks.row(context, activityRow{
		Verb: "updated", Field: text(key), Comment: comment,
		OldValue: text(activityTextOrEmpty(was)), NewValue: text(activityTextOrEmpty(now)),
	})}
}

// trackDescription is the one tracker that can write nothing and still have done something.
//
// When the line before it was also a description change by the same person, that line's timestamp is moved to now instead of a new one being written. So a run of edits collapses into a single entry that keeps moving, which is what stops a history being nothing but the same sentence over and over.
func (tasks *IssueActivityTasks) trackDescription(ctx context.Context, requested, current map[string]any, context activityContext) ([]activityRow, error) {
	was, now := current["description_html"], requested["description_html"]
	if equalActivityValues(was, now) {
		return nil, nil
	}
	var last struct {
		ID      string  `gorm:"column:id"`
		Field   *string `gorm:"column:field"`
		ActorID *string `gorm:"column:actor_id"`
	}
	err := tasks.db.WithContext(ctx).Table("issue_activities").Select("id, field, actor_id").
		Where("issue_id = ? AND deleted_at IS NULL", context.issueID).
		Order("created_at DESC").Limit(1).Take(&last).Error
	if err == nil && last.Field != nil && *last.Field == "description" &&
		last.ActorID != nil && *last.ActorID == context.actorID {
		return nil, tasks.db.WithContext(ctx).Table("issue_activities").
			Where("id = ?", last.ID).Update("created_at", context.now).Error
	}
	return []activityRow{tasks.row(context, activityRow{
		Verb: "updated", Field: text("description"), Comment: "updated the description to",
		OldValue: activityText(was), NewValue: activityText(now),
	})}, nil
}

// trackState reports the two states by name, and a state id that is not a uuid is read as no state at all rather than refused.
func (tasks *IssueActivityTasks) trackState(ctx context.Context, requested, current map[string]any, context activityContext) ([]activityRow, error) {
	wasID := uuidOrNothing(firstPresent(current, "state_id", "state"))
	nowID := uuidOrNothing(firstPresent(requested, "state_id", "state"))
	if wasID == nowID {
		return nil, nil
	}
	was, err := tasks.stateNamed(ctx, wasID, context.projectID)
	if err != nil {
		return nil, err
	}
	now, err := tasks.stateNamed(ctx, nowID, context.projectID)
	if err != nil {
		return nil, err
	}
	return []activityRow{tasks.row(context, activityRow{
		Verb: "updated", Field: text("state"), Comment: "updated the state to",
		OldValue: was.name, NewValue: now.name,
		OldIdentifier: was.id, NewIdentifier: now.id,
	})}, nil
}

// trackParent reports the two parents by their human-facing key, and unlike the state it **abandons the field entirely** when either id is not a uuid rather than reading it as nothing.
func (tasks *IssueActivityTasks) trackParent(ctx context.Context, requested, current map[string]any, context activityContext) ([]activityRow, error) {
	wasRaw := firstPresent(current, "parent_id", "parent")
	nowRaw := firstPresent(requested, "parent_id", "parent")
	if (wasRaw != "" && !looksLikeUUID(wasRaw)) || (nowRaw != "" && !looksLikeUUID(nowRaw)) {
		return nil, nil
	}
	if wasRaw == nowRaw {
		return nil, nil
	}
	was, err := tasks.issueKey(ctx, wasRaw)
	if err != nil {
		return nil, err
	}
	now, err := tasks.issueKey(ctx, nowRaw)
	if err != nil {
		return nil, err
	}
	return []activityRow{tasks.row(context, activityRow{
		Verb: "updated", Field: text("parent"), Comment: "updated the parent issue to",
		OldValue: text(was.key), NewValue: text(now.key),
		OldIdentifier: was.id, NewIdentifier: now.id,
	})}, nil
}

// trackLabels writes one line per label added and one per label removed.
//
// A label the work item still points at but that has since been deleted **abandons the whole batch**: Django reads it with a get, which raises. Reproduced rather than corrected.
func (tasks *IssueActivityTasks) trackLabels(ctx context.Context, requested, current map[string]any, context activityContext) ([]activityRow, error) {
	added, dropped := activityIDDifference(requested, current, "label_ids", "labels")
	rows := []activityRow{}
	for _, identifier := range added {
		name, found, err := tasks.namedRow(ctx, "labels", "name", identifier)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, errActivityAbandoned
		}
		rows = append(rows, tasks.row(context, activityRow{
			Verb: "updated", Field: text("labels"), Comment: "added label ",
			OldValue: text(""), NewValue: text(name), NewIdentifier: text(identifier),
		}))
	}
	for _, identifier := range dropped {
		name, found, err := tasks.namedRow(ctx, "labels", "name", identifier)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, errActivityAbandoned
		}
		rows = append(rows, tasks.row(context, activityRow{
			Verb: "updated", Field: text("labels"), Comment: "removed label ",
			OldValue: text(name), NewValue: text(""), OldIdentifier: text(identifier),
		}))
	}
	return rows, nil
}

// trackAssignees writes one line per person added and one per person removed, and quietly signs up everybody added to hear about the work item.
func (tasks *IssueActivityTasks) trackAssignees(ctx context.Context, requested, current map[string]any, context activityContext) ([]activityRow, error) {
	added, dropped := activityIDDifference(requested, current, "assignee_ids", "assignees")
	rows := []activityRow{}
	subscribers := []map[string]any{}
	for _, identifier := range added {
		name, found, err := tasks.namedRow(ctx, "users", "display_name", identifier)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, errActivityAbandoned
		}
		rows = append(rows, tasks.row(context, activityRow{
			Verb: "updated", Field: text("assignees"), Comment: "added assignee ",
			OldValue: text(""), NewValue: text(name), NewIdentifier: text(identifier),
		}))
		rowID, err := newTaskUUID()
		if err != nil {
			return nil, err
		}
		// The subscription is attributed to the person themselves rather than to whoever assigned them, which is what the bulk create passes and what bypassing save leaves in place.
		subscribers = append(subscribers, map[string]any{
			"id": rowID, "created_at": context.now, "updated_at": context.now,
			"created_by_id": identifier, "updated_by_id": identifier,
			"subscriber_id": identifier, "issue_id": context.issueID,
			"workspace_id": context.workspaceID, "project_id": context.projectID,
		})
	}
	if len(subscribers) > 0 {
		err := tasks.db.WithContext(ctx).Table("issue_subscribers").
			Clauses(clause.OnConflict{DoNothing: true}).Create(subscribers).Error
		if err != nil {
			return nil, err
		}
	}
	for _, identifier := range dropped {
		name, found, err := tasks.namedRow(ctx, "users", "display_name", identifier)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, errActivityAbandoned
		}
		rows = append(rows, tasks.row(context, activityRow{
			Verb: "updated", Field: text("assignees"), Comment: "removed assignee ",
			OldValue: text(name), NewValue: text(""), OldIdentifier: text(identifier),
		}))
	}
	return rows, nil
}

// trackEstimate reports the estimate by its value, and names the field after the kind of estimate the project keeps.
//
// **Clearing an estimate abandons the whole batch.** The field name is built from the new estimate's own type, and with nothing to move to there is no new estimate to ask — Django raises there and the request writes no history at all, not even for the fields that did change. Reproduced rather than corrected.
func (tasks *IssueActivityTasks) trackEstimate(ctx context.Context, requested, current map[string]any, context activityContext) ([]activityRow, error) {
	was, now := current["estimate_point"], requested["estimate_point"]
	if equalActivityValues(was, now) {
		return nil, nil
	}
	wasID, nowID := activityTextOrEmpty(was), activityTextOrEmpty(now)
	if nowID == "" {
		return nil, errActivityAbandoned
	}
	wasValue, _, err := tasks.estimateValue(ctx, wasID)
	if err != nil {
		return nil, err
	}
	nowValue, nowType, err := tasks.estimateValue(ctx, nowID)
	if err != nil {
		return nil, err
	}
	if nowType == "" {
		// The new estimate does not exist, and Django reads its type anyway.
		return nil, errActivityAbandoned
	}
	return []activityRow{tasks.row(context, activityRow{
		Verb: "updated", Field: text("estimate_" + nowType), Comment: "updated the estimate point to ",
		OldValue: wasValue, NewValue: nowValue,
		OldIdentifier: textOrNil(wasID), NewIdentifier: textOrNil(nowID),
	})}, nil
}

// trackArchivedAt says whether the work item was put away or brought back, and who put it away — a sweep and a person leave different words behind.
func (tasks *IssueActivityTasks) trackArchivedAt(requested, current map[string]any, context activityContext) []activityRow {
	was, now := current["archived_at"], requested["archived_at"]
	if equalActivityValues(was, now) {
		return nil
	}
	if now == nil {
		return []activityRow{tasks.row(context, activityRow{
			Verb: "updated", Field: text("archived_at"), Comment: "has restored the issue",
			OldValue: text("archive"), NewValue: text("restore"),
		})}
	}
	comment, newValue := "Actor has archived the issue", "manual_archive"
	if truthy(requested["automation"]) {
		comment, newValue = "Plane has archived the issue", "archive"
	}
	return []activityRow{tasks.row(context, activityRow{
		Verb: "updated", Field: text("archived_at"), Comment: comment, NewValue: text(newValue),
	})}
}

// trackClosedTo is what the nightly sweep leaves behind when it closes a work item nobody has touched.
func (tasks *IssueActivityTasks) trackClosedTo(ctx context.Context, requested map[string]any, context activityContext) ([]activityRow, error) {
	target := activityTextOrEmpty(requested["closed_to"])
	if target == "" {
		return nil, nil
	}
	state, err := tasks.stateNamed(ctx, target, context.projectID)
	if err != nil {
		return nil, err
	}
	if state.id == nil {
		// Django reads this one with a get, which raises when the state is not the project's.
		return nil, errActivityAbandoned
	}
	return []activityRow{tasks.row(context, activityRow{
		Verb: "updated", Field: text("state"), Comment: "Plane updated the state to ",
		NewValue: state.name, NewIdentifier: state.id,
	})}, nil
}

// row fills in everything a line of history carries whatever it is about.
func (tasks *IssueActivityTasks) row(context activityContext, row activityRow) activityRow {
	identifier, err := newTaskUUID()
	if err != nil {
		// newTaskUUID only fails when the system has no randomness, which is not a condition this can carry on from.
		panic(err)
	}
	row.ID = identifier
	row.ProjectID = context.projectID
	row.WorkspaceID = context.workspaceID
	row.Epoch = context.epoch
	row.CreatedAt = context.now
	if context.issueID != "" {
		row.IssueID = text(context.issueID)
	}
	if row.ActorID == nil && context.actorID != "" {
		row.ActorID = text(context.actorID)
	}
	return row
}

// writeActivities puts the lines in the table. The two audit columns are left empty because the worker has no current user, which is what BaseModel.save does to any row it writes here.
func (tasks *IssueActivityTasks) writeActivities(ctx context.Context, rows []activityRow) error {
	values := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		values = append(values, map[string]any{
			"id": row.ID, "created_at": row.CreatedAt, "updated_at": row.CreatedAt,
			"created_by_id": nil, "updated_by_id": nil,
			"issue_id": row.IssueID, "actor_id": row.ActorID,
			"verb": row.Verb, "field": row.Field,
			"old_value": row.OldValue, "new_value": row.NewValue,
			"comment": row.Comment, "attachments": "{}",
			"old_identifier": row.OldIdentifier, "new_identifier": row.NewIdentifier,
			"issue_comment_id": row.IssueCommentID,
			"project_id":       row.ProjectID, "workspace_id": row.WorkspaceID,
			"epoch": row.Epoch,
		})
	}
	return tasks.db.WithContext(ctx).Table("issue_activities").Create(values).Error
}

// publishNotifications hands the written lines to the task that turns them into notifications, which still runs on the Python worker.
func (tasks *IssueActivityTasks) publishNotifications(ctx context.Context, activityType, issueID, actorID, projectID string, subscriber bool, rows []activityRow, requestedData, currentInstance string) error {
	if tasks.notifications == nil {
		return nil
	}
	serialized := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		serialized = append(serialized, map[string]any{
			"id": row.ID, "created_at": row.CreatedAt, "updated_at": row.CreatedAt,
			"deleted_at": nil, "verb": row.Verb, "field": nullableString(row.Field),
			"old_value": nullableString(row.OldValue), "new_value": nullableString(row.NewValue),
			"comment": row.Comment, "attachments": []string{},
			"old_identifier": nullableString(row.OldIdentifier),
			"new_identifier": nullableString(row.NewIdentifier),
			"epoch":          row.Epoch, "created_by": nil, "updated_by": nil,
			"project": row.ProjectID, "workspace": row.WorkspaceID,
			"issue": nullableString(row.IssueID), "issue_comment": nullableString(row.IssueCommentID),
			"actor": nullableString(row.ActorID),
		})
	}
	encoded, err := json.Marshal(serialized)
	if err != nil {
		return err
	}
	return tasks.notifications.PublishNotifications(ctx, map[string]any{
		"type": activityType, "issue_id": nullableID(issueID), "actor_id": actorID,
		"project_id": projectID, "subscriber": subscriber,
		"issue_activities_created": string(encoded),
		"requested_data":           anyString(requestedData), "current_instance": anyString(currentInstance),
	})
}
