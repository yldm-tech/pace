package externalapi

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"github.com/yldm-tech/pace/apps/api-go/internal/drf"
)

func (handler *Handler) registerIssueSearchRoutes(router gin.IRouter) {
	for _, name := range []string{"issues", "work-items"} {
		router.GET("/api/v1/workspaces/:slug/"+name+"/search/", handler.authenticated(handler.externalIssueSearch))
		router.GET("/api/v1/workspaces/:slug/"+name+"/:reference/", handler.authenticated(handler.issueByReference))
	}
}

// searchSequencePattern is the \b\d+\b the sequence search pulls out of the query.
var searchSequencePattern = regexp.MustCompile(`\b\d+\b`)

// externalIssueSearch finds work items by name, project identifier or number.
//
// It has **no permission class at all** — the base view asks only that the caller is authenticated, so any valid key may search any workspace's slug. What keeps it honest is the queryset, which narrows to projects the caller is an active member of.
//
// And unlike its session-API cousin the sequence branch has **no length guard**: a query of any length is scanned for numbers.
func (handler *Handler) externalIssueSearch(c *gin.Context, user *auth.User, _ APIToken) {
	search := c.Query("search")
	if search == "" {
		// An empty search answers an empty list rather than everything, which is the opposite of the workspace search in the session API.
		drf.Respond(c, http.StatusOK, gin.H{"issues": []gin.H{}})
		return
	}
	// The limit is parsed with int(), so a value that is not a number raises.
	limit := 10
	if raw := c.Query("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			handler.serverError(c, errBadSearchLimit)
			return
		}
		limit = parsed
	}

	conditions := []string{"i.name ILIKE ?", "p.identifier ILIKE ?"}
	arguments := []any{"%" + search + "%", "%" + search + "%"}
	for _, sequence := range searchSequencePattern.FindAllString(search, -1) {
		conditions = append(conditions, "i.sequence_id = ?")
		arguments = append(arguments, sequence)
	}

	query := handler.db.WithContext(c.Request.Context()).Table("issues i").
		Joins("JOIN workspaces w ON w.id = i.workspace_id").
		Joins("JOIN projects p ON p.id = i.project_id AND p.archived_at IS NULL").
		Joins("JOIN project_members pm ON pm.project_id = i.project_id AND pm.member_id = ? AND pm.is_active = TRUE", user.ID).
		Joins("LEFT JOIN states s ON s.id = i.state_id").
		Where("w.slug = ?", c.Param("slug")).
		Where(`i.deleted_at IS NULL AND i.archived_at IS NULL AND i.is_draft = FALSE
			AND (s.group IS DISTINCT FROM 'triage')`).
		Where("("+strings.Join(conditions, " OR ")+")", arguments...)
	if projectID := c.Query("project_id"); c.DefaultQuery("workspace_search", "false") == "false" && projectID != "" {
		query = query.Where("i.project_id = ?", projectID)
	}

	var rows []struct {
		Name              string `gorm:"column:name"`
		ID                string `gorm:"column:id"`
		SequenceID        int64  `gorm:"column:sequence_id"`
		ProjectIdentifier string `gorm:"column:project_identifier"`
		ProjectID         string `gorm:"column:project_id"`
		WorkspaceSlug     string `gorm:"column:workspace_slug"`
	}
	err := query.Distinct().Select(`i.name, i.id, i.sequence_id, p.identifier AS project_identifier,
		i.project_id, w.slug AS workspace_slug`).Limit(limit).Scan(&rows).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	results := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		results = append(results, gin.H{
			"name": row.Name, "id": row.ID, "sequence_id": row.SequenceID,
			"project__identifier": row.ProjectIdentifier, "project_id": row.ProjectID,
			"workspace__slug": row.WorkspaceSlug,
		})
	}
	drf.Respond(c, http.StatusOK, gin.H{"issues": results})
}

var errBadSearchLimit = &orderError{"the search limit is not a number"}

// referencePattern splits PROJ-42 into the project's identifier and the work item's number.
//
// The url captures both halves in one segment with a hyphen between them, and an identifier may itself contain no hyphen — the project routes refuse one — so the **last** hyphen is the split.
var referencePattern = regexp.MustCompile(`^(.+)-([^-]*)$`)

// issueByReference looks a work item up the way a person would name it: by its project's identifier and its number.
//
// This is the only route in the API addressed by something other than a uuid.
func (handler *Handler) issueByReference(c *gin.Context, user *auth.User, _ APIToken) {
	reference := c.Param("reference")
	parts := referencePattern.FindStringSubmatch(reference)
	if parts == nil {
		notFound(c)
		return
	}
	identifier, number := parts[1], parts[2]
	if _, err := strconv.Atoi(number); err != nil {
		// Django matches the segment as two strings and then compares the second against an integer column, which raises rather than answering.
		handler.serverError(c, errBadReference)
		return
	}
	// The permission class reads project_id from the url, and this url has none — so it falls through to the workspace check alone.
	if !handler.requireWorkspaceUser(c, user) {
		return
	}

	var rows []externalIssueRow
	err := handler.db.WithContext(c.Request.Context()).Table("issues i").
		Select(externalIssueSelection()).
		Joins("JOIN workspaces w ON w.id = i.workspace_id").
		Joins("JOIN projects p ON p.id = i.project_id").
		Joins("LEFT JOIN states s ON s.id = i.state_id").
		Where("w.slug = ? AND p.identifier = ? AND i.sequence_id = ?", c.Param("slug"), identifier, number).
		Where(`i.deleted_at IS NULL AND i.archived_at IS NULL AND i.is_draft = FALSE
			AND (s.group IS DISTINCT FROM 'triage') AND p.archived_at IS NULL`).
		Limit(1).Scan(&rows).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if len(rows) == 0 {
		notFound(c)
		return
	}
	drf.Respond(c, http.StatusOK, narrow(externalIssueJSON(rows[0]), requestedFields(c)))
}

var errBadReference = &orderError{"the work item number is not a number"}

// externalIssueRow is the issue the external serializer reports, with the two id lists and the sub-issue count it annotates.
type externalIssueRow struct {
	ID              string         `gorm:"column:id"`
	CreatedAt       time.Time      `gorm:"column:created_at"`
	UpdatedAt       time.Time      `gorm:"column:updated_at"`
	CreatedByID     *string        `gorm:"column:created_by_id"`
	UpdatedByID     *string        `gorm:"column:updated_by_id"`
	DeletedAt       *time.Time     `gorm:"column:deleted_at"`
	ProjectID       string         `gorm:"column:project_id"`
	WorkspaceID     string         `gorm:"column:workspace_id"`
	Name            string         `gorm:"column:name"`
	DescriptionHTML string         `gorm:"column:description_html"`
	DescriptionBin  []byte         `gorm:"column:description_binary"`
	Priority        string         `gorm:"column:priority"`
	StartDate       *time.Time     `gorm:"column:start_date"`
	TargetDate      *time.Time     `gorm:"column:target_date"`
	SequenceID      int64          `gorm:"column:sequence_id"`
	SortOrder       float64        `gorm:"column:sort_order"`
	CompletedAt     *time.Time     `gorm:"column:completed_at"`
	ArchivedAt      *time.Time     `gorm:"column:archived_at"`
	IsDraft         bool           `gorm:"column:is_draft"`
	ExternalSource  *string        `gorm:"column:external_source"`
	ExternalID      *string        `gorm:"column:external_id"`
	StateID         *string        `gorm:"column:state_id"`
	ParentID        *string        `gorm:"column:parent_id"`
	EstimatePointID *string        `gorm:"column:estimate_point_id"`
	TypeID          *string        `gorm:"column:type_id"`
	Point           *int64         `gorm:"column:point"`
	AssigneeIDs     pq.StringArray `gorm:"column:assignee_ids;type:uuid[]"`
	LabelIDs        pq.StringArray `gorm:"column:label_ids;type:uuid[]"`
}

// externalIssueSelection is the columns plus the two many-to-many lists the serializer renders.
func externalIssueSelection() string {
	return `i.*,
		COALESCE((SELECT ARRAY_AGG(DISTINCT ia.assignee_id) FROM issue_assignees ia
			WHERE ia.issue_id = i.id AND ia.deleted_at IS NULL), '{}') AS assignee_ids,
		COALESCE((SELECT ARRAY_AGG(DISTINCT il.label_id) FROM issue_labels il
			WHERE il.issue_id = i.id AND il.deleted_at IS NULL), '{}') AS label_ids`
}

// externalIssueJSON is the external API's IssueSerializer: twenty-nine fields, with the two many-to-many sets rendered as **lists of ids**.
//
// The type is reported twice, once as the relation and once as its id, which is what `type` and `type_id` are.
//
// Three of the columns hold a date rather than an instant — the two planning dates and the archive stamp — and DRF renders a DateField as the day alone. The estimate is a **relation**, and the point beside it is the integer the older estimate used, so neither is a float however the column is read.
func externalIssueJSON(row externalIssueRow) gin.H {
	return gin.H{
		"id": row.ID, "created_at": row.CreatedAt, "updated_at": row.UpdatedAt,
		"created_by": row.CreatedByID, "updated_by": row.UpdatedByID, "deleted_at": row.DeletedAt,
		"name": row.Name, "description_html": row.DescriptionHTML, "description_binary": row.DescriptionBin,
		"priority": row.Priority, "start_date": issueDate(row.StartDate), "target_date": issueDate(row.TargetDate),
		"sequence_id": row.SequenceID, "sort_order": row.SortOrder,
		"completed_at": row.CompletedAt, "archived_at": issueDate(row.ArchivedAt), "is_draft": row.IsDraft,
		"external_source": row.ExternalSource, "external_id": row.ExternalID,
		"point": row.Point, "estimate_point": row.EstimatePointID,
		"state": row.StateID, "parent": row.ParentID,
		"type": row.TypeID, "type_id": row.TypeID,
		"assignees": stringsOrEmptyList(row.AssigneeIDs), "labels": stringsOrEmptyList(row.LabelIDs),
		"project": row.ProjectID, "workspace": row.WorkspaceID,
	}
}

// issueDate renders a date column as the day alone, which is what DRF makes of a DateField. Rendering it as an instant would hand every caller a midnight that is not in the data.
func issueDate(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.Format("2006-01-02")
}

// stringsOrEmptyList renders an id array as a list rather than a null.
func stringsOrEmptyList(values pq.StringArray) []string {
	if values == nil {
		return []string{}
	}
	return []string(values)
}
