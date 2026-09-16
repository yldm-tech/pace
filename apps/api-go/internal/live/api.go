package live

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// apiTimeout is how long a call to the API is given before it is abandoned, matching the service this replaces.
const apiTimeout = 20 * time.Second

// APIClient talks to the API on a user's behalf, forwarding their session cookie. Every call the live service makes is made as the user who is editing, not as the service, so the API's own permission checks are what decide whether a page can be read or written.
type APIClient struct {
	baseURL string
	http    *http.Client
}

// NewAPIClient builds a client against the configured API. The redirect policy is the interesting part: it refuses to follow one, so a route that answers with a redirect hands the location back rather than being followed into object storage with the session cookie attached.
func NewAPIClient(baseURL string) *APIClient {
	return &APIClient{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		http: &http.Client{
			Timeout: apiTimeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// APIError is a failed call, carrying the status so a caller can tell a page that is too large from a page that is gone.
type APIError struct {
	Method     string
	Path       string
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	if e.StatusCode == 0 {
		return fmt.Sprintf("%s %s: %s", e.Method, e.Path, e.Message)
	}
	return fmt.Sprintf("%s %s: %d: %s", e.Method, e.Path, e.StatusCode, e.Message)
}

// request performs one call and hands back the body. A status outside 2xx is an error carrying that status, except for a redirect, which the caller may want to read the location off.
func (c *APIClient) request(ctx context.Context, method, path, cookie string, body any, query map[string]string) (*http.Response, []byte, error) {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, nil, &APIError{Method: method, Path: path, Message: err.Error()}
		}
		reader = bytes.NewReader(encoded)
	}

	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return nil, nil, &APIError{Method: method, Path: path, Message: err.Error()}
	}
	if cookie != "" {
		request.Header.Set("Cookie", cookie)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if len(query) > 0 {
		values := request.URL.Query()
		for name, value := range query {
			values.Set(name, value)
		}
		request.URL.RawQuery = values.Encode()
	}

	response, err := c.http.Do(request)
	if err != nil {
		return nil, nil, &APIError{Method: method, Path: path, Message: err.Error()}
	}
	defer response.Body.Close()

	payload, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, nil, &APIError{Method: method, Path: path, StatusCode: response.StatusCode, Message: err.Error()}
	}
	if response.StatusCode >= 400 {
		return response, payload, &APIError{Method: method, Path: path, StatusCode: response.StatusCode, Message: apiMessage(payload, response.Status)}
	}
	return response, payload, nil
}

// apiMessage pulls the message out of an error body when there is one, the way the service it replaces does, and falls back to the status line.
func apiMessage(payload []byte, status string) string {
	var body struct {
		Message string `json:"message"`
		Detail  string `json:"detail"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(payload, &body); err == nil {
		for _, candidate := range []string{body.Message, body.Detail, body.Error} {
			if candidate != "" {
				return candidate
			}
		}
	}
	return status
}
