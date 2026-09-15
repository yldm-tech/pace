package auth

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestDjangoCompatibleCSRFMaskAndOrigin(t *testing.T) {
	settings := Settings{
		CSRFCookieName: "csrftoken", CSRFCookieAge: time.Hour,
		CSRFTrustedOrigins: []string{"https://*.pace.test"},
	}
	csrf := NewCSRF(settings)
	getRequest := httptest.NewRequest(http.MethodGet, "https://api.pace.test/auth/get-csrf-token/", nil)
	getResponse := httptest.NewRecorder()
	token, err := csrf.Token(getRequest, getResponse)
	if err != nil {
		t.Fatal(err)
	}
	cookie := getResponse.Result().Cookies()[0]
	if len(cookie.Value) != csrfSecretLength || len(token) != csrfSecretLength*2 || unmaskCSRF(token) != cookie.Value {
		t.Fatalf("cookie=%q token=%q", cookie.Value, token)
	}
	form := url.Values{"csrfmiddlewaretoken": {token}}
	post := httptest.NewRequest(http.MethodPost, "https://api.pace.test/auth/sign-in/", strings.NewReader(form.Encode()))
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	post.Header.Set("Origin", "https://app.pace.test")
	post.AddCookie(cookie)
	if err := csrf.Check(post); err != nil {
		t.Fatal(err)
	}
	post.Header.Set("Origin", "https://attacker.test")
	if err := csrf.Check(post); err == nil {
		t.Fatal("untrusted origin should be rejected")
	}
}
