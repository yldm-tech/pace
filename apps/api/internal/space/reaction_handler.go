package space

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api/internal/auth"
	"github.com/yldm-tech/pace/apps/api/internal/drf"
	"gorm.io/gorm"
)

func (handler *Handler) registerReactionRoutes(router gin.IRouter) {
	const base = "/api/public/anchor/:anchor/"
	router.GET(base+"issues/:issue/votes/", handler.authenticated(handler.voteList))
	router.POST(base+"issues/:issue/votes/", handler.authenticated(handler.voteCreate))
	router.DELETE(base+"issues/:issue/votes/", handler.authenticated(handler.voteDestroy))
	router.GET(base+"issues/:issue/reactions/", handler.authenticated(handler.issueReactionList))
	router.POST(base+"issues/:issue/reactions/", handler.authenticated(handler.issueReactionCreate))
	router.DELETE(base+"issues/:issue/reactions/:reaction/", handler.authenticated(handler.issueReactionDestroy))
	router.GET(base+"comments/:comment/reactions/", handler.authenticated(handler.commentReactionList))
	router.POST(base+"comments/:comment/reactions/", handler.authenticated(handler.commentReactionCreate))
	router.DELETE(base+"comments/:comment/reactions/:reaction/", handler.authenticated(handler.commentReactionDestroy))
}

// IssueVote is one reader's opinion of a work item, which is an up or a down rather than a count.
type IssueVote struct {
	ID          string     `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt   time.Time  `gorm:"column:created_at"`
	UpdatedAt   time.Time  `gorm:"column:updated_at"`
	CreatedByID *string    `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID *string    `gorm:"column:updated_by_id;type:uuid"`
	DeletedAt   *time.Time `gorm:"column:deleted_at"`
	ProjectID   string     `gorm:"column:project_id;type:uuid"`
	WorkspaceID string     `gorm:"column:workspace_id;type:uuid"`
	IssueID     string     `gorm:"column:issue_id;type:uuid"`
	ActorID     string     `gorm:"column:actor_id;type:uuid"`
	Vote        int        `gorm:"column:vote"`
}

func (IssueVote) TableName() string { return "issue_votes" }

// Reaction is one emoji on a work item or on a comment; the two live in different tables with the same shape.
type Reaction struct {
	ID          string     `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt   time.Time  `gorm:"column:created_at"`
	UpdatedAt   time.Time  `gorm:"column:updated_at"`
	CreatedByID *string    `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID *string    `gorm:"column:updated_by_id;type:uuid"`
	DeletedAt   *time.Time `gorm:"column:deleted_at"`
	ProjectID   string     `gorm:"column:project_id;type:uuid"`
	WorkspaceID string     `gorm:"column:workspace_id;type:uuid"`
	IssueID     *string    `gorm:"column:issue_id;type:uuid"`
	CommentID   *string    `gorm:"column:comment_id;type:uuid"`
	ActorID     string     `gorm:"column:actor_id;type:uuid"`
	Reaction    string     `gorm:"column:reaction"`
}

// voteList is always empty.
//
// Its queryset looks the board up by **workspace slug** and is handed the anchor, so the lookup never matches and the empty list is what a reader gets however many votes a work item has. Reproduced rather than corrected: a list that starts returning rows is a change no client asked for, and the board reads its votes off the work item itself.
func (handler *Handler) voteList(c *gin.Context, _ *auth.User) {
	drf.Respond(c, http.StatusOK, []gin.H{})
}

// voteCreate records a reader's vote, or moves the one they already cast. The vote itself defaults to **up** when the payload names none.
func (handler *Handler) voteCreate(c *gin.Context, user *auth.User) {
	board, ok := handler.boardForWriting(c, func(board DeployBoard) bool { return board.IsVotesEnabled },
		"Votes are not enabled for this project board")
	if !ok {
		return
	}
	if !handler.issueInBoard(c, board) {
		return
	}
	var payload map[string]json.RawMessage
	_ = c.ShouldBindJSON(&payload)
	vote := 1
	if raw, given := payload["vote"]; given {
		if json.Unmarshal(raw, &vote) != nil {
			vote = 1
		}
	}

	now := handler.clock().UTC()
	var existing []IssueVote
	err := handler.db.WithContext(c.Request.Context()).Table("issue_votes").
		Where("actor_id = ? AND project_id = ? AND issue_id = ? AND deleted_at IS NULL",
			user.ID, *board.ProjectID, c.Param("issue")).
		Limit(1).Scan(&existing).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	row := IssueVote{
		ProjectID: *board.ProjectID, WorkspaceID: board.WorkspaceID,
		IssueID: c.Param("issue"), ActorID: user.ID, Vote: vote,
		CreatedAt: now, UpdatedAt: now,
	}
	if len(existing) > 0 {
		row = existing[0]
		row.Vote = vote
		row.UpdatedAt = now
	}
	err = handler.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if len(existing) > 0 {
			return tx.Table("issue_votes").Where("id = ?", row.ID).
				Updates(map[string]any{"vote": vote, "updated_at": now}).Error
		}
		identifier, err := newUUID()
		if err != nil {
			return err
		}
		row.ID = identifier
		if err := tx.Table("issue_votes").Create(map[string]any{
			"id": row.ID, "created_at": now, "updated_at": now,
			"project_id": row.ProjectID, "workspace_id": row.WorkspaceID,
			"issue_id": row.IssueID, "actor_id": row.ActorID, "vote": vote,
		}).Error; err != nil {
			return err
		}
		return handler.rememberPublicMember(tx, board, user.ID, now)
	})
	if err != nil {
		handler.serverError(c, err)
		return
	}
	drf.Respond(c, http.StatusCreated, voteJSON(row))
}

// voteDestroy takes a reader's vote back. It asks nothing about whether votes are enabled, so a vote cast before the board was closed can still be withdrawn.
func (handler *Handler) voteDestroy(c *gin.Context, user *auth.User) {
	board, found, err := handler.projectBoardByAnchor(c)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !found || board.ProjectID == nil {
		notFound(c)
		return
	}
	var votes []IssueVote
	err = handler.db.WithContext(c.Request.Context()).Table("issue_votes").
		Where("issue_id = ? AND actor_id = ? AND project_id = ? AND workspace_id = ? AND deleted_at IS NULL",
			c.Param("issue"), user.ID, *board.ProjectID, board.WorkspaceID).
		Limit(1).Scan(&votes).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if len(votes) == 0 {
		notFound(c)
		return
	}
	now := handler.clock().UTC()
	err = handler.db.WithContext(c.Request.Context()).Table("issue_votes").Where("id = ?", votes[0].ID).
		Updates(map[string]any{"deleted_at": now, "updated_at": now}).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// issueReactionList is empty for the same reason the vote list is: its queryset reads two url parameters this route does not carry, so the board lookup never matches.
func (handler *Handler) issueReactionList(c *gin.Context, _ *auth.User) {
	drf.Respond(c, http.StatusOK, []gin.H{})
}

func (handler *Handler) issueReactionCreate(c *gin.Context, user *auth.User) {
	board, ok := handler.boardForWriting(c, func(board DeployBoard) bool { return board.IsReactionsEnabled },
		"Reactions are not enabled for this project board")
	if !ok {
		return
	}
	if !handler.issueInBoard(c, board) {
		return
	}
	reaction, ok := reactionFromPayload(c)
	if !ok {
		return
	}
	issueID := c.Param("issue")
	row, err := handler.writeReaction(c, board, user, "issue_reactions", map[string]any{"issue_id": issueID}, reaction)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	row.IssueID = &issueID
	drf.Respond(c, http.StatusCreated, reactionJSON(row))
}

func (handler *Handler) issueReactionDestroy(c *gin.Context, user *auth.User) {
	board, ok := handler.boardForWriting(c, func(board DeployBoard) bool { return board.IsReactionsEnabled },
		"Reactions are not enabled for this project board")
	if !ok {
		return
	}
	handler.destroyReaction(c, board, user, "issue_reactions",
		"issue_id = ? AND reaction = ? AND actor_id = ?", c.Param("issue"))
}

// commentReactionList is empty for a third reason: its queryset reads the anchor properly but the route never reaches the rows, because a public board only ever shows reactions on an **external** comment and the list is filtered to those after a lookup that already failed.
func (handler *Handler) commentReactionList(c *gin.Context, _ *auth.User) {
	board, found, err := handler.projectBoardByAnchor(c)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !found || !board.IsReactionsEnabled || board.ProjectID == nil {
		drf.Respond(c, http.StatusOK, []gin.H{})
		return
	}
	var rows []Reaction
	err = handler.db.WithContext(c.Request.Context()).Table("comment_reactions cr").Select("cr.*").
		Joins("JOIN issue_comments ic ON ic.id = cr.comment_id AND ic.access = 'EXTERNAL'").
		Where("cr.workspace_id = ? AND cr.project_id = ? AND cr.comment_id = ? AND cr.deleted_at IS NULL",
			board.WorkspaceID, *board.ProjectID, c.Param("comment")).
		Order("cr.created_at DESC").Scan(&rows).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	results := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		results = append(results, reactionJSON(row))
	}
	drf.Respond(c, http.StatusOK, results)
}

func (handler *Handler) commentReactionCreate(c *gin.Context, user *auth.User) {
	board, ok := handler.boardForWriting(c, func(board DeployBoard) bool { return board.IsReactionsEnabled },
		"Reactions are not enabled for this board")
	if !ok {
		return
	}
	// Only a comment the board shows may be reacted to, which means an external one.
	var count int64
	err := handler.db.WithContext(c.Request.Context()).Table("issue_comments").
		Where("id = ? AND project_id = ? AND workspace_id = ? AND access = 'EXTERNAL' AND deleted_at IS NULL",
			c.Param("comment"), *board.ProjectID, board.WorkspaceID).Count(&count).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if count == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Comment not found"})
		return
	}
	reaction, ok := reactionFromPayload(c)
	if !ok {
		return
	}
	commentID := c.Param("comment")
	row, err := handler.writeReaction(c, board, user, "comment_reactions", map[string]any{"comment_id": commentID}, reaction)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	row.CommentID = &commentID
	drf.Respond(c, http.StatusCreated, reactionJSON(row))
}

func (handler *Handler) commentReactionDestroy(c *gin.Context, user *auth.User) {
	board, ok := handler.boardForWriting(c, func(board DeployBoard) bool { return board.IsReactionsEnabled },
		"Reactions are not enabled for this board")
	if !ok {
		return
	}
	handler.destroyReaction(c, board, user, "comment_reactions",
		"comment_id = ? AND reaction = ? AND actor_id = ?", c.Param("comment"))
}

// boardForWriting resolves the anchor and asks whether the board allows what is being written.
func (handler *Handler) boardForWriting(c *gin.Context, allowed func(DeployBoard) bool, refusal string) (DeployBoard, bool) {
	board, found, err := handler.projectBoardByAnchor(c)
	if err != nil {
		handler.serverError(c, err)
		return DeployBoard{}, false
	}
	if !found || board.ProjectID == nil {
		notFound(c)
		return DeployBoard{}, false
	}
	if !allowed(board) {
		c.JSON(http.StatusBadRequest, gin.H{"error": refusal})
		return DeployBoard{}, false
	}
	return board, true
}

// issueInBoard binds the work item in the url to the board, through the manager the board itself reads — so nothing hidden from the board can be written on.
func (handler *Handler) issueInBoard(c *gin.Context, board DeployBoard) bool {
	var count int64
	err := handler.db.WithContext(c.Request.Context()).Table("issues i").
		Joins("JOIN projects p ON p.id = i.project_id").
		Joins("LEFT JOIN states s ON s.id = i.state_id").
		Where("i.id = ? AND i.project_id = ? AND i.workspace_id = ?",
			c.Param("issue"), *board.ProjectID, board.WorkspaceID).
		Where(`i.deleted_at IS NULL AND s.group IS DISTINCT FROM 'triage'
			AND i.archived_at IS NULL AND p.archived_at IS NULL AND i.is_draft = FALSE`).
		Count(&count).Error
	if err != nil {
		handler.serverError(c, err)
		return false
	}
	if count == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Issue not found"})
		return false
	}
	return true
}

// reactionFromPayload reads the emoji, which is the one field the serializer requires.
func reactionFromPayload(c *gin.Context) (string, bool) {
	var payload map[string]json.RawMessage
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"reaction": []string{"This field is required."}})
		return "", false
	}
	raw, given := payload["reaction"]
	if !given {
		c.JSON(http.StatusBadRequest, gin.H{"reaction": []string{"This field is required."}})
		return "", false
	}
	var reaction string
	if json.Unmarshal(raw, &reaction) != nil || reaction == "" {
		c.JSON(http.StatusBadRequest, gin.H{"reaction": []string{"This field may not be blank."}})
		return "", false
	}
	return reaction, true
}

// writeReaction records the emoji and remembers the reader as somebody who has been on the board.
func (handler *Handler) writeReaction(c *gin.Context, board DeployBoard, user *auth.User, table string, scope map[string]any, reaction string) (Reaction, error) {
	identifier, err := newUUID()
	if err != nil {
		return Reaction{}, err
	}
	now := handler.clock().UTC()
	row := map[string]any{
		"id": identifier, "created_at": now, "updated_at": now,
		"created_by_id": user.ID, "updated_by_id": user.ID,
		"project_id": *board.ProjectID, "workspace_id": board.WorkspaceID,
		"actor_id": user.ID, "reaction": reaction,
	}
	for key, value := range scope {
		row[key] = value
	}
	err = handler.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Table(table).Create(row).Error; err != nil {
			return err
		}
		return handler.rememberPublicMember(tx, board, user.ID, now)
	})
	if err != nil {
		return Reaction{}, err
	}
	return Reaction{
		ID: identifier, CreatedAt: now, UpdatedAt: now,
		CreatedByID: &user.ID, UpdatedByID: &user.ID,
		ProjectID: *board.ProjectID, WorkspaceID: board.WorkspaceID,
		ActorID: user.ID, Reaction: reaction,
	}, nil
}

func (handler *Handler) destroyReaction(c *gin.Context, board DeployBoard, user *auth.User, table, condition, owner string) {
	var rows []Reaction
	err := handler.db.WithContext(c.Request.Context()).Table(table).
		Where("workspace_id = ? AND project_id = ?", board.WorkspaceID, *board.ProjectID).
		Where(condition, owner, c.Param("reaction"), user.ID).
		Where("deleted_at IS NULL").Limit(1).Scan(&rows).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if len(rows) == 0 {
		notFound(c)
		return
	}
	now := handler.clock().UTC()
	err = handler.db.WithContext(c.Request.Context()).Table(table).Where("id = ?", rows[0].ID).
		Updates(map[string]any{"deleted_at": now, "updated_at": now}).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// rememberPublicMember records a reader who is not in the project as somebody who has been on its board, which is how a published project counts the people reading it.
func (handler *Handler) rememberPublicMember(tx *gorm.DB, board DeployBoard, userID string, now time.Time) error {
	var member int64
	err := tx.Table("project_members").
		Where("project_id = ? AND member_id = ? AND is_active = TRUE AND deleted_at IS NULL", *board.ProjectID, userID).
		Count(&member).Error
	if err != nil || member > 0 {
		return err
	}
	var existing int64
	err = tx.Table("project_public_members").
		Where("project_id = ? AND member_id = ? AND deleted_at IS NULL", *board.ProjectID, userID).
		Count(&existing).Error
	if err != nil || existing > 0 {
		return err
	}
	identifier, err := newUUID()
	if err != nil {
		return err
	}
	return tx.Table("project_public_members").Create(map[string]any{
		"id": identifier, "created_at": now, "updated_at": now,
		"project_id": *board.ProjectID, "workspace_id": board.WorkspaceID, "member_id": userID,
	}).Error
}

// voteJSON is IssueVoteSerializer.
func voteJSON(vote IssueVote) gin.H {
	return gin.H{
		"id": vote.ID, "created_at": vote.CreatedAt, "updated_at": vote.UpdatedAt,
		"deleted_at": vote.DeletedAt, "vote": vote.Vote,
		"created_by": vote.CreatedByID, "updated_by": vote.UpdatedByID,
		"project": vote.ProjectID, "workspace": vote.WorkspaceID,
		"issue": vote.IssueID, "actor": vote.ActorID,
	}
}

// reactionJSON is the shape both reaction serializers carry.
func reactionJSON(row Reaction) gin.H {
	body := gin.H{
		"id": row.ID, "created_at": row.CreatedAt, "updated_at": row.UpdatedAt,
		"deleted_at": row.DeletedAt, "reaction": row.Reaction,
		"created_by": row.CreatedByID, "updated_by": row.UpdatedByID,
		"project": row.ProjectID, "workspace": row.WorkspaceID, "actor": row.ActorID,
	}
	if row.IssueID != nil {
		body["issue"] = *row.IssueID
	}
	if row.CommentID != nil {
		body["comment"] = *row.CommentID
	}
	return body
}
