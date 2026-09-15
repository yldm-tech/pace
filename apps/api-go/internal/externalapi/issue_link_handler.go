package externalapi

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"github.com/yldm-tech/pace/apps/api-go/internal/drf"
	"gorm.io/gorm"
)

func (handler *Handler) registerIssueLinkRoutes(router gin.IRouter) {
	base := "/api/v1/workspaces/:slug/projects/:project/"
	// Every work item route is mounted under both names, the current one and the one it had before an issue was called a work item.
	for _, name := range []string{"issues", "work-items"} {
		links := base + name + "/:issue/links/"
		router.GET(links, handler.authenticated(handler.issueLinkList))
		router.POST(links, handler.authenticated(handler.issueLinkCreate))
		router.GET(links+":link/", handler.authenticated(handler.issueLinkRetrieve))
		router.PATCH(links+":link/", handler.authenticated(handler.issueLinkUpdate))
		router.DELETE(links+":link/", handler.authenticated(handler.issueLinkDestroy))
	}
}

// IssueLink is the db.IssueLink table as the external API sees it.
type IssueLink struct {
	ID          string     `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt   time.Time  `gorm:"column:created_at"`
	UpdatedAt   time.Time  `gorm:"column:updated_at"`
	CreatedByID *string    `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID *string    `gorm:"column:updated_by_id;type:uuid"`
	DeletedAt   *time.Time `gorm:"column:deleted_at"`
	ProjectID   string     `gorm:"column:project_id;type:uuid"`
	WorkspaceID string     `gorm:"column:workspace_id;type:uuid"`
	IssueID     string     `gorm:"column:issue_id;type:uuid"`
	Title       *string    `gorm:"column:title"`
	URL         string     `gorm:"column:url"`
	Metadata    []byte     `gorm:"column:metadata;type:jsonb"`
	SortOrder   float64    `gorm:"column:sort_order"`
}

func (IssueLink) TableName() string { return "issue_links" }

func (handler *Handler) issueLinkList(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireProjectMember(c, user, http.MethodGet) {
		return
	}
	var links []IssueLink
	if err := handler.issueLinkScope(c, user).Order("l.created_at DESC").Scan(&links).Error; err != nil {
		handler.serverError(c, err)
		return
	}
	fields := requestedFields(c)
	results := make([]gin.H, 0, len(links))
	for _, link := range links {
		results = append(results, narrow(issueLinkJSON(link), fields))
	}
	handler.respondPaged(c, results)
}

func (handler *Handler) issueLinkRetrieve(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireProjectMember(c, user, http.MethodGet) {
		return
	}
	link, found, err := handler.issueLinkByID(c, user)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !found {
		notFound(c)
		return
	}
	drf.Respond(c, http.StatusOK, narrow(issueLinkJSON(link), requestedFields(c)))
}

// issueLinkCreate attaches an external link to a work item.
//
// The duplicate check is scoped to the **issue** rather than to the project, so the same url may hang off two different work items. And the creator can be **named in the body** — the row is written first and then its author is rewritten from `created_by`, which is how an integration attributes a link to the person it acted for rather than to the key.
func (handler *Handler) issueLinkCreate(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireProjectMember(c, user, http.MethodPost) {
		return
	}
	projectID, issueID := c.Param("project"), c.Param("issue")
	var payload map[string]any
	if err := c.ShouldBindJSON(&payload); err != nil {
		invalidPayload(c)
		return
	}
	target, _ := payload["url"].(string)
	if message := validateLinkURL(target); message != "" {
		c.JSON(http.StatusBadRequest, gin.H{"url": []string{message}})
		return
	}
	duplicate, err := handler.linkURLExists(c, issueID, target, "")
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if duplicate {
		c.JSON(http.StatusBadRequest, gin.H{"error": "URL already exists for this Issue"})
		return
	}

	var workspaceIDs []string
	err = handler.db.WithContext(c.Request.Context()).Table("projects").
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
	// The author is the caller unless the body names somebody else, and nothing checks that the somebody else is real.
	author := user.ID
	if value, ok := payload["created_by"].(string); ok && value != "" {
		author = value
	}
	link := IssueLink{
		ID: identifier, CreatedAt: now, UpdatedAt: now, CreatedByID: &author, UpdatedByID: &user.ID,
		ProjectID: projectID, WorkspaceID: workspaceIDs[0], IssueID: issueID,
		URL: target, Metadata: []byte("{}"), SortOrder: 65535,
	}
	if value, ok := payload["title"].(string); ok {
		link.Title = &value
	}
	if err := handler.db.WithContext(c.Request.Context()).Create(&link).Error; err != nil {
		handler.serverError(c, err)
		return
	}
	// The crawler is told to fetch the page's title, and the activity is attributed to the named author rather than to the caller.
	if handler.tasks != nil {
		if err := handler.tasks.PublishCrawlLinkTitle(c.Request.Context(), link.ID, link.URL); err != nil {
			handler.serverError(c, err)
			return
		}
	}
	drf.Respond(c, http.StatusCreated, issueLinkJSON(link))
}

// issueLinkUpdate edits a link, and asks the crawler for a new title only when the url actually changed.
func (handler *Handler) issueLinkUpdate(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireProjectMember(c, user, http.MethodPatch) {
		return
	}
	var payload map[string]any
	if err := c.ShouldBindJSON(&payload); err != nil {
		invalidPayload(c)
		return
	}
	link, found, err := handler.issueLinkByID(c, user)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !found {
		notFound(c)
		return
	}
	previous := link.URL
	// The update goes through the full serializer rather than the create one, so it carries **no** duplicate check and no scheme check: any string at all may be written over a url.
	if value, ok := payload["url"].(string); ok {
		link.URL = value
	}
	if value, present := payload["title"]; present {
		if text, ok := value.(string); ok {
			link.Title = &text
		} else {
			link.Title = nil
		}
	}
	if value, present := payload["metadata"]; present {
		if encoded, err := json.Marshal(value); err == nil {
			link.Metadata = encoded
		}
	}
	now := handler.clock().UTC()
	link.UpdatedAt, link.UpdatedByID = now, &user.ID
	err = handler.db.WithContext(c.Request.Context()).Model(&IssueLink{}).Where("id = ?", link.ID).
		Updates(map[string]any{
			"url": link.URL, "title": link.Title, "metadata": link.Metadata,
			"updated_at": link.UpdatedAt, "updated_by_id": link.UpdatedByID,
		}).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if handler.tasks != nil && link.URL != "" && link.URL != previous {
		if err := handler.tasks.PublishCrawlLinkTitle(c.Request.Context(), link.ID, link.URL); err != nil {
			handler.serverError(c, err)
			return
		}
	}
	drf.Respond(c, http.StatusOK, issueLinkJSON(link))
}

func (handler *Handler) issueLinkDestroy(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireProjectMember(c, user, http.MethodDelete) {
		return
	}
	link, found, err := handler.issueLinkByID(c, user)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !found {
		notFound(c)
		return
	}
	err = handler.db.WithContext(c.Request.Context()).Model(&IssueLink{}).
		Where("id = ?", link.ID).Update("deleted_at", handler.clock().UTC()).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// validateLinkURL is the create serializer's two checks, in order: the shape first, then the scheme.
func validateLinkURL(target string) string {
	if strings.TrimSpace(target) == "" {
		return "This field is required."
	}
	parsed, err := url.Parse(target)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "Invalid URL format."
	}
	// Django's URLValidator accepts ftp and others, and the scheme check that follows is what narrows it to the two.
	if !strings.HasPrefix(target, "http://") && !strings.HasPrefix(target, "https://") {
		return "Invalid URL scheme."
	}
	return ""
}

// linkURLExists reports whether the work item already carries this url, optionally ignoring one link.
func (handler *Handler) linkURLExists(c *gin.Context, issueID, target, exclude string) (bool, error) {
	query := handler.db.WithContext(c.Request.Context()).Table("issue_links").
		Where("issue_id = ? AND url = ? AND deleted_at IS NULL", issueID, target)
	if exclude != "" {
		query = query.Where("id <> ?", exclude)
	}
	var count int64
	err := query.Count(&count).Error
	return count > 0, err
}

// issueLinkScope is the queryset every link route reads through.
func (handler *Handler) issueLinkScope(c *gin.Context, user *auth.User) *gorm.DB {
	return handler.db.WithContext(c.Request.Context()).Table("issue_links l").Select("l.*").
		Joins("JOIN workspaces w ON w.id = l.workspace_id").
		Joins("JOIN projects p ON p.id = l.project_id AND p.archived_at IS NULL").
		Joins("JOIN project_members pm ON pm.project_id = l.project_id AND pm.member_id = ? AND pm.is_active = TRUE", user.ID).
		Where("w.slug = ? AND l.project_id = ? AND l.issue_id = ? AND l.deleted_at IS NULL",
			c.Param("slug"), c.Param("project"), c.Param("issue"))
}

func (handler *Handler) issueLinkByID(c *gin.Context, user *auth.User) (IssueLink, bool, error) {
	var links []IssueLink
	err := handler.issueLinkScope(c, user).Where("l.id = ?", c.Param("link")).Limit(1).Scan(&links).Error
	if err != nil || len(links) == 0 {
		return IssueLink{}, false, err
	}
	return links[0], true, nil
}

// issueLinkJSON is the external API's IssueLinkSerializer, which asks for every field.
func issueLinkJSON(link IssueLink) gin.H {
	return gin.H{
		"id": link.ID, "created_at": link.CreatedAt, "updated_at": link.UpdatedAt,
		"created_by": link.CreatedByID, "updated_by": link.UpdatedByID, "deleted_at": link.DeletedAt,
		"title": link.Title, "url": link.URL, "metadata": decodeJSON(link.Metadata),
		"sort_order": link.SortOrder,
		"project":    link.ProjectID, "workspace": link.WorkspaceID, "issue": link.IssueID,
	}
}
