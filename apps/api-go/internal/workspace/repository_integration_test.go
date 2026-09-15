package workspace

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// TestWorkspaceModelsAgainstDjangoSchema is opt-in because it writes to the
// configured database. Every write is rolled back, and Go never creates or
// migrates tables.
func TestWorkspaceModelsAgainstDjangoSchema(t *testing.T) {
	databaseURL := os.Getenv("WORKSPACE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set WORKSPACE_TEST_DATABASE_URL to a disposable database with the Django schema")
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
	users := auth.NewGORMRepository(transaction, false, "pace-workspace-integration-secret")
	user, err := users.CreateUser(ctx, "go-workspace-"+suffix+"@pace.invalid", "!", true, true)
	if err != nil {
		t.Fatalf("create integration user: %v", err)
	}

	now := time.Now().UTC()
	workspaceID, err := newUUID()
	if err != nil {
		t.Fatal(err)
	}
	workspace := Workspace{
		ID: workspaceID, CreatedAt: now, UpdatedAt: now, CreatedByID: &user.ID,
		Name: "Go Workspace " + suffix, OwnerID: user.ID, Slug: "go-workspace-" + suffix,
		Timezone: "UTC", BackgroundColor: randomColor(),
	}
	if err := transaction.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace through Django schema: %v", err)
	}

	memberID, err := newUUID()
	if err != nil {
		t.Fatal(err)
	}
	member := WorkspaceMember{
		ID: memberID, CreatedAt: now, UpdatedAt: now, CreatedByID: &user.ID,
		WorkspaceID: workspace.ID, MemberID: user.ID, Role: roleAdmin, IsActive: true,
		ViewProps: defaultPropsJSON(), DefaultProps: defaultPropsJSON(), IssueProps: issuePropsJSON(),
		GettingStartedChecklist: emptyJSON(), Tips: emptyJSON(), ExploredFeatures: emptyJSON(),
	}
	if err := transaction.Create(&member).Error; err != nil {
		t.Fatalf("create workspace member through Django schema: %v", err)
	}

	handler := NewHandler(transaction, nil, users, Settings{})
	rows, err := handler.queryWorkspaces(ctx, user.ID, workspace.Slug, "", "")
	if err != nil {
		t.Fatalf("query workspace through shared schema: %v", err)
	}
	if len(rows) != 1 || rows[0].Role != roleAdmin || rows[0].TotalMembers != 1 {
		t.Fatalf("workspace rows = %#v", rows)
	}

	inviteID, err := newUUID()
	if err != nil {
		t.Fatal(err)
	}
	invite := WorkspaceInvite{
		ID: inviteID, CreatedAt: now, UpdatedAt: now, CreatedByID: &user.ID,
		WorkspaceID: workspace.ID, Email: "invite-" + suffix + "@pace.invalid",
		Token: "integration-token", Role: roleMember,
	}
	if err := transaction.Create(&invite).Error; err != nil {
		t.Fatalf("create workspace invitation through Django schema: %v", err)
	}
	serialized, err := handler.invitationJSON(ctx, invite)
	if err != nil {
		t.Fatalf("serialize workspace invitation: %v", err)
	}
	if serialized["token"] != invite.Token || serialized["invite_link"] == "" {
		t.Fatalf("serialized invitation = %#v", serialized)
	}
}
