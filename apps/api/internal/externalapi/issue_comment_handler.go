package externalapi

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api/internal/auth"
	"github.com/yldm-tech/pace/apps/api/internal/drf"
	"github.com/yldm-tech/pace/apps/api/internal/htmlsanitizer"
	"gorm.io/gorm"
)

func (handler *Handler) registerIssueCommentRoutes(router gin.IRouter) {
	base := "/api/v1/workspaces/:slug/projects/:project/"
	for _, name := range []string{"issues", "work-items"} {
		comments := base + name + "/:issue/comments/"
		router.GET(comments, handler.authenticated(handler.issueCommentList))
		router.POST(comments, handler.authenticated(handler.issueCommentCreate))
		router.GET(comments+":comment/", handler.authenticated(handler.issueCommentRetrieve))
		router.PATCH(comments+":comment/", handler.authenticated(handler.issueCommentUpdate))
		router.DELETE(comments+":comment/", handler.authenticated(handler.issueCommentDestroy))
	}
}

// IssueComment is the db.IssueComment table as the external API sees it.
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
	CommentStripped *string    `gorm:"column:comment_stripped"`
	CommentJSON     []byte     `gorm:"column:comment_json;type:jsonb"`
	CommentHTML     string     `gorm:"column:comment_html"`
	Attachments     []byte     `gorm:"column:attachments;type:text[]"`
	Access          string     `gorm:"column:access"`
	EditedAt        *time.Time `gorm:"column:edited_at"`
	ExternalSource  *string    `gorm:"column:external_source"`
	ExternalID      *string    `gorm:"column:external_id"`
	Description     []byte     `gorm:"column:description;type:jsonb"`
	ParentID        *string    `gorm:"column:parent_id;type:uuid"`
}

func (IssueComment) TableName() string { return "issue_comments" }

// commentRow is the comment with the membership flag the queryset annotates on.
type commentRow struct {
	IssueComment
	IsMember bool `gorm:"column:is_member"`
}

func (handler *Handler) issueCommentList(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireProjectLite(c, user) {
		return
	}
	var rows []commentRow
	err := handler.issueCommentScope(c, user).Order("cm.created_at DESC").Scan(&rows).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	fields := requestedFields(c)
	results := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		results = append(results, narrow(issueCommentJSON(row), fields))
	}
	handler.respondPaged(c, results)
}

func (handler *Handler) issueCommentRetrieve(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireProjectLite(c, user) {
		return
	}
	row, found, err := handler.issueCommentByID(c, user)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !found {
		notFound(c)
		return
	}
	drf.Respond(c, http.StatusOK, narrow(issueCommentJSON(row), requestedFields(c)))
}

// issueCommentCreate adds a comment.
//
// An integration may name both the **author** and the **creation time**, which is what lets it import a conversation with its original timestamps. Neither is checked.
func (handler *Handler) issueCommentCreate(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireProjectLite(c, user) {
		return
	}
	slug, projectID, issueID := c.Param("slug"), c.Param("project"), c.Param("issue")
	var payload map[string]any
	if err := c.ShouldBindJSON(&payload); err != nil {
		invalidPayload(c)
		return
	}
	externalID, _ := payload["external_id"].(string)
	externalSource, _ := payload["external_source"].(string)
	if externalID != "" && externalSource != "" {
		existing, err := handler.commentWithExternalID(c, slug, projectID, externalSource, externalID, "")
		if err != nil {
			handler.serverError(c, err)
			return
		}
		if existing != "" {
			c.JSON(http.StatusConflict, gin.H{
				"error": "Work item comment with the same external id and external source already exists",
				"id":    existing,
			})
			return
		}
	}

	html, ok := sanitizedComment(c, payload)
	if !ok {
		return
	}
	var workspaceIDs []string
	err := handler.db.WithContext(c.Request.Context()).Table("projects").
		Where("id = ?", projectID).Limit(1).Pluck("workspace_id", &workspaceIDs).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if len(workspaceIDs) == 0 {
		notFound(c)
		return
	}
	identifier, err := newUUID()
	if err != nil {
		handler.serverError(c, err)
		return
	}
	now := handler.clock().UTC()
	created := now
	if value, ok := payload["created_at"].(string); ok && value != "" {
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"created_at": []string{"Datetime has wrong format."}})
			return
		}
		created = parsed
	}
	author := user.ID
	if value, ok := payload["created_by"].(string); ok && value != "" {
		author = value
	}
	comment := IssueComment{
		ID: identifier, CreatedAt: created, UpdatedAt: now,
		CreatedByID: &author, UpdatedByID: &user.ID, ActorID: &author,
		ProjectID: projectID, WorkspaceID: workspaceIDs[0], IssueID: issueID,
		CommentHTML: html, CommentJSON: []byte("{}"), Description: []byte("{}"), Access: "INTERNAL",
	}
	if value, ok := payload["access"].(string); ok && value != "" {
		comment.Access = value
	}
	if externalID != "" {
		comment.ExternalID = &externalID
	}
	if externalSource != "" {
		comment.ExternalSource = &externalSource
	}
	if value, present := payload["comment_json"]; present {
		if encoded, err := json.Marshal(value); err == nil {
			comment.CommentJSON = encoded
		}
	}
	if err := handler.db.WithContext(c.Request.Context()).Create(&comment).Error; err != nil {
		handler.serverError(c, err)
		return
	}
	// The model activity is attributed to the **caller** while the issue activity is attributed to the named author, so an imported comment is announced by one and recorded by the other.
	if handler.tasks != nil {
		requested, err := json.Marshal(payload)
		if err != nil {
			handler.serverError(c, err)
			return
		}
		err = handler.tasks.PublishModelActivity(c.Request.Context(), "issue_comment", comment.ID,
			string(requested), nil, user.ID, slug, handler.origin(c))
		if err != nil {
			handler.serverError(c, err)
			return
		}
	}
	drf.Respond(c, http.StatusCreated, issueCommentJSON(commentRow{IssueComment: comment}))
}

// issueCommentUpdate edits a comment.
//
// The external-id check compares against the comment's own id, so rewriting a comment with the id it already has is not a conflict — and the source it compares against falls back to the comment's when the request does not name one.
func (handler *Handler) issueCommentUpdate(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireProjectLite(c, user) {
		return
	}
	slug, projectID := c.Param("slug"), c.Param("project")
	var payload map[string]any
	if err := c.ShouldBindJSON(&payload); err != nil {
		invalidPayload(c)
		return
	}
	row, found, err := handler.issueCommentByID(c, user)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !found {
		notFound(c)
		return
	}
	comment := row.IssueComment

	if externalID, ok := payload["external_id"].(string); ok && externalID != "" &&
		(comment.ExternalID == nil || *comment.ExternalID != externalID) {
		source := ""
		if comment.ExternalSource != nil {
			source = *comment.ExternalSource
		}
		if value, ok := payload["external_source"].(string); ok {
			source = value
		}
		existing, err := handler.commentWithExternalID(c, slug, projectID, source, externalID, "")
		if err != nil {
			handler.serverError(c, err)
			return
		}
		if existing != "" {
			c.JSON(http.StatusConflict, gin.H{
				"error": "Work item comment with the same external id and external source already exists",
				"id":    comment.ID,
			})
			return
		}
	}

	updates := map[string]any{}
	if _, present := payload["comment_html"]; present {
		html, ok := sanitizedComment(c, payload)
		if !ok {
			return
		}
		updates["comment_html"] = html
		comment.CommentHTML = html
	}
	if value, ok := payload["access"].(string); ok && value != "" {
		updates["access"] = value
		comment.Access = value
	}
	for payloadName, column := range map[string]string{"external_source": "external_source", "external_id": "external_id"} {
		value, present := payload[payloadName]
		if !present {
			continue
		}
		if text, ok := value.(string); ok {
			updates[column] = text
		} else {
			updates[column] = nil
		}
	}
	now := handler.clock().UTC()
	updates["updated_at"] = now
	updates["updated_by_id"] = user.ID
	err = handler.db.WithContext(c.Request.Context()).Model(&IssueComment{}).
		Where("id = ?", comment.ID).Updates(updates).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	// The update answers 200 where the create answers 201, which is the ordinary pair — unlike the state routes, where both answer 200.
	drf.Respond(c, http.StatusOK, issueCommentJSON(commentRow{IssueComment: comment, IsMember: row.IsMember}))
}

func (handler *Handler) issueCommentDestroy(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireProjectLite(c, user) {
		return
	}
	row, found, err := handler.issueCommentByID(c, user)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !found {
		notFound(c)
		return
	}
	err = handler.db.WithContext(c.Request.Context()).Model(&IssueComment{}).
		Where("id = ?", row.ID).Update("deleted_at", handler.clock().UTC()).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// sanitizedComment runs the html through the sanitizer, refusing what it rejects. It reports whether the request may carry on.
func sanitizedComment(c *gin.Context, payload map[string]any) (string, bool) {
	html, _ := payload["comment_html"].(string)
	if html == "" {
		return "<p></p>", true
	}
	valid, _, cleaned := htmlsanitizer.ValidateHTMLContent(html)
	if !valid {
		// Unlike the sticky, which answers with a message of its own, this one hands back the sanitizer's.
		c.JSON(http.StatusBadRequest, gin.H{"comment_html": []string{"html content is not valid"}})
		return "", false
	}
	if cleaned != nil {
		return *cleaned, true
	}
	return html, true
}

func (handler *Handler) commentWithExternalID(c *gin.Context, slug, projectID, source, external, exclude string) (string, error) {
	query := handler.db.WithContext(c.Request.Context()).Table("issue_comments cm").
		Joins("JOIN workspaces w ON w.id = cm.workspace_id").
		Where("w.slug = ? AND cm.project_id = ? AND cm.external_source = ? AND cm.external_id = ? AND cm.deleted_at IS NULL",
			slug, projectID, source, external)
	if exclude != "" {
		query = query.Where("cm.id <> ?", exclude)
	}
	var identifiers []string
	if err := query.Limit(1).Pluck("cm.id", &identifiers).Error; err != nil {
		return "", err
	}
	if len(identifiers) == 0 {
		return "", nil
	}
	return identifiers[0], nil
}

// issueCommentScope is the queryset every comment route reads through.
func (handler *Handler) issueCommentScope(c *gin.Context, user *auth.User) *gorm.DB {
	return handler.db.WithContext(c.Request.Context()).Table("issue_comments cm").
		Select(`cm.*, EXISTS (SELECT 1 FROM project_members im
			JOIN workspaces iw ON iw.id = im.workspace_id
			WHERE im.project_id = ? AND im.member_id = ? AND im.is_active = TRUE AND iw.slug = ?) AS is_member`,
			c.Param("project"), user.ID, c.Param("slug")).
		Joins("JOIN workspaces w ON w.id = cm.workspace_id").
		Joins("JOIN projects p ON p.id = cm.project_id AND p.archived_at IS NULL").
		Joins("JOIN project_members pm ON pm.project_id = cm.project_id AND pm.member_id = ? AND pm.is_active = TRUE", user.ID).
		Where("w.slug = ? AND cm.project_id = ? AND cm.issue_id = ? AND cm.deleted_at IS NULL",
			c.Param("slug"), c.Param("project"), c.Param("issue"))
}

func (handler *Handler) issueCommentByID(c *gin.Context, user *auth.User) (commentRow, bool, error) {
	var rows []commentRow
	err := handler.issueCommentScope(c, user).Where("cm.id = ?", c.Param("comment")).Limit(1).Scan(&rows).Error
	if err != nil || len(rows) == 0 {
		return commentRow{}, false, err
	}
	return rows[0], true, nil
}

// issueCommentJSON is the external API's IssueCommentSerializer.
//
// It **excludes** two columns rather than listing what it wants, so the stripped text and the document tree are the only things held back — everything else the model grows arrives automatically.
func issueCommentJSON(row commentRow) gin.H {
	return gin.H{
		"id": row.ID, "created_at": row.CreatedAt, "updated_at": row.UpdatedAt,
		"created_by": row.CreatedByID, "updated_by": row.UpdatedByID, "deleted_at": row.DeletedAt,
		"comment_html": row.CommentHTML, "attachments": decodeJSON(row.Attachments),
		"access": row.Access, "edited_at": row.EditedAt,
		"external_source": row.ExternalSource, "external_id": row.ExternalID,
		"description": decodeJSON(row.Description), "parent": row.ParentID,
		"project": row.ProjectID, "workspace": row.WorkspaceID, "issue": row.IssueID,
		"actor": row.ActorID, "is_member": row.IsMember,
	}
}
