package externalapi

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api/internal/auth"
	"github.com/yldm-tech/pace/apps/api/internal/drf"
	"gorm.io/gorm"
)

func (handler *Handler) registerCycleIssueRoutes(router gin.IRouter) {
	const base = "/api/v1/workspaces/:slug/projects/:project/cycles/:cycle/cycle-issues/"
	router.GET(base, handler.authenticated(handler.cycleIssueList))
	router.POST(base, handler.authenticated(handler.cycleIssueCreate))
	router.GET(base+":issue/", handler.authenticated(handler.cycleIssueRetrieve))
	router.DELETE(base+":issue/", handler.authenticated(handler.cycleIssueDestroy))
}

// CycleIssue is the link between a cycle and a work item.
type CycleIssue struct {
	ID          string     `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt   time.Time  `gorm:"column:created_at"`
	UpdatedAt   time.Time  `gorm:"column:updated_at"`
	CreatedByID *string    `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID *string    `gorm:"column:updated_by_id;type:uuid"`
	DeletedAt   *time.Time `gorm:"column:deleted_at"`
	ProjectID   string     `gorm:"column:project_id;type:uuid"`
	WorkspaceID string     `gorm:"column:workspace_id;type:uuid"`
	CycleID     string     `gorm:"column:cycle_id;type:uuid"`
	IssueID     string     `gorm:"column:issue_id;type:uuid"`
}

func (CycleIssue) TableName() string { return "cycle_issues" }

// cycleIssueRow is the link with the one annotation its serializer reports.
type cycleIssueRow struct {
	CycleIssue
	SubIssuesCount *int64 `gorm:"column:sub_issues_count"`
}

// cycleIssueList returns the **work items** in a cycle rather than the links, which is why its shape is the work item serializer and the detail's is the link serializer.
//
// It orders with a bare order_by, so `priority` sorts the words rather than the severities, and an ordering that reaches through a multi-valued relation repeats a work item once per related row.
func (handler *Handler) cycleIssueList(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireProjectMember(c, user, c.Request.Method) {
		return
	}
	order := plainIssueOrderBy(c.Query("order_by"))
	query := handler.db.WithContext(c.Request.Context()).Table("issues i").
		Select(externalIssueSelection()).
		Joins("JOIN workspaces w ON w.id = i.workspace_id").
		Joins("JOIN projects p ON p.id = i.project_id").
		Joins("LEFT JOIN states s ON s.id = i.state_id").
		Joins("JOIN cycle_issues ci ON ci.issue_id = i.id AND ci.cycle_id = ? AND ci.deleted_at IS NULL", c.Param("cycle")).
		Where("w.slug = ? AND i.project_id = ?", c.Param("slug"), c.Param("project")).
		Where(`i.deleted_at IS NULL AND s.group IS DISTINCT FROM 'triage'
			AND i.archived_at IS NULL AND p.archived_at IS NULL AND i.is_draft = FALSE`)
	for _, join := range order.Joins {
		query = query.Joins(join)
	}
	var rows []externalIssueRow
	if err := query.Order(order.Clause).Scan(&rows).Error; err != nil {
		handler.serverError(c, err)
		return
	}
	bodies, err := handler.issueBodies(c, rows)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	handler.respondPaged(c, bodies)
}

// cycleIssueRetrieve returns one link.
func (handler *Handler) cycleIssueRetrieve(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireProjectMember(c, user, c.Request.Method) {
		return
	}
	row, found, err := handler.cycleIssueLink(c)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !found {
		notFound(c)
		return
	}
	drf.Respond(c, http.StatusOK, narrow(cycleIssueJSON(row), requestedFields(c)))
}

// cycleIssueCreate puts work items into a cycle.
//
// A work item already in **another** cycle is moved rather than copied, since a work item belongs to at most one cycle at a time. The move writes only the cycle column, so no timestamp on the link changes.
func (handler *Handler) cycleIssueCreate(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireProjectMember(c, user, c.Request.Method) {
		return
	}
	var request struct {
		Issues []string `json:"issues"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		invalidPayload(c)
		return
	}
	if len(request.Issues) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Work items are required", "code": "MISSING_WORK_ITEMS"})
		return
	}
	slug, projectID, cycleID := c.Param("slug"), c.Param("project"), c.Param("cycle")
	cycle, found, err := handler.externalCycleByID(c, cycleID)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !found {
		notFound(c)
		return
	}
	now := handler.clock().UTC()
	if cycle.EndDate != nil && cycle.EndDate.Before(now) {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    "CYCLE_COMPLETED",
			"message": "The Cycle has already been completed so no new issues can be added",
		})
		return
	}

	// The links being moved are scoped to this workspace and project. Django's are not, which would let a caller pull another workspace's links into their own cycle by naming its work item ids — the same hole that was closed on the session API rather than one to carry over.
	var moving []CycleIssue
	err = handler.db.WithContext(c.Request.Context()).Table("cycle_issues ci").
		Joins("JOIN workspaces w ON w.id = ci.workspace_id").
		Where("ci.cycle_id <> ? AND ci.issue_id IN ? AND w.slug = ? AND ci.project_id = ? AND ci.deleted_at IS NULL",
			cycleID, request.Issues, slug, projectID).
		Find(&moving).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	linked := map[string]bool{}
	for _, link := range moving {
		linked[link.IssueID] = true
	}
	candidates := make([]string, 0, len(request.Issues))
	for _, issueID := range request.Issues {
		if !linked[issueID] {
			candidates = append(candidates, issueID)
		}
	}
	// Only work items the manager can see get a new link, so an archived, draft or triage one is dropped from the request rather than refusing it.
	fresh := []string{}
	if len(candidates) > 0 {
		err := handler.db.WithContext(c.Request.Context()).Table("issues i").
			Joins("JOIN workspaces w ON w.id = i.workspace_id").
			Joins("JOIN projects p ON p.id = i.project_id").
			Joins("LEFT JOIN states s ON s.id = i.state_id").
			Where("i.id IN ? AND w.slug = ? AND i.project_id = ?", candidates, slug, projectID).
			Where(`i.deleted_at IS NULL AND s.group IS DISTINCT FROM 'triage'
				AND i.archived_at IS NULL AND p.archived_at IS NULL AND i.is_draft = FALSE`).
			Pluck("i.id", &fresh).Error
		if err != nil {
			handler.serverError(c, err)
			return
		}
	}

	created := make([]CycleIssue, 0, len(fresh))
	for _, issueID := range fresh {
		linkID, err := newUUID()
		if err != nil {
			handler.serverError(c, err)
			return
		}
		// bulk_create goes around save(), so the link records nobody as its author.
		created = append(created, CycleIssue{
			ID: linkID, CreatedAt: now, UpdatedAt: now,
			ProjectID: projectID, WorkspaceID: cycle.WorkspaceID,
			CycleID: cycleID, IssueID: issueID,
		})
	}
	err = handler.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if len(created) > 0 {
			if err := tx.Clauses(onConflictDoNothing()).Create(&created).Error; err != nil {
				return err
			}
		}
		for _, link := range moving {
			// bulk_update writes only the cycle column, so no timestamp moves.
			if err := tx.Table("cycle_issues").Where("id = ?", link.ID).Update("cycle_id", cycleID).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if err := handler.publishCycleIssueActivity(c, user, request.Issues, cycleID, projectID, moving, created, now); err != nil {
		handler.serverError(c, err)
		return
	}

	rows, err := handler.cycleIssueLinks(c, user)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	results := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		results = append(results, cycleIssueJSON(row))
	}
	// The answer is every link in the cycle rather than the ones that were just written, and it is a 200 rather than a 201.
	drf.Respond(c, http.StatusOK, results)
}

// cycleIssueDestroy takes a work item out of a cycle. The activity is sent before the link goes, since the task reads the cycle by the id the request named rather than from the link.
func (handler *Handler) cycleIssueDestroy(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireProjectMember(c, user, c.Request.Method) {
		return
	}
	row, found, err := handler.cycleIssueLink(c)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !found {
		notFound(c)
		return
	}
	now := handler.clock().UTC()
	if handler.tasks != nil {
		requested, err := json.Marshal(map[string]any{
			"cycle_id": c.Param("cycle"), "issues": []string{row.IssueID},
		})
		if err != nil {
			handler.serverError(c, err)
			return
		}
		err = handler.tasks.PublishIssueActivity(c.Request.Context(), map[string]any{
			"type": "cycle.activity.deleted", "requested_data": string(requested),
			"actor_id": user.ID, "issue_id": row.IssueID, "project_id": c.Param("project"),
			"current_instance": nil, "epoch": now.Unix(),
		})
		if err != nil {
			handler.serverError(c, err)
			return
		}
	}
	err = handler.db.WithContext(c.Request.Context()).Table("cycle_issues").Where("id = ?", row.ID).
		Updates(map[string]any{"deleted_at": now, "updated_at": now}).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if handler.tasks != nil {
		if err := handler.tasks.PublishSoftDeleteRelatedObjects(c.Request.Context(), "db", "cycleissue", row.ID); err != nil {
			handler.serverError(c, err)
			return
		}
	}
	c.Status(http.StatusNoContent)
}

// cycleIssueLink reads one link, which the detail routes are scoped by the work item rather than by the link's own id.
func (handler *Handler) cycleIssueLink(c *gin.Context) (cycleIssueRow, bool, error) {
	var rows []cycleIssueRow
	err := handler.db.WithContext(c.Request.Context()).Table("cycle_issues ci").
		Select(cycleIssueSelection()).
		Joins("JOIN workspaces w ON w.id = ci.workspace_id").
		Where("w.slug = ? AND ci.project_id = ? AND ci.cycle_id = ? AND ci.issue_id = ? AND ci.deleted_at IS NULL",
			c.Param("slug"), c.Param("project"), c.Param("cycle"), c.Param("issue")).
		Limit(1).Scan(&rows).Error
	if err != nil || len(rows) == 0 {
		return cycleIssueRow{}, false, err
	}
	return rows[0], true, nil
}

// cycleIssueLinks reads every live link in the cycle, which is what the create answers with. The membership it joins is the **caller's**, so a link survives whether or not whoever made it is still in the project.
func (handler *Handler) cycleIssueLinks(c *gin.Context, user *auth.User) ([]cycleIssueRow, error) {
	var rows []cycleIssueRow
	err := handler.db.WithContext(c.Request.Context()).Table("cycle_issues ci").
		Select(cycleIssueSelection()).
		Joins("JOIN workspaces w ON w.id = ci.workspace_id").
		Joins("JOIN project_members pm ON pm.project_id = ci.project_id AND pm.member_id = ? AND pm.is_active = TRUE", user.ID).
		Where("w.slug = ? AND ci.project_id = ? AND ci.cycle_id = ? AND ci.deleted_at IS NULL",
			c.Param("slug"), c.Param("project"), c.Param("cycle")).
		Order("ci.created_at DESC").Scan(&rows).Error
	return rows, err
}

// cycleIssueSelection carries the one annotation the link serializer reports, which counts a work item's children through the manager rather than the plain table.
func cycleIssueSelection() string {
	return `ci.*,
		(SELECT COUNT(*) FROM issues si
			JOIN projects sp ON sp.id = si.project_id
			LEFT JOIN states ss ON ss.id = si.state_id
			WHERE si.parent_id = ci.issue_id AND si.deleted_at IS NULL AND si.archived_at IS NULL
			AND si.is_draft = FALSE AND sp.archived_at IS NULL
			AND ss.group IS DISTINCT FROM 'triage') AS sub_issues_count`
}

// cycleIssueJSON is CycleIssueSerializer: eleven fields, with the work item's child count beside them.
func cycleIssueJSON(row cycleIssueRow) gin.H {
	return gin.H{
		"id": row.ID, "sub_issues_count": row.SubIssuesCount,
		"created_at": row.CreatedAt, "updated_at": row.UpdatedAt, "deleted_at": row.DeletedAt,
		"created_by": row.CreatedByID, "updated_by": row.UpdatedByID,
		"project": row.ProjectID, "workspace": row.WorkspaceID,
		"issue": row.IssueID, "cycle": row.CycleID,
	}
}

// publishCycleIssueActivity sends what the task reads. created_cycle_issues is a JSON **string** inside the snapshot rather than a nested object, because Django builds it with serializers.serialize and then dumps the whole snapshot around it; the task calls json.loads on it.
func (handler *Handler) publishCycleIssueActivity(c *gin.Context, user *auth.User, requested []string, cycleID, projectID string, moving, created []CycleIssue, now time.Time) error {
	if handler.tasks == nil {
		return nil
	}
	updated := make([]map[string]any, 0, len(moving))
	for _, link := range moving {
		updated = append(updated, map[string]any{
			"old_cycle_id": link.CycleID, "new_cycle_id": cycleID, "issue_id": link.IssueID,
		})
	}
	records := make([]map[string]any, 0, len(created))
	for _, link := range created {
		records = append(records, map[string]any{
			"model": "db.cycleissue",
			"pk":    link.ID,
			"fields": map[string]any{
				"created_at": link.CreatedAt, "updated_at": link.UpdatedAt,
				"created_by": link.CreatedByID, "updated_by": link.UpdatedByID,
				"deleted_at": link.DeletedAt,
				"project":    link.ProjectID, "workspace": link.WorkspaceID,
				"issue": link.IssueID, "cycle": link.CycleID,
			},
		})
	}
	serialized, err := json.Marshal(records)
	if err != nil {
		return err
	}
	requestedData, err := json.Marshal(map[string]any{"cycles_list": requested})
	if err != nil {
		return err
	}
	snapshot, err := json.Marshal(map[string]any{
		"updated_cycle_issues": updated,
		"created_cycle_issues": string(serialized),
	})
	if err != nil {
		return err
	}
	return handler.tasks.PublishIssueActivity(c.Request.Context(), map[string]any{
		"type": "cycle.activity.created", "requested_data": string(requestedData),
		"actor_id": user.ID, "issue_id": nil, "project_id": projectID,
		"current_instance": string(snapshot), "epoch": now.Unix(),
		"notification": true, "origin": handler.origin(c),
	})
}
