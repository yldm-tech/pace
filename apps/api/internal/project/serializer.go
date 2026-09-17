package project

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api/internal/auth"

	"github.com/yldm-tech/pace/apps/api/internal/drf"
)

// projectJSON is ProjectListSerializer: every Project model field plus the
// annotations and method fields the serializer declares.
func (handler *Handler) projectJSON(ctx context.Context, row projectRow) (gin.H, error) {
	members, err := handler.projectMemberIDs(ctx, row.ID)
	if err != nil {
		return nil, err
	}
	sequence, err := handler.nextWorkItemSequence(ctx, row.ID)
	if err != nil {
		return nil, err
	}
	data := projectFieldsJSON(row.Project)
	data["is_favorite"] = row.IsFavorite
	data["sort_order"] = row.SortOrder
	data["member_role"] = row.MemberRole
	data["anchor"] = row.Anchor
	data["members"] = members
	data["cover_image_url"] = coverImageURL(row.Project)
	data["inbox_view"] = row.IntakeView
	data["next_work_item_sequence"] = sequence
	return data, nil
}

// projectDetailJSON is ProjectSerializer, the shape partial_update snapshots
// into current_instance for the model activity task.
func (handler *Handler) projectDetailJSON(ctx context.Context, project Project) (gin.H, error) {
	data := projectFieldsJSON(project)
	data["inbox_view"] = project.IntakeView
	workspace, err := handler.workspaceLite(ctx, project.WorkspaceID)
	if err != nil {
		return nil, err
	}
	data["workspace_detail"] = workspace
	return data, nil
}

func projectFieldsJSON(project Project) gin.H {
	return gin.H{
		"id": project.ID, "created_at": project.CreatedAt, "updated_at": project.UpdatedAt,
		"created_by": project.CreatedByID, "updated_by": project.UpdatedByID,
		"deleted_at": project.DeletedAt, "name": project.Name, "description": project.Description,
		"description_text": decodeJSON(project.DescriptionText), "description_html": decodeJSON(project.DescriptionHTML),
		"network": project.Network, "workspace": project.WorkspaceID, "identifier": project.Identifier,
		"default_assignee": project.DefaultAssigneeID, "project_lead": project.ProjectLeadID,
		"emoji": project.Emoji, "icon_prop": decodeJSON(project.IconProp),
		"module_view": project.ModuleView, "cycle_view": project.CycleView,
		"issue_views_view": project.IssueViewsView, "page_view": project.PageView,
		"intake_view": project.IntakeView, "is_time_tracking_enabled": project.IsTimeTrackingEnabled,
		"is_issue_type_enabled": project.IsIssueTypeEnabled, "guest_view_all_features": project.GuestViewAllFeatures,
		"cover_image": project.CoverImage, "cover_image_asset": project.CoverImageAssetID,
		"estimate": project.EstimateID, "archive_in": project.ArchiveIn, "close_in": project.CloseIn,
		"logo_props": decodeJSON(project.LogoProps), "default_state": project.DefaultStateID,
		"archived_at": project.ArchivedAt, "timezone": project.Timezone,
		"external_source": project.ExternalSource, "external_id": project.ExternalID,
	}
}

// projectListJSON is the values() projection the light list route returns.
func projectListJSON(row projectListRow) gin.H {
	return gin.H{
		"id": row.ID, "name": row.Name, "identifier": row.Identifier, "sort_order": row.SortOrder,
		"logo_props": decodeJSON(row.LogoProps), "member_role": row.MemberRole,
		"intake_count": row.IntakeCount, "archived_at": row.ArchivedAt, "workspace": row.WorkspaceID,
		"cycle_view": row.CycleView, "issue_views_view": row.IssueViewsView,
		"module_view": row.ModuleView, "page_view": row.PageView, "inbox_view": row.InboxView,
		"guest_view_all_features": row.GuestViewAllFeatures, "project_lead": row.ProjectLeadID,
		"network": row.Network, "created_at": row.CreatedAt, "updated_at": row.UpdatedAt,
		"created_by": row.CreatedByID, "updated_by": row.UpdatedByID,
	}
}

func coverImageURL(project Project) any {
	if project.CoverImageAssetID != nil {
		return "/api/assets/v2/static/" + *project.CoverImageAssetID + "/"
	}
	if project.CoverImage != nil && *project.CoverImage != "" {
		return *project.CoverImage
	}
	return nil
}

// projectMemberIDs is ProjectListSerializer.get_members, which reads the
// prefetched active members and skips bots.
func (handler *Handler) projectMemberIDs(ctx context.Context, projectID string) ([]string, error) {
	members := make([]string, 0)
	err := handler.db.WithContext(ctx).Table("project_members pm").
		Joins("JOIN users u ON u.id = pm.member_id").
		Where("pm.project_id = ? AND pm.is_active = TRUE AND pm.deleted_at IS NULL AND u.is_bot = FALSE", projectID).
		Order("pm.created_at DESC").Pluck("pm.member_id", &members).Error
	return members, err
}

// nextWorkItemSequence is get_next_work_item_sequence: the highest issue
// sequence plus one, or one when the project has none. Django treats a stored
// zero as missing because it tests the aggregate for truthiness.
func (handler *Handler) nextWorkItemSequence(ctx context.Context, projectID string) (int64, error) {
	var maximum *int64
	err := handler.db.WithContext(ctx).Table("issue_sequences").
		Where("project_id = ? AND deleted_at IS NULL", projectID).
		Select("MAX(sequence)").Scan(&maximum).Error
	if err != nil {
		return 0, err
	}
	if maximum == nil || *maximum == 0 {
		return 1, nil
	}
	return *maximum + 1, nil
}

func (handler *Handler) workspaceLite(ctx context.Context, workspaceID string) (gin.H, error) {
	var workspace struct {
		ID          string  `gorm:"column:id"`
		Name        string  `gorm:"column:name"`
		Slug        string  `gorm:"column:slug"`
		Logo        *string `gorm:"column:logo"`
		LogoAssetID *string `gorm:"column:logo_asset_id"`
	}
	err := handler.db.WithContext(ctx).Table("workspaces").
		Where("id = ?", workspaceID).Take(&workspace).Error
	if err != nil {
		return nil, err
	}
	logoURL := any(nil)
	if workspace.LogoAssetID != nil {
		logoURL = "/api/assets/v2/static/" + *workspace.LogoAssetID + "/"
	} else if workspace.Logo != nil && *workspace.Logo != "" {
		logoURL = *workspace.Logo
	}
	return gin.H{"name": workspace.Name, "slug": workspace.Slug, "id": workspace.ID, "logo_url": logoURL}, nil
}

func decodeJSON(value []byte) any {
	return drf.DecodeJSON(value)
}

// projectMemberRoleJSON is ProjectMemberRoleSerializer. DynamicBaseSerializer
// discards the fields argument the views pass, so every declared field is
// returned, and original_role simply mirrors role.
func projectMemberRoleJSON(member ProjectMember) gin.H {
	return gin.H{
		"id": member.ID, "role": member.Role, "member": member.MemberID,
		"project": member.ProjectID, "original_role": member.Role,
		"created_at": member.CreatedAt,
	}
}

// projectMemberJSON is ProjectMemberSerializer, or ProjectMemberAdminSerializer
// when admin is set, which is the same shape with the member's email and last
// login medium added.
func (handler *Handler) projectMemberJSON(ctx context.Context, member ProjectMember, admin bool) (gin.H, error) {
	var user auth.User
	if err := handler.db.WithContext(ctx).Where("id = ?", member.MemberID).Take(&user).Error; err != nil {
		return nil, err
	}
	workspace, err := handler.workspaceLite(ctx, member.WorkspaceID)
	if err != nil {
		return nil, err
	}
	project, err := handler.projectLite(ctx, member.ProjectID)
	if err != nil {
		return nil, err
	}
	return gin.H{
		"id": member.ID, "created_at": member.CreatedAt, "updated_at": member.UpdatedAt,
		"created_by": member.CreatedByID, "updated_by": member.UpdatedByID,
		"deleted_at": member.DeletedAt, "workspace": workspace, "project": project,
		"member": liteUserJSON(user, admin), "comment": member.Comment, "role": member.Role,
		"view_props": decodeJSON(member.ViewProps), "default_props": decodeJSON(member.DefaultProps),
		"preferences": decodeJSON(member.Preferences), "sort_order": member.SortOrder,
		"is_active": member.IsActive,
	}, nil
}

// projectMemberPreferenceJSON is ProjectMemberPreferenceSerializer, whose
// project_id, member_id, and workspace_id resolve to the model's id attributes.
func projectMemberPreferenceJSON(member ProjectMember) gin.H {
	return gin.H{
		"preferences": decodeJSON(member.Preferences), "project_id": member.ProjectID,
		"member_id": member.MemberID, "workspace_id": member.WorkspaceID,
	}
}

// projectLite is ProjectLiteSerializer.
func (handler *Handler) projectLite(ctx context.Context, projectID string) (gin.H, error) {
	var project Project
	if err := handler.db.WithContext(ctx).Where("id = ?", projectID).Take(&project).Error; err != nil {
		return nil, err
	}
	return gin.H{
		"id": project.ID, "identifier": project.Identifier, "name": project.Name,
		"cover_image": project.CoverImage, "cover_image_url": coverImageURL(project),
		"logo_props": decodeJSON(project.LogoProps), "description": project.Description,
	}, nil
}

// liteUserJSON is UserLiteSerializer, or UserAdminLiteSerializer when admin is
// set.
func liteUserJSON(user auth.User, admin bool) gin.H {
	avatar := any(nil)
	if user.AvatarAssetID != nil {
		avatar = "/api/assets/v2/static/" + *user.AvatarAssetID + "/"
	} else if user.Avatar != "" {
		avatar = user.Avatar
	}
	data := gin.H{
		"id": user.ID, "first_name": user.FirstName, "last_name": user.LastName,
		"avatar": user.Avatar, "avatar_url": avatar, "is_bot": user.IsBot,
		"display_name": user.DisplayName,
	}
	if admin {
		data["email"] = user.Email
		data["last_login_medium"] = user.LastLoginMedium
	}
	return data
}
