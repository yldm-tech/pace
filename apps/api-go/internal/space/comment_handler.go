package space

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"github.com/yldm-tech/pace/apps/api-go/internal/drf"
	"github.com/yldm-tech/pace/apps/api-go/internal/issues"
	"gorm.io/gorm"
)

func (handler *Handler) registerCommentRoutes(router gin.IRouter) {
	const base = "/api/public/anchor/:anchor/issues/:issue/comments/"
	// The two reads carry no session and the three writes do, which is the one route pair in this app where that line runs through a single viewset.
	router.GET(base, handler.commentList)
	router.GET(base+":comment/", handler.commentRetrieve)
	router.POST(base, handler.authenticated(handler.commentCreate))
	router.PATCH(base+":comment/", handler.authenticated(handler.commentUpdate))
	router.DELETE(base+":comment/", handler.authenticated(handler.commentDestroy))
}

// IssueComment is a comment as the published board sees one, which is only ever an external comment.
type IssueComment struct {
	ID              string     `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt       time.Time  `gorm:"column:created_at"`
	UpdatedAt       time.Time  `gorm:"column:updated_at"`
	CreatedByID     *string    `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID     *string    `gorm:"column:updated_by_id;type:uuid"`
	DeletedAt       *time.Time `gorm:"column:deleted_at"`
	ProjectID       string     `gorm:"column:project_id;type:uuid"`
	WorkspaceID     string     `gorm:"column:workspace_id;type:uuid"`
	IssueID         string     `gorm:"column:issue_id;type:uuid"`
	ActorID         *string    `gorm:"column:actor_id;type:uuid"`
	CommentStripped string     `gorm:"column:comment_stripped"`
	CommentJSON     []byte     `gorm:"column:comment_json;type:jsonb"`
	CommentHTML     string     `gorm:"column:comment_html"`
	Attachments     []byte     `gorm:"column:attachments"`
	Access          string     `gorm:"column:access"`
	ExternalSource  *string    `gorm:"column:external_source"`
	ExternalID      *string    `gorm:"column:external_id"`
	EditedAt        *time.Time `gorm:"column:edited_at"`
	DescriptionID   *string    `gorm:"column:description_id;type:uuid"`
	ParentID        *string    `gorm:"column:parent_id;type:uuid"`
}

func (IssueComment) TableName() string { return "issue_comments" }

// commentList reports a work item's public comments, oldest first, and an **empty list** when the board has comments switched off rather than a refusal.
func (handler *Handler) commentList(c *gin.Context) {
	board, found, err := handler.projectBoardByAnchor(c)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !found || !board.IsCommentsEnabled || board.ProjectID == nil {
		drf.Respond(c, http.StatusOK, []gin.H{})
		return
	}
	comments, err := handler.boardComments(c, board, "")
	if err != nil {
		handler.serverError(c, err)
		return
	}
	bodies, err := handler.commentBodies(c, board, comments)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, bodies)
}

func (handler *Handler) commentRetrieve(c *gin.Context) {
	board, found, err := handler.projectBoardByAnchor(c)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !found || !board.IsCommentsEnabled || board.ProjectID == nil {
		notFound(c)
		return
	}
	comments, err := handler.boardComments(c, board, c.Param("comment"))
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if len(comments) == 0 {
		notFound(c)
		return
	}
	bodies, err := handler.commentBodies(c, board, comments)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, bodies[0])
}

// commentCreate writes a comment as the reader, always **external** whatever the payload says — the access field is on the serializer and the view overrides it.
func (handler *Handler) commentCreate(c *gin.Context, user *auth.User) {
	board, ok := handler.boardForWriting(c, func(board DeployBoard) bool { return board.IsCommentsEnabled },
		"Comments are not enabled for this project")
	if !ok {
		return
	}
	if !handler.issueInBoard(c, board) {
		return
	}
	var payload map[string]json.RawMessage
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "The payload is not valid"})
		return
	}
	html := "<p></p>"
	if raw, given := payload["comment_html"]; given {
		var value string
		if json.Unmarshal(raw, &value) != nil {
			c.JSON(http.StatusBadRequest, gin.H{"comment_html": []string{"Not a valid string."}})
			return
		}
		html = value
	}
	identifier, err := newUUID()
	if err != nil {
		handler.serverError(c, err)
		return
	}
	now := handler.clock().UTC()
	comment := IssueComment{
		ID: identifier, CreatedAt: now, UpdatedAt: now,
		CreatedByID: &user.ID, UpdatedByID: &user.ID,
		ProjectID: *board.ProjectID, WorkspaceID: board.WorkspaceID,
		IssueID: c.Param("issue"), ActorID: &user.ID,
		CommentHTML: html, CommentStripped: stripTags(html), Access: "EXTERNAL",
		CommentJSON: commentJSONOrEmpty(payload["comment_json"]),
	}
	err = handler.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		row := map[string]any{
			"id": comment.ID, "created_at": now, "updated_at": now,
			"created_by_id": user.ID, "updated_by_id": user.ID,
			"project_id": comment.ProjectID, "workspace_id": comment.WorkspaceID,
			"issue_id": comment.IssueID, "actor_id": user.ID,
			"comment_html": comment.CommentHTML, "comment_stripped": comment.CommentStripped,
			"comment_json": auth.JSONValue(comment.CommentJSON), "access": "EXTERNAL",
			"attachments": "{}",
		}
		if err := tx.Table("issue_comments").Create(row).Error; err != nil {
			return err
		}
		return handler.rememberPublicMember(tx, board, user.ID, now)
	})
	if err != nil {
		handler.serverError(c, err)
		return
	}
	bodies, err := handler.commentBodies(c, board, []IssueComment{comment})
	if err != nil {
		handler.serverError(c, err)
		return
	}
	drf.Respond(c, http.StatusCreated, bodies[0])
}

// commentUpdate edits a comment, and only one the caller wrote themselves.
func (handler *Handler) commentUpdate(c *gin.Context, user *auth.User) {
	board, ok := handler.boardForWriting(c, func(board DeployBoard) bool { return board.IsCommentsEnabled },
		"Comments are not enabled for this project")
	if !ok {
		return
	}
	comment, found, err := handler.ownComment(c, board, user)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !found {
		notFound(c)
		return
	}
	var payload map[string]json.RawMessage
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "The payload is not valid"})
		return
	}
	updates := map[string]any{}
	if raw, given := payload["comment_html"]; given {
		var value string
		if json.Unmarshal(raw, &value) != nil {
			c.JSON(http.StatusBadRequest, gin.H{"comment_html": []string{"Not a valid string."}})
			return
		}
		updates["comment_html"] = value
		// The stripped copy follows the html, which the model writes on every save.
		updates["comment_stripped"] = stripTags(value)
		comment.CommentHTML = value
		comment.CommentStripped = stripTags(value)
	}
	if raw, given := payload["comment_json"]; given && json.Valid(raw) {
		updates["comment_json"] = auth.JSONValue(append([]byte(nil), raw...))
		comment.CommentJSON = append([]byte(nil), raw...)
	}
	if len(updates) > 0 {
		now := handler.clock().UTC()
		updates["updated_at"] = now
		updates["updated_by_id"] = user.ID
		comment.UpdatedAt = now
		err := handler.db.WithContext(c.Request.Context()).Table("issue_comments").
			Where("id = ?", comment.ID).Updates(updates).Error
		if err != nil {
			handler.serverError(c, err)
			return
		}
	}
	bodies, err := handler.commentBodies(c, board, []IssueComment{comment})
	if err != nil {
		handler.serverError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, bodies[0])
}

func (handler *Handler) commentDestroy(c *gin.Context, user *auth.User) {
	board, ok := handler.boardForWriting(c, func(board DeployBoard) bool { return board.IsCommentsEnabled },
		"Comments are not enabled for this project")
	if !ok {
		return
	}
	comment, found, err := handler.ownComment(c, board, user)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !found {
		notFound(c)
		return
	}
	now := handler.clock().UTC()
	err = handler.db.WithContext(c.Request.Context()).Table("issue_comments").Where("id = ?", comment.ID).
		Updates(map[string]any{"deleted_at": now, "updated_at": now}).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// ownComment reads a comment the caller wrote, bound to the board, the work item and the external access the board shows.
func (handler *Handler) ownComment(c *gin.Context, board DeployBoard, user *auth.User) (IssueComment, bool, error) {
	var comments []IssueComment
	err := handler.db.WithContext(c.Request.Context()).Table("issue_comments").
		Where(`id = ? AND issue_id = ? AND project_id = ? AND workspace_id = ?
			AND access = 'EXTERNAL' AND actor_id = ? AND deleted_at IS NULL`,
			c.Param("comment"), c.Param("issue"), *board.ProjectID, board.WorkspaceID, user.ID).
		Limit(1).Scan(&comments).Error
	if err != nil || len(comments) == 0 {
		return IssueComment{}, false, err
	}
	return comments[0], true, nil
}

// boardComments reads the work item's public comments, oldest first.
func (handler *Handler) boardComments(c *gin.Context, board DeployBoard, commentID string) ([]IssueComment, error) {
	query := handler.db.WithContext(c.Request.Context()).Table("issue_comments").
		Where(`issue_id = ? AND project_id = ? AND workspace_id = ? AND access = 'EXTERNAL' AND deleted_at IS NULL`,
			c.Param("issue"), *board.ProjectID, board.WorkspaceID)
	if commentID != "" {
		query = query.Where("id = ?", commentID)
	}
	var comments []IssueComment
	err := query.Order("created_at").Scan(&comments).Error
	return comments, err
}

// commentBodies is IssueCommentSerializer: the comment with its author, its work item, its project, its workspace and its reactions nested inside, and a flag saying whether the author is in the project.
func (handler *Handler) commentBodies(c *gin.Context, board DeployBoard, comments []IssueComment) ([]gin.H, error) {
	results := make([]gin.H, 0, len(comments))
	if len(comments) == 0 {
		return results, nil
	}
	project, err := handler.projectLiteJSON(c, *board.ProjectID)
	if err != nil {
		return nil, err
	}
	workspace, err := handler.workspaceLiteJSON(c, board.WorkspaceID)
	if err != nil {
		return nil, err
	}
	issue, err := handler.issueFlatJSON(c, comments[0].IssueID)
	if err != nil {
		return nil, err
	}
	for _, comment := range comments {
		actor := any(nil)
		if comment.ActorID != nil {
			rendered, err := handler.userLiteJSON(c, *comment.ActorID)
			if err != nil {
				return nil, err
			}
			actor = rendered
		}
		reactions, err := handler.commentReactions(c, comment.ID)
		if err != nil {
			return nil, err
		}
		// is_member is annotated for the **caller** rather than for the author. The two reads carry no session at all, so it is false on every comment they report.
		member, err := handler.isProjectMember(c, board, callerID(c))
		if err != nil {
			return nil, err
		}
		results = append(results, gin.H{
			"id": comment.ID, "actor_detail": actor, "issue_detail": issue,
			"project_detail": project, "workspace_detail": workspace,
			"comment_reactions": reactions, "is_member": member,
			"created_at": comment.CreatedAt, "updated_at": comment.UpdatedAt, "deleted_at": comment.DeletedAt,
			"comment_stripped": comment.CommentStripped, "comment_json": decodeJSON(comment.CommentJSON),
			"comment_html": comment.CommentHTML, "attachments": []any{},
			"access": comment.Access, "external_source": comment.ExternalSource, "external_id": comment.ExternalID,
			"edited_at":  comment.EditedAt,
			"created_by": comment.CreatedByID, "updated_by": comment.UpdatedByID,
			"project": comment.ProjectID, "workspace": comment.WorkspaceID,
			"description": comment.DescriptionID, "issue": comment.IssueID,
			"actor": comment.ActorID, "parent": comment.ParentID,
		})
	}
	return results, nil
}

// isProjectMember answers the flag the serializer carries. An empty identifier is nobody, which is what an unauthenticated read passes.
func (handler *Handler) isProjectMember(c *gin.Context, board DeployBoard, userID string) (bool, error) {
	if userID == "" {
		return false, nil
	}
	var count int64
	err := handler.db.WithContext(c.Request.Context()).Table("project_members").
		Where("workspace_id = ? AND project_id = ? AND member_id = ? AND is_active = TRUE AND deleted_at IS NULL",
			board.WorkspaceID, *board.ProjectID, userID).Count(&count).Error
	return count > 0, err
}

func (handler *Handler) commentReactions(c *gin.Context, commentID string) ([]gin.H, error) {
	var rows []Reaction
	err := handler.db.WithContext(c.Request.Context()).Table("comment_reactions").
		Where("comment_id = ? AND deleted_at IS NULL", commentID).
		Order("created_at").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	results := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		row.CommentID = &commentID
		results = append(results, reactionJSON(row))
	}
	return results, nil
}

// issueFlatJSON is IssueFlatSerializer: ten fields of the work item the comment is on.
func (handler *Handler) issueFlatJSON(c *gin.Context, issueID string) (gin.H, error) {
	var rows []struct {
		ID              string     `gorm:"column:id"`
		Name            string     `gorm:"column:name"`
		DescriptionJSON []byte     `gorm:"column:description_json"`
		DescriptionHTML string     `gorm:"column:description_html"`
		Priority        string     `gorm:"column:priority"`
		StartDate       *time.Time `gorm:"column:start_date"`
		TargetDate      *time.Time `gorm:"column:target_date"`
		SequenceID      int64      `gorm:"column:sequence_id"`
		SortOrder       float64    `gorm:"column:sort_order"`
		IsDraft         bool       `gorm:"column:is_draft"`
	}
	err := handler.db.WithContext(c.Request.Context()).Table("issues").
		Select("id, name, description_json, description_html, priority, start_date, target_date, sequence_id, sort_order, is_draft").
		Where("id = ?", issueID).Limit(1).Scan(&rows).Error
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	row := rows[0]
	return gin.H{
		"id": row.ID, "name": row.Name,
		"description_json": decodeJSON(row.DescriptionJSON), "description_html": row.DescriptionHTML,
		"priority": row.Priority, "start_date": dayOrNil(row.StartDate), "target_date": dayOrNil(row.TargetDate),
		"sequence_id": row.SequenceID, "sort_order": row.SortOrder, "is_draft": row.IsDraft,
	}, nil
}

// userLiteJSON is the space app's UserLiteSerializer, which reports the bot flag the other apps' lite serializers leave out.
func (handler *Handler) userLiteJSON(c *gin.Context, userID string) (gin.H, error) {
	var rows []struct {
		ID          string  `gorm:"column:id"`
		FirstName   string  `gorm:"column:first_name"`
		LastName    string  `gorm:"column:last_name"`
		Avatar      string  `gorm:"column:avatar"`
		AvatarAsset *string `gorm:"column:avatar_asset_id"`
		IsBot       bool    `gorm:"column:is_bot"`
		DisplayName string  `gorm:"column:display_name"`
	}
	err := handler.db.WithContext(c.Request.Context()).Table("users").
		Select("id, first_name, last_name, avatar, avatar_asset_id, is_bot, display_name").
		Where("id = ?", userID).Limit(1).Scan(&rows).Error
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	row := rows[0]
	var avatarURL any
	switch {
	case row.AvatarAsset != nil:
		avatarURL = "/api/assets/v2/static/" + *row.AvatarAsset + "/"
	case row.Avatar != "":
		avatarURL = row.Avatar
	}
	return gin.H{
		"id": row.ID, "first_name": row.FirstName, "last_name": row.LastName,
		"avatar": row.Avatar, "avatar_url": avatarURL,
		"is_bot": row.IsBot, "display_name": row.DisplayName,
	}, nil
}

// callerID is who is asking, which on the two read routes is nobody.
func callerID(c *gin.Context) string {
	if value, present := c.Get("space.caller"); present {
		if identifier, ok := value.(string); ok {
			return identifier
		}
	}
	return ""
}

func commentJSONOrEmpty(raw json.RawMessage) []byte {
	if len(raw) > 0 && json.Valid(raw) {
		return append([]byte(nil), raw...)
	}
	return []byte(`{}`)
}

// stripTags is Django's strip_tags over the comment html, which the model writes beside it. A comment with no html has an empty stripped copy rather than a null, which is where it differs from a work item's description.
func stripTags(html string) string {
	stripped := issues.StripTags(html)
	if stripped == nil {
		return ""
	}
	return *stripped
}

func dayOrNil(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.Format("2006-01-02")
}
