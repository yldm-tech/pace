package space

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api/internal/drf"
)

func (handler *Handler) registerProjectRoutes(router gin.IRouter) {
	const base = "/api/public/anchor/:anchor/"
	router.GET(base+"settings/", handler.boardSettings)
	router.GET(base+"meta/", handler.projectMeta)
	router.GET(base+"members/", handler.projectMembers)
	router.GET(base+"states/", handler.projectStates)
	router.GET(base+"labels/", handler.projectLabels)
	router.GET(base+"cycles/", handler.projectCycles)
	router.GET(base+"modules/", handler.projectModules)
	router.GET("/api/public/workspaces/:slug/projects/:project/anchor/", handler.projectAnchor)
}

// boardSettings reports what the published project allows: comments, reactions, votes, and the layouts its board offers.
func (handler *Handler) boardSettings(c *gin.Context) {
	board, found, err := handler.projectBoardByAnchor(c)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !found {
		notFound(c)
		return
	}
	body, err := handler.deployBoardJSON(c, board)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, body)
}

// projectAnchor is the same body read from the other end: a workspace and a project rather than an anchor. It is how the application finds the link for a project it has just published.
func (handler *Handler) projectAnchor(c *gin.Context) {
	var boards []DeployBoard
	err := handler.db.WithContext(c.Request.Context()).Table("deploy_boards d").Select("d.*").
		Joins("JOIN workspaces w ON w.id = d.workspace_id").
		Where("w.slug = ? AND d.project_id = ? AND d.entity_name = 'project' AND d.deleted_at IS NULL",
			c.Param("slug"), c.Param("project")).
		Limit(1).Scan(&boards).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if len(boards) == 0 {
		notFound(c)
		return
	}
	body, err := handler.deployBoardJSON(c, boards[0])
	if err != nil {
		handler.serverError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, body)
}

// projectMeta is the project itself, cut down to what a page needs to render a title and an icon.
//
// It reads the project by the board's **entity identifier** rather than by its project column, which for a project board is the same id twice.
func (handler *Handler) projectMeta(c *gin.Context) {
	board, found, err := handler.projectBoardByAnchor(c)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "Project is not published"})
		return
	}
	if board.EntityIdentifier == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Project is not published"})
		return
	}
	var projects []struct {
		ID          string  `gorm:"column:id"`
		Identifier  string  `gorm:"column:identifier"`
		Name        string  `gorm:"column:name"`
		CoverImage  *string `gorm:"column:cover_image"`
		IconProp    []byte  `gorm:"column:icon_prop"`
		Emoji       *string `gorm:"column:emoji"`
		Description string  `gorm:"column:description"`
	}
	err = handler.db.WithContext(c.Request.Context()).Table("projects").
		Where("id = ? AND deleted_at IS NULL", *board.EntityIdentifier).Limit(1).Scan(&projects).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if len(projects) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Project is not published"})
		return
	}
	project := projects[0]
	drf.Respond(c, http.StatusOK, gin.H{
		"id": project.ID, "identifier": project.Identifier, "name": project.Name,
		"cover_image": project.CoverImage, "icon_prop": decodeJSON(project.IconProp),
		"emoji": project.Emoji, "description": project.Description,
	})
}

// projectMembers reports who is in the project, with the avatar **column** rather than the url every other API reports — so a member whose picture is an uploaded asset comes back with an empty avatar here.
func (handler *Handler) projectMembers(c *gin.Context) {
	board, found, err := handler.boardByAnchor(c)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !found {
		invalidAnchor(c)
		return
	}
	var rows []struct {
		ID          string `gorm:"column:id"`
		MemberID    string `gorm:"column:member_id"`
		DisplayName string `gorm:"column:display_name"`
		Avatar      string `gorm:"column:avatar"`
	}
	err = handler.db.WithContext(c.Request.Context()).Table("project_members pm").
		Select("pm.id, pm.member_id, u.display_name, u.avatar").
		Joins("JOIN users u ON u.id = pm.member_id").
		Where("pm.project_id = ? AND pm.workspace_id = ? AND pm.is_active = TRUE AND pm.deleted_at IS NULL",
			board.ProjectID, board.WorkspaceID).
		Scan(&rows).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	results := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		results = append(results, gin.H{
			"id": row.ID, "member": row.MemberID,
			"member__display_name": row.DisplayName, "member__avatar": row.Avatar,
		})
	}
	drf.Respond(c, http.StatusOK, results)
}

// projectStates reports the project's states, hiding the triage one **by name** rather than by its flag — so a state somebody renamed is reported and one they called Triage is not.
func (handler *Handler) projectStates(c *gin.Context) {
	board, found, err := handler.boardByAnchor(c)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !found {
		invalidAnchor(c)
		return
	}
	var rows []struct {
		Name     string  `gorm:"column:name"`
		Group    string  `gorm:"column:group"`
		Color    string  `gorm:"column:color"`
		ID       string  `gorm:"column:id"`
		Sequence float64 `gorm:"column:sequence"`
	}
	err = handler.db.WithContext(c.Request.Context()).Table("states s").
		Select(`s.name, s."group", s.color, s.id, s.sequence`).
		Joins("JOIN workspaces w ON w.id = s.workspace_id").
		Where("w.id = ? AND s.project_id = ? AND s.name <> 'Triage' AND s.deleted_at IS NULL",
			board.WorkspaceID, board.ProjectID).
		Order("s.sequence").Scan(&rows).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	results := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		results = append(results, gin.H{
			"name": row.Name, "group": row.Group, "color": row.Color,
			"id": row.ID, "sequence": row.Sequence,
		})
	}
	drf.Respond(c, http.StatusOK, results)
}

// projectLabels reports the project's labels, four fields each: the parent is named rather than nested.
func (handler *Handler) projectLabels(c *gin.Context) {
	board, found, err := handler.boardByAnchor(c)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !found {
		invalidAnchor(c)
		return
	}
	var rows []struct {
		ID     string  `gorm:"column:id"`
		Name   string  `gorm:"column:name"`
		Color  string  `gorm:"column:color"`
		Parent *string `gorm:"column:parent_id"`
	}
	err = handler.db.WithContext(c.Request.Context()).Table("labels").
		Select("id, name, color, parent_id").
		Where("workspace_id = ? AND project_id = ? AND deleted_at IS NULL", board.WorkspaceID, board.ProjectID).
		Order("created_at").Scan(&rows).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	results := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		results = append(results, gin.H{
			"id": row.ID, "name": row.Name, "color": row.Color, "parent": row.Parent,
		})
	}
	drf.Respond(c, http.StatusOK, results)
}

// projectCycles and projectModules report an id and a name and nothing else, archived ones included — the published board has no notion of an archive.
func (handler *Handler) projectCycles(c *gin.Context) {
	handler.respondWithIDAndName(c, "cycles")
}

func (handler *Handler) projectModules(c *gin.Context) {
	handler.respondWithIDAndName(c, "modules")
}

func (handler *Handler) respondWithIDAndName(c *gin.Context, table string) {
	board, found, err := handler.boardByAnchor(c)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !found {
		invalidAnchor(c)
		return
	}
	var rows []struct {
		ID   string `gorm:"column:id"`
		Name string `gorm:"column:name"`
	}
	err = handler.db.WithContext(c.Request.Context()).Table(table).
		Select("id, name").
		Where("workspace_id = ? AND project_id = ? AND deleted_at IS NULL", board.WorkspaceID, board.ProjectID).
		Order("created_at").Scan(&rows).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	results := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		results = append(results, gin.H{"id": row.ID, "name": row.Name})
	}
	drf.Respond(c, http.StatusOK, results)
}

// deployBoardJSON is DeployBoardSerializer, the same shape the application's own deploy board routes answer with.
func (handler *Handler) deployBoardJSON(c *gin.Context, board DeployBoard) (gin.H, error) {
	project := any(nil)
	if board.ProjectID != nil {
		rendered, err := handler.projectLiteJSON(c, *board.ProjectID)
		if err != nil {
			return nil, err
		}
		project = rendered
	}
	workspace, err := handler.workspaceLiteJSON(c, board.WorkspaceID)
	if err != nil {
		return nil, err
	}
	return gin.H{
		"id": board.ID, "project_details": project, "workspace_detail": workspace,
		"created_at": board.CreatedAt, "updated_at": board.UpdatedAt, "deleted_at": board.DeletedAt,
		"entity_identifier": board.EntityIdentifier, "entity_name": board.EntityName,
		"anchor": board.Anchor, "is_comments_enabled": board.IsCommentsEnabled,
		"is_reactions_enabled": board.IsReactionsEnabled, "is_votes_enabled": board.IsVotesEnabled,
		"view_props": decodeJSON(board.ViewProps), "is_activity_enabled": board.IsActivityEnabled,
		"is_disabled": board.IsDisabled, "created_by": board.CreatedByID, "updated_by": board.UpdatedByID,
		"workspace": board.WorkspaceID, "project": board.ProjectID, "intake": board.IntakeID,
	}, nil
}

func (handler *Handler) projectLiteJSON(c *gin.Context, projectID string) (gin.H, error) {
	var rows []struct {
		ID          string  `gorm:"column:id"`
		Identifier  string  `gorm:"column:identifier"`
		Name        string  `gorm:"column:name"`
		CoverImage  *string `gorm:"column:cover_image"`
		CoverAsset  *string `gorm:"column:cover_image_asset_id"`
		LogoProps   []byte  `gorm:"column:logo_props"`
		Description string  `gorm:"column:description"`
	}
	err := handler.db.WithContext(c.Request.Context()).Table("projects").
		Select("id, identifier, name, cover_image, cover_image_asset_id, logo_props, description").
		Where("id = ?", projectID).Limit(1).Scan(&rows).Error
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	row := rows[0]
	coverURL := any(nil)
	if row.CoverAsset != nil {
		coverURL = "/api/assets/v2/static/" + *row.CoverAsset + "/"
	} else if row.CoverImage != nil {
		coverURL = *row.CoverImage
	}
	return gin.H{
		"id": row.ID, "identifier": row.Identifier, "name": row.Name,
		"cover_image": row.CoverImage, "cover_image_url": coverURL,
		"logo_props": decodeJSON(row.LogoProps), "description": row.Description,
	}, nil
}

func (handler *Handler) workspaceLiteJSON(c *gin.Context, workspaceID string) (gin.H, error) {
	var rows []struct {
		ID        string  `gorm:"column:id"`
		Name      string  `gorm:"column:name"`
		Slug      string  `gorm:"column:slug"`
		LogoAsset *string `gorm:"column:logo_asset_id"`
	}
	err := handler.db.WithContext(c.Request.Context()).Table("workspaces").
		Select("id, name, slug, logo_asset_id").Where("id = ?", workspaceID).Limit(1).Scan(&rows).Error
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	logoURL := any(nil)
	if rows[0].LogoAsset != nil {
		logoURL = "/api/assets/v2/static/" + *rows[0].LogoAsset + "/"
	}
	return gin.H{"name": rows[0].Name, "slug": rows[0].Slug, "id": rows[0].ID, "logo_url": logoURL}, nil
}
