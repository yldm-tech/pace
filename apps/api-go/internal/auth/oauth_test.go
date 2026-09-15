package auth

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestGoogleOAuthContract(t *testing.T) {
	request, _ := http.NewRequest(http.MethodGet, "http://api.pace.test/auth/google/", nil)
	configuration := map[string]string{"GOOGLE_CLIENT_ID": "google-id", "GOOGLE_CLIENT_SECRET": "google-secret"}
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.String() {
		case "https://oauth2.googleapis.com/token":
			if request.Method != http.MethodPost {
				t.Fatalf("token method = %s", request.Method)
			}
			return jsonResponse(http.StatusOK, `{"access_token":"access","refresh_token":"refresh","expires_in":3600,"id_token":"id-token"}`), nil
		case "https://www.googleapis.com/oauth2/v2/userinfo":
			if request.Header.Get("Authorization") != "Bearer access" {
				t.Fatalf("authorization = %q", request.Header.Get("Authorization"))
			}
			return jsonResponse(http.StatusOK, `{"id":"google-user","email":"USER@pace.test","verified_email":true,"picture":"https://cdn.pace.test/avatar.png","given_name":"Pace","family_name":"User"}`), nil
		default:
			t.Fatalf("unexpected request %s", request.URL)
			return nil, nil
		}
	})}
	provider, authenticationError := newOAuthProvider("google", request, "state-value", "code-value", func(key, fallback string) string {
		if value, exists := configuration[key]; exists {
			return value
		}
		return fallback
	}, client)
	if authenticationError != nil {
		t.Fatal(authenticationError)
	}
	if !strings.Contains(provider.authURL, "state=state-value") || !strings.Contains(provider.authURL, "client_id=google-id") {
		t.Fatalf("auth URL = %q", provider.authURL)
	}
	identity, tokens, authenticationError := provider.Authenticate(request.Context())
	if authenticationError != nil {
		t.Fatal(authenticationError)
	}
	if identity.Email != "user@pace.test" || identity.ProviderID != "google-user" || identity.FirstName != "Pace" {
		t.Fatalf("identity = %#v", identity)
	}
	if tokens.AccessToken != "access" || tokens.RefreshToken == nil || *tokens.RefreshToken != "refresh" {
		t.Fatalf("tokens = %#v", tokens)
	}
}

func TestOAuthProvidersRequireVerifiedEmail(t *testing.T) {
	request, _ := http.NewRequest(http.MethodGet, "http://api.pace.test/auth/google/callback/", nil)
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if strings.Contains(request.URL.String(), "/token") {
			return jsonResponse(http.StatusOK, `{"access_token":"access"}`), nil
		}
		return jsonResponse(http.StatusOK, `{"id":"google-user","email":"user@pace.test","verified_email":false}`), nil
	})}
	provider, authenticationError := newOAuthProvider("google", request, "", "code", func(key, fallback string) string {
		if strings.HasSuffix(key, "CLIENT_ID") || strings.HasSuffix(key, "CLIENT_SECRET") {
			return "configured"
		}
		return fallback
	}, client)
	if authenticationError != nil {
		t.Fatal(authenticationError)
	}
	_, _, authenticationError = provider.Authenticate(request.Context())
	if authenticationError == nil || authenticationError.Code != ErrorOAuthProviderUnverifiedEmail {
		t.Fatalf("authentication error = %#v", authenticationError)
	}
}

func TestGitHubUsesPrimaryVerifiedEmail(t *testing.T) {
	request, _ := http.NewRequest(http.MethodGet, "http://api.pace.test/auth/github/callback/", nil)
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.String() {
		case "https://github.com/login/oauth/access_token":
			return jsonResponse(http.StatusOK, `{"access_token":"access"}`), nil
		case "https://api.github.com/user":
			return jsonResponse(http.StatusOK, `{"id":42,"login":"pace-user","name":"Pace User"}`), nil
		case "https://api.github.com/user/emails":
			return jsonResponse(http.StatusOK, `[{"email":"unverified@pace.test","primary":true,"verified":false},{"email":"verified@pace.test","primary":true,"verified":true}]`), nil
		default:
			t.Fatalf("unexpected request %s", request.URL)
			return nil, nil
		}
	})}
	provider, authenticationError := newOAuthProvider("github", request, "", "code", func(key, fallback string) string {
		if key == "GITHUB_CLIENT_ID" || key == "GITHUB_CLIENT_SECRET" {
			return "configured"
		}
		return fallback
	}, client)
	if authenticationError != nil {
		t.Fatal(authenticationError)
	}
	identity, _, authenticationError := provider.Authenticate(request.Context())
	if authenticationError != nil {
		t.Fatal(authenticationError)
	}
	if identity.Email != "verified@pace.test" || identity.ProviderID != "42" {
		t.Fatalf("identity = %#v", identity)
	}
}
