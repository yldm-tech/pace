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

func (handler *Handler) registerIssueInteractionRoutes(router gin.IRouter) {
	router.GET("/api/workspaces/:slug/projects/:id/issues/:issue/reactions/", handler.authenticatedIssueUUID(handler.issueReactionList))
	router.POST("/api/workspaces/:slug/projects/:id/issues/:issue/reactions/", handler.authenticatedIssueUUID(handler.issueReactionCreate))
	// The reaction code is a free-form string in Django's URL, not a UUID.
	router.DELETE("/api/workspaces/:slug/projects/:id/issues/:issue/reactions/:reaction/", handler.authenticatedIssueUUID(handler.issueReactionDelete))

	router.GET("/api/workspaces/:slug/projects/:id/issues/:issue/issue-subscribers/", handler.authenticatedIssueUUID(handler.issueSubscriberList))
	router.POST("/api/workspaces/:slug/projects/:id/issues/:issue/issue-subscribers/", handler.authenticatedIssueUUID(handler.issueSubscriberCreate))
	router.DELETE("/api/workspaces/:slug/projects/:id/issues/:issue/issue-subscribers/:subscriber/", handler.authenticatedIssueUUID(handler.issueSubscriberDelete))

	router.GET("/api/workspaces/:slug/projects/:id/issues/:issue/subscribe/", handler.authenticatedIssueUUID(handler.issueSubscriptionStatus))
	router.POST("/api/workspaces/:slug/projects/:id/issues/:issue/subscribe/", handler.authenticatedIssueUUID(handler.issueSubscribe))
	router.DELETE("/api/workspaces/:slug/projects/:id/issues/:issue/subscribe/", handler.authenticatedIssueUUID(handler.issueUnsubscribe))
}

func (handler *Handler) authenticatedIssueUUID(next func(*gin.Context, *auth.User)) gin.HandlerFunc {
	inner := handler.authenticatedUUID(next)
	return func(c *gin.Context) {
		if !uuidPattern.MatchString(c.Param("issue")) {
			handler.notFound(c)
			return
		}
		inner(c)
	}
}

// issueReactionList applies the queryset's own filters: the caller must be an
// active member of the project and the project must not be archived.
func (handler *Handler) issueReactionList(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	var reactions []IssueReaction
	err := handler.db.WithContext(c.Request.Context()).
		Where(`issue_id = ? AND project_id = ? AND workspace_id = (SELECT id FROM workspaces WHERE slug = ?)
			AND deleted_at IS NULL
			AND EXISTS (SELECT 1 FROM projects p WHERE p.id = issue_reactions.project_id AND p.archived_at IS NULL AND p.deleted_at IS NULL)
			AND EXISTS (SELECT 1 FROM project_members pm WHERE pm.project_id = issue_reactions.project_id AND pm.member_id = ? AND pm.is_active = TRUE AND pm.deleted_at IS NULL)`,
			c.Param("issue"), c.Param("id"), c.Param("slug"), user.ID).
		Order("created_at DESC").Find(&reactions).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	response := make([]gin.H, 0, len(reactions))
	for _, reaction := range reactions {
		data, err := handler.issueReactionJSON(c.Request.Context(), reaction)
		if err != nil {
			handler.internalError(c, err)
			return
		}
		response = append(response, data)
	}
	drf.Respond(c, http.StatusOK, response)
}

func (handler *Handler) issueReactionCreate(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	slug, projectID, issueID := c.Param("slug"), c.Param("id"), c.Param("issue")
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
	// reaction is a TextField with blank unset, so DRF refuses an empty value.
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
	reaction := IssueReaction{
		ID: reactionID, CreatedAt: now, UpdatedAt: now, CreatedByID: &user.ID,
		ProjectID: projectID, WorkspaceID: workspaceID, IssueID: issueID,
		ActorID: user.ID, Reaction: reactionCode,
	}
	if err := handler.db.WithContext(c.Request.Context()).Create(&reaction).Error; err != nil {
		if isUniqueViolation(err) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "The payload is not valid"})
			return
		}
		handler.internalError(c, err)
		return
	}
	requested := map[string]any{}
	for key, value := range body {
		var decoded any
		if json.Unmarshal(value, &decoded) == nil {
			requested[key] = decoded
		}
	}
	encoded, err := json.Marshal(requested)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	requestedData := string(encoded)
	if err := handler.publishIssueActivity(c, issueActivity{
		Type: "issue_reaction.activity.created", RequestedData: &requestedData,
		ActorID: user.ID, IssueID: issueID, ProjectID: projectID,
		Notification: true, Origin: handler.origin(), Epoch: now,
	}); err != nil {
		handler.internalError(c, err)
		return
	}
	data, err := handler.issueReactionJSON(c.Request.Context(), reaction)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusCreated, data)
}

func (handler *Handler) issueReactionDelete(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	slug, projectID, issueID := c.Param("slug"), c.Param("id"), c.Param("issue")
	var reaction IssueReaction
	err := handler.db.WithContext(c.Request.Context()).
		Where(`workspace_id = (SELECT id FROM workspaces WHERE slug = ?) AND project_id = ? AND issue_id = ?
			AND reaction = ? AND actor_id = ? AND deleted_at IS NULL`,
			slug, projectID, issueID, c.Param("reaction"), user.ID).
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
	// Django queues the activity before it deletes, and the current instance it
	// sends carries the row identifier.
	instance, err := json.Marshal(map[string]any{
		"reaction": reaction.Reaction, "identifier": reaction.ID,
	})
	if err != nil {
		handler.internalError(c, err)
		return
	}
	current := string(instance)
	if err := handler.publishIssueActivity(c, issueActivity{
		Type: "issue_reaction.activity.deleted", CurrentInstance: &current,
		ActorID: user.ID, IssueID: issueID, ProjectID: projectID,
		Notification: true, Origin: handler.origin(), Epoch: now,
	}); err != nil {
		handler.internalError(c, err)
		return
	}
	err = handler.db.WithContext(c.Request.Context()).Model(&IssueReaction{}).
		Where("id = ?", reaction.ID).Updates(map[string]any{
		"deleted_at": now, "updated_at": now, "updated_by_id": user.ID,
	}).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if handler.tasks != nil {
		err := handler.tasks.PublishSoftDeleteRelatedObjects(c.Request.Context(), "db", "issuereaction", reaction.ID)
		if err != nil {
			handler.internalError(c, err)
			return
		}
	}
	c.Status(http.StatusNoContent)
}

// issueSubscriberList is the one that does not list subscribers: Django returns
// the project's members through ProjectMemberLiteSerializer instead. That is
// reproduced rather than corrected.
func (handler *Handler) issueSubscriberList(c *gin.Context, user *auth.User) {
	if !handler.requireProjectMembership(c, user) {
		return
	}
	var members []ProjectMember
	err := handler.db.WithContext(c.Request.Context()).
		Where("project_id = ? AND workspace_id = (SELECT id FROM workspaces WHERE slug = ?) AND is_active = TRUE AND deleted_at IS NULL",
			c.Param("id"), c.Param("slug")).
		Order("created_at DESC").Find(&members).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	response := make([]gin.H, 0, len(members))
	for _, member := range members {
		data, err := handler.projectMemberLiteJSON(c.Request.Context(), member)
		if err != nil {
			handler.internalError(c, err)
			return
		}
		response = append(response, data)
	}
	drf.Respond(c, http.StatusOK, response)
}

func (handler *Handler) issueSubscriberCreate(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember) {
		return
	}
	slug, projectID, issueID := c.Param("slug"), c.Param("id"), c.Param("issue")
	var body struct {
		Subscriber *string `json:"subscriber"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		handler.invalidDetail(c)
		return
	}
	if body.Subscriber == nil {
		c.JSON(http.StatusBadRequest, gin.H{"subscriber": []string{"This field is required."}})
		return
	}
	canonical, valid := canonicalUUID(*body.Subscriber)
	if !valid {
		c.JSON(http.StatusBadRequest, gin.H{"subscriber": []string{`“` + *body.Subscriber + `” is not a valid UUID.`}})
		return
	}
	exists, err := handler.rowExists(c.Request.Context(), "users", canonical)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !exists {
		c.JSON(http.StatusBadRequest, gin.H{"subscriber": []string{`Invalid pk "` + canonical + `" - object does not exist.`}})
		return
	}
	subscriber, ok := handler.createSubscriber(c, slug, projectID, issueID, canonical, user.ID)
	if !ok {
		return
	}
	drf.Respond(c, http.StatusCreated, issueSubscriberJSON(subscriber))
}

func (handler *Handler) issueSubscriberDelete(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember) {
		return
	}
	handler.deleteSubscriber(c, user, c.Param("subscriber"))
}

// issueSubscribe uses ProjectLitePermission, so any active project member may
// subscribe themselves regardless of role.
func (handler *Handler) issueSubscribe(c *gin.Context, user *auth.User) {
	if !handler.requireProjectMembership(c, user) {
		return
	}
	slug, projectID, issueID := c.Param("slug"), c.Param("id"), c.Param("issue")
	var count int64
	err := handler.db.WithContext(c.Request.Context()).Model(&IssueSubscriber{}).
		Where("issue_id = ? AND subscriber_id = ? AND project_id = ? AND workspace_id = (SELECT id FROM workspaces WHERE slug = ?) AND deleted_at IS NULL",
			issueID, user.ID, projectID, slug).Count(&count).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if count > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "User already subscribed to the issue."})
		return
	}
	subscriber, ok := handler.createSubscriber(c, slug, projectID, issueID, user.ID, user.ID)
	if !ok {
		return
	}
	drf.Respond(c, http.StatusCreated, issueSubscriberJSON(subscriber))
}

func (handler *Handler) issueUnsubscribe(c *gin.Context, user *auth.User) {
	if !handler.requireProjectMembership(c, user) {
		return
	}
	handler.deleteSubscriber(c, user, user.ID)
}

func (handler *Handler) issueSubscriptionStatus(c *gin.Context, user *auth.User) {
	if !handler.requireProjectMembership(c, user) {
		return
	}
	var count int64
	err := handler.db.WithContext(c.Request.Context()).Model(&IssueSubscriber{}).
		Where("issue_id = ? AND subscriber_id = ? AND project_id = ? AND workspace_id = (SELECT id FROM workspaces WHERE slug = ?) AND deleted_at IS NULL",
			c.Param("issue"), user.ID, c.Param("id"), c.Param("slug")).Count(&count).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, gin.H{"subscribed": count > 0})
}

func (handler *Handler) createSubscriber(c *gin.Context, slug, projectID, issueID, subscriberID, actorID string) (IssueSubscriber, bool) {
	workspaceID, ok := handler.projectWorkspaceID(c, slug, projectID)
	if !ok {
		return IssueSubscriber{}, false
	}
	subscriberRowID, err := newUUID()
	if err != nil {
		handler.internalError(c, err)
		return IssueSubscriber{}, false
	}
	now := handler.clock().UTC()
	subscriber := IssueSubscriber{
		ID: subscriberRowID, CreatedAt: now, UpdatedAt: now, CreatedByID: &actorID,
		ProjectID: projectID, WorkspaceID: workspaceID, IssueID: issueID,
		SubscriberID: subscriberID,
	}
	if err := handler.db.WithContext(c.Request.Context()).Create(&subscriber).Error; err != nil {
		if isUniqueViolation(err) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "The payload is not valid"})
			return IssueSubscriber{}, false
		}
		handler.internalError(c, err)
		return IssueSubscriber{}, false
	}
	return subscriber, true
}

func (handler *Handler) deleteSubscriber(c *gin.Context, user *auth.User, subscriberID string) {
	var subscriber IssueSubscriber
	err := handler.db.WithContext(c.Request.Context()).
		Where("project_id = ? AND subscriber_id = ? AND workspace_id = (SELECT id FROM workspaces WHERE slug = ?) AND issue_id = ? AND deleted_at IS NULL",
			c.Param("id"), subscriberID, c.Param("slug"), c.Param("issue")).
		Order("created_at DESC").Take(&subscriber).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		handler.notFound(c)
		return
	}
	if err != nil {
		handler.internalError(c, err)
		return
	}
	now := handler.clock().UTC()
	err = handler.db.WithContext(c.Request.Context()).Model(&IssueSubscriber{}).
		Where("id = ?", subscriber.ID).Updates(map[string]any{
		"deleted_at": now, "updated_at": now, "updated_by_id": user.ID,
	}).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if handler.tasks != nil {
		err := handler.tasks.PublishSoftDeleteRelatedObjects(c.Request.Context(), "db", "issuesubscriber", subscriber.ID)
		if err != nil {
			handler.internalError(c, err)
			return
		}
	}
	c.Status(http.StatusNoContent)
}

// requireProjectMembership is ProjectLitePermission: an active membership of
// the project, whatever the role.
func (handler *Handler) requireProjectMembership(c *gin.Context, user *auth.User) bool {
	_, found, err := handler.activeProjectMember(c.Request.Context(), c.Param("slug"), c.Param("id"), user.ID)
	if err != nil {
		handler.internalError(c, err)
		return false
	}
	if !found {
		c.JSON(http.StatusForbidden, gin.H{"detail": "You do not have permission to perform this action."})
		return false
	}
	return true
}

func (handler *Handler) projectWorkspaceID(c *gin.Context, slug, projectID string) (string, bool) {
	var workspaceID string
	err := handler.db.WithContext(c.Request.Context()).Table("projects p").
		Joins("JOIN workspaces w ON w.id = p.workspace_id").
		Where("p.id = ? AND w.slug = ? AND p.deleted_at IS NULL", projectID, slug).
		Limit(1).Pluck("p.workspace_id", &workspaceID).Error
	if err != nil {
		handler.internalError(c, err)
		return "", false
	}
	if workspaceID == "" {
		handler.notFound(c)
		return "", false
	}
	return workspaceID, true
}

// issueActivity carries the keyword arguments issue_activity is called with.
type issueActivity struct {
	Type            string
	RequestedData   *string
	CurrentInstance *string
	ActorID         string
	IssueID         string
	ProjectID       string
	Notification    bool
	Origin          string
	Epoch           time.Time
	// SubscriberSet marks the one call that turns the subscriber flag off.
	SubscriberSet bool
	// IntakeID names the intake link an activity belongs to, which the intake routes send and nothing else does.
	IntakeID string
}

// publishIssueActivity queues issue_activity, which still runs on the Python
// worker; the publisher routes anything not migrated to the Celery queue.
func (handler *Handler) publishIssueActivity(c *gin.Context, activity issueActivity) error {
	if handler.tasks == nil {
		return nil
	}
	keywords := map[string]any{
		"type":             activity.Type,
		"requested_data":   nullableString(activity.RequestedData),
		"current_instance": nullableString(activity.CurrentInstance),
		// The cycle transfer is the one activity that names no issue: the move is about the cycle and the task fans it out over the list it carries. Django sends None there, and an empty string is not the same thing to a task that looks the issue up.
		"issue_id":   nullableID(activity.IssueID),
		"actor_id":   activity.ActorID,
		"project_id": activity.ProjectID,
		// Django sends the epoch as an integer second count.
		"epoch":        activity.Epoch.Unix(),
		"notification": activity.Notification,
		"origin":       activity.Origin,
	}
	if activity.SubscriberSet {
		keywords["subscriber"] = false
	}
	if activity.IntakeID != "" {
		keywords["intake"] = activity.IntakeID
	}
	return handler.tasks.PublishIssueActivity(c.Request.Context(), keywords)
}

// nullableID renders an unset id as null rather than as the empty string.
func nullableID(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullableString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

// issueReactionJSON is IssueReactionSerializer: the model's fields plus the
// actor expanded through UserLiteSerializer.
func (handler *Handler) issueReactionJSON(ctx context.Context, reaction IssueReaction) (gin.H, error) {
	var actor auth.User
	if err := handler.db.WithContext(ctx).Where("id = ?", reaction.ActorID).Take(&actor).Error; err != nil {
		return nil, err
	}
	return gin.H{
		"id": reaction.ID, "created_at": reaction.CreatedAt, "updated_at": reaction.UpdatedAt,
		"created_by": reaction.CreatedByID, "updated_by": reaction.UpdatedByID,
		"deleted_at": reaction.DeletedAt, "project": reaction.ProjectID,
		"workspace": reaction.WorkspaceID, "issue": reaction.IssueID,
		"actor": reaction.ActorID, "reaction": reaction.Reaction,
		"actor_detail": liteUserJSON(actor, false),
	}, nil
}

// issueSubscriberJSON is IssueSubscriberSerializer, which lists every field.
func issueSubscriberJSON(subscriber IssueSubscriber) gin.H {
	return gin.H{
		"id": subscriber.ID, "created_at": subscriber.CreatedAt, "updated_at": subscriber.UpdatedAt,
		"created_by": subscriber.CreatedByID, "updated_by": subscriber.UpdatedByID,
		"deleted_at": subscriber.DeletedAt, "project": subscriber.ProjectID,
		"workspace": subscriber.WorkspaceID, "issue": subscriber.IssueID,
		"subscriber": subscriber.SubscriberID,
	}
}

// projectMemberLiteJSON is ProjectMemberLiteSerializer. is_subscribed is an
// annotation the subscriber list route never adds, so it comes back null.
func (handler *Handler) projectMemberLiteJSON(ctx context.Context, member ProjectMember) (gin.H, error) {
	var user auth.User
	if err := handler.db.WithContext(ctx).Where("id = ?", member.MemberID).Take(&user).Error; err != nil {
		return nil, err
	}
	return gin.H{
		"member": liteUserJSON(user, false), "id": member.ID, "is_subscribed": nil,
	}, nil
}
