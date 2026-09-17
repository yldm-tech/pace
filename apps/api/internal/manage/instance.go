package manage

import (
	"bufio"
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/yldm-tech/pace/apps/api/internal/auth"
	"github.com/yldm-tech/pace/apps/api/internal/worker"
)

//go:embed instance_config.tsv
var instanceConfigTSV string

// SecretKey is the key an encrypted configuration value is written under. A binary that does not set it cannot write one.
var SecretKey string

// configVariable is one instance configuration row as the seeding knows it: not a value, but where the value comes from.
type configVariable struct {
	Key       string
	Env       string
	Default   string
	Category  string
	Encrypted bool
}

// instanceConfigVariables parses the embedded list, which CI keeps in step with the Django app.
func instanceConfigVariables() []configVariable {
	variables := make([]configVariable, 0, 40)
	scanner := bufio.NewScanner(strings.NewReader(instanceConfigTSV))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) != 5 {
			continue
		}
		variables = append(variables, configVariable{
			Key: parts[0], Env: parts[1], Default: parts[2],
			Category: parts[3], Encrypted: parts[4] == "1",
		})
	}
	return variables
}

// configureInstance is manage.py configure_instance: write one configuration row per variable, reading each value out of the environment.
//
// A row that already exists is left exactly as it is — the value is not refreshed from the environment — so changing an environment variable after the first run does nothing until the row is removed. Reproduced.
func configureInstance(ctx context.Context, env Environment, _ []string) error {
	db, err := database(env)
	if err != nil {
		return err
	}
	if SecretKey == "" {
		return commandError("SECRET_KEY env variable is required.")
	}

	now := time.Now().UTC()
	for _, variable := range instanceConfigVariables() {
		var existing int64
		err := db.WithContext(ctx).Table("instance_configurations").
			Where("key = ? AND deleted_at IS NULL", variable.Key).Count(&existing).Error
		if err != nil {
			return err
		}
		if existing > 0 {
			write(env, "%s configuration already exists", variable.Key)
			continue
		}
		value := variable.Default
		if fromEnvironment, present := os.LookupEnv(variable.Env); present {
			value = fromEnvironment
		}
		if variable.Encrypted {
			// An empty value is stored as an empty string rather than as a token, which is what encrypt_data's own guard leaves behind.
			value, err = auth.EncryptConfiguration(value, SecretKey)
			if err != nil {
				return err
			}
		}
		err = db.WithContext(ctx).Table("instance_configurations").Create(map[string]any{
			"id": uuid.NewString(), "created_at": now, "updated_at": now,
			"created_by_id": nil, "updated_by_id": nil,
			"key": variable.Key, "value": value,
			"category": variable.Category, "is_encrypted": variable.Encrypted,
		}).Error
		if err != nil {
			return err
		}
		write(env, "%s loaded with value from environment variable.", variable.Key)
	}
	return nil
}

// registerInstance is manage.py register_instance: record that this installation exists, or refresh what is already recorded, and then report its numbers.
func registerInstance(ctx context.Context, env Environment, arguments []string) error {
	db, err := database(env)
	if err != nil {
		return err
	}
	values := positional(arguments)
	signature := ""
	if len(values) > 0 {
		signature = values[0]
	}

	current := currentVersion(env)
	latest := latestVersion(ctx, env, current)

	var instances []string
	// Instance.Meta orders newest first, so .first() is the newest registration.
	err = db.WithContext(ctx).Table("instances").Where("deleted_at IS NULL").
		Order("created_at DESC").Limit(1).Pluck("id", &instances).Error
	if err != nil {
		return err
	}
	isTest := os.Getenv("IS_TEST") == "1"
	now := time.Now().UTC()

	if len(instances) == 0 {
		if signature == "" {
			return commandError("Machine signature is required")
		}
		identifier, err := instanceIdentifier()
		if err != nil {
			return err
		}
		err = db.WithContext(ctx).Table("instances").Create(
			newInstanceRow(identifier, current, latest, now, isTest)).Error
		if err != nil {
			return err
		}
		write(env, "Instance registered")
	} else {
		write(env, "Instance already registered")
		// save() writes every column, so the version and the edition are refreshed even when nothing about them changed.
		err = db.WithContext(ctx).Table("instances").Where("id = ?", instances[0]).Updates(map[string]any{
			"last_checked_at": now, "current_version": current, "latest_version": latest,
			"is_test": isTest, "edition": "PLANE_COMMUNITY", "updated_at": now,
		}).Error
		if err != nil {
			return err
		}
	}

	// The machine signature is taken as an argument and never written down, which is what upstream does with it too.
	_ = signature

	if Queue == nil || Queue() == nil {
		return nil
	}
	return Queue().PublishAfter(ctx, worker.PushInstanceMetricsTask, map[string]any{}, 0)
}

// newInstanceRow is the row register_instance writes for an installation that has never been recorded.
//
// It is a function of its own so that a test can check it against the schema. Django fills a column the command does not mention from the field's default, and for a text field that allows empty strings and has no explicit default that means the empty string — not null. domain is such a column, and omitting it here made the insert fail on its NOT NULL constraint the first time this command was ever run.
func newInstanceRow(identifier, current, latest string, now time.Time, isTest bool) map[string]any {
	return map[string]any{
		"id": uuid.NewString(), "created_at": now, "updated_at": now,
		"created_by_id": nil, "updated_by_id": nil,
		"instance_name": "Plane Community Edition", "instance_id": identifier,
		"domain":          "",
		"current_version": current, "latest_version": latest, "last_checked_at": now,
		"is_test": isTest, "edition": "PLANE_COMMUNITY",
		"is_telemetry_enabled": true, "is_support_required": true,
		"is_setup_done": false, "is_signup_screen_visited": false,
		"is_verified": false, "is_current_version_deprecated": false,
	}
}

// instanceIdentifier is secrets.token_hex(12): twenty-four hex characters.
func instanceIdentifier() (string, error) {
	value := make([]byte, 12)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

// currentVersion is APP_VERSION, and failing that the version in package.json beside the working directory.
func currentVersion(env Environment) string {
	if value := os.Getenv("APP_VERSION"); value != "" {
		return value
	}
	raw, err := os.ReadFile("package.json")
	if err != nil {
		write(env, "Error checking for current version")
		return "v0.1.0"
	}
	var manifest struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil || manifest.Version == "" {
		write(env, "Error checking for current version")
		return "v0.1.0"
	}
	return manifest.Version
}

// latestVersion asks GitHub what the newest release is, and falls back to the version already running when it cannot.
func latestVersion(ctx context.Context, env Environment, fallback string) string {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://api.github.com/repos/makeplane/plane/releases/latest", nil)
	if err != nil {
		write(env, "Error checking for latest version")
		return fallback
	}
	client := &http.Client{Timeout: 10 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		write(env, "Error checking for latest version")
		return fallback
	}
	defer response.Body.Close()
	if response.StatusCode >= 400 {
		write(env, "Error checking for latest version")
		return fallback
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		write(env, "Error checking for latest version")
		return fallback
	}
	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(payload, &release); err != nil || release.TagName == "" {
		write(env, "Error checking for latest version")
		return fallback
	}
	return release.TagName
}
