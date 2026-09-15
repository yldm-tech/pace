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
	"gorm.io/gorm/clause"
)

func (handler *Handler) registerCycleIssueRoutes(router gin.IRouter) {
	router.POST("/api/workspaces/:slug/projects/:id/cycles/:cycle/cycle-issues/", handler.authenticated(handler.cycleIssueCreate))
	router.POST("/api/workspaces/:slug/projects/:id/cycles/:cycle/archive/", handler.authenticated(handler.cycleArchive))
	router.DELETE("/api/workspaces/:slug/projects/:id/cycles/:cycle/archive/", handler.authenticated(handler.cycleUnarchive))
}

// CycleIssue is the db.CycleIssue table, the link between a cycle and an issue.
type CycleIssue struct {
	ID          string     `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt   time.Time  `gorm:"column:created_at"`
	UpdatedAt   time.Time  `gorm:"column:updated_at"`
	CreatedByID *string    `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID *string    `gorm:"column:updated_by_id;type:uuid"`
	DeletedAt   *time.Time `gorm:"column:deleted_at"`
	ProjectID   string     `gorm:"column:project_id;type:uuid"`
	WorkspaceID string     `gorm:"column:workspace_id;type:uuid"`
	CycleID     string     `gorm:"column:cycle_id;type:uuid"`
	IssueID     string     `gorm:"column:issue_id;type:uuid"`
}

func (CycleIssue) TableName() string { return "cycle_issues" }

// cycleIssueCreate adds issues to a cycle. An issue already in another cycle is moved rather than duplicated, since an issue belongs to at most one cycle at a time.
func (handler *Handler) cycleIssueCreate(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember) {
		return
	}
	slug, projectID, cycleID := c.Param("slug"), c.Param("id"), c.Param("cycle")
	var request struct {
		Issues []string `json:"issues"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		handler.invalidDetail(c)
		return
	}
	if len(request.Issues) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Issues are required"})
		return
	}

	cycle, found, err := handler.cycleByID(c, slug, projectID, cycleID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "The required object does not exist."})
		return
	}
	now := handler.clock().UTC()
	if cycle.EndDate != nil && cycle.EndDate.Before(now) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "The Cycle has already been completed so no new issues can be added"})
		return
	}

	// The existing links are scoped to the workspace and project, which is what stops a foreign link from being reassigned into this cycle (GHSA-4w5x-wc9w-f47x).
	var moving []CycleIssue
	err = handler.db.WithContext(c.Request.Context()).Table("cycle_issues ci").
		Joins("JOIN workspaces w ON w.id = ci.workspace_id").
		Where("ci.cycle_id <> ? AND ci.issue_id IN ? AND w.slug = ? AND ci.project_id = ? AND ci.deleted_at IS NULL",
			cycleID, request.Issues, slug, projectID).
		Find(&moving).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	alreadyLinked := map[string]bool{}
	for _, link := range moving {
		alreadyLinked[link.IssueID] = true
	}
	candidates := make([]string, 0, len(request.Issues))
	for _, issueID := range request.Issues {
		if !alreadyLinked[issueID] {
			candidates = append(candidates, issueID)
		}
	}

	// The ids that will get a new link are narrowed to this project as well.
	fresh := []string{}
	if len(candidates) > 0 {
		err := handler.db.WithContext(c.Request.Context()).Table("issues i").
			Joins("JOIN workspaces w ON w.id = i.workspace_id").
			Where("i.id IN ? AND w.slug = ? AND i.project_id = ?", candidates, slug, projectID).
			Where(issueObjectsPredicate("i")).Pluck("i.id", &fresh).Error
		if err != nil {
			handler.internalError(c, err)
			return
		}
	}

	created := make([]CycleIssue, 0, len(fresh))
	for _, issueID := range fresh {
		linkID, err := newUUID()
		if err != nil {
			handler.internalError(c, err)
			return
		}
		created = append(created, CycleIssue{
			ID: linkID, CreatedAt: now, UpdatedAt: now,
			CreatedByID: &user.ID, UpdatedByID: &user.ID,
			ProjectID: projectID, WorkspaceID: cycle.WorkspaceID,
			CycleID: cycleID, IssueID: issueID,
		})
	}

	err = handler.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if len(created) > 0 {
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&created).Error; err != nil {
				return err
			}
		}
		for _, link := range moving {
			// bulk_update writes only the cycle column, so no timestamp moves.
			if err := tx.Model(&CycleIssue{}).Where("id = ?", link.ID).Update("cycle_id", cycleID).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		handler.internalError(c, err)
		return
	}

	if err := handler.publishCycleIssueActivity(c, user, request.Issues, cycleID, projectID, moving, created, now); err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusCreated, gin.H{"message": "success"})
}

// publishCycleIssueActivity sends what the task reads. created_cycle_issues is a JSON string inside the snapshot rather than a nested object, because Django builds it with serializers.serialize and then dumps the whole snapshot around it.
func (handler *Handler) publishCycleIssueActivity(c *gin.Context, user *auth.User, requestedIssues []string, cycleID, projectID string, moving, created []CycleIssue, now time.Time) error {
	updated := make([]map[string]any, 0, len(moving))
	for _, link := range moving {
		updated = append(updated, map[string]any{
			"old_cycle_id": link.CycleID, "new_cycle_id": cycleID, "issue_id": link.IssueID,
		})
	}
	records := make([]map[string]any, 0, len(created))
	for _, link := range created {
		// Django's serialize format: the model label, the primary key, and every other column under fields.
		records = append(records, map[string]any{
			"model": "db.cycleissue",
			"pk":    link.ID,
			"fields": map[string]any{
				"created_at": link.CreatedAt, "updated_at": link.UpdatedAt,
				"created_by": link.CreatedByID, "updated_by": link.UpdatedByID,
				"project": link.ProjectID, "workspace": link.WorkspaceID,
				"cycle": link.CycleID, "issue": link.IssueID,
			},
		})
	}
	serialized, err := json.Marshal(records)
	if err != nil {
		return err
	}
	requested, err := json.Marshal(map[string]any{"cycles_list": requestedIssues})
	if err != nil {
		return err
	}
	snapshot, err := json.Marshal(map[string]any{
		"updated_cycle_issues": updated,
		// A string, not an object: the task calls json.loads on it.
		"created_cycle_issues": string(serialized),
	})
	if err != nil {
		return err
	}
	requestedData, currentInstance := string(requested), string(snapshot)
	return handler.publishIssueActivity(c, issueActivity{
		Type: "cycle.activity.created", RequestedData: &requestedData, CurrentInstance: &currentInstance,
		ActorID: user.ID, ProjectID: projectID,
		Notification: true, Origin: handler.origin(), Epoch: now,
	})
}

// cycleIssueDestroy takes an issue out of a cycle. The activity is sent before the link goes, since the task reads the cycle by the id the request named rather than from the link.
func (handler *Handler) cycleIssueDestroy(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember) {
		return
	}
	slug, projectID, cycleID, issueID := c.Param("slug"), c.Param("id"), c.Param("cycle"), c.Param("issue")
	now := handler.clock().UTC()
	requested, err := json.Marshal(map[string]any{"cycle_id": cycleID, "issues": []string{issueID}})
	if err != nil {
		handler.internalError(c, err)
		return
	}
	requestedData := string(requested)
	err = handler.publishIssueActivity(c, issueActivity{
		Type: "cycle.activity.deleted", RequestedData: &requestedData,
		ActorID: user.ID, IssueID: issueID, ProjectID: projectID,
		Notification: true, Origin: handler.origin(), Epoch: now,
	})
	if err != nil {
		handler.internalError(c, err)
		return
	}
	// A queryset delete, so it answers 204 whether or not the link was there.
	err = handler.db.WithContext(c.Request.Context()).Model(&CycleIssue{}).
		Where(`issue_id = ? AND cycle_id = ? AND project_id = ? AND deleted_at IS NULL
			AND workspace_id = (SELECT id FROM workspaces WHERE slug = ?)`,
			issueID, cycleID, projectID, slug).
		Update("deleted_at", now).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// cycleArchive puts a completed cycle away. Only a completed one may be archived, and the favourite goes with it.
func (handler *Handler) cycleArchive(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember) {
		return
	}
	slug, projectID, cycleID := c.Param("slug"), c.Param("id"), c.Param("cycle")
	cycle, found, err := handler.cycleByID(c, slug, projectID, cycleID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "The required object does not exist."})
		return
	}
	now := handler.clock().UTC()
	if cycle.EndDate == nil || !cycle.EndDate.Before(now) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Only completed cycles can be archived"})
		return
	}
	err = handler.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		// save writes the whole instance, so the timestamp moves.
		err := tx.Model(&Cycle{}).Where("id = ?", cycleID).
			Updates(map[string]any{"archived_at": now, "updated_at": now}).Error
		if err != nil {
			return err
		}
		// Every member's favourite goes, not only the caller's.
		return tx.Model(&UserFavorite{}).
			Where(`entity_type = 'cycle' AND entity_identifier = ? AND project_id = ? AND deleted_at IS NULL
				AND workspace_id = (SELECT id FROM workspaces WHERE slug = ?)`, cycleID, projectID, slug).
			Update("deleted_at", now).Error
	})
	if err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, gin.H{"archived_at": now})
}

func (handler *Handler) cycleUnarchive(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember) {
		return
	}
	slug, projectID, cycleID := c.Param("slug"), c.Param("id"), c.Param("cycle")
	_, found, err := handler.cycleByID(c, slug, projectID, cycleID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "The required object does not exist."})
		return
	}
	now := handler.clock().UTC()
	// A typed nil would be skipped, so the column is cleared with an untyped one.
	var cleared any
	err = handler.db.WithContext(c.Request.Context()).Model(&Cycle{}).Where("id = ?", cycleID).
		Updates(map[string]any{"archived_at": cleared, "updated_at": now}).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (handler *Handler) cycleByID(c *gin.Context, slug, projectID, cycleID string) (Cycle, bool, error) {
	var cycle Cycle
	err := handler.db.WithContext(c.Request.Context()).Table("cycles c").
		Joins("JOIN workspaces w ON w.id = c.workspace_id").
		Where("c.id = ? AND c.project_id = ? AND w.slug = ? AND c.deleted_at IS NULL", cycleID, projectID, slug).
		Take(&cycle).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Cycle{}, false, nil
	}
	if err != nil {
		return Cycle{}, false, err
	}
	return cycle, true, nil
}
