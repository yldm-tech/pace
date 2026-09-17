package project

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
	"github.com/yldm-tech/pace/apps/api/internal/auth"
	"github.com/yldm-tech/pace/apps/api/internal/drf"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (handler *Handler) registerIssueRelationRoutes(router gin.IRouter) {
	router.GET("/api/workspaces/:slug/projects/:id/issues/:issue/issue-relation/", handler.authenticatedIssueUUID(handler.issueRelationList))
	router.POST("/api/workspaces/:slug/projects/:id/issues/:issue/issue-relation/", handler.authenticatedIssueUUID(handler.issueRelationCreate))
	router.POST("/api/workspaces/:slug/projects/:id/issues/:issue/remove-relation/", handler.authenticatedIssueUUID(handler.issueRelationRemove))
}

// relatedIssueRow is the values() projection the list returns. It carries the issue's own audit columns, not the relation's.
type relatedIssueRow struct {
	ID          string         `gorm:"column:id"`
	Name        string         `gorm:"column:name"`
	StateID     *string        `gorm:"column:state_id"`
	SortOrder   float64        `gorm:"column:sort_order"`
	Priority    string         `gorm:"column:priority"`
	SequenceID  int            `gorm:"column:sequence_id"`
	ProjectID   string         `gorm:"column:project_id"`
	LabelIDs    pq.StringArray `gorm:"column:label_ids;type:uuid[]"`
	AssigneeIDs pq.StringArray `gorm:"column:assignee_ids;type:uuid[]"`
	CreatedAt   time.Time      `gorm:"column:created_at"`
	UpdatedAt   time.Time      `gorm:"column:updated_at"`
	CreatedByID *string        `gorm:"column:created_by"`
	UpdatedByID *string        `gorm:"column:updated_by"`
}

// relationBucket names one key of the response and the relation rows behind it. reversed says which column of a relation row points at the issue to return: the far end of a relation the caller's issue owns, or the near end of one that points back at it.
//
// duplicate and relates_to are symmetric, so each is read from both ends. Django unions those two readings into one query and applies DISTINCT, so an issue related from both ends appears once; two separate queries would list it twice.
type relationBucket struct {
	key      string
	stored   string
	reversed []bool
}

var relationBuckets = []relationBucket{
	{key: "blocking", stored: "blocked_by", reversed: []bool{true}},
	{key: "blocked_by", stored: "blocked_by", reversed: []bool{false}},
	{key: "duplicate", stored: "duplicate", reversed: []bool{false, true}},
	{key: "relates_to", stored: "relates_to", reversed: []bool{false, true}},
	{key: "start_after", stored: "start_before", reversed: []bool{true}},
	{key: "start_before", stored: "start_before", reversed: []bool{false}},
	{key: "finish_after", stored: "finish_before", reversed: []bool{true}},
	{key: "finish_before", stored: "finish_before", reversed: []bool{false}},
}

func (handler *Handler) issueRelationList(c *gin.Context, user *auth.User) {
	if !handler.requireProjectMembership(c, user) {
		return
	}
	slug, issueID := c.Param("slug"), c.Param("issue")
	response := gin.H{}
	for _, bucket := range relationBuckets {
		rows, err := handler.relatedIssues(c.Request.Context(), slug, issueID, bucket)
		if err != nil {
			handler.internalError(c, err)
			return
		}
		serialized := make([]gin.H, 0, len(rows))
		for _, row := range rows {
			serialized = append(serialized, relatedIssueJSON(row, bucket.key))
		}
		response[bucket.key] = serialized
	}
	drf.Respond(c, http.StatusOK, response)
}

// relatedIssues reads one bucket. The issue queryset is scoped to the workspace and not to the project on purpose: a relation may cross projects, and Django scopes only the relation rows.
//
// The result carries no ordering. Django's own ordering is on the relation queryset that feeds the subquery, not on the issues, and the grouped values() query drops the model's default ordering, so Postgres returns these rows in whatever order it likes on both sides.
func (handler *Handler) relatedIssues(ctx context.Context, slug, issueID string, bucket relationBucket) ([]relatedIssueRow, error) {
	// Each direction contributes one subquery; the outer IN matches an issue that either of them names.
	clauses := ""
	arguments := []any{}
	for _, reversed := range bucket.reversed {
		returned, matched := "r.related_issue_id", "r.issue_id"
		if reversed {
			returned, matched = "r.issue_id", "r.related_issue_id"
		}
		if clauses != "" {
			clauses += " OR "
		}
		clauses += "i.id IN (SELECT " + returned + ` FROM issue_relations r
			JOIN workspaces rw ON rw.id = r.workspace_id
			WHERE r.deleted_at IS NULL AND rw.slug = ? AND ` + matched + " = ? AND r.relation_type = ?)"
		arguments = append(arguments, slug, issueID, bucket.stored)
	}

	var rows []relatedIssueRow
	err := handler.db.WithContext(ctx).Table("issues i").
		Select(`i.id, i.name, i.state_id, i.sort_order, i.priority, i.sequence_id, i.project_id,
			COALESCE(ARRAY_AGG(DISTINCT il.label_id) FILTER (WHERE il.label_id IS NOT NULL AND il.deleted_at IS NULL), '{}') AS label_ids,
			COALESCE(ARRAY_AGG(DISTINCT ia.assignee_id) FILTER (WHERE ia.assignee_id IS NOT NULL AND pm.is_active AND ia.deleted_at IS NULL), '{}') AS assignee_ids,
			i.created_at, i.updated_at, i.created_by_id AS created_by, i.updated_by_id AS updated_by`).
		Joins("JOIN workspaces w ON w.id = i.workspace_id").
		// The three joins the array aggregates filter over. project_members carries no deleted_at predicate, because Django's join does not: a model's default manager never filters a join traversed through it.
		Joins("LEFT JOIN issue_labels il ON il.issue_id = i.id").
		Joins("LEFT JOIN issue_assignees ia ON ia.issue_id = i.id").
		Joins("LEFT JOIN project_members pm ON pm.member_id = ia.assignee_id").
		Where("w.slug = ?", slug).
		Where(issueObjectsPredicate("i")).
		Where("("+clauses+")", arguments...).
		Group("i.id").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// relatedIssueJSON is the values() projection plus the relation_type literal the queryset annotates.
func relatedIssueJSON(row relatedIssueRow, relationType string) gin.H {
	return gin.H{
		"id": row.ID, "name": row.Name, "state_id": row.StateID, "sort_order": row.SortOrder,
		"priority": row.Priority, "sequence_id": row.SequenceID, "project_id": row.ProjectID,
		"label_ids": stringsOrEmpty(row.LabelIDs), "assignee_ids": stringsOrEmpty(row.AssigneeIDs),
		"created_at": row.CreatedAt, "updated_at": row.UpdatedAt,
		"created_by": row.CreatedByID, "updated_by": row.UpdatedByID,
		"relation_type": relationType,
	}
}

func (handler *Handler) issueRelationCreate(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember) {
		return
	}
	slug, projectID, issueID := c.Param("slug"), c.Param("id"), c.Param("issue")
	var body map[string]json.RawMessage
	if err := c.ShouldBindJSON(&body); err != nil {
		handler.invalidDetail(c)
		return
	}
	var relationType string
	if raw, present := body["relation_type"]; !present || json.Unmarshal(raw, &relationType) != nil || relationType == "" {
		// Django checks only for the key being absent or null, and its message says message rather than error.
		c.JSON(http.StatusBadRequest, gin.H{"message": "Issue relation type is required"})
		return
	}
	var requestedIssues []string
	if raw, present := body["issues"]; present {
		if err := json.Unmarshal(raw, &requestedIssues); err != nil {
			handler.invalidDetail(c)
			return
		}
	}

	var project Project
	err := handler.db.WithContext(c.Request.Context()).Where("id = ?", projectID).Take(&project).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// Django's unguarded Project.objects.get raises DoesNotExist, which BaseAPIView turns into this 404.
		c.JSON(http.StatusNotFound, gin.H{"error": "The required object does not exist."})
		return
	}
	if err != nil {
		handler.internalError(c, err)
		return
	}

	// The ids are narrowed to the workspace, not the project, because a relation may legitimately cross projects; the workspace scope is what stops a cross-tenant reference.
	scoped := []string{}
	if len(requestedIssues) > 0 {
		err = handler.db.WithContext(c.Request.Context()).Table("issues i").
			Where("i.id IN ? AND i.workspace_id = (SELECT id FROM workspaces WHERE slug = ?)", requestedIssues, slug).
			Where(issueObjectsPredicate("i")).Pluck("i.id", &scoped).Error
		if err != nil {
			handler.internalError(c, err)
			return
		}
	}

	now := handler.clock().UTC()
	stored := storedRelationType(relationType)
	reversed := relationIsReversed(relationType)
	relations := make([]IssueRelation, 0, len(scoped))
	for _, otherID := range scoped {
		owner, related := issueID, otherID
		if reversed {
			owner, related = otherID, issueID
		}
		id, err := newUUID()
		if err != nil {
			handler.internalError(c, err)
			return
		}
		relations = append(relations, IssueRelation{
			ID: id, CreatedAt: now, UpdatedAt: now, CreatedByID: &user.ID, UpdatedByID: &user.ID,
			ProjectID: projectID, WorkspaceID: project.WorkspaceID,
			IssueID: owner, RelatedIssueID: related, RelationType: stored,
		})
	}
	if len(relations) > 0 {
		// bulk_create(ignore_conflicts=True): a pair that already exists is skipped rather than raising, and the object is still returned.
		err := handler.db.WithContext(c.Request.Context()).Clauses(clause.OnConflict{DoNothing: true}).Create(&relations).Error
		if err != nil {
			handler.internalError(c, err)
			return
		}
	}

	requested, err := json.Marshal(decodeRawFields(body))
	if err != nil {
		handler.internalError(c, err)
		return
	}
	requestedData := string(requested)
	if err := handler.publishIssueActivity(c, issueActivity{
		Type: "issue_relation.activity.created", RequestedData: &requestedData,
		ActorID: user.ID, IssueID: issueID, ProjectID: projectID,
		Notification: true, Origin: handler.origin(), Epoch: now,
	}); err != nil {
		handler.internalError(c, err)
		return
	}

	serialized, err := handler.relationSerializerData(c.Request.Context(), relations, reversed)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusCreated, serialized)
}

func (handler *Handler) issueRelationRemove(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember) {
		return
	}
	slug, projectID, issueID := c.Param("slug"), c.Param("id"), c.Param("issue")
	var body map[string]json.RawMessage
	if err := c.ShouldBindJSON(&body); err != nil {
		handler.invalidDetail(c)
		return
	}
	var relatedIssue string
	if raw, present := body["related_issue"]; present {
		_ = json.Unmarshal(raw, &relatedIssue)
	}

	// Either direction of the pair is removed, which is what lets the caller pass the id it sees regardless of which end stored the row.
	var relation IssueRelation
	err := handler.db.WithContext(c.Request.Context()).
		Where("workspace_id = (SELECT id FROM workspaces WHERE slug = ?) AND deleted_at IS NULL", slug).
		Where("(issue_id = ? AND related_issue_id = ?) OR (issue_id = ? AND related_issue_id = ?)",
			relatedIssue, issueID, issueID, relatedIssue).
		Take(&relation).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// Django calls .delete() on the None that .first() returned, so a pair that is not there is a 500 rather than a 404. Reproduced rather than corrected, since a client cannot tell the two apart and turning it into a 404 would change a status code the frontend may branch on.
		handler.internalError(c, errors.New("issue relation not found"))
		return
	}
	if err != nil {
		handler.internalError(c, err)
		return
	}

	snapshot, err := json.Marshal(issueRelationJSON(relation, relation.RelatedIssueID))
	if err != nil {
		handler.internalError(c, err)
		return
	}
	currentInstance := string(snapshot)

	now := handler.clock().UTC()
	err = handler.db.WithContext(c.Request.Context()).Model(&IssueRelation{}).
		Where("id = ?", relation.ID).
		// Model.delete stamps deleted_at and saves, and the save bumps updated_at.
		Updates(map[string]any{"deleted_at": now, "updated_at": now}).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if handler.tasks != nil {
		err := handler.tasks.PublishSoftDeleteRelatedObjects(c.Request.Context(), "db", "issuerelation", relation.ID)
		if err != nil {
			handler.internalError(c, err)
			return
		}
	}

	requested, err := json.Marshal(decodeRawFields(body))
	if err != nil {
		handler.internalError(c, err)
		return
	}
	requestedData := string(requested)
	if err := handler.publishIssueActivity(c, issueActivity{
		Type: "issue_relation.activity.deleted", RequestedData: &requestedData, CurrentInstance: &currentInstance,
		ActorID: user.ID, IssueID: issueID, ProjectID: projectID,
		Notification: true, Origin: handler.origin(), Epoch: now,
	}); err != nil {
		handler.internalError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// relationSerializerData is IssueRelationSerializer, or RelatedIssueSerializer when the request was the reversed reading. The two differ only in which end of the relation they read the issue's fields from, so one function covers both.
//
// Both declare assignee_ids write_only, so it never reaches the response.
func (handler *Handler) relationSerializerData(ctx context.Context, relations []IssueRelation, reversed bool) ([]gin.H, error) {
	serialized := make([]gin.H, 0, len(relations))
	for _, relation := range relations {
		// RelatedIssueSerializer sources from issue, IssueRelationSerializer from related_issue.
		subject := relation.RelatedIssueID
		if reversed {
			subject = relation.IssueID
		}
		serialized = append(serialized, issueRelationJSON(relation, subject))
	}
	if err := handler.fillRelationIssueFields(ctx, serialized); err != nil {
		return nil, err
	}
	return serialized, nil
}

// issueRelationJSON lays out the audit fields the two relation serializers share, which are the relation's own. The five fields they reach through the foreign key are filled in afterwards.
//
// relation_type is the stored type, so a request that asked for blocking gets blocked_by back.
func issueRelationJSON(relation IssueRelation, subjectIssueID string) gin.H {
	return gin.H{
		"id": subjectIssueID, "relation_type": relation.RelationType,
		"created_by": relation.CreatedByID, "updated_by": relation.UpdatedByID,
		"created_at": relation.CreatedAt, "updated_at": relation.UpdatedAt,
	}
}

// fillRelationIssueFields resolves the five fields the serializers reach through the relation's foreign key. Django follows that key per object; one query does here.
//
// state_id is the one that can go missing. Its source is related_issue.state.id, and DRF's attribute walk stops at the null state and raises SkipField on a field that is not required, so an issue with no state has no state_id key at all rather than a null one. The other four are one level deep and always present.
func (handler *Handler) fillRelationIssueFields(ctx context.Context, serialized []gin.H) error {
	if len(serialized) == 0 {
		return nil
	}
	identifiers := make([]string, 0, len(serialized))
	for _, item := range serialized {
		identifiers = append(identifiers, item["id"].(string))
	}
	var rows []struct {
		ID         string  `gorm:"column:id"`
		ProjectID  string  `gorm:"column:project_id"`
		SequenceID int     `gorm:"column:sequence_id"`
		Name       string  `gorm:"column:name"`
		StateID    *string `gorm:"column:state_id"`
		Priority   string  `gorm:"column:priority"`
	}
	// Django follows the foreign key through the model's base manager, which carries no soft-delete filter, so a soft-deleted issue is still resolved here.
	err := handler.db.WithContext(ctx).Table("issues").
		Select("id, project_id, sequence_id, name, state_id, priority").
		Where("id IN ?", identifiers).Scan(&rows).Error
	if err != nil {
		return err
	}
	byID := make(map[string]int, len(rows))
	for index, row := range rows {
		byID[row.ID] = index
	}
	for _, item := range serialized {
		index, found := byID[item["id"].(string)]
		if !found {
			continue
		}
		row := rows[index]
		item["project_id"] = row.ProjectID
		item["sequence_id"] = row.SequenceID
		item["name"] = row.Name
		item["priority"] = row.Priority
		if row.StateID != nil {
			item["state_id"] = *row.StateID
		}
	}
	return nil
}
