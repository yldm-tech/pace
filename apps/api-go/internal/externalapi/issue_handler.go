package externalapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"github.com/yldm-tech/pace/apps/api-go/internal/drf"
	"github.com/yldm-tech/pace/apps/api-go/internal/pagination"
	"gorm.io/gorm"
)

func (handler *Handler) registerIssueRoutes(router gin.IRouter) {
	base := "/api/v1/workspaces/:slug/projects/:project/"
	// Both spellings carry the same two routes here, unlike the attachments, where each name has a path of its own.
	for _, name := range []string{"issues/", "work-items/"} {
		router.GET(base+name, handler.authenticated(handler.issueList))
		router.POST(base+name, handler.authenticated(handler.issueCreate))
		router.GET(base+name+":issue/", handler.authenticated(handler.issueRetrieve))
		router.PATCH(base+name+":issue/", handler.authenticated(handler.issueUpdate))
		router.DELETE(base+name+":issue/", handler.authenticated(handler.issueDestroy))
	}
}

// issueList returns a page of a project's work items.
//
// The view annotates a cycle id, a link count, an attachment count and a sub-issue count onto every row and then renders them through a serializer that declares none of them, so all four are computed and thrown away. They are not read here either, because a response that never carried them cannot start carrying them now.
func (handler *Handler) issueList(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireProjectMember(c, user, c.Request.Method) {
		return
	}
	// Community builds have no query language, and the two parameters are refused by name rather than ignored.
	unsupported := []string{}
	for _, parameter := range []string{"pql", "filters"} {
		if c.Query(parameter) != "" {
			unsupported = append(unsupported, parameter)
		}
	}
	if len(unsupported) > 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"pql": "PQL and structured filters are not supported on this Plane edition. " +
				"Remove the pql/filters parameter and filter results client-side, or use " +
				"a Plane edition that supports work item query filtering.",
			"unsupported_parameters": unsupported,
		})
		return
	}

	if c.Query("external_id") != "" && c.Query("external_source") != "" {
		handler.issueByExternalID(c)
		return
	}

	order, err := externalIssueOrderClause(c.Query("order_by"))
	if err != nil {
		handler.serverError(c, err)
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

	// The total is counted over a queryset of its own, which carries no ordering and none of the annotations.
	var total int64
	if err := handler.issueScope(c).Count(&total).Error; err != nil {
		handler.serverError(c, err)
		return
	}
	page := pagination.PlanOffsetPage(perPage, cursor, int(total), 0, pagination.DefaultPerPage)

	var rows []externalIssueRow
	// One row past the page is read, which is how the paginator knows whether another page follows.
	err = handler.issueScope(c).Select(externalIssueSelection()).
		Order(order).Offset(page.Offset).Limit(page.Limit + 1).Scan(&rows).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	page = pagination.PlanOffsetPage(perPage, cursor, int(total), len(rows), pagination.DefaultPerPage)
	if len(rows) > page.Limit {
		rows = rows[:page.Limit]
	}

	bodies, err := handler.issueBodies(c, rows)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, page.Envelope(bodies, len(bodies), nil, nil, nil))
}

// issueByExternalID is the shortcut the list takes when both external parameters are given: one work item, unpaginated.
//
// It reads through the plain manager rather than the issue manager the list uses, so this is the one way to reach an archived, draft or triage work item through the list route. Two work items carrying the same pair answer 500 rather than picking one, which is what Django's .get does with more than one row.
func (handler *Handler) issueByExternalID(c *gin.Context) {
	var rows []externalIssueRow
	err := handler.db.WithContext(c.Request.Context()).Table("issues i").
		Select(externalIssueSelection()).
		Joins("JOIN workspaces w ON w.id = i.workspace_id").
		Where("w.slug = ? AND i.project_id = ? AND i.external_id = ? AND i.external_source = ? AND i.deleted_at IS NULL",
			c.Param("slug"), c.Param("project"), c.Query("external_id"), c.Query("external_source")).
		Limit(2).Scan(&rows).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if len(rows) == 0 {
		notFound(c)
		return
	}
	if len(rows) > 1 {
		handler.serverError(c, errMoreThanOneExternalIssue)
		return
	}
	bodies, err := handler.issueBodies(c, rows[:1])
	if err != nil {
		handler.serverError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, bodies[0])
}

var errMoreThanOneExternalIssue = &orderError{"more than one work item carries that external id and source"}

// issueRetrieve returns one work item, read through the issue manager — so an archived or draft one is a 404 here even though it exists.
func (handler *Handler) issueRetrieve(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireProjectMember(c, user, c.Request.Method) {
		return
	}
	var rows []externalIssueRow
	err := handler.issueScope(c).Select(externalIssueSelection()).
		Where("i.id = ?", c.Param("issue")).Limit(1).Scan(&rows).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if len(rows) == 0 {
		notFound(c)
		return
	}
	bodies, err := handler.issueBodies(c, rows)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, bodies[0])
}

// issueScope is Issue.issue_objects over one project.
//
// The manager hides more than soft-deleted rows: a triage work item, an archived one, a draft one and anything in an archived project are all out. A work item with no state at all passes the triage check, because Django's exclude over the nullable join renders as NOT (group = 'triage' AND group IS NOT NULL).
func (handler *Handler) issueScope(c *gin.Context) *gorm.DB {
	return handler.db.WithContext(c.Request.Context()).Table("issues i").
		Joins("LEFT JOIN states s ON s.id = i.state_id").
		Joins("JOIN projects p ON p.id = i.project_id").
		Joins("JOIN workspaces w ON w.id = i.workspace_id").
		Where("w.slug = ? AND i.project_id = ?", c.Param("slug"), c.Param("project")).
		Where(`i.deleted_at IS NULL AND s.group IS DISTINCT FROM 'triage'
			AND i.archived_at IS NULL AND p.archived_at IS NULL AND i.is_draft = FALSE`)
}

// issueBodies renders the rows, narrowed by fields and widened by expand.
func (handler *Handler) issueBodies(c *gin.Context, rows []externalIssueRow) ([]gin.H, error) {
	fields := requestedFields(c)
	bodies := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		bodies = append(bodies, externalIssueJSON(row))
	}
	if err := handler.expandIssues(c, rows, bodies, expandedFields(c)); err != nil {
		return nil, err
	}
	for index, body := range bodies {
		bodies[index] = narrow(body, fields)
	}
	return bodies, nil
}

// expandedFields is the expand parameter, which replaces an id with the object it names. It is read exactly as fields is: split on commas with the empty pieces dropped.
func expandedFields(c *gin.Context) []string {
	names := []string{}
	for _, name := range strings.Split(c.Query("expand"), ",") {
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}

// expandIssues replaces each expanded id with the record it points at.
//
// A relation that is null expands to an empty object rather than to null, because DRF hands the serializer None and a serializer with no instance renders the empty mapping. A name the expansion table does not know falls back to the instance's own `<name>_id`, which for everything but the work item type does not exist and lands as null.
func (handler *Handler) expandIssues(c *gin.Context, rows []externalIssueRow, bodies []gin.H, expand []string) error {
	if len(expand) == 0 || len(rows) == 0 {
		return nil
	}
	for _, name := range expand {
		if !externalIssueFields[name] {
			// Only a field the serializer declares can be expanded; anything else is ignored.
			continue
		}
		var err error
		switch name {
		case "state":
			err = handler.expandState(c, rows, bodies)
		case "project":
			err = handler.expandProject(c, rows, bodies)
		case "workspace":
			err = handler.expandWorkspace(c, rows, bodies)
		case "created_by", "updated_by":
			err = handler.expandActor(c, rows, bodies, name)
		case "parent":
			err = handler.expandParent(c, rows, bodies)
		case "estimate_point":
			err = handler.expandEstimatePoint(c, rows, bodies)
		case "assignees":
			err = handler.expandAssignees(c, rows, bodies)
		case "labels":
			err = handler.expandLabels(c, rows, bodies)
		case "type":
			// The type has no serializer of its own in the expansion table, so it falls back to the id it already carries.
			continue
		default:
			for _, body := range bodies {
				body[name] = nil
			}
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (handler *Handler) expandState(c *gin.Context, rows []externalIssueRow, bodies []gin.H) error {
	identifiers := collectIDs(rows, func(row externalIssueRow) *string { return row.StateID })
	records := map[string]gin.H{}
	if len(identifiers) > 0 {
		var states []State
		err := handler.db.WithContext(c.Request.Context()).Table("states").Where("id IN ?", identifiers).Scan(&states).Error
		if err != nil {
			return err
		}
		for _, state := range states {
			// StateLiteSerializer is four fields, not the whole state.
			records[state.ID] = gin.H{"id": state.ID, "name": state.Name, "color": state.Color, "group": state.Group}
		}
	}
	fill(rows, bodies, "state", func(row externalIssueRow) any { return lookup(records, row.StateID) })
	return nil
}

func (handler *Handler) expandProject(c *gin.Context, rows []externalIssueRow, bodies []gin.H) error {
	var projects []Project
	err := handler.db.WithContext(c.Request.Context()).Table("projects").
		Where("id = ?", c.Param("project")).Scan(&projects).Error
	if err != nil {
		return err
	}
	records := map[string]gin.H{}
	for _, project := range projects {
		records[project.ID] = projectLiteJSON(project)
	}
	fill(rows, bodies, "project", func(row externalIssueRow) any { return lookup(records, &row.ProjectID) })
	return nil
}

func (handler *Handler) expandWorkspace(c *gin.Context, rows []externalIssueRow, bodies []gin.H) error {
	var workspaces []struct {
		ID   string `gorm:"column:id"`
		Name string `gorm:"column:name"`
		Slug string `gorm:"column:slug"`
	}
	err := handler.db.WithContext(c.Request.Context()).Table("workspaces").
		Where("slug = ?", c.Param("slug")).Scan(&workspaces).Error
	if err != nil {
		return err
	}
	records := map[string]gin.H{}
	for _, workspace := range workspaces {
		// WorkspaceLiteSerializer is a name, a slug and an id.
		records[workspace.ID] = gin.H{"name": workspace.Name, "slug": workspace.Slug, "id": workspace.ID}
	}
	fill(rows, bodies, "workspace", func(row externalIssueRow) any { return lookup(records, &row.WorkspaceID) })
	return nil
}

func (handler *Handler) expandActor(c *gin.Context, rows []externalIssueRow, bodies []gin.H, name string) error {
	pick := func(row externalIssueRow) *string { return row.CreatedByID }
	if name == "updated_by" {
		pick = func(row externalIssueRow) *string { return row.UpdatedByID }
	}
	records, err := handler.userLiteRecords(c, collectIDs(rows, pick))
	if err != nil {
		return err
	}
	fill(rows, bodies, name, func(row externalIssueRow) any { return lookup(records, pick(row)) })
	return nil
}

func (handler *Handler) expandParent(c *gin.Context, rows []externalIssueRow, bodies []gin.H) error {
	identifiers := collectIDs(rows, func(row externalIssueRow) *string { return row.ParentID })
	records := map[string]gin.H{}
	if len(identifiers) > 0 {
		var parents []struct {
			ID         string `gorm:"column:id"`
			SequenceID int64  `gorm:"column:sequence_id"`
			ProjectID  string `gorm:"column:project_id"`
		}
		err := handler.db.WithContext(c.Request.Context()).Table("issues").Where("id IN ?", identifiers).Scan(&parents).Error
		if err != nil {
			return err
		}
		for _, parent := range parents {
			// IssueLiteSerializer is three fields, and the project comes back under project_id rather than project.
			records[parent.ID] = gin.H{"id": parent.ID, "sequence_id": parent.SequenceID, "project_id": parent.ProjectID}
		}
	}
	fill(rows, bodies, "parent", func(row externalIssueRow) any { return lookup(records, row.ParentID) })
	return nil
}

func (handler *Handler) expandEstimatePoint(c *gin.Context, rows []externalIssueRow, bodies []gin.H) error {
	identifiers := collectIDs(rows, func(row externalIssueRow) *string { return row.EstimatePointID })
	records := map[string]gin.H{}
	if len(identifiers) > 0 {
		var points []EstimatePoint
		err := handler.db.WithContext(c.Request.Context()).Table("estimate_points").Where("id IN ?", identifiers).Scan(&points).Error
		if err != nil {
			return err
		}
		for _, point := range points {
			records[point.ID] = estimatePointJSON(point)
		}
	}
	fill(rows, bodies, "estimate_point", func(row externalIssueRow) any { return lookup(records, row.EstimatePointID) })
	return nil
}

func (handler *Handler) expandAssignees(c *gin.Context, rows []externalIssueRow, bodies []gin.H) error {
	identifiers := []string{}
	for _, row := range rows {
		identifiers = append(identifiers, row.AssigneeIDs...)
	}
	records, err := handler.userLiteRecords(c, identifiers)
	if err != nil {
		return err
	}
	for index, row := range rows {
		// The list is read back through the id column rather than the prefetch, so an assignee whose user row is gone drops out.
		people := []gin.H{}
		for _, identifier := range row.AssigneeIDs {
			if record, present := records[identifier]; present {
				people = append(people, record)
			}
		}
		bodies[index]["assignees"] = people
	}
	return nil
}

func (handler *Handler) expandLabels(c *gin.Context, rows []externalIssueRow, bodies []gin.H) error {
	identifiers := []string{}
	for _, row := range rows {
		identifiers = append(identifiers, row.LabelIDs...)
	}
	records := map[string]gin.H{}
	if len(identifiers) > 0 {
		var labels []Label
		err := handler.db.WithContext(c.Request.Context()).Table("labels").Where("id IN ?", identifiers).Scan(&labels).Error
		if err != nil {
			return err
		}
		for _, label := range labels {
			records[label.ID] = labelJSON(label)
		}
	}
	for index, row := range rows {
		applied := []gin.H{}
		for _, identifier := range row.LabelIDs {
			if record, present := records[identifier]; present {
				applied = append(applied, record)
			}
		}
		bodies[index]["labels"] = applied
	}
	return nil
}

// userLiteRecords reads the people an expansion names, rendered through UserLiteSerializer.
func (handler *Handler) userLiteRecords(c *gin.Context, identifiers []string) (map[string]gin.H, error) {
	records := map[string]gin.H{}
	if len(identifiers) == 0 {
		return records, nil
	}
	var people []struct {
		ID          string  `gorm:"column:id"`
		FirstName   string  `gorm:"column:first_name"`
		LastName    string  `gorm:"column:last_name"`
		Email       string  `gorm:"column:email"`
		Avatar      string  `gorm:"column:avatar"`
		AvatarAsset *string `gorm:"column:avatar_asset_id"`
		DisplayName string  `gorm:"column:display_name"`
	}
	err := handler.db.WithContext(c.Request.Context()).Table("users").Where("id IN ?", identifiers).Scan(&people).Error
	if err != nil {
		return nil, err
	}
	for _, person := range people {
		var avatarURL any
		switch {
		case person.AvatarAsset != nil:
			avatarURL = "/api/assets/v2/static/" + *person.AvatarAsset + "/"
		case person.Avatar != "":
			avatarURL = person.Avatar
		}
		records[person.ID] = gin.H{
			"id": person.ID, "first_name": person.FirstName, "last_name": person.LastName,
			"email": person.Email, "avatar": person.Avatar, "avatar_url": avatarURL,
			"display_name": person.DisplayName,
		}
	}
	return records, nil
}

// EstimatePoint is one point of an estimate, which the work item's estimate_point expands to.
type EstimatePoint struct {
	ID          string     `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt   time.Time  `gorm:"column:created_at"`
	UpdatedAt   time.Time  `gorm:"column:updated_at"`
	CreatedByID *string    `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID *string    `gorm:"column:updated_by_id;type:uuid"`
	DeletedAt   *time.Time `gorm:"column:deleted_at"`
	ProjectID   *string    `gorm:"column:project_id;type:uuid"`
	WorkspaceID string     `gorm:"column:workspace_id;type:uuid"`
	EstimateID  string     `gorm:"column:estimate_id;type:uuid"`
	Key         int        `gorm:"column:key"`
	Description string     `gorm:"column:description"`
	Value       string     `gorm:"column:value"`
}

func (EstimatePoint) TableName() string { return "estimate_points" }

// estimatePointJSON is EstimatePointSerializer: twelve fields, with the value a string rather than a number.
func estimatePointJSON(point EstimatePoint) gin.H {
	return gin.H{
		"id": point.ID, "created_at": point.CreatedAt, "updated_at": point.UpdatedAt,
		"deleted_at": point.DeletedAt, "key": point.Key, "description": point.Description,
		"value": point.Value, "created_by": point.CreatedByID, "updated_by": point.UpdatedByID,
		"project": point.ProjectID, "workspace": point.WorkspaceID, "estimate": point.EstimateID,
	}
}

// externalIssueFields is what the serializer declares, which is the set expand is checked against.
var externalIssueFields = map[string]bool{
	"id": true, "assignees": true, "labels": true, "type_id": true,
	"created_at": true, "updated_at": true, "deleted_at": true, "point": true, "name": true,
	"description_html": true, "description_binary": true, "priority": true,
	"start_date": true, "target_date": true, "sequence_id": true, "sort_order": true,
	"completed_at": true, "archived_at": true, "is_draft": true,
	"external_source": true, "external_id": true, "created_by": true, "updated_by": true,
	"project": true, "workspace": true, "parent": true, "state": true,
	"estimate_point": true, "type": true,
}

// collectIDs gathers the non-null ids one relation points at.
func collectIDs(rows []externalIssueRow, pick func(externalIssueRow) *string) []string {
	identifiers := []string{}
	for _, row := range rows {
		if value := pick(row); value != nil && *value != "" {
			identifiers = append(identifiers, *value)
		}
	}
	return identifiers
}

// lookup answers with the record an id names, and with an empty object when there is no id — which is what a serializer handed None renders.
func lookup(records map[string]gin.H, identifier *string) any {
	if identifier == nil {
		return gin.H{}
	}
	if record, present := records[*identifier]; present {
		return record
	}
	return gin.H{}
}

func fill(rows []externalIssueRow, bodies []gin.H, name string, value func(externalIssueRow) any) {
	for index, row := range rows {
		bodies[index][name] = value(row)
	}
}
