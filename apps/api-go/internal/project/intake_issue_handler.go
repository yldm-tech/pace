package project

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"github.com/yldm-tech/pace/apps/api-go/internal/drf"
	"github.com/yldm-tech/pace/apps/api-go/internal/issues"
	"github.com/yldm-tech/pace/apps/api-go/internal/pagination"
	"gorm.io/gorm"
)

func (handler *Handler) registerIntakeIssueRoutes(router gin.IRouter) {
	// Mounted twice, like the intake itself: under its current name and the one it had before intake was called inbox.
	for _, name := range []string{"intake-issues", "inbox-issues"} {
		base := "/api/workspaces/:slug/projects/:id/" + name + "/"
		router.GET(base, handler.authenticated(handler.intakeIssueList))
		router.POST(base, handler.authenticated(handler.intakeIssueCreate))
		router.GET(base+":issue/", handler.authenticatedIssueUUID(handler.intakeIssueRetrieve))
		router.PATCH(base+":issue/", handler.authenticatedIssueUUID(handler.intakeIssueUpdate))
		router.DELETE(base+":issue/", handler.authenticatedIssueUUID(handler.intakeIssueDestroy))
	}
}

// IntakeIssue is the db.IntakeIssue table: one issue waiting in, or already triaged out of, an intake.
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

// The five statuses an intake issue moves through.
const (
	intakeStatusPending   = -2
	intakeStatusRejected  = -1
	intakeStatusSnoozed   = 0
	intakeStatusAccepted  = 1
	intakeStatusDuplicate = 2
)

// intakeIssueRow is the link with the two id lists the queryset annotates onto it.
type intakeIssueRow struct {
	IntakeIssue
	LabelIDs    pq.StringArray `gorm:"column:label_ids;type:uuid[]"`
	AssigneeIDs pq.StringArray `gorm:"column:assignee_ids;type:uuid[]"`
}

// intakeIssueOrderByAllowlist is INTAKE_ISSUE_ORDER_BY_ALLOWLIST. Most of its entries reach through the issue, which is why the default is the issue's creation time rather than the link's.
var intakeIssueOrderByAllowlist = map[string]bool{
	"issue__created_at": true, "issue__updated_at": true, "issue__sequence_id": true,
	"issue__sort_order": true, "issue__target_date": true, "issue__start_date": true,
	"issue__priority": true, "issue__state__name": true,
	"created_at": true, "updated_at": true, "status": true,
}

// intakeOrderColumn turns an allowlisted order field into the column it names.
func intakeOrderColumn(field string) string {
	descending := strings.HasPrefix(field, "-")
	bare := strings.TrimPrefix(field, "-")
	column := map[string]string{
		"issue__created_at":  "i.created_at",
		"issue__updated_at":  "i.updated_at",
		"issue__sequence_id": "i.sequence_id",
		"issue__sort_order":  "i.sort_order",
		"issue__target_date": "i.target_date",
		"issue__start_date":  "i.start_date",
		"issue__priority":    "i.priority",
		"issue__state__name": "(SELECT s.name FROM states s WHERE s.id = i.state_id)",
		"created_at":         "ii.created_at",
		"updated_at":         "ii.updated_at",
		"status":             "ii.status",
	}[bare]
	if column == "" {
		column = "i.created_at"
	}
	if descending {
		return column + " DESC"
	}
	return column
}

// intakeIssueList returns what is in the project's intake.
//
// The status filter defaults to **pending alone**, so a caller that names nothing sees only what still needs triaging rather than everything that ever passed through.
func (handler *Handler) intakeIssueList(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	slug, projectID := c.Param("slug"), c.Param("id")

	intake, found, err := handler.projectIntake(c, slug, projectID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "Intake not found"})
		return
	}

	filters := issueFilters(queryParams(c), "GET", "issue__", handler.clock().UTC())
	joins, conditions, arguments, translatable := intakeIssueFilterSQL(filters)
	if !translatable {
		handler.internalError(c, errors.New("intake issues: a filter names a field the schema does not have"))
		return
	}

	query := handler.intakeIssueScope(c, intake.ID, projectID)
	for _, join := range joins {
		query = query.Joins(join)
	}
	consumed := 0
	for _, condition := range conditions {
		count := countPlaceholders(condition)
		query = query.Where(condition, arguments[consumed:consumed+count]...)
		consumed += count
	}

	// The status parameter is a comma-separated list with "null" dropped, and an empty result narrows nothing rather than matching nothing.
	statuses := []string{}
	for _, item := range strings.Split(c.DefaultQuery("status", "-2"), ",") {
		if item != "null" {
			statuses = append(statuses, item)
		}
	}
	if len(statuses) > 0 {
		query = query.Where("ii.status IN ?", statuses)
	}

	restricted, err := handler.viewsRestrictedToOwner(c, user, slug, projectID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if restricted {
		query = query.Where("ii.created_by_id = ?", user.ID)
	}

	order := sanitizeOrderBy(c.Query("order_by"), intakeIssueOrderByAllowlist, "-issue__created_at")
	var rows []intakeIssueRow
	if err := query.Order(intakeOrderColumn(order)).Scan(&rows).Error; err != nil {
		handler.internalError(c, err)
		return
	}

	results, err := handler.intakeIssueBodies(c, rows, false)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	handler.respondPagedIntakeIssues(c, results)
}

// respondPagedIntakeIssues wraps the list in the offset paginator's envelope, which this route always uses rather than only when asked.
func (handler *Handler) respondPagedIntakeIssues(c *gin.Context, results []gin.H) {
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
	page := pagination.PlanOffsetPage(perPage, cursor, len(results), len(results), pagination.DefaultPerPage)
	offset := page.Offset
	if offset > len(results) {
		offset = len(results)
	}
	end := offset + page.Limit
	if end > len(results) {
		end = len(results)
	}
	window := results[offset:end]
	drf.Respond(c, http.StatusOK, page.Envelope(window, len(window), nil, nil, nil))
}

// intakeIssueScope is the queryset the list and the detail routes read through.
func (handler *Handler) intakeIssueScope(c *gin.Context, intakeID, projectID string) *gorm.DB {
	return handler.db.WithContext(c.Request.Context()).Table("intake_issues ii").
		Select(`ii.*,
			COALESCE((SELECT ARRAY_AGG(DISTINCT il.label_id) FROM issue_labels il
				WHERE il.issue_id = ii.issue_id AND il.deleted_at IS NULL), '{}') AS label_ids,
			COALESCE((SELECT ARRAY_AGG(DISTINCT ia.assignee_id) FROM issue_assignees ia
				JOIN project_members apm ON apm.member_id = ia.assignee_id AND apm.project_id = ii.project_id AND apm.is_active = TRUE
				WHERE ia.issue_id = ii.issue_id AND ia.deleted_at IS NULL), '{}') AS assignee_ids`).
		Joins("JOIN issues i ON i.id = ii.issue_id").
		Where("ii.intake_id = ? AND ii.project_id = ? AND ii.deleted_at IS NULL", intakeID, projectID)
}

// intakeIssueFilterSQL translates the issue filters, which arrive prefixed because they are applied to the link rather than to the issue.
func intakeIssueFilterSQL(filters map[string]filterValue) ([]string, []string, []any, bool) {
	stripped := map[string]filterValue{}
	for lookup, value := range filters {
		stripped[strings.TrimPrefix(lookup, "issue__")] = value
	}
	joins, conditions, arguments, ok := issueFilterSQL(stripped)
	return joins, conditions, arguments, ok
}

// projectIntake reads the project's intake, of which there is expected to be one.
func (handler *Handler) projectIntake(c *gin.Context, slug, projectID string) (Intake, bool, error) {
	var intakes []Intake
	err := handler.db.WithContext(c.Request.Context()).Table("intakes i").Select("i.*").
		Joins("JOIN workspaces w ON w.id = i.workspace_id").
		Where("w.slug = ? AND i.project_id = ? AND i.deleted_at IS NULL", slug, projectID).
		Order("i.created_at DESC").Limit(1).Scan(&intakes).Error
	if err != nil {
		return Intake{}, false, err
	}
	if len(intakes) == 0 {
		return Intake{}, false, nil
	}
	return intakes[0], true, nil
}

// intakeIssueCreate puts a new work item into the intake, in the triage state.
//
// The issue is created with the project's triage state, which is made on the spot if the project has none — a project that has never used intake gets one the first time something lands in it.
func (handler *Handler) intakeIssueCreate(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	slug, projectID := c.Param("slug"), c.Param("id")

	var body map[string]json.RawMessage
	if err := c.ShouldBindJSON(&body); err != nil {
		handler.invalidDetail(c)
		return
	}
	var issueBody map[string]json.RawMessage
	if raw, present := body["issue"]; present {
		if err := json.Unmarshal(raw, &issueBody); err != nil {
			issueBody = nil
		}
	}
	name := ""
	if raw, present := issueBody["name"]; present {
		_ = json.Unmarshal(raw, &name)
	}
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Name is required"})
		return
	}
	priority := "none"
	if raw, present := issueBody["priority"]; present {
		if err := json.Unmarshal(raw, &priority); err != nil {
			priority = ""
		}
	}
	// The priority is checked here rather than by the serializer, so an unknown one is a plain message instead of a field error.
	if !validIssuePriorities[priority] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid priority"})
		return
	}

	var project Project
	err := handler.db.WithContext(c.Request.Context()).Where("id = ?", projectID).Take(&project).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	intake, found, err := handler.projectIntake(c, slug, projectID)
	if err != nil {
		handler.internalError(c, err)
		return
	}

	now := handler.clock().UTC()
	triageID, err := handler.ensureTriageState(c, slug, projectID, project.WorkspaceID, now)
	if err != nil {
		handler.internalError(c, err)
		return
	}

	fields, ok := handler.issueFields(c, issueBody, projectID)
	if !ok {
		return
	}
	fields.values["state_id"] = triageID

	assignees := fields.assigneeIDs
	if !fields.hasAssignees || len(assignees) == 0 {
		assignees, err = handler.defaultAssignee(c.Request.Context(), project, projectID)
		if err != nil {
			handler.internalError(c, err)
			return
		}
	}

	issueID, err := newUUID()
	if err != nil {
		handler.internalError(c, err)
		return
	}
	linkID, err := newUUID()
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		// Django reads the id off the result of .first() with no guard, so a project with no intake raises rather than answering.
		handler.internalError(c, errors.New("intake issues: the project has no intake"))
		return
	}

	source := "IN_APP"
	link := IntakeIssue{
		ID: linkID, CreatedAt: now, UpdatedAt: now, CreatedByID: &user.ID, UpdatedByID: &user.ID,
		ProjectID: projectID, WorkspaceID: project.WorkspaceID, IntakeID: intake.ID, IssueID: issueID,
		Status: intakeStatusPending, Source: &source, Extra: []byte("{}"),
	}
	err = handler.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := handler.writeNewIssue(tx, issueID, projectID, project, user.ID, now, fields, assignees); err != nil {
			return err
		}
		return tx.Create(&link).Error
	})
	if err != nil {
		handler.internalError(c, err)
		return
	}

	requested, err := json.Marshal(decodeRawFields(body))
	if err != nil {
		handler.internalError(c, err)
		return
	}
	requestedData := string(requested)
	err = handler.publishIssueActivity(c, issueActivity{
		Type: "issue.activity.created", RequestedData: &requestedData,
		ActorID: user.ID, IssueID: issueID, ProjectID: projectID,
		Notification: true, Origin: handler.origin(), Epoch: now, IntakeID: link.ID,
	})
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if handler.tasks != nil {
		// The version task is told this is a creation, which is what makes it record the first version rather than a diff.
		err = handler.tasks.PublishIssueDescriptionVersion(c.Request.Context(), requestedData, issueID, user.ID)
		if err != nil {
			handler.internalError(c, err)
			return
		}
	}

	rows, err := handler.intakeIssueRows(c, intake.ID, projectID, issueID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if len(rows) == 0 {
		handler.internalError(c, errors.New("intake issues: the link could not be read back"))
		return
	}
	bodies, err := handler.intakeIssueBodies(c, rows, true)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	// The create answers 200 rather than 201, which is what the handler returns.
	drf.Respond(c, http.StatusOK, bodies[0])
}

// validIssuePriorities is the list the create checks against by hand.
var validIssuePriorities = map[string]bool{"low": true, "medium": true, "high": true, "urgent": true, "none": true}

// ensureTriageState returns the project's triage state, creating it when the project has none.
func (handler *Handler) ensureTriageState(c *gin.Context, slug, projectID, workspaceID string, now time.Time) (string, error) {
	var existing []string
	err := handler.db.WithContext(c.Request.Context()).Table("states s").
		Joins("JOIN workspaces w ON w.id = s.workspace_id").
		Where("w.slug = ? AND s.project_id = ? AND s.deleted_at IS NULL AND s.is_triage = TRUE", slug, projectID).
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
		ProjectID: projectID, WorkspaceID: workspaceID,
		Name: "Triage", Group: "triage", Color: "#4E5355", Sequence: 65000, Default: false,
	}
	if err := handler.db.WithContext(c.Request.Context()).Create(&state).Error; err != nil {
		return "", err
	}
	return state.ID, nil
}

// intakeIssueRetrieve returns one intake issue.
func (handler *Handler) intakeIssueRetrieve(c *gin.Context, user *auth.User) {
	if !handler.requireIntakeIssueAccess(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	slug, projectID, issueID := c.Param("slug"), c.Param("id"), c.Param("issue")

	intake, found, err := handler.projectIntake(c, slug, projectID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		handler.internalError(c, errors.New("intake issues: the project has no intake"))
		return
	}
	rows, err := handler.intakeIssueRows(c, intake.ID, projectID, issueID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if len(rows) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "The required object does not exist."})
		return
	}
	restricted, err := handler.viewsRestrictedToOwner(c, user, slug, projectID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if restricted && (rows[0].CreatedByID == nil || *rows[0].CreatedByID != user.ID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "You are not allowed to view this issue"})
		return
	}
	bodies, err := handler.intakeIssueBodies(c, rows, true)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, bodies[0])
}

// intakeIssueDestroy removes an intake issue, and the work item with it unless it was accepted.
//
// An accepted issue has left the intake and become ordinary work, so only the link goes. Every other status takes the issue with it.
func (handler *Handler) intakeIssueDestroy(c *gin.Context, user *auth.User) {
	if !handler.requireIntakeIssueAccess(c, user, roleAdmin) {
		return
	}
	slug, projectID, issueID := c.Param("slug"), c.Param("id"), c.Param("issue")

	intake, found, err := handler.projectIntake(c, slug, projectID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		handler.internalError(c, errors.New("intake issues: the project has no intake"))
		return
	}
	rows, err := handler.intakeIssueRows(c, intake.ID, projectID, issueID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if len(rows) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "The required object does not exist."})
		return
	}

	now := handler.clock().UTC()
	link := rows[0]
	err = handler.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if link.Status != intakeStatusAccepted {
			err := tx.Model(&Issue{}).Where("id = ?", link.IssueID).Update("deleted_at", now).Error
			if err != nil {
				return err
			}
		}
		return tx.Model(&IntakeIssue{}).Where("id = ?", link.ID).Update("deleted_at", now).Error
	})
	if err != nil {
		handler.internalError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// intakeIssueRows reads the link and its two id lists.
func (handler *Handler) intakeIssueRows(c *gin.Context, intakeID, projectID, issueID string) ([]intakeIssueRow, error) {
	var rows []intakeIssueRow
	err := handler.intakeIssueScope(c, intakeID, projectID).
		Where("ii.issue_id = ?", issueID).Limit(1).Scan(&rows).Error
	return rows, err
}

// requireIntakeIssueAccess is allow_permission with creator=True over the **issue**, not the link, which is why a guest who raised something can still reach it.
func (handler *Handler) requireIntakeIssueAccess(c *gin.Context, user *auth.User, roles ...int) bool {
	var createdBy []string
	err := handler.db.WithContext(c.Request.Context()).Table("issues").
		Where("id = ?", c.Param("issue")).Limit(1).Pluck("created_by_id", &createdBy).Error
	if err != nil {
		handler.internalError(c, err)
		return false
	}
	var creator *string
	if len(createdBy) > 0 {
		creator = &createdBy[0]
	}
	return handler.requireViewCreatorOrRoles(c, user, creator, roles...)
}

// intakeIssueBodies renders the link and the issue inside it.
//
// The two serializers differ in what they wrap the issue in: the list uses the intake-shaped one, the detail the full issue, and only the detail carries the duplicate it points at.
func (handler *Handler) intakeIssueBodies(c *gin.Context, rows []intakeIssueRow, detailed bool) ([]gin.H, error) {
	results := make([]gin.H, 0, len(rows))
	if len(rows) == 0 {
		return results, nil
	}
	issueIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		issueIDs = append(issueIDs, row.IssueID)
		if row.DuplicateToID != nil {
			issueIDs = append(issueIDs, *row.DuplicateToID)
		}
	}
	issues, err := handler.intakeIssueDetails(c, issueIDs)
	if err != nil {
		return nil, err
	}

	for _, row := range rows {
		issue := issues[row.IssueID]
		if issue != nil {
			// The annotated lists live on the link and are handed to the issue on the way out, which is what to_representation does.
			issue["label_ids"] = stringsOrEmpty(row.LabelIDs)
			if detailed {
				issue["assignee_ids"] = stringsOrEmpty(row.AssigneeIDs)
			}
		}
		data := gin.H{
			"id": row.ID, "status": row.Status, "duplicate_to": row.DuplicateToID,
			"snoozed_till": row.SnoozedTill, "source": row.Source, "issue": issue,
		}
		if detailed {
			var duplicate gin.H
			if row.DuplicateToID != nil {
				duplicate = issues[*row.DuplicateToID]
			}
			data["duplicate_issue_detail"] = duplicate
		} else {
			data["created_by"] = row.CreatedByID
		}
		results = append(results, data)
	}
	return results, nil
}

// intakeIssueDetails reads the issues the bodies wrap.
func (handler *Handler) intakeIssueDetails(c *gin.Context, issueIDs []string) (map[string]gin.H, error) {
	details := map[string]gin.H{}
	if len(issueIDs) == 0 {
		return details, nil
	}
	var rows []issueRow
	err := handler.db.WithContext(c.Request.Context()).Table("issues i").
		Select(issueDetailAnnotations()).
		Where("i.id IN ? AND i.deleted_at IS NULL", issueIDs).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		details[row.ID] = issueDetailJSON(row, false)
	}
	return details, nil
}

// intakeIssueUpdate moves an intake issue along, and edits the work item under it.
//
// The two halves are separately gated. A guest may edit the work item, and only its name and description at that — everything else they send is dropped. The link itself, and so the status, moves only for someone above a member or a workspace admin.
func (handler *Handler) intakeIssueUpdate(c *gin.Context, user *auth.User) {
	if !handler.requireIntakeIssueAccess(c, user, roleAdmin) {
		return
	}
	slug, projectID, issueID := c.Param("slug"), c.Param("id"), c.Param("issue")

	var body map[string]json.RawMessage
	if err := c.ShouldBindJSON(&body); err != nil {
		handler.invalidDetail(c)
		return
	}

	intake, found, err := handler.projectIntake(c, slug, projectID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		handler.internalError(c, errors.New("intake issues: the project has no intake"))
		return
	}
	rows, err := handler.intakeIssueRows(c, intake.ID, projectID, issueID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if len(rows) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "The required object does not exist."})
		return
	}
	link := rows[0]

	member, inProject, err := handler.activeProjectMember(c.Request.Context(), slug, projectID, user.ID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	workspaceRole, _, err := handler.workspaceMemberRole(c.Request.Context(), slug, user.ID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	isWorkspaceAdmin := workspaceRole == roleAdmin
	if !inProject && !isWorkspaceAdmin {
		c.JSON(http.StatusForbidden, gin.H{"error": "Only admin or creator can update the intake work items"})
		return
	}
	isGuest := inProject && member.Role <= roleGuest
	if isGuest && !isWorkspaceAdmin && (link.CreatedByID == nil || *link.CreatedByID != user.ID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "You cannot edit intake issues"})
		return
	}

	now := handler.clock().UTC()
	var issueBody map[string]json.RawMessage
	if raw, present := body["issue"]; present {
		if err := json.Unmarshal(raw, &issueBody); err != nil {
			issueBody = nil
		}
	}
	if len(issueBody) > 0 {
		if isGuest {
			// A guest's edit is narrowed to three fields before it reaches the serializer, so everything else they sent is dropped rather than refused.
			narrowed := map[string]json.RawMessage{}
			for _, field := range []string{"name", "description_html", "description_json"} {
				if raw, present := issueBody[field]; present {
					narrowed[field] = raw
				}
			}
			issueBody = narrowed
		}
		if err := handler.applyIntakeIssueEdit(c, user, issueBody, projectID, link, now); err != nil {
			return
		}
	}

	// Only someone above a member moves the link, which is what keeps a guest from accepting their own work item.
	if (inProject && member.Role > roleMember) || isWorkspaceAdmin {
		if !handler.applyIntakeStatus(c, user, body, link, projectID, now) {
			return
		}
	}

	rows, err = handler.intakeIssueRows(c, intake.ID, projectID, issueID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	bodies, err := handler.intakeIssueBodies(c, rows, true)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, bodies[0])
}

// applyIntakeIssueEdit writes the work item half of an update and records what changed.
func (handler *Handler) applyIntakeIssueEdit(c *gin.Context, user *auth.User, issueBody map[string]json.RawMessage, projectID string, link intakeIssueRow, now time.Time) error {
	fields, ok := handler.issueFields(c, issueBody, projectID)
	if !ok {
		return errors.New("intake issues: the work item payload is invalid")
	}
	current, err := handler.intakeIssueDetails(c, []string{link.IssueID})
	if err != nil {
		handler.internalError(c, err)
		return err
	}
	snapshot, err := json.Marshal(current[link.IssueID])
	if err != nil {
		handler.internalError(c, err)
		return err
	}
	requested, err := json.Marshal(decodeRawFields(issueBody))
	if err != nil {
		handler.internalError(c, err)
		return err
	}

	var issue Issue
	if err := handler.db.WithContext(c.Request.Context()).Where("id = ?", link.IssueID).Take(&issue).Error; err != nil {
		handler.internalError(c, err)
		return err
	}
	err = handler.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		updates := fields.updates(now, user.ID)
		if err := applyIssueSavePath(tx, issue, updates, now); err != nil {
			return err
		}
		if err := tx.Model(&Issue{}).Where("id = ?", issue.ID).Updates(updates).Error; err != nil {
			return err
		}
		if fields.hasAssignees {
			if err := handler.syncIssueAssignees(tx, issue, fields.assigneeIDs, now); err != nil {
				return err
			}
		}
		if fields.hasLabels {
			return handler.syncIssueLabels(tx, issue, fields.labelIDs, now)
		}
		return nil
	})
	if err != nil {
		handler.internalError(c, err)
		return err
	}

	// skip_activity together with a description change is how the migration tool writes without leaving a trail; anything else is recorded.
	skipActivity := false
	if raw, present := issueBody["skip_activity"]; present {
		_ = json.Unmarshal(raw, &skipActivity)
	}
	_, describes := issueBody["description_html"]
	if skipActivity && describes {
		return nil
	}

	requestedData := string(requested)
	currentInstance := string(snapshot)
	err = handler.publishIssueActivity(c, issueActivity{
		Type: "issue.activity.updated", RequestedData: &requestedData, CurrentInstance: &currentInstance,
		ActorID: user.ID, IssueID: link.IssueID, ProjectID: projectID,
		Notification: true, Origin: handler.origin(), Epoch: now, IntakeID: link.ID,
	})
	if err != nil {
		handler.internalError(c, err)
		return err
	}
	if handler.tasks != nil {
		// The version task is handed the **previous** state rather than the new one, which is what makes the version the state before the edit.
		err = handler.tasks.PublishIssueDescriptionVersion(c.Request.Context(), currentInstance, link.IssueID, user.ID)
		if err != nil {
			handler.internalError(c, err)
			return err
		}
	}
	return nil
}

// applyIntakeStatus writes the link half of an update: the status, the snooze and the duplicate it points at.
//
// Accepting an issue that is still in triage moves it to the project's default state, and a project with no default refuses the acceptance rather than leaving the issue stuck in triage.
func (handler *Handler) applyIntakeStatus(c *gin.Context, user *auth.User, body map[string]json.RawMessage, link intakeIssueRow, projectID string, now time.Time) bool {
	updates := map[string]any{}
	status := link.Status
	if raw, present := body["status"]; present {
		if err := json.Unmarshal(raw, &status); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"status": []string{"A valid integer is required."}})
			return false
		}
		updates["status"] = status
	}
	if raw, present := body["snoozed_till"]; present {
		var snoozed *string
		if err := json.Unmarshal(raw, &snoozed); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"snoozed_till": []string{"Not a valid string."}})
			return false
		}
		if snoozed == nil {
			updates["snoozed_till"] = nil
		} else {
			parsed, err := time.Parse(time.RFC3339, *snoozed)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"snoozed_till": []string{
					"Datetime has wrong format. Use one of these formats instead: YYYY-MM-DDThh:mm[:ss[.uuuuuu]][+HH:MM|-HH:MM|Z].",
				}})
				return false
			}
			updates["snoozed_till"] = parsed
		}
	}
	if raw, present := body["duplicate_to"]; present {
		var duplicate *string
		if err := json.Unmarshal(raw, &duplicate); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"duplicate_to": []string{"Not a valid string."}})
			return false
		}
		updates["duplicate_to_id"] = duplicate
	}
	if len(updates) == 0 {
		return true
	}

	// The default state is checked before anything is written, so a project without one leaves the issue where it was.
	defaultStateID := ""
	if status == intakeStatusAccepted {
		group, err := issues.StateGroup(handler.db.WithContext(c.Request.Context()), link.issueStateID(c, handler))
		if err != nil {
			handler.internalError(c, err)
			return false
		}
		if group == "triage" {
			var defaults []string
			err := handler.db.WithContext(c.Request.Context()).Table("states").
				Where("project_id = ? AND workspace_id = ? AND \"default\" = TRUE AND deleted_at IS NULL",
					projectID, link.WorkspaceID).
				Order("created_at").Limit(1).Pluck("id", &defaults).Error
			if err != nil {
				handler.internalError(c, err)
				return false
			}
			if len(defaults) == 0 {
				c.JSON(http.StatusBadRequest, gin.H{
					"status": "Cannot accept intake issue: No default state found for the project",
				})
				return false
			}
			defaultStateID = defaults[0]
		}
	}

	snapshot, err := json.Marshal(gin.H{
		"id": link.ID, "status": link.Status, "duplicate_to": link.DuplicateToID,
		"snoozed_till": link.SnoozedTill, "source": link.Source, "created_by": link.CreatedByID,
	})
	if err != nil {
		handler.internalError(c, err)
		return false
	}
	requested, err := json.Marshal(decodeRawFields(body))
	if err != nil {
		handler.internalError(c, err)
		return false
	}

	updates["updated_at"] = now
	updates["updated_by_id"] = user.ID
	err = handler.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&IntakeIssue{}).Where("id = ?", link.ID).Updates(updates).Error; err != nil {
			return err
		}
		if defaultStateID == "" {
			return nil
		}
		return tx.Model(&Issue{}).Where("id = ?", link.IssueID).
			Updates(map[string]any{"state_id": defaultStateID, "updated_at": now, "updated_by_id": user.ID}).Error
	})
	if err != nil {
		handler.internalError(c, err)
		return false
	}

	requestedData := string(requested)
	currentInstance := string(snapshot)
	// The status activity is sent without a notification, unlike the work item's.
	err = handler.publishIssueActivity(c, issueActivity{
		Type: "intake.activity.created", RequestedData: &requestedData, CurrentInstance: &currentInstance,
		ActorID: user.ID, IssueID: link.IssueID, ProjectID: projectID,
		Notification: false, Origin: handler.origin(), Epoch: now, IntakeID: link.ID,
	})
	if err != nil {
		handler.internalError(c, err)
		return false
	}
	return true
}

// issueStateID reads the state the work item is currently in.
func (row intakeIssueRow) issueStateID(c *gin.Context, handler *Handler) string {
	var states []string
	err := handler.db.WithContext(c.Request.Context()).Table("issues").
		Where("id = ?", row.IssueID).Limit(1).Pluck("state_id", &states).Error
	if err != nil || len(states) == 0 {
		return ""
	}
	return states[0]
}
