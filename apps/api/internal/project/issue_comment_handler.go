package project

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
	"github.com/yldm-tech/pace/apps/api/internal/auth"
	"github.com/yldm-tech/pace/apps/api/internal/drf"
	"github.com/yldm-tech/pace/apps/api/internal/htmlsanitizer"
	"gorm.io/gorm"
)

// commentAccessInternal is IssueComment.access's default.
const commentAccessInternal = "INTERNAL"

func (handler *Handler) registerIssueCommentRoutes(router gin.IRouter) {
	router.GET("/api/workspaces/:slug/projects/:id/issues/:issue/comments/", handler.authenticatedIssueUUID(handler.commentList))
	router.POST("/api/workspaces/:slug/projects/:id/issues/:issue/comments/", handler.authenticatedIssueUUID(handler.commentCreate))
	router.GET("/api/workspaces/:slug/projects/:id/issues/:issue/comments/:comment/", handler.authenticatedCommentUUID(handler.commentRetrieve))
	router.PATCH("/api/workspaces/:slug/projects/:id/issues/:issue/comments/:comment/", handler.authenticatedCommentUUID(handler.commentPatch))
	router.PUT("/api/workspaces/:slug/projects/:id/issues/:issue/comments/:comment/", handler.authenticatedCommentUUID(handler.fullUpdate("comment", handler.commentPatch)))
	router.DELETE("/api/workspaces/:slug/projects/:id/issues/:issue/comments/:comment/", handler.authenticatedCommentUUID(handler.commentDelete))
}

func (handler *Handler) authenticatedCommentUUID(next func(*gin.Context, *auth.User)) gin.HandlerFunc {
	inner := handler.authenticatedIssueUUID(next)
	return func(c *gin.Context) {
		if !uuidPattern.MatchString(c.Param("comment")) {
			handler.notFound(c)
			return
		}
		inner(c)
	}
}

func (handler *Handler) commentList(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	var comments []IssueComment
	err := handler.db.WithContext(c.Request.Context()).
		Where(`issue_id = ? AND project_id = ? AND workspace_id = (SELECT id FROM workspaces WHERE slug = ?)
			AND deleted_at IS NULL
			AND EXISTS (SELECT 1 FROM projects p WHERE p.id = issue_comments.project_id AND p.archived_at IS NULL AND p.deleted_at IS NULL)
			AND EXISTS (SELECT 1 FROM project_members pm WHERE pm.project_id = issue_comments.project_id AND pm.member_id = ? AND pm.is_active = TRUE AND pm.deleted_at IS NULL)`,
			c.Param("issue"), c.Param("id"), c.Param("slug"), user.ID).
		Order("created_at DESC").Find(&comments).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	// is_member is an Exists annotation over the caller's own membership, so it
	// is the same value for every row in the list.
	isMember, err := handler.isActiveProjectMember(c.Request.Context(), c.Param("slug"), c.Param("id"), user.ID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	response := make([]gin.H, 0, len(comments))
	for _, comment := range comments {
		data, err := handler.issueCommentJSON(c.Request.Context(), comment, &isMember)
		if err != nil {
			handler.internalError(c, err)
			return
		}
		response = append(response, data)
	}
	drf.Respond(c, http.StatusOK, response)
}

func (handler *Handler) commentRetrieve(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	comment, found, err := handler.issueCommentByID(c.Request.Context(), c.Param("slug"), c.Param("id"), c.Param("issue"), c.Param("comment"))
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		handler.notFound(c)
		return
	}
	isMember, err := handler.isActiveProjectMember(c.Request.Context(), c.Param("slug"), c.Param("id"), user.ID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	data, err := handler.issueCommentJSON(c.Request.Context(), comment, &isMember)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, data)
}

func (handler *Handler) commentCreate(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	slug, projectID, issueID := c.Param("slug"), c.Param("id"), c.Param("issue")
	// A guest may only comment when the project opens all features to guests or
	// the guest raised the issue themselves.
	allowed, err := handler.guestMayComment(c.Request.Context(), slug, projectID, issueID, user.ID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !allowed {
		c.JSON(http.StatusBadRequest, gin.H{"error": "You are not allowed to comment on the issue"})
		return
	}
	workspaceID, ok := handler.projectWorkspaceID(c, slug, projectID)
	if !ok {
		return
	}
	body, ok := handler.issueLinkBody(c)
	if !ok {
		return
	}
	fields, ok := handler.issueCommentFieldsFrom(c, body)
	if !ok {
		return
	}
	commentID, err := newUUID()
	if err != nil {
		handler.internalError(c, err)
		return
	}
	now := handler.clock().UTC()
	comment := IssueComment{
		ID: commentID, CreatedAt: now, UpdatedAt: now, CreatedByID: &user.ID,
		ProjectID: projectID, WorkspaceID: workspaceID, IssueID: issueID,
		ActorID: &user.ID, ParentID: fields.parentID,
		CommentHTML: "<p></p>", CommentJSON: emptyJSON(),
		Attachments: pq.StringArray{}, Access: commentAccessInternal,
	}
	if fields.hasCommentHTML {
		comment.CommentHTML = fields.commentHTML
	}
	if fields.hasCommentJSON {
		comment.CommentJSON = fields.commentJSON
	}
	if fields.hasAccess {
		comment.Access = fields.access
	}
	if fields.hasExternalSource {
		comment.ExternalSource = fields.externalSource
	}
	if fields.hasExternalID {
		comment.ExternalID = fields.externalID
	}
	// IssueComment.save derives the stripped text and keeps a Description row.
	comment.CommentStripped = strippedComment(comment.CommentHTML)
	err = handler.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&comment).Error; err != nil {
			return err
		}
		descriptionID, err := newUUID()
		if err != nil {
			return err
		}
		stripped := comment.CommentStripped
		description := Description{
			ID: descriptionID, CreatedAt: now, UpdatedAt: now, CreatedByID: &user.ID,
			WorkspaceID: workspaceID, ProjectID: &projectID,
			DescriptionJSON: comment.CommentJSON, DescriptionHTML: comment.CommentHTML,
			DescriptionStripped: &stripped,
		}
		if err := tx.Create(&description).Error; err != nil {
			return err
		}
		comment.DescriptionID = &description.ID
		return tx.Model(&IssueComment{}).Where("id = ?", comment.ID).
			Update("description_id", description.ID).Error
	})
	if err != nil {
		if isUniqueViolation(err) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "The payload is not valid"})
			return
		}
		handler.internalError(c, err)
		return
	}
	data, err := handler.issueCommentJSON(c.Request.Context(), comment, nil)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	serialized, err := json.Marshal(data)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	requestedData := string(serialized)
	if err := handler.publishIssueActivity(c, issueActivity{
		Type: "comment.activity.created", RequestedData: &requestedData,
		ActorID: user.ID, IssueID: issueID, ProjectID: projectID,
		Notification: true, Origin: handler.origin(), Epoch: now,
	}); err != nil {
		handler.internalError(c, err)
		return
	}
	if handler.tasks != nil {
		err := handler.tasks.PublishModelActivity(c.Request.Context(), "issue_comment", comment.ID,
			decodeRawFields(body), nil, user.ID, slug, handler.origin())
		if err != nil {
			handler.internalError(c, err)
			return
		}
	}
	drf.Respond(c, http.StatusCreated, data)
}

func (handler *Handler) commentPatch(c *gin.Context, user *auth.User) {
	slug, projectID, issueID := c.Param("slug"), c.Param("id"), c.Param("issue")
	comment, found, err := handler.issueCommentByID(c.Request.Context(), slug, projectID, issueID, c.Param("comment"))
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		handler.notFound(c)
		return
	}
	if !handler.requireCommentAuthorOrAdmin(c, user, comment) {
		return
	}
	before, err := handler.issueCommentJSON(c.Request.Context(), comment, nil)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	snapshot, err := json.Marshal(before)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	currentInstance := string(snapshot)

	body, ok := handler.issueLinkBody(c)
	if !ok {
		return
	}
	requested, err := json.Marshal(decodeRawFields(body))
	if err != nil {
		handler.internalError(c, err)
		return
	}
	requestedData := string(requested)
	fields, ok := handler.issueCommentFieldsFrom(c, body)
	if !ok {
		return
	}
	now := handler.clock().UTC()
	updates := map[string]any{"updated_at": now, "updated_by_id": user.ID}
	descriptionUpdates := map[string]any{}
	if fields.hasCommentHTML {
		updates["comment_html"] = fields.commentHTML
		stripped := strippedComment(fields.commentHTML)
		updates["comment_stripped"] = stripped
		// Django only touches the description for the fields that changed.
		if fields.commentHTML != comment.CommentHTML {
			descriptionUpdates["description_html"] = fields.commentHTML
			descriptionUpdates["description_stripped"] = stripped
		}
		// edited_at is stamped only when the html differs from what is stored.
		if fields.commentHTML != comment.CommentHTML {
			updates["edited_at"] = now
		}
	}
	if fields.hasCommentJSON {
		updates["comment_json"] = fields.commentJSON
		if string(fields.commentJSON) != string(comment.CommentJSON) {
			descriptionUpdates["description_json"] = fields.commentJSON
		}
	}
	if fields.hasAccess {
		updates["access"] = fields.access
	}
	if fields.hasParent {
		updates["parent_id"] = fields.parentID
	}
	if fields.hasExternalSource {
		updates["external_source"] = fields.externalSource
	}
	if fields.hasExternalID {
		updates["external_id"] = fields.externalID
	}
	err = handler.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&IssueComment{}).Where("id = ?", comment.ID).Updates(updates).Error; err != nil {
			return err
		}
		if len(descriptionUpdates) == 0 || comment.DescriptionID == nil {
			return nil
		}
		descriptionUpdates["updated_by_id"] = user.ID
		descriptionUpdates["updated_at"] = now
		return tx.Model(&Description{}).Where("id = ?", *comment.DescriptionID).Updates(descriptionUpdates).Error
	})
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if err := handler.db.WithContext(c.Request.Context()).Where("id = ?", comment.ID).Take(&comment).Error; err != nil {
		handler.internalError(c, err)
		return
	}
	if err := handler.publishIssueActivity(c, issueActivity{
		Type: "comment.activity.updated", RequestedData: &requestedData, CurrentInstance: &currentInstance,
		ActorID: user.ID, IssueID: issueID, ProjectID: projectID,
		Notification: true, Origin: handler.origin(), Epoch: now,
	}); err != nil {
		handler.internalError(c, err)
		return
	}
	if handler.tasks != nil {
		err := handler.tasks.PublishModelActivity(c.Request.Context(), "issue_comment", comment.ID,
			decodeRawFields(body), &currentInstance, user.ID, slug, handler.origin())
		if err != nil {
			handler.internalError(c, err)
			return
		}
	}
	data, err := handler.issueCommentJSON(c.Request.Context(), comment, nil)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, data)
}

func (handler *Handler) commentDelete(c *gin.Context, user *auth.User) {
	slug, projectID, issueID := c.Param("slug"), c.Param("id"), c.Param("issue")
	comment, found, err := handler.issueCommentByID(c.Request.Context(), slug, projectID, issueID, c.Param("comment"))
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		handler.notFound(c)
		return
	}
	if !handler.requireCommentAuthorOrAdmin(c, user, comment) {
		return
	}
	before, err := handler.issueCommentJSON(c.Request.Context(), comment, nil)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	snapshot, err := json.Marshal(before)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	currentInstance := string(snapshot)
	requested, err := json.Marshal(map[string]any{"comment_id": comment.ID})
	if err != nil {
		handler.internalError(c, err)
		return
	}
	requestedData := string(requested)
	now := handler.clock().UTC()
	err = handler.db.WithContext(c.Request.Context()).Model(&IssueComment{}).Where("id = ?", comment.ID).
		Updates(map[string]any{"deleted_at": now, "updated_at": now, "updated_by_id": user.ID}).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if handler.tasks != nil {
		if err := handler.tasks.PublishSoftDeleteRelatedObjects(c.Request.Context(), "db", "issuecomment", comment.ID); err != nil {
			handler.internalError(c, err)
			return
		}
	}
	// Django queues the activity after the delete on this route.
	if err := handler.publishIssueActivity(c, issueActivity{
		Type: "comment.activity.deleted", RequestedData: &requestedData, CurrentInstance: &currentInstance,
		ActorID: user.ID, IssueID: issueID, ProjectID: projectID,
		Notification: true, Origin: handler.origin(), Epoch: now,
	}); err != nil {
		handler.internalError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// requireCommentAuthorOrAdmin is allow_permission(allowed_roles=[ADMIN],
// creator=True, model=IssueComment): the row's creator may always act, and
// otherwise a project admin may.
func (handler *Handler) requireCommentAuthorOrAdmin(c *gin.Context, user *auth.User, comment IssueComment) bool {
	member, found, err := handler.activeProjectMember(c.Request.Context(), c.Param("slug"), c.Param("id"), user.ID)
	if err != nil {
		handler.internalError(c, err)
		return false
	}
	if !found {
		c.JSON(http.StatusForbidden, gin.H{"error": "You don't have the required permissions."})
		return false
	}
	if comment.CreatedByID != nil && *comment.CreatedByID == user.ID {
		return true
	}
	if member.Role == roleAdmin {
		return true
	}
	workspaceRole, _, err := handler.workspaceMemberRole(c.Request.Context(), c.Param("slug"), user.ID)
	if err != nil {
		handler.internalError(c, err)
		return false
	}
	if workspaceRole == roleAdmin {
		return true
	}
	c.JSON(http.StatusForbidden, gin.H{"error": "You don't have the required permissions."})
	return false
}

// guestMayComment reproduces the guard on create: a guest is refused unless the
// project opens all features to guests or they raised the issue.
func (handler *Handler) guestMayComment(ctx context.Context, slug, projectID, issueID, userID string) (bool, error) {
	member, found, err := handler.activeProjectMember(ctx, slug, projectID, userID)
	if err != nil || !found {
		return found, err
	}
	if member.Role != roleGuest {
		return true, nil
	}
	var project Project
	if err := handler.db.WithContext(ctx).Where("id = ?", projectID).Take(&project).Error; err != nil {
		return false, err
	}
	if project.GuestViewAllFeatures {
		return true, nil
	}
	// A slice, and a nullable one. Pluck walks rows into a slice: handed a plain *string it never calls Next and fails with `sql: Scan called without calling Next` on every row, null or not -- so this check answered 500 rather than yes or no, for every guest, always. created_by_id is null for anything created outside a request, which BaseModel.save does whenever there is no user, so the element has to carry that too.
	var creator []sql.NullString
	err = handler.db.WithContext(ctx).Table("issues").Where("id = ?", issueID).
		Limit(1).Pluck("created_by_id", &creator).Error
	if err != nil {
		return false, err
	}
	return len(creator) > 0 && creator[0].Valid && creator[0].String == userID, nil
}

func (handler *Handler) isActiveProjectMember(ctx context.Context, slug, projectID, userID string) (bool, error) {
	_, found, err := handler.activeProjectMember(ctx, slug, projectID, userID)
	return found, err
}

func (handler *Handler) issueCommentByID(ctx context.Context, slug, projectID, issueID, commentID string) (IssueComment, bool, error) {
	var comment IssueComment
	err := handler.db.WithContext(ctx).
		Where("id = ? AND issue_id = ? AND project_id = ? AND workspace_id = (SELECT id FROM workspaces WHERE slug = ?) AND deleted_at IS NULL",
			commentID, issueID, projectID, slug).Take(&comment).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return IssueComment{}, false, nil
	}
	return comment, err == nil, err
}

type issueCommentInput struct {
	commentHTML       string
	commentJSON       auth.JSONValue
	access            string
	parentID          *string
	externalSource    *string
	externalID        *string
	hasCommentHTML    bool
	hasCommentJSON    bool
	hasAccess         bool
	hasParent         bool
	hasExternalSource bool
	hasExternalID     bool
}

func (handler *Handler) issueCommentFieldsFrom(c *gin.Context, body map[string]json.RawMessage) (issueCommentInput, bool) {
	result := issueCommentInput{}
	if raw, exists := body["comment_html"]; exists {
		result.hasCommentHTML = true
		if string(raw) == "null" {
			c.JSON(http.StatusBadRequest, gin.H{"comment_html": []string{"This field may not be null."}})
			return issueCommentInput{}, false
		}
		var value string
		if json.Unmarshal(raw, &value) != nil {
			c.JSON(http.StatusBadRequest, gin.H{"comment_html": []string{"Not a valid string."}})
			return issueCommentInput{}, false
		}
		// IssueCommentSerializer.validate runs the content through nh3 and
		// stores what comes back, refusing only when the helper reports the
		// content unusable.
		if value != "" {
			valid, _, cleaned := htmlsanitizer.ValidateHTMLContent(value)
			if !valid {
				c.JSON(http.StatusBadRequest, gin.H{"comment_html": "HTML content is not valid"})
				return issueCommentInput{}, false
			}
			if cleaned != nil {
				value = *cleaned
			}
		}
		result.commentHTML = value
	}
	if raw, exists := body["comment_json"]; exists {
		result.hasCommentJSON = true
		if string(raw) == "null" {
			c.JSON(http.StatusBadRequest, gin.H{"comment_json": []string{"This field may not be null."}})
			return issueCommentInput{}, false
		}
		if !json.Valid(raw) {
			c.JSON(http.StatusBadRequest, gin.H{"comment_json": []string{"Value must be valid JSON."}})
			return issueCommentInput{}, false
		}
		result.commentJSON = auth.JSONValue(append([]byte(nil), raw...))
	}
	if raw, exists := body["access"]; exists {
		result.hasAccess = true
		var value string
		if json.Unmarshal(raw, &value) != nil || (value != "INTERNAL" && value != "EXTERNAL") {
			var decoded any
			_ = json.Unmarshal(raw, &decoded)
			c.JSON(http.StatusBadRequest, gin.H{"access": []string{invalidChoice(decoded)}})
			return issueCommentInput{}, false
		}
		result.access = value
	}
	if raw, exists := body["parent"]; exists {
		result.hasParent = true
		if !blankRelation(raw) {
			var value string
			if json.Unmarshal(raw, &value) != nil {
				c.JSON(http.StatusBadRequest, gin.H{"parent": []string{`“` + value + `” is not a valid UUID.`}})
				return issueCommentInput{}, false
			}
			canonical, valid := canonicalUUID(value)
			if !valid {
				c.JSON(http.StatusBadRequest, gin.H{"parent": []string{`“` + value + `” is not a valid UUID.`}})
				return issueCommentInput{}, false
			}
			result.parentID = &canonical
		}
	}
	for _, external := range []struct {
		name  string
		value **string
		set   *bool
	}{
		{name: "external_source", value: &result.externalSource, set: &result.hasExternalSource},
		{name: "external_id", value: &result.externalID, set: &result.hasExternalID},
	} {
		raw, exists := body[external.name]
		if !exists {
			continue
		}
		*external.set = true
		if string(raw) == "null" {
			continue
		}
		var value string
		if json.Unmarshal(raw, &value) != nil {
			c.JSON(http.StatusBadRequest, gin.H{external.name: []string{"Not a valid string."}})
			return issueCommentInput{}, false
		}
		if utf8.RuneCountInString(value) > 255 {
			c.JSON(http.StatusBadRequest, gin.H{external.name: []string{"Ensure this field has no more than 255 characters."}})
			return issueCommentInput{}, false
		}
		stored := value
		*external.value = &stored
	}
	return result, true
}

var commentTagPattern = regexp.MustCompile(`(?s)<[^>]*>`)

// strippedComment is IssueComment.save's comment_stripped, which is Django's
// strip_tags over the html and an empty string when the html is empty.
func strippedComment(html string) string {
	if html == "" {
		return ""
	}
	return commentTagPattern.ReplaceAllString(html, "")
}

// invalidChoice is the message a field sends back when the value is not one of the ones it accepts. The value is quoted with strconv.Quote so that a value containing a quote of its own reads unambiguously instead of running the message together.
func invalidChoice(value any) string {
	return strconv.Quote(stringify(value)) + " is not a valid choice."
}

func stringify(value any) string {
	if value == nil {
		return "None"
	}
	if text, ok := value.(string); ok {
		return text
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return strings.Trim(string(encoded), `"`)
}

// issueCommentJSON is IssueCommentSerializer: every model field plus the actor,
// issue, project and workspace details, the comment's reactions, and is_member.
func (handler *Handler) issueCommentJSON(ctx context.Context, comment IssueComment, isMember *bool) (gin.H, error) {
	data := gin.H{
		"id": comment.ID, "created_at": comment.CreatedAt, "updated_at": comment.UpdatedAt,
		"created_by": comment.CreatedByID, "updated_by": comment.UpdatedByID,
		"deleted_at": comment.DeletedAt, "project": comment.ProjectID,
		"workspace": comment.WorkspaceID, "issue": comment.IssueID,
		"actor": comment.ActorID, "parent": comment.ParentID,
		"description":      comment.DescriptionID,
		"comment_stripped": comment.CommentStripped,
		"comment_json":     decodeJSON(comment.CommentJSON),
		"comment_html":     comment.CommentHTML,
		"attachments":      []string(comment.Attachments),
		"access":           comment.Access,
		"external_source":  comment.ExternalSource, "external_id": comment.ExternalID,
		"edited_at": comment.EditedAt, "is_member": isMember,
	}
	if data["attachments"] == nil {
		data["attachments"] = []string{}
	}
	if comment.ActorID != nil {
		var actor auth.User
		if err := handler.db.WithContext(ctx).Where("id = ?", *comment.ActorID).Take(&actor).Error; err == nil {
			data["actor_detail"] = liteUserJSON(actor, false)
		}
	}
	if _, ok := data["actor_detail"]; !ok {
		data["actor_detail"] = nil
	}
	project, err := handler.projectLite(ctx, comment.ProjectID)
	if err != nil {
		return nil, err
	}
	data["project_detail"] = project
	workspace, err := handler.workspaceLite(ctx, comment.WorkspaceID)
	if err != nil {
		return nil, err
	}
	data["workspace_detail"] = workspace
	issue, err := handler.issueFlatJSON(ctx, comment.IssueID)
	if err != nil {
		return nil, err
	}
	data["issue_detail"] = issue
	reactions, err := handler.commentReactionsJSON(ctx, comment.ID)
	if err != nil {
		return nil, err
	}
	data["comment_reactions"] = reactions
	return data, nil
}

// issueFlatJSON is IssueFlatSerializer, the narrow issue shape the comment
// serializer nests.
func (handler *Handler) issueFlatJSON(ctx context.Context, issueID string) (gin.H, error) {
	var row struct {
		ID              string  `gorm:"column:id"`
		Name            string  `gorm:"column:name"`
		DescriptionJSON []byte  `gorm:"column:description_json"`
		DescriptionHTML *string `gorm:"column:description_html"`
		Priority        string  `gorm:"column:priority"`
		StartDate       *string `gorm:"column:start_date"`
		TargetDate      *string `gorm:"column:target_date"`
		SequenceID      int64   `gorm:"column:sequence_id"`
		SortOrder       float64 `gorm:"column:sort_order"`
		IsDraft         bool    `gorm:"column:is_draft"`
	}
	err := handler.db.WithContext(ctx).Table("issues").Where("id = ?", issueID).
		Select("id, name, description_json, description_html, priority, start_date, target_date, sequence_id, sort_order, is_draft").
		Take(&row).Error
	if err != nil {
		return nil, err
	}
	return gin.H{
		"id": row.ID, "name": row.Name, "description_json": decodeJSON(row.DescriptionJSON),
		"description_html": row.DescriptionHTML, "priority": row.Priority,
		"start_date": row.StartDate, "target_date": row.TargetDate,
		"sequence_id": row.SequenceID, "sort_order": row.SortOrder, "is_draft": row.IsDraft,
	}, nil
}
