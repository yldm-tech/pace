package project

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"github.com/yldm-tech/pace/apps/api-go/internal/drf"
)

func (handler *Handler) registerIssueIdentifierRoutes(router gin.IRouter) {
	router.GET("/api/workspaces/:slug/work-items/:identifier/", handler.authenticated(handler.issueByIdentifier))
}

// issueByIdentifier resolves the human-facing key — the project identifier and the sequence number, as in PROJ-42 — which is what a pasted link carries.
//
// It is the only route that annotates is_intake, so it is also the only one whose IssueDetailSerializer output includes that key.
func (handler *Handler) issueByIdentifier(c *gin.Context, user *auth.User) {
	projectIdentifier, sequence, found := strings.Cut(c.Param("identifier"), "-")
	if !found {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid issue identifier"})
		return
	}
	// strict_str_to_int accepts only digits, with an optional leading minus; anything else is a 400 rather than a 404.
	if !strictInteger(sequence) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid issue identifier"})
		return
	}
	sequenceID, err := strconv.Atoi(sequence)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid issue identifier"})
		return
	}
	slug := c.Param("slug")

	// The identifier is matched case-insensitively, and an unguarded .get means a project that is not there is a 404.
	var projects []Project
	err = handler.db.WithContext(c.Request.Context()).Table("projects p").
		Joins("JOIN workspaces w ON w.id = p.workspace_id").
		Where("LOWER(p.identifier) = LOWER(?) AND w.slug = ?", projectIdentifier, slug).
		Limit(1).Find(&projects).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if len(projects) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "The required object does not exist."})
		return
	}
	project := projects[0]

	member, isMember, err := handler.activeProjectMember(c.Request.Context(), slug, project.ID, user.ID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !isMember {
		c.JSON(http.StatusForbidden, gin.H{"error": "You are not allowed to view this issue"})
		return
	}

	row, ok, err := handler.issueBySequence(c, project.ID, slug, sequenceID, user.ID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "The required object does not exist."})
		return
	}

	// The guest check runs after the issue is read, so a guest asking for an issue that is not there is told it does not exist rather than that they may not see it.
	if member.Role == roleGuest && !project.GuestViewAllFeatures &&
		(row.CreatedByID == nil || *row.CreatedByID != user.ID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "You are not allowed to view this issue"})
		return
	}

	data := issueDetailJSON(row, true)
	data["is_intake"] = row.IsIntake
	drf.Respond(c, http.StatusOK, data)
}

// issueBySequence reads the annotated issue. The queryset is the plain soft-delete manager, so an archived or draft issue is still reachable by its key.
func (handler *Handler) issueBySequence(c *gin.Context, projectID, slug string, sequenceID int, userID string) (issueRow, bool, error) {
	var rows []issueRow
	err := handler.db.WithContext(c.Request.Context()).Table("issues i").
		Select(issueIdentifierAnnotations(), userID, projectID, slug, sequenceID, slug, projectID).
		Joins("JOIN workspaces w ON w.id = i.workspace_id").
		Where("i.project_id = ? AND w.slug = ? AND i.sequence_id = ? AND i.deleted_at IS NULL",
			projectID, slug, sequenceID).
		Order("i.created_at DESC").Limit(1).Scan(&rows).Error
	if err != nil {
		return issueRow{}, false, err
	}
	if len(rows) == 0 {
		return issueRow{}, false, nil
	}
	return rows[0], true, nil
}

// issueIdentifierAnnotations is this route's own select. It differs from the detail route's in annotating is_intake, and in reaching the subscriber check through the sequence number rather than the issue id, which is what the Django queryset does.
func issueIdentifierAnnotations() string {
	return `i.*,
		(SELECT ci.cycle_id FROM cycle_issues ci WHERE ci.issue_id = i.id LIMIT 1) AS cycle_id,
		(SELECT COUNT(*) FROM issue_links il WHERE il.issue_id = i.id AND il.deleted_at IS NULL) AS link_count,
		(SELECT COUNT(*) FROM file_assets fa WHERE fa.issue_id = i.id AND fa.entity_type = 'ISSUE_ATTACHMENT' AND fa.deleted_at IS NULL) AS attachment_count,
		(SELECT COUNT(*) FROM issues sub WHERE sub.parent_id = i.id AND ` + issueObjectsPredicate("sub") + `) AS sub_issues_count,
		COALESCE((SELECT ARRAY_AGG(DISTINCT il2.label_id) FROM issue_labels il2 WHERE il2.issue_id = i.id AND il2.deleted_at IS NULL), '{}') AS label_ids,
		COALESCE((SELECT ARRAY_AGG(DISTINCT ia.assignee_id) FROM issue_assignees ia
			JOIN project_members pm2 ON pm2.member_id = ia.assignee_id AND pm2.is_active = TRUE
			WHERE ia.issue_id = i.id AND ia.deleted_at IS NULL), '{}') AS assignee_ids,
		COALESCE((SELECT ARRAY_AGG(DISTINCT mi.module_id) FROM module_issues mi
			JOIN modules m ON m.id = mi.module_id AND m.archived_at IS NULL
			WHERE mi.issue_id = i.id AND mi.deleted_at IS NULL), '{}') AS module_ids,
		EXISTS (SELECT 1 FROM issue_subscribers isub
			JOIN issues si ON si.id = isub.issue_id
			JOIN workspaces sw ON sw.id = isub.workspace_id
			WHERE isub.subscriber_id = ? AND isub.project_id = ? AND sw.slug = ? AND si.sequence_id = ?) AS is_subscribed,
		EXISTS (SELECT 1 FROM intake_issues ii
			JOIN workspaces iw ON iw.id = ii.workspace_id
			WHERE ii.issue_id = i.id AND ii.status IN (-2, 0) AND iw.slug = ? AND ii.project_id = ?) AS is_intake`
}

// strictInteger is strict_str_to_int: digits, with an optional leading minus, and nothing else. A value like "1e3" or " 12" is refused rather than coerced.
func strictInteger(value string) bool {
	digits := value
	if strings.HasPrefix(digits, "-") {
		digits = digits[1:]
	}
	if digits == "" {
		return false
	}
	for _, character := range digits {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}
