package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

type fakeRepository struct {
	configured      bool
	users           map[string]*User
	configs         map[string]string
	loginCount      int
	updatedPassword string
}

func (repository *fakeRepository) InstanceConfigured(context.Context) (bool, error) {
	return repository.configured, nil
}
func (repository *fakeRepository) ConfigurationValue(_ context.Context, key, fallback string) (string, error) {
	if value, exists := repository.configs[key]; exists {
		return value, nil
	}
	return fallback, nil
}
func (repository *fakeRepository) SignupAllowed(context.Context, string) (bool, error) {
	return false, nil
}
func (repository *fakeRepository) FindUserByEmail(_ context.Context, email string) (*User, error) {
	user, exists := repository.users[strings.ToLower(email)]
	if !exists {
		return nil, ErrNotFound
	}
	copy := *user
	return &copy, nil
}
func (repository *fakeRepository) FindUserByID(_ context.Context, id string) (*User, error) {
	for _, user := range repository.users {
		if user.ID == id {
			copy := *user
			return &copy, nil
		}
	}
	return nil, ErrNotFound
}
func (repository *fakeRepository) CreateUser(_ context.Context, email, password string, autoset, verified bool) (*User, error) {
	user := &User{ID: "new-user-id", Email: email, Password: password, IsActive: true, IsPasswordAutoset: autoset, IsEmailVerified: verified}
	repository.users[email] = user
	copy := *user
	return &copy, nil
}
func (repository *fakeRepository) CreateOAuthUser(_ context.Context, identity OAuthIdentity, password string) (*User, error) {
	return repository.CreateUser(context.Background(), identity.Email, password, true, true)
}
func (repository *fakeRepository) SyncOAuthUser(context.Context, string, OAuthIdentity, time.Time) error {
	return nil
}
func (repository *fakeRepository) UpsertOAuthAccount(context.Context, string, OAuthIdentity, OAuthTokens, time.Time) error {
	return nil
}
func (repository *fakeRepository) ProcessAcceptedInvitations(context.Context, *User, time.Time) error {
	return nil
}
func (repository *fakeRepository) RecordAuthentication(_ context.Context, user *User, medium, ip, agent string, at time.Time) error {
	repository.loginCount++
	user.LastLoginMedium = medium
	user.LastLoginIP = ip
	user.LastLoginUserAgent = agent
	user.LastLogin = &at
	return nil
}
func (repository *fakeRepository) RecordSessionLogin(_ context.Context, userID string, at time.Time) error {
	for _, user := range repository.users {
		if user.ID == userID {
			user.LastLogin = &at
		}
	}
	return nil
}
func (repository *fakeRepository) RecordLogout(context.Context, string, string, time.Time) error {
	return nil
}

func (repository *fakeRepository) UpdatePassword(_ context.Context, target *User, password string, _ time.Time) error {
	repository.updatedPassword = password
	for _, user := range repository.users {
		if user.ID == target.ID {
			user.Password = password
			user.IsPasswordAutoset = false
		}
	}
	return nil
}

type fakeSessionRepository struct{ records map[string]*SessionRecord }

func (repository *fakeSessionRepository) Load(_ context.Context, key string, now time.Time) (*SessionRecord, error) {
	record, exists := repository.records[key]
	if !exists || !record.ExpireDate.After(now) {
		return nil, ErrNotFound
	}
	copy := *record
	return &copy, nil
}
func (repository *fakeSessionRepository) Save(_ context.Context, record *SessionRecord) error {
	copy := *record
	repository.records[record.SessionKey] = &copy
	return nil
}
func (repository *fakeSessionRepository) Delete(_ context.Context, key string) error {
	delete(repository.records, key)
	return nil
}

func newAuthTestRouter(t *testing.T, repository *fakeRepository, options ...HandlerOption) (*gin.Engine, *fakeSessionRepository) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	settings := Settings{
		SecretKey: "pace-test-secret", AppBaseURL: "http://app.pace.test", SpaceBaseURL: "http://space.pace.test",
		SpaceBasePath: "/spaces/", SessionCookieName: "session-id", SessionCookieAge: time.Hour,
		CSRFCookieName: "csrftoken", CSRFCookieAge: time.Hour,
	}
	sessionRepository := &fakeSessionRepository{records: map[string]*SessionRecord{}}
	sessions, err := NewSessionManager(sessionRepository, repository, settings)
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	NewHandler(repository, sessions, settings, options...).Register(router)
	return router, sessionRepository
}

type fakeMagicStore struct {
	generatedEmail string
	verifiedCode   string
	verifyError    *Error
}

func (store *fakeMagicStore) Generate(_ context.Context, email string, _ bool) (string, string, *Error, error) {
	store.generatedEmail = email
	return "magic_" + email, "123456", nil, nil
}
func (store *fakeMagicStore) Verify(_ context.Context, _, code, _ string, _ bool) (*Error, error) {
	store.verifiedCode = code
	return store.verifyError, nil
}

type fakeTaskPublisher struct {
	magicEmail  string
	forgotEmail string
	activation  string
}

func (publisher *fakeTaskPublisher) PublishMagicLink(_ context.Context, email, _, _ string) error {
	publisher.magicEmail = email
	return nil
}
func (publisher *fakeTaskPublisher) PublishForgotPassword(_ context.Context, _, email, _, _, _ string) error {
	publisher.forgotEmail = email
	return nil
}
func (publisher *fakeTaskPublisher) PublishUserActivation(_ context.Context, _, userID string) error {
	publisher.activation = userID
	return nil
}

func csrfCredentials(t *testing.T, router http.Handler) (*http.Cookie, string) {
	t.Helper()
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/auth/get-csrf-token/", nil)
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("csrf status = %d", response.Code)
	}
	var payload struct {
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == "csrftoken" {
			return cookie, payload.CSRFToken
		}
	}
	t.Fatal("CSRF cookie missing")
	return nil, ""
}

func postForm(router http.Handler, path string, values url.Values, cookie *http.Cookie) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("User-Agent", "Pace Contract Test")
	request.RemoteAddr = "127.0.0.1:12345"
	if cookie != nil {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func TestCredentialSignInCreatesDjangoSession(t *testing.T) {
	encoded := "pbkdf2_sha256$1000$abcdefghijklmnopqrstuv$gyIKGYrTlRTAwlDBIge8uQnhX+Bk/ZRF8dGfP4K2Zjc="
	repository := &fakeRepository{configured: true, users: map[string]*User{
		"user@pace.test": {ID: "018f3c3e-1234-7abc-9def-1234567890ab", Email: "user@pace.test", Password: encoded, IsActive: true},
	}, configs: map[string]string{}}
	router, sessions := newAuthTestRouter(t, repository)
	csrfCookie, csrfToken := csrfCredentials(t, router)
	response := postForm(router, "/auth/sign-in/", url.Values{
		"email": {"user@pace.test"}, "password": {"Correct-Horse-Battery-7"}, "csrfmiddlewaretoken": {csrfToken},
	}, csrfCookie)
	if response.Code != http.StatusFound || response.Header().Get("Location") != "http://app.pace.test" {
		t.Fatalf("response = %d location=%q body=%q", response.Code, response.Header().Get("Location"), response.Body.String())
	}
	if repository.loginCount != 1 {
		t.Fatalf("login count = %d", repository.loginCount)
	}
	var sessionCookie *http.Cookie
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == "session-id" {
			sessionCookie = cookie
		}
	}
	if sessionCookie == nil {
		t.Fatal("session cookie missing")
	}
	record := sessions.records[sessionCookie.Value]
	if record == nil || record.UserID == nil || *record.UserID != "018f3c3e-1234-7abc-9def-1234567890ab" {
		t.Fatalf("session record = %#v", record)
	}
	codec, _ := NewSessionCodec("pace-test-secret", nil)
	data, err := codec.Decode(record.SessionData)
	if err != nil {
		t.Fatal(err)
	}
	if data["_auth_user_backend"] != djangoAuthenticationBackend {
		t.Fatalf("session data = %#v", data)
	}
}

func TestAuthenticateRotatesSessionSignedWithFallbackSecret(t *testing.T) {
	user := &User{ID: "018f3c3e-1234-7abc-9def-1234567890ab", Email: "user@pace.test", Password: "encoded-password", IsActive: true}
	repository := &fakeRepository{configured: true, users: map[string]*User{user.Email: user}, configs: map[string]string{}}
	sessionRepository := &fakeSessionRepository{records: map[string]*SessionRecord{}}
	settings := Settings{
		SecretKey: "current-secret", SecretKeyFallbacks: []string{"old-secret"},
		SessionCookieName: "session-id", SessionCookieAge: time.Hour,
	}
	manager, err := NewSessionManager(sessionRepository, repository, settings)
	if err != nil {
		t.Fatal(err)
	}
	oldCodec, err := NewSessionCodec("old-secret", nil)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := oldCodec.Encode(map[string]any{
		"_auth_user_id":      user.ID,
		"_auth_user_backend": djangoAuthenticationBackend,
		"_auth_user_hash":    sessionAuthHash(user.Password, "old-secret"),
	})
	if err != nil {
		t.Fatal(err)
	}
	const oldKey = "old-session-key"
	userID := user.ID
	sessionRepository.records[oldKey] = &SessionRecord{
		SessionKey: oldKey, SessionData: encoded, ExpireDate: time.Now().Add(time.Hour), UserID: &userID,
	}
	request := httptest.NewRequest(http.MethodGet, "/auth/set-password/", nil)
	request.AddCookie(&http.Cookie{Name: settings.SessionCookieName, Value: oldKey})
	response := httptest.NewRecorder()

	authenticated, session, err := manager.Authenticate(request.Context(), request, response)
	if err != nil {
		t.Fatal(err)
	}
	if authenticated.ID != user.ID {
		t.Fatalf("authenticated user = %#v", authenticated)
	}
	if session.Key == oldKey || session.Key == "" {
		t.Fatalf("rotated session key = %q", session.Key)
	}
	if _, exists := sessionRepository.records[oldKey]; exists {
		t.Fatal("old session row was not deleted")
	}
	rotated := sessionRepository.records[session.Key]
	if rotated == nil {
		t.Fatal("rotated session row was not saved")
	}
	data, err := manager.codec.Decode(rotated.SessionData)
	if err != nil {
		t.Fatal(err)
	}
	if data["_auth_user_hash"] != sessionAuthHash(user.Password, settings.SecretKey) {
		t.Fatalf("rotated auth hash = %#v", data["_auth_user_hash"])
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != settings.SessionCookieName || cookies[0].Value != session.Key {
		t.Fatalf("rotated cookies = %#v", cookies)
	}
}

func TestCredentialSignInContractErrors(t *testing.T) {
	repository := &fakeRepository{configured: true, users: map[string]*User{}, configs: map[string]string{}}
	router, _ := newAuthTestRouter(t, repository)
	csrfCookie, csrfToken := csrfCredentials(t, router)
	response := postForm(router, "/auth/sign-in/", url.Values{"csrfmiddlewaretoken": {csrfToken}}, csrfCookie)
	if response.Code != http.StatusFound || !strings.Contains(response.Header().Get("Location"), "REQUIRED_EMAIL_PASSWORD_SIGN_IN") {
		t.Fatalf("location = %q", response.Header().Get("Location"))
	}
	response = postForm(router, "/auth/sign-in/", url.Values{
		"email": {"missing@pace.test"}, "password": {"secret"}, "csrfmiddlewaretoken": {csrfToken},
	}, csrfCookie)
	if !strings.Contains(response.Header().Get("Location"), "USER_DOES_NOT_EXIST") {
		t.Fatalf("location = %q", response.Header().Get("Location"))
	}
}

func TestCredentialEndpointRejectsMissingCSRF(t *testing.T) {
	repository := &fakeRepository{configured: true, users: map[string]*User{}, configs: map[string]string{}}
	router, _ := newAuthTestRouter(t, repository)
	response := postForm(router, "/auth/sign-in/", url.Values{"email": {"user@pace.test"}, "password": {"secret"}}, nil)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d", response.Code)
	}
}

func TestEmailCheckContract(t *testing.T) {
	repository := &fakeRepository{configured: true, users: map[string]*User{
		"magic@pace.test": {ID: "user-id", Email: "magic@pace.test", IsPasswordAutoset: true},
	}, configs: map[string]string{"EMAIL_HOST": "smtp.pace.test", "ENABLE_MAGIC_LINK_LOGIN": "1"}}
	router, _ := newAuthTestRouter(t, repository)
	request := httptest.NewRequest(http.MethodPost, "/auth/email-check/", strings.NewReader(`{"email":"MAGIC@pace.test"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"existing":true`) || !strings.Contains(response.Body.String(), `"status":"MAGIC_CODE"`) {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}

func TestMagicGenerateAndSignInContracts(t *testing.T) {
	encoded := "pbkdf2_sha256$1000$abcdefghijklmnopqrstuv$gyIKGYrTlRTAwlDBIge8uQnhX+Bk/ZRF8dGfP4K2Zjc="
	repository := &fakeRepository{configured: true, users: map[string]*User{
		"magic@pace.test": {ID: "magic-user-id", Email: "magic@pace.test", Password: encoded, IsActive: true},
	}, configs: map[string]string{"EMAIL_HOST": "smtp.pace.test", "ENABLE_MAGIC_LINK_LOGIN": "1"}}
	magic := &fakeMagicStore{}
	tasks := &fakeTaskPublisher{}
	router, _ := newAuthTestRouter(t, repository, WithMagic(magic, tasks))

	request := httptest.NewRequest(http.MethodPost, "/auth/magic-generate/", strings.NewReader(`{"email":"MAGIC@pace.test"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"key":"magic_magic@pace.test"`) {
		t.Fatalf("generate response = %d %s", response.Code, response.Body.String())
	}
	if magic.generatedEmail != "magic@pace.test" || tasks.magicEmail != "magic@pace.test" {
		t.Fatalf("magic=%q task=%q", magic.generatedEmail, tasks.magicEmail)
	}

	csrfCookie, csrfToken := csrfCredentials(t, router)
	response = postForm(router, "/auth/magic-sign-in/", url.Values{
		"email": {"magic@pace.test"}, "code": {"123456"}, "csrfmiddlewaretoken": {csrfToken},
	}, csrfCookie)
	if response.Code != http.StatusFound || strings.Contains(response.Header().Get("Location"), "error_code") {
		t.Fatalf("sign-in response = %d location=%q", response.Code, response.Header().Get("Location"))
	}
	if magic.verifiedCode != "123456" || repository.loginCount != 1 {
		t.Fatalf("verified=%q logins=%d", magic.verifiedCode, repository.loginCount)
	}
}

func TestForgotPasswordPublishesDjangoCeleryTask(t *testing.T) {
	repository := &fakeRepository{configured: true, users: map[string]*User{
		"user@pace.test": {ID: "user-id", Email: "user@pace.test", FirstName: "Pace", Password: "encoded"},
	}, configs: map[string]string{"EMAIL_HOST": "smtp.pace.test"}}
	tasks := &fakeTaskPublisher{}
	router, _ := newAuthTestRouter(t, repository, WithMagic(&fakeMagicStore{}, tasks))
	request := httptest.NewRequest(http.MethodPost, "/auth/forgot-password/", strings.NewReader(`{"email":"user@pace.test"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || tasks.forgotEmail != "user@pace.test" {
		t.Fatalf("response=%d body=%s task=%q", response.Code, response.Body.String(), tasks.forgotEmail)
	}
}

func TestChangePasswordRequiresAuthenticatedDjangoSession(t *testing.T) {
	repository := &fakeRepository{configured: true, users: map[string]*User{}, configs: map[string]string{}}
	router, _ := newAuthTestRouter(t, repository)
	request := httptest.NewRequest(http.MethodPost, "/auth/change-password/", strings.NewReader(`{"old_password":"old","new_password":"new"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || !strings.Contains(response.Body.String(), "Authentication credentials were not provided") {
		t.Fatalf("response=%d body=%s", response.Code, response.Body.String())
	}
}

func TestChangePasswordRefreshesSessionWithNewHash(t *testing.T) {
	oldEncoded := "pbkdf2_sha256$1000$abcdefghijklmnopqrstuv$gyIKGYrTlRTAwlDBIge8uQnhX+Bk/ZRF8dGfP4K2Zjc="
	repository := &fakeRepository{configured: true, users: map[string]*User{
		"user@pace.test": {ID: "user-id", Email: "user@pace.test", Password: oldEncoded, IsActive: true},
	}, configs: map[string]string{}}
	router, _ := newAuthTestRouter(t, repository)
	csrfCookie, csrfToken := csrfCredentials(t, router)
	login := postForm(router, "/auth/sign-in/", url.Values{
		"email": {"user@pace.test"}, "password": {"Correct-Horse-Battery-7"}, "csrfmiddlewaretoken": {csrfToken},
	}, csrfCookie)
	var sessionCookie *http.Cookie
	for _, cookie := range login.Result().Cookies() {
		if cookie.Name == "session-id" {
			sessionCookie = cookie
		}
	}
	if sessionCookie == nil {
		t.Fatal("login session cookie missing")
	}
	request := httptest.NewRequest(http.MethodPost, "/auth/change-password/", strings.NewReader(`{"old_password":"Correct-Horse-Battery-7","new_password":"A-New-Pace-Passphrase-42!"}`))
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(sessionCookie)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("response=%d body=%s", response.Code, response.Body.String())
	}
	if repository.updatedPassword == "" || !VerifyPassword("A-New-Pace-Passphrase-42!", repository.updatedPassword) {
		t.Fatal("new Django-compatible password was not persisted")
	}
	if len(response.Result().Cookies()) == 0 {
		t.Fatal("password change should rotate the session cookie")
	}
}

func TestAuthRedirectFlowUsesSharedRateLimit(t *testing.T) {
	repository := &fakeRepository{configured: true, users: map[string]*User{}, configs: map[string]string{}}
	limiter, err := newMemoryRateLimiter("1/minute")
	if err != nil {
		t.Fatal(err)
	}
	router, _ := newAuthTestRouter(t, repository, WithRateLimiter(limiter))
	csrfCookie, csrfToken := csrfCredentials(t, router)
	values := url.Values{"csrfmiddlewaretoken": {csrfToken}}
	first := postForm(router, "/auth/sign-in/", values, csrfCookie)
	if !strings.Contains(first.Header().Get("Location"), "REQUIRED_EMAIL_PASSWORD_SIGN_IN") {
		t.Fatalf("first location = %q", first.Header().Get("Location"))
	}
	second := postForm(router, "/auth/sign-up/", values, csrfCookie)
	if !strings.Contains(second.Header().Get("Location"), "RATE_LIMIT_EXCEEDED") {
		t.Fatalf("second location = %q", second.Header().Get("Location"))
	}
}

func TestOAuthInitiationPersistsStateInDjangoSession(t *testing.T) {
	repository := &fakeRepository{configured: true, users: map[string]*User{}, configs: map[string]string{
		"GOOGLE_CLIENT_ID": "google-id", "GOOGLE_CLIENT_SECRET": "google-secret",
	}}
	router, sessions := newAuthTestRouter(t, repository)
	request := httptest.NewRequest(http.MethodGet, "/auth/google/?next_path=%2Fworkspace", nil)
	request.Host = "api.pace.test"
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusFound || !strings.HasPrefix(response.Header().Get("Location"), "https://accounts.google.com/") {
		t.Fatalf("response=%d location=%q", response.Code, response.Header().Get("Location"))
	}
	var sessionCookie *http.Cookie
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == "session-id" {
			sessionCookie = cookie
		}
	}
	if sessionCookie == nil || sessions.records[sessionCookie.Value] == nil {
		t.Fatal("OAuth state session missing")
	}
	codec, _ := NewSessionCodec("pace-test-secret", nil)
	data, err := codec.Decode(sessions.records[sessionCookie.Value].SessionData)
	if err != nil || data["state"] == "" || data["next_path"] != "/workspace" {
		t.Fatalf("session=%#v err=%v", data, err)
	}
}

func TestAuthenticationRouteInventoryMatchesDjangoModule(t *testing.T) {
	repository := &fakeRepository{configured: true, users: map[string]*User{}, configs: map[string]string{}}
	router, _ := newAuthTestRouter(t, repository)
	routes := router.Routes()
	if len(routes) != 37 {
		t.Fatalf("registered routes = %d, want 37: %#v", len(routes), routes)
	}
	want := map[string]string{
		"GET /auth/get-csrf-token/": "", "POST /auth/sign-in/": "", "POST /auth/sign-up/": "",
		"POST /auth/spaces/sign-in/": "", "POST /auth/spaces/sign-up/": "", "POST /auth/sign-out/": "",
		"POST /auth/spaces/sign-out/": "", "POST /auth/email-check/": "", "POST /auth/spaces/email-check/": "",
		"POST /auth/magic-generate/": "", "POST /auth/magic-sign-in/": "", "POST /auth/magic-sign-up/": "",
		"POST /auth/spaces/magic-generate/": "", "POST /auth/spaces/magic-sign-in/": "", "POST /auth/spaces/magic-sign-up/": "",
		"POST /auth/forgot-password/": "", "POST /auth/spaces/forgot-password/": "",
		"POST /auth/reset-password/:uid/:token/": "", "POST /auth/spaces/reset-password/:uid/:token/": "",
		"POST /auth/change-password/": "", "POST /auth/set-password/": "",
	}
	for _, provider := range []string{"google", "github", "gitlab", "gitea"} {
		want["GET /auth/"+provider+"/"] = ""
		want["GET /auth/"+provider+"/callback/"] = ""
		want["GET /auth/spaces/"+provider+"/"] = ""
		want["GET /auth/spaces/"+provider+"/callback/"] = ""
	}
	for _, route := range routes {
		delete(want, route.Method+" "+route.Path)
	}
	if len(want) != 0 {
		t.Fatalf("missing routes = %#v", want)
	}
}

func TestSessionRepositoryNotFoundSentinel(t *testing.T) {
	repository := &fakeSessionRepository{records: map[string]*SessionRecord{}}
	_, err := repository.Load(context.Background(), "missing", time.Now())
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v", err)
	}
}
