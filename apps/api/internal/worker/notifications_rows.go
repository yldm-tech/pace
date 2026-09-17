package worker

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"gorm.io/gorm/clause"
)

// notificationIssue is what a notification says about the work item it is about.
type notificationIssue struct {
	ID          string  `gorm:"column:id"`
	Name        string  `gorm:"column:name"`
	SequenceID  int     `gorm:"column:sequence_id"`
	CreatedByID *string `gorm:"column:created_by_id"`
	StateName   string  `gorm:"column:state_name"`
	StateGroup  string  `gorm:"column:state_group"`
}

// notificationProject is what it says about the project the work item is in.
type notificationProject struct {
	ID            string `gorm:"column:id"`
	Identifier    string `gorm:"column:identifier"`
	WorkspaceID   string `gorm:"column:workspace_id"`
	WorkspaceSlug string `gorm:"column:workspace_slug"`
}

// lastActivityRow is the line of history the mention branch reads back out of the table.
type lastActivityRow struct {
	ID        string    `gorm:"column:id"`
	Verb      string    `gorm:"column:verb"`
	Field     *string   `gorm:"column:field"`
	ActorID   *string   `gorm:"column:actor_id"`
	OldValue  *string   `gorm:"column:old_value"`
	NewValue  *string   `gorm:"column:new_value"`
	CreatedAt time.Time `gorm:"column:created_at"`
}

// notificationPreferenceRow is the five switches that decide whether an email goes out.
type notificationPreferenceRow struct {
	PropertyChange bool `gorm:"column:property_change"`
	StateChange    bool `gorm:"column:state_change"`
	Comment        bool `gorm:"column:comment"`
	Mention        bool `gorm:"column:mention"`
	IssueCompleted bool `gorm:"column:issue_completed"`
}

// activeProjectMembers is who is in the project, which is what every mention and every subscriber is narrowed to.
func (tasks *NotificationTasks) activeProjectMembers(ctx context.Context, projectID string) (map[string]bool, error) {
	identifiers := []string{}
	err := tasks.db.WithContext(ctx).Table("project_members").
		Where("project_id = ? AND is_active = TRUE AND deleted_at IS NULL", projectID).
		Pluck("member_id", &identifiers).Error
	if err != nil {
		return nil, err
	}
	members := make(map[string]bool, len(identifiers))
	for _, identifier := range identifiers {
		members[identifier] = true
	}
	return members, nil
}

// notificationIssue reads the work item, with the state's name and group beside it — a work item with no state reads as one with empty ones, which is where Django would have raised.
func (tasks *NotificationTasks) notificationIssue(ctx context.Context, issueID string) (notificationIssue, bool, error) {
	var rows []notificationIssue
	err := tasks.db.WithContext(ctx).Table("issues i").
		Select(`i.id, i.name, i.sequence_id, i.created_by_id,
			COALESCE(s.name, '') AS state_name, COALESCE(s."group", '') AS state_group`).
		Joins("LEFT JOIN states s ON s.id = i.state_id").
		Where("i.id = ? AND i.deleted_at IS NULL", issueID).Limit(1).Scan(&rows).Error
	if err != nil || len(rows) == 0 {
		return notificationIssue{}, false, err
	}
	return rows[0], true, nil
}

func (tasks *NotificationTasks) notificationProject(ctx context.Context, projectID string) (notificationProject, bool, error) {
	var rows []notificationProject
	err := tasks.db.WithContext(ctx).Table("projects p").
		Select("p.id, p.identifier, p.workspace_id, w.slug AS workspace_slug").
		Joins("JOIN workspaces w ON w.id = p.workspace_id").
		Where("p.id = ? AND p.deleted_at IS NULL", projectID).Limit(1).Scan(&rows).Error
	if err != nil || len(rows) == 0 {
		return notificationProject{}, false, err
	}
	return rows[0], true, nil
}

// issueSubscribers is who is following the work item, minus whoever was just mentioned and the person who made the change — they hear about it another way.
func (tasks *NotificationTasks) issueSubscribers(ctx context.Context, projectID, issueID string, members, excluded map[string]bool) ([]string, error) {
	identifiers := []string{}
	err := tasks.db.WithContext(ctx).Table("issue_subscribers").
		Where("project_id = ? AND issue_id = ? AND deleted_at IS NULL", projectID, issueID).
		Pluck("subscriber_id", &identifiers).Error
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	subscribers := []string{}
	for _, identifier := range identifiers {
		if !members[identifier] || excluded[identifier] || seen[identifier] {
			continue
		}
		seen[identifier] = true
		subscribers = append(subscribers, identifier)
	}
	// Python builds a set here, which has no order. Fixing one makes the notifications come out the same way twice.
	sort.Strings(subscribers)
	return subscribers, nil
}

func (tasks *NotificationTasks) issueAssignees(ctx context.Context, projectID, issueID string, members map[string]bool) (map[string]bool, error) {
	identifiers := []string{}
	err := tasks.db.WithContext(ctx).Table("issue_assignees").
		Where("project_id = ? AND issue_id = ? AND deleted_at IS NULL", projectID, issueID).
		Pluck("assignee_id", &identifiers).Error
	if err != nil {
		return nil, err
	}
	assignees := map[string]bool{}
	for _, identifier := range identifiers {
		if members[identifier] {
			assignees[identifier] = true
		}
	}
	return assignees, nil
}

// notificationPreference reads somebody's five switches. Having none at all, or more than one, is what Django's get raises on and what loses the whole batch.
func (tasks *NotificationTasks) notificationPreference(ctx context.Context, userID string) (notificationPreferenceRow, bool, error) {
	var rows []notificationPreferenceRow
	err := tasks.db.WithContext(ctx).Table("user_notification_preferences").
		Select("property_change, state_change, comment, mention, issue_completed").
		Where("user_id = ? AND deleted_at IS NULL", userID).Limit(2).Scan(&rows).Error
	if err != nil || len(rows) != 1 {
		return notificationPreferenceRow{}, false, err
	}
	return rows[0], true, nil
}

// commentStripped is the comment's plain text, which the inbox shows under the line of history.
func (tasks *NotificationTasks) commentStripped(ctx context.Context, commentID, issueID, projectID, workspaceID string) (string, error) {
	if commentID == "" {
		return "", nil
	}
	var values []string
	err := tasks.db.WithContext(ctx).Table("issue_comments").
		Where(`id = ? AND issue_id = ? AND project_id = ? AND workspace_id = ? AND deleted_at IS NULL`,
			commentID, issueID, projectID, workspaceID).
		Limit(1).Pluck("comment_stripped", &values).Error
	if err != nil || len(values) == 0 {
		return "", err
	}
	return values[0], nil
}

func (tasks *NotificationTasks) lastIssueActivity(ctx context.Context, issueID string) (*lastActivityRow, error) {
	var rows []lastActivityRow
	err := tasks.db.WithContext(ctx).Table("issue_activities").
		Select("id, verb, field, actor_id, old_value, new_value, created_at").
		Where("issue_id = ? AND deleted_at IS NULL", issueID).
		Order("created_at DESC").Limit(1).Scan(&rows).Error
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return &rows[0], nil
}

func (tasks *NotificationTasks) displayName(ctx context.Context, userID string) (string, bool, error) {
	var values []string
	err := tasks.db.WithContext(ctx).Table("users").Where("id = ?", userID).
		Limit(1).Pluck("display_name", &values).Error
	if err != nil || len(values) == 0 {
		return "", false, err
	}
	return values[0], true, nil
}

// subscribeOne signs the person who made the change up to hear about the work item, and does nothing when they already are.
func (tasks *NotificationTasks) subscribeOne(ctx context.Context, project notificationProject, issueID, userID string, now time.Time) error {
	var count int64
	err := tasks.db.WithContext(ctx).Table("issue_subscribers").
		Where("project_id = ? AND issue_id = ? AND subscriber_id = ? AND deleted_at IS NULL",
			project.ID, issueID, userID).Count(&count).Error
	if err != nil || count > 0 {
		return err
	}
	identifier, err := newTaskUUID()
	if err != nil {
		return err
	}
	return tasks.db.WithContext(ctx).Table("issue_subscribers").
		Clauses(clause.OnConflict{DoNothing: true}).Create(map[string]any{
		"id": identifier, "created_at": now, "updated_at": now,
		"created_by_id": nil, "updated_by_id": nil,
		"workspace_id": project.WorkspaceID, "project_id": project.ID,
		"issue_id": issueID, "subscriber_id": userID,
	}).Error
}

// subscribeMany signs up everybody named in a description or a comment, skipping the ones who are already following it, already assigned to it, or raised it — and anybody who is not in the project.
func (tasks *NotificationTasks) subscribeMany(ctx context.Context, project notificationProject, issueID string, mentions []string, now time.Time) error {
	rows := []map[string]any{}
	for _, mention := range mentions {
		eligible, err := tasks.mentionNeedsSubscription(ctx, project.ID, issueID, mention)
		if err != nil {
			return err
		}
		if !eligible {
			continue
		}
		identifier, err := newTaskUUID()
		if err != nil {
			return err
		}
		rows = append(rows, map[string]any{
			"id": identifier, "created_at": now, "updated_at": now,
			"created_by_id": nil, "updated_by_id": nil,
			"workspace_id": project.WorkspaceID, "project_id": project.ID,
			"issue_id": issueID, "subscriber_id": mention,
		})
	}
	if len(rows) == 0 {
		return nil
	}
	return tasks.db.WithContext(ctx).Table("issue_subscribers").
		Clauses(clause.OnConflict{DoNothing: true}).Create(rows).Error
}

func (tasks *NotificationTasks) mentionNeedsSubscription(ctx context.Context, projectID, issueID, mention string) (bool, error) {
	var already int64
	err := tasks.db.WithContext(ctx).Table("issue_subscribers").
		Where("issue_id = ? AND subscriber_id = ? AND project_id = ? AND deleted_at IS NULL",
			issueID, mention, projectID).Count(&already).Error
	if err != nil || already > 0 {
		return false, err
	}
	err = tasks.db.WithContext(ctx).Table("issue_assignees").
		Where("project_id = ? AND issue_id = ? AND assignee_id = ? AND deleted_at IS NULL",
			projectID, issueID, mention).Count(&already).Error
	if err != nil || already > 0 {
		return false, err
	}
	err = tasks.db.WithContext(ctx).Table("issues").
		Where("project_id = ? AND id = ? AND created_by_id = ? AND deleted_at IS NULL",
			projectID, issueID, mention).Count(&already).Error
	if err != nil || already > 0 {
		return false, err
	}
	var member int64
	err = tasks.db.WithContext(ctx).Table("project_members").
		Where("project_id = ? AND member_id = ? AND is_active = TRUE AND deleted_at IS NULL",
			projectID, mention).Count(&member).Error
	return member > 0, err
}

// recordMentions keeps the list of people named in the description in step with the description itself.
func (tasks *NotificationTasks) recordMentions(ctx context.Context, project notificationProject, issueID string, added, removed []string, now time.Time) error {
	rows := []map[string]any{}
	for _, mention := range added {
		identifier, err := newTaskUUID()
		if err != nil {
			return err
		}
		rows = append(rows, map[string]any{
			"id": identifier, "created_at": now, "updated_at": now,
			"created_by_id": nil, "updated_by_id": nil,
			"mention_id": mention, "issue_id": issueID,
			"project_id": project.ID, "workspace_id": project.WorkspaceID,
		})
	}
	if len(rows) > 0 {
		err := tasks.db.WithContext(ctx).Table("issue_mentions").
			Clauses(clause.OnConflict{DoNothing: true}).Create(rows).Error
		if err != nil {
			return err
		}
	}
	if len(removed) == 0 {
		return nil
	}
	// A queryset delete is a soft one.
	return tasks.db.WithContext(ctx).Table("issue_mentions").
		Where("issue_id = ? AND mention_id IN ? AND deleted_at IS NULL", issueID, removed).
		Update("deleted_at", now).Error
}

func (tasks *NotificationTasks) writeNotifications(ctx context.Context, rows []notificationRow, now time.Time) error {
	if len(rows) == 0 {
		return nil
	}
	values := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		data, err := json.Marshal(row.Data)
		if err != nil {
			return err
		}
		message := any(nil)
		if row.Message != nil {
			encoded, err := json.Marshal(row.Message)
			if err != nil {
				return err
			}
			message = string(encoded)
		}
		values = append(values, map[string]any{
			"id": row.ID, "created_at": now, "updated_at": now,
			"created_by_id": nil, "updated_by_id": nil,
			"workspace_id": row.WorkspaceID, "project_id": row.ProjectID,
			"data": string(data), "entity_identifier": row.EntityIdentifier, "entity_name": "issue",
			// A notification carries either a title or a message and never both, and the one it does not carry is the column's own empty default rather than null.
			"title": row.Title, "message": message, "message_html": "<p></p>", "message_stripped": nil,
			"sender": row.Sender, "triggered_by_id": row.TriggeredByID, "receiver_id": row.ReceiverID,
		})
	}
	return tasks.db.WithContext(ctx).Table("notifications").Create(values).Error
}

func (tasks *NotificationTasks) writeEmailLogs(ctx context.Context, rows []emailLogRow, now time.Time) error {
	if len(rows) == 0 {
		return nil
	}
	values := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		data, err := json.Marshal(row.Data)
		if err != nil {
			return err
		}
		values = append(values, map[string]any{
			"id": row.ID, "created_at": now, "updated_at": now,
			"created_by_id": nil, "updated_by_id": nil,
			"receiver_id": row.ReceiverID, "triggered_by_id": row.TriggeredByID,
			"entity_identifier": row.EntityIdentifier, "entity_name": "issue",
			"data": string(data), "processed_at": nil, "sent_at": nil,
			"entity": "", "old_value": nil, "new_value": nil,
		})
	}
	return tasks.db.WithContext(ctx).Table("email_notification_logs").
		Clauses(clause.OnConflict{DoNothing: true}).Create(values).Error
}
