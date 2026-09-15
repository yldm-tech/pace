package externalapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"github.com/yldm-tech/pace/apps/api-go/internal/drf"
	"gorm.io/gorm"
)

func (handler *Handler) registerModuleIssueRoutes(router gin.IRouter) {
	const base = "/api/v1/workspaces/:slug/projects/:project/modules/:module/module-issues/"
	router.GET(base, handler.authenticated(handler.moduleIssueList))
	router.POST(base, handler.authenticated(handler.moduleIssueCreate))
	router.GET(base+":issue/", handler.authenticated(handler.moduleIssueRetrieve))
	router.DELETE(base+":issue/", handler.authenticated(handler.moduleIssueDestroy))
}

// ModuleIssue is the link between a module and a work item.
type ModuleIssue struct {
	ID          string     `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt   time.Time  `gorm:"column:created_at"`
	UpdatedAt   time.Time  `gorm:"column:updated_at"`
	CreatedByID *string    `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID *string    `gorm:"column:updated_by_id;type:uuid"`
	DeletedAt   *time.Time `gorm:"column:deleted_at"`
	ProjectID   string     `gorm:"column:project_id;type:uuid"`
	WorkspaceID string     `gorm:"column:workspace_id;type:uuid"`
	ModuleID    string     `gorm:"column:module_id;type:uuid"`
	IssueID     string     `gorm:"column:issue_id;type:uuid"`
}

func (ModuleIssue) TableName() string { return "module_issues" }

// moduleIssueRow is the link with the one annotation its serializer reports.
type moduleIssueRow struct {
	ModuleIssue
	SubIssuesCount *int64 `gorm:"column:sub_issues_count"`
}

// moduleIssueList returns the work items in a module, through the work item serializer rather than the link's.
func (handler *Handler) moduleIssueList(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireProjectMember(c, user, c.Request.Method) {
		return
	}
	handler.respondWithModuleIssues(c, "")
}

// moduleIssueRetrieve answers with **a page holding one work item** rather than with the link, which is where this module and the cycle's part company: there the detail is the link, here it is the work item and it is still paginated.
func (handler *Handler) moduleIssueRetrieve(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireProjectMember(c, user, c.Request.Method) {
		return
	}
	handler.respondWithModuleIssues(c, c.Param("issue"))
}

func (handler *Handler) respondWithModuleIssues(c *gin.Context, issueID string) {
	order := plainIssueOrderBy(c.Query("order_by"))
	query := handler.db.WithContext(c.Request.Context()).Table("issues i").
		Select(externalIssueSelection()).
		Joins("JOIN workspaces w ON w.id = i.workspace_id").
		Joins("JOIN projects p ON p.id = i.project_id").
		Joins("LEFT JOIN states s ON s.id = i.state_id").
		Joins("JOIN module_issues mi ON mi.issue_id = i.id AND mi.module_id = ? AND mi.deleted_at IS NULL", c.Param("module")).
		Where("w.slug = ? AND i.project_id = ?", c.Param("slug"), c.Param("project")).
		Where(`i.deleted_at IS NULL AND s.group IS DISTINCT FROM 'triage'
			AND i.archived_at IS NULL AND p.archived_at IS NULL AND i.is_draft = FALSE`)
	if issueID != "" {
		query = query.Where("i.id = ?", issueID)
	}
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

// moduleIssueCreate puts work items into a module.
//
// It only ever **adds**. The loop that is meant to move a work item out of another module compares a string against a queryset of UUIDs, which is never equal, so the branch that would move one is dead: every named work item takes the create path, and a work item already in another module ends up in both. The unique index is what keeps a work item already in *this* module from being added twice, since the insert ignores a conflict.
//
// The ids it acts on come from the plain manager rather than the work item one, so an archived, draft or triage work item can be put into a module here even though the module's own list will not show it afterwards.
func (handler *Handler) moduleIssueCreate(c *gin.Context, user *auth.User, _ APIToken) {
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
		c.JSON(http.StatusBadRequest, gin.H{"error": "Issues are required"})
		return
	}
	slug, projectID, moduleID := c.Param("slug"), c.Param("project"), c.Param("module")
	module, found, err := handler.externalModuleByIdentifier(c, moduleID)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !found {
		notFound(c)
		return
	}

	var known []string
	err = handler.db.WithContext(c.Request.Context()).Table("issues i").
		Joins("JOIN workspaces w ON w.id = i.workspace_id").
		Where("w.slug = ? AND i.project_id = ? AND i.id IN ? AND i.deleted_at IS NULL",
			slug, projectID, request.Issues).
		Order("i.created_at").Pluck("i.id", &known).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}

	now := handler.clock().UTC()
	created := make([]ModuleIssue, 0, len(known))
	for _, issueID := range known {
		linkID, err := newUUID()
		if err != nil {
			handler.serverError(c, err)
			return
		}
		// Unlike the cycle's, this bulk_create names the caller, because the objects are built with created_by set.
		created = append(created, ModuleIssue{
			ID: linkID, CreatedAt: now, UpdatedAt: now,
			CreatedByID: &user.ID, UpdatedByID: &user.ID,
			ProjectID: projectID, WorkspaceID: module.WorkspaceID,
			ModuleID: moduleID, IssueID: issueID,
		})
	}
	if len(created) > 0 {
		err := handler.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
			return tx.Clauses(onConflictDoNothing()).Create(&created).Error
		})
		if err != nil {
			handler.serverError(c, err)
			return
		}
	}
	if err := handler.publishModuleIssueActivity(c, user, known, moduleID, projectID, created, now); err != nil {
		handler.serverError(c, err)
		return
	}

	rows, err := handler.moduleIssueLinks(c, user)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	results := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		results = append(results, moduleIssueJSON(row))
	}
	// The answer is every link in the module rather than the ones that were just written.
	drf.Respond(c, http.StatusOK, results)
}

// moduleIssueDestroy takes a work item out of a module.
func (handler *Handler) moduleIssueDestroy(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireProjectMember(c, user, c.Request.Method) {
		return
	}
	row, found, err := handler.moduleIssueLink(c)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !found {
		notFound(c)
		return
	}
	module, _, err := handler.externalModuleByIdentifier(c, c.Param("module"))
	if err != nil {
		handler.serverError(c, err)
		return
	}
	now := handler.clock().UTC()
	err = handler.db.WithContext(c.Request.Context()).Table("module_issues").Where("id = ?", row.ID).
		Updates(map[string]any{"deleted_at": now, "updated_at": now}).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if handler.tasks != nil {
		requested, err := json.Marshal(map[string]any{
			"module_id": c.Param("module"), "issues": []string{row.IssueID},
		})
		if err != nil {
			handler.serverError(c, err)
			return
		}
		snapshot, err := json.Marshal(map[string]any{"module_name": module.Name})
		if err != nil {
			handler.serverError(c, err)
			return
		}
		// The delete names neither a notification nor an origin, where the create names an origin.
		err = handler.tasks.PublishIssueActivity(c.Request.Context(), map[string]any{
			"type": "module.activity.deleted", "requested_data": string(requested),
			"actor_id": user.ID, "issue_id": row.IssueID, "project_id": c.Param("project"),
			"current_instance": string(snapshot), "epoch": now.Unix(),
		})
		if err != nil {
			handler.serverError(c, err)
			return
		}
		if err := handler.tasks.PublishSoftDeleteRelatedObjects(c.Request.Context(), "db", "moduleissue", row.ID); err != nil {
			handler.serverError(c, err)
			return
		}
	}
	c.Status(http.StatusNoContent)
}

func (handler *Handler) moduleIssueLink(c *gin.Context) (moduleIssueRow, bool, error) {
	var rows []moduleIssueRow
	err := handler.db.WithContext(c.Request.Context()).Table("module_issues mi").
		Select(moduleIssueSelection()).
		Joins("JOIN workspaces w ON w.id = mi.workspace_id").
		Where("w.slug = ? AND mi.project_id = ? AND mi.module_id = ? AND mi.issue_id = ? AND mi.deleted_at IS NULL",
			c.Param("slug"), c.Param("project"), c.Param("module"), c.Param("issue")).
		Limit(1).Scan(&rows).Error
	if err != nil || len(rows) == 0 {
		return moduleIssueRow{}, false, err
	}
	return rows[0], true, nil
}

// moduleIssueLinks reads every live link in the module, which is what the create answers with. Its queryset asks for an unarchived project as well as for the caller's membership.
func (handler *Handler) moduleIssueLinks(c *gin.Context, user *auth.User) ([]moduleIssueRow, error) {
	var rows []moduleIssueRow
	err := handler.db.WithContext(c.Request.Context()).Table("module_issues mi").
		Select(moduleIssueSelection()).
		Joins("JOIN workspaces w ON w.id = mi.workspace_id").
		Joins("JOIN projects p ON p.id = mi.project_id AND p.archived_at IS NULL").
		Joins("JOIN project_members pm ON pm.project_id = mi.project_id AND pm.member_id = ? AND pm.is_active = TRUE", user.ID).
		Where("w.slug = ? AND mi.project_id = ? AND mi.module_id = ? AND mi.deleted_at IS NULL",
			c.Param("slug"), c.Param("project"), c.Param("module")).
		Order("mi.created_at DESC").Scan(&rows).Error
	return rows, err
}

func moduleIssueSelection() string {
	return `mi.*,
		(SELECT COUNT(*) FROM issues si
			JOIN projects sp ON sp.id = si.project_id
			LEFT JOIN states ss ON ss.id = si.state_id
			WHERE si.parent_id = mi.issue_id AND si.deleted_at IS NULL AND si.archived_at IS NULL
			AND si.is_draft = FALSE AND sp.archived_at IS NULL
			AND ss.group IS DISTINCT FROM 'triage') AS sub_issues_count`
}

// moduleIssueJSON is ModuleIssueSerializer: eleven fields, the same shape the cycle's link carries.
func moduleIssueJSON(row moduleIssueRow) gin.H {
	return gin.H{
		"id": row.ID, "sub_issues_count": row.SubIssuesCount,
		"created_at": row.CreatedAt, "updated_at": row.UpdatedAt, "deleted_at": row.DeletedAt,
		"created_by": row.CreatedByID, "updated_by": row.UpdatedByID,
		"project": row.ProjectID, "workspace": row.WorkspaceID,
		"issue": row.IssueID, "module": row.ModuleID,
	}
}

// publishModuleIssueActivity sends what the task reads.
//
// requested_data carries the **repr of a queryset** rather than a list, because Django builds it with str() over one: a payload reading `<QuerySet [UUID('…')]>`, truncated after twenty entries the way Python truncates one. Nothing parses it, and reproducing it is cheaper than explaining a difference in a log.
func (handler *Handler) publishModuleIssueActivity(c *gin.Context, user *auth.User, known []string, moduleID, projectID string, created []ModuleIssue, now time.Time) error {
	if handler.tasks == nil {
		return nil
	}
	records := make([]map[string]any, 0, len(created))
	for _, link := range created {
		records = append(records, map[string]any{
			"model": "db.moduleissue",
			"pk":    link.ID,
			"fields": map[string]any{
				"created_at": link.CreatedAt, "updated_at": link.UpdatedAt,
				"created_by": link.CreatedByID, "updated_by": link.UpdatedByID,
				"deleted_at": link.DeletedAt,
				"project":    link.ProjectID, "workspace": link.WorkspaceID,
				"module": link.ModuleID, "issue": link.IssueID,
			},
		})
	}
	serialized, err := json.Marshal(records)
	if err != nil {
		return err
	}
	requested, err := json.Marshal(map[string]any{"modules_list": querysetRepr(known)})
	if err != nil {
		return err
	}
	snapshot, err := json.Marshal(map[string]any{
		// Nothing is ever moved, so this list is always empty.
		"updated_module_issues": []any{},
		"created_module_issues": string(serialized),
	})
	if err != nil {
		return err
	}
	return handler.tasks.PublishIssueActivity(c.Request.Context(), map[string]any{
		"type": "module.activity.created", "requested_data": string(requested),
		"actor_id": user.ID, "issue_id": nil, "project_id": projectID,
		"current_instance": string(snapshot), "epoch": now.Unix(),
		"origin": handler.origin(c),
	})
}

// querysetRepr is what Python prints for a queryset of identifiers, which is what this payload carries in place of a list. A queryset prints at most twenty entries and then says how it ended.
func querysetRepr(values []string) string {
	const limit = 20
	rendered := make([]string, 0, limit+1)
	for index, value := range values {
		if index == limit {
			rendered = append(rendered, "'...(remaining elements truncated)...'")
			break
		}
		rendered = append(rendered, "UUID('"+value+"')")
	}
	return "<QuerySet [" + strings.Join(rendered, ", ") + "]>"
}
