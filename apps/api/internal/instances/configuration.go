package instances

import (
	"context"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api/internal/auth"
	"github.com/yldm-tech/pace/apps/api/internal/drf"
	"gorm.io/gorm"
)

// InstanceConfiguration is one setting an operator can change without restarting anything.
type InstanceConfiguration struct {
	ID          string     `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt   time.Time  `gorm:"column:created_at"`
	UpdatedAt   time.Time  `gorm:"column:updated_at"`
	CreatedByID *string    `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID *string    `gorm:"column:updated_by_id;type:uuid"`
	DeletedAt   *time.Time `gorm:"column:deleted_at"`
	Key         string     `gorm:"column:key"`
	Value       *string    `gorm:"column:value"`
	Category    string     `gorm:"column:category"`
	IsEncrypted bool       `gorm:"column:is_encrypted"`
}

func (InstanceConfiguration) TableName() string { return "instance_configurations" }

// configurationList hands the console every setting, with the encrypted ones decrypted.
//
// They really are decrypted on the way out — a client secret is sent in full to whoever is signed in as an administrator, which is what lets the console show it in a field. Reproduced.
func (handler *Handler) configurationList(c *gin.Context, _ *auth.User, _ *Instance) {
	var rows []InstanceConfiguration
	// The model orders newest first.
	err := handler.db.WithContext(c.Request.Context()).Table("instance_configurations").
		Where("deleted_at IS NULL").Order("created_at DESC").Scan(&rows).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	results := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		results = append(results, handler.configurationJSON(row))
	}
	handler.respond(c, http.StatusOK, results)
}

// configurationJSON is InstanceConfigurationSerializer.
func (handler *Handler) configurationJSON(row InstanceConfiguration) gin.H {
	value := any(row.Value)
	if row.IsEncrypted && row.Value != nil {
		// decrypt_data answers an empty string for anything it cannot read, so a value written under another secret reads as empty rather than failing the request.
		decrypted, err := auth.DecryptConfiguration(*row.Value, handler.settings.SecretKey)
		if err != nil {
			decrypted = ""
		}
		value = decrypted
	}
	return gin.H{
		"id": row.ID, "created_at": drf.Time(row.CreatedAt), "updated_at": drf.Time(row.UpdatedAt),
		"deleted_at": drf.At(row.DeletedAt),
		"key":        row.Key, "value": value, "category": row.Category, "is_encrypted": row.IsEncrypted,
		"created_by": row.CreatedByID, "updated_by": row.UpdatedByID,
	}
}

// configurationUpdate writes the settings the body names, and only those.
//
// A key the table does not have is ignored rather than created, so the body can name anything. A value is trimmed and turned into text first, so a number arrives as its digits and a null arrives as an empty string — which is how a setting is cleared.
func (handler *Handler) configurationUpdate(c *gin.Context, _ *auth.User, _ *Instance) {
	payload := map[string]any{}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "JSON parse error"})
		return
	}
	keys := make([]string, 0, len(payload))
	for key := range payload {
		keys = append(keys, key)
	}
	var rows []InstanceConfiguration
	if len(keys) > 0 {
		err := handler.db.WithContext(c.Request.Context()).Table("instance_configurations").
			Where("key IN ? AND deleted_at IS NULL", keys).Order("created_at DESC").Scan(&rows).Error
		if err != nil {
			handler.internalError(c, err)
			return
		}
	}

	results := make([]gin.H, 0, len(rows))
	for index, row := range rows {
		value := configurationText(payload[row.Key], row.Value)
		stored := value
		if row.IsEncrypted {
			encrypted, err := auth.EncryptConfiguration(value, handler.settings.SecretKey)
			if err != nil {
				handler.internalError(c, err)
				return
			}
			stored = encrypted
		}
		// bulk_update writes only the value column, so updated_at is left where it was.
		err := handler.db.WithContext(c.Request.Context()).Table("instance_configurations").
			Where("id = ?", row.ID).Update("value", stored).Error
		if err != nil {
			handler.internalError(c, err)
			return
		}
		// The response is built from the rows in memory, which now carry the plain value rather than what was stored — so an encrypted setting reads back decrypted without being read again.
		rows[index].Value = &stored
		results = append(results, handler.configurationJSON(rows[index]))
	}
	handler.respond(c, http.StatusOK, results)
}

// configurationText is what a value becomes on its way into the column: null becomes an empty string, and anything else is rendered and trimmed.
func configurationText(value any, current *string) string {
	if value == nil {
		if current == nil {
			return ""
		}
		// A key present in the body with a null value is read as the row's own value first and then turned into an empty string, because the fallback is only reached for a key that is absent — and a key that is absent is not in the query at all.
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case bool:
		if typed {
			return "True"
		}
		return "False"
	case float64:
		// A whole number in a json body is an int to python and a float to Go, so an integral one is written back without a decimal point — 587 rather than 587.0, which is what a port would otherwise store in EMAIL_PORT.
		if typed == math.Trunc(typed) && math.Abs(typed) < 1e15 {
			return strconv.FormatInt(int64(typed), 10)
		}
		return strings.TrimSpace(drf.FormatFloat(typed))
	}
	return ""
}

// emailConfigurationKeys are the six the disable route clears.
var emailConfigurationKeys = []string{"EMAIL_HOST", "EMAIL_HOST_USER", "EMAIL_HOST_PASSWORD", "ENABLE_SMTP", "EMAIL_PORT", "EMAIL_FROM"}

// disableEmailFeature turns the mail settings off in one statement: the switch goes to zero and the other five are emptied.
//
// The emptied ones are written as plain empty strings even where the row is marked encrypted, because the update is SQL rather than a save — so the stored password becomes the literal empty string rather than an encrypted one. Nothing reads it afterwards, since the switch is off.
func (handler *Handler) disableEmailFeature(c *gin.Context, _ *auth.User, _ *Instance) {
	// One statement with a CASE, which is what the Django update renders.
	err := handler.db.WithContext(c.Request.Context()).Table("instance_configurations").
		Where("key IN ? AND deleted_at IS NULL", emailConfigurationKeys).
		Update("value", gorm.Expr("CASE WHEN key = 'ENABLE_SMTP' THEN '0' ELSE '' END")).Error
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to disable email configuration"})
		return
	}
	c.Status(http.StatusOK)
}

// configurationValues is get_configuration_value.
//
// Two things about it are easy to get wrong. When SKIP_ENV_VAR is set — which it is by default — the row's value is used *even when it is an empty string*, so a setting somebody cleared reads as cleared rather than falling back. And when it is not set the rows are ignored entirely and every value comes straight from the environment, so an installation configured through the console but running without that flag shows none of it.
func (handler *Handler) configurationValues(ctx context.Context, wanted []struct{ Key, Fallback string }) map[string]string {
	values := map[string]string{}
	if !handler.settings.SkipEnvironmentConfig {
		for _, entry := range wanted {
			value := entry.Fallback
			if fromEnvironment, present := handler.settings.Environment[entry.Key]; present {
				value = fromEnvironment
			}
			values[entry.Key] = value
		}
		return values
	}

	keys := make([]string, 0, len(wanted))
	for _, entry := range wanted {
		keys = append(keys, entry.Key)
	}
	var rows []InstanceConfiguration
	// One query for all of them, which is what the Django helper does too.
	_ = handler.db.WithContext(ctx).Table("instance_configurations").
		Where("key IN ? AND deleted_at IS NULL", keys).Order("created_at DESC").Scan(&rows).Error

	stored := map[string]InstanceConfiguration{}
	for _, row := range rows {
		if _, seen := stored[row.Key]; !seen {
			stored[row.Key] = row
		}
	}

	for _, entry := range wanted {
		row, known := stored[entry.Key]
		if !known {
			// Only a key with no row at all falls back, and the fallback is the environment when it names one.
			value := entry.Fallback
			if fromEnvironment, present := handler.settings.Environment[entry.Key]; present {
				value = fromEnvironment
			}
			values[entry.Key] = value
			continue
		}
		if row.Value == nil {
			values[entry.Key] = ""
			continue
		}
		if row.IsEncrypted {
			// decrypt_data answers an empty string for anything it cannot read.
			decrypted, err := auth.DecryptConfiguration(*row.Value, handler.settings.SecretKey)
			if err != nil {
				decrypted = ""
			}
			values[entry.Key] = decrypted
			continue
		}
		values[entry.Key] = *row.Value
	}
	return values
}
