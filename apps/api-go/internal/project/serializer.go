package project

import (
	"context"
	"encoding/json"

	"github.com/gin-gonic/gin"
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
	if len(value) == 0 {
		return nil
	}
	var decoded any
	if json.Unmarshal(value, &decoded) != nil {
		return nil
	}
	return decoded
}
