package project

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"github.com/yldm-tech/pace/apps/api-go/internal/drf"
	"github.com/yldm-tech/pace/apps/api-go/internal/pagination"
	"gorm.io/gorm"
)

func (handler *Handler) registerIssueListRoutes(router gin.IRouter) {
	router.GET("/api/workspaces/:slug/projects/:id/issues/", handler.authenticated(handler.issueList))
}

// issueListRow is the values() projection issue_on_results returns for the ungrouped list.
type issueListRow struct {
	Issue
	CycleID         *string        `gorm:"column:cycle_id"`
	SubIssuesCount  *int64         `gorm:"column:sub_issues_count"`
	AttachmentCount *int64         `gorm:"column:attachment_count"`
	LinkCount       *int64         `gorm:"column:link_count"`
	StateGroup      *string        `gorm:"column:state_group"`
	LabelIDs        pq.StringArray `gorm:"column:label_ids;type:uuid[]"`
	AssigneeIDs     pq.StringArray `gorm:"column:assignee_ids;type:uuid[]"`
	ModuleIDs       pq.StringArray `gorm:"column:module_ids;type:uuid[]"`
}

// issueListRequest is everything both halves of the route need once the query string has been read.
type issueListRequest struct {
	slug       string
	projectID  string
	perPage    int
	cursor     pagination.OffsetCursor
	joins      []string
	conditions []string
	arguments  []any
	total      int
	// basePredicate is the manager the route reads through. The live list uses issue_objects; the archive uses the plain soft-delete manager plus its own predicates, which is the only way to reach a row issue_objects hides.
	basePredicate string
	baseArguments []any
}

// issueListScope is the filtered set both halves share: the issue_objects manager, the workspace and project, and whatever the filters added.
func (handler *Handler) issueListScope(ctx context.Context, request issueListRequest) *gorm.DB {
	predicate := request.basePredicate
	if predicate == "" {
		predicate = issueObjectsPredicate("i")
	}
	query := handler.db.WithContext(ctx).Table("issues i").
		Joins("JOIN workspaces w ON w.id = i.workspace_id").
		Where("w.slug = ? AND i.project_id = ?", request.slug, request.projectID).
		Where(predicate, request.baseArguments...)
	for _, join := range request.joins {
		query = query.Joins(join)
	}
	consumed := 0
	for _, condition := range request.conditions {
		count := countPlaceholders(condition)
		query = query.Where(condition, request.arguments[consumed:consumed+count]...)
		consumed += count
	}
	return query
}

// issueList reads the query string, then hands off to whichever half the request asked for.
func (handler *Handler) issueList(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	slug, projectID := c.Param("slug"), c.Param("id")

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

	// The project is read before anything else, and an unguarded .get means a missing one is a 404 rather than an empty list.
	var project Project
	err = handler.db.WithContext(c.Request.Context()).Table("projects p").
		Joins("JOIN workspaces w ON w.id = p.workspace_id").
		Where("p.id = ? AND w.slug = ?", projectID, slug).Take(&project).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "The required object does not exist."})
		return
	}
	if err != nil {
		handler.internalError(c, err)
		return
	}

	filters := issueFilters(queryParams(c), "GET", "", handler.clock().UTC())
	joins, conditions, arguments, translatable := issueFilterSQL(filters)
	if !translatable {
		// A lookup the ORM cannot resolve either, which Django answers 500 for.
		handler.internalError(c, errors.New("issue list: a filter names a field the schema does not have"))
		return
	}
	if raw := c.Query("updated_at__gt"); raw != "" {
		since, ok := parseDjangoDateTime(raw)
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Please provide valid detail"})
			return
		}
		conditions = append(conditions, "i.updated_at > ?")
		arguments = append(arguments, since)
	}

	// A guest sees only what they raised, unless the project opens all features to guests.
	member, found, err := handler.activeProjectMember(c.Request.Context(), slug, projectID, user.ID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if found && member.Role == roleGuest && !project.GuestViewAllFeatures {
		conditions = append(conditions, "i.created_by_id = ?")
		arguments = append(arguments, user.ID)
	}

	request := issueListRequest{
		slug: slug, projectID: projectID, perPage: perPage, cursor: cursor,
		joins: joins, conditions: conditions, arguments: arguments,
	}
	// The filtered set is counted before the annotations are applied, and over distinct issues, since a filter's join can multiply rows.
	var total int64
	if err := handler.issueListScope(c.Request.Context(), request).Distinct("i.id").Count(&total).Error; err != nil {
		handler.internalError(c, err)
		return
	}
	request.total = int(total)

	if handler.tasks != nil {
		// The list records a visit to the project, not to any issue.
		err := handler.tasks.PublishRecentVisit(c.Request.Context(), "project", projectID, user.ID, projectID, slug)
		if err != nil {
			handler.internalError(c, err)
			return
		}
	}

	if c.Query("group_by") != "" {
		handler.issueListGrouped(c, user, request)
		return
	}

	rows, err := handler.issueListPage(c, request)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	page := pagination.PlanOffsetPage(perPage, cursor, request.total, len(rows), pagination.DefaultPerPage)
	if len(rows) > page.Limit {
		rows = rows[:page.Limit]
	}
	results := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		results = append(results, issueListRowJSON(row))
	}
	drf.Respond(c, http.StatusOK, page.Envelope(results, len(results), nil, nil, nil))
}

// issueListPage reads the rows the cursor asked for, plus the one extra the paginator uses to decide whether a page follows.
func (handler *Handler) issueListPage(c *gin.Context, request issueListRequest) ([]issueListRow, error) {
	page := pagination.PlanOffsetPage(request.perPage, request.cursor, request.total, 0, pagination.DefaultPerPage)
	var rows []issueListRow
	err := handler.issueListScope(c.Request.Context(), request).
		Select(issueListAnnotations()).
		Group("i.id").
		Order(issueOrderClause(c.Query("order_by"))).
		Offset(page.Offset).Limit(page.Stop - page.Offset).
		Scan(&rows).Error
	return rows, err
}

// issueListAnnotations is apply_annotations plus the id arrays issue_queryset_grouper adds. The counts do not coalesce here, which is why the serializer turns a null into zero rather than the query doing it.
func issueListAnnotations() string {
	return `i.*,
		(SELECT ci.cycle_id FROM cycle_issues ci WHERE ci.issue_id = i.id AND ci.deleted_at IS NULL LIMIT 1) AS cycle_id,
		(SELECT COUNT(*) FROM issue_links il WHERE il.issue_id = i.id AND il.deleted_at IS NULL) AS link_count,
		(SELECT COUNT(*) FROM file_assets fa WHERE fa.issue_id = i.id AND fa.entity_type = 'ISSUE_ATTACHMENT' AND fa.deleted_at IS NULL) AS attachment_count,
		(SELECT COUNT(*) FROM issues sub WHERE sub.parent_id = i.id AND ` + issueObjectsPredicate("sub") + `) AS sub_issues_count,
		(SELECT s.group FROM states s WHERE s.id = i.state_id) AS state_group,
		COALESCE((SELECT ARRAY_AGG(DISTINCT il2.label_id) FROM issue_labels il2 WHERE il2.issue_id = i.id AND il2.deleted_at IS NULL), '{}') AS label_ids,
		COALESCE((SELECT ARRAY_AGG(DISTINCT ia.assignee_id) FROM issue_assignees ia WHERE ia.issue_id = i.id AND ia.deleted_at IS NULL), '{}') AS assignee_ids,
		COALESCE((SELECT ARRAY_AGG(DISTINCT mi.module_id) FROM module_issues mi
			JOIN modules m ON m.id = mi.module_id AND m.archived_at IS NULL
			WHERE mi.issue_id = i.id AND mi.deleted_at IS NULL), '{}') AS module_ids`
}

// issueListRowJSON is the projection issue_on_results returns: the twenty-three required fields plus the three id arrays.
func issueListRowJSON(row issueListRow) gin.H {
	return gin.H{
		"id": row.ID, "name": row.Name, "state_id": row.StateID, "sort_order": row.SortOrder,
		"completed_at": row.CompletedAt, "estimate_point": row.EstimatePointID,
		"priority": row.Priority, "start_date": dateOnly(row.StartDate), "target_date": dateOnly(row.TargetDate),
		"sequence_id": row.SequenceID, "project_id": row.ProjectID, "parent_id": row.ParentID,
		"cycle_id": row.CycleID, "sub_issues_count": countOrZero(row.SubIssuesCount),
		"created_at": row.CreatedAt, "updated_at": row.UpdatedAt,
		"created_by": row.CreatedByID, "updated_by": row.UpdatedByID,
		"attachment_count": countOrZero(row.AttachmentCount), "link_count": countOrZero(row.LinkCount),
		"is_draft": row.IsDraft, "archived_at": dateOnly(row.ArchivedAt),
		"state__group": row.StateGroup,
		"assignee_ids": stringsOrEmpty(row.AssigneeIDs),
		"label_ids":    stringsOrEmpty(row.LabelIDs),
		"module_ids":   stringsOrEmpty(row.ModuleIDs),
	}
}

// queryParams flattens the query string the way Django's QueryDict.get does, which is to take the last value when a parameter repeats.
func queryParams(c *gin.Context) map[string]string {
	params := map[string]string{}
	for name, values := range c.Request.URL.Query() {
		if len(values) > 0 {
			params[name] = values[len(values)-1]
		}
	}
	return params
}

// countPlaceholders says how many arguments a condition consumes, so the flat argument list can be handed out condition by condition.
func countPlaceholders(condition string) int {
	return strings.Count(condition, "?")
}

// ServePublicIssueList is the work item list a published board shows.
//
// It is the session list without the parts that need an account: no project lookup of its own, no guest rule, no recorded visit. Everything else — the filters, the ordering, the annotations, the grouped and sub-grouped paginators — is the same code, because the board's list is the same list read by somebody who is not signed in.
func ServePublicIssueList(c *gin.Context, database *gorm.DB, now time.Time, slug, projectID string) {
	runner := &Handler{db: database, clock: func() time.Time { return now }}

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

	filters := issueFilters(queryParams(c), "GET", "", now)
	joins, conditions, arguments, translatable := issueFilterSQL(filters)
	if !translatable {
		runner.internalError(c, errors.New("issue list: a filter names a field the schema does not have"))
		return
	}
	request := issueListRequest{
		slug: slug, projectID: projectID, perPage: perPage, cursor: cursor,
		joins: joins, conditions: conditions, arguments: arguments,
	}
	var total int64
	if err := runner.issueListScope(c.Request.Context(), request).Distinct("i.id").Count(&total).Error; err != nil {
		runner.internalError(c, err)
		return
	}
	request.total = int(total)

	if c.Query("group_by") != "" {
		runner.issueListGrouped(c, nil, request)
		return
	}
	rows, err := runner.issueListPage(c, request)
	if err != nil {
		runner.internalError(c, err)
		return
	}
	page := pagination.PlanOffsetPage(perPage, cursor, request.total, len(rows), pagination.DefaultPerPage)
	if len(rows) > page.Limit {
		rows = rows[:page.Limit]
	}
	results := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		results = append(results, issueListRowJSON(row))
	}
	drf.Respond(c, http.StatusOK, page.Envelope(results, len(results), nil, nil, nil))
}
