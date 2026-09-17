package project

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api/internal/auth"
	"github.com/yldm-tech/pace/apps/api/internal/drf"
	"github.com/yldm-tech/pace/apps/api/internal/pagination"
)

func (handler *Handler) registerIssueV2ListRoutes(router gin.IRouter) {
	router.GET("/api/workspaces/:slug/projects/:id/v2/issues/", handler.authenticated(handler.issueV2List))
}

// issueV2List is the sync endpoint: a client walks the whole project in updated_at order and keeps a local copy. It is the only issue list that pages with the cursor paginator rather than the offset one, and the only one ordered ascending.
func (handler *Handler) issueV2List(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	slug, projectID := c.Param("slug"), c.Param("id")

	cursor := pagination.DefaultCursor()
	if raw := c.Query("cursor"); raw != "" {
		parsed, err := pagination.ParseCursor(raw)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Please provide valid detail"})
			return
		}
		cursor = parsed
	}

	conditions := []string{}
	arguments := []any{}
	if raw := c.Query("updated_at__gt"); raw != "" {
		since, ok := parseDjangoDateTime(raw)
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Please provide valid detail"})
			return
		}
		conditions = append(conditions, "i.updated_at > ?")
		arguments = append(arguments, since)
	}

	// A guest without full feature access walks only what they raised.
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
		conditions: conditions, arguments: arguments,
	}
	var total int64
	if err := handler.issueListScope(c.Request.Context(), request).Distinct("i.id").Count(&total).Error; err != nil {
		handler.internalError(c, err)
		return
	}
	page, err := pagination.Plan(cursor, int(total))
	if err != nil {
		handler.internalError(c, err)
		return
	}

	var rows []issueListRow
	if page.End > page.Start {
		err := handler.issueListScope(c.Request.Context(), request).
			Select(issueV2Annotations()).
			// Ascending, which is what lets a client resume from where it stopped.
			Order("i.updated_at ASC").
			Offset(page.Start).Limit(page.End - page.Start).
			Scan(&rows).Error
		if err != nil {
			handler.internalError(c, err)
			return
		}
	}

	location, err := userLocation(user)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	withDescription := strings.EqualFold(c.DefaultQuery("description", "false"), "true")
	results := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		results = append(results, issueV2JSON(row, location, withDescription))
	}
	drf.Respond(c, http.StatusOK, page.Envelope(results, len(results)))
}

// issueV2Annotations is this route's own select. Its three id arrays carry no soft-delete filter on the through table, which every other issue list does — so a soft-deleted label link still contributes its id here.
func issueV2Annotations() string {
	return `i.*,
		(SELECT ci.cycle_id FROM cycle_issues ci WHERE ci.issue_id = i.id AND ci.deleted_at IS NULL LIMIT 1) AS cycle_id,
		(SELECT COUNT(*) FROM issue_links il WHERE il.issue_id = i.id AND il.deleted_at IS NULL) AS link_count,
		(SELECT COUNT(*) FROM file_assets fa WHERE fa.issue_id = i.id AND fa.entity_type = 'ISSUE_ATTACHMENT' AND fa.deleted_at IS NULL) AS attachment_count,
		(SELECT COUNT(*) FROM issues sub WHERE sub.parent_id = i.id AND ` + issueObjectsPredicate("sub") + `) AS sub_issues_count,
		(SELECT s.group FROM states s WHERE s.id = i.state_id) AS state_group,
		COALESCE((SELECT ARRAY_AGG(DISTINCT il2.label_id) FROM issue_labels il2 WHERE il2.issue_id = i.id), '{}') AS label_ids,
		COALESCE((SELECT ARRAY_AGG(DISTINCT ia.assignee_id) FROM issue_assignees ia
			JOIN project_members pm2 ON pm2.member_id = ia.assignee_id AND pm2.is_active = TRUE
			WHERE ia.issue_id = i.id), '{}') AS assignee_ids,
		COALESCE((SELECT ARRAY_AGG(DISTINCT mi.module_id) FROM module_issues mi
			JOIN modules m ON m.id = mi.module_id AND m.archived_at IS NULL
			WHERE mi.issue_id = i.id), '{}') AS module_ids`
}

// issueV2JSON is the sync projection: twenty-six fields, plus description_html when the caller asked for it.
func issueV2JSON(row issueListRow, location *time.Location, withDescription bool) gin.H {
	data := gin.H{
		"id": row.ID, "name": row.Name, "state_id": row.StateID, "state__group": row.StateGroup,
		"sort_order": row.SortOrder, "completed_at": row.CompletedAt, "estimate_point": row.EstimatePointID,
		"priority": row.Priority, "start_date": dateOnly(row.StartDate), "target_date": dateOnly(row.TargetDate),
		"sequence_id": row.SequenceID, "project_id": row.ProjectID, "parent_id": row.ParentID,
		"cycle_id": row.CycleID,
		// Only these two move into the caller's timezone.
		"created_at": row.CreatedAt.In(location), "updated_at": row.UpdatedAt.In(location),
		"created_by": row.CreatedByID, "updated_by": row.UpdatedByID,
		"is_draft": row.IsDraft, "archived_at": dateOnly(row.ArchivedAt),
		"module_ids": stringsOrEmpty(row.ModuleIDs), "label_ids": stringsOrEmpty(row.LabelIDs),
		"assignee_ids": stringsOrEmpty(row.AssigneeIDs),
		"link_count":   row.LinkCount, "attachment_count": row.AttachmentCount,
		"sub_issues_count": row.SubIssuesCount,
	}
	if withDescription {
		data["description_html"] = row.DescriptionHTML
	}
	return data
}
