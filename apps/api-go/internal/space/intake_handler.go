package space

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"github.com/yldm-tech/pace/apps/api-go/internal/drf"
	"github.com/yldm-tech/pace/apps/api-go/internal/htmlsanitizer"
	"github.com/yldm-tech/pace/apps/api-go/internal/issuefilters"
	"gorm.io/gorm"
)

func (handler *Handler) registerIntakeRoutes(router gin.IRouter) {
	const base = "/api/public/anchor/:anchor/intakes/:intake/"
	// The queue is mounted twice under two names, and the older one serves only two of the five methods.
	router.GET(base+"intake-issues/", handler.authenticated(handler.intakeList))
	router.POST(base+"intake-issues/", handler.authenticated(handler.intakeCreate))
	router.GET(base+"intake-issues/:issue/", handler.authenticated(handler.intakeRetrieve))
	router.PATCH(base+"intake-issues/:issue/", handler.authenticated(handler.intakeUpdate))
	router.DELETE(base+"intake-issues/:issue/", handler.authenticated(handler.intakeDestroy))
	router.GET(base+"inbox-issues/", handler.authenticated(handler.intakeList))
	router.POST(base+"inbox-issues/", handler.authenticated(handler.intakeCreate))
}

// intakeBoard resolves the anchor and refuses a board with no intake, which is the first thing all five routes do.
func (handler *Handler) intakeBoard(c *gin.Context) (DeployBoard, bool) {
	board, found, err := handler.projectBoardByAnchor(c)
	if err != nil {
		handler.serverError(c, err)
		return DeployBoard{}, false
	}
	if !found || board.ProjectID == nil {
		notFound(c)
		return DeployBoard{}, false
	}
	if board.IntakeID == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Intake is not enabled for this Project Board"})
		return DeployBoard{}, false
	}
	return board, true
}

// intakeList reports the work items waiting in the queue.
//
// It reads through the **plain** manager rather than the work item one, so a draft or an archived work item in the queue is reported, and it is ordered by when each one is snoozed until and then by its status.
func (handler *Handler) intakeList(c *gin.Context, _ *auth.User) {
	board, ok := handler.intakeBoard(c)
	if !ok {
		return
	}
	filters := issuefilters.Parse(queryParams(c), "GET", "", handler.clock().UTC())
	joins, conditions, arguments, supported := issuefilters.SQL(filters)
	if !supported {
		// A lookup the ORM cannot resolve is a 500 rather than an empty list, which is what Django raises.
		handler.serverError(c, errUnsupportedFilter)
		return
	}
	query := handler.db.WithContext(c.Request.Context()).Table("issues i").
		Joins("JOIN intake_issues ii ON ii.issue_id = i.id AND ii.intake_id = ? AND ii.deleted_at IS NULL", c.Param("intake")).
		Where("i.workspace_id = ? AND i.project_id = ? AND i.deleted_at IS NULL",
			board.WorkspaceID, *board.ProjectID)
	for _, join := range joins {
		query = query.Joins(join)
	}
	// Each condition takes as many arguments as it has placeholders, which is how the list of both is handed back.
	consumed := 0
	for _, condition := range conditions {
		count := strings.Count(condition, "?")
		query = query.Where(condition, arguments[consumed:consumed+count]...)
		consumed += count
	}
	var rows []intakeIssueRow
	err := query.Select(intakeIssueSelection()).
		Order("ii.snoozed_till, ii.status").Scan(&rows).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	results := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		results = append(results, intakeIssueJSON(row))
	}
	drf.Respond(c, http.StatusOK, results)
}

var errUnsupportedFilter = &spaceError{"the filter names a lookup the ORM cannot resolve"}

type spaceError struct{ message string }

func (err *spaceError) Error() string { return err.message }

// intakeCreate raises a work item into the queue.
//
// It writes the work item **directly** rather than through the serializer, so it takes no sequence number, no sort order and no default assignee — the same shortcut the external API's intake create takes. The priority defaults to `low` when the payload names none, even though the check just above it treats an absent priority as `none`.
func (handler *Handler) intakeCreate(c *gin.Context, user *auth.User) {
	board, ok := handler.intakeBoard(c)
	if !ok {
		return
	}
	if c.Param("intake") != *board.IntakeID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Intake does not belong to this Project Board"})
		return
	}
	var request struct {
		Issue map[string]json.RawMessage `json:"issue"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Name is required"})
		return
	}
	name := ""
	if raw, given := request.Issue["name"]; given {
		_ = json.Unmarshal(raw, &name)
	}
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Name is required"})
		return
	}
	priority := "none"
	if raw, given := request.Issue["priority"]; given {
		_ = json.Unmarshal(raw, &priority)
	}
	if !validIntakePriority(priority) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid priority"})
		return
	}
	// The value written is not the value checked: an absent priority passes the check as "none" and is written as "low".
	written := "low"
	if raw, given := request.Issue["priority"]; given {
		var value string
		if json.Unmarshal(raw, &value) == nil && value != "" {
			written = value
		}
	}
	html := "<p></p>"
	if raw, given := request.Issue["description_html"]; given {
		var value string
		if json.Unmarshal(raw, &value) == nil {
			html = value
		}
	}
	// Only the cleaned text is kept, and a rejection leaves the empty paragraph.
	if _, _, cleaned := htmlsanitizer.ValidateHTMLContent(html); cleaned != nil {
		html = *cleaned
	} else {
		html = "<p></p>"
	}
	descriptionJSON := []byte(`{}`)
	if raw, given := request.Issue["description_json"]; given && json.Valid(raw) {
		descriptionJSON = append([]byte(nil), raw...)
	}

	now := handler.clock().UTC()
	triageID, err := handler.ensureTriageState(c, board, now)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	issueID, err := newUUID()
	if err != nil {
		handler.serverError(c, err)
		return
	}
	linkID, err := newUUID()
	if err != nil {
		handler.serverError(c, err)
		return
	}
	err = handler.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		// The number a work item is known by inside its project. It is NOT NULL and nothing hands one out, so it is taken the way every other issue takes it: one past the largest the project has issued.
		var largestSequence *int
		if err := tx.Table("issue_sequences").
			Where("project_id = ? AND deleted_at IS NULL", *board.ProjectID).
			Select("MAX(sequence)").Scan(&largestSequence).Error; err != nil {
			return err
		}
		sequence := 1
		if largestSequence != nil {
			sequence = *largestSequence + 1
		}
		err := tx.Table("issues").Create(map[string]any{
			"id": issueID, "created_at": now, "updated_at": now,
			"name": name, "description_json": auth.JSONValue(descriptionJSON), "description_html": html,
			"priority": written, "project_id": *board.ProjectID, "workspace_id": board.WorkspaceID,
			"state_id": triageID, "sort_order": 65535, "is_draft": false,
			"sequence_id": sequence,
		}).Error
		if err != nil {
			return err
		}
		source := "IN_APP"
		return tx.Table("intake_issues").Create(map[string]any{
			"id": linkID, "created_at": now, "updated_at": now,
			"created_by_id": user.ID, "updated_by_id": user.ID,
			"intake_id": c.Param("intake"), "project_id": *board.ProjectID,
			"workspace_id": board.WorkspaceID, "issue_id": issueID,
			"status": -2, "source": source, "extra": auth.JSONValue([]byte(`{}`)),
		}).Error
	})
	if err != nil {
		handler.serverError(c, err)
		return
	}
	rows, err := handler.intakeIssueRows(c, board, issueID)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if len(rows) == 0 {
		handler.serverError(c, errUnsupportedFilter)
		return
	}
	// The create answers 200 rather than 201, like the rest of this app.
	drf.Respond(c, http.StatusOK, intakeIssueJSON(rows[0]))
}

func (handler *Handler) intakeRetrieve(c *gin.Context, _ *auth.User) {
	board, ok := handler.intakeBoard(c)
	if !ok {
		return
	}
	link, found, err := handler.intakeLink(c, board)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !found {
		notFound(c)
		return
	}
	rows, err := handler.intakeIssueRows(c, board, link.IssueID)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if len(rows) == 0 {
		notFound(c)
		return
	}
	drf.Respond(c, http.StatusOK, intakeIssueJSON(rows[0]))
}

// intakeUpdate edits the work item behind a queue entry, and only one the caller raised themselves.
//
// Three fields are all it will take — the name, the html and the json — and the refusal for somebody else's entry is a **400** rather than a 403.
func (handler *Handler) intakeUpdate(c *gin.Context, user *auth.User) {
	board, ok := handler.intakeBoard(c)
	if !ok {
		return
	}
	link, found, err := handler.intakeLink(c, board)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !found {
		notFound(c)
		return
	}
	if link.CreatedByID == nil || *link.CreatedByID != user.ID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "You cannot edit intake issues"})
		return
	}
	var request struct {
		Issue map[string]json.RawMessage `json:"issue"`
	}
	_ = c.ShouldBindJSON(&request)
	updates := map[string]any{}
	if raw, given := request.Issue["name"]; given {
		var value string
		if json.Unmarshal(raw, &value) == nil {
			updates["name"] = value
		}
	}
	if raw, given := request.Issue["description_html"]; given {
		var value string
		if json.Unmarshal(raw, &value) == nil {
			if _, _, cleaned := htmlsanitizer.ValidateHTMLContent(value); cleaned != nil {
				value = *cleaned
			}
			updates["description_html"] = value
		}
	}
	if raw, given := request.Issue["description_json"]; given && json.Valid(raw) {
		updates["description_json"] = auth.JSONValue(append([]byte(nil), raw...))
	}
	if len(updates) > 0 {
		now := handler.clock().UTC()
		updates["updated_at"] = now
		err := handler.db.WithContext(c.Request.Context()).Table("issues").
			Where("id = ? AND project_id = ? AND workspace_id = ?",
				link.IssueID, *board.ProjectID, board.WorkspaceID).
			Updates(updates).Error
		if err != nil {
			handler.serverError(c, err)
			return
		}
	}
	rows, err := handler.intakeIssueRows(c, board, link.IssueID)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if len(rows) == 0 {
		notFound(c)
		return
	}
	drf.Respond(c, http.StatusOK, intakeIssueJSON(rows[0]))
}

// intakeDestroy removes a queue entry, and only one the caller raised. The work item itself stays.
func (handler *Handler) intakeDestroy(c *gin.Context, user *auth.User) {
	board, ok := handler.intakeBoard(c)
	if !ok {
		return
	}
	link, found, err := handler.intakeLink(c, board)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !found {
		notFound(c)
		return
	}
	if link.CreatedByID == nil || *link.CreatedByID != user.ID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "You cannot delete intake issue"})
		return
	}
	now := handler.clock().UTC()
	err = handler.db.WithContext(c.Request.Context()).Table("intake_issues").Where("id = ?", link.ID).
		Updates(map[string]any{"deleted_at": now, "updated_at": now}).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// ensureTriageState finds the project's triage state and makes one when there is none, which is what lets a board accept its first submission.
func (handler *Handler) ensureTriageState(c *gin.Context, board DeployBoard, now time.Time) (string, error) {
	var identifiers []string
	err := handler.db.WithContext(c.Request.Context()).Table("states").
		Where("project_id = ? AND workspace_id = ? AND is_triage = TRUE AND deleted_at IS NULL",
			*board.ProjectID, board.WorkspaceID).
		Order("created_at").Limit(1).Pluck("id", &identifiers).Error
	if err != nil {
		return "", err
	}
	if len(identifiers) > 0 {
		return identifiers[0], nil
	}
	stateID, err := newUUID()
	if err != nil {
		return "", err
	}
	err = handler.db.WithContext(c.Request.Context()).Table("states").Create(map[string]any{
		"id": stateID, "created_at": now, "updated_at": now,
		"name": "Triage", "group": "triage", "project_id": *board.ProjectID,
		"workspace_id": board.WorkspaceID, "color": "#4E5355", "sequence": 65000,
		"default": false, "is_triage": true,
		// Both NOT NULL, and both an empty string when Django is not given one.
		"description": "", "slug": "",
	}).Error
	if err != nil {
		return "", err
	}
	return stateID, nil
}

// IntakeLink is the queue entry itself.
type IntakeLink struct {
	ID          string     `gorm:"column:id;type:uuid;primaryKey"`
	CreatedByID *string    `gorm:"column:created_by_id;type:uuid"`
	IssueID     string     `gorm:"column:issue_id;type:uuid"`
	Status      int        `gorm:"column:status"`
	SnoozedTill *time.Time `gorm:"column:snoozed_till"`
	Source      *string    `gorm:"column:source"`
	DuplicateTo *string    `gorm:"column:duplicate_to_id;type:uuid"`
}

func (handler *Handler) intakeLink(c *gin.Context, board DeployBoard) (IntakeLink, bool, error) {
	var links []IntakeLink
	err := handler.db.WithContext(c.Request.Context()).Table("intake_issues").
		Select("id, created_by_id, issue_id, status, snoozed_till, source, duplicate_to_id").
		Where("id = ? AND intake_id = ? AND project_id = ? AND workspace_id = ? AND deleted_at IS NULL",
			c.Param("issue"), c.Param("intake"), *board.ProjectID, board.WorkspaceID).
		Limit(1).Scan(&links).Error
	if err != nil || len(links) == 0 {
		return IntakeLink{}, false, err
	}
	return links[0], true, nil
}

// intakeIssueRow is the work item as the queue reports one, with the entry's own fields beside it.
type intakeIssueRow struct {
	ID              string     `gorm:"column:id"`
	Name            string     `gorm:"column:name"`
	DescriptionHTML string     `gorm:"column:description_html"`
	Priority        string     `gorm:"column:priority"`
	StateID         *string    `gorm:"column:state_id"`
	StateGroup      *string    `gorm:"column:state_group"`
	SequenceID      int64      `gorm:"column:sequence_id"`
	SortOrder       float64    `gorm:"column:sort_order"`
	ProjectID       string     `gorm:"column:project_id"`
	CreatedAt       time.Time  `gorm:"column:created_at"`
	UpdatedAt       time.Time  `gorm:"column:updated_at"`
	CreatedByID     *string    `gorm:"column:created_by_id"`
	BridgeID        *string    `gorm:"column:bridge_id"`
	Status          *int       `gorm:"column:status"`
	SnoozedTill     *time.Time `gorm:"column:snoozed_till"`
	DuplicateTo     *string    `gorm:"column:duplicate_to_id"`
	Source          *string    `gorm:"column:source"`
	SubIssuesCount  int64      `gorm:"column:sub_issues_count"`
}

func intakeIssueSelection() string {
	return `i.id, i.name, i.description_html, i.priority, i.state_id, i.sequence_id, i.sort_order,
		i.project_id, i.created_at, i.updated_at, i.created_by_id,
		ii.id AS bridge_id, ii.status, ii.snoozed_till, ii.duplicate_to_id, ii.source,
		(SELECT s.group FROM states s WHERE s.id = i.state_id) AS state_group,
		(SELECT COUNT(*) FROM issues si WHERE si.parent_id = i.id AND si.deleted_at IS NULL
			AND si.archived_at IS NULL AND si.is_draft = FALSE) AS sub_issues_count`
}

func (handler *Handler) intakeIssueRows(c *gin.Context, board DeployBoard, issueID string) ([]intakeIssueRow, error) {
	var rows []intakeIssueRow
	err := handler.db.WithContext(c.Request.Context()).Table("issues i").
		Joins("JOIN intake_issues ii ON ii.issue_id = i.id AND ii.intake_id = ? AND ii.deleted_at IS NULL", c.Param("intake")).
		Where("i.id = ? AND i.workspace_id = ? AND i.project_id = ? AND i.deleted_at IS NULL",
			issueID, board.WorkspaceID, *board.ProjectID).
		Select(intakeIssueSelection()).Limit(1).Scan(&rows).Error
	return rows, err
}

// intakeIssueJSON is IssueStateIntakeSerializer: the work item with its queue entry folded in.
func intakeIssueJSON(row intakeIssueRow) gin.H {
	return gin.H{
		"id": row.ID, "name": row.Name, "description_html": row.DescriptionHTML,
		"priority": row.Priority, "state_id": row.StateID, "state_detail": gin.H{"group": row.StateGroup},
		"sequence_id": row.SequenceID, "sort_order": row.SortOrder, "project_id": row.ProjectID,
		"created_at": row.CreatedAt, "updated_at": row.UpdatedAt, "created_by": row.CreatedByID,
		"bridge_id": row.BridgeID, "issue_intake": []gin.H{{
			"id": row.BridgeID, "status": row.Status, "snoozed_till": row.SnoozedTill,
			"duplicate_to": row.DuplicateTo, "source": row.Source,
		}},
		"sub_issues_count": row.SubIssuesCount,
	}
}

// validIntakePriority is the five the check accepts, which is not the same list the model allows.
func validIntakePriority(value string) bool {
	switch value {
	case "low", "medium", "high", "urgent", "none":
		return true
	}
	return false
}

// queryParams is the query string as a map, which is what the filter parser reads.
func queryParams(c *gin.Context) map[string]string {
	params := map[string]string{}
	for key, values := range c.Request.URL.Query() {
		if len(values) > 0 {
			params[key] = values[0]
		}
	}
	return params
}
