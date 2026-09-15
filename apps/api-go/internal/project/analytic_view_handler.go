package project

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"github.com/yldm-tech/pace/apps/api-go/internal/drf"
	"gorm.io/gorm"
)

func (handler *Handler) registerAnalyticViewRoutes(router gin.IRouter) {
	router.GET("/api/workspaces/:slug/analytic-view/", handler.authenticated(handler.analyticViewList))
	router.POST("/api/workspaces/:slug/analytic-view/", handler.authenticated(handler.analyticViewCreate))
	router.GET("/api/workspaces/:slug/analytic-view/:view/", handler.authenticated(handler.analyticViewRetrieve))
	router.PATCH("/api/workspaces/:slug/analytic-view/:view/", handler.authenticated(handler.analyticViewUpdate))
	router.DELETE("/api/workspaces/:slug/analytic-view/:view/", handler.authenticated(handler.analyticViewDestroy))
	router.POST("/api/workspaces/:slug/export-analytics/", handler.authenticated(handler.exportAnalytics))
}

// AnalyticView is the db.AnalyticView table: a saved set of analytics filters.
type AnalyticView struct {
	ID          string     `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt   time.Time  `gorm:"column:created_at"`
	UpdatedAt   time.Time  `gorm:"column:updated_at"`
	CreatedByID *string    `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID *string    `gorm:"column:updated_by_id;type:uuid"`
	DeletedAt   *time.Time `gorm:"column:deleted_at"`
	WorkspaceID string     `gorm:"column:workspace_id;type:uuid"`
	Name        string     `gorm:"column:name"`
	Description string     `gorm:"column:description"`
	Query       []byte     `gorm:"column:query;type:jsonb"`
	QueryDict   []byte     `gorm:"column:query_dict;type:jsonb"`
}

func (AnalyticView) TableName() string { return "analytic_views" }

// validAnalyticsFields is VALID_ANALYTICS_FIELDS: what a chart may be grouped or segmented by.
var validAnalyticsFields = map[string]bool{
	"state_id": true, "state__group": true, "labels__id": true, "assignees__id": true,
	"estimate_point__value": true, "issue_cycle__cycle_id": true, "issue_module__module_id": true,
	"priority": true, "start_date": true, "target_date": true, "created_at": true, "completed_at": true,
}

// validAnalyticsYAxis is VALID_YAXIS: what a chart may measure.
var validAnalyticsYAxis = map[string]bool{"issue_count": true, "estimate": true}

func (handler *Handler) analyticViewList(c *gin.Context, user *auth.User) {
	if !handler.requireWorkspaceRole(c, user, roleAdmin) {
		return
	}
	var views []AnalyticView
	err := handler.db.WithContext(c.Request.Context()).Table("analytic_views av").Select("av.*").
		Joins("JOIN workspaces w ON w.id = av.workspace_id").
		Where("w.slug = ? AND av.deleted_at IS NULL", c.Param("slug")).
		Order("av.created_at DESC").Scan(&views).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	results := make([]gin.H, 0, len(views))
	for _, view := range views {
		results = append(results, analyticViewJSON(view))
	}
	drf.Respond(c, http.StatusOK, results)
}

func (handler *Handler) analyticViewRetrieve(c *gin.Context, user *auth.User) {
	if !handler.requireWorkspaceRole(c, user, roleAdmin) {
		return
	}
	view, found, err := handler.analyticViewByID(c)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"detail": "No AnalyticView matches the given query."})
		return
	}
	drf.Respond(c, http.StatusOK, analyticViewJSON(view))
}

// analyticViewCreate saves a set of analytics filters, deriving the stored query from them.
func (handler *Handler) analyticViewCreate(c *gin.Context, user *auth.User) {
	if !handler.requireWorkspaceRole(c, user, roleAdmin) {
		return
	}
	slug := c.Param("slug")
	var payload map[string]any
	if err := c.ShouldBindJSON(&payload); err != nil {
		handler.invalidDetail(c)
		return
	}
	if name, ok := payload["name"].(string); !ok || strings.TrimSpace(name) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"name": []string{"This field is required."}})
		return
	}

	var workspaceIDs []string
	err := handler.db.WithContext(c.Request.Context()).Table("workspaces").
		Where("slug = ?", slug).Limit(1).Pluck("id", &workspaceIDs).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if len(workspaceIDs) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "The required object does not exist."})
		return
	}

	identifier, err := newUUID()
	if err != nil {
		handler.internalError(c, err)
		return
	}
	now := handler.clock().UTC()
	view := AnalyticView{
		ID: identifier, CreatedAt: now, UpdatedAt: now, CreatedByID: &user.ID, UpdatedByID: &user.ID,
		WorkspaceID: workspaceIDs[0], QueryDict: []byte("{}"),
	}
	applyAnalyticViewPayload(&view, payload)
	// The query is derived from query_dict rather than accepted, and it is derived here rather than in the model: this table has no save() of its own.
	view.Query, err = viewQueryJSON(view.QueryDict, now)
	if err != nil {
		handler.internalError(c, err)
		return
	}

	if err := handler.db.WithContext(c.Request.Context()).Create(&view).Error; err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusCreated, analyticViewJSON(view))
}

// analyticViewUpdate edits a saved set of filters.
//
// **Every update clears the stored query.** The serializer reads `query_data` on the update path — a key the model does not have and no caller sends — so the filters it derives from are always empty. It then derives them a second time with a method the parser does not know, which changes nothing since they were empty already. Reproduced: the column comes back as the empty object whatever the request said.
func (handler *Handler) analyticViewUpdate(c *gin.Context, user *auth.User) {
	if !handler.requireWorkspaceRole(c, user, roleAdmin) {
		return
	}
	var payload map[string]any
	if err := c.ShouldBindJSON(&payload); err != nil {
		handler.invalidDetail(c)
		return
	}
	view, found, err := handler.analyticViewByID(c)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"detail": "No AnalyticView matches the given query."})
		return
	}

	applyAnalyticViewPayload(&view, payload)
	now := handler.clock().UTC()
	view.UpdatedAt, view.UpdatedByID = now, &user.ID
	view.Query = []byte("{}")

	err = handler.db.WithContext(c.Request.Context()).Model(&AnalyticView{}).Where("id = ?", view.ID).
		Updates(map[string]any{
			"name": view.Name, "description": view.Description, "query_dict": view.QueryDict,
			"query": view.Query, "updated_at": view.UpdatedAt, "updated_by_id": view.UpdatedByID,
		}).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, analyticViewJSON(view))
}

func (handler *Handler) analyticViewDestroy(c *gin.Context, user *auth.User) {
	if !handler.requireWorkspaceRole(c, user, roleAdmin) {
		return
	}
	view, found, err := handler.analyticViewByID(c)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"detail": "No AnalyticView matches the given query."})
		return
	}
	err = handler.db.WithContext(c.Request.Context()).Model(&AnalyticView{}).
		Where("id = ?", view.ID).Update("deleted_at", handler.clock().UTC()).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// exportAnalytics validates a chart's shape and hands the work to the mailer.
//
// It checks the axes and then does nothing with them itself: the task reads the body again. So the validation here is about refusing a request early rather than about what gets exported.
func (handler *Handler) exportAnalytics(c *gin.Context, user *auth.User) {
	if !handler.requireWorkspaceRole(c, user, roleAdmin, roleMember) {
		return
	}
	var payload map[string]any
	if err := c.ShouldBindJSON(&payload); err != nil {
		handler.invalidDetail(c)
		return
	}
	if message := validateAnalyticsAxes(payload["x_axis"], payload["y_axis"], payload["segment"]); message != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": message})
		return
	}
	if handler.tasks != nil {
		if err := handler.tasks.PublishAnalyticExport(c.Request.Context(), user.Email, payload, c.Param("slug")); err != nil {
			handler.internalError(c, err)
			return
		}
	}
	drf.Respond(c, http.StatusOK, gin.H{
		"message": "Once the export is ready it will be emailed to you at " + user.Email,
	})
}

// validateAnalyticsAxes is the pair of checks every chart endpoint runs, in the order they run them.
func validateAnalyticsAxes(xAxis, yAxis, segment any) string {
	x, _ := xAxis.(string)
	y, _ := yAxis.(string)
	if x == "" || y == "" || !validAnalyticsFields[x] || !validAnalyticsYAxis[y] {
		return "x-axis and y-axis dimensions are required and the values should be valid"
	}
	// A segment is optional, and an empty one is not a segment at all.
	if value, ok := segment.(string); ok && value != "" {
		if !validAnalyticsFields[value] || value == x {
			return "Both segment and x axis cannot be same and segment should be valid"
		}
	}
	return ""
}

func (handler *Handler) analyticViewByID(c *gin.Context) (AnalyticView, bool, error) {
	var view AnalyticView
	err := handler.db.WithContext(c.Request.Context()).Table("analytic_views av").Select("av.*").
		Joins("JOIN workspaces w ON w.id = av.workspace_id").
		Where("w.slug = ? AND av.id = ? AND av.deleted_at IS NULL", c.Param("slug"), c.Param("view")).
		Take(&view).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return AnalyticView{}, false, nil
	}
	if err != nil {
		return AnalyticView{}, false, err
	}
	return view, true, nil
}

// applyAnalyticViewPayload writes the writable fields. The workspace and the query are read-only, so a caller naming either changes nothing.
func applyAnalyticViewPayload(view *AnalyticView, payload map[string]any) {
	if value, ok := payload["name"].(string); ok {
		view.Name = value
	}
	if value, ok := payload["description"].(string); ok {
		view.Description = value
	}
	if value, present := payload["query_dict"]; present {
		encoded, err := json.Marshal(value)
		if err == nil {
			view.QueryDict = encoded
		}
	}
}

// analyticViewJSON is AnalyticViewSerializer over one row.
func analyticViewJSON(view AnalyticView) gin.H {
	return gin.H{
		"id": view.ID, "created_at": view.CreatedAt, "updated_at": view.UpdatedAt,
		"created_by": view.CreatedByID, "updated_by": view.UpdatedByID, "deleted_at": view.DeletedAt,
		"name": view.Name, "description": view.Description,
		"query": decodeJSON(view.Query), "query_dict": decodeJSON(view.QueryDict),
		"workspace": view.WorkspaceID,
	}
}
