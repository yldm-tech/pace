package project

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/yldm-tech/pace/apps/api/internal/access"
	"github.com/yldm-tech/pace/apps/api/internal/auth"
	"github.com/yldm-tech/pace/apps/api/internal/drf"
	"github.com/yldm-tech/pace/apps/api/internal/pagination"
	"gorm.io/gorm"
)

// The export routes start a work item export and read the history of the ones already asked for. The work itself happens in the worker; this only records that it was asked for and hands the token over.
func (handler *Handler) registerExporterRoutes(router gin.IRouter) {
	const base = "/api/workspaces/:slug/export-issues/"
	router.GET(base, handler.authenticated(handler.exporterList))
	router.POST(base, handler.authenticated(handler.exporterCreate))
}

// ExporterHistory is one export somebody asked for, from the moment it was asked for to the link it ends up as.
type ExporterHistory struct {
	ID            string         `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt     time.Time      `gorm:"column:created_at"`
	UpdatedAt     time.Time      `gorm:"column:updated_at"`
	CreatedByID   *string        `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID   *string        `gorm:"column:updated_by_id;type:uuid"`
	Name          *string        `gorm:"column:name"`
	Type          string         `gorm:"column:type"`
	WorkspaceID   string         `gorm:"column:workspace_id;type:uuid"`
	Project       pq.StringArray `gorm:"column:project;type:uuid[]"`
	Provider      string         `gorm:"column:provider"`
	Status        string         `gorm:"column:status"`
	Reason        string         `gorm:"column:reason"`
	Key           string         `gorm:"column:key"`
	URL           *string        `gorm:"column:url"`
	Token         string         `gorm:"column:token"`
	InitiatedByID string         `gorm:"column:initiated_by_id;type:uuid"`
}

func (ExporterHistory) TableName() string { return "exporters" }

// exporterProviders are the three formats the route takes. Anything else is refused by name rather than ignored.
var exporterProviders = map[string]bool{"csv": true, "xlsx": true, "json": true}

// exporterOrderColumns are the columns the history may be ordered by. Django hands whatever arrives straight to order_by, so a name that is not a field raises FieldError and answers 500; a name that is not here does the same.
var exporterOrderColumns = map[string]string{
	"created_at": "e.created_at", "updated_at": "e.updated_at", "provider": "e.provider",
	"status": "e.status", "type": "e.type", "token": "e.token", "id": "e.id",
}

// exporterCreate records that an export was asked for and hands the token to the worker.
//
// The project ids are taken on trust: a caller who names them is not checked against them, and only a caller who names none gets the list of projects they are really a member of. The worker's own queryset is what keeps a foreign project out of the file, so naming one here produces an export with nothing in it rather than one with somebody else's work items.
func (handler *Handler) exporterCreate(c *gin.Context, user *auth.User) {
	if !handler.requireWorkspaceRole(c, user, roleAdmin, roleMember) {
		return
	}
	// The three keys are read one at a time rather than bound together, because Django reads them one at a time: a project that is not a list does not stop the provider from being read.
	payload := map[string]any{}
	_ = c.ShouldBindJSON(&payload)

	provider, _ := payload["provider"].(string)
	if !exporterProviders[provider] {
		// Django renders whatever arrived, so an absent provider reads back as False rather than as an empty string.
		c.JSON(http.StatusBadRequest, gin.H{"error": "Provider '" + exporterProviderName(payload) + "' not found."})
		return
	}
	multiple, _ := payload["multiple"].(bool)
	projectIDs := exporterProjectIDs(payload["project"])

	var workspaceIDs []string
	err := handler.db.WithContext(c.Request.Context()).Table("workspaces").
		Where("slug = ?", c.Param("slug")).Limit(1).Pluck("id", &workspaceIDs).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if len(workspaceIDs) == 0 {
		handler.internalError(c, gorm.ErrRecordNotFound)
		return
	}
	workspaceID := workspaceIDs[0]

	if len(projectIDs) == 0 {
		// Nobody named a project, so it is every project in the workspace this person is still a member of and which has not been archived.
		err = handler.db.WithContext(c.Request.Context()).Table("projects p").
			Joins(access.MemberJoin("p", "id", access.WithMembershipSoftDelete()), user.ID).
			Where("p.workspace_id = ? AND p.archived_at IS NULL AND p.deleted_at IS NULL", workspaceID).
			Distinct().Pluck("p.id", &projectIDs).Error
		if err != nil {
			handler.internalError(c, err)
			return
		}
	}

	now := handler.clock().UTC()
	row := ExporterHistory{
		ID: uuid.NewString(), CreatedAt: now, UpdatedAt: now,
		CreatedByID: &user.ID, UpdatedByID: &user.ID,
		Type: "issue_exports", WorkspaceID: workspaceID, Project: pq.StringArray(projectIDs),
		Provider: provider, Status: "queued", Token: exporterToken(), InitiatedByID: user.ID,
	}
	if err := handler.db.WithContext(c.Request.Context()).Create(&row).Error; err != nil {
		handler.internalError(c, err)
		return
	}

	if handler.tasks != nil {
		if err := handler.tasks.PublishIssueExport(c.Request.Context(), provider, workspaceID, projectIDs, row.Token, multiple, c.Param("slug")); err != nil {
			handler.internalError(c, err)
			return
		}
	}
	drf.Respond(c, http.StatusOK, gin.H{"message": "Once the export is ready you will be able to download it"})
}

// exporterProviderName renders the provider back into the refusal the way python's f-string does. An absent key reads as False, since that is the default the view asks for, and one sent as null reads as None.
func exporterProviderName(payload map[string]any) string {
	value, present := payload["provider"]
	if !present {
		return "False"
	}
	switch typed := value.(type) {
	case nil:
		return "None"
	case string:
		return typed
	case bool:
		if typed {
			return "True"
		}
		return "False"
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		return fmt.Sprint(typed)
	}
}

// exporterProjectIDs reads the project list. Anything that is not a list of strings is taken as no list at all, which is what an empty one means too.
func exporterProjectIDs(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	ids := make([]string, 0, len(items))
	for _, item := range items {
		if text, ok := item.(string); ok {
			ids = append(ids, text)
		}
	}
	return ids
}

// exporterToken is generate_token: a uuid with its dashes taken out, which is what the worker is handed instead of the row's id.
func exporterToken() string {
	return strings.ReplaceAll(uuid.NewString(), "-", "")
}

// exporterList reads the exports already asked for in this workspace, and refuses to read them at all unless the caller pages.
func (handler *Handler) exporterList(c *gin.Context, user *auth.User) {
	if !handler.requireWorkspaceRole(c, user, roleAdmin, roleMember) {
		return
	}
	// The route only answers a paged request. Without both parameters it reports what is missing rather than returning everything.
	if c.Query("per_page") == "" || c.Query("cursor") == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "per_page and cursor are required"})
		return
	}
	perPage, err := pagination.PerPage(c.Query("per_page"), pagination.DefaultPerPage, pagination.DefaultPerPage)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	cursor, err := pagination.ParseOffsetCursor(c.Query("cursor"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "Invalid cursor parameter."})
		return
	}

	orderBy := c.DefaultQuery("order_by", "-created_at")
	direction := " ASC"
	name := orderBy
	if len(name) > 0 && name[0] == '-' {
		name, direction = name[1:], " DESC"
	}
	column, known := exporterOrderColumns[name]
	if !known {
		// Django hands the name straight to order_by, so one that is not a field raises FieldError and answers 500 rather than 400.
		handler.internalError(c, fmt.Errorf("exporter history: %q is not a field it can be ordered by", orderBy))
		return
	}

	scope := func() *gorm.DB {
		return handler.db.WithContext(c.Request.Context()).Table("exporters e").
			Joins("JOIN workspaces w ON w.id = e.workspace_id").
			Where("w.slug = ? AND e.type = ? AND e.deleted_at IS NULL", c.Param("slug"), "issue_exports")
	}
	var total int64
	if err := scope().Count(&total).Error; err != nil {
		handler.internalError(c, err)
		return
	}
	window := pagination.PlanOffsetPage(perPage, cursor, int(total), 0, pagination.DefaultPerPage)
	var rows []ExporterHistory
	if err := scope().Select("e.*").Order(column + direction).Offset(window.Offset).Limit(window.Stop - window.Offset).Scan(&rows).Error; err != nil {
		handler.internalError(c, err)
		return
	}
	page := pagination.PlanOffsetPage(perPage, cursor, int(total), len(rows), pagination.DefaultPerPage)
	if len(rows) > page.Limit {
		rows = rows[:page.Limit]
	}

	people, err := handler.exporterInitiators(c, rows)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	results := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		results = append(results, exporterHistoryJSON(row, people))
	}
	drf.Respond(c, http.StatusOK, page.Envelope(results, len(results), nil, nil, nil))
}

// exporterInitiators reads the people the exports were asked for by, which the serializer expands through UserLiteSerializer.
func (handler *Handler) exporterInitiators(c *gin.Context, rows []ExporterHistory) (map[string]auth.User, error) {
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.InitiatedByID)
	}
	people := map[string]auth.User{}
	if len(ids) == 0 {
		return people, nil
	}
	var users []auth.User
	if err := handler.db.WithContext(c.Request.Context()).Where("id IN ?", ids).Find(&users).Error; err != nil {
		return nil, err
	}
	for _, person := range users {
		people[person.ID] = person
	}
	return people, nil
}

// exporterHistoryJSON is ExporterHistorySerializer.
func exporterHistoryJSON(row ExporterHistory, people map[string]auth.User) gin.H {
	// The column is a nullable array, so an export recorded without one reads back as null rather than as an empty list.
	var projects any
	if row.Project != nil {
		projects = []string(row.Project)
	}
	var initiator any
	if person, found := people[row.InitiatedByID]; found {
		initiator = liteUserJSON(person, false)
	}
	return gin.H{
		"id": row.ID, "created_at": row.CreatedAt, "updated_at": row.UpdatedAt,
		"project": projects, "provider": row.Provider, "status": row.Status, "url": row.URL,
		"initiated_by": row.InitiatedByID, "initiated_by_detail": initiator,
		"token": row.Token, "created_by": row.CreatedByID, "updated_by": row.UpdatedByID,
	}
}
