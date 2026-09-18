package externalapi

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api/internal/access"
	"github.com/yldm-tech/pace/apps/api/internal/auth"
	"github.com/yldm-tech/pace/apps/api/internal/cycles"
	"github.com/yldm-tech/pace/apps/api/internal/drf"
	"gorm.io/gorm"
)

func (handler *Handler) registerCycleCrudRoutes(router gin.IRouter) {
	const base = "/api/v1/workspaces/:slug/projects/:project/cycles/"
	router.GET(base, handler.authenticated(handler.cycleList))
	router.POST(base, handler.authenticated(handler.cycleCreate))
	router.GET(base+":cycle/", handler.authenticated(handler.cycleRetrieve))
	router.PATCH(base+":cycle/", handler.authenticated(handler.cycleUpdate))
	router.DELETE(base+":cycle/", handler.authenticated(handler.cycleDestroy))
}

// cycleList returns a project's live cycles.
//
// cycle_view narrows the list, and `current` is the one value that answers a **plain list** rather than a page — every other value, including a value nobody recognises, comes back in the paginated envelope.
//
// The queryset annotates the six counts and not the three estimates, so the estimate keys are absent here while the archived list carries them. A read-only field with nothing behind it is dropped by DRF rather than rendered as null.
func (handler *Handler) cycleList(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireProjectMember(c, user, c.Request.Method) {
		return
	}
	if _, found, err := handler.projectByID(c); err != nil {
		handler.serverError(c, err)
		return
	} else if !found {
		notFound(c)
		return
	}

	now := handler.clock().UTC()
	query := handler.cycleScope(c, user).Where("c.archived_at IS NULL")
	view := c.Query("cycle_view")
	switch view {
	case "current":
		query = query.Where("c.start_date <= ? AND c.end_date >= ?", now, now)
	case "upcoming":
		query = query.Where("c.start_date > ?", now)
	case "completed":
		query = query.Where("c.end_date < ?", now)
	case "draft":
		query = query.Where("c.end_date IS NULL AND c.start_date IS NULL")
	case "incomplete":
		query = query.Where("c.end_date >= ? OR c.end_date IS NULL", now)
	}

	var rows []cycleRow
	err := query.Select(externalCycleCountAnnotations()).Group("c.id").Order("c.created_at DESC").Scan(&rows).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	fields := requestedFields(c)
	results := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		results = append(results, narrow(cycleListJSON(row), fields))
	}
	if view == "current" {
		drf.Respond(c, http.StatusOK, results)
		return
	}
	handler.respondPaged(c, results)
}

// cycleRetrieve returns one live cycle. An archived one is a 404 here, because the queryset it is read through hides them.
func (handler *Handler) cycleRetrieve(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireProjectMember(c, user, c.Request.Method) {
		return
	}
	if _, found, err := handler.projectByID(c); err != nil {
		handler.serverError(c, err)
		return
	} else if !found {
		notFound(c)
		return
	}
	var rows []cycleRow
	err := handler.cycleScope(c, user).Where("c.archived_at IS NULL AND c.id = ?", c.Param("cycle")).
		Select(externalCycleCountAnnotations()).Group("c.id").Limit(1).Scan(&rows).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if len(rows) == 0 {
		notFound(c)
		return
	}
	drf.Respond(c, http.StatusOK, narrow(cycleListJSON(rows[0]), requestedFields(c)))
}

// cycleScope is the queryset the list and the detail share: the project's cycles, and only while the caller is an active member of it.
func (handler *Handler) cycleScope(c *gin.Context, user *auth.User) *gorm.DB {
	return handler.db.WithContext(c.Request.Context()).Table("cycles c").
		Joins("JOIN workspaces w ON w.id = c.workspace_id").
		Joins(access.MemberJoin("c", "project_id"), user.ID).
		Joins("LEFT JOIN cycle_issues ci ON ci.cycle_id = c.id").
		Joins("LEFT JOIN issues ii ON ii.id = ci.issue_id").
		Where("w.slug = ? AND c.project_id = ? AND c.deleted_at IS NULL", c.Param("slug"), c.Param("project"))
}

// externalCycleCountAnnotations is the six counts the list carries, and no estimates.
func externalCycleCountAnnotations() string {
	const liveIssues = `ci.deleted_at IS NULL AND ii.archived_at IS NULL AND ii.is_draft = FALSE`
	stateCount := func(group string) string {
		return `COUNT(*) FILTER (WHERE ` + liveIssues +
			` AND (SELECT s.group FROM states s WHERE s.id = ii.state_id) = '` + group + `')`
	}
	return `c.*,
		COUNT(*) FILTER (WHERE ` + liveIssues + `) AS total_issues,
		` + stateCount("cancelled") + ` AS cancelled_issues,
		` + stateCount("completed") + ` AS completed_issues,
		` + stateCount("started") + ` AS started_issues,
		` + stateCount("unstarted") + ` AS unstarted_issues,
		` + stateCount("backlog") + ` AS backlog_issues`
}

// cycleCreate makes a cycle.
//
// The two dates go together: both or neither. A cycle with one of them is refused before the serializer is ever built, which is why that message is worded from the outside rather than as a field error.
func (handler *Handler) cycleCreate(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireProjectMember(c, user, c.Request.Method) {
		return
	}
	var body map[string]json.RawMessage
	if err := c.ShouldBindJSON(&body); err != nil {
		invalidPayload(c)
		return
	}
	_, hasStart := nonNullField(body, "start_date")
	_, hasEnd := nonNullField(body, "end_date")
	if hasStart != hasEnd {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Both start date and end date are either required or are to be null",
		})
		return
	}

	project, found, err := handler.projectByID(c)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !found {
		// The serializer looks the project up itself and reports it as a plain error rather than a 404.
		c.JSON(http.StatusBadRequest, gin.H{"non_field_errors": []string{"Project not found"}})
		return
	}
	input, failures := handler.validateExternalCycle(c, body, project, false)
	if failures != nil {
		c.JSON(http.StatusBadRequest, failures)
		return
	}

	slug, projectID := c.Param("slug"), c.Param("project")
	externalID, externalSource := payloadString(body, "external_id"), payloadString(body, "external_source")
	if externalID != "" && externalSource != "" {
		existing, err := handler.cycleByExternalPair(c, projectID, externalSource, externalID)
		if err != nil {
			handler.serverError(c, err)
			return
		}
		if existing != "" {
			c.JSON(http.StatusConflict, gin.H{
				"error": "Cycle with the same external id and external source already exists",
				"id":    existing,
			})
			return
		}
	}

	now := handler.clock().UTC()
	cycleID, err := newUUID()
	if err != nil {
		handler.serverError(c, err)
		return
	}
	values := map[string]any{"id": cycleID}
	for key, value := range input {
		values[key] = value
	}
	values["created_at"] = now
	values["updated_at"] = now
	values["created_by_id"] = user.ID
	values["updated_by_id"] = user.ID
	values["project_id"] = projectID
	values["workspace_id"] = project.WorkspaceID
	if _, given := values["owned_by_id"]; !given {
		values["owned_by_id"] = user.ID
	}
	err = handler.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		// A new cycle is placed **ahead** of the ones already there, which is the opposite of how a new work item is placed.
		var smallest *float64
		err := tx.Table("cycles").Where("project_id = ? AND deleted_at IS NULL", projectID).
			Select("MIN(sort_order)").Scan(&smallest).Error
		if err != nil {
			return err
		}
		values["sort_order"] = 65535.0
		if smallest != nil {
			values["sort_order"] = *smallest - 10000
		}
		return tx.Table("cycles").Create(values).Error
	})
	if err != nil {
		handler.serverError(c, err)
		return
	}

	if handler.tasks != nil {
		err := handler.tasks.PublishModelActivity(c.Request.Context(), "cycle", cycleID,
			decodeRawBody(body), nil, user.ID, slug, handler.origin(c))
		if err != nil {
			handler.serverError(c, err)
			return
		}
	}
	row, found, err := handler.externalCycleByID(c, cycleID)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !found {
		handler.serverError(c, errCycleVanished)
		return
	}
	// The response is the full serializer over a plain instance, so none of the nine numbers is on it and the body is twenty-two fields.
	drf.Respond(c, http.StatusCreated, cycleJSON(cycleRow{Cycle: row}, false))
}

var errCycleVanished = &orderError{"the cycle could not be read back after it was written"}

// cycleUpdate edits a cycle.
//
// A cycle whose end date has passed is meant to be frozen but for its sort order. The narrowing is written and then dropped on the floor — the serializer is handed the original payload rather than the narrowed one — so a completed cycle really can be edited in full, as long as the payload names a sort order at all. That is upstream's and is reproduced here.
func (handler *Handler) cycleUpdate(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireProjectMember(c, user, c.Request.Method) {
		return
	}
	current, found, err := handler.externalCycleByID(c, c.Param("cycle"))
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !found {
		notFound(c)
		return
	}
	if current.ArchivedAt != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Archived cycle cannot be edited"})
		return
	}
	var body map[string]json.RawMessage
	if err := c.ShouldBindJSON(&body); err != nil {
		invalidPayload(c)
		return
	}
	now := handler.clock().UTC()
	if current.EndDate != nil && current.EndDate.Before(now) {
		if _, given := body["sort_order"]; !given {
			c.JSON(http.StatusBadRequest, gin.H{"error": "The Cycle has already been completed so it cannot be edited"})
			return
		}
	}

	project, found, err := handler.projectByID(c)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !found {
		c.JSON(http.StatusBadRequest, gin.H{"non_field_errors": []string{"Project not found"}})
		return
	}
	input, failures := handler.validateExternalCycle(c, body, project, true)
	if failures != nil {
		c.JSON(http.StatusBadRequest, failures)
		return
	}

	slug, projectID := c.Param("slug"), c.Param("project")
	if externalID := payloadString(body, "external_id"); externalID != "" && stringOrNil(current.ExternalID) != externalID {
		source := stringOrNil(current.ExternalSource)
		if given := payloadString(body, "external_source"); given != "" {
			source = given
		}
		existing, err := handler.cycleByExternalPair(c, projectID, source, externalID)
		if err != nil {
			handler.serverError(c, err)
			return
		}
		if existing != "" {
			c.JSON(http.StatusConflict, gin.H{
				"error": "Cycle with the same external id and external source already exists",
				"id":    current.ID,
			})
			return
		}
	}

	snapshot, err := json.Marshal(drf.Rendered(cycleJSON(cycleRow{Cycle: current}, false)))
	if err != nil {
		handler.serverError(c, err)
		return
	}
	updates := map[string]any{"updated_at": now, "updated_by_id": user.ID}
	for key, value := range input {
		updates[key] = value
	}
	err = handler.db.WithContext(c.Request.Context()).Table("cycles").Where("id = ?", current.ID).Updates(updates).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if handler.tasks != nil {
		currentInstance := string(snapshot)
		err := handler.tasks.PublishModelActivity(c.Request.Context(), "cycle", current.ID,
			decodeRawBody(body), &currentInstance, user.ID, slug, handler.origin(c))
		if err != nil {
			handler.serverError(c, err)
			return
		}
	}
	row, found, err := handler.externalCycleByID(c, current.ID)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !found {
		handler.serverError(c, errCycleVanished)
		return
	}
	drf.Respond(c, http.StatusOK, cycleJSON(cycleRow{Cycle: row}, false))
}

// cycleDestroy removes a cycle. Only an admin or the person who owns it may, which is narrower than the permission class the route carries.
//
// The favourites are removed **for real** rather than soft deleted, and the activity is queued before the cycle is gone so that it can still name the work items that were in it.
func (handler *Handler) cycleDestroy(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireProjectMember(c, user, c.Request.Method) {
		return
	}
	current, found, err := handler.externalCycleByID(c, c.Param("cycle"))
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !found {
		notFound(c)
		return
	}
	if current.OwnedByID != user.ID {
		role, member, err := handler.projectRole(c, user)
		if err != nil {
			handler.serverError(c, err)
			return
		}
		if !member || role != roleAdmin {
			c.JSON(http.StatusForbidden, gin.H{"error": "Only admin or creator can delete the cycle"})
			return
		}
	}

	var issueIDs []string
	err = handler.db.WithContext(c.Request.Context()).Table("cycle_issues").
		Where("cycle_id = ? AND deleted_at IS NULL", current.ID).Pluck("issue_id", &issueIDs).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if issueIDs == nil {
		issueIDs = []string{}
	}
	now := handler.clock().UTC()
	if handler.tasks != nil {
		requested, err := json.Marshal(map[string]any{
			"cycle_id": current.ID, "cycle_name": current.Name, "issues": issueIDs,
		})
		if err != nil {
			handler.serverError(c, err)
			return
		}
		// Like the work item delete, this one names no notification and no origin.
		err = handler.tasks.PublishIssueActivity(c.Request.Context(), map[string]any{
			"type": "cycle.activity.deleted", "requested_data": string(requested),
			"actor_id": user.ID, "issue_id": nil, "project_id": c.Param("project"),
			"current_instance": nil, "epoch": now.Unix(),
		})
		if err != nil {
			handler.serverError(c, err)
			return
		}
	}

	err = handler.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		err := tx.Table("cycles").Where("id = ?", current.ID).
			Updates(map[string]any{"deleted_at": now, "updated_at": now}).Error
		if err != nil {
			return err
		}
		// The favourites go for good rather than being soft deleted.
		return tx.Exec(`DELETE FROM user_favorites WHERE entity_type = 'cycle' AND entity_identifier = ? AND project_id = ?`,
			current.ID, c.Param("project")).Error
	})
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if handler.tasks != nil {
		if err := handler.tasks.PublishSoftDeleteRelatedObjects(c.Request.Context(), "db", "cycle", current.ID); err != nil {
			handler.serverError(c, err)
			return
		}
	}
	c.Status(http.StatusNoContent)
}

// validateExternalCycle is CycleCreateSerializer, which the update inherits whole.
func (handler *Handler) validateExternalCycle(c *gin.Context, body map[string]json.RawMessage, project Project, partial bool) (map[string]any, gin.H) {
	values := map[string]any{}
	errors := gin.H{}

	if raw, given := body["name"]; given {
		if value, failure := charField(raw, 255); failure != nil {
			errors["name"] = failure
		} else if *value == "" {
			errors["name"] = []string{"This field may not be blank."}
		} else {
			values["name"] = *value
		}
	} else if !partial {
		errors["name"] = []string{"This field is required."}
	}
	if raw, given := body["description"]; given {
		if value, failure := charField(raw, 0); failure != nil {
			errors["description"] = failure
		} else {
			values["description"] = *value
		}
	}
	var start, end *time.Time
	for _, field := range []struct {
		name   string
		target **time.Time
	}{{"start_date", &start}, {"end_date", &end}} {
		raw, given := body[field.name]
		if !given {
			continue
		}
		value, failure := dateTimeField(raw)
		if failure != nil {
			errors[field.name] = failure
			continue
		}
		*field.target = value
		values[field.name] = nil
		if value != nil {
			values[field.name] = *value
		}
	}
	for _, field := range []string{"external_source", "external_id"} {
		raw, given := body[field]
		if !given {
			continue
		}
		if string(raw) == "null" {
			values[field] = nil
			continue
		}
		value, failure := charField(raw, 255)
		if failure != nil {
			errors[field] = failure
			continue
		}
		values[field] = *value
	}
	if raw, given := body["timezone"]; given {
		value, failure := charField(raw, 255)
		switch {
		case failure != nil:
			errors["timezone"] = failure
		default:
			if _, err := time.LoadLocation(*value); err != nil {
				errors["timezone"] = []string{`"` + *value + `" is not a valid choice.`}
			} else {
				values["timezone"] = *value
			}
		}
	}
	if raw, given := body["owned_by"]; given {
		value, failure := primaryKeyField(raw)
		switch {
		case failure != nil:
			errors["owned_by"] = failure
		case value == nil:
			values["owned_by_id"] = nil
		default:
			exists, err := handler.rowExists(c, "users", "id = ?", *value)
			if err != nil {
				handler.serverError(c, err)
				return nil, gin.H{}
			}
			if !exists {
				errors["owned_by"] = []string{"Invalid pk \"" + *value + "\" - object does not exist."}
			} else {
				values["owned_by_id"] = *value
			}
		}
	}
	if len(errors) > 0 {
		return nil, errors
	}

	if !project.CycleView {
		return nil, gin.H{"non_field_errors": []string{"Cycles are not enabled for this project"}}
	}
	if start != nil && end != nil {
		if start.After(*end) {
			return nil, gin.H{"non_field_errors": []string{"Start date cannot exceed end date"}}
		}
		// The pair is stored as the project's day boundaries rather than as the instants the caller sent, so only the date part of each survives.
		startAt, endAt, ok := cycles.ConvertToUTC(start.UTC().Format("2006-01-02"), end.UTC().Format("2006-01-02"),
			project.Timezone, handler.clock().UTC())
		if !ok {
			return nil, gin.H{"non_field_errors": []string{"Start date cannot exceed end date"}}
		}
		values["start_date"] = startAt
		values["end_date"] = endAt
	}
	return values, nil
}

// nonNullField reports whether a payload names a field with something other than a null, which is how the date pair is counted.
func nonNullField(body map[string]json.RawMessage, field string) (json.RawMessage, bool) {
	raw, given := body[field]
	if !given || string(raw) == "null" {
		return nil, false
	}
	return raw, true
}

func (handler *Handler) cycleByExternalPair(c *gin.Context, projectID, source, identifier string) (string, error) {
	var found []string
	err := handler.db.WithContext(c.Request.Context()).Table("cycles c").
		Joins("JOIN workspaces w ON w.id = c.workspace_id").
		Where("w.slug = ? AND c.project_id = ? AND c.external_source = ? AND c.external_id = ? AND c.deleted_at IS NULL",
			c.Param("slug"), projectID, source, identifier).
		Order("c.created_at").Limit(1).Pluck("c.id", &found).Error
	if err != nil || len(found) == 0 {
		return "", err
	}
	return found[0], nil
}

// cycleListJSON is the serializer over a row the list annotated: the six counts are there and the three estimates are not, because the queryset does not compute them and DRF drops a read-only field with nothing behind it.
func cycleListJSON(row cycleRow) gin.H {
	data := cycleJSON(row, false)
	data["total_issues"] = row.TotalIssues
	data["cancelled_issues"] = row.CancelledIssues
	data["completed_issues"] = row.CompletedIssues
	data["started_issues"] = row.StartedIssues
	data["unstarted_issues"] = row.UnstartedIssues
	data["backlog_issues"] = row.BacklogIssues
	return data
}
