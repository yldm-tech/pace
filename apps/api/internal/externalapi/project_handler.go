package externalapi

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api/internal/auth"
	"github.com/yldm-tech/pace/apps/api/internal/drf"
	"gorm.io/gorm"
)

func (handler *Handler) registerProjectRoutes(router gin.IRouter) {
	handler.registerProjectListCreateRoutes(router)
	router.GET("/api/v1/workspaces/:slug/projects-lite/", handler.authenticated(handler.projectLiteList))
	router.POST("/api/v1/workspaces/:slug/projects/:project/archive/", handler.authenticated(handler.projectArchive))
	router.DELETE("/api/v1/workspaces/:slug/projects/:project/archive/", handler.authenticated(handler.projectUnarchive))
	router.GET("/api/v1/workspaces/:slug/projects/:project/summary/", handler.authenticated(handler.projectSummary))
}

// Project is the db.Project table as the external API's lite serializer sees it.
type Project struct {
	ID          string     `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt   time.Time  `gorm:"column:created_at"`
	UpdatedAt   time.Time  `gorm:"column:updated_at"`
	DeletedAt   *time.Time `gorm:"column:deleted_at"`
	WorkspaceID string     `gorm:"column:workspace_id;type:uuid"`
	Name        string     `gorm:"column:name"`
	Identifier  string     `gorm:"column:identifier"`
	Description string     `gorm:"column:description"`
	Network     int        `gorm:"column:network"`
	CoverImage  *string    `gorm:"column:cover_image"`
	CoverAsset  *string    `gorm:"column:cover_image_asset_id;type:uuid"`
	IconProp    []byte     `gorm:"column:icon_prop;type:jsonb"`
	Emoji       *string    `gorm:"column:emoji"`
	ArchivedAt  *time.Time `gorm:"column:archived_at"`
	IntakeView  bool       `gorm:"column:intake_view"`
	// The rest of the columns the full serializer reports.
	DescriptionText      []byte  `gorm:"column:description_text;type:jsonb"`
	DescriptionHTML      []byte  `gorm:"column:description_html;type:jsonb"`
	ModuleViewOn         bool    `gorm:"column:module_view"`
	IssueViewsView       bool    `gorm:"column:issue_views_view"`
	PageView             bool    `gorm:"column:page_view"`
	TimeTrackingEnabled  bool    `gorm:"column:is_time_tracking_enabled"`
	IssueTypeEnabled     bool    `gorm:"column:is_issue_type_enabled"`
	GuestViewAllFeatures bool    `gorm:"column:guest_view_all_features"`
	ArchiveIn            int     `gorm:"column:archive_in"`
	CloseIn              int     `gorm:"column:close_in"`
	LogoProps            []byte  `gorm:"column:logo_props;type:jsonb"`
	ExternalSource       *string `gorm:"column:external_source"`
	ExternalID           *string `gorm:"column:external_id"`
	CreatedByID          *string `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID          *string `gorm:"column:updated_by_id;type:uuid"`
	ProjectLeadID        *string `gorm:"column:project_lead_id;type:uuid"`
	EstimateID           *string `gorm:"column:estimate_id;type:uuid"`
	DefaultStateID       *string `gorm:"column:default_state_id;type:uuid"`
	// DefaultAssigneeID is who a work item goes to when its creator names nobody.
	DefaultAssigneeID *string `gorm:"column:default_assignee_id;type:uuid"`
	// CycleView is the switch a project turns cycles off with, which the cycle serializer refuses to write against.
	CycleView bool `gorm:"column:cycle_view"`
	// ModuleView is the same switch for modules, which the module serializer refuses to write against.
	ModuleView bool   `gorm:"column:module_view"`
	Timezone   string `gorm:"column:timezone"`
}

func (Project) TableName() string { return "projects" }

// projectOrderByAllowlist is PROJECT_ORDER_BY_ALLOWLIST.
var projectOrderByAllowlist = map[string]string{
	"created_at": "created_at", "updated_at": "updated_at", "name": "name", "network": "network", "sort_order": "sort_order",
}

// projectLiteList is the picker list: the projects the caller is an active member of, plus every public one.
//
// A public project is offered whether or not the caller is in it, which is the one place in this API where membership is not required to see something.
func (handler *Handler) projectLiteList(c *gin.Context, user *auth.User, _ APIToken) {
	slug := c.Param("slug")
	var workspaces int64
	err := handler.db.WithContext(c.Request.Context()).Table("workspaces").
		Where("slug = ?", slug).Count(&workspaces).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if workspaces == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Provided workspace does not exist"})
		return
	}

	query := handler.db.WithContext(c.Request.Context()).Table("projects p").
		Joins("JOIN workspaces w ON w.id = p.workspace_id").
		Where("w.slug = ? AND p.deleted_at IS NULL", slug).
		Where(`EXISTS (SELECT 1 FROM project_members pm WHERE pm.project_id = p.id
			AND pm.member_id = ? AND pm.is_active = TRUE) OR p.network = 2`, user.ID)
	// The switch reads "true" or "1" and nothing else, and it lowercases first, so "TRUE" works and "yes" does not.
	included := strings.ToLower(c.DefaultQuery("include_archived", "false"))
	if included != "true" && included != "1" {
		query = query.Where("p.archived_at IS NULL")
	}

	order := sanitizeOrderBy(c.Query("order_by"), projectOrderByAllowlist, "-created_at")
	if strings.TrimPrefix(order, "-") == "sort_order" {
		// The lite list has no sort_order of its own to order by, since it never joins the membership that carries one. Postgres refuses the column, so the request fails rather than falling back.
		handler.serverError(c, errNoSortOrderOnTheLiteList)
		return
	}

	var projects []Project
	if err := query.Select("p.*").Distinct().Order(orderClause("p.", order)).Scan(&projects).Error; err != nil {
		handler.serverError(c, err)
		return
	}
	results := make([]gin.H, 0, len(projects))
	for _, project := range projects {
		results = append(results, projectLiteJSON(project))
	}
	handler.respondPaged(c, results)
}

// errNoSortOrderOnTheLiteList names the failure a caller gets for asking the lite list to order by a column it does not select.
var errNoSortOrderOnTheLiteList = &orderError{"the lite list has no sort_order column to order by"}

type orderError struct{ message string }

func (err *orderError) Error() string { return err.message }

// sanitizeOrderBy is plane.utils.order_queryset.sanitize_order_by: at most one leading dash is stripped, and anything the allowlist does not name falls back.
// orderClause turns what sanitizeOrderBy returns into SQL. Its result is Django's ordering syntax, where a leading minus means descending, and concatenating that after a table alias produces `p.-created_at` -- which Postgres rejects with `syntax error at or near "-"`.
func orderClause(alias, order string) string {
	column := strings.TrimPrefix(order, "-")
	if strings.HasPrefix(order, "-") {
		return alias + column + " DESC"
	}
	return alias + column + " ASC"
}

// sanitizeOrderBy maps the order_by query parameter onto the column it is allowed to produce.
//
// The return value is read out of the allowlist rather than built from the parameter, so the string that reaches the query is always one this file wrote. That is what makes it safe, and it is also what makes the safety visible: nothing derived from the request survives the lookup.
func sanitizeOrderBy(value string, allowed map[string]string, fallback string) string {
	if value == "" {
		return fallback
	}
	descending := strings.HasPrefix(value, "-")
	bare := strings.TrimPrefix(value, "-")
	column, permitted := allowed[bare]
	if !permitted || strings.HasPrefix(bare, "-") {
		return fallback
	}
	if descending {
		return "-" + column
	}
	return column
}

// projectArchive hides a project from the active lists and clears every member's favourite of it.
//
// The favourite clear is by **project** rather than by entity, so it takes the favourites of everything inside the project with it — a favourited cycle in an archived project stops being favourited too.
func (handler *Handler) projectArchive(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireWorkspaceAdmin(c, user) {
		return
	}
	project, found, err := handler.projectByID(c)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !found {
		notFound(c)
		return
	}
	now := handler.clock().UTC()
	err = handler.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		err := tx.Table("projects").Where("id = ?", project.ID).
			Updates(map[string]any{"archived_at": now, "updated_at": now}).Error
		if err != nil {
			return err
		}
		return tx.Table("user_favorites").
			Where(`project_id = ? AND deleted_at IS NULL
				AND workspace_id = (SELECT id FROM workspaces WHERE slug = ?)`, project.ID, c.Param("slug")).
			Update("deleted_at", now).Error
	})
	if err != nil {
		handler.serverError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// projectUnarchive brings a project back. It does **not** restore the favourites the archive cleared.
func (handler *Handler) projectUnarchive(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireWorkspaceAdmin(c, user) {
		return
	}
	project, found, err := handler.projectByID(c)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !found {
		notFound(c)
		return
	}
	now := handler.clock().UTC()
	err = handler.db.WithContext(c.Request.Context()).Table("projects").Where("id = ?", project.ID).
		Updates(map[string]any{"archived_at": nil, "updated_at": now}).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// projectSummaryFields are the eight things a summary can count.
var projectSummaryFields = []string{"members", "states", "labels", "cycles", "modules", "issues", "intakes", "pages"}

// projectSummary counts what a project holds, computing only what was asked for.
//
// Asking for nothing valid asks for **all eight**, the same way the workspace project stats endpoint reads its own field list.
func (handler *Handler) projectSummary(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireWorkspaceAdmin(c, user) {
		return
	}
	project, found, err := handler.projectByID(c)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "Project not found"})
		return
	}

	known := map[string]bool{}
	for _, field := range projectSummaryFields {
		known[field] = true
	}
	requested := []string{}
	seen := map[string]bool{}
	for _, raw := range strings.Split(c.Query("fields"), ",") {
		raw = strings.TrimSpace(raw)
		if !known[raw] {
			continue
		}
		// Collect the name as this file spells it rather than as the request spelled it, so the string that ends up in the SELECT is one of ours even though the two compare equal.
		for _, field := range projectSummaryFields {
			if field == raw && !seen[field] {
				requested = append(requested, field)
				seen[field] = true
			}
		}
	}
	if len(requested) == 0 {
		requested = append(requested, projectSummaryFields...)
	}
	sort.Strings(requested)

	selection := make([]string, 0, len(requested))
	for _, field := range requested {
		selection = append(selection, projectSummaryColumn(field)+" AS "+field)
	}
	var row struct {
		Members int64 `gorm:"column:members"`
		States  int64 `gorm:"column:states"`
		Labels  int64 `gorm:"column:labels"`
		Cycles  int64 `gorm:"column:cycles"`
		Modules int64 `gorm:"column:modules"`
		Issues  int64 `gorm:"column:issues"`
		Intakes int64 `gorm:"column:intakes"`
		Pages   int64 `gorm:"column:pages"`
	}
	err = handler.db.WithContext(c.Request.Context()).Table("projects p").
		Where("p.id = ?", project.ID).Select(strings.Join(selection, ", ")).Scan(&row).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	values := map[string]int64{
		"members": row.Members, "states": row.States, "labels": row.Labels, "cycles": row.Cycles,
		"modules": row.Modules, "issues": row.Issues, "intakes": row.Intakes, "pages": row.Pages,
	}
	counts := gin.H{}
	for _, field := range requested {
		counts[field] = values[field]
	}
	drf.Respond(c, http.StatusOK, gin.H{
		"id": project.ID, "name": project.Name, "identifier": project.Identifier, "counts": counts,
	})
}

// projectSummaryColumn is the subquery each field counts with.
//
// None of them go through a manager: each counts rows of its own table, soft-deleted ones included, because the subqueries are built from the plain default managers and those only filter the model they are asked about. The issue count is the one exception and it excludes **triage** alone — not archived issues, not drafts.
func projectSummaryColumn(field string) string {
	switch field {
	case "members":
		return `(SELECT COUNT(*) FROM project_members sm WHERE sm.project_id = p.id)`
	case "states":
		return `(SELECT COUNT(*) FROM states ss WHERE ss.project_id = p.id)`
	case "labels":
		return `(SELECT COUNT(*) FROM labels sl WHERE sl.project_id = p.id)`
	case "cycles":
		return `(SELECT COUNT(*) FROM cycles sc WHERE sc.project_id = p.id)`
	case "modules":
		return `(SELECT COUNT(*) FROM modules smo WHERE smo.project_id = p.id)`
	case "issues":
		return `(SELECT COUNT(*) FROM issues si
			WHERE si.project_id = p.id AND si.deleted_at IS NULL
			AND (SELECT sst.group FROM states sst WHERE sst.id = si.state_id) IS DISTINCT FROM 'triage')`
	case "intakes":
		return `(SELECT COUNT(*) FROM intake_issues sii WHERE sii.project_id = p.id)`
	case "pages":
		return `(SELECT COUNT(*) FROM project_pages spp WHERE spp.project_id = p.id)`
	}
	return "0"
}

// requireWorkspaceAdmin is WorkSpaceAdminPermission, which the archive and the summary carry rather than the project permission the rest of this app uses.
func (handler *Handler) requireWorkspaceAdmin(c *gin.Context, user *auth.User) bool {
	var admins int64
	err := handler.db.WithContext(c.Request.Context()).Table("workspace_members wm").
		Joins("JOIN workspaces w ON w.id = wm.workspace_id").
		Where("w.slug = ? AND wm.member_id = ? AND wm.role = ? AND wm.is_active = TRUE",
			c.Param("slug"), user.ID, roleAdmin).
		Count(&admins).Error
	if err != nil {
		handler.serverError(c, err)
		return false
	}
	if admins == 0 {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
			"detail": "You do not have permission to perform this action.",
		})
		return false
	}
	return true
}

func (handler *Handler) projectByID(c *gin.Context) (Project, bool, error) {
	var projects []Project
	err := handler.db.WithContext(c.Request.Context()).Table("projects p").Select("p.*").
		Joins("JOIN workspaces w ON w.id = p.workspace_id").
		Where("w.slug = ? AND p.id = ? AND p.deleted_at IS NULL", c.Param("slug"), c.Param("project")).
		Limit(1).Scan(&projects).Error
	if err != nil {
		return Project{}, false, err
	}
	if len(projects) == 0 {
		return Project{}, false, nil
	}
	return projects[0], true, nil
}

// projectLiteJSON is the external API's ProjectLiteSerializer: nine fields and nothing else.
func projectLiteJSON(project Project) gin.H {
	return gin.H{
		"id": project.ID, "identifier": project.Identifier, "name": project.Name,
		"cover_image": project.CoverImage, "icon_prop": decodeJSON(project.IconProp),
		"emoji": project.Emoji, "description": project.Description,
		"cover_image_url": coverImageURL(project), "archived_at": project.ArchivedAt,
	}
}

// coverImageURL prefers the uploaded asset over the stored url.
func coverImageURL(project Project) any {
	if project.CoverAsset != nil {
		return "/api/assets/v2/static/" + *project.CoverAsset + "/"
	}
	if project.CoverImage != nil && *project.CoverImage != "" {
		return *project.CoverImage
	}
	return nil
}
