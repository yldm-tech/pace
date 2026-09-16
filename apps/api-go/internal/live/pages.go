package live

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// DocumentType names the kind of page a connection is editing. There is one, and anything else is refused, because the base path a page is read and written through is built from it.
type DocumentType string

// ProjectPage is the only document type the service serves.
const ProjectPage DocumentType = "project_page"

// PageService reads and writes one project's pages on one user's behalf. Its base path carries the workspace and the project, so a connection that named neither cannot be served.
type PageService struct {
	client   *APIClient
	basePath string
	cookie   string
}

// NewPageService builds the service for a connection's context. It refuses a context that is missing any of the three things the path and the credentials are built from, which is the same refusal the service it replaces makes and for the same reason: without them there is nothing to read.
func NewPageService(client *APIClient, context ConnectionContext) (*PageService, error) {
	if context.DocumentType != ProjectPage {
		return nil, fmt.Errorf("invalid document type %s provided", context.DocumentType)
	}
	if context.WorkspaceSlug == "" || context.ProjectID == "" {
		return nil, errors.New("missing required fields")
	}
	if context.Cookie == "" {
		return nil, errors.New("cookie is required")
	}
	return &PageService{
		client:   client,
		basePath: fmt.Sprintf("/api/workspaces/%s/projects/%s", context.WorkspaceSlug, context.ProjectID),
		cookie:   context.Cookie,
	}, nil
}

// Page is the part of a page's row this service reads: enough to build a document out of when there is no binary yet.
type Page struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	DescriptionHTML string `json:"description_html"`
}

// DocumentPayload is what a page's three description columns are written as.
type DocumentPayload struct {
	DescriptionBinary string          `json:"description_binary"`
	DescriptionHTML   string          `json:"description_html"`
	DescriptionJSON   json.RawMessage `json:"description_json"`
}

// FetchDetails reads a page's row.
func (s *PageService) FetchDetails(ctx context.Context, pageID string) (Page, error) {
	_, payload, err := s.client.request(ctx, http.MethodGet, fmt.Sprintf("%s/pages/%s/", s.basePath, pageID), s.cookie, nil, nil)
	if err != nil {
		return Page{}, err
	}
	var page Page
	if err := json.Unmarshal(payload, &page); err != nil {
		return Page{}, fmt.Errorf("decode page %s: %w", pageID, err)
	}
	return page, nil
}

// FetchDescriptionBinary reads a page's Yjs update. An empty body is not an error: it is a page nobody has opened in the collaborative editor yet.
func (s *PageService) FetchDescriptionBinary(ctx context.Context, pageID string) ([]byte, error) {
	_, payload, err := s.client.request(ctx, http.MethodGet, fmt.Sprintf("%s/pages/%s/description/", s.basePath, pageID), s.cookie, nil, nil)
	if err != nil {
		return nil, err
	}
	return payload, nil
}

// UpdateDescriptionBinary writes all three description columns back.
func (s *PageService) UpdateDescriptionBinary(ctx context.Context, pageID string, payload DocumentPayload) error {
	_, _, err := s.client.request(ctx, http.MethodPatch, fmt.Sprintf("%s/pages/%s/description/", s.basePath, pageID), s.cookie, payload, nil)
	return err
}

// UpdatePageProperties patches a page's row, which is how a title typed into the document reaches the page list.
func (s *PageService) UpdatePageProperties(ctx context.Context, pageID string, data map[string]any) error {
	_, _, err := s.client.request(ctx, http.MethodPatch, fmt.Sprintf("%s/pages/%s/", s.basePath, pageID), s.cookie, data, nil)
	return err
}

// UserMention is one person a page may mention.
type UserMention struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	AvatarURL   string `json:"avatar_url,omitempty"`
}

// FetchUserMentions lists the people a page mentions.
func (s *PageService) FetchUserMentions(ctx context.Context, pageID string) ([]UserMention, error) {
	_, payload, err := s.client.request(ctx, http.MethodGet, fmt.Sprintf("%s/pages/%s/mentions/", s.basePath, pageID), s.cookie, nil, map[string]string{"mention_type": "user_mention"})
	if err != nil {
		return nil, err
	}
	var mentions []UserMention
	if err := json.Unmarshal(payload, &mentions); err != nil {
		return nil, fmt.Errorf("decode mentions for %s: %w", pageID, err)
	}
	return mentions, nil
}

// User is the part of the current user's row authentication compares against.
type User struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
}

// CurrentUser reads whoever the cookie belongs to.
func (c *APIClient) CurrentUser(ctx context.Context, cookie string) (User, error) {
	_, payload, err := c.request(ctx, http.MethodGet, "/api/users/me/", cookie, nil, nil)
	if err != nil {
		return User{}, err
	}
	var user User
	if err := json.Unmarshal(payload, &user); err != nil {
		return User{}, fmt.Errorf("decode current user: %w", err)
	}
	return user, nil
}
