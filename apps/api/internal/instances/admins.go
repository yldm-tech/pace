package instances

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	zxcvbn "github.com/nbutton23/zxcvbn-go"
	"github.com/yldm-tech/pace/apps/api/internal/auth"
	"github.com/yldm-tech/pace/apps/api/internal/drf"
	"gorm.io/gorm"
)

// The error codes the admin console reads out of the query string when a sign-in or a sign-up is refused.
const (
	errorAdminAlreadyExists                = 5150
	errorRequiredAdminEmailPasswordAndName = 5155
	errorInvalidAdminEmail                 = 5160
	errorRequiredAdminEmailPassword        = 5170
	errorAdminAuthenticationFailed         = 5175
	errorAdminUserAlreadyExists            = 5180
	errorAdminUserDoesNotExist             = 5185
	errorAdminUserDeactivated              = 5190
	errorPasswordTooWeak                   = 5021
	errorInstanceNotConfigured             = 5000
)

// InstanceAdmin is one person who may configure this installation.
type InstanceAdmin struct {
	ID          string     `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt   time.Time  `gorm:"column:created_at"`
	UpdatedAt   time.Time  `gorm:"column:updated_at"`
	CreatedByID *string    `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID *string    `gorm:"column:updated_by_id;type:uuid"`
	DeletedAt   *time.Time `gorm:"column:deleted_at"`
	UserID      *string    `gorm:"column:user_id;type:uuid"`
	InstanceID  string     `gorm:"column:instance_id;type:uuid"`
	Role        int        `gorm:"column:role"`
	IsVerified  bool       `gorm:"column:is_verified"`
}

func (InstanceAdmin) TableName() string { return "instance_admins" }

// adminList is every administrator of this installation.
func (handler *Handler) adminList(c *gin.Context, _ *auth.User, instance *Instance) {
	var rows []InstanceAdmin
	err := handler.db.WithContext(c.Request.Context()).Table("instance_admins").
		Where("instance_id = ? AND deleted_at IS NULL", instance.ID).
		Order("created_at DESC").Scan(&rows).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	people, err := handler.adminUsers(c, rows)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	results := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		results = append(results, adminJSON(row, people))
	}
	handler.respond(c, http.StatusOK, results)
}

// adminCreate makes somebody who already has an account an administrator of this installation.
//
// The account has to exist: the lookup is unguarded, so an email nobody has raises and the base view turns it into a 404 rather than a message about the email.
func (handler *Handler) adminCreate(c *gin.Context, _ *auth.User, instance *Instance) {
	payload := map[string]any{}
	_ = c.ShouldBindJSON(&payload)
	email, _ := payload["email"].(string)
	if email == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Email is required"})
		return
	}
	role := 20
	if given, ok := payload["role"].(float64); ok {
		role = int(given)
	}

	var users []string
	if err := handler.db.WithContext(c.Request.Context()).Table("users").Where("email = ?", email).Limit(1).Pluck("id", &users).Error; err != nil {
		handler.internalError(c, err)
		return
	}
	if len(users) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "The required object does not exist."})
		return
	}

	now := handler.clock().UTC()
	row := InstanceAdmin{
		ID: uuid.NewString(), CreatedAt: now, UpdatedAt: now,
		UserID: &users[0], InstanceID: instance.ID, Role: role,
	}
	if err := handler.db.WithContext(c.Request.Context()).Table("instance_admins").Create(map[string]any{
		"id": row.ID, "created_at": now, "updated_at": now,
		"created_by_id": nil, "updated_by_id": nil,
		"user_id": users[0], "instance_id": instance.ID, "role": role, "is_verified": false,
	}).Error; err != nil {
		handler.internalError(c, err)
		return
	}
	people, err := handler.adminUsers(c, []InstanceAdmin{row})
	if err != nil {
		handler.internalError(c, err)
		return
	}
	handler.respond(c, http.StatusCreated, adminJSON(row, people))
}

// adminDelete takes somebody's administration of this installation away.
//
// It is a hard delete rather than a soft one, and it answers 204 whether or not there was anything to delete.
func (handler *Handler) adminDelete(c *gin.Context, _ *auth.User, instance *Instance) {
	err := handler.db.WithContext(c.Request.Context()).Exec(
		"DELETE FROM instance_admins WHERE instance_id = ? AND id = ?", instance.ID, c.Param("pk")).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// adminMe is the signed-in administrator's own account.
func (handler *Handler) adminMe(c *gin.Context, user *auth.User, _ *Instance) {
	handler.respond(c, http.StatusOK, adminMeJSON(user))
}

// adminSession answers whether whoever is calling is signed in as an administrator, and needs no session itself.
//
// The check here is not the one the other routes make: it asks only whether the caller is an administrator of *any* instance, with no role floor and without naming this one. So somebody left over from an earlier registration reads as signed in here and is refused everywhere else.
func (handler *Handler) adminSession(c *gin.Context) {
	if handler.sessions == nil {
		handler.respond(c, http.StatusOK, gin.H{"is_authenticated": false})
		return
	}
	user, _, err := handler.sessions.Authenticate(c.Request.Context(), c.Request, c.Writer)
	if err != nil || user == nil {
		handler.respond(c, http.StatusOK, gin.H{"is_authenticated": false})
		return
	}
	var count int64
	err = handler.db.WithContext(c.Request.Context()).Table("instance_admins").
		Where("user_id = ? AND deleted_at IS NULL", user.ID).Count(&count).Error
	if err != nil || count == 0 {
		handler.respond(c, http.StatusOK, gin.H{"is_authenticated": false})
		return
	}
	handler.respond(c, http.StatusOK, gin.H{"is_authenticated": true, "user": adminMeJSON(user)})
}

// adminUsers reads the people behind a page of administrator rows.
func (handler *Handler) adminUsers(c *gin.Context, rows []InstanceAdmin) (map[string]auth.User, error) {
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		if row.UserID != nil {
			ids = append(ids, *row.UserID)
		}
	}
	people := map[string]auth.User{}
	if len(ids) == 0 {
		return people, nil
	}
	var users []auth.User
	if err := handler.db.WithContext(c.Request.Context()).Where("id IN ?", ids).Find(&users).Error; err != nil {
		return nil, err
	}
	for _, person := range users {
		people[person.ID] = person
	}
	return people, nil
}

// adminJSON is InstanceAdminSerializer, whose user_detail is UserAdminLiteSerializer — the lite user with the email and the last login medium beside it.
func adminJSON(row InstanceAdmin, people map[string]auth.User) gin.H {
	var detail any
	if row.UserID != nil {
		if person, found := people[*row.UserID]; found {
			detail = adminLiteUserJSON(person)
		}
	}
	return gin.H{
		"id": row.ID, "user_detail": detail,
		"created_at": drf.Time(row.CreatedAt), "updated_at": drf.Time(row.UpdatedAt),
		"deleted_at": drf.At(row.DeletedAt),
		"role":       row.Role, "is_verified": row.IsVerified,
		"created_by": row.CreatedByID, "updated_by": row.UpdatedByID,
		"user": row.UserID, "instance": row.InstanceID,
	}
}

// adminLiteUserJSON is UserAdminLiteSerializer.
func adminLiteUserJSON(user auth.User) gin.H {
	return gin.H{
		"id": user.ID, "first_name": user.FirstName, "last_name": user.LastName,
		"avatar": user.Avatar, "avatar_url": avatarURL(user), "is_bot": user.IsBot,
		"display_name": user.DisplayName, "email": user.Email,
		"last_login_medium": user.LastLoginMedium,
	}
}

// adminMeJSON is InstanceAdminMeSerializer, which declares is_email_verified twice and so renders fifteen keys rather than sixteen.
func adminMeJSON(user *auth.User) gin.H {
	return gin.H{
		"id": user.ID, "avatar": user.Avatar, "avatar_url": avatarURL(*user),
		"cover_image": user.CoverImage, "date_joined": drf.Time(user.DateJoined),
		"display_name": user.DisplayName, "email": user.Email,
		"first_name": user.FirstName, "last_name": user.LastName,
		"is_active": user.IsActive, "is_bot": user.IsBot,
		"is_email_verified": user.IsEmailVerified, "user_timezone": user.UserTimezone,
		"username": user.Username, "is_password_autoset": user.IsPasswordAutoset,
	}
}

// avatarURL is the model property behind avatar_url: the stored asset when there is one, and the free-text field otherwise.
func avatarURL(user auth.User) any {
	if user.AvatarAssetID != nil {
		return "/api/assets/v2/static/" + *user.AvatarAssetID + "/"
	}
	if user.Avatar != "" {
		return user.Avatar
	}
	return nil
}

// adminSignIn is the console's own sign-in: a form post that answers with a redirect rather than json.
//
// Every refusal redirects back to the console with the error in the query string, which is how the console knows what to say. It never answers a status other than 302.
func (handler *Handler) adminSignIn(c *gin.Context) {
	base := handler.adminBaseURL()
	instance, err := handler.instance(c.Request.Context())
	if err != nil {
		handler.redirectError(c, base, errorInstanceNotConfigured, "INSTANCE_NOT_CONFIGURED", nil)
		return
	}

	email := c.PostForm("email")
	password := c.PostForm("password")
	if email == "" || password == "" {
		handler.redirectError(c, base, errorRequiredAdminEmailPassword, "REQUIRED_ADMIN_EMAIL_PASSWORD",
			map[string]string{"email": email})
		return
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if !validEmail(email) {
		handler.redirectError(c, base, errorInvalidAdminEmail, "INVALID_ADMIN_EMAIL",
			map[string]string{"email": email})
		return
	}

	var user auth.User
	err = handler.db.WithContext(c.Request.Context()).Where("email = ?", email).Take(&user).Error
	if err != nil {
		handler.redirectError(c, base, errorAdminUserDoesNotExist, "ADMIN_USER_DOES_NOT_EXIST",
			map[string]string{"email": email})
		return
	}
	// A bot is never registered as an administrator, so this is not reachable — it is the same guard the app flow carries, and it answers the generic failure rather than naming the reason.
	if user.IsBot {
		handler.redirectError(c, base, errorAdminAuthenticationFailed, "ADMIN_AUTHENTICATION_FAILED",
			map[string]string{"email": email})
		return
	}
	if !user.IsActive {
		// The one refusal that carries no email with it.
		handler.redirectError(c, base, errorAdminUserDeactivated, "ADMIN_USER_DEACTIVATED", nil)
		return
	}
	if !auth.VerifyPassword(password, user.Password) {
		handler.redirectError(c, base, errorAdminAuthenticationFailed, "ADMIN_AUTHENTICATION_FAILED",
			map[string]string{"email": email})
		return
	}
	var count int64
	err = handler.db.WithContext(c.Request.Context()).Table("instance_admins").
		Where("instance_id = ? AND user_id = ? AND deleted_at IS NULL", instance.ID, user.ID).Count(&count).Error
	if err != nil || count == 0 {
		handler.redirectError(c, base, errorAdminAuthenticationFailed, "ADMIN_AUTHENTICATION_FAILED",
			map[string]string{"email": email})
		return
	}

	if err := handler.recordLogin(c, &user); err != nil {
		handler.internalError(c, err)
		return
	}
	if err := handler.login(c, &user, base); err != nil {
		handler.internalError(c, err)
		return
	}
	c.Redirect(http.StatusFound, base+"general/")
}

// adminSignUp is the first-run screen: the first administrator, made at the same time as the account behind them.
//
// It can only ever run once. The guard is checked twice — once cheaply and once under a row lock on the instance — because two people submitting the form at the same moment could otherwise both become the first administrator.
func (handler *Handler) adminSignUp(c *gin.Context) {
	base := handler.adminBaseURL()
	instance, err := handler.instance(c.Request.Context())
	if err != nil {
		handler.redirectError(c, base, errorInstanceNotConfigured, "INSTANCE_NOT_CONFIGURED", nil)
		return
	}
	if taken, err := handler.setupAlreadyDone(c, instance); err != nil {
		handler.internalError(c, err)
		return
	} else if taken {
		handler.redirectError(c, base, errorAdminAlreadyExists, "ADMIN_ALREADY_EXIST", nil)
		return
	}

	email := c.PostForm("email")
	password := c.PostForm("password")
	firstName := c.PostForm("first_name")
	lastName := c.PostForm("last_name")
	companyName := c.PostForm("company_name")
	// Absent means true, because the view's default is the boolean rather than a string.
	telemetry := "True"
	if given, present := c.GetPostForm("is_telemetry_enabled"); present {
		telemetry = given
	}
	payload := map[string]string{
		"email": email, "first_name": firstName, "last_name": lastName,
		"company_name": companyName, "is_telemetry_enabled": telemetry,
	}

	if email == "" || password == "" || firstName == "" {
		handler.redirectError(c, base, errorRequiredAdminEmailPasswordAndName,
			"REQUIRED_ADMIN_EMAIL_PASSWORD_FIRST_NAME", payload)
		return
	}
	email = strings.ToLower(strings.TrimSpace(email))
	payload["email"] = email
	if !validEmail(email) {
		handler.redirectError(c, base, errorInvalidAdminEmail, "INVALID_ADMIN_EMAIL", payload)
		return
	}

	var existing int64
	if err := handler.db.WithContext(c.Request.Context()).Table("users").Where("email = ?", email).Count(&existing).Error; err != nil {
		handler.internalError(c, err)
		return
	}
	if existing > 0 {
		handler.redirectError(c, base, errorAdminUserAlreadyExists, "ADMIN_USER_ALREADY_EXIST", payload)
		return
	}
	if zxcvbn.PasswordStrength(password, nil).Score < 3 {
		handler.redirectError(c, base, errorPasswordTooWeak, "PASSWORD_TOO_WEAK", payload)
		return
	}

	hashed, err := auth.HashPassword(password)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	user := auth.User{}
	alreadyTaken := false
	err = handler.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		// The instance row is locked so two forms submitted together cannot both pass the guard.
		var locked Instance
		if err := tx.Raw("SELECT * FROM instances WHERE id = ? FOR UPDATE", instance.ID).Scan(&locked).Error; err != nil {
			return err
		}
		var admins int64
		if err := tx.Table("instance_admins").Where("deleted_at IS NULL").Count(&admins).Error; err != nil {
			return err
		}
		if locked.IsSetupDone || admins > 0 {
			alreadyTaken = true
			return nil
		}

		now := handler.clock().UTC()
		// The same three rows the ordinary sign-up writes, built by the same function. Writing them here by hand is what left god-mode unable to create its first admin at all: the insert named columns that had moved to the profile, named audit columns the profile does not have, and left out nineteen NOT NULL columns between the two tables.
		created, profile, preference, err := auth.NewUserRecords(email, hashed, firstName, lastName, "", false, false, now)
		if err != nil {
			return err
		}
		created.LastLoginIP = requestIP(c)
		created.LastLoginUserAgent = c.Request.UserAgent()
		created.TokenUpdatedAt = &now
		profile.CompanyName = companyName
		user = *created
		if err := tx.Create(created).Error; err != nil {
			return err
		}
		if err := tx.Create(profile).Error; err != nil {
			return err
		}
		if err := tx.Create(preference).Error; err != nil {
			return err
		}
		err = tx.Table("instance_admins").Create(map[string]any{
			"id": uuid.NewString(), "created_at": now, "updated_at": now,
			"created_by_id": nil, "updated_by_id": nil,
			"user_id": user.ID, "instance_id": instance.ID, "role": 20, "is_verified": false,
		}).Error
		if err != nil {
			return err
		}
		// The company name becomes the instance's name, and the telemetry answer is stored as whatever the form sent — so the string "false" is truthy and leaves telemetry on.
		return tx.Table("instances").Where("id = ?", instance.ID).Updates(map[string]any{
			"is_setup_done": true, "instance_name": companyName,
			"is_telemetry_enabled": telemetryEnabled(telemetry), "updated_at": now,
		}).Error
	})
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if alreadyTaken {
		handler.redirectError(c, base, errorAdminAlreadyExists, "ADMIN_ALREADY_EXIST", nil)
		return
	}

	if err := handler.login(c, &user, base); err != nil {
		handler.internalError(c, err)
		return
	}
	c.Redirect(http.StatusFound, base+"general/")
}

// telemetryEnabled is what the form's answer becomes in the column.
//
// The view stores the posted value straight into a boolean field, and any non-empty string is true — so "false" turns telemetry on. Reproduced: only an empty answer turns it off.
func telemetryEnabled(value string) bool {
	return value != ""
}

// adminSignOut ends the console session and sends the browser back to it.
//
// It redirects whatever happens, including when the session was never there.
func (handler *Handler) adminSignOut(c *gin.Context) {
	base := handler.adminBaseURL()
	if handler.sessions != nil {
		if user, _, err := handler.sessions.Authenticate(c.Request.Context(), c.Request, c.Writer); err == nil && user != nil {
			_ = handler.db.WithContext(c.Request.Context()).Table("users").Where("id = ?", user.ID).
				Updates(map[string]any{
					"last_logout_ip": requestIP(c), "last_logout_time": handler.clock().UTC(),
					"updated_at": handler.clock().UTC(),
				}).Error
		}
		_ = handler.sessions.Logout(c.Request.Context(), c.Request)
		handler.sessions.DeleteCookie(c.Writer)
	}
	c.Redirect(http.StatusFound, base)
}

// setupAlreadyDone is the guard that keeps the first-run screen to one use. It asks whether *any* administrator exists rather than any of this instance, so a stray second registration cannot be used to get past it.
func (handler *Handler) setupAlreadyDone(c *gin.Context, instance *Instance) (bool, error) {
	if instance.IsSetupDone {
		return true, nil
	}
	var admins int64
	err := handler.db.WithContext(c.Request.Context()).Table("instance_admins").
		Where("deleted_at IS NULL").Count(&admins).Error
	if err != nil {
		return false, err
	}
	return admins > 0, nil
}

// recordLogin stamps the account with when and where it was last signed in from, which the sign-in does before the session is made.
func (handler *Handler) recordLogin(c *gin.Context, user *auth.User) error {
	now := handler.clock().UTC()
	return handler.db.WithContext(c.Request.Context()).Table("users").Where("id = ?", user.ID).
		Updates(map[string]any{
			"is_active": true, "last_active": now, "last_login_time": now,
			"last_login_ip": requestIP(c), "last_login_uagent": c.Request.UserAgent(),
			"token_updated_at": now, "updated_at": now,
		}).Error
}

// login makes the session the console then carries.
func (handler *Handler) login(c *gin.Context, user *auth.User, base string) error {
	if handler.sessions == nil {
		return nil
	}
	session, err := handler.sessions.Login(c.Request.Context(), c.Request, user, base)
	if err != nil {
		return err
	}
	handler.sessions.SetCookie(c.Writer, session.Key)
	return nil
}

// redirectError sends the browser back to the console with the refusal in the query string.
func (handler *Handler) redirectError(c *gin.Context, base string, code int, message string, payload map[string]string) {
	values := url.Values{}
	values.Set("error_code", strconv.Itoa(code))
	values.Set("error_message", message)
	for key, value := range payload {
		values.Set(key, value)
	}
	c.Redirect(http.StatusFound, base+"?"+values.Encode())
}

// requestIP is get_client_ip: the first address a proxy named, and failing that whoever connected.
func requestIP(c *gin.Context) string {
	if forwarded := c.GetHeader("X-Forwarded-For"); forwarded != "" {
		first, _, _ := strings.Cut(forwarded, ",")
		return first
	}
	host, _, found := strings.Cut(c.Request.RemoteAddr, ":")
	if found {
		return host
	}
	return c.Request.RemoteAddr
}

// validEmail is django.core.validators.validate_email, reduced to what it really refuses here.
func validEmail(email string) bool {
	if email == "" || len(email) > 320 {
		return false
	}
	local, domain, found := strings.Cut(email, "@")
	if !found || local == "" || domain == "" {
		return false
	}
	if strings.Contains(domain, "@") || !strings.Contains(domain, ".") {
		return false
	}
	return !strings.ContainsAny(email, " \t\r\n")
}
