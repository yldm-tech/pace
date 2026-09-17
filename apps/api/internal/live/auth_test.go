package live

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestBuildContextTakesTheCookieFromTheToken(t *testing.T) {
	parameters := url.Values{
		"documentType":  {"project_page"},
		"workspaceSlug": {"a-workspace"},
		"projectId":     {"a-project"},
	}
	connection, err := buildContext(`{"id":"a-user","cookie":"session=abc"}`, http.Header{}, parameters)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if connection.UserID != "a-user" || connection.Cookie != "session=abc" {
		t.Errorf("connection = %+v", connection)
	}
	if connection.DocumentType != ProjectPage || connection.WorkspaceSlug != "a-workspace" || connection.ProjectID != "a-project" {
		t.Errorf("connection = %+v", connection)
	}
}

// TestBuildContextFallsBackToTheRequestCookie covers the case the token exists for: a browser opening a socket to another origin does not always attach its cookies, so the token carries one. The fallback is the other way round — a token with no cookie in it uses the request's.
func TestBuildContextFallsBackToTheRequestCookie(t *testing.T) {
	headers := http.Header{}
	headers.Set("Cookie", "session=from-the-header")
	connection, err := buildContext(`{"id":"a-user"}`, headers, url.Values{})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if connection.Cookie != "session=from-the-header" {
		t.Errorf("cookie = %q", connection.Cookie)
	}
}

// TestBuildContextSurvivesAnUnparseableToken pins that a token which is not JSON is not fatal on its own, which is what the service it replaces does — it logs and carries on to the header fallback.
func TestBuildContextSurvivesAnUnparseableToken(t *testing.T) {
	headers := http.Header{}
	headers.Set("Cookie", "session=abc")
	if _, err := buildContext("not json at all", headers, url.Values{}); !errors.Is(err, ErrMissingCredentials) {
		t.Fatalf("err = %v, want missing credentials, because the user id only ever comes from the token", err)
	}
}

func TestBuildContextRefusesMissingCredentials(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		token   string
		headers http.Header
	}{
		{name: "nothing at all", token: "", headers: http.Header{}},
		{name: "a user and no cookie", token: `{"id":"a-user"}`, headers: http.Header{}},
		{name: "a cookie and no user", token: `{"cookie":"session=abc"}`, headers: http.Header{}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := buildContext(testCase.token, testCase.headers, url.Values{}); !errors.Is(err, ErrMissingCredentials) {
				t.Errorf("err = %v, want missing credentials", err)
			}
		})
	}
}

func TestAuthenticateComparesTheUser(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/users/me/" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("Cookie"); got != "session=abc" {
			t.Errorf("cookie = %q, want the connection's own", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"a-user","display_name":"Someone"}`))
	}))
	defer api.Close()

	server := NewServer(Config{APIBaseURL: api.URL, BasePath: "/live"}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	user, err := server.authenticate(context.Background(), ConnectionContext{UserID: "a-user", Cookie: "session=abc"})
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if user.DisplayName != "Someone" {
		t.Errorf("user = %+v", user)
	}

	if _, err := server.authenticate(context.Background(), ConnectionContext{UserID: "somebody-else", Cookie: "session=abc"}); !errors.Is(err, ErrUserMismatch) {
		t.Errorf("err = %v, want a mismatch", err)
	}
}

func TestAuthenticateRefusesWhenTheAPIDoes(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"not logged in"}`))
	}))
	defer api.Close()

	server := NewServer(Config{APIBaseURL: api.URL, BasePath: "/live"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	_, err := server.authenticate(context.Background(), ConnectionContext{UserID: "a-user", Cookie: "session=stale"})
	if err == nil {
		t.Fatal("no error, want one")
	}
	var apiError *APIError
	if !errors.As(err, &apiError) || apiError.StatusCode != http.StatusUnauthorized {
		t.Fatalf("err = %v, want a 401 carried through", err)
	}
	if apiError.Message != "not logged in" {
		t.Errorf("message = %q, want the one the API gave", apiError.Message)
	}
}
