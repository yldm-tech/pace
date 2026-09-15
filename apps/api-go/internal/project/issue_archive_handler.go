package project

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"github.com/yldm-tech/pace/apps/api-go/internal/drf"
	"gorm.io/gorm"
)

func (handler *Handler) registerIssueArchiveRoutes(router gin.IRouter) {
	router.GET("/api/workspaces/:slug/projects/:id/issues/:issue/archive/", handler.authenticatedIssueUUID(handler.issueArchiveRetrieve))
	router.POST("/api/workspaces/:slug/projects/:id/issues/:issue/archive/", handler.authenticatedIssueUUID(handler.issueArchive))
	router.DELETE("/api/workspaces/:slug/projects/:id/issues/:issue/archive/", handler.authenticatedIssueUUID(handler.issueUnarchive))
	router.POST("/api/workspaces/:slug/projects/:id/bulk-archive-issues/", handler.authenticated(handler.bulkArchiveIssues))
}

// errorCodeInvalidArchiveStateGroup is ERROR_CODES["INVALID_ARCHIVE_STATE_GROUP"].
const errorCodeInvalidArchiveStateGroup = 4091

// archivableStateGroups is the rule both archive routes enforce: an issue may only be archived once its work is over.
var archivableStateGroups = map[string]bool{"completed": true, "cancelled": true}

// issueArchiveRetrieve returns an archived issue. Unlike the issue detail route this queryset applies no annotations beyond is_subscribed, so every field IssueDetailSerializer reads off one is dropped by DRF and the response is the plain instance plus description_html and is_subscribed.
func (handler *Handler) issueArchiveRetrieve(c *gin.Context, user *auth.User) {
	// retrieve carries no allow_permission decorator, so it falls back to the viewset's default of merely being authenticated.
	slug, projectID, issueID := c.Param("slug"), c.Param("id"), c.Param("issue")
	var rows []issueRow
	err := handler.db.WithContext(c.Request.Context()).Table("issues i").
		Select(`i.*, EXISTS (SELECT 1 FROM issue_subscribers isub
			JOIN workspaces sw ON sw.id = isub.workspace_id
			WHERE isub.issue_id = i.id AND isub.subscriber_id = ? AND isub.project_id = ? AND sw.slug = ?) AS is_subscribed`,
			user.ID, projectID, slug).
		Joins("JOIN workspaces w ON w.id = i.workspace_id").
		// The archive queryset reads through the plain soft-delete manager, so a draft issue is still returned; what it does add is that the issue must be archived and must not be an epic.
		Where(`i.id = ? AND i.project_id = ? AND w.slug = ? AND i.deleted_at IS NULL AND i.archived_at IS NOT NULL
			AND (i.type_id IS NULL OR NOT EXISTS (SELECT 1 FROM issue_types t WHERE t.id = i.type_id AND t.is_epic))`,
			issueID, projectID, slug).
		Order("i.created_at DESC").Limit(1).Scan(&rows).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if len(rows) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "The required object does not exist."})
		return
	}
	data := issueSerializerJSON(rows[0])
	data["description_html"] = rows[0].DescriptionHTML
	data["is_subscribed"] = rows[0].IsSubscribed
	drf.Respond(c, http.StatusOK, data)
}

func (handler *Handler) issueArchive(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember) {
		return
	}
	slug, projectID, issueID := c.Param("slug"), c.Param("id"), c.Param("issue")
	var issue Issue
	err := handler.db.WithContext(c.Request.Context()).Table("issues i").
		Joins("JOIN workspaces w ON w.id = i.workspace_id").
		Where("i.id = ? AND i.project_id = ? AND w.slug = ?", issueID, projectID, slug).
		Where(issueObjectsPredicate("i")).Take(&issue).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "The required object does not exist."})
		return
	}
	if err != nil {
		handler.internalError(c, err)
		return
	}
	group, err := handler.issueStateGroup(c.Request.Context(), issue)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if group == nil {
		// Django reads issue.state.group with no guard, so a stateless issue raises AttributeError and answers 500. issue_objects hides triage issues but not stateless ones.
		handler.internalError(c, errors.New("issue has no state"))
		return
	}
	if !archivableStateGroups[*group] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Can only archive completed or cancelled state group issue"})
		return
	}

	now := handler.clock().UTC()
	archivedAt := now.Format("2006-01-02")
	if err := handler.publishArchiveActivity(c, issue, user, &archivedAt, now); err != nil {
		handler.internalError(c, err)
		return
	}
	if err := handler.writeArchivedAt(c.Request.Context(), issue, &archivedAt, now); err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, gin.H{"archived_at": archivedAt})
}

func (handler *Handler) issueUnarchive(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember) {
		return
	}
	slug, projectID, issueID := c.Param("slug"), c.Param("id"), c.Param("issue")
	var issue Issue
	// Unarchiving reads through the plain manager, which is what lets it reach the archived row that issue_objects hides.
	err := handler.db.WithContext(c.Request.Context()).Table("issues i").
		Joins("JOIN workspaces w ON w.id = i.workspace_id").
		Where("i.id = ? AND i.project_id = ? AND w.slug = ? AND i.deleted_at IS NULL AND i.archived_at IS NOT NULL",
			issueID, projectID, slug).Take(&issue).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "The required object does not exist."})
		return
	}
	if err != nil {
		handler.internalError(c, err)
		return
	}
	now := handler.clock().UTC()
	if err := handler.publishArchiveActivity(c, issue, user, nil, now); err != nil {
		handler.internalError(c, err)
		return
	}
	if err := handler.writeArchivedAt(c.Request.Context(), issue, nil, now); err != nil {
		handler.internalError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (handler *Handler) bulkArchiveIssues(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember) {
		return
	}
	slug, projectID := c.Param("slug"), c.Param("id")
	var request struct {
		IssueIDs []string `json:"issue_ids"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		handler.invalidDetail(c)
		return
	}
	if len(request.IssueIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Issue IDs are required"})
		return
	}
	// The plain manager, so an already-archived or draft issue is included here even though the single-issue route would not reach it.
	var issues []Issue
	err := handler.db.WithContext(c.Request.Context()).Table("issues i").
		Joins("JOIN workspaces w ON w.id = i.workspace_id").
		Where("i.id IN ? AND i.project_id = ? AND w.slug = ? AND i.deleted_at IS NULL",
			request.IssueIDs, projectID, slug).
		Order("i.created_at DESC").Find(&issues).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}

	now := handler.clock().UTC()
	archivedAt := now.Format("2006-01-02")
	groups, err := handler.issueStateGroups(c.Request.Context(), issues)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	archivable := make([]string, 0, len(issues))
	for _, issue := range issues {
		group, known := groups[issue.ID]
		if !known || !archivableStateGroups[group] {
			// Django returns from inside the loop, so the activities it already queued for the issues ahead of this one stay queued even though bulk_update never runs and nothing is archived. Reproduced rather than tidied: the task is fire-and-forget, so hoisting the check would change which notifications a caller sees.
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code":    errorCodeInvalidArchiveStateGroup,
				"error_message": "INVALID_ARCHIVE_STATE_GROUP",
			})
			return
		}
		if err := handler.publishArchiveActivity(c, issue, user, &archivedAt, now); err != nil {
			handler.internalError(c, err)
			return
		}
		archivable = append(archivable, issue.ID)
	}
	if len(archivable) > 0 {
		// bulk_update writes only archived_at, so unlike the single-issue route no timestamp moves and description_stripped is not recomputed.
		err := handler.db.WithContext(c.Request.Context()).Model(&Issue{}).
			Where("id IN ?", archivable).Update("archived_at", archivedAt).Error
		if err != nil {
			handler.internalError(c, err)
			return
		}
	}
	drf.Respond(c, http.StatusOK, gin.H{"archived_at": archivedAt})
}

// writeArchivedAt is Issue.save on an existing row. Beyond the column that changed it recomputes description_stripped and lets the timestamp move, because save writes the whole instance rather than the one field the caller touched.
//
// completed_at does not move: _sync_completed_at returns early unless the state itself changed, and archiving never touches it.
func (handler *Handler) writeArchivedAt(ctx context.Context, issue Issue, archivedAt *string, now time.Time) error {
	updates := map[string]any{
		"archived_at":          archivedAt,
		"description_stripped": strippedIssueDescription(issue.DescriptionHTML),
		"updated_at":           now,
	}
	return handler.db.WithContext(ctx).Model(&Issue{}).Where("id = ?", issue.ID).Updates(updates).Error
}

// strippedIssueDescription is Issue.save's description_stripped. Unlike the comment's, an empty body stores null rather than an empty string.
func strippedIssueDescription(html string) *string {
	if html == "" {
		return nil
	}
	stripped := strippedComment(html)
	return &stripped
}

// publishArchiveActivity queues the activity both routes send. current_instance is IssueSerializer over the plain instance, so it carries none of the annotated fields.
func (handler *Handler) publishArchiveActivity(c *gin.Context, issue Issue, user *auth.User, archivedAt *string, now time.Time) error {
	requested := map[string]any{"archived_at": nil}
	if archivedAt != nil {
		// Archiving sends the date and an automation flag; unarchiving sends only the null.
		requested = map[string]any{"archived_at": *archivedAt, "automation": false}
	}
	requestedBody, err := json.Marshal(requested)
	if err != nil {
		return err
	}
	snapshot, err := json.Marshal(issueSerializerJSON(issueRow{Issue: issue}))
	if err != nil {
		return err
	}
	requestedData, currentInstance := string(requestedBody), string(snapshot)
	return handler.publishIssueActivity(c, issueActivity{
		Type: "issue.activity.updated", RequestedData: &requestedData, CurrentInstance: &currentInstance,
		ActorID: user.ID, IssueID: issue.ID, ProjectID: issue.ProjectID,
		Notification: true, Origin: handler.origin(), Epoch: now,
	})
}

// issueStateGroups is select_related("state") over a batch: one query rather than one per issue. An issue with no state, or one whose state row is gone, is simply absent from the map, which is the case Django turns into an AttributeError.
func (handler *Handler) issueStateGroups(ctx context.Context, issues []Issue) (map[string]string, error) {
	stateIDs := make([]string, 0, len(issues))
	for _, issue := range issues {
		if issue.StateID != nil {
			stateIDs = append(stateIDs, *issue.StateID)
		}
	}
	groups := map[string]string{}
	if len(stateIDs) == 0 {
		return groups, nil
	}
	var rows []struct {
		ID    string `gorm:"column:id"`
		Group string `gorm:"column:group"`
	}
	err := handler.db.WithContext(ctx).Table("states").Select(`id, "group"`).Where("id IN ?", stateIDs).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	byState := make(map[string]string, len(rows))
	for _, row := range rows {
		byState[row.ID] = row.Group
	}
	for _, issue := range issues {
		if issue.StateID == nil {
			continue
		}
		if group, found := byState[*issue.StateID]; found {
			groups[issue.ID] = group
		}
	}
	return groups, nil
}

// issueStateGroup reads the group Django reaches through issue.state, returning nil when the issue has no state.
func (handler *Handler) issueStateGroup(ctx context.Context, issue Issue) (*string, error) {
	if issue.StateID == nil {
		return nil, nil
	}
	var groups []string
	err := handler.db.WithContext(ctx).Table("states").Where("id = ?", *issue.StateID).Pluck("\"group\"", &groups).Error
	if err != nil {
		return nil, err
	}
	if len(groups) == 0 {
		return nil, nil
	}
	return &groups[0], nil
}
