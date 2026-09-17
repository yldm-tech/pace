package project

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
	"github.com/yldm-tech/pace/apps/api/internal/auth"
	"github.com/yldm-tech/pace/apps/api/internal/drf"
	"github.com/yldm-tech/pace/apps/api/internal/htmlsanitizer"
	"github.com/yldm-tech/pace/apps/api/internal/issues"
	"github.com/yldm-tech/pace/apps/api/internal/pagination"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// The draft routes hang off the workspace rather than a project, because a draft does not need a project yet — that is the whole point of one. They live in this package all the same, since turning a draft into a work item runs the work item's own creation path.
func (handler *Handler) registerDraftIssueRoutes(router gin.IRouter) {
	const base = "/api/workspaces/:slug/draft-issues/"
	router.GET(base, handler.authenticated(handler.draftIssueList))
	router.POST(base, handler.authenticated(handler.draftIssueCreate))
	router.GET(base+":id/", handler.authenticatedUUID(handler.draftIssueRetrieve))
	router.PATCH(base+":id/", handler.authenticatedUUID(handler.draftIssueUpdate))
	router.DELETE(base+":id/", handler.authenticatedUUID(handler.draftIssueDestroy))
	router.POST("/api/workspaces/:slug/draft-to-issue/:id/", handler.authenticatedUUID(handler.draftIssueToIssue))
}

// DraftIssue is a work item somebody started and has not raised yet. It carries most of a work item's columns but none of the ones that only mean something once it exists: no sequence number, no archived_at, no is_draft.
type DraftIssue struct {
	ID                  string         `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt           time.Time      `gorm:"column:created_at"`
	UpdatedAt           time.Time      `gorm:"column:updated_at"`
	CreatedByID         *string        `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID         *string        `gorm:"column:updated_by_id;type:uuid"`
	DeletedAt           *time.Time     `gorm:"column:deleted_at"`
	WorkspaceID         string         `gorm:"column:workspace_id;type:uuid"`
	ProjectID           *string        `gorm:"column:project_id;type:uuid"`
	ParentID            *string        `gorm:"column:parent_id;type:uuid"`
	StateID             *string        `gorm:"column:state_id;type:uuid"`
	EstimatePointID     *string        `gorm:"column:estimate_point_id;type:uuid"`
	TypeID              *string        `gorm:"column:type_id;type:uuid"`
	Name                *string        `gorm:"column:name"`
	DescriptionJSON     auth.JSONValue `gorm:"column:description_json;type:jsonb"`
	DescriptionHTML     string         `gorm:"column:description_html"`
	DescriptionStripped *string        `gorm:"column:description_stripped"`
	Priority            string         `gorm:"column:priority"`
	StartDate           *time.Time     `gorm:"column:start_date"`
	TargetDate          *time.Time     `gorm:"column:target_date"`
	SortOrder           float64        `gorm:"column:sort_order"`
	CompletedAt         *time.Time     `gorm:"column:completed_at"`
	ExternalSource      *string        `gorm:"column:external_source"`
	ExternalID          *string        `gorm:"column:external_id"`
}

func (DraftIssue) TableName() string { return "draft_issues" }

// draftIssueRow is a draft plus the four annotations the queryset adds.
type draftIssueRow struct {
	DraftIssue
	CycleID     *string        `gorm:"column:cycle_id"`
	LabelIDs    pq.StringArray `gorm:"column:label_ids;type:uuid[]"`
	AssigneeIDs pq.StringArray `gorm:"column:assignee_ids;type:uuid[]"`
	ModuleIDs   pq.StringArray `gorm:"column:module_ids;type:uuid[]"`
}

// draftIssueAnnotations is the queryset's four annotations. Each reads its own through table directly rather than joining the target, which is what keeps a soft-deleted link out of the list.
const draftIssueAnnotations = `d.*,
	(SELECT dc.cycle_id FROM draft_issue_cycles dc WHERE dc.draft_issue_id = d.id AND dc.deleted_at IS NULL LIMIT 1) AS cycle_id,
	COALESCE((SELECT ARRAY_AGG(DISTINCT dl.label_id) FROM draft_issue_labels dl WHERE dl.draft_issue_id = d.id AND dl.deleted_at IS NULL), '{}') AS label_ids,
	COALESCE((SELECT ARRAY_AGG(DISTINCT da.assignee_id) FROM draft_issue_assignees da WHERE da.draft_issue_id = d.id AND da.deleted_at IS NULL), '{}') AS assignee_ids,
	COALESCE((SELECT ARRAY_AGG(DISTINCT dm.module_id) FROM draft_issue_modules dm
		JOIN modules m ON m.id = dm.module_id AND m.archived_at IS NULL
		WHERE dm.draft_issue_id = d.id AND dm.deleted_at IS NULL), '{}') AS module_ids`

// draftIssueList is the caller's own drafts across the whole workspace, newest first.
//
// Only the lookups that land on a draft's own columns work here. Everything else — labels, assignees, modules, cycles, subscribers, mentions, intake status — goes through a relation name a DraftIssue does not have, and Django raises FieldError rather than ignoring it, so ?labels= on this route is a 500 today. That is reproduced rather than corrected: the filters are refused here exactly where the ORM refuses them.
func (handler *Handler) draftIssueList(c *gin.Context, user *auth.User) {
	if !handler.requireWorkspaceRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
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

	conditions, arguments, translatable := draftIssueFilterSQL(issueFilters(queryParams(c), "GET", "", handler.clock().UTC()))
	if !translatable {
		handler.internalError(c, errors.New("draft issues: a filter names a relation a draft does not have"))
		return
	}

	scope := func() *gorm.DB {
		query := handler.db.WithContext(c.Request.Context()).Table("draft_issues d").
			Joins("JOIN workspaces w ON w.id = d.workspace_id").
			Where("w.slug = ? AND d.created_by_id = ? AND d.deleted_at IS NULL", c.Param("slug"), user.ID)
		consumed := 0
		for _, condition := range conditions {
			count := countPlaceholders(condition)
			query = query.Where(condition, arguments[consumed:consumed+count]...)
			consumed += count
		}
		return query
	}

	var total int64
	if err := scope().Distinct("d.id").Count(&total).Error; err != nil {
		handler.internalError(c, err)
		return
	}
	window := pagination.PlanOffsetPage(perPage, cursor, int(total), 0, pagination.DefaultPerPage)
	var rows []draftIssueRow
	err = scope().Select(draftIssueAnnotations).Order("d.created_at DESC").
		Offset(window.Offset).Limit(window.Stop - window.Offset).Scan(&rows).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	page := pagination.PlanOffsetPage(perPage, cursor, int(total), len(rows), pagination.DefaultPerPage)
	if len(rows) > page.Limit {
		rows = rows[:page.Limit]
	}
	results := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		results = append(results, draftIssueJSON(row))
	}
	drf.Respond(c, http.StatusOK, page.Envelope(results, len(results), nil, nil, nil))
}

// draftIssueRetrieve reads one draft.
//
// Its role list is ADMIN alone, and the creator rule beside it names Issue rather than DraftIssue — so it looks up the draft's id in the work item table, where it will not be. A member therefore cannot read back a draft they just made; only a workspace admin can. Reproduced as it stands.
func (handler *Handler) draftIssueRetrieve(c *gin.Context, user *auth.User) {
	if !handler.requireWorkspaceRole(c, user, roleAdmin) {
		return
	}
	row, found, err := handler.draftIssueByID(c, user.ID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		handler.notFound(c)
		return
	}
	drf.Respond(c, http.StatusOK, draftIssueJSON(row))
}

// draftIssueCreate makes a draft. The project is optional, which is what separates this from raising a work item: a draft can wait until somebody decides where it belongs.
//
// The project the payload names is taken as it stands rather than checked, because the view reads it straight out of the request body and hands it to the model. A project from another workspace is therefore accepted, and the draft follows it — WorkspaceBaseModel.save reads the workspace off the project rather than from the url.
func (handler *Handler) draftIssueCreate(c *gin.Context, user *auth.User) {
	if !handler.requireWorkspaceRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	var workspaceIDs []string
	err := handler.db.WithContext(c.Request.Context()).Table("workspaces").
		Where("slug = ?", c.Param("slug")).Limit(1).Pluck("id", &workspaceIDs).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if len(workspaceIDs) == 0 {
		handler.notFound(c)
		return
	}

	var body map[string]json.RawMessage
	if err := c.ShouldBindJSON(&body); err != nil {
		handler.invalidDetail(c)
		return
	}
	projectID, ok := handler.draftIssueProject(c, body)
	if !ok {
		return
	}
	fields, ok := handler.draftIssueFields(c, body, projectID)
	if !ok {
		return
	}

	identifier, err := newUUID()
	if err != nil {
		handler.internalError(c, err)
		return
	}
	now := handler.clock().UTC()
	values := map[string]any{"id": identifier}
	for key, value := range fields.values {
		values[key] = value
	}
	values["workspace_id"] = workspaceIDs[0]
	values["created_at"] = now
	values["updated_at"] = now
	// On creation crum sets created_by and leaves updated_by empty; the second one only fills in when somebody edits the draft later.
	values["created_by_id"] = user.ID
	values["updated_by_id"] = nil
	var linkProject *string
	if projectID != "" {
		linkProject = &projectID
	}
	values["project_id"] = linkProject
	for column, fallback := range map[string]any{
		"description_json": auth.JSONValue("{}"), "description_html": "<p></p>",
		"priority": "none", "sort_order": 65535.0,
	} {
		if _, given := values[column]; !given {
			values[column] = fallback
		}
	}

	err = handler.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := draftIssueSavePath(tx, values, projectID, true, now); err != nil {
			return err
		}
		// The workspace follows the project whenever there is one, which is how a draft aimed at another workspace's project ends up there.
		if projectID != "" {
			var owners []string
			if err := tx.Table("projects").Where("id = ?", projectID).Limit(1).Pluck("workspace_id", &owners).Error; err != nil {
				return err
			}
			if len(owners) > 0 {
				values["workspace_id"] = owners[0]
			}
		}
		if err := tx.Table("draft_issues").Create(values).Error; err != nil {
			return err
		}
		// The links keep the workspace the url named even when the draft itself followed its project into another one.
		return handler.writeDraftRelations(tx, identifier, workspaceIDs[0], linkProject, user.ID, now, fields, true, nil, nil)
	})
	if err != nil {
		handler.internalError(c, err)
		return
	}

	row, found, err := handler.draftIssueRow(c, identifier)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		handler.internalError(c, errors.New("draft issues: the draft could not be read back"))
		return
	}
	drf.Respond(c, http.StatusCreated, draftIssueJSON(row))
}

// draftIssueUpdate edits a draft and answers with nothing at all.
//
// Its creator rule names Issue rather than DraftIssue, the same slip the read has, so what actually decides the request is the workspace role: admin or member. A guest cannot edit a draft they made themselves.
func (handler *Handler) draftIssueUpdate(c *gin.Context, user *auth.User) {
	if !handler.requireWorkspaceRole(c, user, roleAdmin, roleMember) {
		return
	}
	row, found, err := handler.draftIssueByID(c, user.ID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "Issue not found"})
		return
	}

	var body map[string]json.RawMessage
	if err := c.ShouldBindJSON(&body); err != nil {
		handler.invalidDetail(c)
		return
	}
	// The project is only the context the related checks run against. An edit never moves a draft between projects, whatever project_id the payload carries.
	projectID := ""
	if row.ProjectID != nil {
		projectID = *row.ProjectID
	}
	if raw, given := body["project_id"]; given {
		named, ok := handler.draftIssueProjectValue(c, raw)
		if !ok {
			return
		}
		projectID = named
	}
	fields, ok := handler.draftIssueFields(c, body, projectID)
	if !ok {
		return
	}

	now := handler.clock().UTC()
	values := map[string]any{}
	for key, value := range fields.values {
		values[key] = value
	}
	if _, given := values["state_id"]; !given && row.StateID != nil {
		values["state_id"] = *row.StateID
	}
	if _, given := values["description_html"]; !given {
		values["description_html"] = row.DescriptionHTML
	}
	values["updated_at"] = now
	values["updated_by_id"] = user.ID

	err = handler.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := draftIssueSavePath(tx, values, projectID, false, now); err != nil {
			return err
		}
		if err := tx.Table("draft_issues").Where("id = ?", row.ID).Updates(values).Error; err != nil {
			return err
		}
		// An edit puts the links in the draft's own project, whatever project the payload named for the validation to run against.
		return handler.writeDraftRelations(tx, row.ID, row.WorkspaceID, row.ProjectID, user.ID, now, fields, false, row.CreatedByID, row.UpdatedByID)
	})
	if err != nil {
		handler.internalError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// draftIssueDestroy takes a draft away. This is the one route whose creator rule names the right model, so somebody who is only a member of the workspace can delete a draft they made — and a workspace admin can delete anybody's.
func (handler *Handler) draftIssueDestroy(c *gin.Context, user *auth.User) {
	allowed, err := handler.draftIssueDeletable(c, user)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !allowed {
		return
	}
	// The lookup is over the whole workspace rather than the caller's own drafts, and a missing one is a 404 through the view's exception handler.
	var rows []DraftIssue
	err = handler.db.WithContext(c.Request.Context()).Table("draft_issues d").Select("d.*").
		Joins("JOIN workspaces w ON w.id = d.workspace_id").
		Where("w.slug = ? AND d.id = ? AND d.deleted_at IS NULL", c.Param("slug"), c.Param("id")).
		Limit(1).Scan(&rows).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if len(rows) == 0 {
		handler.notFound(c)
		return
	}
	draft := rows[0]

	now := handler.clock().UTC()
	projectID := ""
	if draft.ProjectID != nil {
		projectID = *draft.ProjectID
	}
	// A soft delete goes through save(), so the same three columns the save path writes are rewritten on the way out.
	values := map[string]any{
		"deleted_at": now, "updated_at": now, "updated_by_id": user.ID,
		"description_html": draft.DescriptionHTML,
	}
	if draft.StateID != nil {
		values["state_id"] = *draft.StateID
	}
	err = handler.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := draftIssueSavePath(tx, values, projectID, false, now); err != nil {
			return err
		}
		return tx.Table("draft_issues").Where("id = ?", draft.ID).Updates(values).Error
	})
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if handler.tasks != nil {
		err := handler.tasks.PublishSoftDeleteRelatedObjects(c.Request.Context(), "db", "draftissue", draft.ID)
		if err != nil {
			handler.internalError(c, err)
			return
		}
	}
	c.Status(http.StatusNoContent)
}

// draftIssueToIssue raises the draft as a real work item and then removes the draft.
//
// The payload is what becomes the work item, not the draft: the draft supplies only the project it is aimed at and the assets that were uploaded against it. So a field that was filled in on the draft and left out of this request is simply not carried over.
func (handler *Handler) draftIssueToIssue(c *gin.Context, user *auth.User) {
	if !handler.requireWorkspaceRole(c, user, roleAdmin, roleMember) {
		return
	}
	slug, draftID := c.Param("slug"), c.Param("id")
	row, found, err := handler.draftIssueRow(c, draftID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		// Django reads the draft without checking it was found and then asks for its project, which raises. That answers 500.
		handler.internalError(c, errors.New("draft to issue: the draft does not exist"))
		return
	}
	if row.ProjectID == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Project is required to create an issue."})
		return
	}
	projectID := *row.ProjectID

	var project Project
	err = handler.db.WithContext(c.Request.Context()).Where("id = ?", projectID).Take(&project).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}

	var body map[string]json.RawMessage
	if err := c.ShouldBindJSON(&body); err != nil {
		handler.invalidDetail(c)
		return
	}
	fields, ok := handler.issueFields(c, body, projectID)
	if !ok {
		return
	}
	name, hasName := fields.values["name"]
	if !hasName || name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"name": []string{"This field is required."}})
		return
	}

	now := handler.clock().UTC()
	issueID, err := newUUID()
	if err != nil {
		handler.internalError(c, err)
		return
	}
	assignees := fields.assigneeIDs
	if !fields.hasAssignees || len(assignees) == 0 {
		assignees, err = handler.defaultAssignee(c.Request.Context(), project, projectID)
		if err != nil {
			handler.internalError(c, err)
			return
		}
	}

	cycleID, ok := handler.draftIssueCycleTarget(c, body)
	if !ok {
		return
	}
	moduleIDs, ok := handler.draftIssueModuleTargets(c, body)
	if !ok {
		return
	}

	var cycleLink *CycleIssue
	err = handler.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := handler.writeNewIssue(tx, issueID, projectID, project, user.ID, now, fields, assignees); err != nil {
			return err
		}
		if cycleID != "" {
			linkID, err := newUUID()
			if err != nil {
				return err
			}
			// The link copies the draft's own audit columns rather than the caller's, which is what Django passes here.
			link := CycleIssue{
				ID: linkID, CreatedAt: now, UpdatedAt: now,
				CreatedByID: row.CreatedByID, UpdatedByID: row.UpdatedByID,
				CycleID: cycleID, IssueID: issueID, ProjectID: projectID, WorkspaceID: row.WorkspaceID,
			}
			if err := tx.Create(&link).Error; err != nil {
				return err
			}
			cycleLink = &link
		}
		links := make([]ModuleIssue, 0, len(moduleIDs))
		for _, moduleID := range moduleIDs {
			linkID, err := newUUID()
			if err != nil {
				return err
			}
			links = append(links, ModuleIssue{
				ID: linkID, CreatedAt: now, UpdatedAt: now,
				CreatedByID: row.CreatedByID, UpdatedByID: row.UpdatedByID,
				ModuleID: moduleID, IssueID: issueID, ProjectID: projectID, WorkspaceID: row.WorkspaceID,
			})
		}
		if len(links) > 0 {
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&links).Error; err != nil {
				return err
			}
		}
		// The assets that were uploaded against the draft move onto the work item and stop being draft assets.
		err := tx.Table("file_assets").
			Where("draft_issue_id = ? AND deleted_at IS NULL", draftID).
			Updates(map[string]any{
				"issue_id": issueID, "entity_type": "ISSUE_DESCRIPTION", "draft_issue_id": nil,
			}).Error
		if err != nil {
			return err
		}
		return tx.Table("draft_issues").Where("id = ?", draftID).
			Updates(map[string]any{"deleted_at": now, "updated_at": now, "updated_by_id": user.ID}).Error
	})
	if err != nil {
		handler.internalError(c, err)
		return
	}

	if err := handler.publishDraftIssueActivities(c, user, body, row, issueID, projectID, cycleLink, moduleIDs, now); err != nil {
		handler.internalError(c, err)
		return
	}
	if handler.tasks != nil {
		err := handler.tasks.PublishSoftDeleteRelatedObjects(c.Request.Context(), "db", "draftissue", draftID)
		if err != nil {
			handler.internalError(c, err)
			return
		}
	}

	created, found, err := handler.issueByID(c, slug, issueID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		handler.internalError(c, errors.New("draft to issue: the work item could not be read back"))
		return
	}
	drf.Respond(c, http.StatusCreated, issueCreateSerializerJSON(created, assignees, fields.labelIDs, body))
}

// publishDraftIssueActivities sends the three activities the move produces: one for the work item, one for the cycle it landed in, and one per module.
func (handler *Handler) publishDraftIssueActivities(c *gin.Context, user *auth.User, body map[string]json.RawMessage, row draftIssueRow, issueID, projectID string, cycleLink *CycleIssue, moduleIDs []string, now time.Time) error {
	requested, err := json.Marshal(decodeRawFields(body))
	if err != nil {
		return err
	}
	requestedData := string(requested)
	err = handler.publishIssueActivity(c, issueActivity{
		Type: "issue.activity.created", RequestedData: &requestedData,
		ActorID: user.ID, IssueID: issueID, ProjectID: projectID,
		Notification: true, Origin: handler.origin(), Epoch: now,
	})
	if err != nil {
		return err
	}

	if cycleLink != nil {
		records := []map[string]any{{
			"model": "db.cycleissue",
			"pk":    cycleLink.ID,
			"fields": map[string]any{
				"created_at": cycleLink.CreatedAt, "updated_at": cycleLink.UpdatedAt,
				"created_by": cycleLink.CreatedByID, "updated_by": cycleLink.UpdatedByID,
				"project": cycleLink.ProjectID, "workspace": cycleLink.WorkspaceID,
				"cycle": cycleLink.CycleID, "issue": cycleLink.IssueID,
			},
		}}
		serialized, err := json.Marshal(records)
		if err != nil {
			return err
		}
		snapshot, err := json.Marshal(map[string]any{
			"updated_cycle_issues": nil,
			"created_cycle_issues": string(serialized),
		})
		if err != nil {
			return err
		}
		currentInstance := string(snapshot)
		// The project this activity names is read from a url keyword this route does not have, so Django sends the string "None". The task then fails to find a project by that id, which is why moving a draft into a cycle records no cycle activity today.
		err = handler.publishIssueActivity(c, issueActivity{
			Type: "cycle.activity.created", CurrentInstance: &currentInstance,
			ActorID: user.ID, ProjectID: "None",
			Notification: true, Origin: handler.origin(), Epoch: now,
		})
		if err != nil {
			return err
		}
	}

	for _, moduleID := range moduleIDs {
		payload, err := json.Marshal(map[string]any{"module_id": moduleID})
		if err != nil {
			return err
		}
		moduleData := string(payload)
		err = handler.publishIssueActivity(c, issueActivity{
			Type: "module.activity.created", RequestedData: &moduleData,
			ActorID: user.ID, IssueID: issueID, ProjectID: projectID,
			Notification: true, Origin: handler.origin(), Epoch: now,
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// draftIssueSavePath is DraftIssue.save, which is not the work item's: there is no sequence number to hand out and so no lock, and the sort order is recomputed on creation even when the request asked for one.
//
// The state branch is the part worth reading twice. A draft with no state takes the project's default, and in that case completed_at is left exactly as it was. A draft that has one gets completed_at recomputed from the state's group on every single save — not only when the state changes, which is where this differs from a work item.
func draftIssueSavePath(tx *gorm.DB, values map[string]any, projectID string, adding bool, now time.Time) error {
	stateID, _ := values["state_id"].(string)
	if stateID == "" {
		if projectID != "" {
			resolved, err := issues.DefaultStateID(tx, projectID)
			if err != nil {
				return err
			}
			if resolved != "" {
				values["state_id"] = resolved
				stateID = resolved
			}
		}
	} else {
		group, err := issues.StateGroup(tx, stateID)
		if err != nil {
			return err
		}
		if group == "completed" {
			values["completed_at"] = now
		} else {
			values["completed_at"] = nil
		}
	}

	html, _ := values["description_html"].(string)
	values["description_stripped"] = issues.StripTags(html)
	if !adding {
		return nil
	}

	// A new draft is placed after every draft already sitting in the same state of the same project, and a sort_order the request supplied is overwritten when there is one.
	query := tx.Table("draft_issues").Where("deleted_at IS NULL")
	if projectID == "" {
		query = query.Where("project_id IS NULL")
	} else {
		query = query.Where("project_id = ?", projectID)
	}
	if stateID == "" {
		query = query.Where("state_id IS NULL")
	} else {
		query = query.Where("state_id = ?", stateID)
	}
	var largest []float64
	if err := query.Select("COALESCE(MAX(sort_order), -1)").Scan(&largest).Error; err != nil {
		return err
	}
	if len(largest) > 0 && largest[0] >= 0 {
		values["sort_order"] = largest[0] + 10000
	}
	return nil
}

// writeDraftRelations writes the four related sets. On creation an absent list leaves the set empty; on an edit an absent list leaves the set alone, and a list that was given replaces it outright.
//
// Replacing is a soft delete rather than a real one, because a queryset's delete() on these models is an update of deleted_at. That is what lets the same label be taken off a draft and put back on it: the unique constraint only covers rows whose deleted_at is still empty.
//
// The audit columns on a new row are the draft's own rather than the caller's. On an edit that means the original creator and whoever touched the draft **before** this request, since the serializer reads them off the instance before saving it.
func (handler *Handler) writeDraftRelations(tx *gorm.DB, draftID, workspaceID string, projectID *string, actorID string, now time.Time, fields draftIssueInput, adding bool, creator, editor *string) error {
	createdBy, updatedBy := creator, editor
	if adding {
		createdBy, updatedBy = &actorID, nil
	}
	write := func(table, column string, targets []string, given bool) error {
		if !given {
			return nil
		}
		if !adding {
			err := tx.Table(table).Where("draft_issue_id = ? AND deleted_at IS NULL", draftID).
				Update("deleted_at", now).Error
			if err != nil {
				return err
			}
		}
		if len(targets) == 0 {
			return nil
		}
		rows := make([]map[string]any, 0, len(targets))
		for _, target := range targets {
			rowID, err := newUUID()
			if err != nil {
				return err
			}
			rows = append(rows, map[string]any{
				"id": rowID, "created_at": now, "updated_at": now,
				"created_by_id": createdBy, "updated_by_id": updatedBy,
				"workspace_id": workspaceID, "project_id": projectID,
				"draft_issue_id": draftID, column: target,
			})
		}
		return tx.Table(table).Create(rows).Error
	}

	if err := write("draft_issue_assignees", "assignee_id", fields.assigneeIDs, fields.hasAssignees); err != nil {
		return err
	}
	if err := write("draft_issue_labels", "label_id", fields.labelIDs, fields.hasLabels); err != nil {
		return err
	}
	if err := write("draft_issue_modules", "module_id", fields.moduleIDs, fields.hasModules); err != nil {
		return err
	}
	// The cycle is a single link rather than a list, and on an edit it is only touched when the payload named it — an edit that leaves cycle_id out keeps whatever cycle the draft was in.
	if !fields.hasCycle {
		return nil
	}
	if !adding {
		err := tx.Table("draft_issue_cycles").Where("draft_issue_id = ? AND deleted_at IS NULL", draftID).
			Update("deleted_at", now).Error
		if err != nil {
			return err
		}
	}
	if fields.cycleID == "" {
		return nil
	}
	rowID, err := newUUID()
	if err != nil {
		return err
	}
	// This one link is made one row at a time rather than in bulk, so unlike the other three it goes through the model's save and always takes the caller as its creator.
	return tx.Table("draft_issue_cycles").Create(map[string]any{
		"id": rowID, "created_at": now, "updated_at": now,
		"created_by_id": actorID, "updated_by_id": nil,
		"workspace_id": workspaceID, "project_id": projectID,
		"draft_issue_id": draftID, "cycle_id": fields.cycleID,
	}).Error
}

// draftIssueDeletable is the destroy route's permission, which is the only one of the three that reaches the creator rule it was meant to. Somebody in the workspace who made the draft may delete it; otherwise it takes a workspace admin.
func (handler *Handler) draftIssueDeletable(c *gin.Context, user *auth.User) (bool, error) {
	var memberships int64
	err := handler.db.WithContext(c.Request.Context()).Table("workspace_members wm").
		Joins("JOIN workspaces w ON w.id = wm.workspace_id").
		Where("w.slug = ? AND wm.member_id = ? AND wm.is_active = TRUE AND wm.deleted_at IS NULL",
			c.Param("slug"), user.ID).Count(&memberships).Error
	if err != nil {
		return false, err
	}
	if memberships > 0 {
		var own int64
		err := handler.db.WithContext(c.Request.Context()).Table("draft_issues").
			Where("id = ? AND created_by_id = ? AND deleted_at IS NULL", c.Param("id"), user.ID).
			Count(&own).Error
		if err != nil {
			return false, err
		}
		if own > 0 {
			return true, nil
		}
	}
	return handler.requireWorkspaceRole(c, user, roleAdmin), nil
}

// draftIssueByID reads one of the caller's own drafts.
func (handler *Handler) draftIssueByID(c *gin.Context, userID string) (draftIssueRow, bool, error) {
	var rows []draftIssueRow
	err := handler.db.WithContext(c.Request.Context()).Table("draft_issues d").
		Select(draftIssueAnnotations).
		Joins("JOIN workspaces w ON w.id = d.workspace_id").
		Where("w.slug = ? AND d.id = ? AND d.created_by_id = ? AND d.deleted_at IS NULL",
			c.Param("slug"), c.Param("id"), userID).
		Limit(1).Scan(&rows).Error
	if err != nil || len(rows) == 0 {
		return draftIssueRow{}, false, err
	}
	return rows[0], true, nil
}

// draftIssueRow reads a draft by id alone, which is what the create reads back and what the move reads before it starts.
func (handler *Handler) draftIssueRow(c *gin.Context, draftID string) (draftIssueRow, bool, error) {
	var rows []draftIssueRow
	err := handler.db.WithContext(c.Request.Context()).Table("draft_issues d").
		Select(draftIssueAnnotations).
		Joins("JOIN workspaces w ON w.id = d.workspace_id").
		Where("w.slug = ? AND d.id = ? AND d.deleted_at IS NULL", c.Param("slug"), draftID).
		Limit(1).Scan(&rows).Error
	if err != nil || len(rows) == 0 {
		return draftIssueRow{}, false, err
	}
	return rows[0], true, nil
}

func (handler *Handler) issueByID(c *gin.Context, slug, issueID string) (Issue, bool, error) {
	var rows []Issue
	err := handler.db.WithContext(c.Request.Context()).Table("issues i").Select("i.*").
		Joins("JOIN workspaces w ON w.id = i.workspace_id").
		Where("w.slug = ? AND i.id = ?", slug, issueID).Limit(1).Scan(&rows).Error
	if err != nil || len(rows) == 0 {
		return Issue{}, false, err
	}
	return rows[0], true, nil
}

// draftIssueFilterSQL translates the filters onto a draft's own table, and refuses every lookup that needs a relation.
//
// The refusal is not a shortcut. Each of those lookups reaches through a related name that belongs to Issue — label_issue, issue_assignee, issue_module, issue_cycle — and a DraftIssue has none of them, so Django cannot resolve the keyword and raises FieldError. Since each of them is also the only reason a join is produced, a join here is exactly the signal that the ORM would have refused.
func draftIssueFilterSQL(filters map[string]filterValue) ([]string, []any, bool) {
	joins, conditions, arguments, translatable := issueFilterSQL(filters)
	if !translatable || len(joins) > 0 {
		return nil, nil, false
	}
	rewritten := make([]string, 0, len(conditions))
	for _, condition := range conditions {
		rewritten = append(rewritten, strings.ReplaceAll(condition, "i.", "d."))
	}
	return rewritten, arguments, true
}

type draftIssueInput struct {
	values       map[string]any
	assigneeIDs  []string
	labelIDs     []string
	moduleIDs    []string
	cycleID      string
	hasAssignees bool
	hasLabels    bool
	hasModules   bool
	hasCycle     bool
}

// draftIssueProject reads the project the payload named, which the view takes straight from the request rather than through the serializer.
func (handler *Handler) draftIssueProject(c *gin.Context, body map[string]json.RawMessage) (string, bool) {
	raw, given := body["project_id"]
	if !given {
		return "", true
	}
	return handler.draftIssueProjectValue(c, raw)
}

func (handler *Handler) draftIssueProjectValue(c *gin.Context, raw json.RawMessage) (string, bool) {
	if string(raw) == "null" {
		return "", true
	}
	var value string
	if json.Unmarshal(raw, &value) != nil || value == "" {
		return "", true
	}
	canonical, valid := canonicalUUID(value)
	if !valid {
		// Django hands the raw value to the model, and Postgres refuses to read it as a uuid. That is a 500.
		handler.internalError(c, errors.New("draft issues: project_id is not a uuid"))
		return "", false
	}
	return canonical, true
}

// draftIssueFields is DraftIssueCreateSerializer's validation. It is the work item serializer's without is_draft and with four fields a work item's create does not take: completed_at, external_source, external_id and the work item type.
//
// Every related check runs against the project the payload named, and when it named none the checks run against no project at all — so a draft with no project cannot carry a state, a label, an assignee, a parent or an estimate.
func (handler *Handler) draftIssueFields(c *gin.Context, body map[string]json.RawMessage, projectID string) (draftIssueInput, bool) {
	result := draftIssueInput{values: map[string]any{}}
	ctx := c.Request.Context()

	if raw, exists := body["name"]; exists {
		if string(raw) == "null" {
			result.values["name"] = nil
		} else {
			value, ok := handler.stringField(c, "name", raw, 255)
			if !ok {
				return draftIssueInput{}, false
			}
			result.values["name"] = value
		}
	}
	if raw, exists := body["description_html"]; exists {
		if string(raw) == "null" {
			c.JSON(http.StatusBadRequest, gin.H{"description_html": []string{"This field may not be null."}})
			return draftIssueInput{}, false
		}
		var value string
		if json.Unmarshal(raw, &value) != nil {
			c.JSON(http.StatusBadRequest, gin.H{"description_html": []string{"Not a valid string."}})
			return draftIssueInput{}, false
		}
		if value != "" {
			valid, _, cleaned := htmlsanitizer.ValidateHTMLContent(value)
			if !valid {
				c.JSON(http.StatusBadRequest, gin.H{"error": "html content is not valid"})
				return draftIssueInput{}, false
			}
			if cleaned != nil {
				value = *cleaned
			}
		}
		result.values["description_html"] = value
	}
	if raw, exists := body["description_json"]; exists {
		if string(raw) == "null" {
			c.JSON(http.StatusBadRequest, gin.H{"description_json": []string{"This field may not be null."}})
			return draftIssueInput{}, false
		}
		if !json.Valid(raw) {
			c.JSON(http.StatusBadRequest, gin.H{"description_json": []string{"Value must be valid JSON."}})
			return draftIssueInput{}, false
		}
		result.values["description_json"] = auth.JSONValue(append([]byte(nil), raw...))
	}
	if raw, exists := body["priority"]; exists {
		var value string
		if json.Unmarshal(raw, &value) != nil || !validIssuePriority(value) {
			var decoded any
			_ = json.Unmarshal(raw, &decoded)
			c.JSON(http.StatusBadRequest, gin.H{"priority": []string{invalidChoice(decoded)}})
			return draftIssueInput{}, false
		}
		result.values["priority"] = value
	}
	startDate, hasStart, ok := handler.issueDateField(c, body, "start_date")
	if !ok {
		return draftIssueInput{}, false
	}
	targetDate, hasTarget, ok := handler.issueDateField(c, body, "target_date")
	if !ok {
		return draftIssueInput{}, false
	}
	if hasStart {
		result.values["start_date"] = startDate
	}
	if hasTarget {
		result.values["target_date"] = targetDate
	}
	if startDate != nil && targetDate != nil && startDate.After(*targetDate) {
		c.JSON(http.StatusBadRequest, gin.H{"non_field_errors": []string{"Start date cannot exceed target date"}})
		return draftIssueInput{}, false
	}
	if raw, exists := body["sort_order"]; exists {
		var value float64
		if json.Unmarshal(raw, &value) != nil {
			c.JSON(http.StatusBadRequest, gin.H{"sort_order": []string{"A valid number is required."}})
			return draftIssueInput{}, false
		}
		result.values["sort_order"] = value
	}
	for _, field := range []string{"external_source", "external_id"} {
		raw, exists := body[field]
		if !exists {
			continue
		}
		if string(raw) == "null" {
			result.values[field] = nil
			continue
		}
		value, ok := handler.stringField(c, field, raw, 255)
		if !ok {
			return draftIssueInput{}, false
		}
		result.values[field] = value
	}
	for _, field := range []string{"completed_at", "deleted_at"} {
		raw, exists := body[field]
		if !exists {
			continue
		}
		if string(raw) == "null" {
			result.values[field] = nil
			continue
		}
		var value string
		if json.Unmarshal(raw, &value) != nil {
			c.JSON(http.StatusBadRequest, gin.H{field: []string{"Datetime has wrong format. Use one of these formats instead: YYYY-MM-DDThh:mm[:ss[.uuuuuu]][+HH:MM|-HH:MM|Z]."}})
			return draftIssueInput{}, false
		}
		parsed, valid := parseDjangoDateTime(value)
		if !valid {
			c.JSON(http.StatusBadRequest, gin.H{field: []string{"Datetime has wrong format. Use one of these formats instead: YYYY-MM-DDThh:mm[:ss[.uuuuuu]][+HH:MM|-HH:MM|Z]."}})
			return draftIssueInput{}, false
		}
		result.values[field] = parsed
	}

	// state_id and state both write the state, and parent_id and parent both write the parent; whichever comes last in the payload is the one that lands, which for Django is always the unsuffixed name.
	for _, relation := range []struct {
		fields []string
		column string
		table  string
		reason string
	}{
		{fields: []string{"state_id", "state"}, column: "state_id", table: "states",
			reason: "State is not valid please pass a valid state_id"},
		{fields: []string{"parent_id", "parent"}, column: "parent_id", table: "issues",
			reason: "Parent is not valid issue_id please pass a valid issue_id"},
		{fields: []string{"estimate_point"}, column: "estimate_point_id", table: "estimate_points",
			reason: "Estimate point is not valid please pass a valid estimate_point_id"},
		{fields: []string{"type"}, column: "type_id", table: "issue_types"},
	} {
		for _, field := range relation.fields {
			raw, exists := body[field]
			if !exists {
				continue
			}
			if blankRelation(raw) {
				result.values[relation.column] = nil
				continue
			}
			var value string
			if json.Unmarshal(raw, &value) != nil {
				c.JSON(http.StatusBadRequest, gin.H{field: []string{`“` + value + `” is not a valid UUID.`}})
				return draftIssueInput{}, false
			}
			canonical, valid := canonicalUUID(value)
			if !valid {
				c.JSON(http.StatusBadRequest, gin.H{field: []string{`“` + value + `” is not a valid UUID.`}})
				return draftIssueInput{}, false
			}
			// The work item type is the one relation the serializer does not check against the project, because validate() never looks at it.
			if relation.reason != "" {
				belongs, err := handler.rowInOptionalProject(ctx, relation.table, canonical, projectID)
				if err != nil {
					handler.internalError(c, err)
					return draftIssueInput{}, false
				}
				if !belongs {
					c.JSON(http.StatusBadRequest, gin.H{"non_field_errors": []string{relation.reason}})
					return draftIssueInput{}, false
				}
			}
			result.values[relation.column] = canonical
		}
	}

	if raw, exists := body["assignee_ids"]; exists {
		result.hasAssignees = true
		requested, ok := handler.uuidListField(c, "assignee_ids", raw)
		if !ok {
			return draftIssueInput{}, false
		}
		result.assigneeIDs = []string{}
		if projectID != "" {
			allowed, err := handler.projectMembersAtLeast(ctx, projectID, requested, roleMemberOrAbove)
			if err != nil {
				handler.internalError(c, err)
				return draftIssueInput{}, false
			}
			result.assigneeIDs = allowed
		}
	}
	if raw, exists := body["label_ids"]; exists {
		result.hasLabels = true
		requested, ok := handler.uuidListField(c, "label_ids", raw)
		if !ok {
			return draftIssueInput{}, false
		}
		result.labelIDs = []string{}
		if projectID != "" {
			allowed, err := handler.labelsInProject(ctx, projectID, requested)
			if err != nil {
				handler.internalError(c, err)
				return draftIssueInput{}, false
			}
			result.labelIDs = allowed
		}
	}
	// module_ids and cycle_id never reach the serializer at all: the view reads them off the raw payload, so nothing narrows them to the project and nothing checks they exist.
	if raw, exists := body["module_ids"]; exists && string(raw) != "null" {
		requested, ok := handler.uuidListField(c, "module_ids", raw)
		if !ok {
			return draftIssueInput{}, false
		}
		result.hasModules = true
		result.moduleIDs = requested
	}
	if raw, exists := body["cycle_id"]; exists {
		result.hasCycle = true
		if string(raw) != "null" {
			var value string
			if json.Unmarshal(raw, &value) != nil {
				handler.internalError(c, errors.New("draft issues: cycle_id is not a uuid"))
				return draftIssueInput{}, false
			}
			canonical, valid := canonicalUUID(value)
			if !valid {
				handler.internalError(c, errors.New("draft issues: cycle_id is not a uuid"))
				return draftIssueInput{}, false
			}
			result.cycleID = canonical
		}
	}
	return result, true
}

// draftIssueCycleTarget reads the cycle the move should put the new work item in, which the view takes from the raw payload.
func (handler *Handler) draftIssueCycleTarget(c *gin.Context, body map[string]json.RawMessage) (string, bool) {
	raw, given := body["cycle_id"]
	if !given || string(raw) == "null" {
		return "", true
	}
	var value string
	if json.Unmarshal(raw, &value) != nil || value == "" {
		return "", true
	}
	canonical, valid := canonicalUUID(value)
	if !valid {
		handler.internalError(c, errors.New("draft to issue: cycle_id is not a uuid"))
		return "", false
	}
	return canonical, true
}

func (handler *Handler) draftIssueModuleTargets(c *gin.Context, body map[string]json.RawMessage) ([]string, bool) {
	raw, given := body["module_ids"]
	if !given || string(raw) == "null" {
		return nil, true
	}
	var values []string
	if json.Unmarshal(raw, &values) != nil {
		handler.internalError(c, errors.New("draft to issue: module_ids is not a list"))
		return nil, false
	}
	targets := make([]string, 0, len(values))
	for _, value := range values {
		canonical, valid := canonicalUUID(value)
		if !valid {
			handler.internalError(c, errors.New("draft to issue: a module id is not a uuid"))
			return nil, false
		}
		targets = append(targets, canonical)
	}
	return targets, true
}

// rowInOptionalProject is the related check with a project that may not be there. A draft with no project matches nothing, since every one of these tables requires a project of its own.
func (handler *Handler) rowInOptionalProject(ctx context.Context, table, id, projectID string) (bool, error) {
	if projectID == "" {
		return false, nil
	}
	return handler.rowInProject(ctx, table, id, projectID)
}

// draftIssueJSON is DraftIssueSerializer, which the detail serializer repeats field for field. The create route answers with the same twenty-one keys through a values() projection rather than a serializer, so one shape covers all three.
func draftIssueJSON(row draftIssueRow) gin.H {
	return gin.H{
		"id": row.ID, "name": row.Name, "state_id": row.StateID, "sort_order": row.SortOrder,
		"completed_at": row.CompletedAt, "estimate_point": row.EstimatePointID,
		"priority": row.Priority, "start_date": dateOnly(row.StartDate), "target_date": dateOnly(row.TargetDate),
		"project_id": row.ProjectID, "parent_id": row.ParentID, "cycle_id": row.CycleID,
		"module_ids": stringsOrEmpty(row.ModuleIDs), "label_ids": stringsOrEmpty(row.LabelIDs),
		"assignee_ids": stringsOrEmpty(row.AssigneeIDs),
		"created_at":   row.CreatedAt, "updated_at": row.UpdatedAt,
		"created_by": row.CreatedByID, "updated_by": row.UpdatedByID,
		"type_id": row.TypeID, "description_html": row.DescriptionHTML,
	}
}

// issueCreateSerializerJSON is IssueCreateSerializer's own representation, which the move answers with rather than the annotated projection the ordinary create uses.
//
// Its two id lists are echoed from the request rather than read from the database, so a label the serializer dropped for not belonging to the project still appears in the response.
func issueCreateSerializerJSON(issue Issue, assignees, labels []string, body map[string]json.RawMessage) gin.H {
	data := gin.H{
		"id": issue.ID, "state_id": issue.StateID, "parent_id": issue.ParentID,
		"project_id": issue.ProjectID, "workspace_id": issue.WorkspaceID,
		"created_at": issue.CreatedAt, "updated_at": issue.UpdatedAt, "deleted_at": issue.DeletedAt,
		"point": issue.Point, "name": issue.Name,
		"description_json": decodeJSON(issue.DescriptionJSON),
		"description_html": issue.DescriptionHTML, "description_stripped": issue.DescriptionStripped,
		"description_binary": nil,
		"priority":           issue.Priority,
		"start_date":         dateOnly(issue.StartDate), "target_date": dateOnly(issue.TargetDate),
		"sequence_id": issue.SequenceID, "sort_order": issue.SortOrder,
		"completed_at": issue.CompletedAt, "archived_at": dateOnly(issue.ArchivedAt),
		"is_draft":        issue.IsDraft,
		"external_source": issue.ExternalSource, "external_id": issue.ExternalID,
		"created_by": issue.CreatedByID, "updated_by": issue.UpdatedByID,
		"project": issue.ProjectID, "workspace": issue.WorkspaceID,
		"parent": issue.ParentID, "state": issue.StateID,
		"estimate_point": issue.EstimatePointID, "type": issue.TypeID,
		"assignees": stringsOrEmptySlice(assignees), "labels": stringsOrEmptySlice(labels),
	}
	// to_representation replaces both id lists with what the request sent, and an empty or missing list becomes an empty array rather than null.
	data["assignee_ids"] = echoedIDList(body["assignee_ids"])
	data["label_ids"] = echoedIDList(body["label_ids"])
	return data
}

func echoedIDList(raw json.RawMessage) []string {
	var values []string
	if len(raw) == 0 || json.Unmarshal(raw, &values) != nil || len(values) == 0 {
		return []string{}
	}
	return values
}
