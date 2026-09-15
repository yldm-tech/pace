package project

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"github.com/yldm-tech/pace/apps/api-go/internal/drf"
)

func (handler *Handler) registerIssueBulkListRoutes(router gin.IRouter) {
	router.GET("/api/workspaces/:slug/projects/:id/issues/list/", handler.authenticated(handler.issueBulkList))
}

// issueBulkList reads a named set of issues rather than a page of them, which is what the client uses to refresh the issues it already knows about. It returns a bare list with no envelope.
func (handler *Handler) issueBulkList(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	slug, projectID := c.Param("slug"), c.Param("id")

	raw := c.Query("issues")
	if raw == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Issues are required"})
		return
	}
	identifiers := []string{}
	for _, identifier := range strings.Split(raw, ",") {
		if identifier != "" {
			identifiers = append(identifiers, identifier)
		}
	}

	// The fields parameter is popped by DynamicBaseSerializer and then overwritten by expand, so it has no effect at all; only expand changes the shape.
	expand := c.Query("expand")
	if expand != "" {
		// The expansion serializers are not ported, and answering with an unexpanded body would be a wrong shape rather than an error.
		handler.internalError(c, errors.New("issue list: expand is not migrated"))
		return
	}

	conditions := []string{}
	arguments := []any{}
	filters := issueFilters(queryParams(c), "GET", "", handler.clock().UTC())
	joins, filterConditions, filterArguments, translatable := issueFilterSQL(filters)
	if !translatable {
		handler.internalError(c, errors.New("issue list: a filter names a field the schema does not have"))
		return
	}
	conditions = append(conditions, filterConditions...)
	arguments = append(arguments, filterArguments...)

	// A guest without full feature access sees only what they raised. Unlike the paginated list, the project's own flag is part of the membership lookup here rather than a second read.
	member, found, err := handler.activeProjectMember(c.Request.Context(), slug, projectID, user.ID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if found && member.Role == roleGuest {
		var guestSeesEverything []bool
		err := handler.db.WithContext(c.Request.Context()).Table("projects").
			Where("id = ?", projectID).Limit(1).Pluck("guest_view_all_features", &guestSeesEverything).Error
		if err != nil {
			handler.internalError(c, err)
			return
		}
		if len(guestSeesEverything) > 0 && !guestSeesEverything[0] {
			conditions = append(conditions, "i.created_by_id = ?")
			arguments = append(arguments, user.ID)
		}
	}

	request := issueListRequest{
		slug: slug, projectID: projectID,
		joins: joins, conditions: conditions, arguments: arguments,
	}
	query := handler.issueListScope(c.Request.Context(), request).
		Select(issueListAnnotations()).
		// This endpoint adds one predicate the paginated list does not: the issue's state must not be soft deleted.
		Where("i.id IN ? AND (i.state_id IS NULL OR EXISTS (SELECT 1 FROM states s2 WHERE s2.id = i.state_id AND s2.deleted_at IS NULL))", identifiers).
		Group("i.id")

	var rows []issueListRow
	if err := query.Scan(&rows).Error; err != nil {
		handler.internalError(c, err)
		return
	}

	if c.Query("fields") != "" {
		// With no expand the serializer's fields are untouched, so this is the plain twenty-five field shape.
		serialized := make([]gin.H, 0, len(rows))
		for _, row := range rows {
			serialized = append(serialized, issueSerializerJSON(row.issueRowForSerializer()))
		}
		drf.Respond(c, http.StatusOK, serialized)
		return
	}

	location, err := userLocation(user)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	serialized := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		serialized = append(serialized, issueBulkListJSON(row, location))
	}
	drf.Respond(c, http.StatusOK, serialized)
}

// issueBulkListJSON is this endpoint's own values() projection. It differs from the paginated list's in two ways: it carries deleted_at, and it has no state__group.
func issueBulkListJSON(row issueListRow, location *time.Location) gin.H {
	return gin.H{
		"id": row.ID, "name": row.Name, "state_id": row.StateID, "sort_order": row.SortOrder,
		"completed_at": row.CompletedAt, "estimate_point": row.EstimatePointID,
		"priority": row.Priority, "start_date": dateOnly(row.StartDate), "target_date": dateOnly(row.TargetDate),
		"sequence_id": row.SequenceID, "project_id": row.ProjectID, "parent_id": row.ParentID,
		"cycle_id": row.CycleID, "module_ids": stringsOrEmpty(row.ModuleIDs),
		"label_ids": stringsOrEmpty(row.LabelIDs), "assignee_ids": stringsOrEmpty(row.AssigneeIDs),
		"sub_issues_count": countOrZero(row.SubIssuesCount),
		// Only these two move into the caller's timezone.
		"created_at": row.CreatedAt.In(location), "updated_at": row.UpdatedAt.In(location),
		"created_by": row.CreatedByID, "updated_by": row.UpdatedByID,
		"attachment_count": countOrZero(row.AttachmentCount), "link_count": countOrZero(row.LinkCount),
		"is_draft": row.IsDraft, "archived_at": dateOnly(row.ArchivedAt),
		"deleted_at": row.DeletedAt,
	}
}

// issueRowForSerializer reshapes the list row into what the issue serializers read.
func (row issueListRow) issueRowForSerializer() issueRow {
	return issueRow{
		Issue: row.Issue, CycleID: row.CycleID,
		LinkCount: row.LinkCount, AttachmentCount: row.AttachmentCount, SubIssuesCount: row.SubIssuesCount,
		LabelIDs: row.LabelIDs, AssigneeIDs: row.AssigneeIDs, ModuleIDs: row.ModuleIDs,
	}
}
