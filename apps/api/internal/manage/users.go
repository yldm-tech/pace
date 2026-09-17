package manage

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	zxcvbn "github.com/nbutton23/zxcvbn-go"
	"github.com/yldm-tech/pace/apps/api/internal/auth"
	"github.com/yldm-tech/pace/apps/api/internal/projects"
	"gorm.io/gorm"
)

// activateUser is manage.py activate_user: let somebody sign in again.
func activateUser(ctx context.Context, env Environment, arguments []string) error {
	db, err := database(env)
	if err != nil {
		return err
	}
	values := positional(arguments)
	if len(values) == 0 || values[0] == "" {
		return commandError("Error: Email is required")
	}
	email := values[0]

	var users []string
	if err := db.WithContext(ctx).Table("users").Where("email = ?", email).Limit(1).Pluck("id", &users).Error; err != nil {
		return err
	}
	if len(users) == 0 {
		return commandError("Error: User with %s does not exists", email)
	}
	// user.save() writes every column, so the email is lowercased and trimmed on the way out — which is what User.save does whether or not anything else changed.
	err = db.WithContext(ctx).Table("users").Where("id = ?", users[0]).Updates(map[string]any{
		"is_active": true, "email": strings.ToLower(strings.TrimSpace(email)), "updated_at": time.Now().UTC(),
	}).Error
	if err != nil {
		return err
	}
	write(env, "User activated successfully")
	return nil
}

// resetPassword is manage.py reset_password: set somebody's password from the terminal.
//
// The password is asked for twice and never echoed, refused when the two do not match, refused when it is blank, and refused when zxcvbn scores it below three. The first three answer on stderr and return zero; only the zxcvbn refusal is a CommandError. That difference is upstream's and is kept.
func resetPassword(ctx context.Context, env Environment, arguments []string) error {
	db, err := database(env)
	if err != nil {
		return err
	}
	values := positional(arguments)
	if len(values) == 0 || values[0] == "" {
		write(env, "Error: Email is required")
		return nil
	}
	email := values[0]

	var users []string
	if err := db.WithContext(ctx).Table("users").Where("email = ?", email).Limit(1).Pluck("id", &users).Error; err != nil {
		return err
	}
	if len(users) == 0 {
		write(env, "Error: User with %s does not exists", email)
		return nil
	}

	password, err := env.Secret("Password: ")
	if err != nil {
		return err
	}
	again, err := env.Secret("Password (again): ")
	if err != nil {
		return err
	}
	if password != again {
		write(env, "Error: Your passwords didn't match.")
		return nil
	}
	if strings.TrimSpace(password) == "" {
		write(env, "Error: Blank passwords aren't allowed.")
		return nil
	}
	if zxcvbn.PasswordStrength(password, nil).Score < 3 {
		return commandError("Password is too common please set a complex password")
	}

	hashed, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	err = db.WithContext(ctx).Table("users").Where("id = ?", users[0]).Updates(map[string]any{
		"password": hashed, "is_password_autoset": false,
		"email": strings.ToLower(strings.TrimSpace(email)), "updated_at": time.Now().UTC(),
	}).Error
	if err != nil {
		return err
	}
	write(env, "User password updated successfully")
	return nil
}

// createInstanceAdmin is manage.py create_instance_admin: give somebody the instance-wide admin role.
//
// The instance it attaches them to is the *oldest* registration, not the newest: Instance.objects.last() over a model whose Meta orders newest first.
func createInstanceAdmin(ctx context.Context, env Environment, arguments []string) error {
	db, err := database(env)
	if err != nil {
		return err
	}
	values := positional(arguments)
	if len(values) == 0 || values[0] == "" {
		return commandError("Please provide the email of the admin.")
	}
	email := values[0]

	var users []string
	if err := db.WithContext(ctx).Table("users").Where("email = ?", email).Limit(1).Pluck("id", &users).Error; err != nil {
		return err
	}
	if len(users) == 0 {
		return commandError("User with the provided email does not exist.")
	}

	var instances []string
	// Instance.objects.last() over a model ordered newest first, so it is the *oldest* registration that the admin is attached to.
	err = db.WithContext(ctx).Table("instances").Where("deleted_at IS NULL").
		Order("created_at").Limit(1).Pluck("id", &instances).Error
	if err != nil {
		return commandError("Failed to create the instance admin.")
	}
	var instanceID any
	if len(instances) > 0 {
		instanceID = instances[0]
	}

	var existing int64
	err = db.WithContext(ctx).Table("instance_admins").
		Where("user_id = ? AND instance_id = ? AND role = 20 AND deleted_at IS NULL", users[0], instanceID).
		Count(&existing).Error
	if err != nil {
		return commandError("Failed to create the instance admin.")
	}
	if existing > 0 {
		return commandError("The provided email is already an instance admin.")
	}

	now := time.Now().UTC()
	err = db.WithContext(ctx).Table("instance_admins").Create(map[string]any{
		"id": uuid.NewString(), "created_at": now, "updated_at": now,
		"created_by_id": nil, "updated_by_id": nil,
		"user_id": users[0], "instance_id": instanceID, "role": 20, "is_verified": false,
	}).Error
	if err != nil {
		return commandError("Failed to create the instance admin.")
	}
	write(env, "Successfully created the admin")
	return nil
}

// reactivateWorkspaceMember is manage.py reactivate_workspace_member: put somebody back into a workspace they were removed from.
//
// It reports two things beyond doing the work, because neither is obvious to whoever runs it: removing a member also deactivates their project memberships, and this leaves those alone; and somebody removed by deactivating their account still cannot sign in.
func reactivateWorkspaceMember(ctx context.Context, env Environment, arguments []string) error {
	db, err := database(env)
	if err != nil {
		return err
	}
	values := positional(arguments)
	slug, email := "", ""
	if len(values) > 0 {
		slug = strings.TrimSpace(values[0])
	}
	if len(values) > 1 {
		// Emails are stored lowercased and trimmed, so the lookup normalises before it asks.
		email = strings.ToLower(strings.TrimSpace(values[1]))
	}
	if slug == "" {
		return commandError("Error: Workspace slug is required")
	}
	if email == "" {
		return commandError("Error: Email is required")
	}

	var users []string
	if err := db.WithContext(ctx).Table("users").Where("email = ?", email).Limit(1).Pluck("id", &users).Error; err != nil {
		return err
	}
	if len(users) == 0 {
		return commandError("Error: User with %s does not exist", email)
	}
	var workspaces []string
	if err := db.WithContext(ctx).Table("workspaces").Where("slug = ? AND deleted_at IS NULL", slug).Limit(1).Pluck("id", &workspaces).Error; err != nil {
		return err
	}
	if len(workspaces) == 0 {
		return commandError("Error: Workspace with slug %s does not exist", slug)
	}

	var membership struct {
		ID       string `gorm:"column:id"`
		IsActive bool   `gorm:"column:is_active"`
		Role     int    `gorm:"column:role"`
	}
	err = db.WithContext(ctx).Table("workspace_members").
		Where("workspace_id = ? AND member_id = ? AND deleted_at IS NULL", workspaces[0], users[0]).
		Select("id, is_active, role").Order("created_at DESC").Limit(1).Take(&membership).Error
	if err != nil {
		return commandError("Error: User %s is not a member of workspace %s", email, slug)
	}
	if membership.IsActive {
		write(env, "User %s is already an active member of workspace %s", email, slug)
		return nil
	}

	// update_fields keeps the write to the two columns that change, and disable_auto_set_user is what stops the save from blanking the audit columns the way it would in a worker.
	err = db.WithContext(ctx).Table("workspace_members").Where("id = ?", membership.ID).
		Updates(map[string]any{"is_active": true, "updated_at": time.Now().UTC()}).Error
	if err != nil {
		return err
	}
	write(env, "User %s reactivated successfully in workspace %s as %s", email, slug, workspaceRoleName(membership.Role))

	var inactiveProjects int64
	err = db.WithContext(ctx).Table("project_members").
		Where("workspace_id = ? AND member_id = ? AND is_active = FALSE AND deleted_at IS NULL", workspaces[0], users[0]).
		Count(&inactiveProjects).Error
	if err != nil {
		return err
	}
	if inactiveProjects > 0 {
		write(env, "Note: %d project membership(s) remain inactive; removing a member also deactivates their project memberships", inactiveProjects)
	}

	var active []bool
	if err := db.WithContext(ctx).Table("users").Where("id = ?", users[0]).Limit(1).Pluck("is_active", &active).Error; err != nil {
		return err
	}
	if len(active) > 0 && !active[0] {
		write(env, "Note: the account for %s is deactivated and cannot sign in. Run 'manage activate_user %s' to activate it.", email, email)
	}
	return nil
}

// workspaceRoleName is get_role_display: the label beside the number in the model's choices.
func workspaceRoleName(role int) string {
	switch role {
	case 20:
		return "Admin"
	case 15:
		return "Member"
	case 5:
		return "Guest"
	}
	return ""
}

// createProjectMember is manage.py create_project_member: add somebody already in the workspace to one of its projects.
//
// The role defaults to twenty, which is admin — so running it without --role makes an administrator of the project rather than a member.
func createProjectMember(ctx context.Context, env Environment, arguments []string) error {
	db, err := database(env)
	if err != nil {
		return err
	}
	projectID, _ := flagValue(arguments, "project_id")
	userEmail, _ := flagValue(arguments, "user_email")
	role := projectMemberRole(arguments)

	if projectID == "" {
		return commandError("Project ID is required")
	}
	if userEmail == "" {
		return commandError("User Email is required")
	}
	write(env, "Role: %d", role)

	var users []string
	if err := db.WithContext(ctx).Table("users").Where("email = ?", userEmail).Limit(1).Pluck("id", &users).Error; err != nil {
		return err
	}
	if len(users) == 0 {
		return commandError("User not found")
	}
	var project struct {
		ID          string `gorm:"column:id"`
		WorkspaceID string `gorm:"column:workspace_id"`
	}
	err = db.WithContext(ctx).Table("projects").Where("id = ? AND deleted_at IS NULL", projectID).
		Select("id, workspace_id").Take(&project).Error
	if err != nil {
		return commandError("Project not found")
	}

	var inWorkspace int64
	err = db.WithContext(ctx).Table("workspace_members").
		Where("workspace_id = ? AND member_id = ? AND is_active = TRUE AND deleted_at IS NULL", project.WorkspaceID, users[0]).
		Count(&inWorkspace).Error
	if err != nil {
		return err
	}
	if inWorkspace == 0 {
		return commandError("User not member in workspace")
	}

	now := time.Now().UTC()
	var existing int64
	err = db.WithContext(ctx).Table("project_members").
		Where("project_id = ? AND member_id = ? AND deleted_at IS NULL", project.ID, users[0]).Count(&existing).Error
	if err != nil {
		return err
	}
	if existing > 0 {
		// update() writes the columns it names and nothing else, so updated_at does not move and the audit columns are left where they were.
		err = db.WithContext(ctx).Table("project_members").
			Where("project_id = ? AND member_id = ? AND deleted_at IS NULL", project.ID, users[0]).
			Updates(map[string]any{"is_active": true, "role": role}).Error
		if err != nil {
			return err
		}
		// The membership already existed, so nothing seeded the person's ordering for this project; get_or_create writes it with the column default rather than with the ordering ProjectMember.save would have computed.
		if err := ensureProjectUserProperty(ctx, db, project.ID, project.WorkspaceID, users[0], 65535, now); err != nil {
			return err
		}
	} else if err := addProjectMember(ctx, db, project.ID, project.WorkspaceID, users[0], role, now); err != nil {
		return err
	}
	write(env, "User %s added to project %s", userEmail, projectID)
	return nil
}

// addProjectMember is ProjectMember.save on the adding path. It writes the membership and, ahead of it, the person's ordering for this project — which is seeded ten thousand below everything else they already have, so a project they were just added to sorts first.
//
// Both rows are written with no author at all. BaseModel.save reads the current user out of crum and a management command has none, so it blanks the two audit columns whatever the row was built with.
func addProjectMember(ctx context.Context, db *gorm.DB, projectID, workspaceID, memberID string, role int, now time.Time) error {
	var minimum *float64
	err := db.WithContext(ctx).Table("project_user_properties").
		Where("workspace_id = ? AND user_id = ? AND deleted_at IS NULL", workspaceID, memberID).
		Select("MIN(sort_order)").Scan(&minimum).Error
	if err != nil {
		return err
	}
	sortOrder := 65535.0
	if minimum != nil {
		sortOrder = *minimum - 10000
	}
	if err := ensureProjectUserProperty(ctx, db, projectID, workspaceID, memberID, sortOrder, now); err != nil {
		return err
	}
	return db.WithContext(ctx).Table("project_members").Create(map[string]any{
		"id": uuid.NewString(), "created_at": now, "updated_at": now,
		"created_by_id": nil, "updated_by_id": nil,
		"project_id": projectID, "workspace_id": workspaceID, "member_id": memberID,
		"role": role, "is_active": true,
		"view_props": string(projects.DefaultPropsJSON()), "default_props": string(projects.DefaultPropsJSON()),
		"preferences": string(projects.DefaultPreferencesJSON()),
		// The membership's own sort order is the column default rather than the one just computed, which belongs to the ordering row.
		"sort_order": 65535,
	}).Error
}

// projectMemberRole reads --role, and falls back to twenty when it was not given or is not a number.
func projectMemberRole(arguments []string) int {
	value, given := flagValue(arguments, "role")
	if !given || value == "" {
		return 20
	}
	role := 0
	for _, character := range value {
		if character < '0' || character > '9' {
			return 20
		}
		role = role*10 + int(character-'0')
	}
	return role
}

// ensureProjectUserProperty is get_or_create over the row that holds somebody's display settings and their ordering for one project.
func ensureProjectUserProperty(ctx context.Context, db *gorm.DB, projectID, workspaceID, userID string, sortOrder float64, now time.Time) error {
	var existing int64
	err := db.WithContext(ctx).Table("project_user_properties").
		Where("project_id = ? AND user_id = ? AND deleted_at IS NULL", projectID, userID).Count(&existing).Error
	if err != nil {
		return err
	}
	if existing > 0 {
		return nil
	}
	return db.WithContext(ctx).Table("project_user_properties").Create(map[string]any{
		"id": uuid.NewString(), "created_at": now, "updated_at": now,
		"created_by_id": nil, "updated_by_id": nil,
		"project_id": projectID, "workspace_id": workspaceID, "user_id": userID,
		"filters": string(projects.DefaultFiltersJSON()), "display_filters": string(projects.DefaultDisplayFiltersJSON()),
		"display_properties": string(projects.DefaultDisplayPropertiesJSON()), "rich_filters": "{}",
		"preferences": string(projects.DefaultPreferencesJSON()), "sort_order": sortOrder,
	}).Error
}
