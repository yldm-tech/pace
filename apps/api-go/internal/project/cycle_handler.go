package project

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"github.com/yldm-tech/pace/apps/api-go/internal/cycles"
	"github.com/yldm-tech/pace/apps/api-go/internal/drf"
	"gorm.io/gorm"
)

func (handler *Handler) registerCycleBasicsRoutes(router gin.IRouter) {
	router.POST("/api/workspaces/:slug/projects/:id/cycles/date-check/", handler.authenticated(handler.cycleDateCheck))
	router.POST("/api/workspaces/:slug/projects/:id/user-favorite-cycles/", handler.authenticated(handler.cycleFavoriteCreate))
	router.GET("/api/workspaces/:slug/projects/:id/user-favorite-cycles/", handler.authenticated(handler.cycleFavoriteList))
	router.DELETE("/api/workspaces/:slug/projects/:id/user-favorite-cycles/:cycle/", handler.authenticated(handler.cycleFavoriteDestroy))
	router.GET("/api/workspaces/:slug/projects/:id/cycles/:cycle/user-properties/", handler.authenticated(handler.cycleUserProperties))
	router.PATCH("/api/workspaces/:slug/projects/:id/cycles/:cycle/user-properties/", handler.authenticated(handler.updateCycleUserProperties))
}

// cycleDateCheck answers whether a proposed interval overlaps a cycle that already exists, which is what the create form asks before it lets a date through.
func (handler *Handler) cycleDateCheck(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember) {
		return
	}
	slug, projectID := c.Param("slug"), c.Param("id")
	var request struct {
		StartDate json.RawMessage `json:"start_date"`
		EndDate   json.RawMessage `json:"end_date"`
		CycleID   *string         `json:"cycle_id"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		handler.invalidDetail(c)
		return
	}
	start, startGiven := dateFromRaw(request.StartDate)
	end, endGiven := dateFromRaw(request.EndDate)
	// Django treats a missing key and an empty value alike, since it reads them with a default of False.
	if !startGiven || start == "" || !endGiven || end == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Start date and end date both are required"})
		return
	}

	var timezones []string
	err := handler.db.WithContext(c.Request.Context()).Table("projects").
		Where("id = ?", projectID).Limit(1).Pluck("timezone", &timezones).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if len(timezones) == 0 {
		// Django's unguarded Project.objects.get raises DoesNotExist.
		c.JSON(http.StatusNotFound, gin.H{"error": "The required object does not exist."})
		return
	}
	startAt, endAt, ok := cycleInterval(start, end, timezones[0], handler.clock().UTC())
	if !ok {
		// strptime raises on a date it cannot read, which answers 500.
		handler.internalError(c, errMalformedDate)
		return
	}

	query := handler.db.WithContext(c.Request.Context()).Table("cycles c").
		Joins("JOIN workspaces w ON w.id = c.workspace_id").
		Where("w.slug = ? AND c.project_id = ? AND c.deleted_at IS NULL", slug, projectID).
		Where(`(c.start_date <= ? AND c.end_date >= ?)
			OR (c.start_date <= ? AND c.end_date >= ?)
			OR (c.start_date >= ? AND c.end_date <= ?)`,
			startAt, startAt, endAt, endAt, startAt, endAt)
	if request.CycleID != nil {
		query = query.Where("c.id <> ?", *request.CycleID)
	}
	var overlapping int64
	if err := query.Count(&overlapping).Error; err != nil {
		handler.internalError(c, err)
		return
	}
	if overlapping > 0 {
		// Django answers 200 here rather than a 4xx, since a clash is an answer rather than an error.
		drf.Respond(c, http.StatusOK, gin.H{
			"error":  "You have a cycle already on the given dates, if you want to create a draft cycle you can do that by removing dates",
			"status": false,
		})
		return
	}
	drf.Respond(c, http.StatusOK, gin.H{"status": true})
}

// cycleInterval is convert_to_utc over the pair, which lives in internal/cycles because the external API writes a cycle's dates through the same conversion.
func cycleInterval(start, end, timezone string, now time.Time) (time.Time, time.Time, bool) {
	return cycles.ConvertToUTC(start, end, timezone, now)
}

// cycleFavoriteList is broken upstream and reproduced as such. The viewset inherits DRF's list from ModelViewSet but declares no serializer_class, so get_serializer_class asserts and answers 500. Answering anything else here would be inventing a response the endpoint has never given.
func (handler *Handler) cycleFavoriteList(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember) {
		return
	}
	handler.internalError(c, errFavouriteListHasNoSerializer)
}

// errFavouriteListHasNoSerializer names the assertion DRF raises for that viewset.
var errFavouriteListHasNoSerializer = errNoSerializer{}

type errNoSerializer struct{}

func (errNoSerializer) Error() string {
	return "cycle favourites: the viewset declares no serializer_class, so DRF cannot list them"
}

// cycleFavoriteCreate marks a cycle as one of the caller's own. It writes unconditionally, so favouriting twice is a constraint violation rather than a no-op.
func (handler *Handler) cycleFavoriteCreate(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember) {
		return
	}
	var request struct {
		Cycle *string `json:"cycle"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		handler.invalidDetail(c)
		return
	}
	if !handler.createFavorite(c, user, "cycle", request.Cycle) {
		return
	}
	c.Status(http.StatusNoContent)
}

func (handler *Handler) cycleFavoriteDestroy(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember) {
		return
	}
	handler.destroyFavorite(c, user, "cycle", c.Param("cycle"))
}

// createFavorite writes a favourite of any entity type. It writes unconditionally, so favouriting twice hits the partial unique index and is answered as a bad payload.
func (handler *Handler) createFavorite(c *gin.Context, user *auth.User, entityType string, entityID *string) bool {
	slug, projectID := c.Param("slug"), c.Param("id")
	var workspaceIDs []string
	err := handler.db.WithContext(c.Request.Context()).Table("workspaces").
		Where("slug = ?", slug).Limit(1).Pluck("id", &workspaceIDs).Error
	if err != nil {
		handler.internalError(c, err)
		return false
	}
	if len(workspaceIDs) == 0 {
		handler.notFound(c)
		return false
	}
	favoriteID, err := newUUID()
	if err != nil {
		handler.internalError(c, err)
		return false
	}
	now := handler.clock().UTC()
	err = handler.db.WithContext(c.Request.Context()).Create(&UserFavorite{
		ID: favoriteID, CreatedAt: now, UpdatedAt: now, CreatedByID: &user.ID, UpdatedByID: &user.ID,
		ProjectID: &projectID, WorkspaceID: workspaceIDs[0], UserID: user.ID,
		EntityType: entityType, EntityIdentifier: entityID, Sequence: 65535,
	}).Error
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "The payload is not valid"})
		return false
	}
	return true
}

// destroyFavorite removes the row outright rather than soft deleting it, which is what delete(soft=False) does.
func (handler *Handler) destroyFavorite(c *gin.Context, user *auth.User, entityType, entityID string) {
	result := handler.db.WithContext(c.Request.Context()).
		Where(`project_id = ? AND entity_type = ? AND user_id = ? AND entity_identifier = ?
			AND workspace_id = (SELECT id FROM workspaces WHERE slug = ?) AND deleted_at IS NULL`,
			c.Param("id"), entityType, user.ID, entityID, c.Param("slug")).
		Delete(&UserFavorite{})
	if result.Error != nil {
		handler.internalError(c, result.Error)
		return
	}
	if result.RowsAffected == 0 {
		// Django's unguarded .get raises DoesNotExist.
		c.JSON(http.StatusNotFound, gin.H{"error": "The required object does not exist."})
		return
	}
	c.Status(http.StatusNoContent)
}

func (handler *Handler) cycleUserProperties(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	properties, err := handler.ensureCycleUserProperties(c, user)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, cycleUserPropertiesJSON(properties))
}

// updateCycleUserProperties answers 201 rather than 200, which is what the endpoint returns even though it creates nothing.
func (handler *Handler) updateCycleUserProperties(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	var properties CycleUserProperties
	err := handler.db.WithContext(c.Request.Context()).Table("cycle_user_properties p").
		Joins("JOIN workspaces w ON w.id = p.workspace_id").
		Where("p.user_id = ? AND p.cycle_id = ? AND p.project_id = ? AND w.slug = ? AND p.deleted_at IS NULL",
			user.ID, c.Param("cycle"), c.Param("id"), c.Param("slug")).
		Take(&properties).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// Unlike the read, the update does not create the row.
		c.JSON(http.StatusNotFound, gin.H{"error": "The required object does not exist."})
		return
	}
	if err != nil {
		handler.internalError(c, err)
		return
	}
	var body map[string]json.RawMessage
	if err := c.ShouldBindJSON(&body); err != nil {
		handler.invalidDetail(c)
		return
	}
	updates := map[string]any{}
	for _, field := range []string{"filters", "rich_filters", "display_filters", "display_properties"} {
		raw, present := body[field]
		if !present || !json.Valid(raw) {
			continue
		}
		updates[field] = auth.JSONValue(raw)
	}
	if len(updates) > 0 {
		now := handler.clock().UTC()
		updates["updated_at"] = now
		err := handler.db.WithContext(c.Request.Context()).Model(&CycleUserProperties{}).
			Where("id = ?", properties.ID).Updates(updates).Error
		if err != nil {
			handler.internalError(c, err)
			return
		}
		if err := handler.db.WithContext(c.Request.Context()).Where("id = ?", properties.ID).Take(&properties).Error; err != nil {
			handler.internalError(c, err)
			return
		}
	}
	drf.Respond(c, http.StatusCreated, cycleUserPropertiesJSON(properties))
}

// ensureCycleUserProperties is get_or_create, which the read path uses and the update path does not.
func (handler *Handler) ensureCycleUserProperties(c *gin.Context, user *auth.User) (CycleUserProperties, error) {
	slug, projectID, cycleID := c.Param("slug"), c.Param("id"), c.Param("cycle")
	var properties CycleUserProperties
	err := handler.db.WithContext(c.Request.Context()).Table("cycle_user_properties p").
		Joins("JOIN workspaces w ON w.id = p.workspace_id").
		Where("p.user_id = ? AND p.cycle_id = ? AND p.project_id = ? AND w.slug = ? AND p.deleted_at IS NULL",
			user.ID, cycleID, projectID, slug).
		Take(&properties).Error
	if err == nil {
		return properties, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return CycleUserProperties{}, err
	}
	var workspaceIDs []string
	err = handler.db.WithContext(c.Request.Context()).Table("projects").
		Where("id = ?", projectID).Limit(1).Pluck("workspace_id", &workspaceIDs).Error
	if err != nil {
		return CycleUserProperties{}, err
	}
	if len(workspaceIDs) == 0 {
		return CycleUserProperties{}, gorm.ErrRecordNotFound
	}
	propertiesID, err := newUUID()
	if err != nil {
		return CycleUserProperties{}, err
	}
	now := handler.clock().UTC()
	properties = CycleUserProperties{
		ID: propertiesID, CreatedAt: now, UpdatedAt: now,
		ProjectID: projectID, WorkspaceID: workspaceIDs[0], CycleID: cycleID, UserID: user.ID,
		Filters: defaultFiltersJSON(), DisplayFilters: defaultDisplayFiltersJSON(),
		DisplayProperties: defaultDisplayPropertiesJSON(), RichFilters: emptyJSON(),
	}
	if err := handler.db.WithContext(c.Request.Context()).Create(&properties).Error; err != nil {
		return CycleUserProperties{}, err
	}
	return properties, nil
}

// cycleUserPropertiesJSON is CycleUserPropertiesSerializer: fields = "__all__" with the four relations read-only.
func cycleUserPropertiesJSON(properties CycleUserProperties) gin.H {
	return gin.H{
		"id": properties.ID, "created_at": properties.CreatedAt, "updated_at": properties.UpdatedAt,
		"created_by": properties.CreatedByID, "updated_by": properties.UpdatedByID,
		"deleted_at": properties.DeletedAt,
		"filters":    decodeJSON(properties.Filters), "display_filters": decodeJSON(properties.DisplayFilters),
		"display_properties": decodeJSON(properties.DisplayProperties),
		"rich_filters":       decodeJSON(properties.RichFilters),
		"project":            properties.ProjectID, "workspace": properties.WorkspaceID,
		"cycle": properties.CycleID, "user": properties.UserID,
	}
}
