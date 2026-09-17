package project

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api/internal/auth"
	"github.com/yldm-tech/pace/apps/api/internal/drf"
	"github.com/yldm-tech/pace/apps/api/internal/pagination"
)

func (handler *Handler) registerIssueDetailListRoutes(router gin.IRouter) {
	router.GET("/api/workspaces/:slug/projects/:id/issues-detail/", handler.authenticated(handler.issueDetailList))
}

// issueDetailList is the flat paginated list behind issues-detail/. It differs from issues/ in three ways: it never groups, it narrows through a permission subquery rather than a separate guest check, and its rows go through IssueListDetailSerializer rather than the values() projection.
func (handler *Handler) issueDetailList(c *gin.Context, user *auth.User) {
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

	filters := issueFilters(queryParams(c), "GET", "", handler.clock().UTC())
	joins, conditions, arguments, translatable := issueFilterSQL(filters)
	if !translatable {
		handler.internalError(c, errors.New("issues-detail: a filter names a field the schema does not have"))
		return
	}

	// The permission subquery is what narrows a guest's view here: a member above guest sees everything, a guest sees everything when the project opens all features to guests, and otherwise only what they raised.
	conditions = append(conditions, issueVisibilityPredicate)
	arguments = append(arguments, user.ID, user.ID, user.ID, user.ID)

	request := issueListRequest{
		slug: slug, projectID: projectID, perPage: perPage, cursor: cursor,
		joins: joins, conditions: conditions, arguments: arguments,
	}
	var total int64
	if err := handler.issueListScope(c.Request.Context(), request).Distinct("i.id").Count(&total).Error; err != nil {
		handler.internalError(c, err)
		return
	}
	request.total = int(total)

	rows, err := handler.issueListPage(c, request)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	page := pagination.PlanOffsetPage(perPage, cursor, request.total, len(rows), pagination.DefaultPerPage)
	if len(rows) > page.Limit {
		rows = rows[:page.Limit]
	}

	expand := map[string]bool{}
	for _, name := range strings.Split(c.Query("expand"), ",") {
		if name != "" {
			expand[name] = true
		}
	}
	relations, err := handler.issueRelationExpansions(c, rows, expand)
	if err != nil {
		handler.internalError(c, err)
		return
	}

	results := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		results = append(results, issueListDetailJSON(row, relations[row.ID]))
	}
	drf.Respond(c, http.StatusOK, page.Envelope(results, len(results), nil, nil, nil))
}

// issueVisibilityPredicate is the Exists subquery the route filters on. The three branches are the same three the list route's guest check collapses into, written as one predicate because Django writes them that way here.
const issueVisibilityPredicate = `EXISTS (
	SELECT 1 FROM project_members pv
	JOIN projects pvp ON pvp.id = pv.project_id
	WHERE pv.project_id = i.project_id AND pv.is_active = TRUE AND (
		(pv.member_id = ? AND pv.role > 5)
		OR (pv.member_id = ? AND pv.role = 5 AND pvp.guest_view_all_features = TRUE)
		OR (pv.member_id = ? AND pv.role = 5 AND pvp.guest_view_all_features = FALSE AND i.created_by_id = ?)
	))`

// issueListDetailJSON is IssueListDetailSerializer: twenty-three fields, plus the two relation lists when they were asked for.
//
// Its id arrays come from prefetched rows rather than from an aggregate, so unlike the other projections they carry no archived-module filter — only the soft-delete one the related manager applies.
func issueListDetailJSON(row issueListRow, relations map[string][]gin.H) gin.H {
	data := gin.H{
		"id": row.ID, "name": row.Name, "state_id": row.StateID, "sort_order": row.SortOrder,
		"completed_at": row.CompletedAt, "estimate_point": row.EstimatePointID,
		"priority": row.Priority, "start_date": dateOnly(row.StartDate), "target_date": dateOnly(row.TargetDate),
		"sequence_id": row.SequenceID, "project_id": row.ProjectID, "parent_id": row.ParentID,
		"created_at": row.CreatedAt, "updated_at": row.UpdatedAt,
		"created_by": row.CreatedByID, "updated_by": row.UpdatedByID,
		"is_draft": row.IsDraft, "archived_at": dateOnly(row.ArchivedAt),
		"cycle_id": row.CycleID, "module_ids": stringsOrEmpty(row.ModuleIDs),
		"label_ids": stringsOrEmpty(row.LabelIDs), "assignee_ids": stringsOrEmpty(row.AssigneeIDs),
		// These three are the raw annotations rather than coalesced counts, so a null stays null here.
		"sub_issues_count": row.SubIssuesCount,
		"attachment_count": row.AttachmentCount,
		"link_count":       row.LinkCount,
	}
	for name, list := range relations {
		data[name] = list
	}
	return data
}

// issueRelationExpansions reads the two relation lists the expand parameter can ask for. A relation whose far side is gone is skipped rather than rendered as null.
func (handler *Handler) issueRelationExpansions(c *gin.Context, rows []issueListRow, expand map[string]bool) (map[string]map[string][]gin.H, error) {
	wanted := []string{}
	for _, name := range []string{"issue_relation", "issue_related"} {
		if expand[name] {
			wanted = append(wanted, name)
		}
	}
	expansions := map[string]map[string][]gin.H{}
	if len(wanted) == 0 || len(rows) == 0 {
		return expansions, nil
	}
	identifiers := make([]string, 0, len(rows))
	for _, row := range rows {
		identifiers = append(identifiers, row.ID)
		expansions[row.ID] = map[string][]gin.H{}
		for _, name := range wanted {
			expansions[row.ID][name] = []gin.H{}
		}
	}

	for _, name := range wanted {
		// issue_relation reads the far side of a relation the issue owns; issue_related reads the near side of one that points at it.
		owner, far := "r.issue_id", "r.related_issue_id"
		if name == "issue_related" {
			owner, far = "r.related_issue_id", "r.issue_id"
		}
		var rows []struct {
			OwnerID      string    `gorm:"column:owner_id"`
			ID           string    `gorm:"column:id"`
			ProjectID    string    `gorm:"column:project_id"`
			SequenceID   int       `gorm:"column:sequence_id"`
			Name         string    `gorm:"column:name"`
			RelationType string    `gorm:"column:relation_type"`
			StateID      *string   `gorm:"column:state_id"`
			Priority     string    `gorm:"column:priority"`
			CreatedByID  *string   `gorm:"column:created_by_id"`
			UpdatedByID  *string   `gorm:"column:updated_by_id"`
			CreatedAt    time.Time `gorm:"column:created_at"`
			UpdatedAt    time.Time `gorm:"column:updated_at"`
		}
		err := handler.db.WithContext(c.Request.Context()).Table("issue_relations r").
			Select(owner+` AS owner_id, far.id, far.project_id, far.sequence_id, far.name,
				r.relation_type, far.state_id, far.priority, far.created_by_id, far.updated_by_id,
				far.created_at, far.updated_at`).
			Joins("JOIN issues far ON far.id = "+far).
			Where(owner+" IN ? AND r.deleted_at IS NULL", identifiers).
			Scan(&rows).Error
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			expansions[row.OwnerID][name] = append(expansions[row.OwnerID][name], gin.H{
				"id": row.ID, "project_id": row.ProjectID, "sequence_id": row.SequenceID,
				"name": row.Name, "relation_type": row.RelationType, "state_id": row.StateID,
				"priority": row.Priority, "created_by": row.CreatedByID, "updated_by": row.UpdatedByID,
				"created_at": row.CreatedAt, "updated_at": row.UpdatedAt,
			})
		}
	}
	return expansions, nil
}
