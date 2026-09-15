package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type OAuthIdentity struct {
	Provider   string
	ProviderID string
	Email      string
	Avatar     string
	FirstName  string
	LastName   string
}

type OAuthTokens struct {
	AccessToken           string
	RefreshToken          *string
	AccessTokenExpiredAt  *time.Time
	RefreshTokenExpiredAt *time.Time
	IDToken               string
}

type oauthProvider struct {
	name           string
	clientID       string
	clientSecret   string
	host           string
	organizationID string
	scope          string
	redirectURI    string
	authURL        string
	tokenURL       string
	userInfoURL    string
	code           string
	httpClient     *http.Client
}

type oauthConfiguration func(key, fallback string) string

func newOAuthProvider(name string, request *http.Request, state, code string, configuration oauthConfiguration, client *http.Client) (*oauthProvider, *Error) {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	scheme := "http"
	if request.TLS != nil || strings.EqualFold(request.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	provider := &oauthProvider{name: name, code: code, httpClient: client}
	provider.redirectURI = fmt.Sprintf("%s://%s/auth/%s/callback/", scheme, request.Host, name)
	switch name {
	case "google":
		provider.clientID = configuration("GOOGLE_CLIENT_ID", "")
		provider.clientSecret = configuration("GOOGLE_CLIENT_SECRET", "")
		if provider.clientID == "" || provider.clientSecret == "" {
			return nil, authError(ErrorGoogleNotConfigured, "GOOGLE_NOT_CONFIGURED", nil)
		}
		provider.scope = "https://www.googleapis.com/auth/userinfo.email https://www.googleapis.com/auth/userinfo.profile"
		provider.tokenURL = "https://oauth2.googleapis.com/token"
		provider.userInfoURL = "https://www.googleapis.com/oauth2/v2/userinfo"
		provider.authURL = oauthAuthorizationURL("https://accounts.google.com/o/oauth2/v2/auth", [][2]string{
			{"client_id", provider.clientID}, {"scope", provider.scope}, {"redirect_uri", provider.redirectURI},
			{"response_type", "code"}, {"access_type", "offline"}, {"prompt", "consent"}, {"state", state},
		})
	case "github":
		provider.clientID = configuration("GITHUB_CLIENT_ID", "")
		provider.clientSecret = configuration("GITHUB_CLIENT_SECRET", "")
		provider.organizationID = configuration("GITHUB_ORGANIZATION_ID", "")
		if provider.clientID == "" || provider.clientSecret == "" {
			return nil, authError(ErrorGitHubNotConfigured, "GITHUB_NOT_CONFIGURED", nil)
		}
		provider.scope = "read:user user:email"
		if provider.organizationID != "" {
			provider.scope += " read:org"
		}
		provider.tokenURL = "https://github.com/login/oauth/access_token"
		provider.userInfoURL = "https://api.github.com/user"
		provider.authURL = oauthAuthorizationURL("https://github.com/login/oauth/authorize", [][2]string{
			{"client_id", provider.clientID}, {"redirect_uri", provider.redirectURI}, {"scope", provider.scope}, {"state", state},
		})
	case "gitlab":
		provider.clientID = configuration("GITLAB_CLIENT_ID", "")
		provider.clientSecret = configuration("GITLAB_CLIENT_SECRET", "")
		provider.host = strings.TrimRight(configuration("GITLAB_HOST", "https://gitlab.com"), "/")
		if provider.clientID == "" || provider.clientSecret == "" || !validOAuthHost(provider.host) {
			return nil, authError(ErrorGitLabNotConfigured, "GITLAB_NOT_CONFIGURED", nil)
		}
		provider.scope = "read_user"
		provider.tokenURL = provider.host + "/oauth/token"
		provider.userInfoURL = provider.host + "/api/v4/user"
		provider.authURL = oauthAuthorizationURL(provider.host+"/oauth/authorize", [][2]string{
			{"client_id", provider.clientID}, {"redirect_uri", provider.redirectURI}, {"response_type", "code"},
			{"scope", provider.scope}, {"state", state},
		})
	case "gitea":
		provider.clientID = configuration("GITEA_CLIENT_ID", "")
		provider.clientSecret = configuration("GITEA_CLIENT_SECRET", "")
		provider.host = strings.TrimRight(configuration("GITEA_HOST", ""), "/")
		if provider.clientID == "" || provider.clientSecret == "" || !validOAuthHost(provider.host) {
			return nil, authError(ErrorGiteaNotConfigured, "GITEA_NOT_CONFIGURED", nil)
		}
		provider.scope = "openid email profile read:user"
		provider.tokenURL = provider.host + "/login/oauth/access_token"
		provider.userInfoURL = provider.host + "/api/v1/user"
		provider.authURL = oauthAuthorizationURL(provider.host+"/login/oauth/authorize", [][2]string{
			{"client_id", provider.clientID}, {"scope", provider.scope}, {"redirect_uri", provider.redirectURI},
			{"response_type", "code"}, {"state", state},
		})
	default:
		return nil, authError(ErrorOAuthNotConfigured, "OAUTH_NOT_CONFIGURED", nil)
	}
	return provider, nil
}

func (provider *oauthProvider) Authenticate(ctx context.Context) (OAuthIdentity, OAuthTokens, *Error) {
	tokenResponse, authenticationError := provider.exchangeCode(ctx)
	if authenticationError != nil {
		return OAuthIdentity{}, OAuthTokens{}, authenticationError
	}
	tokens := provider.tokens(tokenResponse)
	identity, authenticationError := provider.identity(ctx, tokens.AccessToken)
	return identity, tokens, authenticationError
}

func (provider *oauthProvider) exchangeCode(ctx context.Context) (map[string]any, *Error) {
	form := url.Values{"client_id": {provider.clientID}, "client_secret": {provider.clientSecret}, "code": {provider.code}, "redirect_uri": {provider.redirectURI}}
	if provider.name == "google" || provider.name == "gitlab" || provider.name == "gitea" {
		form.Set("grant_type", "authorization_code")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, provider.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, provider.providerError()
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if provider.name != "google" {
		request.Header.Set("Accept", "application/json")
	}
	response, err := provider.httpClient.Do(request)
	if err != nil || response.StatusCode < 200 || response.StatusCode >= 300 {
		if response != nil {
			response.Body.Close()
		}
		return nil, provider.providerError()
	}
	defer response.Body.Close()
	var payload map[string]any
	decoder := json.NewDecoder(io.LimitReader(response.Body, 1024*1024))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return nil, provider.providerError()
	}
	return payload, nil
}

func (provider *oauthProvider) tokens(payload map[string]any) OAuthTokens {
	accessToken := stringValue(payload["access_token"])
	refreshToken := optionalString(payload["refresh_token"])
	idToken := stringValue(payload["id_token"])
	var accessExpiry, refreshExpiry *time.Time
	expiresIn := int64Value(payload["expires_in"])
	if expiresIn > 0 {
		var expiry time.Time
		switch provider.name {
		case "gitlab":
			expiry = time.Unix(int64Value(payload["created_at"])+expiresIn, 0).UTC()
		case "gitea":
			expiry = time.Now().UTC().Add(time.Duration(expiresIn) * time.Second)
		default:
			// Match the existing provider adapters, which interpret expires_in
			// as an epoch value for Google and GitHub.
			expiry = time.Unix(expiresIn, 0).UTC()
		}
		accessExpiry = &expiry
	}
	if value := int64Value(payload["refresh_token_expired_at"]); value > 0 {
		expiry := time.Unix(value, 0).UTC()
		refreshExpiry = &expiry
	}
	return OAuthTokens{
		AccessToken: accessToken, RefreshToken: refreshToken, AccessTokenExpiredAt: accessExpiry,
		RefreshTokenExpiredAt: refreshExpiry, IDToken: idToken,
	}
}

func (provider *oauthProvider) identity(ctx context.Context, accessToken string) (OAuthIdentity, *Error) {
	user, ok := provider.getJSON(ctx, provider.userInfoURL, accessToken, nil)
	if !ok {
		return OAuthIdentity{}, provider.providerError()
	}
	identity := OAuthIdentity{
		Provider: provider.name, ProviderID: stringValue(user["id"]), Avatar: stringValue(user["avatar_url"]),
		FirstName: stringValue(user["name"]), LastName: stringValue(user["family_name"]),
	}
	switch provider.name {
	case "google":
		if verified, _ := user["verified_email"].(bool); !verified {
			return OAuthIdentity{}, authError(ErrorOAuthProviderUnverifiedEmail, "OAUTH_PROVIDER_UNVERIFIED_EMAIL", nil)
		}
		identity.Email = stringValue(user["email"])
		identity.Avatar = stringValue(user["picture"])
		identity.FirstName = stringValue(user["given_name"])
	case "github":
		if provider.organizationID != "" {
			membershipURL := "https://api.github.com/orgs/" + url.PathEscape(provider.organizationID) + "/memberships/" + url.PathEscape(stringValue(user["login"]))
			if _, ok := provider.getJSON(ctx, membershipURL, accessToken, nil); !ok {
				return OAuthIdentity{}, authError(ErrorGitHubUserNotInOrg, "GITHUB_USER_NOT_IN_ORG", nil)
			}
		}
		emails, ok := provider.getJSONArray(ctx, "https://api.github.com/user/emails", accessToken)
		if !ok {
			return OAuthIdentity{}, provider.providerError()
		}
		for _, email := range emails {
			primary, _ := email["primary"].(bool)
			verified, _ := email["verified"].(bool)
			if primary && verified {
				identity.Email = stringValue(email["email"])
				break
			}
		}
		if identity.Email == "" {
			return OAuthIdentity{}, authError(ErrorOAuthProviderUnverifiedEmail, "OAUTH_PROVIDER_UNVERIFIED_EMAIL", nil)
		}
	case "gitlab":
		if user["confirmed_at"] == nil || stringValue(user["confirmed_at"]) == "" {
			return OAuthIdentity{}, authError(ErrorOAuthProviderUnverifiedEmail, "OAUTH_PROVIDER_UNVERIFIED_EMAIL", nil)
		}
		identity.Email = stringValue(user["email"])
	case "gitea":
		emails, ok := provider.getJSONArray(ctx, provider.userInfoURL+"/emails", accessToken)
		if !ok {
			return OAuthIdentity{}, provider.providerError()
		}
		for _, email := range emails {
			primary, _ := email["primary"].(bool)
			verified, _ := email["verified"].(bool)
			if primary && verified {
				identity.Email = stringValue(email["email"])
				break
			}
		}
		if identity.Email == "" {
			for _, email := range emails {
				if verified, _ := email["verified"].(bool); verified {
					identity.Email = stringValue(email["email"])
					break
				}
			}
		}
		if identity.Email == "" {
			return OAuthIdentity{}, authError(ErrorOAuthProviderUnverifiedEmail, "OAUTH_PROVIDER_UNVERIFIED_EMAIL", nil)
		}
		identity.FirstName = stringValue(user["full_name"])
		if identity.FirstName == "" {
			identity.FirstName = stringValue(user["login"])
		}
		identity.LastName = ""
	}
	identity.Email = strings.ToLower(strings.TrimSpace(identity.Email))
	if identity.Email == "" || identity.ProviderID == "" {
		return OAuthIdentity{}, provider.providerError()
	}
	return identity, nil
}

func (provider *oauthProvider) getJSON(ctx context.Context, endpoint, accessToken string, headers map[string]string) (map[string]any, bool) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, false
	}
	request.Header.Set("Authorization", "Bearer "+accessToken)
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := provider.httpClient.Do(request)
	if err != nil || response.StatusCode < 200 || response.StatusCode >= 300 {
		if response != nil {
			response.Body.Close()
		}
		return nil, false
	}
	defer response.Body.Close()
	var payload map[string]any
	decoder := json.NewDecoder(io.LimitReader(response.Body, 1024*1024))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return nil, false
	}
	return payload, true
}

func (provider *oauthProvider) getJSONArray(ctx context.Context, endpoint, accessToken string) ([]map[string]any, bool) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, false
	}
	request.Header.Set("Authorization", "Bearer "+accessToken)
	request.Header.Set("Accept", "application/json")
	response, err := provider.httpClient.Do(request)
	if err != nil || response.StatusCode < 200 || response.StatusCode >= 300 {
		if response != nil {
			response.Body.Close()
		}
		return nil, false
	}
	defer response.Body.Close()
	var payload []map[string]any
	decoder := json.NewDecoder(io.LimitReader(response.Body, 1024*1024))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return nil, false
	}
	return payload, true
}

func (provider *oauthProvider) providerError() *Error {
	switch provider.name {
	case "google":
		return authError(ErrorGoogleOAuthProvider, "GOOGLE_OAUTH_PROVIDER_ERROR", nil)
	case "github":
		return authError(ErrorGitHubOAuthProvider, "GITHUB_OAUTH_PROVIDER_ERROR", nil)
	case "gitlab":
		return authError(ErrorGitLabOAuthProvider, "GITLAB_OAUTH_PROVIDER_ERROR", nil)
	case "gitea":
		return authError(ErrorGiteaOAuthProvider, "GITEA_OAUTH_PROVIDER_ERROR", nil)
	default:
		return authError(ErrorOAuthNotConfigured, "OAUTH_NOT_CONFIGURED", nil)
	}
}

func oauthAuthorizationURL(base string, parameters [][2]string) string {
	parts := make([]string, 0, len(parameters))
	for _, parameter := range parameters {
		parts = append(parts, url.QueryEscape(parameter[0])+"="+url.QueryEscape(parameter[1]))
	}
	return base + "?" + strings.Join(parts, "&")
}

func validOAuthHost(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	return err == nil && (parsed.Scheme == "https" || parsed.Scheme == "http") && parsed.Host != ""
}

func stringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case json.Number:
		return typed.String()
	case float64:
		return strconv.FormatInt(int64(typed), 10)
	case nil:
		return ""
	default:
		return fmt.Sprint(typed)
	}
}

func optionalString(value any) *string {
	result := stringValue(value)
	if result == "" {
		return nil
	}
	return &result
}

func int64Value(value any) int64 {
	switch typed := value.(type) {
	case float64:
		return int64(typed)
	case json.Number:
		result, _ := typed.Int64()
		return result
	case string:
		result, _ := strconv.ParseInt(typed, 10, 64)
		return result
	default:
		return 0
	}
}
