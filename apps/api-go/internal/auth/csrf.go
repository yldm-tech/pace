package auth

import (
	"crypto/subtle"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	csrfSecretLength = 32
	csrfCharacters   = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
)

type CSRF struct{ settings Settings }

func NewCSRF(settings Settings) *CSRF {
	if settings.CSRFCookieName == "" {
		settings.CSRFCookieName = "csrftoken"
	}
	return &CSRF{settings: settings}
}

func (csrf *CSRF) Token(request *http.Request, response http.ResponseWriter) (string, error) {
	secret := ""
	if cookie, err := request.Cookie(csrf.settings.CSRFCookieName); err == nil && validCSRFValue(cookie.Value) {
		secret = unmaskCSRF(cookie.Value)
	}
	if secret == "" {
		var err error
		secret, err = randomString(csrfSecretLength, csrfCharacters)
		if err != nil {
			return "", err
		}
	}
	http.SetCookie(response, &http.Cookie{
		Name: csrf.settings.CSRFCookieName, Value: secret, Path: "/", Domain: csrf.settings.CSRFCookieDomain,
		MaxAge: int(csrf.settings.CSRFCookieAge.Seconds()), Expires: nowUTC().Add(csrf.settings.CSRFCookieAge),
		Secure: csrf.settings.CSRFCookieSecure, HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
	appendVaryCookie(response.Header())
	return maskCSRF(secret)
}

func (csrf *CSRF) Check(request *http.Request) error {
	if request.Method == http.MethodGet || request.Method == http.MethodHead || request.Method == http.MethodOptions || request.Method == http.MethodTrace {
		return nil
	}
	cookie, err := request.Cookie(csrf.settings.CSRFCookieName)
	if err != nil || !validCSRFValue(cookie.Value) {
		return fmt.Errorf("CSRF cookie not set")
	}
	if origin := request.Header.Get("Origin"); origin != "" && !csrf.originAllowed(request, origin) {
		return fmt.Errorf("origin checking failed")
	}
	if request.TLS != nil && request.Header.Get("Origin") == "" {
		referer := request.Header.Get("Referer")
		if referer == "" || !csrf.originAllowed(request, referer) {
			return fmt.Errorf("referer checking failed")
		}
	}
	provided := request.FormValue("csrfmiddlewaretoken")
	if provided == "" {
		provided = request.Header.Get("X-CSRFToken")
	}
	if !validCSRFValue(provided) {
		return fmt.Errorf("CSRF token missing or malformed")
	}
	secret := unmaskCSRF(cookie.Value)
	provided = unmaskCSRF(provided)
	if subtle.ConstantTimeCompare([]byte(secret), []byte(provided)) != 1 {
		return fmt.Errorf("CSRF token incorrect")
	}
	return nil
}

func (csrf *CSRF) Rotate(response http.ResponseWriter) error {
	secret, err := randomString(csrfSecretLength, csrfCharacters)
	if err != nil {
		return err
	}
	http.SetCookie(response, &http.Cookie{
		Name: csrf.settings.CSRFCookieName, Value: secret, Path: "/", Domain: csrf.settings.CSRFCookieDomain,
		MaxAge: int(csrf.settings.CSRFCookieAge.Seconds()), Expires: nowUTC().Add(csrf.settings.CSRFCookieAge),
		Secure: csrf.settings.CSRFCookieSecure, HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
	appendVaryCookie(response.Header())
	return nil
}

func maskCSRF(secret string) (string, error) {
	mask, err := randomString(csrfSecretLength, csrfCharacters)
	if err != nil {
		return "", err
	}
	var cipher strings.Builder
	cipher.Grow(csrfSecretLength)
	for index := 0; index < csrfSecretLength; index++ {
		secretIndex := strings.IndexByte(csrfCharacters, secret[index])
		maskIndex := strings.IndexByte(csrfCharacters, mask[index])
		cipher.WriteByte(csrfCharacters[(secretIndex+maskIndex)%len(csrfCharacters)])
	}
	return mask + cipher.String(), nil
}

func unmaskCSRF(token string) string {
	if len(token) == csrfSecretLength {
		return token
	}
	if len(token) != csrfSecretLength*2 {
		return ""
	}
	mask := token[:csrfSecretLength]
	cipher := token[csrfSecretLength:]
	var secret strings.Builder
	secret.Grow(csrfSecretLength)
	for index := 0; index < csrfSecretLength; index++ {
		cipherIndex := strings.IndexByte(csrfCharacters, cipher[index])
		maskIndex := strings.IndexByte(csrfCharacters, mask[index])
		secret.WriteByte(csrfCharacters[(cipherIndex-maskIndex+len(csrfCharacters))%len(csrfCharacters)])
	}
	return secret.String()
}

func validCSRFValue(value string) bool {
	if len(value) != csrfSecretLength && len(value) != csrfSecretLength*2 {
		return false
	}
	for _, character := range value {
		if !strings.ContainsRune(csrfCharacters, character) {
			return false
		}
	}
	return true
}

func (csrf *CSRF) originAllowed(request *http.Request, rawOrigin string) bool {
	origin, err := url.Parse(rawOrigin)
	if err != nil || origin.Scheme == "" || origin.Host == "" {
		return false
	}
	scheme := "http"
	if request.TLS != nil || strings.EqualFold(request.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	if strings.EqualFold(origin.Scheme+"://"+origin.Host, scheme+"://"+request.Host) {
		return true
	}
	for _, trusted := range csrf.settings.CSRFTrustedOrigins {
		trusted = strings.TrimRight(trusted, "/")
		if strings.EqualFold(origin.Scheme+"://"+origin.Host, trusted) {
			return true
		}
		trustedURL, err := url.Parse(trusted)
		if err == nil && strings.EqualFold(origin.Scheme, trustedURL.Scheme) && strings.HasPrefix(trustedURL.Hostname(), "*.") {
			domain := strings.TrimPrefix(strings.ToLower(trustedURL.Hostname()), "*.")
			host := strings.ToLower(origin.Hostname())
			if host == domain || strings.HasSuffix(host, "."+domain) {
				return true
			}
		}
	}
	if domain := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(csrf.settings.CSRFCookieDomain)), "."); domain != "" {
		host := strings.ToLower(origin.Hostname())
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return true
		}
	}
	return false
}

var nowUTC = func() time.Time { return time.Now().UTC() }
