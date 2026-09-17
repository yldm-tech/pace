package instances

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api/internal/auth"
	"github.com/yldm-tech/pace/apps/api/internal/drf"
)

// instanceRead is the one route here anybody may call, because the sign-in screen has to know what this installation offers before anybody has signed in.
//
// An installation with no registration answers two flags and nothing else, which is how the console knows to show the setup screen.
func (handler *Handler) instanceRead(c *gin.Context) {
	instance, err := handler.instance(c.Request.Context())
	if err != nil {
		handler.respond(c, http.StatusOK, gin.H{"is_activated": false, "is_setup_done": false})
		return
	}

	values := handler.configurationValues(c.Request.Context(), instanceConfigurationKeys)
	var workspaces int64
	if err := handler.db.WithContext(c.Request.Context()).Table("workspaces").Where("deleted_at IS NULL").Count(&workspaces).Error; err != nil {
		handler.internalError(c, err)
		return
	}

	config := gin.H{
		"enable_signup":                  values["ENABLE_SIGNUP"] == "1",
		"is_workspace_creation_disabled": values["DISABLE_WORKSPACE_CREATION"] == "1",
		"is_google_enabled":              values["IS_GOOGLE_ENABLED"] == "1",
		"is_github_enabled":              values["IS_GITHUB_ENABLED"] == "1",
		"is_gitlab_enabled":              values["IS_GITLAB_ENABLED"] == "1",
		"is_gitea_enabled":               values["IS_GITEA_ENABLED"] == "1",
		"is_magic_login_enabled":         values["ENABLE_MAGIC_LINK_LOGIN"] == "1",
		"is_email_password_enabled":      values["ENABLE_EMAIL_PASSWORD"] == "1",
		"github_app_name":                values["GITHUB_APP_NAME"],
		"slack_client_id":                values["SLACK_CLIENT_ID"],
		"has_unsplash_configured":        values["UNSPLASH_ACCESS_KEY"] != "",
		"has_llm_configured":             values["LLM_API_KEY"] != "",
		"file_size_limit":                drf.Float(handler.settings.FileSizeLimit),
		"is_smtp_configured":             values["EMAIL_HOST"] != "",
		"admin_base_url":                 nilWhenEmpty(handler.settings.AdminBaseURL),
		"space_base_url":                 nilWhenEmpty(handler.settings.SpaceBaseURL),
		"app_base_url":                   nilWhenEmpty(handler.settings.AppBaseURL),
		"instance_changelog_url":         nilWhenEmpty(handler.settings.InstanceChangelogURL),
		"is_self_managed":                handler.settings.IsSelfManaged,
	}
	// The serialized instance carries one extra key the model has no field for, which the console reads to decide whether to offer the first workspace.
	payload := instanceJSON(instance)
	payload["workspaces_exist"] = workspaces >= 1

	handler.respond(c, http.StatusOK, gin.H{"config": config, "instance": payload})
}

// instanceConfigurationKeys are the thirteen the config response is built from, each falling back to an environment variable when no row has been written.
//
// Three of those fallbacks disagree with what configure_instance seeds, which is visible on an installation that has never been configured: signup falls back to off here and is seeded on, and the magic link and the email password fall back to on here and are seeded off and on respectively.
var instanceConfigurationKeys = []struct{ Key, Fallback string }{
	{"ENABLE_SIGNUP", "0"},
	{"DISABLE_WORKSPACE_CREATION", "0"},
	{"IS_GOOGLE_ENABLED", "0"},
	{"IS_GITHUB_ENABLED", "0"},
	{"GITHUB_APP_NAME", ""},
	{"IS_GITLAB_ENABLED", "0"},
	{"IS_GITEA_ENABLED", "0"},
	{"EMAIL_HOST", ""},
	{"ENABLE_MAGIC_LINK_LOGIN", "1"},
	{"ENABLE_EMAIL_PASSWORD", "1"},
	{"SLACK_CLIENT_ID", ""},
	{"UNSPLASH_ACCESS_KEY", ""},
	{"LLM_API_KEY", ""},
}

// nilWhenEmpty keeps an unset base url as null rather than as an empty string, which is what an unset Django setting renders as.
func nilWhenEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}

// instanceJSON is InstanceSerializer.
//
// It declares primary_owner_details and the model has no primary_owner at all, so the field's attribute is missing and DRF drops it rather than rendering it as null — the same rule that shortens the other serializers on instances. Twenty-two keys, not twenty-three.
func instanceJSON(instance *Instance) gin.H {
	return gin.H{
		"id": instance.ID, "created_at": drf.Time(instance.CreatedAt), "updated_at": drf.Time(instance.UpdatedAt),
		"deleted_at":    drf.At(instance.DeletedAt),
		"instance_name": instance.InstanceName, "whitelist_emails": instance.WhitelistEmails,
		"instance_id": instance.InstanceID, "current_version": instance.CurrentVersion,
		"latest_version": instance.LatestVersion, "edition": instance.Edition, "domain": instance.Domain,
		"last_checked_at": drf.Time(instance.LastCheckedAt), "namespace": instance.Namespace,
		"is_telemetry_enabled": instance.IsTelemetryEnabled, "is_support_required": instance.IsSupportRequired,
		"is_setup_done": instance.IsSetupDone, "is_signup_screen_visited": instance.IsSignupScreenVisited,
		"is_verified": instance.IsVerified, "is_test": instance.IsTest,
		"is_current_version_deprecated": instance.IsCurrentVersionDeprecated,
		"created_by":                    instance.CreatedByID, "updated_by": instance.UpdatedByID,
	}
}

// instanceWritableFields are the columns a PATCH may set. The serializer's read_only_fields names id, email, last_checked_at and is_setup_done — and `email` is not a field the model has, so naming it changes nothing.
var instanceWritableFields = map[string]string{
	"instance_name": "instance_name", "whitelist_emails": "whitelist_emails",
	"instance_id": "instance_id", "current_version": "current_version",
	"latest_version": "latest_version", "edition": "edition", "domain": "domain",
	"namespace": "namespace", "is_telemetry_enabled": "is_telemetry_enabled",
	"is_support_required": "is_support_required", "is_signup_screen_visited": "is_signup_screen_visited",
	"is_verified": "is_verified", "is_test": "is_test",
	"is_current_version_deprecated": "is_current_version_deprecated",
}

// instanceUpdate edits the registration.
func (handler *Handler) instanceUpdate(c *gin.Context, _ *auth.User, instance *Instance) {
	payload := map[string]any{}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "JSON parse error"})
		return
	}
	columns := map[string]any{}
	for name, value := range payload {
		column, writable := instanceWritableFields[name]
		if !writable {
			// A read-only or unknown field is ignored rather than refused, which is what a partial ModelSerializer does with one.
			continue
		}
		columns[column] = value
	}
	if len(columns) > 0 {
		columns["updated_at"] = handler.clock().UTC()
		if err := handler.db.WithContext(c.Request.Context()).Table("instances").
			Where("id = ?", instance.ID).Updates(columns).Error; err != nil {
			handler.internalError(c, err)
			return
		}
	}
	updated, err := handler.instance(c.Request.Context())
	if err != nil {
		handler.internalError(c, err)
		return
	}
	handler.respond(c, http.StatusOK, instanceJSON(updated))
}

// signUpScreenVisited records that somebody has seen the first-run screen, so the console stops offering it.
//
// Anybody may call it, and it needs no session at all.
func (handler *Handler) signUpScreenVisited(c *gin.Context) {
	instance, err := handler.instance(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Instance is not configured"})
		return
	}
	// instance.save() writes every column, so updated_at moves.
	err = handler.db.WithContext(c.Request.Context()).Table("instances").Where("id = ?", instance.ID).
		Updates(map[string]any{"is_signup_screen_visited": true, "updated_at": handler.clock().UTC()}).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
