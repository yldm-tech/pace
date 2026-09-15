package user

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	redis "github.com/redis/go-redis/v9"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
)

type fakeUsers struct{ current *auth.User }

func (fakeUsers) InstanceConfigured(context.Context) (bool, error)                   { return true, nil }
func (fakeUsers) ConfigurationValue(context.Context, string, string) (string, error) { return "", nil }
func (fakeUsers) SignupAllowed(context.Context, string) (bool, error)                { return false, nil }
func (fakeUsers) FindUserByEmail(context.Context, string) (*auth.User, error) {
	return nil, auth.ErrNotFound
}

func (users fakeUsers) FindUserByID(_ context.Context, id string) (*auth.User, error) {
	if users.current != nil && users.current.ID == id {
		copy := *users.current
		return &copy, nil
	}
	return nil, auth.ErrNotFound
}
func (fakeUsers) CreateUser(context.Context, string, string, bool, bool) (*auth.User, error) {
	return nil, nil
}
func (fakeUsers) CreateOAuthUser(context.Context, auth.OAuthIdentity, string) (*auth.User, error) {
	return nil, nil
}
func (fakeUsers) SyncOAuthUser(context.Context, string, auth.OAuthIdentity, time.Time) error {
	return nil
}
func (fakeUsers) UpsertOAuthAccount(context.Context, string, auth.OAuthIdentity, auth.OAuthTokens, time.Time) error {
	return nil
}
func (fakeUsers) ProcessAcceptedInvitations(context.Context, *auth.User, time.Time) error { return nil }
func (fakeUsers) RecordAuthentication(context.Context, *auth.User, string, string, string, time.Time) error {
	return nil
}
func (fakeUsers) RecordSessionLogin(context.Context, string, time.Time) error         { return nil }
func (fakeUsers) RecordLogout(context.Context, string, string, time.Time) error       { return nil }
func (fakeUsers) UpdatePassword(context.Context, *auth.User, string, time.Time) error { return nil }

type fakeSessions struct {
	records map[string]*auth.SessionRecord
}

func (sessions *fakeSessions) Load(_ context.Context, key string, _ time.Time) (*auth.SessionRecord, error) {
	record, ok := sessions.records[key]
	if !ok {
		return nil, auth.ErrNotFound
	}
	return record, nil
}
func (sessions *fakeSessions) Save(_ context.Context, record *auth.SessionRecord) error {
	sessions.records[record.SessionKey] = record
	return nil
}
func (sessions *fakeSessions) Delete(_ context.Context, key string) error {
	delete(sessions.records, key)
	return nil
}

func TestUserSerializersPreferStaticAvatarAsset(t *testing.T) {
	avatarID := "018f3c3e-1234-7abc-9def-1234567890ab"
	coverID := "018f3c3e-1234-7abc-9def-1234567890ac"
	cover := "https://cdn.pace.test/cover.png"
	serialized := userMe(&auth.User{ID: "user-id", Avatar: "https://cdn.pace.test/avatar.png", AvatarAssetID: &avatarID, CoverImage: &cover, CoverImageAssetID: &coverID})
	if serialized["avatar_url"] != "/api/assets/v2/static/"+avatarID+"/" {
		t.Fatalf("avatar_url = %#v", serialized["avatar_url"])
	}
	if serialized["cover_image_url"] != "/api/assets/v2/static/"+coverID+"/" {
		t.Fatalf("cover_image_url = %#v", serialized["cover_image_url"])
	}
}

func TestUnauthenticatedUserSessionMatchesDjangoContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sessionRepository := &fakeSessions{records: map[string]*auth.SessionRecord{}}
	settings := auth.Settings{SecretKey: "pace-user-test", SessionCookieName: "session-id", SessionCookieAge: time.Hour}
	sessions, err := auth.NewSessionManager(sessionRepository, fakeUsers{}, settings)
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	NewHandler(nil, sessions, fakeUsers{}, Settings{}).Register(router)
	request := httptest.NewRequest(http.MethodGet, "/api/users/session/", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != `{"is_authenticated":false}` {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}

func TestEmailVerificationCodeUsesDjangoRedisContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	redisServer := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	user := &auth.User{ID: "user-id", Email: "old@pace.test", IsActive: true}
	users := fakeUsers{current: user}
	sessionRepository := &fakeSessions{records: map[string]*auth.SessionRecord{}}
	settings := auth.Settings{SecretKey: "pace-user-test", SessionCookieName: "session-id", SessionCookieAge: time.Hour}
	sessions, err := auth.NewSessionManager(sessionRepository, users, settings)
	if err != nil {
		t.Fatal(err)
	}
	loginRequest := httptest.NewRequest(http.MethodGet, "/", nil)
	loginResponse := httptest.NewRecorder()
	session, err := sessions.Login(context.Background(), loginRequest, user, "http://app.pace.test")
	if err != nil {
		t.Fatal(err)
	}
	sessions.SetCookie(loginResponse, session.Key)
	cookie := loginResponse.Result().Cookies()[0]
	router := gin.New()
	handler := NewHandler(nil, sessions, users, Settings{})
	handler.SetRedis(redisClient)
	handler.Register(router)
	request := httptest.NewRequest(http.MethodPost, "/api/users/me/email/generate-code/", strings.NewReader(`{"email":"new@pace.test"}`))
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
	if token, err := redisClient.Get(context.Background(), "magic_email_update_user-id_new@pace.test").Result(); err != nil || len(token) != 6 {
		t.Fatalf("stored token = %q err=%v", token, err)
	}
}

func TestUserRouteInventory(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sessionRepository := &fakeSessions{records: map[string]*auth.SessionRecord{}}
	settings := auth.Settings{SecretKey: "pace-user-test", SessionCookieName: "session-id", SessionCookieAge: time.Hour}
	sessions, err := auth.NewSessionManager(sessionRepository, fakeUsers{}, settings)
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	NewHandler(nil, sessions, fakeUsers{}, Settings{}).Register(router)
	if got := len(router.Routes()); got != 16 {
		t.Fatalf("user routes = %d, want 16", got)
	}
}
