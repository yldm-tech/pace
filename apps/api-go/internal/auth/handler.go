package auth

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
	zxcvbn "github.com/nbutton23/zxcvbn-go"
	"github.com/yldm-tech/pace/apps/api-go/internal/drf"
)

type Handler struct {
	repository  Repository
	sessions    *SessionManager
	csrf        *CSRF
	magic       MagicStore
	tasks       TaskPublisher
	limiter     RateLimiter
	settings    Settings
	clock       func() time.Time
	validator   *validator.Validate
	resetTokens *PasswordResetTokens
	oauthHTTP   *http.Client
	cache       CacheInvalidator
}

type HandlerOption func(*Handler)

func WithMagic(store MagicStore, publisher TaskPublisher) HandlerOption {
	return func(handler *Handler) {
		handler.magic = store
		handler.tasks = publisher
	}
}

func WithRateLimiter(limiter RateLimiter) HandlerOption {
	return func(handler *Handler) { handler.limiter = limiter }
}

func WithCacheInvalidator(invalidator CacheInvalidator) HandlerOption {
	return func(handler *Handler) { handler.cache = invalidator }
}

func WithOAuthHTTPClient(client *http.Client) HandlerOption {
	return func(handler *Handler) { handler.oauthHTTP = client }
}

func NewHandler(repository Repository, sessions *SessionManager, settings Settings, options ...HandlerOption) *Handler {
	limiter, err := newMemoryRateLimiter(settings.AuthenticationRateLimit)
	if err != nil {
		limiter, _ = newMemoryRateLimiter("10/minute")
	}
	handler := &Handler{
		repository: repository, sessions: sessions, csrf: NewCSRF(settings), settings: settings,
		clock: time.Now, validator: validator.New(validator.WithRequiredStructEnabled()), limiter: limiter,
		resetTokens: NewPasswordResetTokens(settings.SecretKey, settings.SecretKeyFallbacks),
		oauthHTTP:   &http.Client{Timeout: 15 * time.Second},
	}
	for _, option := range options {
		option(handler)
	}
	return handler
}

func (handler *Handler) Register(router gin.IRouter) {
	router.GET("/auth/get-csrf-token/", handler.getCSRFToken)
	router.POST("/auth/sign-in/", handler.csrfProtected(handler.signIn(false)))
	router.POST("/auth/sign-up/", handler.csrfProtected(handler.signUp(false)))
	router.POST("/auth/spaces/sign-in/", handler.csrfProtected(handler.signIn(true)))
	router.POST("/auth/spaces/sign-up/", handler.csrfProtected(handler.signUp(true)))
	router.POST("/auth/sign-out/", handler.csrfProtected(handler.signOut(false)))
	router.POST("/auth/spaces/sign-out/", handler.csrfProtected(handler.signOut(true)))
	router.POST("/auth/email-check/", handler.emailCheck)
	router.POST("/auth/spaces/email-check/", handler.emailCheck)
	router.POST("/auth/magic-generate/", handler.magicGenerate)
	router.POST("/auth/spaces/magic-generate/", handler.magicGenerate)
	router.POST("/auth/magic-sign-in/", handler.csrfProtected(handler.magicSignIn(false)))
	router.POST("/auth/spaces/magic-sign-in/", handler.csrfProtected(handler.magicSignIn(true)))
	router.POST("/auth/magic-sign-up/", handler.csrfProtected(handler.magicSignUp(false)))
	router.POST("/auth/spaces/magic-sign-up/", handler.csrfProtected(handler.magicSignUp(true)))
	router.POST("/auth/forgot-password/", handler.forgotPassword(false))
	router.POST("/auth/spaces/forgot-password/", handler.forgotPassword(true))
	router.POST("/auth/reset-password/:uid/:token/", handler.csrfProtected(handler.resetPassword(false)))
	router.POST("/auth/spaces/reset-password/:uid/:token/", handler.csrfProtected(handler.resetPassword(true)))
	router.POST("/auth/change-password/", handler.changePassword)
	router.POST("/auth/set-password/", handler.setPassword)
	for _, provider := range []string{"google", "github", "gitlab", "gitea"} {
		router.GET("/auth/"+provider+"/", handler.oauthInitiate(provider, false))
		router.GET("/auth/"+provider+"/callback/", handler.oauthCallback(provider, false))
		router.GET("/auth/spaces/"+provider+"/", handler.oauthInitiate(provider, true))
		router.GET("/auth/spaces/"+provider+"/callback/", handler.oauthCallback(provider, true))
	}
}

func (handler *Handler) getCSRFToken(c *gin.Context) {
	token, err := handler.csrf.Token(c.Request, c.Writer)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, gin.H{"csrf_token": token})
}

func (handler *Handler) csrfProtected(next gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := c.Request.ParseForm(); err != nil {
			c.Data(http.StatusForbidden, "text/html; charset=utf-8", []byte("<!doctype html><html><body><h1>Forbidden</h1><p>CSRF verification failed.</p></body></html>"))
			return
		}
		if err := handler.csrf.Check(c.Request); err != nil {
			c.Data(http.StatusForbidden, "text/html; charset=utf-8", []byte("<!doctype html><html><body><h1>Forbidden</h1><p>CSRF verification failed.</p></body></html>"))
			return
		}
		next(c)
	}
}

func (handler *Handler) signIn(space bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		baseURL := handler.baseURL(space)
		nextPath := c.Request.FormValue("next_path")
		if !handler.allow(c, true, baseURL, nextPath) {
			return
		}
		if !handler.requireConfigured(c, baseURL, nextPath, true) {
			return
		}
		email := c.Request.FormValue("email")
		password := c.Request.FormValue("password")
		if email == "" || password == "" {
			payloadEmail := email
			if email == "" {
				payloadEmail = "False"
			}
			handler.redirectError(c, baseURL, nextPath, authError(ErrorRequiredEmailPasswordSignIn, "REQUIRED_EMAIL_PASSWORD_SIGN_IN", map[string]any{"email": payloadEmail}))
			return
		}
		email = strings.ToLower(strings.TrimSpace(email))
		if !handler.validEmail(email) {
			handler.redirectError(c, baseURL, nextPath, authError(ErrorInvalidEmailSignIn, "INVALID_EMAIL_SIGN_IN", map[string]any{"email": email}))
			return
		}
		user, err := handler.repository.FindUserByEmail(c.Request.Context(), email)
		if errors.Is(err, ErrNotFound) {
			handler.redirectError(c, baseURL, nextPath, authError(ErrorUserDoesNotExist, "USER_DOES_NOT_EXIST", map[string]any{"email": email}))
			return
		}
		if err != nil {
			handler.internalError(c, err)
			return
		}
		if !handler.emailPasswordEnabled(c.Request.Context()) {
			handler.redirectError(c, baseURL, nextPath, authError(ErrorEmailPasswordAuthenticationDisabled, "EMAIL_PASSWORD_AUTHENTICATION_DISABLED", nil))
			return
		}
		if !VerifyPassword(password, user.Password) {
			handler.redirectError(c, baseURL, nextPath, authError(ErrorAuthenticationFailedSignIn, "AUTHENTICATION_FAILED_SIGN_IN", map[string]any{"email": email}))
			return
		}
		if authenticationError := interactiveUserError(user); authenticationError != nil {
			handler.redirectError(c, baseURL, nextPath, authenticationError)
			return
		}
		if err := handler.recordAuthentication(c, user, "email", baseURL); err != nil {
			handler.internalError(c, err)
			return
		}
		if !space {
			if err := handler.repository.ProcessAcceptedInvitations(c.Request.Context(), user, handler.clock().UTC()); err != nil {
				handler.internalError(c, err)
				return
			}
		}
		if err := handler.completeLoginSession(c, user, baseURL); err != nil {
			handler.internalError(c, err)
			return
		}
		if space {
			c.Redirect(http.StatusFound, spaceSuccessURL(baseURL, nextPath))
			return
		}
		c.Redirect(http.StatusFound, safeRedirectURL(baseURL, nextPath, nil))
	}
}

func (handler *Handler) signUp(space bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		baseURL := handler.baseURL(space)
		nextPath := c.Request.FormValue("next_path")
		if !handler.allow(c, true, baseURL, nextPath) {
			return
		}
		if !handler.requireConfigured(c, baseURL, nextPath, true) {
			return
		}
		email := c.Request.FormValue("email")
		password := c.Request.FormValue("password")
		if email == "" || password == "" {
			payloadEmail := email
			if email == "" {
				payloadEmail = "False"
			}
			handler.redirectError(c, baseURL, nextPath, authError(ErrorRequiredEmailPasswordSignUp, "REQUIRED_EMAIL_PASSWORD_SIGN_UP", map[string]any{"email": payloadEmail}))
			return
		}
		email = strings.ToLower(strings.TrimSpace(email))
		if !handler.validEmail(email) {
			handler.redirectError(c, baseURL, nextPath, authError(ErrorInvalidEmailSignUp, "INVALID_EMAIL_SIGN_UP", map[string]any{"email": email}))
			return
		}
		if _, err := handler.repository.FindUserByEmail(c.Request.Context(), email); err == nil {
			handler.redirectError(c, baseURL, nextPath, authError(ErrorUserAlreadyExists, "USER_ALREADY_EXIST", map[string]any{"email": email}))
			return
		} else if !errors.Is(err, ErrNotFound) {
			handler.internalError(c, err)
			return
		}
		if !handler.emailPasswordEnabled(c.Request.Context()) {
			handler.redirectError(c, baseURL, nextPath, authError(ErrorEmailPasswordAuthenticationDisabled, "EMAIL_PASSWORD_AUTHENTICATION_DISABLED", nil))
			return
		}
		if !handler.signupEnabled(c.Request.Context(), email) {
			handler.redirectError(c, baseURL, nextPath, authError(ErrorSignupDisabled, "SIGNUP_DISABLED", map[string]any{"email": email}))
			return
		}
		if zxcvbn.PasswordStrength(password, nil).Score < 3 {
			handler.redirectError(c, baseURL, nextPath, authError(ErrorPasswordTooWeak, "PASSWORD_TOO_WEAK", map[string]any{"email": email}))
			return
		}
		encodedPassword, err := HashPassword(password)
		if err != nil {
			handler.internalError(c, err)
			return
		}
		user, err := handler.repository.CreateUser(c.Request.Context(), email, encodedPassword, false, false)
		if err != nil {
			handler.internalError(c, err)
			return
		}
		if err := handler.recordAuthentication(c, user, "email", baseURL); err != nil {
			handler.internalError(c, err)
			return
		}
		if !space {
			if err := handler.repository.ProcessAcceptedInvitations(c.Request.Context(), user, handler.clock().UTC()); err != nil {
				handler.internalError(c, err)
				return
			}
		}
		if err := handler.completeLoginSession(c, user, baseURL); err != nil {
			handler.internalError(c, err)
			return
		}
		if space {
			c.Redirect(http.StatusFound, spaceSuccessURL(baseURL, nextPath))
			return
		}
		c.Redirect(http.StatusFound, safeRedirectURL(baseURL, nextPath, nil))
	}
}

func (handler *Handler) signOut(space bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		baseURL := handler.baseURL(space)
		nextPath := ""
		if space {
			nextPath = c.Request.FormValue("next_path")
		}
		if user, _, err := handler.sessions.Authenticate(c.Request.Context(), c.Request, c.Writer); err == nil {
			_ = handler.repository.RecordLogout(c.Request.Context(), user.ID, clientIP(c.Request), handler.clock().UTC())
		}
		_ = handler.sessions.Logout(c.Request.Context(), c.Request)
		handler.sessions.DeleteCookie(c.Writer)
		if space {
			c.Redirect(http.StatusFound, safeRedirectURL(baseURL, nextPath, nil))
			return
		}
		c.Redirect(http.StatusFound, baseURL)
	}
}

func (handler *Handler) emailCheck(c *gin.Context) {
	if !handler.allow(c, false, "", "") {
		return
	}
	if !handler.requireConfigured(c, "", "", false) {
		return
	}
	var request struct {
		Email any `json:"email" form:"email"`
	}
	if err := c.ShouldBind(&request); err != nil {
		request.Email = nil
	}
	if request.Email == nil || strings.TrimSpace(toString(request.Email)) == "" {
		c.JSON(http.StatusBadRequest, authError(ErrorEmailRequired, "EMAIL_REQUIRED", nil).Response())
		return
	}
	email := strings.ToLower(strings.TrimSpace(toString(request.Email)))
	if !handler.validEmail(email) {
		c.JSON(http.StatusBadRequest, authError(ErrorInvalidEmail, "INVALID_EMAIL", nil).Response())
		return
	}
	smtpConfigured := handler.configurationValue(c.Request.Context(), "EMAIL_HOST", "") != ""
	magicEnabled := handler.configurationValue(c.Request.Context(), "ENABLE_MAGIC_LINK_LOGIN", "1") == "1"
	user, err := handler.repository.FindUserByEmail(c.Request.Context(), email)
	if err != nil && !errors.Is(err, ErrNotFound) {
		handler.internalError(c, err)
		return
	}
	existing := err == nil
	status := "CREDENTIAL"
	if smtpConfigured && magicEnabled && (!existing || user.IsPasswordAutoset) {
		status = "MAGIC_CODE"
	}
	drf.Respond(c, http.StatusOK, gin.H{"existing": existing, "status": status})
}

func (handler *Handler) magicGenerate(c *gin.Context) {
	if !handler.allow(c, false, "", "") || !handler.requireConfigured(c, "", "", false) {
		return
	}
	var request struct {
		Email string `json:"email" form:"email"`
	}
	if err := c.ShouldBind(&request); err != nil {
		request.Email = ""
	}
	email := strings.ToLower(strings.TrimSpace(request.Email))
	if !handler.validEmail(email) {
		c.JSON(http.StatusBadRequest, authError(ErrorInvalidEmail, "INVALID_EMAIL", nil).Response())
		return
	}
	if handler.configurationValue(c.Request.Context(), "EMAIL_HOST", "") == "" {
		c.JSON(http.StatusBadRequest, authError(ErrorSMTPNotConfigured, "SMTP_NOT_CONFIGURED", map[string]any{"email": email}).Response())
		return
	}
	if handler.configurationValue(c.Request.Context(), "ENABLE_MAGIC_LINK_LOGIN", "1") == "0" {
		c.JSON(http.StatusBadRequest, authError(ErrorMagicLinkLoginDisabled, "MAGIC_LINK_LOGIN_DISABLED", map[string]any{"email": email}).Response())
		return
	}
	if handler.magic == nil || handler.tasks == nil {
		handler.internalError(c, errors.New("magic authentication services are unavailable"))
		return
	}
	_, findErr := handler.repository.FindUserByEmail(c.Request.Context(), email)
	userExists := findErr == nil
	if findErr != nil && !errors.Is(findErr, ErrNotFound) {
		handler.internalError(c, findErr)
		return
	}
	key, token, authenticationError, err := handler.magic.Generate(c.Request.Context(), email, userExists)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if authenticationError != nil {
		c.JSON(http.StatusBadRequest, authenticationError.Response())
		return
	}
	if err := handler.tasks.PublishMagicLink(c.Request.Context(), email, key, token); err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, gin.H{"key": key})
}

func (handler *Handler) magicSignIn(space bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		baseURL := handler.baseURL(space)
		nextPath := c.Request.FormValue("next_path")
		if !handler.allow(c, true, baseURL, nextPath) {
			return
		}
		code := strings.TrimSpace(c.Request.FormValue("code"))
		email := strings.ToLower(strings.TrimSpace(c.Request.FormValue("email")))
		if code == "" || email == "" {
			handler.redirectError(c, baseURL, nextPath, authError(ErrorMagicSignInEmailCodeRequired, "MAGIC_SIGN_IN_EMAIL_CODE_REQUIRED", nil))
			return
		}
		user, err := handler.repository.FindUserByEmail(c.Request.Context(), email)
		if errors.Is(err, ErrNotFound) {
			handler.redirectError(c, baseURL, nextPath, authError(ErrorUserDoesNotExist, "USER_DOES_NOT_EXIST", nil))
			return
		}
		if err != nil {
			handler.internalError(c, err)
			return
		}
		if handler.magic == nil {
			handler.internalError(c, errors.New("magic authentication store is unavailable"))
			return
		}
		if authenticationError := handler.magicConfigurationError(c.Request.Context(), email); authenticationError != nil {
			handler.redirectError(c, baseURL, nextPath, authenticationError)
			return
		}
		authenticationError, err := handler.magic.Verify(c.Request.Context(), "magic_"+email, code, email, true)
		if err != nil {
			handler.internalError(c, err)
			return
		}
		if authenticationError != nil {
			handler.redirectError(c, baseURL, nextPath, authenticationError)
			return
		}
		if authenticationError = interactiveUserError(user); authenticationError != nil {
			handler.redirectError(c, baseURL, nextPath, authenticationError)
			return
		}
		if err := handler.recordAuthentication(c, user, "magic-code", baseURL); err != nil {
			handler.internalError(c, err)
			return
		}
		if !space {
			if err := handler.repository.ProcessAcceptedInvitations(c.Request.Context(), user, handler.clock().UTC()); err != nil {
				handler.internalError(c, err)
				return
			}
		}
		if err := handler.completeLoginSession(c, user, baseURL); err != nil {
			handler.internalError(c, err)
			return
		}
		if space {
			c.Redirect(http.StatusFound, spaceSuccessURL(baseURL, nextPath))
		} else {
			c.Redirect(http.StatusFound, safeRedirectURL(baseURL, nextPath, nil))
		}
	}
}

func (handler *Handler) magicSignUp(space bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		baseURL := handler.baseURL(space)
		nextPath := c.Request.FormValue("next_path")
		if !handler.allow(c, true, baseURL, nextPath) {
			return
		}
		code := strings.TrimSpace(c.Request.FormValue("code"))
		email := strings.ToLower(strings.TrimSpace(c.Request.FormValue("email")))
		if code == "" || email == "" {
			handler.redirectError(c, baseURL, nextPath, authError(ErrorMagicSignUpEmailCodeRequired, "MAGIC_SIGN_UP_EMAIL_CODE_REQUIRED", nil))
			return
		}
		if _, err := handler.repository.FindUserByEmail(c.Request.Context(), email); err == nil {
			handler.redirectError(c, baseURL, nextPath, authError(ErrorUserAlreadyExists, "USER_ALREADY_EXIST", nil))
			return
		} else if !errors.Is(err, ErrNotFound) {
			handler.internalError(c, err)
			return
		}
		if handler.magic == nil {
			handler.internalError(c, errors.New("magic authentication store is unavailable"))
			return
		}
		if authenticationError := handler.magicConfigurationError(c.Request.Context(), email); authenticationError != nil {
			handler.redirectError(c, baseURL, nextPath, authenticationError)
			return
		}
		authenticationError, err := handler.magic.Verify(c.Request.Context(), "magic_"+email, code, email, false)
		if err != nil {
			handler.internalError(c, err)
			return
		}
		if authenticationError != nil {
			handler.redirectError(c, baseURL, nextPath, authenticationError)
			return
		}
		if !handler.signupEnabled(c.Request.Context(), email) {
			handler.redirectError(c, baseURL, nextPath, authError(ErrorSignupDisabled, "SIGNUP_DISABLED", map[string]any{"email": email}))
			return
		}
		temporaryPassword, err := randomHex(16)
		if err != nil {
			handler.internalError(c, err)
			return
		}
		encodedPassword, err := HashPassword(temporaryPassword)
		if err != nil {
			handler.internalError(c, err)
			return
		}
		user, err := handler.repository.CreateUser(c.Request.Context(), email, encodedPassword, true, true)
		if err != nil {
			handler.internalError(c, err)
			return
		}
		if err := handler.recordAuthentication(c, user, "magic-code", baseURL); err != nil {
			handler.internalError(c, err)
			return
		}
		if !space {
			if err := handler.repository.ProcessAcceptedInvitations(c.Request.Context(), user, handler.clock().UTC()); err != nil {
				handler.internalError(c, err)
				return
			}
		}
		if err := handler.completeLoginSession(c, user, baseURL); err != nil {
			handler.internalError(c, err)
			return
		}
		if space {
			c.Redirect(http.StatusFound, spaceSuccessURL(baseURL, nextPath))
		} else {
			c.Redirect(http.StatusFound, safeRedirectURL(baseURL, nextPath, nil))
		}
	}
}

func (handler *Handler) forgotPassword(space bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !handler.allow(c, false, "", "") || !handler.requireConfigured(c, "", "", false) {
			return
		}
		if handler.configurationValue(c.Request.Context(), "EMAIL_HOST", "") == "" {
			c.JSON(http.StatusBadRequest, authError(ErrorSMTPNotConfigured, "SMTP_NOT_CONFIGURED", nil).Response())
			return
		}
		var request struct {
			Email string `json:"email" form:"email"`
		}
		if err := c.ShouldBind(&request); err != nil {
			request.Email = ""
		}
		email := strings.ToLower(strings.TrimSpace(request.Email))
		if !handler.validEmail(email) {
			c.JSON(http.StatusBadRequest, authError(ErrorInvalidEmail, "INVALID_EMAIL", nil).Response())
			return
		}
		user, err := handler.repository.FindUserByEmail(c.Request.Context(), email)
		if errors.Is(err, ErrNotFound) {
			c.JSON(http.StatusBadRequest, authError(ErrorUserDoesNotExist, "USER_DOES_NOT_EXIST", nil).Response())
			return
		}
		if err != nil {
			handler.internalError(c, err)
			return
		}
		if handler.tasks == nil {
			handler.internalError(c, errors.New("authentication task publisher is unavailable"))
			return
		}
		uid, token := handler.resetTokens.Make(user)
		if err := handler.tasks.PublishForgotPassword(c.Request.Context(), user.FirstName, user.Email, uid, token, handler.baseURL(space)); err != nil {
			handler.internalError(c, err)
			return
		}
		drf.Respond(c, http.StatusOK, gin.H{"message": "Check your email to reset your password"})
	}
}

func (handler *Handler) resetPassword(space bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid, err := decodePasswordResetUID(c.Param("uid"))
		if err != nil {
			if errors.Is(err, errPasswordResetUIDUnicode) {
				handler.passwordResetRedirect(c, space, authError(ErrorExpiredPasswordToken, "EXPIRED_PASSWORD_TOKEN", nil))
			} else {
				handler.passwordResetRedirect(c, space, authError(ErrorInvalidPasswordToken, "INVALID_PASSWORD_TOKEN", nil))
			}
			return
		}
		user, err := handler.repository.FindUserByID(c.Request.Context(), uid)
		if err != nil || !handler.resetTokens.Check(user, c.Param("token")) {
			handler.passwordResetRedirect(c, space, authError(ErrorInvalidPasswordToken, "INVALID_PASSWORD_TOKEN", nil))
			return
		}
		password := c.Request.FormValue("password")
		if password == "" {
			handler.passwordResetRedirect(c, space, authError(ErrorInvalidPassword, "INVALID_PASSWORD", nil))
			return
		}
		if zxcvbn.PasswordStrength(password, nil).Score < 3 {
			handler.passwordResetRedirect(c, space, authError(ErrorPasswordTooWeak, "PASSWORD_TOO_WEAK", nil))
			return
		}
		encoded, err := HashPassword(password)
		if err != nil {
			handler.internalError(c, err)
			return
		}
		if err := handler.repository.UpdatePassword(c.Request.Context(), user, encoded, handler.clock().UTC()); err != nil {
			handler.internalError(c, err)
			return
		}
		if space {
			c.Redirect(http.StatusFound, handler.baseURL(true))
			return
		}
		c.Redirect(http.StatusFound, strings.TrimRight(handler.baseURL(false), "/")+"/sign-in?success=True")
	}
}

func (handler *Handler) changePassword(c *gin.Context) {
	user, _, err := handler.sessions.Authenticate(c.Request.Context(), c.Request, c.Writer)
	if err != nil {
		handler.notAuthenticated(c)
		return
	}
	var request struct {
		OldPassword string `json:"old_password" form:"old_password"`
		NewPassword string `json:"new_password" form:"new_password"`
	}
	if err := c.ShouldBind(&request); err != nil {
		request = struct {
			OldPassword string `json:"old_password" form:"old_password"`
			NewPassword string `json:"new_password" form:"new_password"`
		}{}
	}
	if !user.IsPasswordAutoset && request.OldPassword == "" {
		c.JSON(http.StatusBadRequest, authError(ErrorMissingPassword, "MISSING_PASSWORD", map[string]any{"error": "Old password is missing"}).Response())
		return
	}
	if request.NewPassword == "" {
		c.JSON(http.StatusBadRequest, authError(ErrorMissingPassword, "MISSING_PASSWORD", map[string]any{"error": "Old or new password is missing"}).Response())
		return
	}
	if !user.IsPasswordAutoset && !VerifyPassword(request.OldPassword, user.Password) {
		c.JSON(http.StatusBadRequest, authError(ErrorIncorrectOldPassword, "INCORRECT_OLD_PASSWORD", map[string]any{"error": "Old password is not correct"}).Response())
		return
	}
	if zxcvbn.PasswordStrength(request.NewPassword, nil).Score < 3 {
		c.JSON(http.StatusBadRequest, authError(ErrorPasswordTooWeak, "PASSWORD_TOO_WEAK", nil).Response())
		return
	}
	encoded, err := HashPassword(request.NewPassword)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if err := handler.repository.UpdatePassword(c.Request.Context(), user, encoded, handler.clock().UTC()); err != nil {
		handler.internalError(c, err)
		return
	}
	user.Password = encoded
	user.IsPasswordAutoset = false
	if err := handler.completeLoginSession(c, user, handler.baseURL(false)); err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, gin.H{"message": "Password updated successfully"})
}

func (handler *Handler) setPassword(c *gin.Context) {
	user, _, err := handler.sessions.Authenticate(c.Request.Context(), c.Request, c.Writer)
	if err != nil {
		handler.notAuthenticated(c)
		return
	}
	if !user.IsPasswordAutoset {
		c.JSON(http.StatusBadRequest, authError(ErrorPasswordAlreadySet, "PASSWORD_ALREADY_SET", map[string]any{
			"error": "Your password is already set please change your password from profile",
		}).Response())
		return
	}
	if err := invalidateUserCache(c.Request.Context(), handler.cache, user.ID); err != nil {
		handler.internalError(c, err)
		return
	}
	var request struct {
		Password string `json:"password" form:"password"`
	}
	if err := c.ShouldBind(&request); err != nil {
		request.Password = ""
	}
	if request.Password == "" || zxcvbn.PasswordStrength(request.Password, nil).Score < 3 {
		c.JSON(http.StatusBadRequest, authError(ErrorInvalidPassword, "INVALID_PASSWORD", nil).Response())
		return
	}
	encoded, err := HashPassword(request.Password)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if err := handler.repository.UpdatePassword(c.Request.Context(), user, encoded, handler.clock().UTC()); err != nil {
		handler.internalError(c, err)
		return
	}
	user.Password = encoded
	user.IsPasswordAutoset = false
	if err := handler.completeLoginSession(c, user, handler.baseURL(false)); err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, serializeUser(user))
}

func (handler *Handler) passwordResetRedirect(c *gin.Context, space bool, authenticationError *Error) {
	values := url.Values{}
	values.Set("error_code", strconv.Itoa(authenticationError.Code))
	values.Set("error_message", authenticationError.Message)
	if space {
		c.Redirect(http.StatusFound, handler.baseURL(true)+"/accounts/reset-password/?"+values.Encode())
		return
	}
	baseURL := strings.TrimRight(handler.baseURL(false), "/")
	c.Redirect(http.StatusFound, baseURL+"/accounts/reset-password?"+values.Encode())
}

func (handler *Handler) notAuthenticated(c *gin.Context) {
	c.JSON(http.StatusUnauthorized, gin.H{"detail": "Authentication credentials were not provided."})
}

func serializeUser(user *User) gin.H {
	return gin.H{
		"id": user.ID, "last_login": user.LastLogin, "username": user.Username, "mobile_number": user.MobileNumber,
		"email": user.Email, "display_name": user.DisplayName, "first_name": user.FirstName, "last_name": user.LastName,
		"avatar": user.Avatar, "avatar_asset": user.AvatarAssetID, "cover_image": user.CoverImage,
		"cover_image_asset": user.CoverImageAssetID, "date_joined": user.DateJoined, "created_at": user.CreatedAt,
		"updated_at": user.UpdatedAt, "last_location": user.LastLocation, "created_location": user.CreatedLocation,
		"is_superuser": user.IsSuperuser, "is_managed": user.IsManaged, "is_password_expired": user.IsPasswordExpired,
		"is_active": user.IsActive, "is_staff": user.IsStaff, "is_email_verified": user.IsEmailVerified,
		"is_password_autoset": user.IsPasswordAutoset, "is_password_reset_required": user.IsPasswordResetRequired,
		"token": user.Token, "last_active": user.LastActive, "last_login_time": user.LastLoginTime,
		"last_logout_time": user.LastLogoutTime, "last_login_ip": user.LastLoginIP, "last_logout_ip": user.LastLogoutIP,
		"last_login_medium": user.LastLoginMedium, "last_login_uagent": user.LastLoginUserAgent,
		"token_updated_at": user.TokenUpdatedAt, "is_bot": user.IsBot, "bot_type": user.BotType,
		"user_timezone": user.UserTimezone, "is_email_valid": user.IsEmailValid, "masked_at": user.MaskedAt,
	}
}

func (handler *Handler) oauthInitiate(providerName string, space bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		baseURL := handler.baseURL(space)
		nextPath := c.Query("next_path")
		session, err := handler.sessions.Load(c.Request.Context(), c.Request)
		if err != nil {
			handler.internalError(c, err)
			return
		}
		session.Data["host"] = baseURL
		if nextPath != "" && (!space || providerName == "gitea") {
			if providerName == "gitea" {
				session.Data["next_path"] = validateNextPath(nextPath)
			} else {
				session.Data["next_path"] = nextPath
			}
		}
		if err := handler.sessions.Save(c.Request.Context(), session); err != nil {
			handler.internalError(c, err)
			return
		}
		handler.sessions.SetCookie(c.Writer, session.Key)
		if !handler.requireConfigured(c, baseURL, nextPath, true) {
			return
		}
		state, err := randomHex(16)
		if err != nil {
			handler.internalError(c, err)
			return
		}
		provider, authenticationError := newOAuthProvider(
			providerName,
			c.Request,
			state,
			"",
			func(key, fallback string) string {
				return handler.configurationValue(c.Request.Context(), key, fallback)
			},
			handler.oauthHTTP,
		)
		if authenticationError != nil {
			handler.redirectError(c, baseURL, nextPath, authenticationError)
			return
		}
		session.Data["state"] = state
		if err := handler.sessions.Save(c.Request.Context(), session); err != nil {
			handler.internalError(c, err)
			return
		}
		handler.sessions.SetCookie(c.Writer, session.Key)
		c.Redirect(http.StatusFound, provider.authURL)
	}
}

func (handler *Handler) oauthCallback(providerName string, space bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		baseURL := handler.baseURL(space)
		session, err := handler.sessions.Load(c.Request.Context(), c.Request)
		if err != nil {
			handler.internalError(c, err)
			return
		}
		if sessionHost, ok := session.Data["host"].(string); ok && sessionHost != "" {
			baseURL = sessionHost
		}
		nextPath, _ := session.Data["next_path"].(string)
		expectedState, _ := session.Data["state"].(string)
		if c.Query("state") != expectedState || c.Query("code") == "" {
			handler.redirectError(c, baseURL, nextPath, oauthProviderError(providerName))
			return
		}
		provider, authenticationError := newOAuthProvider(
			providerName,
			c.Request,
			"",
			c.Query("code"),
			func(key, fallback string) string {
				return handler.configurationValue(c.Request.Context(), key, fallback)
			},
			handler.oauthHTTP,
		)
		if authenticationError != nil {
			handler.redirectError(c, baseURL, nextPath, authenticationError)
			return
		}
		identity, tokens, authenticationError := provider.Authenticate(c.Request.Context())
		if authenticationError != nil {
			handler.redirectError(c, baseURL, nextPath, authenticationError)
			return
		}
		if !handler.validEmail(identity.Email) {
			handler.redirectError(c, baseURL, nextPath, authError(ErrorInvalidEmail, "INVALID_EMAIL", map[string]any{"email": identity.Email}))
			return
		}
		user, err := handler.repository.FindUserByEmail(c.Request.Context(), identity.Email)
		if errors.Is(err, ErrNotFound) {
			if !handler.signupEnabled(c.Request.Context(), identity.Email) {
				handler.redirectError(c, baseURL, nextPath, authError(ErrorSignupDisabled, "SIGNUP_DISABLED", map[string]any{"email": identity.Email}))
				return
			}
			temporaryPassword, randomErr := randomHex(16)
			if randomErr != nil {
				handler.internalError(c, randomErr)
				return
			}
			encodedPassword, hashErr := HashPassword(temporaryPassword)
			if hashErr != nil {
				handler.internalError(c, hashErr)
				return
			}
			user, err = handler.repository.CreateOAuthUser(c.Request.Context(), identity, encodedPassword)
		} else if err == nil {
			if authenticationError = interactiveUserError(user); authenticationError != nil {
				handler.redirectError(c, baseURL, nextPath, authenticationError)
				return
			}
			if handler.configurationValue(c.Request.Context(), "ENABLE_"+strings.ToUpper(providerName)+"_SYNC", "0") == "1" {
				if syncErr := handler.repository.SyncOAuthUser(c.Request.Context(), user.ID, identity, handler.clock().UTC()); syncErr != nil {
					handler.internalError(c, syncErr)
					return
				}
			}
		}
		if err != nil {
			handler.internalError(c, err)
			return
		}
		if err := handler.recordAuthentication(c, user, providerName, baseURL); err != nil {
			handler.internalError(c, err)
			return
		}
		if !space {
			if err := handler.repository.ProcessAcceptedInvitations(c.Request.Context(), user, handler.clock().UTC()); err != nil {
				handler.internalError(c, err)
				return
			}
		}
		if err := handler.repository.UpsertOAuthAccount(c.Request.Context(), user.ID, identity, tokens, handler.clock().UTC()); err != nil {
			// Django deliberately treats an account-link persistence collision as
			// non-fatal after logging it; authentication still succeeds.
			log.Printf("persist OAuth account: %v", err)
		}
		if err := handler.completeLoginSession(c, user, baseURL); err != nil {
			handler.internalError(c, err)
			return
		}
		if space {
			c.Redirect(http.StatusFound, spaceSuccessURL(handler.baseURL(true), nextPath))
		} else {
			c.Redirect(http.StatusFound, safeRedirectURL(baseURL, nextPath, nil))
		}
	}
}

func oauthProviderError(provider string) *Error {
	switch provider {
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

func (handler *Handler) recordAuthentication(c *gin.Context, user *User, medium, baseURL string) error {
	at := handler.clock().UTC()
	wasInactive := !user.IsActive
	if err := handler.repository.RecordAuthentication(c.Request.Context(), user, medium, clientIP(c.Request), c.Request.UserAgent(), at); err != nil {
		return err
	}
	if wasInactive && handler.tasks != nil {
		if err := handler.tasks.PublishUserActivation(c.Request.Context(), baseURL, user.ID); err != nil {
			log.Printf("publish user activation task: %v", err)
		}
	}
	return nil
}

func (handler *Handler) completeLoginSession(c *gin.Context, user *User, baseURL string) error {
	at := handler.clock().UTC()
	if err := handler.repository.RecordSessionLogin(c.Request.Context(), user.ID, at); err != nil {
		return err
	}
	user.LastLogin = &at
	session, err := handler.sessions.Login(c.Request.Context(), c.Request, user, baseURL)
	if err != nil {
		return err
	}
	handler.sessions.SetCookie(c.Writer, session.Key)
	return handler.csrf.Rotate(c.Writer)
}

func interactiveUserError(user *User) *Error {
	if !user.IsActive && user.LastLogoutTime != nil {
		return authError(ErrorUserAccountDeactivated, "USER_ACCOUNT_DEACTIVATED", map[string]any{"email": user.Email})
	}
	if user.IsBot {
		return authError(ErrorBotUserLoginForbidden, "BOT_USER_LOGIN_FORBIDDEN", map[string]any{"email": user.Email})
	}
	return nil
}

func (handler *Handler) requireConfigured(c *gin.Context, baseURL, nextPath string, redirect bool) bool {
	configured, err := handler.repository.InstanceConfigured(c.Request.Context())
	if err != nil {
		handler.internalError(c, err)
		return false
	}
	if configured {
		return true
	}
	authenticationError := authError(ErrorInstanceNotConfigured, "INSTANCE_NOT_CONFIGURED", nil)
	if redirect {
		handler.redirectError(c, baseURL, nextPath, authenticationError)
	} else {
		c.JSON(http.StatusBadRequest, authenticationError.Response())
	}
	return false
}

// baseURL is base_host: is_space=True when space is set, is_app=True otherwise, which is how all but two of the hundred and sixteen call sites in the authentication app used it.
func (handler *Handler) baseURL(space bool) string {
	// base_origin, which both branches fall back to.
	origin := handler.settings.WebURL
	if origin == "" {
		origin = handler.settings.AppBaseURL
	}
	if space {
		// SPACE_BASE_URL only when it is set. Falling through to the bare path, which is what this did before, produced a Location header of "/spaces/" with no origin behind it.
		if handler.settings.SpaceBaseURL != "" {
			return strings.TrimRight(handler.settings.SpaceBaseURL, "/") + handler.settings.SpaceBasePath
		}
		return origin + handler.settings.SpaceBasePath
	}
	if handler.settings.AppBaseURL != "" {
		return handler.settings.AppBaseURL
	}
	return origin
}

func (handler *Handler) validEmail(email string) bool {
	return len(email) <= 320 && handler.validator.Var(email, "required,email") == nil
}

func (handler *Handler) configurationValue(ctx context.Context, key, fallback string) string {
	if handler.settings.Environment != nil {
		if value, exists := handler.settings.Environment[key]; exists {
			fallback = value
		}
	}
	value, err := handler.repository.ConfigurationValue(ctx, key, fallback)
	if err != nil {
		log.Printf("authentication configuration %s: %v", key, err)
		return fallback
	}
	return value
}

func (handler *Handler) emailPasswordEnabled(ctx context.Context) bool {
	return handler.configurationValue(ctx, "ENABLE_EMAIL_PASSWORD", "") != "0"
}

func (handler *Handler) signupEnabled(ctx context.Context, email string) bool {
	if handler.configurationValue(ctx, "ENABLE_SIGNUP", "1") != "0" {
		return true
	}
	allowed, err := handler.repository.SignupAllowed(ctx, email)
	return err == nil && allowed
}

func (handler *Handler) magicConfigurationError(ctx context.Context, email string) *Error {
	if handler.configurationValue(ctx, "EMAIL_HOST", "") == "" {
		return authError(ErrorSMTPNotConfigured, "SMTP_NOT_CONFIGURED", map[string]any{"email": email})
	}
	if handler.configurationValue(ctx, "ENABLE_MAGIC_LINK_LOGIN", "1") == "0" {
		return authError(ErrorMagicLinkLoginDisabled, "MAGIC_LINK_LOGIN_DISABLED", map[string]any{"email": email})
	}
	return nil
}

func (handler *Handler) redirectError(c *gin.Context, baseURL, nextPath string, authenticationError *Error) {
	c.Redirect(http.StatusFound, safeRedirectURL(baseURL, nextPath, authenticationError))
}

func (handler *Handler) allow(c *gin.Context, redirect bool, baseURL, nextPath string) bool {
	allowed, err := handler.limiter.Allow(c.Request.Context(), clientIP(c.Request))
	if err != nil {
		handler.internalError(c, err)
		return false
	}
	if allowed {
		return true
	}
	authenticationError := authError(ErrorRateLimitExceeded, "RATE_LIMIT_EXCEEDED", nil)
	if redirect {
		handler.redirectError(c, baseURL, nextPath, authenticationError)
	} else {
		c.JSON(http.StatusTooManyRequests, authenticationError.Response())
	}
	return false
}

func (handler *Handler) internalError(c *gin.Context, err error) {
	log.Printf("authentication request failed: %v", err)
	c.JSON(http.StatusInternalServerError, authError(ErrorAuthenticationFailed, "AUTHENTICATION_FAILED", nil).Response())
}

func toString(value any) string {
	if stringValue, ok := value.(string); ok {
		return stringValue
	}
	return fmt.Sprint(value)
}
