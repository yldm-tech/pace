package project

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"github.com/yldm-tech/pace/apps/api-go/internal/drf"
	"gorm.io/gorm"
)

func (handler *Handler) registerCycleCRUDRoutes(router gin.IRouter) {
	router.POST("/api/workspaces/:slug/projects/:id/cycles/", handler.authenticated(handler.cycleCreate))
	router.GET("/api/workspaces/:slug/projects/:id/cycles/:cycle/", handler.authenticated(handler.cycleRetrieve))
	router.PATCH("/api/workspaces/:slug/projects/:id/cycles/:cycle/", handler.authenticated(handler.cycleUpdate))
	router.DELETE("/api/workspaces/:slug/projects/:id/cycles/:cycle/", handler.authenticated(handler.cycleDestroy))
}

// cycleCreate makes a cycle. The two dates travel together: either both are given or neither is, and a cycle with neither is the draft the form offers when the dates clash.
func (handler *Handler) cycleCreate(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember) {
		return
	}
	slug, projectID := c.Param("slug"), c.Param("id")
	var body map[string]json.RawMessage
	if err := c.ShouldBindJSON(&body); err != nil {
		handler.invalidDetail(c)
		return
	}
	startGiven := presentAndNotNull(body["start_date"])
	endGiven := presentAndNotNull(body["end_date"])
	if startGiven != endGiven {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Both start date and end date are either required or are to be null"})
		return
	}

	var project Project
	err := handler.db.WithContext(c.Request.Context()).Where("id = ?", projectID).Take(&project).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "The required object does not exist."})
		return
	}
	if err != nil {
		handler.internalError(c, err)
		return
	}

	values, ok := handler.cycleFields(c, body, project, startGiven)
	if !ok {
		return
	}
	name, hasName := values["name"]
	if !hasName || name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"name": []string{"This field is required."}})
		return
	}

	cycleID, err := newUUID()
	if err != nil {
		handler.internalError(c, err)
		return
	}
	now := handler.clock().UTC()
	values["id"] = cycleID
	values["created_at"] = now
	values["updated_at"] = now
	values["created_by_id"] = user.ID
	values["project_id"] = projectID
	values["workspace_id"] = project.WorkspaceID
	// owned_by is the caller, and is read-only on the serializer so a request cannot set it.
	values["owned_by_id"] = user.ID
	if err := handler.db.WithContext(c.Request.Context()).Table("cycles").Create(values).Error; err != nil {
		handler.internalError(c, err)
		return
	}

	if handler.tasks != nil {
		requested := decodeRawFields(body)
		err := handler.tasks.PublishModelActivity(c.Request.Context(), "cycle", cycleID, requested, nil, user.ID, slug, handler.origin())
		if err != nil {
			handler.internalError(c, err)
			return
		}
	}
	handler.respondWithCycle(c, user, slug, projectID, cycleID, project, http.StatusCreated, false)
}

// cycleUpdate refuses an archived cycle outright, and a completed one unless the change is only its sort order.
func (handler *Handler) cycleUpdate(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember) {
		return
	}
	slug, projectID, cycleID := c.Param("slug"), c.Param("id"), c.Param("cycle")
	var cycle Cycle
	err := handler.db.WithContext(c.Request.Context()).Table("cycles c").
		Joins("JOIN workspaces w ON w.id = c.workspace_id").
		Where("c.id = ? AND c.project_id = ? AND w.slug = ? AND c.deleted_at IS NULL", cycleID, projectID, slug).
		Take(&cycle).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// Django reads the cycle with .first() and then touches it, so a missing one is an AttributeError and a 500.
		handler.internalError(c, errors.New("cycle update: the cycle does not exist"))
		return
	}
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if cycle.ArchivedAt != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Archived cycle cannot be updated"})
		return
	}

	var body map[string]json.RawMessage
	if err := c.ShouldBindJSON(&body); err != nil {
		handler.invalidDetail(c)
		return
	}
	now := handler.clock().UTC()
	if cycle.EndDate != nil && cycle.EndDate.Before(now) {
		if _, sortOnly := body["sort_order"]; !sortOnly {
			c.JSON(http.StatusBadRequest, gin.H{"error": "The Cycle has already been completed so it cannot be edited"})
			return
		}
		// A completed cycle may still be reordered, so everything but the sort order is dropped rather than refused.
		body = map[string]json.RawMessage{"sort_order": body["sort_order"]}
	}

	var project Project
	if err := handler.db.WithContext(c.Request.Context()).Where("id = ?", projectID).Take(&project).Error; err != nil {
		handler.internalError(c, err)
		return
	}
	snapshot, err := json.Marshal(cycleWriteJSON(cycle))
	if err != nil {
		handler.internalError(c, err)
		return
	}
	currentInstance := string(snapshot)

	// The dates are converted only when both are present in the request, which is what the write serializer's validate does.
	bothDates := presentAndNotNull(body["start_date"]) && presentAndNotNull(body["end_date"])
	values, ok := handler.cycleFields(c, body, project, bothDates)
	if !ok {
		return
	}
	if len(values) > 0 {
		values["updated_at"] = now
		values["updated_by_id"] = user.ID
		err := handler.db.WithContext(c.Request.Context()).Model(&Cycle{}).Where("id = ?", cycleID).Updates(values).Error
		if err != nil {
			handler.internalError(c, err)
			return
		}
	}

	if handler.tasks != nil {
		err := handler.tasks.PublishModelActivity(c.Request.Context(), "cycle", cycleID,
			decodeRawFields(body), &currentInstance, user.ID, slug, handler.origin())
		if err != nil {
			handler.internalError(c, err)
			return
		}
	}
	handler.respondWithCycle(c, user, slug, projectID, cycleID, project, http.StatusOK, false)
}

// cycleRetrieve returns one cycle, with the extra sub-issue count only this route carries.
func (handler *Handler) cycleRetrieve(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember) {
		return
	}
	slug, projectID, cycleID := c.Param("slug"), c.Param("id"), c.Param("cycle")
	var project Project
	err := handler.db.WithContext(c.Request.Context()).Where("id = ?", projectID).Take(&project).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "The required object does not exist."})
		return
	}
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if handler.tasks != nil {
		if err := handler.tasks.PublishRecentVisit(c.Request.Context(), "cycle", cycleID, user.ID, projectID, slug); err != nil {
			handler.internalError(c, err)
			return
		}
	}
	handler.respondWithCycle(c, user, slug, projectID, cycleID, project, http.StatusOK, true)
}

// cycleDestroy removes the cycle. Only a project admin or whoever created it may, and the favourite and the recent visit go with it.
func (handler *Handler) cycleDestroy(c *gin.Context, user *auth.User) {
	slug, projectID, cycleID := c.Param("slug"), c.Param("id"), c.Param("cycle")
	var cycle Cycle
	err := handler.db.WithContext(c.Request.Context()).Table("cycles c").
		Joins("JOIN workspaces w ON w.id = c.workspace_id").
		Where("c.id = ? AND c.project_id = ? AND w.slug = ? AND c.deleted_at IS NULL", cycleID, projectID, slug).
		Take(&cycle).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "The required object does not exist."})
		return
	}
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !handler.requireCycleAdminOrCreator(c, user, cycle) {
		return
	}

	var issueIDs []string
	err = handler.db.WithContext(c.Request.Context()).Table("cycle_issues").
		Where("cycle_id = ? AND deleted_at IS NULL", cycleID).Pluck("issue_id", &issueIDs).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	now := handler.clock().UTC()
	requested, err := json.Marshal(map[string]any{
		"cycle_id": cycleID, "cycle_name": cycle.Name, "issues": stringsOrEmptySlice(issueIDs),
	})
	if err != nil {
		handler.internalError(c, err)
		return
	}
	requestedData := string(requested)
	// The activity carries the cycle's id in the issue slot, which is how the task finds it.
	err = handler.publishIssueActivity(c, issueActivity{
		Type: "cycle.activity.deleted", RequestedData: &requestedData,
		ActorID: user.ID, IssueID: cycleID, ProjectID: projectID,
		Notification: true, Origin: handler.origin(), Epoch: now,
	})
	if err != nil {
		handler.internalError(c, err)
		return
	}

	err = handler.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&Cycle{}).Where("id = ?", cycleID).Update("deleted_at", now).Error; err != nil {
			return err
		}
		// The favourite is soft deleted; the recent visit is removed outright.
		err := tx.Model(&UserFavorite{}).
			Where("user_id = ? AND entity_type = 'cycle' AND entity_identifier = ? AND project_id = ? AND deleted_at IS NULL",
				user.ID, cycleID, projectID).Update("deleted_at", now).Error
		if err != nil {
			return err
		}
		return tx.Exec(
			`DELETE FROM user_recent_visits WHERE project_id = ? AND entity_identifier = ? AND entity_name = 'cycle'
			 AND workspace_id = (SELECT id FROM workspaces WHERE slug = ?)`,
			projectID, cycleID, slug,
		).Error
	})
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if handler.tasks != nil {
		if err := handler.tasks.PublishSoftDeleteRelatedObjects(c.Request.Context(), "db", "cycle", cycleID); err != nil {
			handler.internalError(c, err)
			return
		}
	}
	c.Status(http.StatusNoContent)
}

// respondWithCycle re-reads the cycle through the annotated queryset and renders it. The create and update responses drop cancelled_issues, which only the list carries; the retrieve adds sub_issues, which only it carries.
func (handler *Handler) respondWithCycle(c *gin.Context, user *auth.User, slug, projectID, cycleID string, project Project, status int, withSubIssues bool) {
	location, err := time.LoadLocation(project.Timezone)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	now := handler.clock().UTC()
	rows, err := handler.cycleRowsByID(c, slug, projectID, user.ID, cycleID, now, withSubIssues)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if len(rows) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Cycle not found"})
		return
	}
	data := cycleListJSON(rows[0], location)
	// cancelled_issues is in the list's projection but not in these three.
	delete(data, "cancelled_issues")
	if withSubIssues {
		data["sub_issues"] = rows[0].SubIssues
	}
	drf.Respond(c, status, data)
}

// presentAndNotNull says whether a key was supplied with a value, which is what Django's `is not None` check asks.
func presentAndNotNull(raw json.RawMessage) bool {
	return len(raw) > 0 && string(raw) != "null"
}

// stringsOrEmptySlice keeps an empty id list an array rather than a null.
func stringsOrEmptySlice(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

// cycleFields is CycleWriteSerializer's validation. Every relation is read-only, so only the plain columns can be written.
func (handler *Handler) cycleFields(c *gin.Context, body map[string]json.RawMessage, project Project, convertDates bool) (map[string]any, bool) {
	values := map[string]any{}

	if raw, present := body["name"]; present {
		value, ok := handler.stringField(c, "name", raw, 255)
		if !ok {
			return nil, false
		}
		values["name"] = value
	}
	if raw, present := body["description"]; present {
		var value string
		if json.Unmarshal(raw, &value) != nil {
			c.JSON(http.StatusBadRequest, gin.H{"description": []string{"Not a valid string."}})
			return nil, false
		}
		values["description"] = value
	}
	for _, field := range []string{"view_props", "progress_snapshot", "logo_props"} {
		raw, present := body[field]
		if !present {
			continue
		}
		if !json.Valid(raw) || string(raw) == "null" {
			c.JSON(http.StatusBadRequest, gin.H{field: []string{"This field may not be null."}})
			return nil, false
		}
		values[field] = auth.JSONValue(raw)
	}
	if raw, present := body["sort_order"]; present {
		var value float64
		if json.Unmarshal(raw, &value) != nil {
			c.JSON(http.StatusBadRequest, gin.H{"sort_order": []string{"A valid number is required."}})
			return nil, false
		}
		values["sort_order"] = value
	}
	for _, field := range []string{"external_source", "external_id", "timezone"} {
		raw, present := body[field]
		if !present {
			continue
		}
		if string(raw) == "null" {
			values[field] = nil
			continue
		}
		var value string
		if json.Unmarshal(raw, &value) != nil {
			c.JSON(http.StatusBadRequest, gin.H{field: []string{"Not a valid string."}})
			return nil, false
		}
		values[field] = value
	}

	if !convertDates {
		// A request that supplies neither date, or only one, writes neither: the serializer converts them as a pair or not at all.
		return values, true
	}
	start, startOK := cycleDateFromRaw(body["start_date"])
	end, endOK := cycleDateFromRaw(body["end_date"])
	if !startOK || !endOK {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Please provide valid detail"})
		return nil, false
	}
	if start.After(end) {
		c.JSON(http.StatusBadRequest, gin.H{"non_field_errors": []string{"Start date cannot exceed end date"}})
		return nil, false
	}
	// The pair goes through convert_to_utc, which anchors each to the project's own day.
	startAt, endAt, ok := cycleInterval(start.Format("2006-01-02"), end.Format("2006-01-02"), project.Timezone, handler.clock().UTC())
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Please provide valid detail"})
		return nil, false
	}
	values["start_date"] = startAt
	values["end_date"] = endAt
	return values, true
}

// cycleDateFromRaw reads a datetime the way the serializer's DateTimeField does, accepting a bare date as well as an instant.
func cycleDateFromRaw(raw json.RawMessage) (time.Time, bool) {
	var text string
	if json.Unmarshal(raw, &text) != nil {
		return time.Time{}, false
	}
	parsed, ok := parseDjangoDateTime(text)
	return parsed, ok
}

// cycleWriteJSON is CycleSerializer over the instance, which is what the update snapshot carries. The annotated counts are absent from it, since the instance carries none.
func cycleWriteJSON(cycle Cycle) gin.H {
	return gin.H{
		"id": cycle.ID, "created_at": cycle.CreatedAt, "updated_at": cycle.UpdatedAt,
		"created_by": cycle.CreatedByID, "updated_by": cycle.UpdatedByID, "deleted_at": cycle.DeletedAt,
		"name": cycle.Name, "description": cycle.Description,
		"start_date": cycle.StartDate, "end_date": cycle.EndDate,
		"view_props": decodeJSON(cycle.ViewProps), "sort_order": cycle.SortOrder,
		"external_source": cycle.ExternalSource, "external_id": cycle.ExternalID,
		"progress_snapshot": decodeJSON(cycle.ProgressSnapshot), "archived_at": cycle.ArchivedAt,
		"logo_props": decodeJSON(cycle.LogoProps), "timezone": cycle.Timezone, "version": cycle.Version,
		"project": cycle.ProjectID, "workspace": cycle.WorkspaceID, "owned_by": cycle.OwnedByID,
	}
}

// requireCycleAdminOrCreator is allow_permission([ADMIN], creator=True, model=Cycle).
func (handler *Handler) requireCycleAdminOrCreator(c *gin.Context, user *auth.User, cycle Cycle) bool {
	member, found, err := handler.activeProjectMember(c.Request.Context(), c.Param("slug"), c.Param("id"), user.ID)
	if err != nil {
		handler.internalError(c, err)
		return false
	}
	if found {
		if cycle.CreatedByID != nil && *cycle.CreatedByID == user.ID {
			return true
		}
		if member.Role == roleAdmin {
			return true
		}
		workspaceRole, _, err := handler.workspaceMemberRole(c.Request.Context(), c.Param("slug"), user.ID)
		if err != nil {
			handler.internalError(c, err)
			return false
		}
		if workspaceRole == roleAdmin {
			return true
		}
	}
	c.JSON(http.StatusForbidden, gin.H{"error": "You don't have the required permissions."})
	return false
}
