package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// The activities about the things hanging off a work item rather than the work item itself: its comments, the cycles and modules it is in, its links and attachments, the relations it has to other work items, the reactions and votes people left, and the two states it can be in before it is really a work item at all.
//
// They are all in one place because they share one shape: read the two snapshots, write a row. What differs is which of the two snapshots the value comes from, and that is not consistent — an attachment reads its value out of the *current* snapshot on creation, where a comment and a link read theirs out of the requested one.

// dispatchRelated handles every activity type that is not the work item's own.
func (tasks *IssueActivityTasks) dispatchRelated(ctx context.Context, activityType, requestedData, currentInstance string, context activityContext) ([]activityRow, error) {
	requested, _ := decodeActivitySnapshot(anyString(requestedData))
	current, _ := decodeActivitySnapshot(anyString(currentInstance))

	switch activityType {
	case "comment.activity.created":
		identifier := textOrNil(activityTextOrEmpty(requested["id"]))
		return []activityRow{tasks.row(context, activityRow{
			Verb: "created", Field: text("comment"), Comment: "created a comment",
			NewValue:      text(activityTextOrEmpty(requested["comment_html"])),
			NewIdentifier: identifier, IssueCommentID: identifier,
		})}, nil

	case "comment.activity.updated":
		if equalActivityValues(current["comment_html"], requested["comment_html"]) {
			return nil, nil
		}
		// Both identifiers come from the snapshot rather than the request, so an edit points at the comment as it was.
		identifier := textOrNil(activityTextOrEmpty(current["id"]))
		return []activityRow{tasks.row(context, activityRow{
			Verb: "updated", Field: text("comment"), Comment: "updated a comment",
			OldValue:      text(activityTextOrEmpty(current["comment_html"])),
			NewValue:      text(activityTextOrEmpty(requested["comment_html"])),
			OldIdentifier: identifier, NewIdentifier: identifier, IssueCommentID: identifier,
		})}, nil

	case "comment.activity.deleted":
		return []activityRow{tasks.row(context, activityRow{
			Verb: "deleted", Field: text("comment"), Comment: "deleted the comment",
			IssueCommentID: textOrNil(activityTextOrEmpty(requested["comment_id"])),
		})}, nil

	case "cycle.activity.created":
		return tasks.cycleIssueCreated(ctx, current, context)

	case "cycle.activity.deleted":
		return tasks.cycleIssueDeleted(ctx, requested, context)

	case "module.activity.created":
		moduleID := activityTextOrEmpty(requested["module_id"])
		name, _, err := tasks.namedRow(ctx, "modules", "name", moduleID)
		if err != nil {
			return nil, err
		}
		if err := tasks.touchIssue(ctx, context.issueID, context.now); err != nil {
			return nil, err
		}
		return []activityRow{tasks.row(context, activityRow{
			Verb: "created", Field: text("modules"), Comment: "added module " + name,
			OldValue: text(""), NewValue: text(name), NewIdentifier: textOrNil(moduleID),
		})}, nil

	case "module.activity.deleted":
		name := activityText(current["module_name"])
		if err := tasks.touchIssue(ctx, context.issueID, context.now); err != nil {
			return nil, err
		}
		return []activityRow{tasks.row(context, activityRow{
			Verb: "deleted", Field: text("modules"),
			Comment:  "removed this issue from " + activityTextOrEmpty(current["module_name"]),
			OldValue: name, NewValue: text(""),
			OldIdentifier: textOrNil(activityTextOrEmpty(requested["module_id"])),
		})}, nil

	case "link.activity.created":
		return []activityRow{tasks.row(context, activityRow{
			Verb: "created", Field: text("link"), Comment: "created a link",
			NewValue:      text(activityTextOrEmpty(requested["url"])),
			NewIdentifier: textOrNil(activityTextOrEmpty(requested["id"])),
		})}, nil

	case "link.activity.updated":
		if equalActivityValues(current["url"], requested["url"]) {
			return nil, nil
		}
		identifier := textOrNil(activityTextOrEmpty(current["id"]))
		return []activityRow{tasks.row(context, activityRow{
			Verb: "updated", Field: text("link"), Comment: "updated a link",
			OldValue:      text(activityTextOrEmpty(current["url"])),
			NewValue:      text(activityTextOrEmpty(requested["url"])),
			OldIdentifier: identifier, NewIdentifier: identifier,
		})}, nil

	case "link.activity.deleted":
		return []activityRow{tasks.row(context, activityRow{
			Verb: "deleted", Field: text("link"), Comment: "deleted the link",
			OldValue: text(activityTextOrEmpty(current["url"])), NewValue: text(""),
		})}, nil

	case "attachment.activity.created":
		// The attachment reads its value out of the snapshot rather than the request, which is the opposite of the comment and the link.
		return []activityRow{tasks.row(context, activityRow{
			Verb: "created", Field: text("attachment"), Comment: "created an attachment",
			NewValue:      text(activityTextOrEmpty(current["asset"])),
			NewIdentifier: textOrNil(activityTextOrEmpty(current["id"])),
		})}, nil

	case "attachment.activity.deleted":
		return []activityRow{tasks.row(context, activityRow{
			Verb: "deleted", Field: text("attachment"), Comment: "deleted the attachment",
		})}, nil

	case "issue_reaction.activity.created":
		return tasks.issueReactionCreated(ctx, requested, context)

	case "issue_reaction.activity.deleted":
		if current == nil || current["reaction"] == nil {
			return nil, nil
		}
		return []activityRow{tasks.row(context, activityRow{
			Verb: "deleted", Field: text("reaction"), Comment: "removed the reaction",
			OldValue:      text(activityTextOrEmpty(current["reaction"])),
			OldIdentifier: textOrNil(activityTextOrEmpty(current["identifier"])),
		})}, nil

	case "comment_reaction.activity.created":
		return tasks.commentReactionCreated(ctx, requested, context)

	case "comment_reaction.activity.deleted":
		return tasks.commentReactionDeleted(ctx, current, context)

	case "issue_vote.activity.created":
		if requested == nil || requested["vote"] == nil {
			return nil, nil
		}
		// A vote is reported as an update rather than a creation, which is upstream's wording.
		return []activityRow{tasks.row(context, activityRow{
			Verb: "updated", Field: text("vote"), Comment: "added the vote",
			NewValue: text(activityTextOrEmpty(requested["vote"])),
		})}, nil

	case "issue_vote.activity.deleted":
		if current == nil || current["vote"] == nil {
			return nil, nil
		}
		return []activityRow{tasks.row(context, activityRow{
			Verb: "deleted", Field: text("vote"), Comment: "removed the vote",
			OldValue:      text(activityTextOrEmpty(current["vote"])),
			OldIdentifier: textOrNil(activityTextOrEmpty(current["identifier"])),
		})}, nil

	case "issue_relation.activity.created":
		return tasks.relationCreated(ctx, requested, current, context)

	case "issue_relation.activity.deleted":
		return tasks.relationDeleted(ctx, requested, context)

	case "issue_draft.activity.created":
		return []activityRow{tasks.row(context, activityRow{
			Verb: "created", Field: text("draft"), Comment: "drafted the issue",
		})}, nil

	case "issue_draft.activity.updated":
		// A draft that stops being one is reported as the work item being created, with no field at all.
		if value, named := requested["is_draft"]; named && value != nil && !truthy(value) {
			return []activityRow{tasks.row(context, activityRow{
				Verb: "updated", Comment: "created the issue",
			})}, nil
		}
		return []activityRow{tasks.row(context, activityRow{
			Verb: "updated", Field: text("draft"), Comment: "updated the draft issue",
		})}, nil

	case "issue_draft.activity.deleted":
		// This one names no work item, which is what leaves a deleted draft's line unattached to anything.
		row := tasks.row(context, activityRow{
			Verb: "deleted", Field: text("draft"), Comment: "deleted the draft issue",
		})
		row.IssueID = nil
		return []activityRow{row}, nil

	case "intake.activity.created":
		if requested == nil || requested["status"] == nil {
			return nil, nil
		}
		return []activityRow{tasks.row(context, activityRow{
			// The verb is the status itself rather than a word, which is what the history renders from.
			Verb: activityTextOrEmpty(requested["status"]), Field: text("intake"),
			Comment:  "updated the intake status",
			OldValue: intakeStatusName(current["status"]), NewValue: intakeStatusName(requested["status"]),
		})}, nil
	}
	return nil, nil
}

// cycleIssueCreated writes a line for each work item that moved cycle and each one that was put into one.
//
// The created half arrives as a **json string inside the snapshot** rather than as an object, because Django builds it with serializers.serialize and then dumps the snapshot around it.
func (tasks *IssueActivityTasks) cycleIssueCreated(ctx context.Context, current map[string]any, context activityContext) ([]activityRow, error) {
	rows := []activityRow{}

	if updated, ok := current["updated_cycle_issues"].([]any); ok {
		for _, entry := range updated {
			record, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			issueID := activityTextOrEmpty(record["issue_id"])
			was, err := tasks.cycleNamed(ctx, activityTextOrEmpty(record["old_cycle_id"]))
			if err != nil {
				return nil, err
			}
			now, err := tasks.cycleNamed(ctx, activityTextOrEmpty(record["new_cycle_id"]))
			if err != nil {
				return nil, err
			}
			if err := tasks.touchIssue(ctx, issueID, context.now); err != nil {
				return nil, err
			}
			row := tasks.row(context, activityRow{
				Verb: "updated", Field: text("cycles"),
				OldValue: text(was.nameOrEmpty()), NewValue: text(now.nameOrEmpty()),
				// The newline and the run of spaces are upstream's, produced by a triple-quoted string laid out across two lines.
				Comment:       fmt.Sprintf("updated cycle from %s\n                to %s", was.nameOrEmpty(), now.nameOrEmpty()),
				OldIdentifier: was.id, NewIdentifier: now.id,
			})
			row.IssueID = textOrNil(issueID)
			rows = append(rows, row)
		}
	}

	created := []any{}
	if encoded, ok := current["created_cycle_issues"].(string); ok && encoded != "" {
		if err := json.Unmarshal([]byte(encoded), &created); err != nil {
			return nil, errActivityAbandoned
		}
	}
	for _, entry := range created {
		record, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		fields, _ := record["fields"].(map[string]any)
		cycleID := activityTextOrEmpty(fields["cycle"])
		issueID := activityTextOrEmpty(fields["issue"])
		cycle, err := tasks.cycleNamed(ctx, cycleID)
		if err != nil {
			return nil, err
		}
		if cycle.id == nil {
			// Django reads the name off the cycle without checking it is there, which raises and abandons the batch.
			return nil, errActivityAbandoned
		}
		if err := tasks.touchIssue(ctx, issueID, context.now); err != nil {
			return nil, err
		}
		row := tasks.row(context, activityRow{
			Verb: "created", Field: text("cycles"), Comment: "added cycle " + *cycle.name,
			OldValue: text(""), NewValue: cycle.name, NewIdentifier: cycle.id,
		})
		row.IssueID = textOrNil(issueID)
		rows = append(rows, row)
	}
	return rows, nil
}

// cycleIssueDeleted writes one line per work item taken out of a cycle. The cycle's name is read from the database when it is still there and from the request when it is not, which is what lets the line survive the cycle being deleted with it.
func (tasks *IssueActivityTasks) cycleIssueDeleted(ctx context.Context, requested map[string]any, context activityContext) ([]activityRow, error) {
	cycleID := activityTextOrEmpty(requested["cycle_id"])
	name := activityTextOrEmpty(requested["cycle_name"])
	cycle, err := tasks.cycleNamed(ctx, cycleID)
	if err != nil {
		return nil, err
	}
	if cycle.name != nil {
		name = *cycle.name
	}
	issues, _ := requested["issues"].([]any)
	rows := make([]activityRow, 0, len(issues))
	for _, entry := range issues {
		issueID := activityTextOrEmpty(entry)
		if err := tasks.touchIssue(ctx, issueID, context.now); err != nil {
			return nil, err
		}
		row := tasks.row(context, activityRow{
			Verb: "deleted", Field: text("cycles"), Comment: "removed this issue from " + name,
			OldValue: text(name), NewValue: text(""), OldIdentifier: textOrNil(cycleID),
		})
		row.IssueID = textOrNil(issueID)
		rows = append(rows, row)
	}
	return rows, nil
}

// issueReactionCreated finds the reaction the caller just left and reports it.
//
// The lookup is by reaction, project and actor and takes whichever row comes first — it does not name the work item, so somebody who left the same reaction on two work items has the line point at one of them arbitrarily. Reproduced rather than corrected.
func (tasks *IssueActivityTasks) issueReactionCreated(ctx context.Context, requested map[string]any, context activityContext) ([]activityRow, error) {
	if requested == nil || requested["reaction"] == nil {
		return nil, nil
	}
	reaction := activityTextOrEmpty(requested["reaction"])
	var identifiers []string
	err := tasks.db.WithContext(ctx).Table("issue_reactions").
		Where("reaction = ? AND project_id = ? AND actor_id = ? AND deleted_at IS NULL",
			reaction, context.projectID, context.actorID).
		Order("created_at DESC").Limit(1).Pluck("id", &identifiers).Error
	if err != nil || len(identifiers) == 0 {
		return nil, err
	}
	return []activityRow{tasks.row(context, activityRow{
		Verb: "created", Field: text("reaction"), Comment: "added the reaction",
		NewValue: text(reaction), NewIdentifier: text(identifiers[0]),
	})}, nil
}

// commentReactionCreated does the same for a comment's reaction, and attaches the line to the work item the comment is on.
func (tasks *IssueActivityTasks) commentReactionCreated(ctx context.Context, requested map[string]any, context activityContext) ([]activityRow, error) {
	if requested == nil || requested["reaction"] == nil {
		return nil, nil
	}
	reaction := activityTextOrEmpty(requested["reaction"])
	var rows []struct {
		ID        string `gorm:"column:id"`
		CommentID string `gorm:"column:comment_id"`
	}
	err := tasks.db.WithContext(ctx).Table("comment_reactions").Select("id, comment_id").
		Where("reaction = ? AND project_id = ? AND actor_id = ? AND deleted_at IS NULL",
			reaction, context.projectID, context.actorID).
		Order("created_at DESC").Limit(1).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		// Django unpacks the missing row into two names, which raises and abandons the batch.
		return nil, errActivityAbandoned
	}
	var issueIDs []string
	err = tasks.db.WithContext(ctx).Table("issue_comments").
		Where("id = ? AND project_id = ? AND deleted_at IS NULL", rows[0].CommentID, context.projectID).
		Limit(1).Pluck("issue_id", &issueIDs).Error
	if err != nil {
		return nil, err
	}
	if len(issueIDs) == 0 {
		// The comment is read with a get, which raises when it is not the project's.
		return nil, errActivityAbandoned
	}
	row := tasks.row(context, activityRow{
		Verb: "created", Field: text("reaction"), Comment: "added the reaction",
		NewValue: text(reaction), NewIdentifier: text(rows[0].ID),
	})
	row.IssueID = text(issueIDs[0])
	return []activityRow{row}, nil
}

func (tasks *IssueActivityTasks) commentReactionDeleted(ctx context.Context, current map[string]any, context activityContext) ([]activityRow, error) {
	if current == nil || current["reaction"] == nil {
		return nil, nil
	}
	var issueIDs []string
	err := tasks.db.WithContext(ctx).Table("issue_comments").
		Where("id = ? AND project_id = ? AND deleted_at IS NULL",
			activityTextOrEmpty(current["comment_id"]), context.projectID).
		Limit(1).Pluck("issue_id", &issueIDs).Error
	if err != nil || len(issueIDs) == 0 {
		return nil, err
	}
	row := tasks.row(context, activityRow{
		Verb: "deleted", Field: text("reaction"), Comment: "removed the reaction",
		OldValue:      text(activityTextOrEmpty(current["reaction"])),
		OldIdentifier: textOrNil(activityTextOrEmpty(current["identifier"])),
	})
	row.IssueID = text(issueIDs[0])
	return []activityRow{row}, nil
}

// inverseRelations is get_inverse_relation: what the relation looks like from the other work item's side.
var inverseRelations = map[string]string{
	"start_after": "start_before", "finish_after": "finish_before",
	"blocked_by": "blocking", "blocking": "blocked_by",
	"start_before": "start_after", "finish_before": "finish_after",
	"implemented_by": "implements", "implements": "implemented_by",
}

func inverseRelation(relationType string) string {
	if inverse, known := inverseRelations[relationType]; known {
		return inverse
	}
	return relationType
}

// relationCreated writes two lines per relation, one from each side, and only when there was no previous state.
func (tasks *IssueActivityTasks) relationCreated(ctx context.Context, requested, current map[string]any, context activityContext) ([]activityRow, error) {
	if current != nil {
		return nil, nil
	}
	related, ok := requested["issues"].([]any)
	if !ok {
		return nil, nil
	}
	relationType := activityTextOrEmpty(requested["relation_type"])
	inverse := inverseRelation(relationType)

	own, err := tasks.issueKey(ctx, context.issueID)
	if err != nil {
		return nil, err
	}
	if own.id == nil {
		return nil, errActivityAbandoned
	}

	rows := []activityRow{}
	for _, entry := range related {
		relatedID := activityTextOrEmpty(entry)
		far, err := tasks.issueKey(ctx, relatedID)
		if err != nil {
			return nil, err
		}
		if far.id == nil {
			// Django reads this one with a get, which raises.
			return nil, errActivityAbandoned
		}
		rows = append(rows, tasks.row(context, activityRow{
			Verb: "updated", Field: text(relationType),
			Comment: "added " + relationType + " relation",
			// The near side's line points back at the far work item through old_identifier, which is where a reader would expect the new one.
			OldValue: text(""), NewValue: text(far.key), OldIdentifier: text(relatedID),
		}))
		mirrored := tasks.row(context, activityRow{
			Verb: "updated", Field: text(inverse),
			Comment:  "added " + inverse + " relation",
			OldValue: text(""), NewValue: text(own.key), OldIdentifier: text(context.issueID),
		})
		mirrored.IssueID = text(relatedID)
		rows = append(rows, mirrored)
	}
	return rows, nil
}

// relationDeleted writes the same two lines in reverse.
//
// The far side's line names the relation by hand rather than through the inverse mapping, and the two disagree: only blocking and blocked_by are turned around, so deleting a start_after relation leaves both lines saying start_after. Reproduced rather than corrected.
func (tasks *IssueActivityTasks) relationDeleted(ctx context.Context, requested map[string]any, context activityContext) ([]activityRow, error) {
	relatedID := activityTextOrEmpty(requested["related_issue"])
	relationType := activityTextOrEmpty(requested["relation_type"])

	far, err := tasks.issueKey(ctx, relatedID)
	if err != nil {
		return nil, err
	}
	if far.id == nil {
		return nil, errActivityAbandoned
	}
	own, err := tasks.issueKey(ctx, context.issueID)
	if err != nil {
		return nil, err
	}
	if own.id == nil {
		return nil, errActivityAbandoned
	}

	farField := relationType
	switch relationType {
	case "blocked_by":
		farField = "blocking"
	case "blocking":
		farField = "blocked_by"
	}

	near := tasks.row(context, activityRow{
		Verb: "deleted", Field: text(relationType),
		Comment:  "deleted " + relationType + " relation",
		OldValue: text(far.key), NewValue: text(""), OldIdentifier: text(relatedID),
	})
	other := tasks.row(context, activityRow{
		Verb: "deleted", Field: text(farField),
		Comment:  "deleted " + relationType + " relation",
		OldValue: text(own.key), NewValue: text(""), OldIdentifier: text(relatedID),
	})
	other.IssueID = text(relatedID)
	return []activityRow{near, other}, nil
}

// namedCycle is a cycle as a history line reports it.
type namedCycle struct {
	id   *string
	name *string
}

func (cycle namedCycle) nameOrEmpty() string {
	if cycle.name == nil {
		return ""
	}
	return *cycle.name
}

func (tasks *IssueActivityTasks) cycleNamed(ctx context.Context, cycleID string) (namedCycle, error) {
	if cycleID == "" {
		return namedCycle{}, nil
	}
	var rows []struct {
		ID   string `gorm:"column:id"`
		Name string `gorm:"column:name"`
	}
	err := tasks.db.WithContext(ctx).Table("cycles").Select("id, name").
		Where("id = ? AND deleted_at IS NULL", cycleID).Limit(1).Scan(&rows).Error
	if err != nil || len(rows) == 0 {
		return namedCycle{}, err
	}
	return namedCycle{id: text(rows[0].ID), name: text(rows[0].Name)}, nil
}

// touchIssue moves a work item's updated_at, which is what a change to something hanging off it counts as.
func (tasks *IssueActivityTasks) touchIssue(ctx context.Context, issueID string, now time.Time) error {
	if issueID == "" {
		return nil
	}
	return tasks.db.WithContext(ctx).Table("issues").
		Where("id = ? AND deleted_at IS NULL", issueID).Update("updated_at", now).Error
}

// intakeStatusNames are the five words the history uses for the five intake statuses.
var intakeStatusNames = map[string]string{
	"-2": "Pending", "-1": "Rejected", "0": "Snoozed", "1": "Accepted", "2": "Duplicate",
}

// intakeStatusName reads a status as its word, and a status with no word as nothing.
func intakeStatusName(value any) *string {
	if value == nil {
		return nil
	}
	name, known := intakeStatusNames[activityTextOrEmpty(value)]
	if !known {
		return nil
	}
	return &name
}
