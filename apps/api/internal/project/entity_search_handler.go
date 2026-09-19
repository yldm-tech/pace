package project

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api/internal/access"
	"github.com/yldm-tech/pace/apps/api/internal/auth"
	"github.com/yldm-tech/pace/apps/api/internal/drf"
	"gorm.io/gorm"
)

func (handler *Handler) registerEntitySearchRoutes(router gin.IRouter) {
	router.GET("/api/workspaces/:slug/entity-search/", handler.authenticated(handler.entitySearch))
}

// entitySearch is what the editor's mention menu and the link pickers call.
//
// It is the global search's smaller sibling and answers a different shape: each requested type is capped at a **count** the caller chooses, and the results carry the fields a menu row needs rather than the ones a search page does. Everything is duplicated across a project-scoped and a workspace-scoped branch, and the two are not quite the same search.
func (handler *Handler) entitySearch(c *gin.Context, user *auth.User) {
	if !handler.requireWorkspaceRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	slug := c.Param("slug")
	query := c.Query("query")
	projectID := c.Query("project_id")

	// The count is parsed with int(), so a value that is not a number raises rather than falling back.
	count := 5
	if raw := c.Query("count"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			handler.internalError(c, errors.New("entity search: the count is not a number"))
			return
		}
		count = parsed
	}

	types := []string{}
	for _, name := range strings.Split(c.DefaultQuery("query_type", "user_mention"), ",") {
		types = append(types, strings.TrimSpace(name))
	}

	results := gin.H{}
	for _, name := range types {
		rows, handled, err := handler.searchEntityType(c, user, name, query, slug, projectID, count)
		if err != nil {
			handler.internalError(c, err)
			return
		}
		// A type nobody knows writes no key at all, so the caller gets a body without it rather than an empty list.
		if handled {
			results[name] = rows
		}
	}
	drf.Respond(c, http.StatusOK, results)
}

func (handler *Handler) searchEntityType(c *gin.Context, user *auth.User, name, query, slug, projectID string, count int) ([]gin.H, bool, error) {
	switch name {
	case "user_mention":
		rows, err := handler.searchMentionableUsers(c, query, slug, projectID, count)
		return rows, true, err
	case "project":
		rows, err := handler.searchMentionableProjects(c, user, query, slug, count)
		return rows, true, err
	case "issue":
		rows, err := handler.searchMentionableIssues(c, user, query, slug, projectID, count)
		return rows, true, err
	case "cycle":
		rows, err := handler.searchMentionableCycles(c, user, query, slug, projectID, count)
		return rows, true, err
	case "module":
		rows, err := handler.searchMentionableModules(c, user, query, slug, projectID, count)
		return rows, true, err
	case "page":
		rows, err := handler.searchMentionablePages(c, user, query, slug, projectID, count)
		return rows, true, err
	}
	return nil, false, nil
}

// searchMentionableUsers is the only search whose **source table** changes with the scope: inside a project it reads the project's members, and outside it the workspace's.
func (handler *Handler) searchMentionableUsers(c *gin.Context, query, slug, projectID string, count int) ([]gin.H, error) {
	table, alias := "workspace_members", "wm"
	if projectID != "" {
		table, alias = "project_members", "pm"
	}
	database := handler.db.WithContext(c.Request.Context()).Table(table+" "+alias).
		Joins("JOIN workspaces w ON w.id = "+alias+".workspace_id").
		Joins("JOIN users u ON u.id = "+alias+".member_id").
		Where("w.slug = ? AND "+alias+".is_active = TRUE AND u.is_bot = FALSE AND "+alias+".deleted_at IS NULL", slug)
	if projectID != "" {
		database = database.Where(alias+".project_id = ?", projectID)
	}
	if query != "" {
		database = database.Where("u.first_name ILIKE ? OR u.last_name ILIKE ? OR u.display_name ILIKE ?",
			"%"+query+"%", "%"+query+"%", "%"+query+"%")
	}
	// The project branch is made distinct and the workspace one is not, which is upstream's asymmetry rather than a decision.
	if projectID != "" {
		database = database.Distinct()
	}

	var rows []struct {
		AvatarURL   *string `gorm:"column:avatar_url"`
		DisplayName string  `gorm:"column:display_name"`
		MemberID    string  `gorm:"column:member_id"`
	}
	err := database.Select(`CASE WHEN u.avatar_asset_id IS NOT NULL
			THEN '/api/assets/v2/static/' || CAST(u.avatar_asset_id AS TEXT) || '/'
			ELSE u.avatar END AS avatar_url,
		u.display_name, u.id AS member_id, ` + alias + `.created_at`).
		Order(alias + ".created_at DESC").Limit(count).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	results := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		results = append(results, gin.H{
			"member__avatar_url": row.AvatarURL, "member__display_name": row.DisplayName, "member__id": row.MemberID,
		})
	}
	return results, nil
}

// searchMentionableProjects is the one search that does not narrow by scope at all: it is the same query in both branches, and it offers a **public** project whether or not the caller is in it.
func (handler *Handler) searchMentionableProjects(c *gin.Context, user *auth.User, query, slug string, count int) ([]gin.H, error) {
	database := handler.db.WithContext(c.Request.Context()).Table("projects p").
		Joins("JOIN workspaces w ON w.id = p.workspace_id").
		Where("w.slug = ? AND p.deleted_at IS NULL", slug).
		// The membership is not required to be active here, unlike everywhere else.
		Where(`EXISTS (SELECT 1 FROM project_members pm WHERE pm.project_id = p.id AND pm.member_id = ?) OR p.network = 2`, user.ID)
	if query != "" {
		database = database.Where("p.name ILIKE ? OR p.identifier ILIKE ?", "%"+query+"%", "%"+query+"%")
	}
	var rows []struct {
		Name       string         `gorm:"column:name"`
		ID         string         `gorm:"column:id"`
		Identifier string         `gorm:"column:identifier"`
		LogoProps  auth.JSONValue `gorm:"column:logo_props"`
		Slug       string         `gorm:"column:workspace_slug"`
	}
	err := database.Distinct().
		Select("p.name, p.id, p.identifier, p.logo_props, w.slug AS workspace_slug, p.created_at").
		Order("p.created_at DESC").Limit(count).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	results := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		results = append(results, gin.H{
			"name": row.Name, "id": row.ID, "identifier": row.Identifier,
			"logo_props": decodeJSON(row.LogoProps), "workspace__slug": row.Slug,
		})
	}
	return results, nil
}

// searchMentionableIssues projects what a mention row needs: the priority, the state and the work item type, rather than the workspace slug the other searches carry.
func (handler *Handler) searchMentionableIssues(c *gin.Context, user *auth.User, query, slug, projectID string, count int) ([]gin.H, error) {
	database := handler.memberIssueScopeAnyProject(c, user, slug).Where(issueObjectsPredicate("i"))
	if query != "" {
		database = applyIssueSearch(database, query)
	}
	if projectID != "" {
		database = database.Where("i.project_id = ?", projectID)
	}
	var rows []struct {
		Name              string  `gorm:"column:name"`
		ID                string  `gorm:"column:id"`
		SequenceID        int64   `gorm:"column:sequence_id"`
		ProjectIdentifier string  `gorm:"column:project_identifier"`
		ProjectID         string  `gorm:"column:project_id"`
		Priority          string  `gorm:"column:priority"`
		StateID           *string `gorm:"column:state_id"`
		TypeID            *string `gorm:"column:type_id"`
	}
	err := database.Distinct().Select(`i.name, i.id, i.sequence_id, p.identifier AS project_identifier,
		i.project_id, i.priority, i.state_id, i.type_id, i.created_at`).
		Order("i.created_at DESC").Limit(count).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	results := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		results = append(results, gin.H{
			"name": row.Name, "id": row.ID, "sequence_id": row.SequenceID,
			"project__identifier": row.ProjectIdentifier, "project_id": row.ProjectID,
			"priority": row.Priority, "state_id": row.StateID, "type_id": row.TypeID,
		})
	}
	return results, nil
}

// memberIssueScopeAnyProject is the issue join without the archived-project filter the global search carries, which this endpoint does not have.
func (handler *Handler) memberIssueScopeAnyProject(c *gin.Context, user *auth.User, slug string) *gorm.DB {
	return handler.db.WithContext(c.Request.Context()).Table("issues i").
		Joins("JOIN workspaces w ON w.id = i.workspace_id").
		Joins("JOIN projects p ON p.id = i.project_id").
		Joins(access.MemberJoin("i", "project_id"), user.ID).
		Where("w.slug = ?", slug)
}

// searchMentionableCycles carries the derived status, computed the same way the cycle list computes it.
func (handler *Handler) searchMentionableCycles(c *gin.Context, user *auth.User, query, slug, projectID string, count int) ([]gin.H, error) {
	now := handler.clock().UTC()
	database := handler.projectScopedTable(c, user, "cycles", slug, projectID)
	if query != "" {
		database = database.Where("t.name ILIKE ?", "%"+query+"%")
	}
	var rows []struct {
		Name              string `gorm:"column:name"`
		ID                string `gorm:"column:id"`
		ProjectID         string `gorm:"column:project_id"`
		ProjectIdentifier string `gorm:"column:project_identifier"`
		Status            string `gorm:"column:status"`
		WorkspaceSlug     string `gorm:"column:workspace_slug"`
	}
	err := database.Distinct().Select(`t.name, t.id, t.project_id, p.identifier AS project_identifier,
		CASE
			WHEN t.start_date <= ? AND t.end_date >= ? THEN 'CURRENT'
			WHEN t.start_date > ? THEN 'UPCOMING'
			WHEN t.end_date < ? THEN 'COMPLETED'
			ELSE 'DRAFT'
		END AS status,
		w.slug AS workspace_slug, t.created_at`, now, now, now, now).
		Order("t.created_at DESC").Limit(count).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return namedSearchResults(rows, func(index int) gin.H {
		row := rows[index]
		return gin.H{
			"name": row.Name, "id": row.ID, "project_id": row.ProjectID,
			"project__identifier": row.ProjectIdentifier, "status": row.Status,
			"workspace__slug": row.WorkspaceSlug,
		}
	}), nil
}

// searchMentionableModules carries the status column the model has, which is a stored value rather than a derived one.
func (handler *Handler) searchMentionableModules(c *gin.Context, user *auth.User, query, slug, projectID string, count int) ([]gin.H, error) {
	database := handler.projectScopedTable(c, user, "modules", slug, projectID)
	if query != "" {
		database = database.Where("t.name ILIKE ?", "%"+query+"%")
	}
	var rows []struct {
		Name              string `gorm:"column:name"`
		ID                string `gorm:"column:id"`
		ProjectID         string `gorm:"column:project_id"`
		ProjectIdentifier string `gorm:"column:project_identifier"`
		Status            string `gorm:"column:status"`
		WorkspaceSlug     string `gorm:"column:workspace_slug"`
	}
	err := database.Distinct().Select(`t.name, t.id, t.project_id, p.identifier AS project_identifier,
		t.status, w.slug AS workspace_slug, t.created_at`).
		Order("t.created_at DESC").Limit(count).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return namedSearchResults(rows, func(index int) gin.H {
		row := rows[index]
		return gin.H{
			"name": row.Name, "id": row.ID, "project_id": row.ProjectID,
			"project__identifier": row.ProjectIdentifier, "status": row.Status,
			"workspace__slug": row.WorkspaceSlug,
		}
	}), nil
}

// projectScopedTable is the join the cycle and module searches share.
func (handler *Handler) projectScopedTable(c *gin.Context, user *auth.User, table, slug, projectID string) *gorm.DB {
	database := handler.db.WithContext(c.Request.Context()).Table(table+" t").
		Joins("JOIN workspaces w ON w.id = t.workspace_id").
		Joins("JOIN projects p ON p.id = t.project_id").
		Joins(access.MemberJoin("t", "project_id"), user.ID).
		Where("w.slug = ? AND t.deleted_at IS NULL", slug)
	if projectID != "" {
		database = database.Where("t.project_id = ?", projectID)
	}
	return database
}

// searchMentionablePages offers **public** pages only, and outside a project only pages marked global.
func (handler *Handler) searchMentionablePages(c *gin.Context, user *auth.User, query, slug, projectID string, count int) ([]gin.H, error) {
	database := handler.db.WithContext(c.Request.Context()).Table("pages pg").
		Joins("JOIN workspaces w ON w.id = pg.workspace_id").
		Joins("JOIN project_pages pp ON pp.page_id = pg.id").
		Joins("JOIN projects p ON p.id = pp.project_id").
		Joins(access.MemberJoin("pp", "project_id"), user.ID).
		Where("w.slug = ? AND pg.deleted_at IS NULL AND pg.access = ?", slug, pagePublicAccess)
	if projectID != "" {
		database = database.Where("pp.project_id = ?", projectID)
	} else {
		// Outside a project only a global page is offered, which is the one filter the two branches do not share.
		database = database.Where("pg.is_global = TRUE")
	}
	if query != "" {
		database = database.Where("pg.name ILIKE ?", "%"+query+"%")
	}
	var rows []struct {
		Name          string         `gorm:"column:name"`
		ID            string         `gorm:"column:id"`
		LogoProps     auth.JSONValue `gorm:"column:logo_props"`
		ProjectID     string         `gorm:"column:joined_project_id"`
		WorkspaceSlug string         `gorm:"column:workspace_slug"`
	}
	// The project id comes from the join rather than from the page, so a page in several projects appears once per project.
	err := database.Distinct().Select(`pg.name, pg.id, pg.logo_props, pp.project_id AS joined_project_id,
		w.slug AS workspace_slug, pg.created_at`).
		Order("pg.created_at DESC").Limit(count).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	results := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		results = append(results, gin.H{
			"name": row.Name, "id": row.ID, "logo_props": decodeJSON(row.LogoProps),
			"projects__id": row.ProjectID, "workspace__slug": row.WorkspaceSlug,
		})
	}
	return results, nil
}

// namedSearchResults renders a slice through a row builder, keeping the empty list rather than a null.
func namedSearchResults[Row any](rows []Row, build func(int) gin.H) []gin.H {
	results := make([]gin.H, 0, len(rows))
	for index := range rows {
		results = append(results, build(index))
	}
	return results
}
