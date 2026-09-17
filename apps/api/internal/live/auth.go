package live

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
)

// ConnectionContext is everything a connection tells the server about itself: who is editing, which page, and the credentials to read and write it with. It is assembled once, when the connection authenticates, and every read and write the document does afterwards uses it.
type ConnectionContext struct {
	UserID        string
	Cookie        string
	DocumentType  DocumentType
	WorkspaceSlug string
	ProjectID     string
}

// tokenDetails is the JSON the client sends as its authentication token. It carries the session cookie as well as the user id, because a browser opening a websocket to another origin does not always attach its cookies.
type tokenDetails struct {
	ID     string `json:"id"`
	Cookie string `json:"cookie"`
}

// ErrMissingCredentials is the refusal for a connection that named neither a user nor a cookie.
var ErrMissingCredentials = errors.New("credentials not provided")

// ErrUserMismatch is the refusal for a token whose user is not the user the cookie belongs to.
var ErrUserMismatch = errors.New("authentication unsuccessful: user id mismatch")

// buildContext assembles a connection's context from its token and the parameters in its url.
//
// The cookie is taken from the token when it carries one and from the request headers otherwise, so a token that is not valid JSON is not fatal on its own — only a connection that ends up with neither a cookie nor a user id is refused.
func buildContext(token string, headers http.Header, parameters url.Values) (ConnectionContext, error) {
	var details tokenDetails
	// A token that will not parse leaves both fields empty and the header fallback below is what saves the connection.
	_ = json.Unmarshal([]byte(token), &details)

	cookie := details.Cookie
	if cookie == "" {
		cookie = headers.Get("Cookie")
	}
	if cookie == "" || details.ID == "" {
		return ConnectionContext{}, ErrMissingCredentials
	}

	return ConnectionContext{
		UserID:        details.ID,
		Cookie:        cookie,
		DocumentType:  DocumentType(parameters.Get("documentType")),
		WorkspaceSlug: parameters.Get("workspaceSlug"),
		ProjectID:     parameters.Get("projectId"),
	}, nil
}

// authenticate checks that the cookie really belongs to the user the token names. It is the whole of the service's own authorisation: everything past this point is done with that cookie, so the API decides what the user may read and write.
func (s *Server) authenticate(ctx context.Context, connection ConnectionContext) (User, error) {
	user, err := s.api.CurrentUser(ctx, connection.Cookie)
	if err != nil {
		return User{}, err
	}
	if user.ID != connection.UserID {
		return User{}, ErrUserMismatch
	}
	return user, nil
}
