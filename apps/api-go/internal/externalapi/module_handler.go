package externalapi

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"gorm.io/gorm"
)

func (handler *Handler) registerModuleRoutes(router gin.IRouter) {
	base := "/api/v1/workspaces/:slug/projects/:project/"
	router.GET(base+"modules-lite/", handler.authenticated(handler.moduleLiteList))
	router.GET(base+"archived-modules/", handler.authenticated(handler.archivedModuleList))
	// The same class is mounted at both paths and dispatches on the method name, so both handlers answer on both.
	for _, path := range []string{"modules/:module/archive/", "archived-modules/:module/unarchive/"} {
		router.POST(base+path, handler.authenticated(handler.moduleArchive))
		router.DELETE(base+path, handler.authenticated(handler.moduleUnarchive))
	}
}

// Module is the db.Module table as the external API sees it.
type Module struct {
	ID              string     `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt       time.Time  `gorm:"column:created_at"`
	UpdatedAt       time.Time  `gorm:"column:updated_at"`
	CreatedByID     *string    `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID     *string    `gorm:"column:updated_by_id;type:uuid"`
	DeletedAt       *time.Time `gorm:"column:deleted_at"`
	ProjectID       string     `gorm:"column:project_id;type:uuid"`
	WorkspaceID     string     `gorm:"column:workspace_id;type:uuid"`
	Name            string     `gorm:"column:name"`
	Description     string     `gorm:"column:description"`
	DescriptionText []byte     `gorm:"column:description_text;type:jsonb"`
	DescriptionHTML []byte     `gorm:"column:description_html;type:jsonb"`
	StartDate       *time.Time `gorm:"column:start_date"`
	TargetDate      *time.Time `gorm:"column:target_date"`
	Status          string     `gorm:"column:status"`
	LeadID          *string    `gorm:"column:lead_id;type:uuid"`
	ViewProps       []byte     `gorm:"column:view_props;type:jsonb"`
	SortOrder       float64    `gorm:"column:sort_order"`
	ExternalSource  *string    `gorm:"column:external_source"`
	ExternalID      *string    `gorm:"column:external_id"`
	ArchivedAt      *time.Time `gorm:"column:archived_at"`
	LogoProps       []byte     `gorm:"column:logo_props;type:jsonb"`
}

func (Module) TableName() string { return "modules" }

// moduleRow is the module with the six counts the full serializer reports.
type moduleRow struct {
	Module
	TotalIssues     int64          `gorm:"column:total_issues"`
	CancelledIssues int64          `gorm:"column:cancelled_issues"`
	CompletedIssues int64          `gorm:"column:completed_issues"`
	StartedIssues   int64          `gorm:"column:started_issues"`
	UnstartedIssues int64          `gorm:"column:unstarted_issues"`
	BacklogIssues   int64          `gorm:"column:backlog_issues"`
	MemberIDs       pq.StringArray `gorm:"column:member_ids;type:uuid[]"`
}

// moduleLiteList is the picker: the project's live modules with no counts.
//
// Like its cycle twin, the order parameter is accepted and ignored — the `sanitize_order_by` call omits the allowlist, so the empty default rejects every field.
func (handler *Handler) moduleLiteList(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireProjectMember(c, user, http.MethodGet) {
		return
	}
	var modules []Module
	err := handler.db.WithContext(c.Request.Context()).Table("modules m").Select("m.*").
		Joins("JOIN workspaces w ON w.id = m.workspace_id").
		Where("w.slug = ? AND m.project_id = ? AND m.archived_at IS NULL AND m.deleted_at IS NULL",
			c.Param("slug"), c.Param("project")).
		Order("m.created_at DESC").Scan(&modules).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	results := make([]gin.H, 0, len(modules))
	for _, module := range modules {
		results = append(results, moduleJSON(moduleRow{Module: module}, false))
	}
	handler.respondPaged(c, results)
}

// archivedModuleList returns the project's archived modules with their counts.
//
// Unlike the cycle one it does not check membership at all: the queryset filters the workspace and the project and nothing else, leaving the permission class to do the whole job.
func (handler *Handler) archivedModuleList(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireProjectMember(c, user, http.MethodGet) {
		return
	}
	var rows []moduleRow
	err := handler.db.WithContext(c.Request.Context()).Table("modules m").
		Select(externalModuleAnnotations()).
		Joins("JOIN workspaces w ON w.id = m.workspace_id").
		Where("w.slug = ? AND m.project_id = ? AND m.archived_at IS NOT NULL AND m.deleted_at IS NULL",
			c.Param("slug"), c.Param("project")).
		Order("m.created_at DESC").Scan(&rows).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	fields := requestedFields(c)
	results := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		results = append(results, narrow(moduleJSON(row, true), fields))
	}
	handler.respondPaged(c, results)
}

// externalModuleAnnotations is the six counts and the member list the archived module list carries.
//
// Every count is over **distinct** issues and requires a live link, so an issue linked twice counts once and a removed one not at all.
func externalModuleAnnotations() string {
	const liveIssues = `mi.deleted_at IS NULL AND mis.archived_at IS NULL AND mis.is_draft = FALSE AND mis.deleted_at IS NULL`
	stateCount := func(group string) string {
		return `COALESCE((SELECT COUNT(DISTINCT mis.id) FROM module_issues mi
			JOIN issues mis ON mis.id = mi.issue_id
			JOIN states ms ON ms.id = mis.state_id AND ms.group = '` + group + `'
			WHERE mi.module_id = m.id AND ` + liveIssues + `), 0)`
	}
	return `m.*,
		COALESCE((SELECT COUNT(DISTINCT mis.id) FROM module_issues mi
			JOIN issues mis ON mis.id = mi.issue_id
			WHERE mi.module_id = m.id AND ` + liveIssues + `), 0) AS total_issues,
		` + stateCount("cancelled") + ` AS cancelled_issues,
		` + stateCount("completed") + ` AS completed_issues,
		` + stateCount("started") + ` AS started_issues,
		` + stateCount("unstarted") + ` AS unstarted_issues,
		` + stateCount("backlog") + ` AS backlog_issues,
		COALESCE((SELECT ARRAY_AGG(DISTINCT mm.member_id) FROM module_members mm
			WHERE mm.module_id = m.id AND mm.deleted_at IS NULL), '{}') AS member_ids`
}

// moduleArchive puts a finished module away.
//
// A module is finished by its **status** where a cycle is finished by its end date — the same asymmetry the session API has, one app over.
func (handler *Handler) moduleArchive(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireProjectMember(c, user, http.MethodPost) {
		return
	}
	module, found, err := handler.externalModuleByID(c)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !found {
		notFound(c)
		return
	}
	if module.Status != "completed" && module.Status != "cancelled" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Only completed or cancelled modules can be archived"})
		return
	}
	now := handler.clock().UTC()
	err = handler.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		err := tx.Table("modules").Where("id = ?", module.ID).
			Updates(map[string]any{"archived_at": now, "updated_at": now}).Error
		if err != nil {
			return err
		}
		return tx.Table("user_favorites").
			Where(`entity_type = 'module' AND entity_identifier = ? AND project_id = ? AND deleted_at IS NULL
				AND workspace_id = (SELECT id FROM workspaces WHERE slug = ?)`,
				module.ID, c.Param("project"), c.Param("slug")).
			Update("deleted_at", now).Error
	})
	if err != nil {
		handler.serverError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (handler *Handler) moduleUnarchive(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireProjectMember(c, user, http.MethodDelete) {
		return
	}
	module, found, err := handler.externalModuleByID(c)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !found {
		notFound(c)
		return
	}
	now := handler.clock().UTC()
	err = handler.db.WithContext(c.Request.Context()).Table("modules").Where("id = ?", module.ID).
		Updates(map[string]any{"archived_at": nil, "updated_at": now}).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (handler *Handler) externalModuleByID(c *gin.Context) (Module, bool, error) {
	var modules []Module
	err := handler.db.WithContext(c.Request.Context()).Table("modules m").Select("m.*").
		Joins("JOIN workspaces w ON w.id = m.workspace_id").
		Where("w.slug = ? AND m.project_id = ? AND m.id = ? AND m.deleted_at IS NULL",
			c.Param("slug"), c.Param("project"), c.Param("module")).
		Limit(1).Scan(&modules).Error
	if err != nil || len(modules) == 0 {
		return Module{}, false, err
	}
	return modules[0], true, nil
}

// moduleJSON is the external API's ModuleSerializer, or the lite one when the counts are left off.
//
// The members field is **write only** on the serializer, so neither shape reports who is on the module — the archived list annotates the ids and then does not render them.
func moduleJSON(row moduleRow, counted bool) gin.H {
	data := gin.H{
		"id": row.ID, "created_at": row.CreatedAt, "updated_at": row.UpdatedAt,
		"created_by": row.CreatedByID, "updated_by": row.UpdatedByID, "deleted_at": row.DeletedAt,
		"name": row.Name, "description": row.Description,
		"description_text": decodeJSON(row.DescriptionText), "description_html": decodeJSON(row.DescriptionHTML),
		"start_date": row.StartDate, "target_date": row.TargetDate, "status": row.Status,
		"view_props": decodeJSON(row.ViewProps), "sort_order": row.SortOrder,
		"external_source": row.ExternalSource, "external_id": row.ExternalID,
		"archived_at": row.ArchivedAt, "logo_props": decodeJSON(row.LogoProps),
		"project": row.ProjectID, "workspace": row.WorkspaceID, "lead": row.LeadID,
	}
	if counted {
		data["total_issues"] = row.TotalIssues
		data["cancelled_issues"] = row.CancelledIssues
		data["completed_issues"] = row.CompletedIssues
		data["started_issues"] = row.StartedIssues
		data["unstarted_issues"] = row.UnstartedIssues
		data["backlog_issues"] = row.BacklogIssues
	}
	return data
}
