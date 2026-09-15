package project

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"github.com/yldm-tech/pace/apps/api-go/internal/htmlsanitizer"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// roleMemberOrAbove is the role floor Django applies when it filters the
// assignees a caller may set.
const roleMemberOrAbove = 15

func (handler *Handler) registerIssueDetailRoutes(router gin.IRouter) {
	router.GET("/api/workspaces/:slug/projects/:id/issues/:issue/", handler.authenticatedIssueUUID(handler.issueRetrieve))
	router.PATCH("/api/workspaces/:slug/projects/:id/issues/:issue/", handler.authenticatedIssueUUID(handler.issuePartialUpdate))
	router.DELETE("/api/workspaces/:slug/projects/:id/issues/:issue/", handler.authenticatedIssueUUID(handler.issueDestroy))
}

// issueRow carries the annotations the detail queryset adds.
type issueRow struct {
	Issue
	CycleID         *string        `gorm:"column:cycle_id"`
	LinkCount       *int64         `gorm:"column:link_count"`
	AttachmentCount *int64         `gorm:"column:attachment_count"`
	SubIssuesCount  *int64         `gorm:"column:sub_issues_count"`
	LabelIDs        pq.StringArray `gorm:"column:label_ids;type:uuid[]"`
	AssigneeIDs     pq.StringArray `gorm:"column:assignee_ids;type:uuid[]"`
	ModuleIDs       pq.StringArray `gorm:"column:module_ids;type:uuid[]"`
	IsSubscribed    bool           `gorm:"column:is_subscribed"`
	// StateGroup is annotated only by the sub-issue read, the same way is_subscribed is annotated only by retrieve. GORM leaves it nil on the querysets that do not select it.
	StateGroup *string `gorm:"column:state_group"`
}

func (handler *Handler) issueRetrieve(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	slug, projectID, issueID := c.Param("slug"), c.Param("id"), c.Param("issue")
	// retrieve reads through the default manager, so an archived or draft issue
	// is still returned here even though the list route hides both.
	row, found, err := handler.issueDetailRow(c.Request.Context(), slug, projectID, issueID, user.ID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		handler.notFound(c)
		return
	}
	visible, err := handler.guestMaySeeIssue(c.Request.Context(), slug, projectID, row.Issue, user.ID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !visible {
		c.JSON(http.StatusForbidden, gin.H{"error": "You are not allowed to view this issue"})
		return
	}
	if handler.tasks != nil {
		err := handler.tasks.PublishRecentVisit(c.Request.Context(), "issue", issueID, user.ID, projectID, slug)
		if err != nil {
			handler.internalError(c, err)
			return
		}
	}
	c.JSON(http.StatusOK, issueDetailJSON(row, true))
}

func (handler *Handler) issuePartialUpdate(c *gin.Context, user *auth.User) {
	slug, projectID, issueID := c.Param("slug"), c.Param("id"), c.Param("issue")
	row, found, err := handler.issueDetailRow(c.Request.Context(), slug, projectID, issueID, user.ID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "Issue not found"})
		return
	}
	if !handler.requireIssueWriter(c, user, row.Issue) {
		return
	}
	// current_instance is the detail serializer's output before the change. It
	// comes from the update queryset, which never annotates is_subscribed, so
	// that key is absent here.
	snapshot, err := json.Marshal(issueDetailJSON(row, false))
	if err != nil {
		handler.internalError(c, err)
		return
	}
	currentInstance := string(snapshot)

	var body map[string]json.RawMessage
	if err := c.ShouldBindJSON(&body); err != nil {
		handler.invalidDetail(c)
		return
	}
	skipActivity := false
	if raw, exists := body["skip_activity"]; exists {
		_ = json.Unmarshal(raw, &skipActivity)
		// Django pops it off the request data before serializing.
		delete(body, "skip_activity")
	}
	_, isDescriptionUpdate := body["description_html"]
	requested, err := json.Marshal(decodeRawFields(body))
	if err != nil {
		handler.internalError(c, err)
		return
	}
	requestedData := string(requested)

	fields, ok := handler.issueFields(c, body, projectID)
	if !ok {
		return
	}
	now := handler.clock().UTC()
	updates := fields.updates(now, user.ID)
	err = handler.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if len(updates) > 0 {
			if err := tx.Model(&Issue{}).Where("id = ?", row.ID).Updates(updates).Error; err != nil {
				return err
			}
		}
		if fields.hasAssignees {
			if err := handler.syncIssueAssignees(tx, row.Issue, fields.assigneeIDs, now); err != nil {
				return err
			}
		}
		if fields.hasLabels {
			if err := handler.syncIssueLabels(tx, row.Issue, fields.labelIDs, now); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		handler.internalError(c, err)
		return
	}
	// A migration description update asks for no activity at all.
	if !(skipActivity && isDescriptionUpdate) {
		if err := handler.publishIssueActivity(c, issueActivity{
			Type: "issue.activity.updated", RequestedData: &requestedData, CurrentInstance: &currentInstance,
			ActorID: user.ID, IssueID: issueID, ProjectID: projectID,
			Notification: true, Origin: handler.origin(), Epoch: now,
		}); err != nil {
			handler.internalError(c, err)
			return
		}
		if handler.tasks != nil {
			err := handler.tasks.PublishModelActivity(c.Request.Context(), "issue", issueID,
				decodeRawFields(body), &currentInstance, user.ID, slug, handler.origin())
			if err != nil {
				handler.internalError(c, err)
				return
			}
			err = handler.tasks.PublishIssueDescriptionVersion(c.Request.Context(), currentInstance, issueID, user.ID)
			if err != nil {
				handler.internalError(c, err)
				return
			}
		}
	}
	// Django answers with no body on this route.
	c.Status(http.StatusNoContent)
}

func (handler *Handler) issueDestroy(c *gin.Context, user *auth.User) {
	slug, projectID, issueID := c.Param("slug"), c.Param("id"), c.Param("issue")
	var issue Issue
	err := handler.db.WithContext(c.Request.Context()).
		Where("id = ? AND project_id = ? AND workspace_id = (SELECT id FROM workspaces WHERE slug = ?) AND deleted_at IS NULL",
			issueID, projectID, slug).Take(&issue).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		handler.notFound(c)
		return
	}
	if err != nil {
		handler.internalError(c, err)
		return
	}
	// destroy allows a project admin or the issue's creator.
	if !handler.requireIssueAdminOrCreator(c, user, issue) {
		return
	}
	now := handler.clock().UTC()
	err = handler.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		err := tx.Model(&Issue{}).Where("id = ?", issue.ID).Updates(map[string]any{
			"deleted_at": now, "updated_at": now, "updated_by_id": user.ID,
		}).Error
		if err != nil {
			return err
		}
		// The recent visits are removed for real, not soft deleted.
		return tx.Exec(
			`DELETE FROM user_recent_visits WHERE project_id = ? AND entity_identifier = ? AND entity_name = 'issue'
			 AND workspace_id = (SELECT id FROM workspaces WHERE slug = ?)`,
			projectID, issueID, slug,
		).Error
	})
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if handler.tasks != nil {
		if err := handler.tasks.PublishSoftDeleteRelatedObjects(c.Request.Context(), "db", "issue", issue.ID); err != nil {
			handler.internalError(c, err)
			return
		}
	}
	requested, err := json.Marshal(map[string]any{"issue_id": issueID})
	if err != nil {
		handler.internalError(c, err)
		return
	}
	requestedData := string(requested)
	empty := "{}"
	// This is the one activity Django sends with subscriber turned off.
	if err := handler.publishIssueActivity(c, issueActivity{
		Type: "issue.activity.deleted", RequestedData: &requestedData, CurrentInstance: &empty,
		ActorID: user.ID, IssueID: issueID, ProjectID: projectID,
		Notification: true, Origin: handler.origin(), Epoch: now, SubscriberSet: true,
	}); err != nil {
		handler.internalError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// issueDetailRow builds the annotated row the detail routes serialize.
func (handler *Handler) issueDetailRow(ctx context.Context, slug, projectID, issueID, userID string) (issueRow, bool, error) {
	var rows []issueRow
	err := handler.db.WithContext(ctx).Table("issues i").
		Select(`i.*,
			(SELECT ci.cycle_id FROM cycle_issues ci WHERE ci.issue_id = i.id AND ci.deleted_at IS NULL LIMIT 1) AS cycle_id,
			(SELECT COUNT(*) FROM issue_links il WHERE il.issue_id = i.id AND il.deleted_at IS NULL) AS link_count,
			(SELECT COUNT(*) FROM file_assets fa WHERE fa.issue_id = i.id AND fa.entity_type = 'ISSUE_ATTACHMENT' AND fa.deleted_at IS NULL) AS attachment_count,
			(SELECT COUNT(*) FROM issues sub WHERE sub.parent_id = i.id AND `+issueObjectsPredicate("sub")+`) AS sub_issues_count,
			COALESCE((SELECT ARRAY_AGG(DISTINCT il2.label_id) FROM issue_labels il2 WHERE il2.issue_id = i.id AND il2.deleted_at IS NULL), '{}') AS label_ids,
			COALESCE((SELECT ARRAY_AGG(DISTINCT ia.assignee_id) FROM issue_assignees ia
				JOIN project_members pm2 ON pm2.member_id = ia.assignee_id AND pm2.is_active = TRUE
				WHERE ia.issue_id = i.id AND ia.deleted_at IS NULL), '{}') AS assignee_ids,
			COALESCE((SELECT ARRAY_AGG(DISTINCT mi.module_id) FROM module_issues mi
				JOIN modules m ON m.id = mi.module_id AND m.archived_at IS NULL
				WHERE mi.issue_id = i.id AND mi.deleted_at IS NULL), '{}') AS module_ids,
			EXISTS (SELECT 1 FROM issue_subscribers isub WHERE isub.issue_id = i.id AND isub.subscriber_id = ?
				AND isub.project_id = i.project_id AND isub.deleted_at IS NULL) AS is_subscribed`, userID).
		Joins("JOIN workspaces w ON w.id = i.workspace_id").
		Where("i.id = ? AND i.project_id = ? AND w.slug = ? AND i.deleted_at IS NULL", issueID, projectID, slug).
		Limit(1).Scan(&rows).Error
	if err != nil {
		return issueRow{}, false, err
	}
	if len(rows) == 0 {
		return issueRow{}, false, nil
	}
	return rows[0], true, nil
}

// guestMaySeeIssue mirrors the guard on retrieve: a guest sees an issue only
// when the project opens all features to guests or they raised it.
func (handler *Handler) guestMaySeeIssue(ctx context.Context, slug, projectID string, issue Issue, userID string) (bool, error) {
	member, found, err := handler.activeProjectMember(ctx, slug, projectID, userID)
	if err != nil || !found {
		return found, err
	}
	if member.Role != roleGuest {
		return true, nil
	}
	var project Project
	if err := handler.db.WithContext(ctx).Where("id = ?", projectID).Take(&project).Error; err != nil {
		return false, err
	}
	if project.GuestViewAllFeatures {
		return true, nil
	}
	return issue.CreatedByID != nil && *issue.CreatedByID == userID, nil
}

// requireIssueWriter is allow_permission([ADMIN, MEMBER], creator=True) on the
// update route.
func (handler *Handler) requireIssueWriter(c *gin.Context, user *auth.User, issue Issue) bool {
	return handler.requireIssueActor(c, user, issue, roleAdmin, roleMember)
}

// requireIssueAdminOrCreator is allow_permission([ADMIN], creator=True) on the
// delete route.
func (handler *Handler) requireIssueAdminOrCreator(c *gin.Context, user *auth.User, issue Issue) bool {
	return handler.requireIssueActor(c, user, issue, roleAdmin)
}

func (handler *Handler) requireIssueActor(c *gin.Context, user *auth.User, issue Issue, allowed ...int) bool {
	member, found, err := handler.activeProjectMember(c.Request.Context(), c.Param("slug"), c.Param("id"), user.ID)
	if err != nil {
		handler.internalError(c, err)
		return false
	}
	if found {
		// The creator branch runs before the role check in Django.
		if issue.CreatedByID != nil && *issue.CreatedByID == user.ID {
			return true
		}
		for _, candidate := range allowed {
			if member.Role == candidate {
				return true
			}
		}
		workspaceRole, _, err := handler.workspaceMemberRole(c.Request.Context(), c.Param("slug"), user.ID)
		if err != nil {
			handler.internalError(c, err)
			return false
		}
		if workspaceRole == roleAdmin {
			return true
		}
	}
	c.JSON(http.StatusForbidden, gin.H{"error": "You don't have the required permissions."})
	return false
}

// syncIssueAssignees reproduces the update path: the existing rows are soft
// deleted and the new set inserted, with conflicts ignored.
func (handler *Handler) syncIssueAssignees(tx *gorm.DB, issue Issue, assigneeIDs []string, now time.Time) error {
	err := tx.Model(&IssueAssignee{}).Where("issue_id = ? AND deleted_at IS NULL", issue.ID).
		Update("deleted_at", now).Error
	if err != nil {
		return err
	}
	rows := make([]IssueAssignee, 0, len(assigneeIDs))
	for _, assigneeID := range assigneeIDs {
		rowID, err := newUUID()
		if err != nil {
			return err
		}
		rows = append(rows, IssueAssignee{
			ID: rowID, CreatedAt: now, UpdatedAt: now,
			CreatedByID: issue.CreatedByID, UpdatedByID: issue.UpdatedByID,
			ProjectID: issue.ProjectID, WorkspaceID: issue.WorkspaceID,
			IssueID: issue.ID, AssigneeID: assigneeID,
		})
	}
	if len(rows) == 0 {
		return nil
	}
	return tx.Clauses(onConflictDoNothing()).Create(&rows).Error
}

func (handler *Handler) syncIssueLabels(tx *gorm.DB, issue Issue, labelIDs []string, now time.Time) error {
	err := tx.Model(&IssueLabel{}).Where("issue_id = ? AND deleted_at IS NULL", issue.ID).
		Update("deleted_at", now).Error
	if err != nil {
		return err
	}
	rows := make([]IssueLabel, 0, len(labelIDs))
	for _, labelID := range labelIDs {
		rowID, err := newUUID()
		if err != nil {
			return err
		}
		rows = append(rows, IssueLabel{
			ID: rowID, CreatedAt: now, UpdatedAt: now,
			CreatedByID: issue.CreatedByID, UpdatedByID: issue.UpdatedByID,
			ProjectID: issue.ProjectID, WorkspaceID: issue.WorkspaceID,
			IssueID: issue.ID, LabelID: labelID,
		})
	}
	if len(rows) == 0 {
		return nil
	}
	return tx.Clauses(onConflictDoNothing()).Create(&rows).Error
}

// issueDetailJSON is IssueDetailSerializer.
//
// is_intake is declared on that serializer but only annotated by
// IssueDetailIdentifierEndpoint. DRF makes a read-only field not required, so
// get_attribute raises SkipField and the key is dropped rather than erroring.
// These routes therefore omit it, and withSubscription says whether the caller
// annotated is_subscribed, which the update queryset does not.
func issueDetailJSON(row issueRow, withSubscription bool) gin.H {
	data := gin.H{
		"id": row.ID, "name": row.Name, "state_id": row.StateID, "sort_order": row.SortOrder,
		"completed_at": row.CompletedAt, "estimate_point": row.EstimatePointID,
		"priority": row.Priority, "start_date": dateOnly(row.StartDate), "target_date": dateOnly(row.TargetDate),
		"sequence_id": row.SequenceID, "project_id": row.ProjectID, "parent_id": row.ParentID,
		"cycle_id": row.CycleID, "module_ids": stringsOrEmpty(row.ModuleIDs),
		"label_ids": stringsOrEmpty(row.LabelIDs), "assignee_ids": stringsOrEmpty(row.AssigneeIDs),
		"sub_issues_count": countOrZero(row.SubIssuesCount),
		"created_at":       row.CreatedAt, "updated_at": row.UpdatedAt,
		"created_by": row.CreatedByID, "updated_by": row.UpdatedByID,
		"attachment_count": countOrZero(row.AttachmentCount), "link_count": countOrZero(row.LinkCount),
		"is_draft": row.IsDraft, "archived_at": dateOnly(row.ArchivedAt),
		"description_html": row.DescriptionHTML,
	}
	if withSubscription {
		data["is_subscribed"] = row.IsSubscribed
	}
	return data
}

func stringsOrEmpty(values pq.StringArray) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func countOrZero(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

// dateOnly renders a DateField the way Django does, without a time part.
func dateOnly(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.Format("2006-01-02")
}

type issueInput struct {
	values       map[string]any
	assigneeIDs  []string
	labelIDs     []string
	hasAssignees bool
	hasLabels    bool
}

func (input issueInput) updates(now time.Time, actorID string) map[string]any {
	if len(input.values) == 0 {
		return map[string]any{}
	}
	updates := make(map[string]any, len(input.values)+2)
	for key, value := range input.values {
		updates[key] = value
	}
	// The serializer bumps updated_at even when only the related sets changed.
	updates["updated_at"] = now
	updates["updated_by_id"] = actorID
	return updates
}

// issueFields is IssueCreateSerializer's validation for the partial update path.
func (handler *Handler) issueFields(c *gin.Context, body map[string]json.RawMessage, projectID string) (issueInput, bool) {
	result := issueInput{values: map[string]any{}}
	ctx := c.Request.Context()

	if raw, exists := body["name"]; exists {
		value, ok := handler.stringField(c, "name", raw, 255)
		if !ok {
			return issueInput{}, false
		}
		result.values["name"] = value
	}
	if raw, exists := body["description_html"]; exists {
		if string(raw) == "null" {
			c.JSON(http.StatusBadRequest, gin.H{"description_html": []string{"This field may not be null."}})
			return issueInput{}, false
		}
		var value string
		if json.Unmarshal(raw, &value) != nil {
			c.JSON(http.StatusBadRequest, gin.H{"description_html": []string{"Not a valid string."}})
			return issueInput{}, false
		}
		if value != "" {
			valid, _, cleaned := htmlsanitizer.ValidateHTMLContent(value)
			if !valid {
				c.JSON(http.StatusBadRequest, gin.H{"error": "html content is not valid"})
				return issueInput{}, false
			}
			if cleaned != nil {
				value = *cleaned
			}
		}
		result.values["description_html"] = value
	}
	if raw, exists := body["description_json"]; exists {
		if string(raw) == "null" {
			c.JSON(http.StatusBadRequest, gin.H{"description_json": []string{"This field may not be null."}})
			return issueInput{}, false
		}
		if !json.Valid(raw) {
			c.JSON(http.StatusBadRequest, gin.H{"description_json": []string{"Value must be valid JSON."}})
			return issueInput{}, false
		}
		result.values["description_json"] = auth.JSONValue(append([]byte(nil), raw...))
	}
	if raw, exists := body["priority"]; exists {
		var value string
		if json.Unmarshal(raw, &value) != nil || !validIssuePriority(value) {
			var decoded any
			_ = json.Unmarshal(raw, &decoded)
			c.JSON(http.StatusBadRequest, gin.H{"priority": []string{`"` + stringify(decoded) + `" is not a valid choice.`}})
			return issueInput{}, false
		}
		result.values["priority"] = value
	}
	startDate, hasStart, ok := handler.issueDateField(c, body, "start_date")
	if !ok {
		return issueInput{}, false
	}
	targetDate, hasTarget, ok := handler.issueDateField(c, body, "target_date")
	if !ok {
		return issueInput{}, false
	}
	if hasStart {
		result.values["start_date"] = startDate
	}
	if hasTarget {
		result.values["target_date"] = targetDate
	}
	if startDate != nil && targetDate != nil && startDate.After(*targetDate) {
		// Django raises this as a non-field error, which DRF reports under
		// non_field_errors.
		c.JSON(http.StatusBadRequest, gin.H{"non_field_errors": []string{"Start date cannot exceed target date"}})
		return issueInput{}, false
	}
	if raw, exists := body["is_draft"]; exists {
		value, ok := handler.booleanField(c, "is_draft", raw)
		if !ok {
			return issueInput{}, false
		}
		result.values["is_draft"] = value
	}
	if raw, exists := body["sort_order"]; exists {
		var value float64
		if json.Unmarshal(raw, &value) != nil {
			c.JSON(http.StatusBadRequest, gin.H{"sort_order": []string{"A valid number is required."}})
			return issueInput{}, false
		}
		result.values["sort_order"] = value
	}

	// The related identifiers each have their own project scoped check.
	for _, relation := range []struct {
		field  string
		column string
		table  string
		scoped bool
		reason string
	}{
		{field: "state_id", column: "state_id", table: "states", scoped: true,
			reason: "State is not valid please pass a valid state_id"},
		{field: "parent_id", column: "parent_id", table: "issues", scoped: true,
			reason: "Parent is not valid issue_id please pass a valid issue_id"},
		{field: "estimate_point", column: "estimate_point_id", table: "estimate_points", scoped: true,
			reason: "Estimate point is not valid please pass a valid estimate_point_id"},
	} {
		raw, exists := body[relation.field]
		if !exists {
			continue
		}
		if string(raw) == "null" {
			result.values[relation.column] = nil
			continue
		}
		var value string
		if json.Unmarshal(raw, &value) != nil {
			c.JSON(http.StatusBadRequest, gin.H{relation.field: []string{`“` + value + `” is not a valid UUID.`}})
			return issueInput{}, false
		}
		canonical, valid := canonicalUUID(value)
		if !valid {
			c.JSON(http.StatusBadRequest, gin.H{relation.field: []string{`“` + value + `” is not a valid UUID.`}})
			return issueInput{}, false
		}
		belongs, err := handler.rowInProject(ctx, relation.table, canonical, projectID)
		if err != nil {
			handler.internalError(c, err)
			return issueInput{}, false
		}
		if !belongs {
			c.JSON(http.StatusBadRequest, gin.H{"non_field_errors": []string{relation.reason}})
			return issueInput{}, false
		}
		result.values[relation.column] = canonical
	}

	if raw, exists := body["assignee_ids"]; exists {
		result.hasAssignees = true
		requested, ok := handler.uuidListField(c, "assignee_ids", raw)
		if !ok {
			return issueInput{}, false
		}
		// Django narrows the list to project members at member level or above,
		// silently dropping anyone else rather than refusing the request.
		allowed, err := handler.projectMembersAtLeast(ctx, projectID, requested, roleMemberOrAbove)
		if err != nil {
			handler.internalError(c, err)
			return issueInput{}, false
		}
		result.assigneeIDs = allowed
	}
	if raw, exists := body["label_ids"]; exists {
		result.hasLabels = true
		requested, ok := handler.uuidListField(c, "label_ids", raw)
		if !ok {
			return issueInput{}, false
		}
		// Labels are narrowed to the project the same way.
		allowed, err := handler.labelsInProject(ctx, projectID, requested)
		if err != nil {
			handler.internalError(c, err)
			return issueInput{}, false
		}
		result.labelIDs = allowed
	}
	return result, true
}

func (handler *Handler) issueDateField(c *gin.Context, body map[string]json.RawMessage, field string) (*time.Time, bool, bool) {
	raw, exists := body[field]
	if !exists {
		return nil, false, true
	}
	if string(raw) == "null" {
		return nil, true, true
	}
	var value string
	if json.Unmarshal(raw, &value) != nil {
		c.JSON(http.StatusBadRequest, gin.H{field: []string{"Date has wrong format. Use one of these formats instead: YYYY-MM-DD."}})
		return nil, false, false
	}
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{field: []string{"Date has wrong format. Use one of these formats instead: YYYY-MM-DD."}})
		return nil, false, false
	}
	return &parsed, true, true
}

func (handler *Handler) uuidListField(c *gin.Context, field string, raw json.RawMessage) ([]string, bool) {
	var values []string
	if json.Unmarshal(raw, &values) != nil {
		c.JSON(http.StatusBadRequest, gin.H{field: []string{`Expected a list of items but got type "` + jsonTypeName(raw) + `".`}})
		return nil, false
	}
	canonicalValues := make([]string, 0, len(values))
	for _, value := range values {
		canonical, valid := canonicalUUID(value)
		if !valid {
			c.JSON(http.StatusBadRequest, gin.H{field: []string{`“` + value + `” is not a valid UUID.`}})
			return nil, false
		}
		canonicalValues = append(canonicalValues, canonical)
	}
	return canonicalValues, true
}

func (handler *Handler) rowInProject(ctx context.Context, table, id, projectID string) (bool, error) {
	var count int64
	err := handler.db.WithContext(ctx).Table(table).
		Where("id = ? AND project_id = ? AND deleted_at IS NULL", id, projectID).Count(&count).Error
	return count > 0, err
}

func (handler *Handler) projectMembersAtLeast(ctx context.Context, projectID string, memberIDs []string, role int) ([]string, error) {
	if len(memberIDs) == 0 {
		return []string{}, nil
	}
	allowed := make([]string, 0, len(memberIDs))
	err := handler.db.WithContext(ctx).Table("project_members").
		Where("project_id = ? AND member_id IN ? AND role >= ? AND is_active = TRUE AND deleted_at IS NULL",
			projectID, memberIDs, role).
		Pluck("member_id", &allowed).Error
	return allowed, err
}

func (handler *Handler) labelsInProject(ctx context.Context, projectID string, labelIDs []string) ([]string, error) {
	if len(labelIDs) == 0 {
		return []string{}, nil
	}
	allowed := make([]string, 0, len(labelIDs))
	err := handler.db.WithContext(ctx).Table("labels").
		Where("project_id = ? AND id IN ? AND deleted_at IS NULL", projectID, labelIDs).
		Pluck("id", &allowed).Error
	return allowed, err
}

func validIssuePriority(value string) bool {
	switch value {
	case "urgent", "high", "medium", "low", "none":
		return true
	}
	return false
}

func jsonTypeName(raw json.RawMessage) string {
	var decoded any
	if json.Unmarshal(raw, &decoded) != nil {
		return "str"
	}
	switch decoded.(type) {
	case map[string]any:
		return "dict"
	case string:
		return "str"
	case float64:
		return "int"
	case bool:
		return "bool"
	case nil:
		return "NoneType"
	}
	return "str"
}

func onConflictDoNothing() clause.OnConflict {
	return clause.OnConflict{DoNothing: true}
}
