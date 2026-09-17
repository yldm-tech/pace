package externalapi

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api/internal/auth"
	"github.com/yldm-tech/pace/apps/api/internal/drf"
)

func (handler *Handler) registerIssueRelationRoutes(router gin.IRouter) {
	// Relations are the one work item route mounted under **one** name only: the urls list it for work-items/ and not for issues/, so the older spelling answers 404 and nothing here may claim it.
	relations := "/api/v1/workspaces/:slug/projects/:project/work-items/:issue/relations/"
	router.GET(relations, handler.authenticated(handler.issueRelationList))
	router.POST(relations, handler.authenticated(handler.issueRelationCreate))
}

// IssueRelation is the db.IssueRelation table.
type IssueRelation struct {
	ID             string     `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt      time.Time  `gorm:"column:created_at"`
	UpdatedAt      time.Time  `gorm:"column:updated_at"`
	CreatedByID    *string    `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID    *string    `gorm:"column:updated_by_id;type:uuid"`
	DeletedAt      *time.Time `gorm:"column:deleted_at"`
	ProjectID      string     `gorm:"column:project_id;type:uuid"`
	WorkspaceID    string     `gorm:"column:workspace_id;type:uuid"`
	IssueID        string     `gorm:"column:issue_id;type:uuid"`
	RelatedIssueID string     `gorm:"column:related_issue_id;type:uuid"`
	RelationType   string     `gorm:"column:relation_type"`
}

func (IssueRelation) TableName() string { return "issue_relations" }

// storedRelationType is issue_relation_mapper.get_actual_relation.
//
// Only **three** kinds are stored: `blocked_by`, `start_before` and `finish_before`. Their opposites — blocking, start_after, finish_after — are the same rows read from the other end, which is why creating one of those writes the relation backwards. Anything the table does not name is stored as it was given.
func storedRelationType(requested string) string {
	switch requested {
	case "start_after":
		return "start_before"
	case "finish_after":
		return "finish_before"
	case "blocking", "blocked_by":
		return "blocked_by"
	case "start_before":
		return "start_before"
	case "finish_before":
		return "finish_before"
	case "implements", "implemented_by":
		return "implemented_by"
	}
	return requested
}

// reversedRelationTypes are the three that are written with the two work items the other way round.
var reversedRelationTypes = map[string]bool{"blocking": true, "start_after": true, "finish_after": true}

// relationBuckets are the eight keys the list always answers with, present even when empty.
var relationBuckets = []string{
	"blocking", "blocked_by", "duplicate", "relates_to",
	"start_after", "start_before", "finish_after", "finish_before",
}

// relationRow is one relation with both ends' projects read through their joins.
type relationRow struct {
	RelationType          string `gorm:"column:relation_type"`
	IssueID               string `gorm:"column:issue_id"`
	RelatedIssueID        string `gorm:"column:related_issue_id"`
	IssueProjectID        string `gorm:"column:issue_project_id"`
	RelatedIssueProjectID string `gorm:"column:related_issue_project_id"`
}

// issueRelationList reports a work item's relations grouped by kind.
//
// The query is **workspace-wide**: it is not narrowed to the project in the url, so a relation to a work item in another project of the same workspace is reported. And the three stored kinds each fill **two** buckets depending on which end the work item sits at.
func (handler *Handler) issueRelationList(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireProjectMember(c, user, http.MethodGet) {
		return
	}
	issueID := c.Param("issue")
	var rows []relationRow
	err := handler.db.WithContext(c.Request.Context()).Table("issue_relations r").
		Select(`r.relation_type, r.issue_id, r.related_issue_id,
			ri.project_id AS issue_project_id, rri.project_id AS related_issue_project_id`).
		Joins("JOIN workspaces w ON w.id = r.workspace_id").
		Joins("JOIN issues ri ON ri.id = r.issue_id").
		Joins("JOIN issues rri ON rri.id = r.related_issue_id").
		Where("w.slug = ? AND r.deleted_at IS NULL", c.Param("slug")).
		Where("r.issue_id = ? OR r.related_issue_id = ?", issueID, issueID).
		Scan(&rows).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, groupRelations(rows, issueID))
}

// groupRelations is the loop the endpoint ends with.
//
// Two kinds are **deduplicated and two are not**. A duplicate or relates_to relation is symmetric, so the same pair could be reported twice and a seen-set holds it back — but the set is keyed on the **other** work item alone, shared across both directions, so a work item related to the same other one under both a duplicate and a relates_to reports only the first. The four directional kinds have no such set, and a pair recorded twice is reported twice.
func groupRelations(rows []relationRow, issueID string) gin.H {
	grouped := gin.H{}
	buckets := map[string][]gin.H{}
	for _, bucket := range relationBuckets {
		buckets[bucket] = []gin.H{}
	}
	seenDuplicate := map[string]bool{}
	seenRelatesTo := map[string]bool{}
	entry := func(project, issue string) gin.H {
		return gin.H{"project_id": project, "issue_id": issue}
	}

	for _, row := range rows {
		switch row.RelationType {
		case "blocked_by":
			// The same row is a blocking from one end and a blocked_by from the other, and a self-relation fills both.
			if row.RelatedIssueID == issueID {
				buckets["blocking"] = append(buckets["blocking"], entry(row.IssueProjectID, row.IssueID))
			}
			if row.IssueID == issueID {
				buckets["blocked_by"] = append(buckets["blocked_by"], entry(row.RelatedIssueProjectID, row.RelatedIssueID))
			}
		case "duplicate":
			if row.IssueID == issueID && !seenDuplicate[row.RelatedIssueID] {
				seenDuplicate[row.RelatedIssueID] = true
				buckets["duplicate"] = append(buckets["duplicate"], entry(row.RelatedIssueProjectID, row.RelatedIssueID))
			}
			if row.RelatedIssueID == issueID && !seenDuplicate[row.IssueID] {
				seenDuplicate[row.IssueID] = true
				buckets["duplicate"] = append(buckets["duplicate"], entry(row.IssueProjectID, row.IssueID))
			}
		case "relates_to":
			if row.IssueID == issueID && !seenRelatesTo[row.RelatedIssueID] {
				seenRelatesTo[row.RelatedIssueID] = true
				buckets["relates_to"] = append(buckets["relates_to"], entry(row.RelatedIssueProjectID, row.RelatedIssueID))
			}
			if row.RelatedIssueID == issueID && !seenRelatesTo[row.IssueID] {
				seenRelatesTo[row.IssueID] = true
				buckets["relates_to"] = append(buckets["relates_to"], entry(row.IssueProjectID, row.IssueID))
			}
		case "start_before":
			if row.RelatedIssueID == issueID {
				buckets["start_after"] = append(buckets["start_after"], entry(row.IssueProjectID, row.IssueID))
			}
			if row.IssueID == issueID {
				buckets["start_before"] = append(buckets["start_before"], entry(row.RelatedIssueProjectID, row.RelatedIssueID))
			}
		case "finish_before":
			if row.RelatedIssueID == issueID {
				buckets["finish_after"] = append(buckets["finish_after"], entry(row.IssueProjectID, row.IssueID))
			}
			if row.IssueID == issueID {
				buckets["finish_before"] = append(buckets["finish_before"], entry(row.RelatedIssueProjectID, row.RelatedIssueID))
			}
		}
		// An implemented_by relation is stored and never reported: the loop has no branch for it.
	}
	for bucket, entries := range buckets {
		grouped[bucket] = entries
	}
	return grouped
}

// issueRelationCreate links work items together.
//
// The ids are narrowed to the **workspace** rather than to the project, so a relation may cross projects. A kind that reads backwards is written with the two ends swapped, which is why only three kinds are ever stored.
func (handler *Handler) issueRelationCreate(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireProjectMember(c, user, http.MethodPost) {
		return
	}
	slug, projectID, issueID := c.Param("slug"), c.Param("project"), c.Param("issue")
	var payload struct {
		RelationType string   `json:"relation_type"`
		Issues       []string `json:"issues"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		invalidPayload(c)
		return
	}
	if payload.RelationType == "" {
		c.JSON(http.StatusBadRequest, gin.H{"relation_type": []string{"This field is required."}})
		return
	}
	if len(payload.Issues) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"issues": []string{"This field is required."}})
		return
	}

	var project Project
	err := handler.db.WithContext(c.Request.Context()).Table("projects p").Select("p.*").
		Joins("JOIN workspaces w ON w.id = p.workspace_id").
		Where("w.slug = ? AND p.id = ?", slug, projectID).Take(&project).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}

	stored := storedRelationType(payload.RelationType)
	reversed := reversedRelationTypes[payload.RelationType]
	// The ids are narrowed through the issue_objects manager and to the workspace, not the project.
	var scoped []string
	err = handler.db.WithContext(c.Request.Context()).Table("issues i").
		Joins("JOIN workspaces w ON w.id = i.workspace_id").
		Joins("JOIN projects p ON p.id = i.project_id").
		Joins("LEFT JOIN states s ON s.id = i.state_id").
		Where("w.slug = ? AND i.id IN ?", slug, payload.Issues).
		Where(`i.deleted_at IS NULL AND i.archived_at IS NULL AND i.is_draft = FALSE
			AND p.archived_at IS NULL AND (s.group IS DISTINCT FROM 'triage')`).
		Pluck("i.id", &scoped).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}

	now := handler.clock().UTC()
	rows := make([]IssueRelation, 0, len(scoped))
	for _, other := range scoped {
		identifier, err := newUUID()
		if err != nil {
			handler.serverError(c, err)
			return
		}
		relation := IssueRelation{
			ID: identifier, CreatedAt: now, UpdatedAt: now, CreatedByID: &user.ID, UpdatedByID: &user.ID,
			ProjectID: projectID, WorkspaceID: project.WorkspaceID, RelationType: stored,
		}
		if reversed {
			relation.IssueID, relation.RelatedIssueID = other, issueID
		} else {
			relation.IssueID, relation.RelatedIssueID = issueID, other
		}
		rows = append(rows, relation)
	}
	if len(rows) > 0 {
		// A pair that already exists is skipped silently rather than refused.
		err = handler.db.WithContext(c.Request.Context()).
			Clauses(onConflictDoNothing()).Create(&rows).Error
		if err != nil {
			handler.serverError(c, err)
			return
		}
	}

	// The response is read back rather than built from what was written, and the two directions render through different serializers.
	var refetched []IssueRelation
	query := handler.db.WithContext(c.Request.Context()).Table("issue_relations r").Select("r.*").
		Joins("JOIN workspaces w ON w.id = r.workspace_id").
		Where("w.slug = ? AND r.relation_type = ? AND r.deleted_at IS NULL", slug, stored)
	if reversed {
		query = query.Where("r.related_issue_id = ? AND r.issue_id IN ?", issueID, scoped)
	} else {
		query = query.Where("r.issue_id = ? AND r.related_issue_id IN ?", issueID, scoped)
	}
	if len(scoped) > 0 {
		if err := query.Scan(&refetched).Error; err != nil {
			handler.serverError(c, err)
			return
		}
	}
	results := make([]gin.H, 0, len(refetched))
	for _, relation := range refetched {
		results = append(results, issueRelationJSON(relation, reversed))
	}
	drf.Respond(c, http.StatusCreated, results)
}

// issueRelationJSON is IssueRelationSerializer, or RelatedIssueSerializer when the relation was written backwards.
//
// The two differ in **which end they report**: the forward one names the related work item, the reverse one names the work item the relation was written from. So the id in the body is the other party either way.
func issueRelationJSON(relation IssueRelation, reversed bool) gin.H {
	other := relation.RelatedIssueID
	if reversed {
		other = relation.IssueID
	}
	return gin.H{
		"id": relation.ID, "created_at": relation.CreatedAt, "updated_at": relation.UpdatedAt,
		"created_by": relation.CreatedByID, "updated_by": relation.UpdatedByID, "deleted_at": relation.DeletedAt,
		"relation_type": relation.RelationType, "issue": other,
		"project": relation.ProjectID, "workspace": relation.WorkspaceID,
	}
}
