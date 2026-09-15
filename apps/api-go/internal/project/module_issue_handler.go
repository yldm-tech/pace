package project

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"github.com/yldm-tech/pace/apps/api-go/internal/drf"
	"github.com/yldm-tech/pace/apps/api-go/internal/pagination"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (handler *Handler) registerModuleIssueRoutes(router gin.IRouter) {
	router.GET("/api/workspaces/:slug/projects/:id/modules/:module/issues/", handler.authenticated(handler.moduleIssueList))
	router.POST("/api/workspaces/:slug/projects/:id/modules/:module/issues/", handler.authenticated(handler.moduleIssueCreate))
	router.POST("/api/workspaces/:slug/projects/:id/issues/:issue/modules/", handler.authenticatedIssueUUID(handler.issueModulesUpdate))
}

// moduleIssueListPredicate is the issue_objects manager narrowed to one module. Both halves of the link condition sit inside one EXISTS, the way the cycle's does.
func moduleIssueListPredicate() string {
	return issueObjectsPredicate("i") +
		" AND EXISTS (SELECT 1 FROM module_issues mil WHERE mil.issue_id = i.id AND mil.module_id = ? AND mil.deleted_at IS NULL)"
}

// moduleIssueList is the issue list of one module, sharing everything the project list does.
func (handler *Handler) moduleIssueList(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	slug, projectID, moduleID := c.Param("slug"), c.Param("id"), c.Param("module")

	perPage, err := pagination.PerPage(c.Query("per_page"), pagination.DefaultPerPage, pagination.DefaultPerPage)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	cursor := pagination.OffsetCursor{Value: perPage}
	if raw := c.Query("cursor"); raw != "" {
		parsed, err := pagination.ParseOffsetCursor(raw)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"detail": "Invalid cursor parameter."})
			return
		}
		cursor = parsed
	}

	filters := issueFilters(queryParams(c), "GET", "", handler.clock().UTC())
	joins, conditions, arguments, translatable := issueFilterSQL(filters)
	if !translatable {
		handler.internalError(c, errors.New("module issues: a filter names a field the schema does not have"))
		return
	}

	request := issueListRequest{
		slug: slug, projectID: projectID, perPage: perPage, cursor: cursor,
		joins: joins, conditions: conditions, arguments: arguments,
		basePredicate: moduleIssueListPredicate(), baseArguments: []any{moduleID},
	}
	var total int64
	if err := handler.issueListScope(c.Request.Context(), request).Distinct("i.id").Count(&total).Error; err != nil {
		handler.internalError(c, err)
		return
	}
	request.total = int(total)

	if c.Query("group_by") != "" {
		handler.issueListGrouped(c, user, request)
		return
	}
	rows, err := handler.issueListPage(c, request)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	page := pagination.PlanOffsetPage(perPage, cursor, request.total, len(rows), pagination.DefaultPerPage)
	if len(rows) > page.Limit {
		rows = rows[:page.Limit]
	}
	results := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		results = append(results, issueListRowJSON(row))
	}
	drf.Respond(c, http.StatusOK, page.Envelope(results, len(results), nil, nil, nil))
}

// moduleIssueCreate adds issues to a module. An issue may belong to several modules at once, so unlike a cycle nothing is moved: the links are simply added, and one that already exists is ignored.
func (handler *Handler) moduleIssueCreate(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember) {
		return
	}
	slug, projectID, moduleID := c.Param("slug"), c.Param("id"), c.Param("module")
	var request struct {
		Issues []string `json:"issues"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		handler.invalidDetail(c)
		return
	}
	if len(request.Issues) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Issues are required"})
		return
	}
	project, found, err := handler.projectByID(c, projectID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "The required object does not exist."})
		return
	}

	// The ids are narrowed to this project, which is what stops a foreign issue from being pulled into the module.
	scoped := []string{}
	err = handler.db.WithContext(c.Request.Context()).Table("issues i").
		Joins("JOIN workspaces w ON w.id = i.workspace_id").
		Where("i.id IN ? AND w.slug = ? AND i.project_id = ?", request.Issues, slug, projectID).
		Where(issueObjectsPredicate("i")).Pluck("i.id", &scoped).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}

	now := handler.clock().UTC()
	if err := handler.linkIssuesToModule(c, scoped, moduleID, projectID, project.WorkspaceID, user.ID, now); err != nil {
		handler.internalError(c, err)
		return
	}
	for _, issueID := range scoped {
		if err := handler.publishModuleActivity(c, user, "module.activity.created", moduleID, issueID, projectID, nil, now); err != nil {
			handler.internalError(c, err)
			return
		}
	}
	drf.Respond(c, http.StatusCreated, gin.H{"message": "success"})
}

// issueModulesUpdate is the other direction: it sets which modules one issue belongs to, adding some and removing others in a single request.
func (handler *Handler) issueModulesUpdate(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember) {
		return
	}
	slug, projectID, issueID := c.Param("slug"), c.Param("id"), c.Param("issue")
	var request struct {
		Modules        []string `json:"modules"`
		RemovedModules []string `json:"removed_modules"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		handler.invalidDetail(c)
		return
	}
	project, found, err := handler.projectByID(c, projectID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "The required object does not exist."})
		return
	}
	now := handler.clock().UTC()

	// Unlike the other direction, the module ids are taken as given rather than narrowed, which is what Django does here.
	for _, moduleID := range request.Modules {
		if err := handler.linkIssuesToModule(c, []string{issueID}, moduleID, projectID, project.WorkspaceID, user.ID, now); err != nil {
			handler.internalError(c, err)
			return
		}
	}
	for _, moduleID := range request.Modules {
		if err := handler.publishModuleActivity(c, user, "module.activity.created", moduleID, issueID, projectID, nil, now); err != nil {
			handler.internalError(c, err)
			return
		}
	}

	for _, moduleID := range request.RemovedModules {
		// The module's name is read before the link goes, and is null when the link was not there to begin with.
		name, err := handler.moduleNameForLink(c, slug, projectID, moduleID, issueID)
		if err != nil {
			handler.internalError(c, err)
			return
		}
		if err := handler.publishModuleActivity(c, user, "module.activity.deleted", moduleID, issueID, projectID, name, now); err != nil {
			handler.internalError(c, err)
			return
		}
		if err := handler.unlinkIssueFromModule(c, slug, projectID, moduleID, issueID, now); err != nil {
			handler.internalError(c, err)
			return
		}
	}
	drf.Respond(c, http.StatusCreated, gin.H{"message": "success"})
}

// moduleIssueDestroy takes one issue out of one module. It is written but left unregistered, and the proxy leaves the whole detail path on Django, for the reason the README gives: Django binds GET, PUT and PATCH on that same path to generic actions whose serializer does not match the queryset's model, and cutting the path over means owning all four.
func (handler *Handler) moduleIssueDestroy(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember) {
		return
	}
	slug, projectID, moduleID, issueID := c.Param("slug"), c.Param("id"), c.Param("module"), c.Param("issue")
	name, err := handler.moduleNameForLink(c, slug, projectID, moduleID, issueID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if name == nil {
		// Django reads the name off .first() with no guard, so removing a link that is not there raises and answers 500.
		handler.internalError(c, errors.New("module issues: the link does not exist"))
		return
	}
	now := handler.clock().UTC()
	if err := handler.publishModuleActivity(c, user, "module.activity.deleted", moduleID, issueID, projectID, name, now); err != nil {
		handler.internalError(c, err)
		return
	}
	if err := handler.unlinkIssueFromModule(c, slug, projectID, moduleID, issueID, now); err != nil {
		handler.internalError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// moduleIssueLinkRows builds the link rows. The workspace comes from the project rather than from the request, which is what ProjectBaseModel.save does.
func moduleIssueLinkRows(issueIDs []string, moduleID, projectID, workspaceID, actorID string, now time.Time, identifier func() (string, error)) []ModuleIssue {
	if len(issueIDs) == 0 {
		return nil
	}
	rows := make([]ModuleIssue, 0, len(issueIDs))
	for _, issueID := range issueIDs {
		rowID, err := identifier()
		if err != nil {
			return nil
		}
		rows = append(rows, ModuleIssue{
			ID: rowID, CreatedAt: now, UpdatedAt: now, CreatedByID: &actorID, UpdatedByID: &actorID,
			ProjectID: projectID, WorkspaceID: workspaceID, ModuleID: moduleID, IssueID: issueID,
		})
	}
	return rows
}

// linkIssuesToModule adds the links, ignoring any that already exist.
func (handler *Handler) linkIssuesToModule(c *gin.Context, issueIDs []string, moduleID, projectID, workspaceID, actorID string, now time.Time) error {
	var failure error
	rows := moduleIssueLinkRows(issueIDs, moduleID, projectID, workspaceID, actorID, now, func() (string, error) {
		rowID, err := newUUID()
		if err != nil {
			failure = err
		}
		return rowID, err
	})
	if failure != nil {
		return failure
	}
	if len(rows) == 0 {
		return nil
	}
	return handler.db.WithContext(c.Request.Context()).Clauses(clause.OnConflict{DoNothing: true}).Create(&rows).Error
}

func (handler *Handler) unlinkIssueFromModule(c *gin.Context, slug, projectID, moduleID, issueID string, now time.Time) error {
	return handler.db.WithContext(c.Request.Context()).Model(&ModuleIssue{}).
		Where(`module_id = ? AND issue_id = ? AND project_id = ? AND deleted_at IS NULL
			AND workspace_id = (SELECT id FROM workspaces WHERE slug = ?)`,
			moduleID, issueID, projectID, slug).Update("deleted_at", now).Error
}

// moduleNameForLink reads the module's name through the link, returning nothing when the link is not there.
func (handler *Handler) moduleNameForLink(c *gin.Context, slug, projectID, moduleID, issueID string) (*string, error) {
	var names []string
	err := handler.db.WithContext(c.Request.Context()).Table("module_issues mi").
		Joins("JOIN workspaces w ON w.id = mi.workspace_id").
		Joins("JOIN modules m ON m.id = mi.module_id").
		Where("mi.module_id = ? AND mi.issue_id = ? AND mi.project_id = ? AND w.slug = ? AND mi.deleted_at IS NULL",
			moduleID, issueID, projectID, slug).
		Limit(1).Pluck("m.name", &names).Error
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return nil, nil
	}
	return &names[0], nil
}

// publishModuleActivity sends what the module tasks read. The removal carries the module's name, which is null when the link was not there.
func (handler *Handler) publishModuleActivity(c *gin.Context, user *auth.User, activityType, moduleID, issueID, projectID string, moduleName *string, now time.Time) error {
	requested, err := json.Marshal(map[string]any{"module_id": moduleID})
	if err != nil {
		return err
	}
	requestedData := string(requested)
	activity := issueActivity{
		Type: activityType, RequestedData: &requestedData,
		ActorID: user.ID, IssueID: issueID, ProjectID: projectID,
		Notification: true, Origin: handler.origin(), Epoch: now,
	}
	if activityType == "module.activity.deleted" {
		current, err := json.Marshal(map[string]any{"module_name": moduleName})
		if err != nil {
			return err
		}
		snapshot := string(current)
		activity.CurrentInstance = &snapshot
	}
	return handler.publishIssueActivity(c, activity)
}

func (handler *Handler) projectByID(c *gin.Context, projectID string) (Project, bool, error) {
	var project Project
	err := handler.db.WithContext(c.Request.Context()).Where("id = ?", projectID).Take(&project).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Project{}, false, nil
	}
	if err != nil {
		return Project{}, false, err
	}
	return project, true, nil
}
