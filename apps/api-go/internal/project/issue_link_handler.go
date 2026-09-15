package project

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"github.com/yldm-tech/pace/apps/api-go/internal/drf"
	"github.com/yldm-tech/pace/apps/api-go/internal/validate"
	"gorm.io/gorm"
)

func (handler *Handler) registerIssueLinkRoutes(router gin.IRouter) {
	router.GET("/api/workspaces/:slug/projects/:id/issues/:issue/issue-links/", handler.authenticatedIssueUUID(handler.issueLinkList))
	router.POST("/api/workspaces/:slug/projects/:id/issues/:issue/issue-links/", handler.authenticatedIssueUUID(handler.issueLinkCreate))
	router.GET("/api/workspaces/:slug/projects/:id/issues/:issue/issue-links/:link/", handler.authenticatedIssueLinkUUID(handler.issueLinkRetrieve))
	router.PATCH("/api/workspaces/:slug/projects/:id/issues/:issue/issue-links/:link/", handler.authenticatedIssueLinkUUID(handler.issueLinkPatch))
	router.PUT("/api/workspaces/:slug/projects/:id/issues/:issue/issue-links/:link/", handler.authenticatedIssueLinkUUID(handler.fullUpdate("link", handler.issueLinkPatch)))
	router.DELETE("/api/workspaces/:slug/projects/:id/issues/:issue/issue-links/:link/", handler.authenticatedIssueLinkUUID(handler.issueLinkDelete))
}

func (handler *Handler) authenticatedIssueLinkUUID(next func(*gin.Context, *auth.User)) gin.HandlerFunc {
	inner := handler.authenticatedIssueUUID(next)
	return func(c *gin.Context) {
		if !uuidPattern.MatchString(c.Param("link")) {
			handler.notFound(c)
			return
		}
		inner(c)
	}
}

func (handler *Handler) issueLinkList(c *gin.Context, user *auth.User) {
	if !handler.requireProjectEntityRead(c, user) {
		return
	}
	var links []IssueLink
	err := handler.db.WithContext(c.Request.Context()).
		Where(`issue_id = ? AND project_id = ? AND workspace_id = (SELECT id FROM workspaces WHERE slug = ?)
			AND deleted_at IS NULL
			AND EXISTS (SELECT 1 FROM projects p WHERE p.id = issue_links.project_id AND p.archived_at IS NULL AND p.deleted_at IS NULL)
			AND EXISTS (SELECT 1 FROM project_members pm WHERE pm.project_id = issue_links.project_id AND pm.member_id = ? AND pm.is_active = TRUE AND pm.deleted_at IS NULL)`,
			c.Param("issue"), c.Param("id"), c.Param("slug"), user.ID).
		Order("created_at DESC").Find(&links).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	response := make([]gin.H, 0, len(links))
	for _, link := range links {
		data, err := handler.issueLinkJSON(c.Request.Context(), link)
		if err != nil {
			handler.internalError(c, err)
			return
		}
		response = append(response, data)
	}
	drf.Respond(c, http.StatusOK, response)
}

func (handler *Handler) issueLinkRetrieve(c *gin.Context, user *auth.User) {
	if !handler.requireProjectEntityRead(c, user) {
		return
	}
	link, found, err := handler.issueLinkByID(c.Request.Context(), c.Param("slug"), c.Param("id"), c.Param("issue"), c.Param("link"))
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		handler.notFound(c)
		return
	}
	data, err := handler.issueLinkJSON(c.Request.Context(), link)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, data)
}

func (handler *Handler) issueLinkCreate(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember) {
		return
	}
	slug, projectID, issueID := c.Param("slug"), c.Param("id"), c.Param("issue")
	workspaceID, ok := handler.projectWorkspaceID(c, slug, projectID)
	if !ok {
		return
	}
	fields, ok := handler.issueLinkFields(c, false)
	if !ok {
		return
	}
	linkID, err := newUUID()
	if err != nil {
		handler.internalError(c, err)
		return
	}
	now := handler.clock().UTC()
	metadata := fields.metadata
	if !fields.hasMetadata {
		metadata = emptyJSON()
	}
	link := IssueLink{
		ID: linkID, CreatedAt: now, UpdatedAt: now, CreatedByID: &user.ID,
		ProjectID: projectID, WorkspaceID: workspaceID, IssueID: issueID,
		Title: fields.title, URL: fields.url, Metadata: metadata,
	}
	if err := handler.db.WithContext(c.Request.Context()).Create(&link).Error; err != nil {
		if isUniqueViolation(err) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "The payload is not valid"})
			return
		}
		handler.internalError(c, err)
		return
	}
	data, err := handler.issueLinkJSON(c.Request.Context(), link)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if handler.tasks != nil {
		// Django crawls the page for a title, then records the activity.
		if err := handler.tasks.PublishCrawlLinkTitle(c.Request.Context(), link.ID, link.URL); err != nil {
			handler.internalError(c, err)
			return
		}
	}
	requested, err := json.Marshal(data)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	requestedData := string(requested)
	if err := handler.publishIssueActivity(c, issueActivity{
		Type: "link.activity.created", RequestedData: &requestedData,
		ActorID: user.ID, IssueID: issueID, ProjectID: projectID,
		Notification: true, Origin: handler.origin(), Epoch: now,
	}); err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusCreated, data)
}

func (handler *Handler) issueLinkPatch(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember) {
		return
	}
	slug, projectID, issueID := c.Param("slug"), c.Param("id"), c.Param("issue")
	link, found, err := handler.issueLinkByID(c.Request.Context(), slug, projectID, issueID, c.Param("link"))
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		handler.notFound(c)
		return
	}
	// Django snapshots both the request body and the serialized row before it
	// applies the change.
	before, err := handler.issueLinkJSON(c.Request.Context(), link)
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
	previousURL := link.URL

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

	fields, ok := handler.issueLinkFieldsFrom(c, body, true)
	if !ok {
		return
	}
	now := handler.clock().UTC()
	updates := map[string]any{"updated_at": now, "updated_by_id": user.ID}
	if fields.hasTitle {
		updates["title"] = fields.title
	}
	if fields.hasURL {
		updates["url"] = fields.url
	}
	if fields.hasMetadata {
		updates["metadata"] = fields.metadata
	}
	err = handler.db.WithContext(c.Request.Context()).Model(&IssueLink{}).Where("id = ?", link.ID).Updates(updates).Error
	if err != nil {
		if isUniqueViolation(err) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "The payload is not valid"})
			return
		}
		handler.internalError(c, err)
		return
	}
	if err := handler.db.WithContext(c.Request.Context()).Where("id = ?", link.ID).Take(&link).Error; err != nil {
		handler.internalError(c, err)
		return
	}
	if handler.tasks != nil && link.URL != "" && link.URL != previousURL {
		// Only a changed URL is crawled again.
		if err := handler.tasks.PublishCrawlLinkTitle(c.Request.Context(), link.ID, link.URL); err != nil {
			handler.internalError(c, err)
			return
		}
	}
	if err := handler.publishIssueActivity(c, issueActivity{
		Type: "link.activity.updated", RequestedData: &requestedData, CurrentInstance: &currentInstance,
		ActorID: user.ID, IssueID: issueID, ProjectID: projectID,
		Notification: true, Origin: handler.origin(), Epoch: now,
	}); err != nil {
		handler.internalError(c, err)
		return
	}
	data, err := handler.issueLinkJSON(c.Request.Context(), link)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, data)
}

func (handler *Handler) issueLinkDelete(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember) {
		return
	}
	slug, projectID, issueID := c.Param("slug"), c.Param("id"), c.Param("issue")
	link, found, err := handler.issueLinkByID(c.Request.Context(), slug, projectID, issueID, c.Param("link"))
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		handler.notFound(c)
		return
	}
	before, err := handler.issueLinkJSON(c.Request.Context(), link)
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
	requested, err := json.Marshal(map[string]any{"link_id": link.ID})
	if err != nil {
		handler.internalError(c, err)
		return
	}
	requestedData := string(requested)
	now := handler.clock().UTC()
	if err := handler.publishIssueActivity(c, issueActivity{
		Type: "link.activity.deleted", RequestedData: &requestedData, CurrentInstance: &currentInstance,
		ActorID: user.ID, IssueID: issueID, ProjectID: projectID,
		Notification: true, Origin: handler.origin(), Epoch: now,
	}); err != nil {
		handler.internalError(c, err)
		return
	}
	err = handler.db.WithContext(c.Request.Context()).Model(&IssueLink{}).Where("id = ?", link.ID).Updates(map[string]any{
		"deleted_at": now, "updated_at": now, "updated_by_id": user.ID,
	}).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if handler.tasks != nil {
		if err := handler.tasks.PublishSoftDeleteRelatedObjects(c.Request.Context(), "db", "issuelink", link.ID); err != nil {
			handler.internalError(c, err)
			return
		}
	}
	c.Status(http.StatusNoContent)
}

// requireProjectEntityRead is ProjectEntityPermission's safe method branch: an
// active membership of the project, whatever the role.
func (handler *Handler) requireProjectEntityRead(c *gin.Context, user *auth.User) bool {
	return handler.requireProjectMembership(c, user)
}

func (handler *Handler) issueLinkByID(ctx context.Context, slug, projectID, issueID, linkID string) (IssueLink, bool, error) {
	var link IssueLink
	err := handler.db.WithContext(ctx).
		Where(`id = ? AND issue_id = ? AND project_id = ? AND workspace_id = (SELECT id FROM workspaces WHERE slug = ?) AND deleted_at IS NULL`,
			linkID, issueID, projectID, slug).Take(&link).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return IssueLink{}, false, nil
	}
	return link, err == nil, err
}

type issueLinkInput struct {
	title       *string
	url         string
	metadata    auth.JSONValue
	hasTitle    bool
	hasURL      bool
	hasMetadata bool
}

func (handler *Handler) issueLinkBody(c *gin.Context) (map[string]json.RawMessage, bool) {
	var body map[string]json.RawMessage
	if err := c.ShouldBindJSON(&body); err != nil {
		handler.invalidDetail(c)
		return nil, false
	}
	return body, true
}

func (handler *Handler) issueLinkFields(c *gin.Context, partial bool) (issueLinkInput, bool) {
	body, ok := handler.issueLinkBody(c)
	if !ok {
		return issueLinkInput{}, false
	}
	return handler.issueLinkFieldsFrom(c, body, partial)
}

// issueLinkFieldsFrom is IssueLinkSerializer, which handles the URL exactly the
// way the workspace quick link serializer does.
func (handler *Handler) issueLinkFieldsFrom(c *gin.Context, body map[string]json.RawMessage, partial bool) (issueLinkInput, bool) {
	result := issueLinkInput{}
	if raw, exists := body["title"]; exists {
		result.hasTitle = true
		if string(raw) != "null" {
			var value string
			if json.Unmarshal(raw, &value) != nil {
				c.JSON(http.StatusBadRequest, gin.H{"title": []string{"Not a valid string."}})
				return issueLinkInput{}, false
			}
			value = strings.TrimSpace(value)
			if value != "" && utf8.RuneCountInString(value) > 255 {
				c.JSON(http.StatusBadRequest, gin.H{"title": []string{"Ensure this field has no more than 255 characters."}})
				return issueLinkInput{}, false
			}
			result.title = &value
		}
	}
	if raw, exists := body["url"]; exists {
		result.hasURL = true
		if string(raw) == "null" {
			c.JSON(http.StatusBadRequest, gin.H{"url": []string{"This field may not be null."}})
			return issueLinkInput{}, false
		}
		var value string
		if json.Unmarshal(raw, &value) != nil {
			c.JSON(http.StatusBadRequest, gin.H{"url": []string{"Not a valid string."}})
			return issueLinkInput{}, false
		}
		value = validate.PrefixScheme(value)
		if strings.TrimSpace(value) == "" {
			c.JSON(http.StatusBadRequest, gin.H{"url": []string{"This field may not be blank."}})
			return issueLinkInput{}, false
		}
		if !validate.URL(value) {
			// validate_url raises with a dict, which DRF nests under the field.
			c.JSON(http.StatusBadRequest, gin.H{"url": gin.H{"error": "Invalid URL format."}})
			return issueLinkInput{}, false
		}
		result.url = value
	} else if !partial {
		c.JSON(http.StatusBadRequest, gin.H{"url": []string{"This field is required."}})
		return issueLinkInput{}, false
	}
	if raw, exists := body["metadata"]; exists {
		result.hasMetadata = true
		if string(raw) == "null" {
			c.JSON(http.StatusBadRequest, gin.H{"metadata": []string{"This field may not be null."}})
			return issueLinkInput{}, false
		}
		if !json.Valid(raw) {
			c.JSON(http.StatusBadRequest, gin.H{"metadata": []string{"Value must be valid JSON."}})
			return issueLinkInput{}, false
		}
		result.metadata = auth.JSONValue(append([]byte(nil), raw...))
	}
	return result, true
}

// issueLinkJSON is IssueLinkSerializer: every model field plus created_by_detail.
func (handler *Handler) issueLinkJSON(ctx context.Context, link IssueLink) (gin.H, error) {
	data := gin.H{
		"id": link.ID, "created_at": link.CreatedAt, "updated_at": link.UpdatedAt,
		"created_by": link.CreatedByID, "updated_by": link.UpdatedByID,
		"deleted_at": link.DeletedAt, "project": link.ProjectID,
		"workspace": link.WorkspaceID, "issue": link.IssueID,
		"title": link.Title, "url": link.URL, "metadata": decodeJSON(link.Metadata),
		"created_by_detail": nil,
	}
	if link.CreatedByID != nil {
		var creator auth.User
		if err := handler.db.WithContext(ctx).Where("id = ?", *link.CreatedByID).Take(&creator).Error; err == nil {
			data["created_by_detail"] = liteUserJSON(creator, false)
		}
	}
	return data, nil
}

func decodeRawFields(body map[string]json.RawMessage) map[string]any {
	decoded := make(map[string]any, len(body))
	for key, value := range body {
		var target any
		if json.Unmarshal(value, &target) == nil {
			decoded[key] = target
		}
	}
	return decoded
}
