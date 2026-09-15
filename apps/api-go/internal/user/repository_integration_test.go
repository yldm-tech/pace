package user

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// TestUserModelsAgainstDjangoSchema is opt-in because it writes to the
// configured database. Every write is rolled back, and Go never creates or
// migrates tables.
func TestUserModelsAgainstDjangoSchema(t *testing.T) {
	databaseURL := os.Getenv("USER_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set USER_TEST_DATABASE_URL to a disposable database with the Django schema")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	database, err := gorm.Open(postgres.Open(databaseURL), &gorm.Config{})
	if err != nil {
		t.Fatalf("open integration database: %v", err)
	}
	sqlDatabase, err := database.DB()
	if err != nil {
		t.Fatalf("access integration database: %v", err)
	}
	t.Cleanup(func() { _ = sqlDatabase.Close() })
	if err := sqlDatabase.PingContext(ctx); err != nil {
		t.Fatalf("ping integration database: %v", err)
	}

	transaction := database.WithContext(ctx).Begin()
	if transaction.Error != nil {
		t.Fatalf("begin integration transaction: %v", transaction.Error)
	}
	t.Cleanup(func() { _ = transaction.Rollback().Error })

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	users := auth.NewGORMRepository(transaction, false, "pace-user-integration-secret")
	createdUser, err := users.CreateUser(ctx, "go-user-"+suffix+"@pace.invalid", "!", true, true)
	if err != nil {
		t.Fatalf("create integration user: %v", err)
	}

	var profile Profile
	if err := transaction.Where("user_id = ?", createdUser.ID).Take(&profile).Error; err != nil {
		t.Fatalf("read profile through Django schema: %v", err)
	}
	if string(profile.Theme) != "{}" || !profile.IsAppRailDocked || profile.BillingAddressCountry != "INDIA" || profile.NotificationViewMode != "full" || profile.Language != "en" {
		t.Fatalf("profile defaults = %#v", profile)
	}
	if err := transaction.Model(&Profile{}).Where("user_id = ?", createdUser.ID).Updates(map[string]any{
		"theme": auth.JSONValue([]byte(`{"palette":"dark"}`)), "company_name": "Pace", "updated_at": time.Now().UTC(),
	}).Error; err != nil {
		t.Fatalf("update profile through Django schema: %v", err)
	}
	if err := transaction.Where("user_id = ?", createdUser.ID).Take(&profile).Error; err != nil {
		t.Fatalf("reload profile through Django schema: %v", err)
	}
	if profile.CompanyName != "Pace" || string(profile.Theme) != `{"palette": "dark"}` && string(profile.Theme) != `{"palette":"dark"}` {
		t.Fatalf("updated profile = %#v", profile)
	}

	accountID, err := integrationUUID()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	account := Account{
		ID: accountID, UserID: createdUser.ID, ProviderAccountID: "go-account-" + suffix,
		Provider: "github", AccessToken: "integration-token", LastConnectedAt: now,
		IDToken: "", Metadata: auth.JSONValue([]byte(`{"source":"integration"}`)),
		CreatedAt: now, UpdatedAt: now,
	}
	if err := transaction.Create(&account).Error; err != nil {
		t.Fatalf("create account through Django schema: %v", err)
	}
	var loaded Account
	if err := transaction.Where("id = ? AND user_id = ?", account.ID, createdUser.ID).Take(&loaded).Error; err != nil {
		t.Fatalf("read account through Django schema: %v", err)
	}
	if loaded.Provider != "github" || loaded.ProviderAccountID != account.ProviderAccountID || decodeJSON(loaded.Metadata).(map[string]any)["source"] != "integration" {
		t.Fatalf("loaded account = %#v", loaded)
	}
	if err := transaction.Where("id = ? AND user_id = ?", account.ID, createdUser.ID).Delete(&Account{}).Error; err != nil {
		t.Fatalf("delete account through Django schema: %v", err)
	}
	var accountCount int64
	if err := transaction.Model(&Account{}).Where("id = ?", account.ID).Count(&accountCount).Error; err != nil {
		t.Fatalf("count deleted account: %v", err)
	}
	if accountCount != 0 {
		t.Fatalf("deleted account count = %d", accountCount)
	}
}

func integrationUUID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", value[0:4], value[4:6], value[6:8], value[8:10], value[10:16]), nil
}
