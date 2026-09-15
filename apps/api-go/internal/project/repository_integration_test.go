package project

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

// TestProjectModelsAgainstDjangoSchema is opt-in because it writes to the
// configured database. Every write is rolled back, and Go never creates or
// migrates tables.
func TestProjectModelsAgainstDjangoSchema(t *testing.T) {
	databaseURL := os.Getenv("PROJECT_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set PROJECT_TEST_DATABASE_URL to a disposable database with the Django schema")
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
	users := auth.NewGORMRepository(transaction, false, "pace-project-integration-secret")
	user, err := users.CreateUser(ctx, "go-project-"+suffix+"@pace.invalid", "!", true, true)
	if err != nil {
		t.Fatalf("create integration user: %v", err)
	}

	now := time.Now().UTC()
	workspaceID, err := newUUID()
	if err != nil {
		t.Fatal(err)
	}
	slug := "go-project-" + suffix
	workspace := testWorkspace{
		ID: workspaceID, CreatedAt: now, UpdatedAt: now, CreatedByID: &user.ID,
		Name: "Go Project WS " + suffix, OwnerID: user.ID, Slug: slug,
		Timezone: "UTC", BackgroundColor: "#3f76ff",
	}
	if err := transaction.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace through Django schema: %v", err)
	}
	workspaceMemberID, err := newUUID()
	if err != nil {
		t.Fatal(err)
	}
	member := testWorkspaceMember{
		ID: workspaceMemberID, CreatedAt: now, UpdatedAt: now, CreatedByID: &user.ID,
		WorkspaceID: workspaceID, MemberID: user.ID, Role: roleAdmin, IsActive: true,
		ViewProps: emptyJSON(), DefaultProps: emptyJSON(), IssueProps: emptyJSON(),
		GettingStartedChecklist: emptyJSON(), Tips: emptyJSON(), ExploredFeatures: emptyJSON(),
	}
	if err := transaction.Create(&member).Error; err != nil {
		t.Fatalf("create workspace member through Django schema: %v", err)
	}

	handler := NewHandler(transaction, nil, Settings{})
	projectID, err := newUUID()
	if err != nil {
		t.Fatal(err)
	}
	project := Project{
		ID: projectID, CreatedAt: now, UpdatedAt: now, CreatedByID: &user.ID,
		WorkspaceID: workspaceID, Name: "Go Project " + suffix, Identifier: "GP" + suffix[len(suffix)-6:],
		Network: 2, PageView: true, Timezone: "UTC", LogoProps: emptyJSON(),
	}
	if err := transaction.Create(&project).Error; err != nil {
		t.Fatalf("create project through Django schema: %v", err)
	}

	identifier := ProjectIdentifier{
		CreatedAt: now, UpdatedAt: now, CreatedByID: &user.ID,
		WorkspaceID: &workspaceID, ProjectID: project.ID, Name: project.Identifier,
	}
	if err := transaction.Create(&identifier).Error; err != nil {
		t.Fatalf("create project identifier through Django schema: %v", err)
	}
	if identifier.ID == 0 {
		t.Fatal("project identifier should receive its BigAutoField primary key")
	}

	if err := handler.addProjectAdmin(transaction, project, user.ID, user.ID, now); err != nil {
		t.Fatalf("create project member through Django schema: %v", err)
	}
	var property ProjectUserProperty
	err = transaction.Where("project_id = ? AND user_id = ? AND deleted_at IS NULL", project.ID, user.ID).Take(&property).Error
	if err != nil {
		t.Fatalf("read seeded project user property: %v", err)
	}
	if property.SortOrder != 65535 {
		t.Fatalf("first project sort order = %v, want the 65535 default", property.SortOrder)
	}

	if err := handler.createDefaultStates(transaction, project, user.ID, now); err != nil {
		t.Fatalf("create default states through Django schema: %v", err)
	}
	var states []State
	err = transaction.Where("project_id = ? AND deleted_at IS NULL", project.ID).Order("sequence").Find(&states).Error
	if err != nil {
		t.Fatalf("read default states: %v", err)
	}
	if len(states) != len(defaultStates) {
		t.Fatalf("default states = %d, want %d", len(states), len(defaultStates))
	}
	for index, state := range states {
		if state.Name != defaultStates[index].Name || state.Group != defaultStates[index].Group {
			t.Fatalf("default state %d = %#v", index, state)
		}
		// bulk_create bypasses State.save, so the slug stays empty.
		if state.Slug != "" {
			t.Fatalf("default state %d slug = %q, want the empty bulk_create value", index, state.Slug)
		}
	}

	row, found, err := handler.projectRowByID(ctx, slug, project.ID, user.ID, true)
	if err != nil || !found {
		t.Fatalf("query annotated project: %v, found=%v", err, found)
	}
	if row.MemberRole == nil || *row.MemberRole != roleAdmin {
		t.Fatalf("annotated member role = %#v", row.MemberRole)
	}
	if row.IsFavorite {
		t.Fatal("a fresh project should not be annotated as a favorite")
	}
	if row.SortOrder == nil || *row.SortOrder != 65535 {
		t.Fatalf("annotated sort order = %#v", row.SortOrder)
	}
	if row.Anchor != nil {
		t.Fatalf("annotated anchor = %#v, want none without a deploy board", row.Anchor)
	}

	data, err := handler.projectJSON(ctx, row)
	if err != nil {
		t.Fatalf("serialize project: %v", err)
	}
	if data["name"] != project.Name || data["next_work_item_sequence"] != int64(1) {
		t.Fatalf("serialized project = %#v", data)
	}
	members, ok := data["members"].([]string)
	if !ok || len(members) != 1 || members[0] != user.ID {
		t.Fatalf("serialized members = %#v", data["members"])
	}

	detail, err := handler.projectDetailJSON(ctx, project)
	if err != nil {
		t.Fatalf("serialize project detail: %v", err)
	}
	if detail["workspace_detail"] == nil || detail["inbox_view"] != project.IntakeView {
		t.Fatalf("serialized project detail = %#v", detail)
	}

	// The member routes read through the same rows the create flow seeds.
	projectMembers, err := handler.activeProjectMembers(ctx, slug, project.ID)
	if err != nil {
		t.Fatalf("list project members through Django schema: %v", err)
	}
	if len(projectMembers) != 1 || projectMembers[0].MemberID != user.ID || projectMembers[0].Role != roleAdmin {
		t.Fatalf("project members = %#v", projectMembers)
	}
	if serialized := projectMemberRoleJSON(projectMembers[0]); serialized["original_role"] != roleAdmin {
		t.Fatalf("serialized member role = %#v", serialized)
	}
	memberData, err := handler.projectMemberJSON(ctx, projectMembers[0], true)
	if err != nil {
		t.Fatalf("serialize project member: %v", err)
	}
	if memberData["project"] == nil || memberData["workspace"] == nil || memberData["member"] == nil {
		t.Fatalf("serialized project member = %#v", memberData)
	}
	roleRow, found, err := handler.activeProjectMember(ctx, slug, project.ID, user.ID)
	if err != nil || !found || roleRow.Role != roleAdmin {
		t.Fatalf("active project member = %#v, found=%v, err=%v", roleRow, found, err)
	}
	sortOrders, err := handler.minimumPropertySortOrders(transaction, workspaceID, []string{user.ID})
	if err != nil {
		t.Fatalf("read minimum sort orders: %v", err)
	}
	if sortOrders[user.ID] != 65535 {
		t.Fatalf("minimum sort order = %v", sortOrders[user.ID])
	}

	// The unique constraints Django relies on must reject a duplicate name.
	duplicateID, err := newUUID()
	if err != nil {
		t.Fatal(err)
	}
	duplicate := project
	duplicate.ID = duplicateID
	duplicate.Identifier = "DUP" + suffix[len(suffix)-5:]
	if err := transaction.Create(&duplicate).Error; err == nil {
		t.Fatal("expected the Django unique name constraint to reject the duplicate project")
	}
}

// testWorkspace and testWorkspaceMember write the two rows the project routes
// depend on. They mirror the columns internal/workspace already exercises.
type testWorkspace struct {
	ID              string    `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt       time.Time `gorm:"column:created_at"`
	UpdatedAt       time.Time `gorm:"column:updated_at"`
	CreatedByID     *string   `gorm:"column:created_by_id;type:uuid"`
	Name            string    `gorm:"column:name"`
	OwnerID         string    `gorm:"column:owner_id;type:uuid"`
	Slug            string    `gorm:"column:slug"`
	Timezone        string    `gorm:"column:timezone"`
	BackgroundColor string    `gorm:"column:background_color"`
}

func (testWorkspace) TableName() string { return "workspaces" }

type testWorkspaceMember struct {
	ID                      string         `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt               time.Time      `gorm:"column:created_at"`
	UpdatedAt               time.Time      `gorm:"column:updated_at"`
	CreatedByID             *string        `gorm:"column:created_by_id;type:uuid"`
	WorkspaceID             string         `gorm:"column:workspace_id;type:uuid"`
	MemberID                string         `gorm:"column:member_id;type:uuid"`
	Role                    int            `gorm:"column:role"`
	IsActive                bool           `gorm:"column:is_active"`
	ViewProps               auth.JSONValue `gorm:"column:view_props;type:jsonb"`
	DefaultProps            auth.JSONValue `gorm:"column:default_props;type:jsonb"`
	IssueProps              auth.JSONValue `gorm:"column:issue_props;type:jsonb"`
	GettingStartedChecklist auth.JSONValue `gorm:"column:getting_started_checklist;type:jsonb"`
	Tips                    auth.JSONValue `gorm:"column:tips;type:jsonb"`
	ExploredFeatures        auth.JSONValue `gorm:"column:explored_features;type:jsonb"`
}

func (testWorkspaceMember) TableName() string { return "workspace_members" }
