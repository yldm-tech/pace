package project

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"github.com/yldm-tech/pace/apps/api-go/internal/drf"
	"gorm.io/gorm"
)

func (handler *Handler) registerGlobalSearchRoutes(router gin.IRouter) {
	router.GET("/api/workspaces/:slug/search/", handler.authenticated(handler.globalSearch))
}

// globalSearchEntities is the mapper the endpoint iterates, and the order matters: with no entities parameter every one of them runs, in this order.
var globalSearchEntities = []string{"workspace", "project", "issue", "cycle", "module", "issue_view", "page", "intake"}

// globalSearch runs up to eight searches side by side and returns them under one key each.
//
// An **empty** search is not a search for nothing. The query is passed as `query or None`, so an empty one reaches each filter as nothing at all, every `if query` is skipped, and the endpoint returns everything the caller can see rather than nothing.
func (handler *Handler) globalSearch(c *gin.Context, user *auth.User) {
	if !handler.requireWorkspaceRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	slug := c.Param("slug")
	search := c.Query("search")
	projectID := c.Query("project_id")
	// Without workspace_search the searches that can be narrowed stay inside the project the caller named.
	scoped := c.DefaultQuery("workspace_search", "false") == "false" && projectID != ""

	wanted := globalSearchEntities
	if raw := c.Query("entities"); raw != "" {
		known := map[string]bool{}
		for _, entity := range globalSearchEntities {
			known[entity] = true
		}
		wanted = []string{}
		for _, entity := range strings.Split(raw, ",") {
			entity = strings.TrimSpace(entity)
			// An entity nobody knows is dropped rather than refused.
			if entity != "" && known[entity] {
				wanted = append(wanted, entity)
			}
		}
	}

	results := gin.H{}
	for _, entity := range wanted {
		rows, err := handler.searchEntity(c, user, entity, search, slug, projectID, scoped)
		if err != nil {
			handler.internalError(c, err)
			return
		}
		results[entity] = rows
	}
	drf.Respond(c, http.StatusOK, gin.H{"results": results})
}

func (handler *Handler) searchEntity(c *gin.Context, user *auth.User, entity, search, slug, projectID string, scoped bool) ([]gin.H, error) {
	switch entity {
	case "workspace":
		return handler.searchWorkspaces(c, user, search)
	case "project":
		return handler.searchProjects(c, user, search, slug)
	case "issue":
		return handler.searchIssues(c, user, search, slug, projectID, scoped)
	case "cycle":
		return handler.searchNamed(c, user, search, slug, projectID, scoped, "cycles")
	case "module":
		return handler.searchNamed(c, user, search, slug, projectID, scoped, "modules")
	case "issue_view":
		return handler.searchNamed(c, user, search, slug, projectID, scoped, "issue_views")
	case "page":
		return handler.searchPages(c, user, search, slug, projectID, scoped)
	case "intake":
		return handler.searchIntakes(c, user, search, slug, projectID, scoped)
	}
	return []gin.H{}, nil
}

// searchWorkspaces is the one search that is not scoped to the workspace in the URL at all, so it answers with every workspace the caller belongs to whatever slug they asked under. It does not check that the membership is active either.
func (handler *Handler) searchWorkspaces(c *gin.Context, user *auth.User, search string) ([]gin.H, error) {
	query := handler.db.WithContext(c.Request.Context()).Table("workspaces w").
		Joins("JOIN workspace_members wm ON wm.workspace_id = w.id AND wm.member_id = ?", user.ID).
		Where("w.deleted_at IS NULL")
	if search != "" {
		query = query.Where("w.name ILIKE ?", "%"+search+"%")
	}
	var rows []struct {
		Name string `gorm:"column:name"`
		ID   string `gorm:"column:id"`
		Slug string `gorm:"column:slug"`
	}
	err := query.Distinct().Select("w.name, w.id, w.slug, w.created_at").
		Order("w.created_at DESC").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	results := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		results = append(results, gin.H{"name": row.Name, "id": row.ID, "slug": row.Slug})
	}
	return results, nil
}

// searchProjects looks at both the name and the identifier, which is the only search that does apart from the two issue ones.
func (handler *Handler) searchProjects(c *gin.Context, user *auth.User, search, slug string) ([]gin.H, error) {
	query := handler.db.WithContext(c.Request.Context()).Table("projects p").
		Joins("JOIN workspaces w ON w.id = p.workspace_id").
		Joins("JOIN project_members pm ON pm.project_id = p.id AND pm.member_id = ? AND pm.is_active = TRUE", user.ID).
		Where("w.slug = ? AND p.archived_at IS NULL AND p.deleted_at IS NULL", slug)
	if search != "" {
		query = query.Where("p.name ILIKE ? OR p.identifier ILIKE ?", "%"+search+"%", "%"+search+"%")
	}
	var rows []struct {
		Name       string `gorm:"column:name"`
		ID         string `gorm:"column:id"`
		Identifier string `gorm:"column:identifier"`
		Slug       string `gorm:"column:workspace_slug"`
	}
	err := query.Distinct().Select("p.name, p.id, p.identifier, w.slug AS workspace_slug, p.created_at").
		Order("p.created_at DESC").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	results := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		results = append(results, gin.H{
			"name": row.Name, "id": row.ID, "identifier": row.Identifier, "workspace__slug": row.Slug,
		})
	}
	return results, nil
}

// searchIssues is the same three-field search the picker uses, capped at a hundred. It goes through the issue_objects manager, so nothing archived, draft or in triage appears.
func (handler *Handler) searchIssues(c *gin.Context, user *auth.User, search, slug, projectID string, scoped bool) ([]gin.H, error) {
	query := handler.memberIssueScope(c, user, slug).Where(issueObjectsPredicate("i"))
	if search != "" {
		query = applyIssueSearch(query, search)
	}
	if scoped {
		query = query.Where("i.project_id = ?", projectID)
	}
	return issueSearchResults(query.Distinct().Limit(100))
}

// searchIntakes is the twin of the issue search with two differences: it looks through the **plain** manager, so an archived, draft or triage issue is eligible, and it keeps only what is still waiting in or snoozed inside an intake.
func (handler *Handler) searchIntakes(c *gin.Context, user *auth.User, search, slug, projectID string, scoped bool) ([]gin.H, error) {
	query := handler.memberIssueScope(c, user, slug).Where("i.deleted_at IS NULL").
		Where(`EXISTS (SELECT 1 FROM issue_intake ii WHERE ii.issue_id = i.id AND ii.deleted_at IS NULL AND ii.status IN (0, -2))`)
	if search != "" {
		query = applyIssueSearch(query, search)
	}
	if scoped {
		query = query.Where("i.project_id = ?", projectID)
	}
	return issueSearchResults(query.Distinct().Order("i.created_at DESC").Limit(100))
}

// memberIssueScope is the join both issue searches start from: issues in projects the caller is an active member of.
func (handler *Handler) memberIssueScope(c *gin.Context, user *auth.User, slug string) *gorm.DB {
	return handler.db.WithContext(c.Request.Context()).Table("issues i").
		Joins("JOIN workspaces w ON w.id = i.workspace_id").
		Joins("JOIN projects p ON p.id = i.project_id AND p.archived_at IS NULL").
		Joins("JOIN project_members pm ON pm.project_id = i.project_id AND pm.member_id = ? AND pm.is_active = TRUE", user.ID).
		Where("w.slug = ?", slug)
}

// issueSearchResults renders the six fields both issue searches project.
func issueSearchResults(query *gorm.DB) ([]gin.H, error) {
	var rows []struct {
		Name              string `gorm:"column:name"`
		ID                string `gorm:"column:id"`
		SequenceID        int64  `gorm:"column:sequence_id"`
		ProjectIdentifier string `gorm:"column:project_identifier"`
		ProjectID         string `gorm:"column:project_id"`
		WorkspaceSlug     string `gorm:"column:workspace_slug"`
	}
	err := query.Select(`i.name, i.id, i.sequence_id, p.identifier AS project_identifier,
		i.project_id, w.slug AS workspace_slug, i.created_at`).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	results := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		results = append(results, gin.H{
			"name": row.Name, "id": row.ID, "sequence_id": row.SequenceID,
			"project__identifier": row.ProjectIdentifier, "project_id": row.ProjectID,
			"workspace__slug": row.WorkspaceSlug,
		})
	}
	return results, nil
}

// searchNamed is the three searches that differ only in their table: cycles, modules and views all match on the name alone and project the same five fields.
func (handler *Handler) searchNamed(c *gin.Context, user *auth.User, search, slug, projectID string, scoped bool, table string) ([]gin.H, error) {
	query := handler.db.WithContext(c.Request.Context()).Table(table+" t").
		Joins("JOIN workspaces w ON w.id = t.workspace_id").
		Joins("JOIN projects p ON p.id = t.project_id AND p.archived_at IS NULL").
		Joins("JOIN project_members pm ON pm.project_id = t.project_id AND pm.member_id = ? AND pm.is_active = TRUE", user.ID).
		Where("w.slug = ? AND t.deleted_at IS NULL", slug)
	if search != "" {
		query = query.Where("t.name ILIKE ?", "%"+search+"%")
	}
	if scoped {
		query = query.Where("t.project_id = ?", projectID)
	}
	var rows []struct {
		Name              string `gorm:"column:name"`
		ID                string `gorm:"column:id"`
		ProjectID         string `gorm:"column:project_id"`
		ProjectIdentifier string `gorm:"column:project_identifier"`
		WorkspaceSlug     string `gorm:"column:workspace_slug"`
	}
	err := query.Distinct().Select(`t.name, t.id, t.project_id, p.identifier AS project_identifier,
		w.slug AS workspace_slug, t.created_at`).Order("t.created_at DESC").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	results := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		results = append(results, gin.H{
			"name": row.Name, "id": row.ID, "project_id": row.ProjectID,
			"project__identifier": row.ProjectIdentifier, "workspace__slug": row.WorkspaceSlug,
		})
	}
	return results, nil
}

// searchPages projects lists rather than single values, because a page reaches its projects through a link table and can sit in more than one.
func (handler *Handler) searchPages(c *gin.Context, user *auth.User, search, slug, projectID string, scoped bool) ([]gin.H, error) {
	query := handler.db.WithContext(c.Request.Context()).Table("pages pg").
		Joins("JOIN workspaces w ON w.id = pg.workspace_id").
		Joins("JOIN project_pages pp ON pp.page_id = pg.id").
		Joins("JOIN projects p ON p.id = pp.project_id AND p.archived_at IS NULL").
		Joins("JOIN project_members pm ON pm.project_id = pp.project_id AND pm.member_id = ? AND pm.is_active = TRUE", user.ID).
		Where("w.slug = ? AND pg.deleted_at IS NULL", slug)
	if search != "" {
		query = query.Where("pg.name ILIKE ?", "%"+search+"%")
	}
	if scoped {
		query = query.Where(`EXISTS (SELECT 1 FROM project_pages sp WHERE sp.page_id = pg.id AND sp.project_id = ?)`, projectID)
	}
	var rows []struct {
		Name               string         `gorm:"column:name"`
		ID                 string         `gorm:"column:id"`
		ProjectIDs         pq.StringArray `gorm:"column:project_ids;type:uuid[]"`
		ProjectIdentifiers pq.StringArray `gorm:"column:project_identifiers;type:text[]"`
		WorkspaceSlug      string         `gorm:"column:workspace_slug"`
	}
	err := query.Group("pg.id, w.slug").Select(`pg.name, pg.id, w.slug AS workspace_slug, pg.created_at,
		COALESCE((SELECT ARRAY_AGG(DISTINCT lp.project_id) FROM project_pages lp WHERE lp.page_id = pg.id), '{}') AS project_ids,
		COALESCE((SELECT ARRAY_AGG(DISTINCT lpr.identifier) FROM project_pages lp2
			JOIN projects lpr ON lpr.id = lp2.project_id WHERE lp2.page_id = pg.id), '{}') AS project_identifiers`).
		Order("pg.created_at DESC").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	results := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		results = append(results, gin.H{
			"name": row.Name, "id": row.ID,
			"project_ids": stringsOrEmpty(row.ProjectIDs), "project_identifiers": stringsOrEmpty(row.ProjectIdentifiers),
			"workspace__slug": row.WorkspaceSlug,
		})
	}
	return results, nil
}
