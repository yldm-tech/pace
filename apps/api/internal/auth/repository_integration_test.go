package auth

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// TestGORMRepositoryAgainstDjangoSchema is opt-in because it writes to the
// configured database. Every write runs inside a transaction that is rolled
// back, and the test never creates or migrates schema.
func TestGORMRepositoryAgainstDjangoSchema(t *testing.T) {
	databaseURL := os.Getenv("AUTH_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set AUTH_TEST_DATABASE_URL to a disposable database with the Django schema")
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

	suffix, err := randomHex(8)
	if err != nil {
		t.Fatal(err)
	}
	repository := NewGORMRepository(transaction, false, "pace-integration-secret")
	user, err := repository.CreateUser(ctx, fmt.Sprintf("go-auth-%s@pace.invalid", suffix), "!", true, true)
	if err != nil {
		t.Fatalf("create user through Django schema: %v", err)
	}

	var profile struct {
		Theme          string  `gorm:"column:theme"`
		BillingAddress *string `gorm:"column:billing_address"`
	}
	if err := transaction.Table("profiles").
		Select("theme::text AS theme, billing_address::text AS billing_address").
		Where("user_id = ?", user.ID).Take(&profile).Error; err != nil {
		t.Fatalf("read created profile: %v", err)
	}
	if profile.Theme != "{}" || profile.BillingAddress != nil {
		t.Fatalf("profile JSON values = theme %q, billing_address %#v", profile.Theme, profile.BillingAddress)
	}

	var preferenceCount int64
	if err := transaction.Table("user_notification_preferences").
		Where("user_id = ? AND workspace_id IS NULL AND project_id IS NULL", user.ID).
		Count(&preferenceCount).Error; err != nil {
		t.Fatalf("read notification preference: %v", err)
	}
	if preferenceCount != 1 {
		t.Fatalf("notification preference count = %d", preferenceCount)
	}

	sessionRepository := NewGORMSessionRepository(transaction)
	userID := user.ID
	record := &SessionRecord{
		SessionKey:  "go-auth-integration-" + suffix,
		SessionData: "e30:integration-signature",
		ExpireDate:  time.Now().UTC().Add(time.Hour),
		DeviceInfo:  map[string]any{"domain": "http://app.pace.test"},
		UserID:      &userID,
	}
	if err := sessionRepository.Save(ctx, record); err != nil {
		t.Fatalf("save Django session row: %v", err)
	}
	loaded, err := sessionRepository.Load(ctx, record.SessionKey, time.Now().UTC())
	if err != nil {
		t.Fatalf("load Django session row: %v", err)
	}
	if loaded.UserID == nil || *loaded.UserID != user.ID || loaded.DeviceInfo["domain"] != "http://app.pace.test" {
		t.Fatalf("loaded session = %#v", loaded)
	}
}
