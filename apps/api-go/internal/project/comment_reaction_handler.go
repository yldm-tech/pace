package project

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"gorm.io/gorm"
)

func (handler *Handler) registerCommentReactionRoutes(router gin.IRouter) {
	router.GET("/api/workspaces/:slug/projects/:id/comments/:comment/reactions/", handler.authenticatedCommentOnlyUUID(handler.commentReactionList))
	router.POST("/api/workspaces/:slug/projects/:id/comments/:comment/reactions/", handler.authenticatedCommentOnlyUUID(handler.commentReactionCreate))
	router.DELETE("/api/workspaces/:slug/projects/:id/comments/:comment/reactions/:reaction/", handler.authenticatedCommentOnlyUUID(handler.commentReactionDelete))
}

// authenticatedCommentOnlyUUID guards the comment reaction routes, which hang
// off a comment directly rather than off an issue.
func (handler *Handler) authenticatedCommentOnlyUUID(next func(*gin.Context, *auth.User)) gin.HandlerFunc {
	inner := handler.authenticatedUUID(next)
	return func(c *gin.Context) {
		if !uuidPattern.MatchString(c.Param("comment")) {
			handler.notFound(c)
			return
		}
		inner(c)
	}
}

func (handler *Handler) commentReactionList(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	reactions, err := handler.commentReactionRows(c.Request.Context(), c.Param("slug"), c.Param("id"), c.Param("comment"), user.ID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	response := make([]gin.H, 0, len(reactions))
	for _, reaction := range reactions {
		data, err := handler.commentReactionJSON(c.Request.Context(), reaction)
		if err != nil {
			handler.internalError(c, err)
			return
		}
		response = append(response, data)
	}
	c.JSON(http.StatusOK, response)
}

func (handler *Handler) commentReactionCreate(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	slug, projectID, commentID := c.Param("slug"), c.Param("id"), c.Param("comment")
	workspaceID, ok := handler.projectWorkspaceID(c, slug, projectID)
	if !ok {
		return
	}
	var body map[string]json.RawMessage
	if err := c.ShouldBindJSON(&body); err != nil {
		handler.invalidDetail(c)
		return
	}
	raw, exists := body["reaction"]
	if !exists {
		c.JSON(http.StatusBadRequest, gin.H{"reaction": []string{"This field is required."}})
		return
	}
	if string(raw) == "null" {
		c.JSON(http.StatusBadRequest, gin.H{"reaction": []string{"This field may not be null."}})
		return
	}
	var reactionCode string
	if json.Unmarshal(raw, &reactionCode) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"reaction": []string{"Not a valid string."}})
		return
	}
	if reactionCode == "" {
		c.JSON(http.StatusBadRequest, gin.H{"reaction": []string{"This field may not be blank."}})
		return
	}
	reactionID, err := newUUID()
	if err != nil {
		handler.internalError(c, err)
		return
	}
	now := handler.clock().UTC()
	reaction := CommentReaction{
		ID: reactionID, CreatedAt: now, UpdatedAt: now, CreatedByID: &user.ID,
		ProjectID: projectID, WorkspaceID: workspaceID, CommentID: commentID,
		ActorID: user.ID, Reaction: reactionCode,
	}
	if err := handler.db.WithContext(c.Request.Context()).Create(&reaction).Error; err != nil {
		if isUniqueViolation(err) {
			// Django catches IntegrityError here with its own message.
			c.JSON(http.StatusBadRequest, gin.H{"error": "Reaction already exists for the user"})
			return
		}
		handler.internalError(c, err)
		return
	}
	requested, err := json.Marshal(decodeRawFields(body))
	if err != nil {
		handler.internalError(c, err)
		return
	}
	requestedData := string(requested)
	// The comment reaction activities carry no issue id, which is what Django
	// sends: issue_id is None on both.
	if err := handler.publishIssueActivity(c, issueActivity{
		Type: "comment_reaction.activity.created", RequestedData: &requestedData,
		ActorID: user.ID, ProjectID: projectID,
		Notification: true, Origin: handler.origin(), Epoch: now,
	}); err != nil {
		handler.internalError(c, err)
		return
	}
	data, err := handler.commentReactionJSON(c.Request.Context(), reaction)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	c.JSON(http.StatusCreated, data)
}

func (handler *Handler) commentReactionDelete(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	slug, projectID, commentID := c.Param("slug"), c.Param("id"), c.Param("comment")
	var reaction CommentReaction
	err := handler.db.WithContext(c.Request.Context()).
		Where(`workspace_id = (SELECT id FROM workspaces WHERE slug = ?) AND project_id = ? AND comment_id = ?
			AND reaction = ? AND actor_id = ? AND deleted_at IS NULL`,
			slug, projectID, commentID, c.Param("reaction"), user.ID).
		Order("created_at DESC").Take(&reaction).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		handler.notFound(c)
		return
	}
	if err != nil {
		handler.internalError(c, err)
		return
	}
	now := handler.clock().UTC()
	instance, err := json.Marshal(map[string]any{
		"reaction": reaction.Reaction, "identifier": reaction.ID, "comment_id": commentID,
	})
	if err != nil {
		handler.internalError(c, err)
		return
	}
	current := string(instance)
	if err := handler.publishIssueActivity(c, issueActivity{
		Type: "comment_reaction.activity.deleted", CurrentInstance: &current,
		ActorID: user.ID, ProjectID: projectID,
		Notification: true, Origin: handler.origin(), Epoch: now,
	}); err != nil {
		handler.internalError(c, err)
		return
	}
	err = handler.db.WithContext(c.Request.Context()).Model(&CommentReaction{}).
		Where("id = ?", reaction.ID).Updates(map[string]any{
		"deleted_at": now, "updated_at": now, "updated_by_id": user.ID,
	}).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if handler.tasks != nil {
		err := handler.tasks.PublishSoftDeleteRelatedObjects(c.Request.Context(), "db", "commentreaction", reaction.ID)
		if err != nil {
			handler.internalError(c, err)
			return
		}
	}
	c.Status(http.StatusNoContent)
}

func (handler *Handler) commentReactionRows(ctx context.Context, slug, projectID, commentID, userID string) ([]CommentReaction, error) {
	var reactions []CommentReaction
	err := handler.db.WithContext(ctx).
		Where(`comment_id = ? AND project_id = ? AND workspace_id = (SELECT id FROM workspaces WHERE slug = ?)
			AND deleted_at IS NULL
			AND EXISTS (SELECT 1 FROM projects p WHERE p.id = comment_reactions.project_id AND p.archived_at IS NULL AND p.deleted_at IS NULL)
			AND EXISTS (SELECT 1 FROM project_members pm WHERE pm.project_id = comment_reactions.project_id AND pm.member_id = ? AND pm.is_active = TRUE AND pm.deleted_at IS NULL)`,
			commentID, projectID, slug, userID).
		Order("created_at DESC").Find(&reactions).Error
	return reactions, err
}

// commentReactionsJSON backs the nested comment_reactions the comment
// serializer carries, which is not filtered by the caller's membership.
func (handler *Handler) commentReactionsJSON(ctx context.Context, commentID string) ([]gin.H, error) {
	var reactions []CommentReaction
	err := handler.db.WithContext(ctx).
		Where("comment_id = ? AND deleted_at IS NULL", commentID).
		Order("created_at DESC").Find(&reactions).Error
	if err != nil {
		return nil, err
	}
	response := make([]gin.H, 0, len(reactions))
	for _, reaction := range reactions {
		data, err := handler.commentReactionJSON(ctx, reaction)
		if err != nil {
			return nil, err
		}
		response = append(response, data)
	}
	return response, nil
}

// commentReactionJSON is CommentReactionSerializer, which lists twelve fields
// explicitly rather than using __all__ and adds the actor's display name.
func (handler *Handler) commentReactionJSON(ctx context.Context, reaction CommentReaction) (gin.H, error) {
	data := gin.H{
		"id": reaction.ID, "actor": reaction.ActorID, "comment": reaction.CommentID,
		"reaction": reaction.Reaction, "display_name": nil,
		"deleted_at": reaction.DeletedAt, "workspace": reaction.WorkspaceID,
		"project": reaction.ProjectID, "created_at": reaction.CreatedAt,
		"updated_at": reaction.UpdatedAt, "created_by": reaction.CreatedByID,
		"updated_by": reaction.UpdatedByID,
	}
	var actor auth.User
	if err := handler.db.WithContext(ctx).Where("id = ?", reaction.ActorID).Take(&actor).Error; err == nil {
		data["display_name"] = actor.DisplayName
	}
	return data, nil
}
