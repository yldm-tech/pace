package project

import (
	"context"
	"database/sql"
	"encoding/json"
	"math/rand"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api/internal/auth"
	"github.com/yldm-tech/pace/apps/api/internal/drf"
	"gorm.io/gorm"
)

func (handler *Handler) registerEstimateRoutes(router gin.IRouter) {
	const base = "/api/workspaces/:slug/projects/:id/estimates/"
	router.GET("/api/workspaces/:slug/projects/:id/project-estimates/", handler.authenticated(handler.projectEstimatePoints))
	router.GET(base, handler.authenticated(handler.estimateList))
	router.POST(base, handler.authenticated(handler.estimateCreate))
	router.GET(base+":estimate/", handler.authenticated(handler.estimateRetrieve))
	router.PATCH(base+":estimate/", handler.authenticated(handler.estimateUpdate))
	router.DELETE(base+":estimate/", handler.authenticated(handler.estimateDestroy))
	router.POST(base+":estimate/estimate-points/", handler.authenticated(handler.estimatePointCreate))
	router.PATCH(base+":estimate/estimate-points/:point/", handler.authenticated(handler.estimatePointUpdate))
	router.DELETE(base+":estimate/estimate-points/:point/", handler.authenticated(handler.estimatePointDestroy))
}

// Estimate is the db.Estimate table: a named scale a project measures work with.
type Estimate struct {
	ID          string     `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt   time.Time  `gorm:"column:created_at"`
	UpdatedAt   time.Time  `gorm:"column:updated_at"`
	CreatedByID *string    `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID *string    `gorm:"column:updated_by_id;type:uuid"`
	DeletedAt   *time.Time `gorm:"column:deleted_at"`
	ProjectID   string     `gorm:"column:project_id;type:uuid"`
	WorkspaceID string     `gorm:"column:workspace_id;type:uuid"`
	Name        string     `gorm:"column:name"`
	Description string     `gorm:"column:description"`
	Type        string     `gorm:"column:type"`
	LastUsed    bool       `gorm:"column:last_used"`
}

func (Estimate) TableName() string { return "estimates" }

// EstimatePoint is one step of a scale.
type EstimatePoint struct {
	ID          string     `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt   time.Time  `gorm:"column:created_at"`
	UpdatedAt   time.Time  `gorm:"column:updated_at"`
	CreatedByID *string    `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID *string    `gorm:"column:updated_by_id;type:uuid"`
	DeletedAt   *time.Time `gorm:"column:deleted_at"`
	ProjectID   string     `gorm:"column:project_id;type:uuid"`
	WorkspaceID string     `gorm:"column:workspace_id;type:uuid"`
	EstimateID  string     `gorm:"column:estimate_id;type:uuid"`
	Key         int        `gorm:"column:key"`
	Description string     `gorm:"column:description"`
	Value       string     `gorm:"column:value"`
}

func (EstimatePoint) TableName() string { return "estimate_points" }

// projectEstimatePoints returns the points of the scale the project is using, and an **empty list** when it uses none — not a null and not a 404.
func (handler *Handler) projectEstimatePoints(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember) {
		return
	}
	// []sql.NullString rather than []*string: Pluck does not honour a pointer element type, so a null column ends the request with `converting NULL to string is unsupported` -- which is every first time, before the row has ever been set.
	var estimateIDs []sql.NullString
	err := handler.db.WithContext(c.Request.Context()).Table("projects p").
		Joins("JOIN workspaces w ON w.id = p.workspace_id").
		Where("w.slug = ? AND p.id = ?", c.Param("slug"), c.Param("id")).
		Limit(1).Pluck("p.estimate_id", &estimateIDs).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if len(estimateIDs) == 0 {
		// Django's unguarded get raises when the project is not there.
		handler.notFound(c)
		return
	}
	if !estimateIDs[0].Valid {
		drf.Respond(c, http.StatusOK, []gin.H{})
		return
	}
	points, err := handler.estimatePoints(c, estimateIDs[0].String)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, estimatePointsJSON(points))
}

// estimateList returns the project's scales with their points nested inside.
func (handler *Handler) estimateList(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	var estimates []Estimate
	err := handler.db.WithContext(c.Request.Context()).Table("estimates e").Select("e.*").
		Joins("JOIN workspaces w ON w.id = e.workspace_id").
		Where("w.slug = ? AND e.project_id = ? AND e.deleted_at IS NULL", c.Param("slug"), c.Param("id")).
		Order("e.created_at").Scan(&estimates).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	results := make([]gin.H, 0, len(estimates))
	for _, estimate := range estimates {
		points, err := handler.estimatePoints(c, estimate.ID)
		if err != nil {
			handler.internalError(c, err)
			return
		}
		results = append(results, estimateReadJSON(estimate, points))
	}
	drf.Respond(c, http.StatusOK, results)
}

func (handler *Handler) estimateRetrieve(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	estimate, found, err := handler.estimateByID(c)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		handler.notFound(c)
		return
	}
	points, err := handler.estimatePoints(c, estimate.ID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, estimateReadJSON(estimate, points))
}

// estimateCreate makes a scale and its points in one call.
//
// A payload with no `estimate` object at all is a **500**: Django reads the name off it without checking that it is there. A payload that has one but no name gets a **random ten-letter name**, which is the only place in this API that names something for the caller.
func (handler *Handler) estimateCreate(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember) {
		return
	}
	var request struct {
		Estimate *struct {
			Name     *string `json:"name"`
			Type     *string `json:"type"`
			LastUsed *bool   `json:"last_used"`
		} `json:"estimate"`
		EstimatePoints []map[string]any `json:"estimate_points"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		handler.invalidDetail(c)
		return
	}
	if request.Estimate == nil {
		handler.internalError(c, errEstimatePayloadHasNoEstimate)
		return
	}
	name := randomEstimateName()
	if request.Estimate.Name != nil {
		name = *request.Estimate.Name
	}
	estimateType := "categories"
	if request.Estimate.Type != nil {
		estimateType = *request.Estimate.Type
	}
	lastUsed := false
	if request.Estimate.LastUsed != nil {
		lastUsed = *request.Estimate.LastUsed
	}
	// Every point needs a value, which is the one field its serializer requires.
	failures := []gin.H{}
	refused := false
	for _, point := range request.EstimatePoints {
		value, given := point["value"].(string)
		if !given || value == "" {
			failures = append(failures, gin.H{"value": []string{"This field is required."}})
			refused = true
			continue
		}
		failures = append(failures, gin.H{})
	}
	if refused {
		c.JSON(http.StatusBadRequest, failures)
		return
	}

	projectID := c.Param("id")
	var workspaceIDs []string
	err := handler.db.WithContext(c.Request.Context()).Table("projects").
		Where("id = ?", projectID).Limit(1).Pluck("workspace_id", &workspaceIDs).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if len(workspaceIDs) == 0 {
		handler.notFound(c)
		return
	}
	now := handler.clock().UTC()
	estimateID, err := newUUID()
	if err != nil {
		handler.internalError(c, err)
		return
	}
	// The scale itself records nobody as its author: it is written without the current user, unlike its points.
	estimate := Estimate{
		ID: estimateID, CreatedAt: now, UpdatedAt: now,
		ProjectID: projectID, WorkspaceID: workspaceIDs[0],
		Name: name, Type: estimateType, LastUsed: lastUsed,
	}
	points := make([]EstimatePoint, 0, len(request.EstimatePoints))
	for _, point := range request.EstimatePoints {
		pointID, err := newUUID()
		if err != nil {
			handler.internalError(c, err)
			return
		}
		value, _ := point["value"].(string)
		description, _ := point["description"].(string)
		key := 0
		if number, ok := point["key"].(float64); ok {
			key = int(number)
		}
		points = append(points, EstimatePoint{
			ID: pointID, CreatedAt: now, UpdatedAt: now, CreatedByID: &user.ID, UpdatedByID: &user.ID,
			ProjectID: projectID, WorkspaceID: workspaceIDs[0], EstimateID: estimateID,
			Key: key, Value: value, Description: description,
		})
	}
	err = handler.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&estimate).Error; err != nil {
			return err
		}
		if len(points) == 0 {
			return nil
		}
		return tx.Clauses(onConflictDoNothing()).Create(&points).Error
	})
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if err := handler.invalidateWorkspaceEstimates(c.Request.Context(), c.Param("slug")); err != nil {
		handler.internalError(c, err)
		return
	}
	// The answer reads the points back out of the payload rather than the database, so a point dropped by a conflict is still reported.
	drf.Respond(c, http.StatusOK, estimateReadJSON(estimate, points))
}

var errEstimatePayloadHasNoEstimate = &estimateError{"the payload carries no estimate object, which Django reads without checking"}

type estimateError struct{ message string }

func (err *estimateError) Error() string { return err.message }

// estimateUpdate edits a scale and the points named in the payload. It refuses a payload with no points even when it only means to rename the scale.
func (handler *Handler) estimateUpdate(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember) {
		return
	}
	var request struct {
		Estimate *struct {
			Name *string `json:"name"`
			Type *string `json:"type"`
		} `json:"estimate"`
		EstimatePoints []map[string]any `json:"estimate_points"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		handler.invalidDetail(c)
		return
	}
	if len(request.EstimatePoints) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Estimate points are required"})
		return
	}
	estimate, found, err := handler.estimateByID(c)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		handler.notFound(c)
		return
	}
	now := handler.clock().UTC()
	if request.Estimate != nil {
		updates := map[string]any{"updated_at": now}
		if request.Estimate.Name != nil {
			updates["name"] = *request.Estimate.Name
			estimate.Name = *request.Estimate.Name
		}
		if request.Estimate.Type != nil {
			updates["type"] = *request.Estimate.Type
			estimate.Type = *request.Estimate.Type
		}
		err := handler.db.WithContext(c.Request.Context()).Model(&Estimate{}).
			Where("id = ?", estimate.ID).Updates(updates).Error
		if err != nil {
			handler.internalError(c, err)
			return
		}
	}

	// Only the points that already belong to this scale are touched, and only their key and value move.
	wanted := map[string]map[string]any{}
	identifiers := []string{}
	for _, point := range request.EstimatePoints {
		identifier, given := point["id"].(string)
		if !given {
			continue
		}
		wanted[identifier] = point
		identifiers = append(identifiers, identifier)
	}
	if len(identifiers) > 0 {
		points, err := handler.estimatePointsByID(c, estimate.ID, identifiers)
		if err != nil {
			handler.internalError(c, err)
			return
		}
		err = handler.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
			for _, point := range points {
				changes := map[string]any{}
				if value, ok := wanted[point.ID]["value"].(string); ok {
					changes["value"] = value
				}
				if number, ok := wanted[point.ID]["key"].(float64); ok {
					changes["key"] = int(number)
				}
				if len(changes) == 0 {
					continue
				}
				// A bulk update writes the named columns and nothing else, so no timestamp moves.
				if err := tx.Model(&EstimatePoint{}).Where("id = ?", point.ID).Updates(changes).Error; err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			handler.internalError(c, err)
			return
		}
	}
	if err := handler.invalidateWorkspaceEstimates(c.Request.Context(), c.Param("slug")); err != nil {
		handler.internalError(c, err)
		return
	}
	points, err := handler.estimatePoints(c, estimate.ID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, estimateReadJSON(estimate, points))
}

func (handler *Handler) estimateDestroy(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember) {
		return
	}
	estimate, found, err := handler.estimateByID(c)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		handler.notFound(c)
		return
	}
	now := handler.clock().UTC()
	err = handler.db.WithContext(c.Request.Context()).Model(&Estimate{}).Where("id = ?", estimate.ID).
		Updates(map[string]any{"deleted_at": now, "updated_at": now}).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if handler.tasks != nil {
		if err := handler.tasks.PublishSoftDeleteRelatedObjects(c.Request.Context(), "db", "estimate", estimate.ID); err != nil {
			handler.internalError(c, err)
			return
		}
	}
	if err := handler.invalidateWorkspaceEstimates(c.Request.Context(), c.Param("slug")); err != nil {
		handler.internalError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// estimatePointCreate adds one step to a scale.
//
// Both the key and the value have to be **truthy**, so a key of zero is refused even though zero is where a scale starts.
func (handler *Handler) estimatePointCreate(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember) {
		return
	}
	var payload map[string]any
	if err := c.ShouldBindJSON(&payload); err != nil {
		handler.invalidDetail(c)
		return
	}
	key, keyGiven := payload["key"].(float64)
	value, valueGiven := payload["value"].(string)
	if !keyGiven || key == 0 || !valueGiven || value == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Key and value are required"})
		return
	}
	estimate, found, err := handler.estimateByID(c)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "Estimate not found"})
		return
	}
	pointID, err := newUUID()
	if err != nil {
		handler.internalError(c, err)
		return
	}
	now := handler.clock().UTC()
	// Like the scale it belongs to, a point made here records nobody as its author.
	point := EstimatePoint{
		ID: pointID, CreatedAt: now, UpdatedAt: now,
		ProjectID: c.Param("id"), WorkspaceID: estimate.WorkspaceID, EstimateID: estimate.ID,
		Key: int(key), Value: value,
	}
	if err := handler.db.WithContext(c.Request.Context()).Create(&point).Error; err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, estimatePointJSON(point))
}

func (handler *Handler) estimatePointUpdate(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember) {
		return
	}
	point, found, err := handler.estimatePointByID(c)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		handler.notFound(c)
		return
	}
	var payload map[string]json.RawMessage
	if err := c.ShouldBindJSON(&payload); err != nil {
		handler.invalidDetail(c)
		return
	}
	updates := map[string]any{}
	if raw, given := payload["value"]; given {
		var value string
		if json.Unmarshal(raw, &value) != nil {
			c.JSON(http.StatusBadRequest, gin.H{"value": []string{"Not a valid string."}})
			return
		}
		if value == "" {
			c.JSON(http.StatusBadRequest, gin.H{"value": []string{"This field may not be blank."}})
			return
		}
		updates["value"] = value
		point.Value = value
	}
	if raw, given := payload["key"]; given {
		var key int
		if json.Unmarshal(raw, &key) != nil {
			c.JSON(http.StatusBadRequest, gin.H{"key": []string{"A valid integer is required."}})
			return
		}
		updates["key"] = key
		point.Key = key
	}
	if raw, given := payload["description"]; given {
		var description string
		if json.Unmarshal(raw, &description) != nil {
			c.JSON(http.StatusBadRequest, gin.H{"description": []string{"Not a valid string."}})
			return
		}
		updates["description"] = description
		point.Description = description
	}
	if len(updates) > 0 {
		now := handler.clock().UTC()
		updates["updated_at"] = now
		updates["updated_by_id"] = user.ID
		point.UpdatedAt = now
		point.UpdatedByID = &user.ID
		err := handler.db.WithContext(c.Request.Context()).Model(&EstimatePoint{}).
			Where("id = ?", point.ID).Updates(updates).Error
		if err != nil {
			handler.internalError(c, err)
			return
		}
	}
	drf.Respond(c, http.StatusOK, estimatePointJSON(point))
}

// estimatePointDestroy removes one step and closes the gap it leaves.
//
// The work items that used it are moved to whichever point `new_estimate_id` names, or left pointing at nothing when it names none — and either way each of them gets an activity of its own. The answer is the points whose **key moved**, not the one that was deleted.
func (handler *Handler) estimatePointDestroy(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember) {
		return
	}
	var request struct {
		NewEstimateID *string `json:"new_estimate_id"`
	}
	_ = c.ShouldBindJSON(&request)

	slug, projectID := c.Param("slug"), c.Param("id")
	points, err := handler.estimatePoints(c, c.Param("estimate"))
	if err != nil {
		handler.internalError(c, err)
		return
	}

	var issues []Issue
	err = handler.db.WithContext(c.Request.Context()).Table("issues i").Select("i.*").
		Joins("JOIN workspaces w ON w.id = i.workspace_id").
		Where("w.slug = ? AND i.project_id = ? AND i.estimate_point_id = ? AND i.deleted_at IS NULL",
			slug, projectID, c.Param("point")).
		Scan(&issues).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	now := handler.clock().UTC()
	for _, issue := range issues {
		requested := `{"estimate_point": null}`
		if request.NewEstimateID != nil {
			requested = `{"estimate_point": "` + *request.NewEstimateID + `"}`
		}
		current := `{"estimate_point": null}`
		if issue.EstimatePointID != nil {
			current = `{"estimate_point": "` + *issue.EstimatePointID + `"}`
		}
		// The activity names no notification and no origin, so the task falls back to its own defaults.
		err := handler.publishIssueActivity(c, issueActivity{
			Type: "issue.activity.updated", RequestedData: &requested, CurrentInstance: &current,
			ActorID: user.ID, IssueID: issue.ID, ProjectID: projectID, Epoch: now,
		})
		if err != nil {
			handler.internalError(c, err)
			return
		}
	}
	// The move itself happens only when a new point was named, and it moves **every** work item that used the old one at once.
	if request.NewEstimateID != nil && len(issues) > 0 {
		err := handler.db.WithContext(c.Request.Context()).Table("issues").
			Where(`project_id = ? AND estimate_point_id = ? AND workspace_id = (SELECT id FROM workspaces WHERE slug = ?)`,
				projectID, c.Param("point"), slug).
			Update("estimate_point_id", *request.NewEstimateID).Error
		if err != nil {
			handler.internalError(c, err)
			return
		}
	}

	removed, found, err := handler.estimatePointByID(c)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "Estimate point not found"})
		return
	}
	moved := []EstimatePoint{}
	for _, point := range points {
		if point.Key > removed.Key {
			point.Key--
			moved = append(moved, point)
		}
	}
	err = handler.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		for _, point := range moved {
			if err := tx.Model(&EstimatePoint{}).Where("id = ?", point.ID).Update("key", point.Key).Error; err != nil {
				return err
			}
		}
		return tx.Model(&EstimatePoint{}).Where("id = ?", removed.ID).
			Updates(map[string]any{"deleted_at": now, "updated_at": now}).Error
	})
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if handler.tasks != nil {
		if err := handler.tasks.PublishSoftDeleteRelatedObjects(c.Request.Context(), "db", "estimatepoint", removed.ID); err != nil {
			handler.internalError(c, err)
			return
		}
	}
	drf.Respond(c, http.StatusOK, estimatePointsJSON(moved))
}

func (handler *Handler) estimateByID(c *gin.Context) (Estimate, bool, error) {
	var estimates []Estimate
	err := handler.db.WithContext(c.Request.Context()).Table("estimates e").Select("e.*").
		Joins("JOIN workspaces w ON w.id = e.workspace_id").
		Where("w.slug = ? AND e.project_id = ? AND e.id = ? AND e.deleted_at IS NULL",
			c.Param("slug"), c.Param("id"), c.Param("estimate")).
		Limit(1).Scan(&estimates).Error
	if err != nil || len(estimates) == 0 {
		return Estimate{}, false, err
	}
	return estimates[0], true, nil
}

func (handler *Handler) estimatePointByID(c *gin.Context) (EstimatePoint, bool, error) {
	var points []EstimatePoint
	err := handler.db.WithContext(c.Request.Context()).Table("estimate_points ep").Select("ep.*").
		Joins("JOIN workspaces w ON w.id = ep.workspace_id").
		Where("w.slug = ? AND ep.project_id = ? AND ep.estimate_id = ? AND ep.id = ? AND ep.deleted_at IS NULL",
			c.Param("slug"), c.Param("id"), c.Param("estimate"), c.Param("point")).
		Limit(1).Scan(&points).Error
	if err != nil || len(points) == 0 {
		return EstimatePoint{}, false, err
	}
	return points[0], true, nil
}

func (handler *Handler) estimatePoints(c *gin.Context, estimateID string) ([]EstimatePoint, error) {
	var points []EstimatePoint
	err := handler.db.WithContext(c.Request.Context()).Table("estimate_points ep").Select("ep.*").
		Joins("JOIN workspaces w ON w.id = ep.workspace_id").
		Where("w.slug = ? AND ep.project_id = ? AND ep.estimate_id = ? AND ep.deleted_at IS NULL",
			c.Param("slug"), c.Param("id"), estimateID).
		Order("ep.key").Scan(&points).Error
	return points, err
}

func (handler *Handler) estimatePointsByID(c *gin.Context, estimateID string, identifiers []string) ([]EstimatePoint, error) {
	var points []EstimatePoint
	err := handler.db.WithContext(c.Request.Context()).Table("estimate_points ep").Select("ep.*").
		Joins("JOIN workspaces w ON w.id = ep.workspace_id").
		Where("w.slug = ? AND ep.project_id = ? AND ep.estimate_id = ? AND ep.id IN ? AND ep.deleted_at IS NULL",
			c.Param("slug"), c.Param("id"), estimateID, identifiers).
		Order("ep.key").Scan(&points).Error
	return points, err
}

// invalidateWorkspaceEstimates drops the cached workspace estimate list, which three of these routes do.
func (handler *Handler) invalidateWorkspaceEstimates(ctx context.Context, slug string) error {
	if handler.cache == nil {
		return nil
	}
	return handler.cache.InvalidatePattern(ctx, "*/api/workspaces/"+slug+"/estimates/*")
}

// randomEstimateName is the ten lowercase letters an unnamed scale is given.
func randomEstimateName() string {
	const letters = "abcdefghijklmnopqrstuvwxyz"
	name := make([]byte, 10)
	for index := range name {
		name[index] = letters[rand.Intn(len(letters))]
	}
	return string(name)
}

// estimateReadJSON is EstimateReadSerializer: the scale with its points nested inside.
func estimateReadJSON(estimate Estimate, points []EstimatePoint) gin.H {
	return gin.H{
		"id": estimate.ID, "points": estimatePointsJSON(points),
		"created_at": estimate.CreatedAt, "updated_at": estimate.UpdatedAt, "deleted_at": estimate.DeletedAt,
		"name": estimate.Name, "description": estimate.Description, "type": estimate.Type,
		"last_used":  estimate.LastUsed,
		"created_by": estimate.CreatedByID, "updated_by": estimate.UpdatedByID,
		"project": estimate.ProjectID, "workspace": estimate.WorkspaceID,
	}
}

// estimatePointJSON is EstimatePointSerializer: twelve fields, with the key an integer and the value a string.
func estimatePointJSON(point EstimatePoint) gin.H {
	return gin.H{
		"id": point.ID, "created_at": point.CreatedAt, "updated_at": point.UpdatedAt,
		"deleted_at": point.DeletedAt, "key": point.Key, "description": point.Description,
		"value": point.Value, "created_by": point.CreatedByID, "updated_by": point.UpdatedByID,
		"project": point.ProjectID, "workspace": point.WorkspaceID, "estimate": point.EstimateID,
	}
}

func estimatePointsJSON(points []EstimatePoint) []gin.H {
	results := make([]gin.H, 0, len(points))
	for _, point := range points {
		results = append(results, estimatePointJSON(point))
	}
	return results
}
