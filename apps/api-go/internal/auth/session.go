package auth

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const djangoAuthenticationBackend = "django.contrib.auth.backends.ModelBackend"

type Settings struct {
	SecretKey               string
	SecretKeyFallbacks      []string
	WebURL                  string
	AppBaseURL              string
	SpaceBaseURL            string
	SpaceBasePath           string
	SessionCookieName       string
	SessionCookieDomain     string
	SessionCookieSecure     bool
	SessionCookieAge        time.Duration
	SessionSaveEveryRequest bool
	CSRFCookieName          string
	CSRFCookieDomain        string
	CSRFCookieSecure        bool
	CSRFCookieAge           time.Duration
	CSRFTrustedOrigins      []string
	AuthenticationRateLimit string
	Environment             map[string]string
}

type Session struct {
	Key    string
	Data   map[string]any
	UserID string
}

type SessionManager struct {
	repository SessionRepository
	users      Repository
	codec      *SessionCodec
	settings   Settings
	clock      func() time.Time
}

func NewSessionManager(repository SessionRepository, users Repository, settings Settings) (*SessionManager, error) {
	codec, err := NewSessionCodec(settings.SecretKey, settings.SecretKeyFallbacks)
	if err != nil {
		return nil, err
	}
	if settings.SessionCookieName == "" {
		settings.SessionCookieName = "session-id"
	}
	if settings.SessionCookieAge <= 0 {
		settings.SessionCookieAge = 7 * 24 * time.Hour
	}
	return &SessionManager{repository: repository, users: users, codec: codec, settings: settings, clock: time.Now}, nil
}

func (manager *SessionManager) Load(ctx context.Context, request *http.Request) (*Session, error) {
	cookie, err := request.Cookie(manager.settings.SessionCookieName)
	if errors.Is(err, http.ErrNoCookie) || strings.TrimSpace(cookie.Value) == "" {
		return &Session{Data: map[string]any{}}, nil
	}
	if err != nil {
		return nil, err
	}
	record, err := manager.repository.Load(ctx, cookie.Value, manager.clock().UTC())
	if errors.Is(err, ErrNotFound) {
		return &Session{Data: map[string]any{}}, nil
	}
	if err != nil {
		return nil, err
	}
	data, err := manager.codec.Decode(record.SessionData)
	if err != nil {
		return &Session{Data: map[string]any{}}, nil
	}
	userID := ""
	if record.UserID != nil {
		userID = *record.UserID
	}
	return &Session{Key: record.SessionKey, Data: data, UserID: userID}, nil
}

func (manager *SessionManager) Save(ctx context.Context, session *Session) error {
	if session.Key == "" {
		key, err := randomString(128, "abcdefghijklmnopqrstuvwxyz0123456789")
		if err != nil {
			return err
		}
		session.Key = key
	}
	encoded, err := manager.codec.Encode(session.Data)
	if err != nil {
		return err
	}
	var userID *string
	if session.UserID != "" {
		userID = &session.UserID
	}
	deviceInfo, _ := session.Data["device_info"].(map[string]any)
	return manager.repository.Save(ctx, &SessionRecord{
		SessionKey: session.Key, SessionData: encoded, ExpireDate: manager.clock().UTC().Add(manager.settings.SessionCookieAge),
		DeviceInfo: deviceInfo, UserID: userID,
	})
}

func (manager *SessionManager) Cycle(ctx context.Context, session *Session) error {
	oldKey := session.Key
	session.Key = ""
	if err := manager.Save(ctx, session); err != nil {
		session.Key = oldKey
		return err
	}
	if oldKey != "" {
		return manager.repository.Delete(ctx, oldKey)
	}
	return nil
}

func (manager *SessionManager) Login(ctx context.Context, request *http.Request, user *User, baseURL string) (*Session, error) {
	session, err := manager.Load(ctx, request)
	if err != nil {
		return nil, err
	}
	if existingID, _ := session.Data["_auth_user_id"].(string); existingID != "" && existingID != user.ID {
		session.Data = map[string]any{}
	}
	session.UserID = user.ID
	session.Data["_auth_user_id"] = user.ID
	session.Data["_auth_user_backend"] = djangoAuthenticationBackend
	session.Data["_auth_user_hash"] = sessionAuthHash(user.Password, manager.settings.SecretKey)
	session.Data["device_info"] = map[string]any{
		"user_agent": request.UserAgent(), "ip_address": clientIP(request), "domain": baseURL,
	}
	if err := manager.Cycle(ctx, session); err != nil {
		return nil, err
	}
	return session, nil
}

func (manager *SessionManager) Authenticate(ctx context.Context, request *http.Request, response http.ResponseWriter) (*User, *Session, error) {
	session, err := manager.Load(ctx, request)
	if err != nil {
		return nil, nil, err
	}
	userID, _ := session.Data["_auth_user_id"].(string)
	backend, _ := session.Data["_auth_user_backend"].(string)
	providedHash, _ := session.Data["_auth_user_hash"].(string)
	if userID == "" || backend != djangoAuthenticationBackend || providedHash == "" {
		return nil, session, ErrNotFound
	}
	user, err := manager.users.FindUserByID(ctx, userID)
	if err != nil || !user.IsActive {
		return nil, session, ErrNotFound
	}
	matchedSecret := -1
	for index, secret := range append([]string{manager.settings.SecretKey}, manager.settings.SecretKeyFallbacks...) {
		expected := sessionAuthHash(user.Password, secret)
		if subtle.ConstantTimeCompare([]byte(providedHash), []byte(expected)) == 1 {
			matchedSecret = index
			break
		}
	}
	if matchedSecret < 0 {
		return nil, session, ErrNotFound
	}
	if matchedSecret > 0 {
		session.Data["_auth_user_hash"] = sessionAuthHash(user.Password, manager.settings.SecretKey)
		if err := manager.Cycle(ctx, session); err != nil {
			return nil, session, err
		}
		manager.SetCookie(response, session.Key)
	}
	return user, session, nil
}

func (manager *SessionManager) Logout(ctx context.Context, request *http.Request) error {
	session, err := manager.Load(ctx, request)
	if err != nil {
		return err
	}
	if session.Key == "" {
		return nil
	}
	return manager.repository.Delete(ctx, session.Key)
}

func (manager *SessionManager) SetCookie(response http.ResponseWriter, key string) {
	http.SetCookie(response, &http.Cookie{
		Name: manager.settings.SessionCookieName, Value: key, Path: "/", Domain: manager.settings.SessionCookieDomain,
		MaxAge: int(manager.settings.SessionCookieAge.Seconds()), Expires: manager.clock().UTC().Add(manager.settings.SessionCookieAge),
		Secure: manager.settings.SessionCookieSecure, HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
	appendVaryCookie(response.Header())
}

func (manager *SessionManager) DeleteCookie(response http.ResponseWriter) {
	http.SetCookie(response, &http.Cookie{
		Name: manager.settings.SessionCookieName, Value: "", Path: "/", Domain: manager.settings.SessionCookieDomain,
		MaxAge: -1, Expires: time.Unix(1, 0).UTC(), Secure: manager.settings.SessionCookieSecure,
		HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
	appendVaryCookie(response.Header())
}

func appendVaryCookie(header http.Header) {
	for _, value := range header.Values("Vary") {
		for _, item := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(item), "Cookie") {
				return
			}
		}
	}
	header.Add("Vary", "Cookie")
}

func clientIP(request *http.Request) string {
	if forwarded := request.Header.Get("X-Forwarded-For"); forwarded != "" {
		return strings.TrimSpace(strings.Split(forwarded, ",")[0])
	}
	address := request.RemoteAddr
	if host, _, found := strings.Cut(address, ":"); found {
		return strings.Trim(host, "[]")
	}
	return address
}

func (manager *SessionManager) String() string {
	return fmt.Sprintf("session manager (%s)", manager.settings.SessionCookieName)
}
