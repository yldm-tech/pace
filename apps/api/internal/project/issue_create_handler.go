package project

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api/internal/auth"
	"github.com/yldm-tech/pace/apps/api/internal/drf"
	"github.com/yldm-tech/pace/apps/api/internal/issues"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (handler *Handler) registerIssueCreateRoutes(router gin.IRouter) {
	router.POST("/api/workspaces/:slug/projects/:id/issues/", handler.authenticated(handler.issueCreate))
}

// IssueSequence records the number an issue was given within its project, which is what makes the human-facing key stable even after an issue is deleted.
type IssueSequence struct {
	ID          string     `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt   time.Time  `gorm:"column:created_at"`
	UpdatedAt   time.Time  `gorm:"column:updated_at"`
	CreatedByID *string    `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID *string    `gorm:"column:updated_by_id;type:uuid"`
	DeletedAt   *time.Time `gorm:"column:deleted_at"`
	ProjectID   string     `gorm:"column:project_id;type:uuid"`
	WorkspaceID string     `gorm:"column:workspace_id;type:uuid"`
	IssueID     *string    `gorm:"column:issue_id;type:uuid"`
	Sequence    int64      `gorm:"column:sequence"`
	Deleted     bool       `gorm:"column:deleted"`
}

func (IssueSequence) TableName() string { return "issue_sequences" }

// issueCreate makes an issue. Most of the work is in Issue.save's adding path, which hands out the sequence number under a lock and derives the sort order from the issues already in the chosen state.
func (handler *Handler) issueCreate(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember) {
		return
	}
	slug, projectID := c.Param("slug"), c.Param("id")
	var project Project
	err := handler.db.WithContext(c.Request.Context()).Where("id = ?", projectID).Take(&project).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "The required object does not exist."})
		return
	}
	if err != nil {
		handler.internalError(c, err)
		return
	}

	var body map[string]json.RawMessage
	if err := c.ShouldBindJSON(&body); err != nil {
		handler.invalidDetail(c)
		return
	}
	fields, ok := handler.issueFields(c, body, projectID)
	if !ok {
		return
	}
	name, hasName := fields.values["name"]
	if !hasName || name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"name": []string{"This field is required."}})
		return
	}

	now := handler.clock().UTC()
	issueID, err := newUUID()
	if err != nil {
		handler.internalError(c, err)
		return
	}

	assignees := fields.assigneeIDs
	if !fields.hasAssignees || len(assignees) == 0 {
		// With no assignees of its own the issue goes to the project's default, but only while they are still a member who could have been assigned it.
		assignees, err = handler.defaultAssignee(c.Request.Context(), project, projectID)
		if err != nil {
			handler.internalError(c, err)
			return
		}
	}

	err = handler.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		return handler.writeNewIssue(tx, issueID, projectID, project, user.ID, now, fields, assignees)
	})
	if err != nil {
		handler.internalError(c, err)
		return
	}

	requested, err := json.Marshal(decodeRawFields(body))
	if err != nil {
		handler.internalError(c, err)
		return
	}
	requestedData := string(requested)
	err = handler.publishIssueActivity(c, issueActivity{
		Type: "issue.activity.created", RequestedData: &requestedData,
		ActorID: user.ID, IssueID: issueID, ProjectID: projectID,
		Notification: true, Origin: handler.origin(), Epoch: now,
	})
	if err != nil {
		handler.internalError(c, err)
		return
	}

	location, err := userLocation(user)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	rows, err := handler.issueCreateResponseRow(c, slug, projectID, issueID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if len(rows) == 0 {
		handler.internalError(c, errors.New("issue create: the issue could not be read back"))
		return
	}
	drf.Respond(c, http.StatusCreated, issueCreateJSON(rows[0], location))
}

// writeNewIssue is Issue.save's adding path, plus the related sets the serializer writes after it.
//
// The save path itself lives in internal/issues, because the external API creates work items through the same model and the two have to hand out the same sequence numbers while they run together.
func (handler *Handler) writeNewIssue(tx *gorm.DB, issueID, projectID string, project Project, actorID string, now time.Time, fields issueInput, assignees []string) error {
	values := map[string]any{}
	for key, value := range fields.values {
		values[key] = value
	}
	values["id"] = issueID
	if _, given := values["description_json"]; !given {
		values["description_json"] = auth.JSONValue("{}")
	}
	sequence, err := issues.PrepareCreate(tx, values, projectID, project.WorkspaceID, actorID, now)
	if err != nil {
		return err
	}

	if err := tx.Table("issues").Create(values).Error; err != nil {
		return err
	}

	sequenceID, err := newUUID()
	if err != nil {
		return err
	}
	err = tx.Create(&IssueSequence{
		ID: sequenceID, CreatedAt: now, UpdatedAt: now,
		ProjectID: projectID, WorkspaceID: project.WorkspaceID,
		IssueID: &issueID, Sequence: sequence,
	}).Error
	if err != nil {
		return err
	}

	// Both related sets ignore a conflict rather than failing the create, which is what the serializer's try/except around bulk_create does.
	if err := insertIssueAssignees(tx, issueID, projectID, project.WorkspaceID, actorID, now, assignees); err != nil {
		return err
	}
	return insertIssueLabels(tx, issueID, projectID, project.WorkspaceID, actorID, now, fields.labelIDs)
}

func insertIssueAssignees(tx *gorm.DB, issueID, projectID, workspaceID, actorID string, now time.Time, assignees []string) error {
	rows := make([]IssueAssignee, 0, len(assignees))
	for _, assigneeID := range assignees {
		rowID, err := newUUID()
		if err != nil {
			return err
		}
		rows = append(rows, IssueAssignee{
			ID: rowID, CreatedAt: now, UpdatedAt: now, CreatedByID: &actorID, UpdatedByID: &actorID,
			ProjectID: projectID, WorkspaceID: workspaceID, IssueID: issueID, AssigneeID: assigneeID,
		})
	}
	if len(rows) == 0 {
		return nil
	}
	return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&rows).Error
}

func insertIssueLabels(tx *gorm.DB, issueID, projectID, workspaceID, actorID string, now time.Time, labels []string) error {
	rows := make([]IssueLabel, 0, len(labels))
	for _, labelID := range labels {
		rowID, err := newUUID()
		if err != nil {
			return err
		}
		rows = append(rows, IssueLabel{
			ID: rowID, CreatedAt: now, UpdatedAt: now, CreatedByID: &actorID, UpdatedByID: &actorID,
			ProjectID: projectID, WorkspaceID: workspaceID, IssueID: issueID, LabelID: labelID,
		})
	}
	if len(rows) == 0 {
		return nil
	}
	return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&rows).Error
}

// defaultAssignee returns the project's default assignee, but only while they are still an active member at member level or above — the same floor the assignee field itself enforces.
func (handler *Handler) defaultAssignee(ctx context.Context, project Project, projectID string) ([]string, error) {
	if project.DefaultAssigneeID == nil {
		return nil, nil
	}
	var count int64
	err := handler.db.WithContext(ctx).Table("project_members").
		Where("member_id = ? AND project_id = ? AND role >= ? AND is_active = TRUE AND deleted_at IS NULL",
			*project.DefaultAssigneeID, projectID, roleMemberOrAbove).Count(&count).Error
	if err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, nil
	}
	return []string{*project.DefaultAssigneeID}, nil
}

// issueCreateResponseRow reads the issue back through the annotated queryset the response uses.
func (handler *Handler) issueCreateResponseRow(c *gin.Context, slug, projectID, issueID string) ([]issueListRow, error) {
	var rows []issueListRow
	err := handler.db.WithContext(c.Request.Context()).Table("issues i").
		Select(issueListAnnotations()).
		Joins("JOIN workspaces w ON w.id = i.workspace_id").
		Where("w.slug = ? AND i.project_id = ? AND i.id = ?", slug, projectID, issueID).
		Where(issueObjectsPredicate("i")).
		Group("i.id").Limit(1).Scan(&rows).Error
	return rows, err
}

// issueCreateJSON is the create response: the list projection plus deleted_at and without state__group, with the two audit timestamps in the caller's timezone.
func issueCreateJSON(row issueListRow, location *time.Location) gin.H {
	data := issueListRowJSON(row)
	delete(data, "state__group")
	data["deleted_at"] = row.DeletedAt
	data["created_at"] = row.CreatedAt.In(location)
	data["updated_at"] = row.UpdatedAt.In(location)
	return data
}
