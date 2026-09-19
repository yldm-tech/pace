package project

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api/internal/auth"
	"github.com/yldm-tech/pace/apps/api/internal/drf"
	"gorm.io/gorm"
)

func (handler *Handler) registerSubIssueRoutes(router gin.IRouter) {
	router.GET("/api/workspaces/:slug/projects/:id/issues/:issue/sub-issues/", handler.authenticatedIssueUUID(handler.subIssueList))
	router.POST("/api/workspaces/:slug/projects/:id/issues/:issue/sub-issues/", handler.authenticatedIssueUUID(handler.subIssueAssign))
}

func (handler *Handler) subIssueList(c *gin.Context, user *auth.User) {
	if !handler.requireProjectMembership(c, user) {
		return
	}
	slug, projectID, issueID := c.Param("slug"), c.Param("id"), c.Param("issue")
	rows, err := handler.subIssueRows(c.Request.Context(), slug, projectID, issueID, c.Query("order_by"))
	if err != nil {
		handler.internalError(c, err)
		return
	}
	location, err := userLocation(user)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	// The state distribution is built from every sub-issue regardless of the grouping, and keys on the raw state group.
	distribution := gin.H{}
	serialized := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		group := groupKey(row.StateGroup)
		distribution[group] = append(asStrings(distribution[group]), row.ID)
		serialized = append(serialized, subIssueValuesJSON(row, location))
	}

	groupBy := c.Query("group_by")
	if groupBy == "" {
		drf.Respond(c, http.StatusOK, gin.H{"sub_issues": serialized, "state_distribution": distribution})
		return
	}
	grouped := gin.H{}
	for index, row := range rows {
		data := serialized[index]
		if groupBy == "assignees__ids" {
			// Django fans an issue out across each of its assignees, and files an unassigned issue under the literal string None.
			if len(row.AssigneeIDs) == 0 {
				grouped["None"] = append(asMaps(grouped["None"]), data)
				continue
			}
			for _, assigneeID := range row.AssigneeIDs {
				grouped[assigneeID] = append(asMaps(grouped[assigneeID]), data)
			}
			continue
		}
		value, present := data[groupBy]
		if !present {
			// Django indexes the values() dict directly, so a group_by naming anything else raises KeyError, which BaseAPIView.handle_exception turns into this 400 rather than a 500.
			c.JSON(http.StatusBadRequest, gin.H{"error": "The required key does not exist."})
			return
		}
		key := pythonString(value)
		grouped[key] = append(asMaps(grouped[key]), data)
	}
	drf.Respond(c, http.StatusOK, gin.H{"sub_issues": grouped, "state_distribution": distribution})
}

func (handler *Handler) subIssueAssign(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember) {
		return
	}
	slug, projectID, issueID := c.Param("slug"), c.Param("id"), c.Param("issue")
	// The parent lookup is scoped to the URL workspace and project on purpose: ProjectEntityPermission only proves membership of that project, not that the issue lives in it.
	var parentCount int64
	err := handler.db.WithContext(c.Request.Context()).Table("issues i").
		Where("i.id = ? AND i.project_id = ? AND i.workspace_id = (SELECT id FROM workspaces WHERE slug = ?)", issueID, projectID, slug).
		Where(issueObjectsPredicate("i")).Count(&parentCount).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if parentCount == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Parent issue not found"})
		return
	}
	var request struct {
		SubIssueIDs []string `json:"sub_issue_ids"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		handler.invalidDetail(c)
		return
	}
	if len(request.SubIssueIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Sub Issue IDs are required"})
		return
	}
	// Only the ids that really live in this project are re-parented, and only those get an activity, so a foreign id cannot reach the task and bump updated_at on an issue the caller cannot see.
	scoped := make([]string, 0, len(request.SubIssueIDs))
	err = handler.db.WithContext(c.Request.Context()).Table("issues i").
		Where("i.id IN ? AND i.project_id = ? AND i.workspace_id = (SELECT id FROM workspaces WHERE slug = ?)", request.SubIssueIDs, projectID, slug).
		Where(issueObjectsPredicate("i")).Order("i.created_at DESC").Pluck("i.id", &scoped).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	now := handler.clock().UTC()
	if len(scoped) > 0 {
		// bulk_update writes only the parent column, so no timestamp moves.
		err := handler.db.WithContext(c.Request.Context()).Model(&Issue{}).
			Where("id IN ?", scoped).Update("parent_id", issueID).Error
		if err != nil {
			handler.internalError(c, err)
			return
		}
	}
	for _, subIssueID := range scoped {
		requested, err := json.Marshal(map[string]any{"parent": issueID})
		if err != nil {
			handler.internalError(c, err)
			return
		}
		// Django sends the sub-issue's own id as the previous parent here.
		current, err := json.Marshal(map[string]any{"parent": subIssueID})
		if err != nil {
			handler.internalError(c, err)
			return
		}
		requestedData, currentInstance := string(requested), string(current)
		if err := handler.publishIssueActivity(c, issueActivity{
			Type: "issue.activity.updated", RequestedData: &requestedData, CurrentInstance: &currentInstance,
			ActorID: user.ID, IssueID: subIssueID, ProjectID: projectID,
			Notification: true, Origin: handler.origin(), Epoch: now,
		}); err != nil {
			handler.internalError(c, err)
			return
		}
	}
	rows, err := handler.subIssuesByID(c.Request.Context(), slug, projectID, scoped)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	distribution := gin.H{}
	serialized := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		group := groupKey(row.StateGroup)
		distribution[group] = append(asStrings(distribution[group]), row.ID)
		serialized = append(serialized, issueSerializerJSON(row))
	}
	drf.Respond(c, http.StatusOK, gin.H{"sub_issues": serialized, "state_distribution": distribution})
}

func (handler *Handler) subIssueRows(ctx context.Context, slug, projectID, parentID, orderBy string) ([]issueRow, error) {
	return handler.scanSubIssues(ctx, slug, projectID, func(query *gorm.DB) *gorm.DB {
		return query.Where("i.parent_id = ?", parentID).Order(issueOrderClause(orderBy))
	})
}

// subIssuesByID re-reads the rows the assign route just re-parented. It annotates only state_group, since that response goes through IssueSerializer, and falls back to the model's own -created_at ordering.
func (handler *Handler) subIssuesByID(ctx context.Context, slug, projectID string, identifiers []string) ([]issueRow, error) {
	if len(identifiers) == 0 {
		return nil, nil
	}
	return handler.scanSubIssues(ctx, slug, projectID, func(query *gorm.DB) *gorm.DB {
		return query.Where("i.id IN ?", identifiers).Order("i.created_at DESC")
	})
}

// subIssueAnnotations is the select list for the endpoint. Every subquery mirrors the SQL Django renders, including the places where it does not filter soft-deleted rows: a model's default manager only filters that model's own queryset, never a join traversed through it, so the project_members and modules joins carry no deleted_at predicate.
func subIssueAnnotations() string {
	return issueListColumns + `,
			(SELECT ci.cycle_id FROM cycle_issues ci WHERE ci.issue_id = i.id AND ci.deleted_at IS NULL LIMIT 1) AS cycle_id,
			COALESCE((SELECT COUNT(*) FROM issue_links il WHERE il.issue_id = i.id AND il.deleted_at IS NULL), 0) AS link_count,
			COALESCE((SELECT COUNT(*) FROM file_assets fa WHERE fa.issue_id = i.id AND fa.entity_type = 'ISSUE_ATTACHMENT' AND fa.deleted_at IS NULL), 0) AS attachment_count,
			COALESCE((SELECT COUNT(*) FROM issues sub WHERE sub.parent_id = i.id AND ` + issueObjectsPredicate("sub") + `), 0) AS sub_issues_count,
			COALESCE((SELECT ARRAY_AGG(DISTINCT il2.label_id) FROM issue_labels il2 WHERE il2.issue_id = i.id AND il2.deleted_at IS NULL), '{}') AS label_ids,
			COALESCE((SELECT ARRAY_AGG(DISTINCT ia.assignee_id) FROM issue_assignees ia
				JOIN project_members pm2 ON pm2.member_id = ia.assignee_id AND pm2.is_active = TRUE
				WHERE ia.issue_id = i.id AND ia.deleted_at IS NULL), '{}') AS assignee_ids,
			COALESCE((SELECT ARRAY_AGG(DISTINCT mi.module_id) FROM module_issues mi
				JOIN modules m ON m.id = mi.module_id AND m.archived_at IS NULL
				WHERE mi.issue_id = i.id AND mi.deleted_at IS NULL), '{}') AS module_ids,
			(SELECT s.group FROM states s WHERE s.id = i.state_id) AS state_group`
}

// scanSubIssues applies the shared projection to whichever narrowing the caller needs.
func (handler *Handler) scanSubIssues(ctx context.Context, slug, projectID string, narrow func(*gorm.DB) *gorm.DB) ([]issueRow, error) {
	query := handler.db.WithContext(ctx).Table("issues i").
		Select(subIssueAnnotations()).
		Joins("JOIN workspaces w ON w.id = i.workspace_id").
		Where("w.slug = ? AND i.project_id = ?", slug, projectID).
		Where(issueObjectsPredicate("i"))
	var rows []issueRow
	if err := narrow(query).Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// subIssueValuesJSON is the values() projection the read route returns: IssueSerializer's fields plus state_group, with the two datetime fields moved into the caller's timezone.
func subIssueValuesJSON(row issueRow, location *time.Location) gin.H {
	data := issueListJSON(row)
	data["state_group"] = row.StateGroup
	data["created_at"] = row.CreatedAt.In(location)
	data["updated_at"] = row.UpdatedAt.In(location)
	return data
}

// issueListJSON is IssueSerializer over an annotated row: the detail shape without description_html and without the flags only the detail serializer adds.
func issueListJSON(row issueRow) gin.H {
	data := issueDetailJSON(row, false)
	delete(data, "description_html")
	return data
}

// issueSerializerJSON is IssueSerializer over a plain model instance, which is what the assign route serializes. None of the seven annotated fields exist on the instance, and DRF makes a read-only field not required, so get_attribute raises SkipField for each and the key is dropped rather than rendered as null.
func issueSerializerJSON(row issueRow) gin.H {
	data := issueListJSON(row)
	for _, annotated := range []string{
		"cycle_id", "module_ids", "label_ids", "assignee_ids",
		"sub_issues_count", "attachment_count", "link_count",
	} {
		delete(data, annotated)
	}
	return data
}

// issueObjectsPredicate is the Issue.issue_objects manager. Beyond the soft-delete filter it hides triage, archived and draft issues, and issues whose project is archived. A null state passes the triage check, because Django's exclude() over a nullable join renders as NOT (group = 'triage' AND group IS NOT NULL).
func issueObjectsPredicate(alias string) string {
	return "(" + alias + ".deleted_at IS NULL" +
		" AND (SELECT s.group FROM states s WHERE s.id = " + alias + ".state_id) IS DISTINCT FROM 'triage'" +
		" AND " + alias + ".archived_at IS NULL" +
		" AND EXISTS (SELECT 1 FROM projects p WHERE p.id = " + alias + ".project_id AND p.archived_at IS NULL)" +
		" AND " + alias + ".is_draft = FALSE)"
}

// userLocation resolves the caller's stored timezone. pytz raises on an unknown name, which BaseAPIView turns into a 500, so an unloadable zone is an error here too rather than a silent fallback.
func userLocation(user *auth.User) (*time.Location, error) {
	if user == nil || user.UserTimezone == "" {
		return time.UTC, nil
	}
	return time.LoadLocation(user.UserTimezone)
}

// groupKey renders a null group the way Django's str(None) does.
func groupKey(value *string) string {
	if value == nil {
		return "None"
	}
	return *value
}

// pythonString is str() over a value that came out of values(), which is how Django builds the grouping key.
func pythonString(value any) string {
	switch typed := value.(type) {
	case nil:
		return "None"
	case string:
		return typed
	case *string:
		return groupKey(typed)
	case bool:
		if typed {
			return "True"
		}
		return "False"
	}
	return stringify(value)
}

func asStrings(value any) []string {
	if values, ok := value.([]string); ok {
		return values
	}
	return nil
}

func asMaps(value any) []gin.H {
	if values, ok := value.([]gin.H); ok {
		return values
	}
	return nil
}
