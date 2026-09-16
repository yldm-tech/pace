package externalapi

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"github.com/yldm-tech/pace/apps/api-go/internal/drf"
	"github.com/yldm-tech/pace/apps/api-go/internal/htmlsanitizer"
	"gorm.io/gorm"
)

func (handler *Handler) registerIntakeRoutes(router gin.IRouter) {
	base := "/api/v1/workspaces/:slug/projects/:project/intake-issues/"
	router.GET(base, handler.authenticated(handler.intakeIssueList))
	router.POST(base, handler.authenticated(handler.intakeIssueCreate))
	router.GET(base+":issue/", handler.authenticated(handler.intakeIssueRetrieve))
	router.PATCH(base+":issue/", handler.authenticated(handler.intakeIssueUpdate))
	router.DELETE(base+":issue/", handler.authenticated(handler.intakeIssueDestroy))
}

// IntakeIssue is the db.IntakeIssue table as the external API sees it.
type IntakeIssue struct {
	ID             string     `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt      time.Time  `gorm:"column:created_at"`
	UpdatedAt      time.Time  `gorm:"column:updated_at"`
	CreatedByID    *string    `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID    *string    `gorm:"column:updated_by_id;type:uuid"`
	DeletedAt      *time.Time `gorm:"column:deleted_at"`
	ProjectID      string     `gorm:"column:project_id;type:uuid"`
	WorkspaceID    string     `gorm:"column:workspace_id;type:uuid"`
	IntakeID       string     `gorm:"column:intake_id;type:uuid"`
	IssueID        string     `gorm:"column:issue_id;type:uuid"`
	Status         int        `gorm:"column:status"`
	SnoozedTill    *time.Time `gorm:"column:snoozed_till"`
	DuplicateToID  *string    `gorm:"column:duplicate_to_id;type:uuid"`
	Source         *string    `gorm:"column:source"`
	SourceEmail    *string    `gorm:"column:source_email"`
	ExternalSource *string    `gorm:"column:external_source"`
	ExternalID     *string    `gorm:"column:external_id"`
	Extra          []byte     `gorm:"column:extra;type:jsonb"`
}

func (IntakeIssue) TableName() string { return "intake_issues" }

// intakeIssueList returns what is waiting in the project's intake.
//
// It hides what is still snoozed — a work item put off until tomorrow is not in the list today — which is the one narrowing the session API's version does not have.
func (handler *Handler) intakeIssueList(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireProjectLite(c, user) {
		return
	}
	intake, enabled, err := handler.projectIntakeIfEnabled(c)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	// A project with no intake, or with the feature switched off, answers an empty page rather than an error.
	results := []gin.H{}
	if enabled {
		now := handler.clock().UTC()
		var rows []IntakeIssue
		err := handler.db.WithContext(c.Request.Context()).Table("intake_issues ii").Select("ii.*").
			Joins("JOIN workspaces w ON w.id = ii.workspace_id").
			Where("w.slug = ? AND ii.project_id = ? AND ii.intake_id = ? AND ii.deleted_at IS NULL",
				c.Param("slug"), c.Param("project"), intake.ID).
			Where("ii.snoozed_till >= ? OR ii.snoozed_till IS NULL", now).
			Order("ii.created_at DESC").Scan(&rows).Error
		if err != nil {
			handler.serverError(c, err)
			return
		}
		results, err = handler.intakeIssueBodies(c, rows)
		if err != nil {
			handler.serverError(c, err)
			return
		}
	}
	fields := requestedFields(c)
	narrowed := make([]gin.H, 0, len(results))
	for _, item := range results {
		narrowed = append(narrowed, narrow(item, fields))
	}
	handler.respondPaged(c, narrowed)
}

func (handler *Handler) intakeIssueRetrieve(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireProjectLite(c, user) {
		return
	}
	row, found, err := handler.intakeIssueByIssue(c)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !found {
		notFound(c)
		return
	}
	bodies, err := handler.intakeIssueBodies(c, []IntakeIssue{row})
	if err != nil {
		handler.serverError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, narrow(bodies[0], requestedFields(c)))
}

// intakeIssueCreate submits a work item for triage.
//
// The description is sanitized and, when the sanitizer rejects it, replaced by the empty paragraph rather than refused — the handler ignores the validity flag and keeps only the cleaned text.
func (handler *Handler) intakeIssueCreate(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireProjectLite(c, user) {
		return
	}
	projectID := c.Param("project")
	var body map[string]json.RawMessage
	if err := c.ShouldBindJSON(&body); err != nil {
		invalidPayload(c)
		return
	}
	var issueBody map[string]any
	if raw, present := body["issue"]; present {
		_ = json.Unmarshal(raw, &issueBody)
	}
	name, _ := issueBody["name"].(string)
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Name is required"})
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
	intake, hasIntake, err := handler.projectIntakeRow(c)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	// The guard is an "and", so a project with an intake row is let through even with the feature switched off, and one without it is refused only when the feature is off too.
	if !hasIntake && !project.IntakeView {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Intake is not enabled for this project enable it through the project's api",
		})
		return
	}
	priority := "none"
	if value, present := issueBody["priority"]; present {
		priority, _ = value.(string)
	}
	if !validIssuePriorities[priority] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid priority"})
		return
	}
	if !hasIntake {
		// Django reads the id off the intake it did not find, so a project with the feature on and no intake row raises.
		handler.serverError(c, errNoIntakeRow)
		return
	}

	now := handler.clock().UTC()
	triageID, err := handler.ensureTriageState(c, project.WorkspaceID, now)
	if err != nil {
		handler.serverError(c, err)
		return
	}

	description := issueBody["description"]
	if description == nil {
		description = issueBody["description_json"]
	}
	descriptionJSON := []byte("{}")
	if description != nil {
		if encoded, err := json.Marshal(description); err == nil {
			descriptionJSON = encoded
		}
	}
	html := "<p></p>"
	if value, ok := issueBody["description_html"].(string); ok {
		html = value
	}
	// The validity flag is discarded: only the cleaned text is kept, and a rejection leaves the empty paragraph.
	_, _, cleaned := htmlsanitizer.ValidateHTMLContent(html)
	if cleaned != nil {
		html = *cleaned
	} else {
		html = "<p></p>"
	}

	issueID, err := newUUID()
	if err != nil {
		handler.serverError(c, err)
		return
	}
	linkID, err := newUUID()
	if err != nil {
		handler.serverError(c, err)
		return
	}
	source := "IN_APP"
	err = handler.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		// The issue is created directly rather than through the serializer, so it gets no sequence number, no sort order and no default assignee — everything the session API's create does is skipped.
		issue := map[string]any{
			"id": issueID, "created_at": now, "updated_at": now,
			"name": name, "description_json": descriptionJSON, "description_html": html,
			"priority": priority, "project_id": projectID, "workspace_id": project.WorkspaceID,
			"state_id": triageID, "sort_order": 65535, "is_draft": false,
		}
		if err := tx.Table("issues").Create(issue).Error; err != nil {
			return err
		}
		link := IntakeIssue{
			ID: linkID, CreatedAt: now, UpdatedAt: now,
			ProjectID: projectID, WorkspaceID: project.WorkspaceID,
			IntakeID: intake.ID, IssueID: issueID, Status: -2, Source: &source, Extra: []byte("{}"),
		}
		return tx.Create(&link).Error
	})
	if err != nil {
		handler.serverError(c, err)
		return
	}

	row, found, err := handler.intakeIssueByID(c, linkID)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !found {
		handler.serverError(c, errNoIntakeRow)
		return
	}
	bodies, err := handler.intakeIssueBodies(c, []IntakeIssue{row})
	if err != nil {
		handler.serverError(c, err)
		return
	}
	drf.Respond(c, http.StatusCreated, bodies[0])
}

// errNoIntakeRow names the failure Django reaches by reading an attribute off the intake it did not find.
var errNoIntakeRow = &orderError{"the project has no intake row"}

// intakeIssueUpdate edits a work item and its place in the queue, with the two halves gated apart.
//
// A **guest** may edit the work item and only its name and description; anything else they send is dropped. The queue entry moves only for a role **above** member, which in practice means an admin.
func (handler *Handler) intakeIssueUpdate(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireProjectLite(c, user) {
		return
	}
	var body map[string]json.RawMessage
	if err := c.ShouldBindJSON(&body); err != nil {
		invalidPayload(c)
		return
	}
	row, found, err := handler.intakeIssueByIssue(c)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !found {
		notFound(c)
		return
	}
	role, member, err := handler.projectRole(c, user)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !member {
		// Django reads the role off an unguarded .get, so somebody who is not in the project raises rather than being refused.
		handler.serverError(c, errNoIntakeRow)
		return
	}
	isGuest := role <= roleGuest
	if isGuest && (row.CreatedByID == nil || *row.CreatedByID != user.ID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "You cannot edit intake work items"})
		return
	}

	now := handler.clock().UTC()
	if raw, present := body["issue"]; present {
		var issueBody map[string]any
		if err := json.Unmarshal(raw, &issueBody); err == nil && len(issueBody) > 0 {
			if isGuest {
				narrowed := map[string]any{}
				for _, field := range []string{"name", "description_html"} {
					if value, ok := issueBody[field]; ok {
						narrowed[field] = value
					}
				}
				description := issueBody["description"]
				if description == nil {
					description = issueBody["description_json"]
				}
				narrowed["description_json"] = description
				issueBody = narrowed
			}
			if err := handler.writeIntakeIssueFields(c, row, issueBody, now, user.ID); err != nil {
				handler.serverError(c, err)
				return
			}
		}
	}

	// Only a role above member moves the queue entry, so a plain member may edit the work item and not triage it.
	if role > roleMember {
		updates := map[string]any{}
		if raw, present := body["status"]; present {
			var status int
			if err := json.Unmarshal(raw, &status); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"status": []string{"A valid integer is required."}})
				return
			}
			updates["status"] = status
		}
		if raw, present := body["snoozed_till"]; present {
			var moment *string
			if err := json.Unmarshal(raw, &moment); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"snoozed_till": []string{"Not a valid string."}})
				return
			}
			if moment == nil {
				updates["snoozed_till"] = nil
			} else {
				parsed, err := time.Parse(time.RFC3339, *moment)
				if err != nil {
					c.JSON(http.StatusBadRequest, gin.H{"snoozed_till": []string{"Datetime has wrong format."}})
					return
				}
				updates["snoozed_till"] = parsed
			}
		}
		for _, field := range []string{"duplicate_to", "source", "source_email"} {
			raw, present := body[field]
			if !present {
				continue
			}
			var value *string
			if err := json.Unmarshal(raw, &value); err != nil {
				continue
			}
			column := field
			if field == "duplicate_to" {
				column = "duplicate_to_id"
			}
			if value == nil {
				updates[column] = nil
			} else {
				updates[column] = *value
			}
		}
		if len(updates) > 0 {
			updates["updated_at"] = now
			updates["updated_by_id"] = user.ID
			err := handler.db.WithContext(c.Request.Context()).Model(&IntakeIssue{}).
				Where("id = ?", row.ID).Updates(updates).Error
			if err != nil {
				handler.serverError(c, err)
				return
			}
		}
	}

	fresh, found, err := handler.intakeIssueByID(c, row.ID)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !found {
		notFound(c)
		return
	}
	bodies, err := handler.intakeIssueBodies(c, []IntakeIssue{fresh})
	if err != nil {
		handler.serverError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, bodies[0])
}

// intakeIssueDestroy takes a work item out of the queue, and the work item with it unless it was accepted.
//
// Deleting the work item wants the person who raised it or a project admin; the queue entry goes either way.
func (handler *Handler) intakeIssueDestroy(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireProjectLite(c, user) {
		return
	}
	row, found, err := handler.intakeIssueByIssue(c)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !found {
		notFound(c)
		return
	}
	now := handler.clock().UTC()
	removeIssue := row.Status != 1
	if removeIssue {
		var creators []string
		err := handler.db.WithContext(c.Request.Context()).Table("issues").
			Where("id = ?", row.IssueID).Limit(1).Pluck("created_by_id", &creators).Error
		if err != nil {
			handler.serverError(c, err)
			return
		}
		role, member, err := handler.projectRole(c, user)
		if err != nil {
			handler.serverError(c, err)
			return
		}
		ownIssue := len(creators) > 0 && creators[0] == user.ID
		if !ownIssue && !(member && role == roleAdmin) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Only admin or creator can delete the work item"})
			return
		}
	}
	err = handler.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if removeIssue {
			err := tx.Table("issues").Where("id = ?", row.IssueID).Update("deleted_at", now).Error
			if err != nil {
				return err
			}
		}
		return tx.Model(&IntakeIssue{}).Where("id = ?", row.ID).Update("deleted_at", now).Error
	})
	if err != nil {
		handler.serverError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// writeIntakeIssueFields writes the work item half of an update.
func (handler *Handler) writeIntakeIssueFields(c *gin.Context, row IntakeIssue, issueBody map[string]any, now time.Time, actorID string) error {
	updates := map[string]any{}
	if value, ok := issueBody["name"].(string); ok {
		updates["name"] = value
	}
	if value, ok := issueBody["description_html"].(string); ok {
		_, _, cleaned := htmlsanitizer.ValidateHTMLContent(value)
		if cleaned != nil {
			updates["description_html"] = *cleaned
		} else {
			updates["description_html"] = value
		}
	}
	if value, present := issueBody["description_json"]; present {
		encoded, err := json.Marshal(value)
		if err == nil {
			updates["description_json"] = encoded
		}
	}
	if value, ok := issueBody["priority"].(string); ok && validIssuePriorities[value] {
		updates["priority"] = value
	}
	if len(updates) == 0 {
		return nil
	}
	updates["updated_at"] = now
	updates["updated_by_id"] = actorID
	return handler.db.WithContext(c.Request.Context()).Table("issues").
		Where("id = ?", row.IssueID).Updates(updates).Error
}

// validIssuePriorities is the list the create checks by hand, the same five the session API checks.
var validIssuePriorities = map[string]bool{"low": true, "medium": true, "high": true, "urgent": true, "none": true}

// requireProjectLite is ProjectLitePermission: any active member of the project, whatever their role.
func (handler *Handler) requireProjectLite(c *gin.Context, user *auth.User) bool {
	_, member, err := handler.projectRole(c, user)
	if err != nil {
		handler.serverError(c, err)
		return false
	}
	if !member {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
			"detail": "You do not have permission to perform this action.",
		})
		return false
	}
	return true
}

func (handler *Handler) projectRole(c *gin.Context, user *auth.User) (int, bool, error) {
	var roles []int
	err := handler.db.WithContext(c.Request.Context()).Table("project_members pm").
		Joins("JOIN workspaces w ON w.id = pm.workspace_id").
		Where("w.slug = ? AND pm.project_id = ? AND pm.member_id = ? AND pm.is_active = TRUE",
			c.Param("slug"), c.Param("project"), user.ID).
		Limit(1).Pluck("pm.role", &roles).Error
	if err != nil || len(roles) == 0 {
		return 0, false, err
	}
	return roles[0], true, nil
}

// projectIntakeIfEnabled reports the project's intake and whether the list should read it at all.
func (handler *Handler) projectIntakeIfEnabled(c *gin.Context) (Intake, bool, error) {
	project, found, err := handler.projectByID(c)
	if err != nil || !found {
		return Intake{}, false, err
	}
	intake, hasIntake, err := handler.projectIntakeRow(c)
	if err != nil {
		return Intake{}, false, err
	}
	// The list's guard is an "or": no intake row **or** the feature switched off empties it.
	return intake, hasIntake && project.IntakeView, nil
}

// Intake is the db.Intake table as this app sees it.
type Intake struct {
	ID          string `gorm:"column:id;type:uuid;primaryKey"`
	ProjectID   string `gorm:"column:project_id;type:uuid"`
	WorkspaceID string `gorm:"column:workspace_id;type:uuid"`
	Name        string `gorm:"column:name"`
	IsDefault   bool   `gorm:"column:is_default"`
}

func (Intake) TableName() string { return "intakes" }

func (handler *Handler) projectIntakeRow(c *gin.Context) (Intake, bool, error) {
	var intakes []Intake
	err := handler.db.WithContext(c.Request.Context()).Table("intakes i").Select("i.*").
		Joins("JOIN workspaces w ON w.id = i.workspace_id").
		Where("w.slug = ? AND i.project_id = ? AND i.deleted_at IS NULL", c.Param("slug"), c.Param("project")).
		Order("i.created_at DESC").Limit(1).Scan(&intakes).Error
	if err != nil || len(intakes) == 0 {
		return Intake{}, false, err
	}
	return intakes[0], true, nil
}

// ensureTriageState returns the project's triage state, creating it when there is none.
func (handler *Handler) ensureTriageState(c *gin.Context, workspaceID string, now time.Time) (string, error) {
	var existing []string
	err := handler.db.WithContext(c.Request.Context()).Table("states s").
		Joins("JOIN workspaces w ON w.id = s.workspace_id").
		Where("w.slug = ? AND s.project_id = ? AND s.deleted_at IS NULL AND s.is_triage = TRUE",
			c.Param("slug"), c.Param("project")).
		Order("s.created_at").Limit(1).Pluck("s.id", &existing).Error
	if err != nil {
		return "", err
	}
	if len(existing) > 0 {
		return existing[0], nil
	}
	identifier, err := newUUID()
	if err != nil {
		return "", err
	}
	state := State{
		ID: identifier, CreatedAt: now, UpdatedAt: now,
		ProjectID: c.Param("project"), WorkspaceID: workspaceID,
		Name: "Triage", Group: "triage", Color: "#4E5355", Sequence: 65000, Default: false,
	}
	if err := handler.db.WithContext(c.Request.Context()).Create(&state).Error; err != nil {
		return "", err
	}
	return state.ID, nil
}

func (handler *Handler) intakeIssueByIssue(c *gin.Context) (IntakeIssue, bool, error) {
	var rows []IntakeIssue
	err := handler.db.WithContext(c.Request.Context()).Table("intake_issues ii").Select("ii.*").
		Joins("JOIN workspaces w ON w.id = ii.workspace_id").
		Where("w.slug = ? AND ii.project_id = ? AND ii.issue_id = ? AND ii.deleted_at IS NULL",
			c.Param("slug"), c.Param("project"), c.Param("issue")).
		Limit(1).Scan(&rows).Error
	if err != nil || len(rows) == 0 {
		return IntakeIssue{}, false, err
	}
	return rows[0], true, nil
}

func (handler *Handler) intakeIssueByID(c *gin.Context, identifier string) (IntakeIssue, bool, error) {
	var rows []IntakeIssue
	err := handler.db.WithContext(c.Request.Context()).Table("intake_issues ii").Select("ii.*").
		Where("ii.id = ? AND ii.deleted_at IS NULL", identifier).Limit(1).Scan(&rows).Error
	if err != nil || len(rows) == 0 {
		return IntakeIssue{}, false, err
	}
	return rows[0], true, nil
}

// intakeIssueBodies renders the link with the work item expanded inside it.
func (handler *Handler) intakeIssueBodies(c *gin.Context, rows []IntakeIssue) ([]gin.H, error) {
	results := make([]gin.H, 0, len(rows))
	if len(rows) == 0 {
		return results, nil
	}
	identifiers := make([]string, 0, len(rows))
	for _, row := range rows {
		identifiers = append(identifiers, row.IssueID)
	}
	var issues []struct {
		ID              string     `gorm:"column:id"`
		Name            string     `gorm:"column:name"`
		SequenceID      int64      `gorm:"column:sequence_id"`
		Priority        string     `gorm:"column:priority"`
		StateID         *string    `gorm:"column:state_id"`
		DescriptionHTML string     `gorm:"column:description_html"`
		CreatedAt       time.Time  `gorm:"column:created_at"`
		UpdatedAt       time.Time  `gorm:"column:updated_at"`
		CreatedByID     *string    `gorm:"column:created_by_id"`
		ProjectID       string     `gorm:"column:project_id"`
		StartDate       *time.Time `gorm:"column:start_date"`
		TargetDate      *time.Time `gorm:"column:target_date"`
	}
	err := handler.db.WithContext(c.Request.Context()).Table("issues").
		Where("id IN ?", identifiers).Scan(&issues).Error
	if err != nil {
		return nil, err
	}
	details := map[string]gin.H{}
	for _, issue := range issues {
		details[issue.ID] = gin.H{
			"id": issue.ID, "name": issue.Name, "sequence_id": issue.SequenceID,
			"priority": issue.Priority, "state_id": issue.StateID,
			"description_html": issue.DescriptionHTML, "project_id": issue.ProjectID,
			"created_at": issue.CreatedAt, "updated_at": issue.UpdatedAt, "created_by": issue.CreatedByID,
			"start_date": issue.StartDate, "target_date": issue.TargetDate,
		}
	}
	for _, row := range rows {
		results = append(results, gin.H{
			"id": row.ID, "created_at": row.CreatedAt, "updated_at": row.UpdatedAt,
			"created_by": row.CreatedByID, "updated_by": row.UpdatedByID, "deleted_at": row.DeletedAt,
			"status": row.Status, "snoozed_till": row.SnoozedTill, "duplicate_to": row.DuplicateToID,
			"source": row.Source, "source_email": row.SourceEmail,
			"external_source": row.ExternalSource, "external_id": row.ExternalID,
			"extra": decodeJSON(row.Extra),
			// The intake is reported twice: once under its own name and once under the one it had before.
			"intake": row.IntakeID, "inbox": row.IntakeID,
			"issue": row.IssueID, "issue_detail": details[row.IssueID],
			"project": row.ProjectID, "workspace": row.WorkspaceID,
		})
	}
	return results, nil
}
