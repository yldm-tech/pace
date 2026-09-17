package externalapi

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api/internal/auth"
	"github.com/yldm-tech/pace/apps/api/internal/drf"
	"github.com/yldm-tech/pace/apps/api/internal/issues"
	"gorm.io/gorm"
)

// issueCreate makes a work item.
//
// The create answers with the serializer's view of the work item as it stood **before** the two audit columns were rewritten. Django reads serializer.data once to find the id it just wrote, which fixes the body then and there, and the created_at and created_by the caller asked for are written after that — so a caller who imports a work item with a date of its own is answered with today's.
func (handler *Handler) issueCreate(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireProjectMember(c, user, c.Request.Method) {
		return
	}
	slug, projectID := c.Param("slug"), c.Param("project")
	// The project is read by its id alone, without the workspace in the url, which is Django's unguarded get.
	project, found, err := handler.projectByID(c)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !found {
		notFound(c)
		return
	}

	var body map[string]json.RawMessage
	if err := c.ShouldBindJSON(&body); err != nil {
		invalidPayload(c)
		return
	}
	input, failures := validateExternalIssue(body, false)
	if failures != nil {
		c.JSON(http.StatusBadRequest, failures)
		return
	}
	if !handler.narrowIssueRelations(c, &input, projectID, project.WorkspaceID) {
		return
	}

	externalID, externalSource := payloadString(body, "external_id"), payloadString(body, "external_source")
	if externalID != "" && externalSource != "" {
		existing, err := handler.issueByExternalPair(c, projectID, externalSource, externalID)
		if err != nil {
			handler.serverError(c, err)
			return
		}
		if existing != "" {
			c.JSON(http.StatusConflict, gin.H{
				"error": "Issue with the same external id and external source already exists",
				"id":    existing,
			})
			return
		}
	}

	now := handler.clock().UTC()
	issueID, err := newUUID()
	if err != nil {
		handler.serverError(c, err)
		return
	}
	// A work item with no type of its own takes the project's default.
	if !input.typeGiven {
		defaultType, err := handler.defaultIssueTypeID(c, projectID)
		if err != nil {
			handler.serverError(c, err)
			return
		}
		if defaultType != "" {
			input.values["type_id"] = defaultType
		}
	}
	assignees := input.assignees
	if !input.hasAssignees || len(assignees) == 0 {
		// With no assignees of its own the work item goes to the project's default, but only while they are still a member who could have been assigned it.
		assignees, err = handler.defaultAssignee(c, project, projectID)
		if err != nil {
			handler.serverError(c, err)
			return
		}
	}

	values := map[string]any{"id": issueID}
	for key, value := range input.values {
		values[key] = value
	}
	err = handler.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		sequence, err := issues.PrepareCreate(tx, values, projectID, project.WorkspaceID, user.ID, now)
		if err != nil {
			return err
		}
		if err := tx.Table("issues").Create(values).Error; err != nil {
			return err
		}
		sequenceID, err := newUUID()
		if err != nil {
			return err
		}
		err = tx.Table("issue_sequences").Create(map[string]any{
			"id": sequenceID, "created_at": now, "updated_at": now,
			"project_id": projectID, "workspace_id": project.WorkspaceID,
			"issue_id": issueID, "sequence": sequence, "deleted": false,
		}).Error
		if err != nil {
			return err
		}
		if err := handler.writeIssueAssignees(tx, issueID, projectID, project.WorkspaceID, user.ID, now, assignees); err != nil {
			return err
		}
		return handler.writeIssueLabels(tx, issueID, projectID, project.WorkspaceID, user.ID, now, input.labels)
	})
	if err != nil {
		handler.serverError(c, err)
		return
	}

	// The response is fixed here, before the audit columns are rewritten below.
	rows, err := handler.issueRowsByID(c, issueID)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if len(rows) == 0 {
		handler.serverError(c, errIssueVanished)
		return
	}
	bodies, err := handler.issueBodies(c, rows)
	if err != nil {
		handler.serverError(c, err)
		return
	}

	// created_at and created_by are taken from the request as they were written rather than from the serializer, which is what lets an import carry the dates it had somewhere else.
	audit := map[string]any{}
	if raw, given := body["created_at"]; given {
		moment, failure := dateTimeField(raw)
		if failure != nil || moment == nil {
			// Django hands the raw value to the model, which refuses it on the way to the database.
			c.JSON(http.StatusBadRequest, gin.H{"error": "Please provide valid detail"})
			return
		}
		audit["created_at"] = *moment
	}
	if value, given := input.values["created_by_id"]; given {
		audit["created_by_id"] = value
	}
	if len(audit) > 0 {
		err := handler.db.WithContext(c.Request.Context()).Table("issues").Where("id = ?", issueID).Updates(audit).Error
		if err != nil {
			handler.serverError(c, err)
			return
		}
	}

	if err := handler.publishIssueCreated(c, user, slug, projectID, issueID, body, now); err != nil {
		handler.serverError(c, err)
		return
	}
	drf.Respond(c, http.StatusCreated, bodies[0])
}

var errIssueVanished = &orderError{"the work item could not be read back after it was written"}

// issueUpdate edits a work item.
//
// It reads through the plain manager rather than the issue manager the list uses, so an archived, draft or triage work item can be edited here even though the list will not show it.
func (handler *Handler) issueUpdate(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireProjectMember(c, user, c.Request.Method) {
		return
	}
	slug, projectID := c.Param("slug"), c.Param("project")
	current, found, err := handler.issueForWriting(c)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !found {
		notFound(c)
		return
	}
	project, found, err := handler.projectByID(c)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !found {
		notFound(c)
		return
	}

	snapshot, err := handler.issueSnapshot(c, current)
	if err != nil {
		handler.serverError(c, err)
		return
	}

	var body map[string]json.RawMessage
	if err := c.ShouldBindJSON(&body); err != nil {
		invalidPayload(c)
		return
	}
	input, failures := validateExternalIssue(body, true)
	if failures != nil {
		c.JSON(http.StatusBadRequest, failures)
		return
	}
	if !handler.narrowIssueRelations(c, &input, projectID, project.WorkspaceID) {
		return
	}

	// The conflict is measured against the work item's own pair: rewriting it with the id it already has is not a clash, and the source falls back to the one it already carries.
	if externalID := payloadString(body, "external_id"); externalID != "" && stringOrNil(current.ExternalID) != externalID {
		source := stringOrNil(current.ExternalSource)
		if given := payloadString(body, "external_source"); given != "" {
			source = given
		}
		existing, err := handler.issueByExternalPair(c, projectID, source, externalID)
		if err != nil {
			handler.serverError(c, err)
			return
		}
		if existing != "" {
			c.JSON(http.StatusConflict, gin.H{
				"error": "Issue with the same external id and external source already exists",
				"id":    current.ID,
			})
			return
		}
	}

	now := handler.clock().UTC()
	updates := map[string]any{}
	for key, value := range input.values {
		updates[key] = value
	}
	// The serializer sets updated_at by hand and then saves, so a request that moves only the assignees still touches the work item's own row.
	updates["updated_at"] = now
	updates["updated_by_id"] = user.ID
	err = handler.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		err := issues.ApplyUpdate(tx, updates, projectID, current.StateID, current.DescriptionHTML, now)
		if err != nil {
			return err
		}
		if err := tx.Table("issues").Where("id = ?", current.ID).Updates(updates).Error; err != nil {
			return err
		}
		if input.hasAssignees {
			err := tx.Exec("DELETE FROM issue_assignees WHERE issue_id = ?", current.ID).Error
			if err != nil {
				return err
			}
			err = handler.writeIssueAssignees(tx, current.ID, projectID, project.WorkspaceID, user.ID, now, input.assignees)
			if err != nil {
				return err
			}
		}
		if input.hasLabels {
			if err := tx.Exec("DELETE FROM issue_labels WHERE issue_id = ?", current.ID).Error; err != nil {
				return err
			}
			return handler.writeIssueLabels(tx, current.ID, projectID, project.WorkspaceID, user.ID, now, input.labels)
		}
		return nil
	})
	if err != nil {
		handler.serverError(c, err)
		return
	}

	if err := handler.publishIssueUpdated(c, user, slug, projectID, current.ID, body, snapshot, now); err != nil {
		handler.serverError(c, err)
		return
	}
	rows, err := handler.issueRowsByID(c, current.ID)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if len(rows) == 0 {
		handler.serverError(c, errIssueVanished)
		return
	}
	bodies, err := handler.issueBodies(c, rows)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, bodies[0])
}

// issueDestroy soft deletes a work item. Only an admin or the person who raised it may, which is a narrower rule than the permission class the route carries.
func (handler *Handler) issueDestroy(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireProjectMember(c, user, c.Request.Method) {
		return
	}
	current, found, err := handler.issueForWriting(c)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !found {
		notFound(c)
		return
	}
	if stringOrNil(current.CreatedByID) != user.ID {
		role, member, err := handler.projectRole(c, user)
		if err != nil {
			handler.serverError(c, err)
			return
		}
		if !member || role != roleAdmin {
			c.JSON(http.StatusForbidden, gin.H{"error": "Only admin or creator can delete the work item"})
			return
		}
	}
	snapshot, err := handler.issueSnapshot(c, current)
	if err != nil {
		handler.serverError(c, err)
		return
	}

	now := handler.clock().UTC()
	err = handler.db.WithContext(c.Request.Context()).Table("issues").Where("id = ?", current.ID).
		Updates(map[string]any{"deleted_at": now, "updated_at": now}).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if handler.tasks != nil {
		if err := handler.tasks.PublishSoftDeleteRelatedObjects(c.Request.Context(), "db", "issue", current.ID); err != nil {
			handler.serverError(c, err)
			return
		}
		requested, err := json.Marshal(map[string]any{"issue_id": current.ID})
		if err != nil {
			handler.serverError(c, err)
			return
		}
		// The delete names no notification and no origin, unlike the create and the update, so the task falls back to its own defaults.
		err = handler.tasks.PublishIssueActivity(c.Request.Context(), map[string]any{
			"type": "issue.activity.deleted", "requested_data": string(requested),
			"actor_id": user.ID, "issue_id": current.ID, "project_id": c.Param("project"),
			"current_instance": snapshot, "epoch": now.Unix(),
		})
		if err != nil {
			handler.serverError(c, err)
			return
		}
	}
	c.Status(http.StatusNoContent)
}

// issueForWriting reads the work item the write routes act on, through the plain manager.
func (handler *Handler) issueForWriting(c *gin.Context) (externalIssueRow, bool, error) {
	var rows []externalIssueRow
	err := handler.db.WithContext(c.Request.Context()).Table("issues i").
		Select(externalIssueSelection()).
		Joins("JOIN workspaces w ON w.id = i.workspace_id").
		Where("w.slug = ? AND i.project_id = ? AND i.id = ? AND i.deleted_at IS NULL",
			c.Param("slug"), c.Param("project"), c.Param("issue")).
		Limit(1).Scan(&rows).Error
	if err != nil || len(rows) == 0 {
		return externalIssueRow{}, false, err
	}
	return rows[0], true, nil
}

// issueRowsByID reads a work item back through the plain manager, which is what the response is built from.
func (handler *Handler) issueRowsByID(c *gin.Context, issueID string) ([]externalIssueRow, error) {
	var rows []externalIssueRow
	err := handler.db.WithContext(c.Request.Context()).Table("issues i").
		Select(externalIssueSelection()).
		Where("i.id = ?", issueID).Limit(1).Scan(&rows).Error
	return rows, err
}

// issueSnapshot is the serializer's view of a work item before it is changed, which the activity carries as current_instance.
func (handler *Handler) issueSnapshot(c *gin.Context, row externalIssueRow) (string, error) {
	encoded, err := json.Marshal(drf.Rendered(externalIssueJSON(row)))
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

// issueByExternalPair answers with the id of the work item already carrying a pair, and the empty string when none does.
func (handler *Handler) issueByExternalPair(c *gin.Context, projectID, source, identifier string) (string, error) {
	var found []string
	err := handler.db.WithContext(c.Request.Context()).Table("issues i").
		Joins("JOIN workspaces w ON w.id = i.workspace_id").
		Where("w.slug = ? AND i.project_id = ? AND i.external_source = ? AND i.external_id = ? AND i.deleted_at IS NULL",
			c.Param("slug"), projectID, source, identifier).
		Order("i.created_at").Limit(1).Pluck("i.id", &found).Error
	if err != nil || len(found) == 0 {
		return "", err
	}
	return found[0], nil
}

// narrowIssueRelations is the half of validate() that needs the database: every relation has to belong to the project, and the two sets are checked rather than narrowed — a name that does not belong is a refusal here, not a silent drop the way it is on the session API.
func (handler *Handler) narrowIssueRelations(c *gin.Context, input *externalIssueInput, projectID, workspaceID string) bool {
	if identifier, present := input.values["created_by_id"].(string); present {
		exists, err := handler.rowExists(c, "users", "id = ?", identifier)
		if err != nil {
			handler.serverError(c, err)
			return false
		}
		if !exists {
			c.JSON(http.StatusBadRequest, gin.H{
				"created_by": []string{"Invalid pk \"" + identifier + "\" - object does not exist."},
			})
			return false
		}
	}
	if identifier, present := input.values["type_id"].(string); present {
		exists, err := handler.rowExists(c, "issue_types", "id = ?", identifier)
		if err != nil {
			handler.serverError(c, err)
			return false
		}
		if !exists {
			c.JSON(http.StatusBadRequest, gin.H{
				"type_id": []string{"Invalid pk \"" + identifier + "\" - object does not exist."},
			})
			return false
		}
	}
	if input.hasAssignees && len(input.assignees) > 0 {
		valid, err := handler.projectMemberIDs(c, projectID, input.assignees)
		if err != nil {
			handler.serverError(c, err)
			return false
		}
		if missing := missingFrom(input.assignees, valid); len(missing) > 0 {
			c.JSON(http.StatusBadRequest, gin.H{
				"non_field_errors": []string{"Assignees " + pythonList(missing) + " are not active members of this project"},
			})
			return false
		}
		input.assignees = valid
	}
	if input.hasLabels && len(input.labels) > 0 {
		valid, err := handler.labelIDsInProject(c, projectID, input.labels)
		if err != nil {
			handler.serverError(c, err)
			return false
		}
		if missing := missingFrom(input.labels, valid); len(missing) > 0 {
			c.JSON(http.StatusBadRequest, gin.H{
				"non_field_errors": []string{"Labels " + pythonList(missing) + " do not belong to this project"},
			})
			return false
		}
		input.labels = valid
	}
	for _, relation := range []struct{ column, table, message string }{
		{"state_id", "states", "State is not valid please pass a valid state_id"},
		{"parent_id", "issues", "Parent is not valid issue_id please pass a valid issue_id"},
		{"estimate_point_id", "estimate_points", "Estimate point is not valid please pass a valid estimate_point_id"},
	} {
		identifier, present := input.values[relation.column].(string)
		if !present {
			continue
		}
		exists, err := handler.rowExists(c, relation.table, "id = ? AND project_id = ?", identifier, projectID)
		if err != nil {
			handler.serverError(c, err)
			return false
		}
		if !exists {
			c.JSON(http.StatusBadRequest, gin.H{"non_field_errors": []string{relation.message}})
			return false
		}
	}
	return true
}

func (handler *Handler) rowExists(c *gin.Context, table, condition string, arguments ...any) (bool, error) {
	var count int64
	err := handler.db.WithContext(c.Request.Context()).Table(table).Where(condition, arguments...).Count(&count).Error
	return count > 0, err
}

// projectMemberIDs keeps the people who are active members at member level or above, which is the floor the assignee list is measured against.
func (handler *Handler) projectMemberIDs(c *gin.Context, projectID string, requested []string) ([]string, error) {
	var found []string
	err := handler.db.WithContext(c.Request.Context()).Table("project_members").
		Where("project_id = ? AND is_active = TRUE AND role >= ? AND member_id IN ?", projectID, roleMember, requested).
		Pluck("member_id", &found).Error
	return found, err
}

func (handler *Handler) labelIDsInProject(c *gin.Context, projectID string, requested []string) ([]string, error) {
	var found []string
	err := handler.db.WithContext(c.Request.Context()).Table("labels").
		Where("project_id = ? AND id IN ?", projectID, requested).
		Pluck("id", &found).Error
	return found, err
}

// defaultIssueTypeID is the type a work item takes when the caller names none.
func (handler *Handler) defaultIssueTypeID(c *gin.Context, projectID string) (string, error) {
	var found []string
	err := handler.db.WithContext(c.Request.Context()).Table("issue_types it").
		Joins("JOIN project_issue_types pit ON pit.issue_type_id = it.id").
		Where("pit.project_id = ? AND it.is_default = TRUE", projectID).
		Limit(1).Pluck("it.id", &found).Error
	if err != nil || len(found) == 0 {
		return "", err
	}
	return found[0], nil
}

// defaultAssignee returns the project's default assignee, but only while they are still an active member at member level or above.
func (handler *Handler) defaultAssignee(c *gin.Context, project Project, projectID string) ([]string, error) {
	if project.DefaultAssigneeID == nil {
		return nil, nil
	}
	var count int64
	err := handler.db.WithContext(c.Request.Context()).Table("project_members").
		Where("member_id = ? AND project_id = ? AND role >= ? AND is_active = TRUE",
			*project.DefaultAssigneeID, projectID, roleMember).Count(&count).Error
	if err != nil || count == 0 {
		return nil, err
	}
	return []string{*project.DefaultAssigneeID}, nil
}

func (handler *Handler) writeIssueAssignees(tx *gorm.DB, issueID, projectID, workspaceID, actorID string, now time.Time, assignees []string) error {
	if len(assignees) == 0 {
		return nil
	}
	rows := make([]map[string]any, 0, len(assignees))
	for _, assignee := range assignees {
		identifier, err := newUUID()
		if err != nil {
			return err
		}
		rows = append(rows, map[string]any{
			"id": identifier, "created_at": now, "updated_at": now,
			"created_by_id": actorID, "updated_by_id": actorID,
			"project_id": projectID, "workspace_id": workspaceID,
			"issue_id": issueID, "assignee_id": assignee,
		})
	}
	// A clash is ignored rather than failing the write, which is what the serializer's try around bulk_create does.
	return tx.Table("issue_assignees").Clauses(onConflictDoNothing()).Create(rows).Error
}

func (handler *Handler) writeIssueLabels(tx *gorm.DB, issueID, projectID, workspaceID, actorID string, now time.Time, labels []string) error {
	if len(labels) == 0 {
		return nil
	}
	rows := make([]map[string]any, 0, len(labels))
	for _, label := range labels {
		identifier, err := newUUID()
		if err != nil {
			return err
		}
		rows = append(rows, map[string]any{
			"id": identifier, "created_at": now, "updated_at": now,
			"created_by_id": actorID, "updated_by_id": actorID,
			"project_id": projectID, "workspace_id": workspaceID,
			"issue_id": issueID, "label_id": label,
		})
	}
	return tx.Table("issue_labels").Clauses(onConflictDoNothing()).Create(rows).Error
}

func (handler *Handler) publishIssueCreated(c *gin.Context, user *auth.User, slug, projectID, issueID string, body map[string]json.RawMessage, now time.Time) error {
	if handler.tasks == nil {
		return nil
	}
	requested, err := json.Marshal(decodeRawBody(body))
	if err != nil {
		return err
	}
	err = handler.tasks.PublishIssueActivity(c.Request.Context(), map[string]any{
		"type": "issue.activity.created", "requested_data": string(requested),
		"actor_id": user.ID, "issue_id": issueID, "project_id": projectID,
		"current_instance": nil, "epoch": now.Unix(),
		"notification": true, "origin": handler.origin(c),
	})
	if err != nil {
		return err
	}
	return handler.tasks.PublishModelActivity(c.Request.Context(), "issue", issueID,
		decodeRawBody(body), nil, user.ID, slug, handler.origin(c))
}

func (handler *Handler) publishIssueUpdated(c *gin.Context, user *auth.User, slug, projectID, issueID string, body map[string]json.RawMessage, snapshot string, now time.Time) error {
	if handler.tasks == nil {
		return nil
	}
	requested, err := json.Marshal(decodeRawBody(body))
	if err != nil {
		return err
	}
	err = handler.tasks.PublishIssueActivity(c.Request.Context(), map[string]any{
		"type": "issue.activity.updated", "requested_data": string(requested),
		"actor_id": user.ID, "issue_id": issueID, "project_id": projectID,
		"current_instance": snapshot, "epoch": now.Unix(),
		"notification": true, "origin": handler.origin(c),
	})
	if err != nil {
		return err
	}
	return handler.tasks.PublishModelActivity(c.Request.Context(), "issue", issueID,
		decodeRawBody(body), &snapshot, user.ID, slug, handler.origin(c))
}

// decodeRawBody reads a payload back into plain values, keeping the numbers as they were written.
func decodeRawBody(body map[string]json.RawMessage) map[string]any {
	decoded := map[string]any{}
	for key, raw := range body {
		decoded[key] = decodeJSON(raw)
	}
	return decoded
}

func payloadString(body map[string]json.RawMessage, field string) string {
	raw, given := body[field]
	if !given {
		return ""
	}
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return ""
	}
	return value
}

func stringOrNil(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// missingFrom is the set difference the two list checks report, which Django prints as a Python list.
func missingFrom(requested, allowed []string) []string {
	known := map[string]bool{}
	for _, value := range allowed {
		known[value] = true
	}
	missing := []string{}
	for _, value := range requested {
		if !known[value] {
			missing = append(missing, value)
		}
	}
	return missing
}

// pythonList writes a list of identifiers the way Python prints one, which is what the message carries.
func pythonList(values []string) string {
	rendered := "["
	for index, value := range values {
		if index > 0 {
			rendered += ", "
		}
		rendered += "UUID('" + value + "')"
	}
	return rendered + "]"
}
