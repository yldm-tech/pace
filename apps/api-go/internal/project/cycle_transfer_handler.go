package project

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"github.com/yldm-tech/pace/apps/api-go/internal/drf"
	"gorm.io/gorm"
)

func (handler *Handler) registerCycleTransferRoutes(router gin.IRouter) {
	router.POST("/api/workspaces/:slug/projects/:id/cycles/:cycle/transfer-issues/", handler.authenticated(handler.cycleTransferIssues))
}

// cycleTransferIssues moves a cycle's unfinished work into another cycle, and freezes what the old one looked like on the way out.
//
// The freeze is the reason the endpoint is more than an update. Once the issues are gone the old cycle can no longer be measured, so everything its board would have shown — the six counts, both distributions, both burndowns — is computed first and written into progress_snapshot, which is what the progress and analytics endpoints read from that point on.
func (handler *Handler) cycleTransferIssues(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember) {
		return
	}
	slug, projectID, cycleID := c.Param("slug"), c.Param("id"), c.Param("cycle")

	var request struct {
		NewCycleID string `json:"new_cycle_id"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		handler.invalidDetail(c)
		return
	}
	if request.NewCycleID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "New Cycle Id is required"})
		return
	}

	var destination Cycle
	err := handler.db.WithContext(c.Request.Context()).Table("cycles c").Select("c.*").
		Joins("JOIN workspaces w ON w.id = c.workspace_id").
		Where("w.slug = ? AND c.project_id = ? AND c.id = ? AND c.deleted_at IS NULL", slug, projectID, request.NewCycleID).
		Take(&destination).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// Django reads the end date off the result of .first() with no guard, so naming a cycle that is not there raises rather than answering 400.
		handler.internalError(c, errors.New("cycle transfer: the destination cycle does not exist"))
		return
	}
	if err != nil {
		handler.internalError(c, err)
		return
	}
	now := handler.clock().UTC()
	if destination.EndDate != nil && destination.EndDate.Before(now) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "The cycle where the issues are transferred is already completed"})
		return
	}

	source, err := handler.cycleWithTransferCounts(c, slug, projectID, cycleID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Source cycle not found"})
		return
	}
	if err != nil {
		handler.internalError(c, err)
		return
	}

	snapshot, err := handler.cycleProgressSnapshot(c, slug, projectID, cycleID, source)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	// The snapshot and the move are two statements, not one transaction: Django runs this view in autocommit, so a failure between them leaves the snapshot written and the issues where they were.
	err = handler.db.WithContext(c.Request.Context()).Model(&Cycle{}).
		Where("id = ?", cycleID).Update("progress_snapshot", encoded).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}

	moved, err := handler.moveUnfinishedCycleIssues(c, slug, projectID, cycleID, request.NewCycleID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if err := handler.publishCycleTransferActivity(c, user, projectID, cycleID, request.NewCycleID, moved, now); err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, gin.H{"message": "Success"})
}

// transferCounts is the source cycle and the six numbers the snapshot freezes.
//
// The counts are declared inline rather than pulled in by embedding a struct of their own, because GORM parses a **second** anonymous struct as nothing at all: the fields under it get no column, scan no value and raise no error. The guard test names the shape. It is the same family of silent failure as a struct embedded two levels deep, which cost a CI run earlier in this migration.
type transferCounts struct {
	Cycle
	Total     int64 `gorm:"column:total_issues"`
	Completed int64 `gorm:"column:completed_issues"`
	Cancelled int64 `gorm:"column:cancelled_issues"`
	Started   int64 `gorm:"column:started_issues"`
	Unstarted int64 `gorm:"column:unstarted_issues"`
	Backlog   int64 `gorm:"column:backlog_issues"`
}

// cycleWithTransferCounts reads the source cycle and counts its work, one state group at a time.
//
// These counts are not the cycle list's. They count **links** rather than distinct issues, and they exclude only what the filter names — deleted, archived and draft issues and dead links — so a triage issue or one in an archived project is counted here and not by the manager the distributions go through.
func (handler *Handler) cycleWithTransferCounts(c *gin.Context, slug, projectID, cycleID string) (transferCounts, error) {
	var row transferCounts
	err := handler.db.WithContext(c.Request.Context()).Table("cycles c").Select("c.*, "+transferCountSelection()).
		Joins("JOIN workspaces w ON w.id = c.workspace_id").
		Where("w.slug = ? AND c.project_id = ? AND c.id = ? AND c.deleted_at IS NULL", slug, projectID, cycleID).
		Take(&row).Error
	return row, err
}

// transferCountSelection is the six counts the snapshot freezes, written as they are counted rather than as the manager would count them.
func transferCountSelection() string {
	const liveLink = `tci.deleted_at IS NULL AND ti.deleted_at IS NULL
		AND ti.archived_at IS NULL AND ti.is_draft = FALSE`
	groupCount := func(group, alias string) string {
		return `(SELECT COUNT(ts.group) FROM cycle_issues tci
			JOIN issues ti ON ti.id = tci.issue_id
			JOIN states ts ON ts.id = ti.state_id
			WHERE tci.cycle_id = c.id AND ts.group = '` + group + `' AND ` + liveLink + `) AS ` + alias
	}
	return `(SELECT COUNT(tci.id) FROM cycle_issues tci
			JOIN issues ti ON ti.id = tci.issue_id
			WHERE tci.cycle_id = c.id AND ` + liveLink + `) AS total_issues,
		` + groupCount("completed", "completed_issues") + `,
		` + groupCount("cancelled", "cancelled_issues") + `,
		` + groupCount("started", "started_issues") + `,
		` + groupCount("unstarted", "unstarted_issues") + `,
		` + groupCount("backlog", "backlog_issues")
}

// transferDistributionCounts is the three counts the frozen distribution carries.
func transferDistributionCounts() string {
	return `COUNT(i.id) FILTER (WHERE ` + liveIssue + `) AS total_issues,
		COUNT(i.id) FILTER (WHERE ` + completedIssues + `) AS completed_issues,
		COUNT(i.id) FILTER (WHERE ` + pendingIssues + `) AS pending_issues`
}

// cycleProgressSnapshot builds what gets frozen: six counts, two distributions and two burndowns.
func (handler *Handler) cycleProgressSnapshot(c *gin.Context, slug, projectID, cycleID string, source transferCounts) (map[string]any, error) {
	assignees, labels, err := handler.cycleTransferDistributions(c, slug, projectID, cycleID)
	if err != nil {
		return nil, err
	}
	chart, err := handler.cycleBurndown(c, slug, projectID, cycleID, source.Cycle, source.Total, false)
	if err != nil {
		return nil, err
	}

	estimates := map[string]any{}
	points, err := handler.projectEstimatesPoints(c, slug, projectID)
	if err != nil {
		return nil, err
	}
	if points {
		estimateAssignees, estimateLabels, err := handler.cycleEstimateDistributions(c, slug, projectID, cycleID)
		if err != nil {
			return nil, err
		}
		estimateChart, err := handler.cycleBurndown(c, slug, projectID, cycleID, source.Cycle, source.Total, true)
		if err != nil {
			return nil, err
		}
		estimates = map[string]any{
			"labels":           estimateLabels,
			"assignees":        estimateAssignees,
			"completion_chart": estimateChart,
		}
	}

	return map[string]any{
		"total_issues":     source.Total,
		"completed_issues": source.Completed,
		"cancelled_issues": source.Cancelled,
		"started_issues":   source.Started,
		"unstarted_issues": source.Unstarted,
		"backlog_issues":   source.Backlog,
		"distribution": map[string]any{
			"labels":           labels,
			"assignees":        assignees,
			"completion_chart": chart,
		},
		"estimate_distribution": estimates,
	}, nil
}

// cycleTransferDistributions counts issues per assignee and per label for the frozen snapshot.
//
// It is the analytics endpoint's query with one word changed, and the word matters: this one counts `id` while that one counts the grouping column. So the bucket holding the issues with **no** assignee is counted properly here and reported as zero there, for the same cycle. Two blocks that read alike and do not agree.
func (handler *Handler) cycleTransferDistributions(c *gin.Context, slug, projectID, cycleID string) ([]gin.H, []gin.H, error) {
	counts := transferDistributionCounts()

	var assigneeRows []distributionRow
	err := handler.cycleAssigneeScope(c, slug, projectID, cycleID).
		Select("u.display_name, ia.assignee_id, " + avatarURL + ", " + counts).
		Order("u.display_name").Scan(&assigneeRows).Error
	if err != nil {
		return nil, nil, err
	}
	var labelRows []distributionRow
	err = handler.cycleLabelScope(c, slug, projectID, cycleID).
		Select("l.name AS label_name, l.color, il.label_id, " + counts).
		Order("l.name").Scan(&labelRows).Error
	if err != nil {
		return nil, nil, err
	}

	assignees := make([]gin.H, 0, len(assigneeRows))
	for _, row := range assigneeRows {
		assignees = append(assignees, gin.H{
			"display_name": row.DisplayName, "assignee_id": row.AssigneeID, "avatar_url": row.AvatarURL,
			"total_issues": row.TotalIssues, "completed_issues": row.DoneIssues, "pending_issues": row.LeftIssues,
		})
	}
	labels := make([]gin.H, 0, len(labelRows))
	for _, row := range labelRows {
		labels = append(labels, gin.H{
			"label_name": row.LabelName, "color": row.Color, "label_id": row.LabelID,
			"total_issues": row.TotalIssues, "completed_issues": row.DoneIssues, "pending_issues": row.LeftIssues,
		})
	}
	return assignees, labels, nil
}

// transferredIssue is one link that moved, named the way the activity payload names it.
type transferredIssue struct {
	IssueID string `gorm:"column:issue_id" json:"issue_id"`
}

// moveUnfinishedCycleIssues repoints the links whose issues are not finished.
//
// Only backlog, unstarted and started move: a completed or cancelled issue stays behind with the cycle it was finished in, and an issue with no state at all moves nowhere, because the filter is a join to states rather than a test on the column. A soft-deleted issue's link does move, since only the link's own deletion is checked.
func (handler *Handler) moveUnfinishedCycleIssues(c *gin.Context, slug, projectID, cycleID, newCycleID string) ([]map[string]string, error) {
	var rows []transferredIssue
	err := handler.db.WithContext(c.Request.Context()).Table("cycle_issues ci").
		Joins("JOIN workspaces w ON w.id = ci.workspace_id").
		Joins("JOIN issues i ON i.id = ci.issue_id").
		Joins("JOIN states s ON s.id = i.state_id").
		Where("ci.cycle_id = ? AND ci.project_id = ? AND w.slug = ? AND ci.deleted_at IS NULL", cycleID, projectID, slug).
		Where("i.archived_at IS NULL AND i.is_draft = FALSE AND s.group IN ?", []string{"backlog", "unstarted", "started"}).
		Select("ci.issue_id").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}

	issueIDs := make([]string, 0, len(rows))
	moved := make([]map[string]string, 0, len(rows))
	for _, row := range rows {
		issueIDs = append(issueIDs, row.IssueID)
		moved = append(moved, map[string]string{
			"old_cycle_id": cycleID, "new_cycle_id": newCycleID, "issue_id": row.IssueID,
		})
	}
	// Only the cycle moves. A bulk update writes the named column and nothing else, so the links keep the timestamps and the author they had.
	err = handler.db.WithContext(c.Request.Context()).Model(&CycleIssue{}).
		Where("cycle_id = ? AND project_id = ? AND issue_id IN ? AND deleted_at IS NULL", cycleID, projectID, issueIDs).
		Update("cycle_id", newCycleID).Error
	if err != nil {
		return nil, err
	}
	return moved, nil
}

// publishCycleTransferActivity sends the one activity the transfer produces. It names no issue: the move is about the cycle, and the task fans it out over the list it carries.
func (handler *Handler) publishCycleTransferActivity(c *gin.Context, user *auth.User, projectID, cycleID, newCycleID string, moved []map[string]string, now time.Time) error {
	if moved == nil {
		moved = []map[string]string{}
	}
	requested, err := json.Marshal(map[string]any{"cycles_list": []any{}})
	if err != nil {
		return err
	}
	current, err := json.Marshal(map[string]any{
		"updated_cycle_issues": moved,
		"created_cycle_issues": []any{},
	})
	if err != nil {
		return err
	}
	requestedData := string(requested)
	snapshot := string(current)
	return handler.publishIssueActivity(c, issueActivity{
		Type: "cycle.activity.created", RequestedData: &requestedData, CurrentInstance: &snapshot,
		ActorID: user.ID, ProjectID: projectID, Notification: true, Origin: handler.origin(), Epoch: now,
	})
}
