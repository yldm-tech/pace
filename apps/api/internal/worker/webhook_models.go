package worker

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"time"
)

// The nine shapes a webhook can carry. They are the v1 API's serializers, read straight out of the tables.
//
// Two things about them are worth knowing before reading any of the queries. **The annotations are absent**: the task reads each model with a plain lookup and no annotations, and DRF leaves a field out rather than failing when the attribute is not there — so a project carries no member count, a cycle no work item counts, and a link between a cycle and a work item no sub-item count. And **the key order is the serializer's**, which is why each of these is written by hand rather than through a map.

// modelData is get_model_data: the object a webhook is told about.
func (tasks *WebhookTasks) modelData(ctx context.Context, event, identifier string) (json.RawMessage, bool, error) {
	switch event {
	case "project":
		return tasks.projectData(ctx, identifier)
	case "issue":
		return tasks.issueData(ctx, identifier, true)
	case "cycle":
		return tasks.cycleData(ctx, identifier)
	case "module":
		return tasks.moduleData(ctx, identifier)
	case "cycle_issue":
		return tasks.cycleIssueData(ctx, identifier)
	case "module_issue":
		return tasks.moduleIssueData(ctx, identifier)
	case "issue_comment":
		return tasks.issueCommentData(ctx, identifier)
	case "user":
		return tasks.userData(ctx, identifier)
	case "intake_issue":
		return tasks.intakeIssueData(ctx, identifier)
	}
	return nil, false, nil
}

func (tasks *WebhookTasks) projectData(ctx context.Context, identifier string) (json.RawMessage, bool, error) {
	var rows []struct {
		ID                    string     `gorm:"column:id"`
		CreatedAt             time.Time  `gorm:"column:created_at"`
		UpdatedAt             time.Time  `gorm:"column:updated_at"`
		DeletedAt             *time.Time `gorm:"column:deleted_at"`
		Name                  string     `gorm:"column:name"`
		Description           string     `gorm:"column:description"`
		DescriptionText       []byte     `gorm:"column:description_text"`
		DescriptionHTML       []byte     `gorm:"column:description_html"`
		Network               int        `gorm:"column:network"`
		Identifier            string     `gorm:"column:identifier"`
		Emoji                 *string    `gorm:"column:emoji"`
		IconProp              []byte     `gorm:"column:icon_prop"`
		ModuleView            bool       `gorm:"column:module_view"`
		CycleView             bool       `gorm:"column:cycle_view"`
		IssueViewsView        bool       `gorm:"column:issue_views_view"`
		PageView              bool       `gorm:"column:page_view"`
		IntakeView            bool       `gorm:"column:intake_view"`
		IsTimeTrackingEnabled bool       `gorm:"column:is_time_tracking_enabled"`
		IsIssueTypeEnabled    bool       `gorm:"column:is_issue_type_enabled"`
		GuestViewAllFeatures  bool       `gorm:"column:guest_view_all_features"`
		CoverImage            *string    `gorm:"column:cover_image"`
		ArchiveIn             int        `gorm:"column:archive_in"`
		CloseIn               int        `gorm:"column:close_in"`
		LogoProps             []byte     `gorm:"column:logo_props"`
		ArchivedAt            *time.Time `gorm:"column:archived_at"`
		Timezone              string     `gorm:"column:timezone"`
		ExternalSource        *string    `gorm:"column:external_source"`
		ExternalID            *string    `gorm:"column:external_id"`
		CreatedByID           *string    `gorm:"column:created_by_id"`
		UpdatedByID           *string    `gorm:"column:updated_by_id"`
		WorkspaceID           string     `gorm:"column:workspace_id"`
		DefaultAssigneeID     *string    `gorm:"column:default_assignee_id"`
		ProjectLeadID         *string    `gorm:"column:project_lead_id"`
		CoverImageAssetID     *string    `gorm:"column:cover_image_asset_id"`
		EstimateID            *string    `gorm:"column:estimate_id"`
		DefaultStateID        *string    `gorm:"column:default_state_id"`
	}
	err := tasks.db.WithContext(ctx).Table("projects").
		Where("id = ? AND deleted_at IS NULL", identifier).Limit(1).Scan(&rows).Error
	if err != nil || len(rows) == 0 {
		return nil, false, err
	}
	row := rows[0]
	// The six annotated fields are absent because nothing annotated them: total_members, total_cycles, total_modules, is_member, member_role, sort_order and is_deployed.
	data, err := orderedJSON([]field{
		{"id", row.ID}, {"cover_image_url", staticAssetURL(row.CoverImageAssetID, stringOrBlank(row.CoverImage))},
		{"created_at", row.CreatedAt}, {"updated_at", row.UpdatedAt}, {"deleted_at", row.DeletedAt},
		{"name", row.Name}, {"description", row.Description},
		{"description_text", rawJSON(row.DescriptionText)}, {"description_html", rawJSON(row.DescriptionHTML)},
		{"network", row.Network}, {"identifier", row.Identifier}, {"emoji", row.Emoji},
		{"icon_prop", rawJSON(row.IconProp)},
		{"module_view", row.ModuleView}, {"cycle_view", row.CycleView},
		{"issue_views_view", row.IssueViewsView}, {"page_view", row.PageView}, {"intake_view", row.IntakeView},
		{"is_time_tracking_enabled", row.IsTimeTrackingEnabled}, {"is_issue_type_enabled", row.IsIssueTypeEnabled},
		{"guest_view_all_features", row.GuestViewAllFeatures},
		{"cover_image", row.CoverImage}, {"archive_in", row.ArchiveIn}, {"close_in", row.CloseIn},
		{"logo_props", rawJSON(row.LogoProps)}, {"archived_at", row.ArchivedAt}, {"timezone", row.Timezone},
		{"external_source", row.ExternalSource}, {"external_id", row.ExternalID},
		{"created_by", row.CreatedByID}, {"updated_by", row.UpdatedByID}, {"workspace", row.WorkspaceID},
		{"default_assignee", row.DefaultAssigneeID}, {"project_lead", row.ProjectLeadID},
		{"cover_image_asset", row.CoverImageAssetID}, {"estimate", row.EstimateID},
		{"default_state", row.DefaultStateID},
	})
	return data, err == nil, err
}

// issueData is IssueExpandSerializer. The cycle and the module it declares are read through a reverse relation that has no single value, so both are absent; the state, the labels and the assignees are there.
//
// expand decides how the two lists are written: the webhook's own copy names each label and each person in full, while the copy nested inside an intake work item carries their ids alone.
func (tasks *WebhookTasks) issueData(ctx context.Context, identifier string, expand bool) (json.RawMessage, bool, error) {
	var rows []struct {
		ID                  string     `gorm:"column:id"`
		CreatedAt           time.Time  `gorm:"column:created_at"`
		UpdatedAt           time.Time  `gorm:"column:updated_at"`
		DeletedAt           *time.Time `gorm:"column:deleted_at"`
		Point               *int       `gorm:"column:point"`
		Name                string     `gorm:"column:name"`
		DescriptionJSON     []byte     `gorm:"column:description_json"`
		DescriptionHTML     string     `gorm:"column:description_html"`
		DescriptionStripped *string    `gorm:"column:description_stripped"`
		DescriptionBinary   []byte     `gorm:"column:description_binary"`
		Priority            string     `gorm:"column:priority"`
		StartDate           *time.Time `gorm:"column:start_date"`
		TargetDate          *time.Time `gorm:"column:target_date"`
		SequenceID          int        `gorm:"column:sequence_id"`
		SortOrder           float64    `gorm:"column:sort_order"`
		CompletedAt         *time.Time `gorm:"column:completed_at"`
		ArchivedAt          *time.Time `gorm:"column:archived_at"`
		IsDraft             bool       `gorm:"column:is_draft"`
		ExternalSource      *string    `gorm:"column:external_source"`
		ExternalID          *string    `gorm:"column:external_id"`
		CreatedByID         *string    `gorm:"column:created_by_id"`
		UpdatedByID         *string    `gorm:"column:updated_by_id"`
		ProjectID           string     `gorm:"column:project_id"`
		WorkspaceID         string     `gorm:"column:workspace_id"`
		ParentID            *string    `gorm:"column:parent_id"`
		EstimatePointID     *string    `gorm:"column:estimate_point_id"`
		TypeID              *string    `gorm:"column:type_id"`
		StateID             *string    `gorm:"column:state_id"`
	}
	err := tasks.db.WithContext(ctx).Table("issues").
		Where("id = ? AND deleted_at IS NULL", identifier).Limit(1).Scan(&rows).Error
	if err != nil || len(rows) == 0 {
		return nil, false, err
	}
	row := rows[0]

	state, err := tasks.stateLite(ctx, row.StateID)
	if err != nil {
		return nil, false, err
	}
	labels, err := tasks.issueLabels(ctx, identifier, expand)
	if err != nil {
		return nil, false, err
	}
	assignees, err := tasks.issueAssigneeList(ctx, identifier, expand)
	if err != nil {
		return nil, false, err
	}

	data, err := orderedJSON([]field{
		{"id", row.ID},
		{"labels", labels}, {"assignees", assignees}, {"state", state},
		{"description", rawJSON(row.DescriptionJSON)},
		{"created_at", row.CreatedAt}, {"updated_at", row.UpdatedAt}, {"deleted_at", row.DeletedAt},
		{"point", row.Point}, {"name", row.Name},
		{"description_json", rawJSON(row.DescriptionJSON)}, {"description_html", row.DescriptionHTML},
		{"description_stripped", row.DescriptionStripped},
		// A binary description is rendered as base64, and an absent one stays absent rather than becoming an empty string.
		{"description_binary", base64OrNull(row.DescriptionBinary)},
		{"priority", row.Priority},
		{"start_date", dateOrNull(row.StartDate)}, {"target_date", dateOrNull(row.TargetDate)},
		{"sequence_id", row.SequenceID}, {"sort_order", row.SortOrder},
		{"completed_at", row.CompletedAt}, {"archived_at", dateOrNull(row.ArchivedAt)},
		{"is_draft", row.IsDraft},
		{"external_source", row.ExternalSource}, {"external_id", row.ExternalID},
		{"created_by", row.CreatedByID}, {"updated_by", row.UpdatedByID},
		{"project", row.ProjectID}, {"workspace", row.WorkspaceID},
		{"parent", row.ParentID}, {"estimate_point", row.EstimatePointID}, {"type", row.TypeID},
	})
	return data, err == nil, err
}

func (tasks *WebhookTasks) cycleData(ctx context.Context, identifier string) (json.RawMessage, bool, error) {
	var rows []struct {
		ID               string     `gorm:"column:id"`
		CreatedAt        time.Time  `gorm:"column:created_at"`
		UpdatedAt        time.Time  `gorm:"column:updated_at"`
		DeletedAt        *time.Time `gorm:"column:deleted_at"`
		Name             string     `gorm:"column:name"`
		Description      string     `gorm:"column:description"`
		StartDate        *time.Time `gorm:"column:start_date"`
		EndDate          *time.Time `gorm:"column:end_date"`
		ViewProps        []byte     `gorm:"column:view_props"`
		SortOrder        float64    `gorm:"column:sort_order"`
		ExternalSource   *string    `gorm:"column:external_source"`
		ExternalID       *string    `gorm:"column:external_id"`
		ProgressSnapshot []byte     `gorm:"column:progress_snapshot"`
		ArchivedAt       *time.Time `gorm:"column:archived_at"`
		LogoProps        []byte     `gorm:"column:logo_props"`
		Timezone         string     `gorm:"column:timezone"`
		Version          int        `gorm:"column:version"`
		CreatedByID      *string    `gorm:"column:created_by_id"`
		UpdatedByID      *string    `gorm:"column:updated_by_id"`
		ProjectID        string     `gorm:"column:project_id"`
		WorkspaceID      string     `gorm:"column:workspace_id"`
		OwnedByID        *string    `gorm:"column:owned_by_id"`
	}
	err := tasks.db.WithContext(ctx).Table("cycles").
		Where("id = ? AND deleted_at IS NULL", identifier).Limit(1).Scan(&rows).Error
	if err != nil || len(rows) == 0 {
		return nil, false, err
	}
	row := rows[0]
	data, err := orderedJSON([]field{
		{"id", row.ID},
		{"created_at", row.CreatedAt}, {"updated_at", row.UpdatedAt}, {"deleted_at", row.DeletedAt},
		{"name", row.Name}, {"description", row.Description},
		{"start_date", row.StartDate}, {"end_date", row.EndDate},
		{"view_props", rawJSON(row.ViewProps)}, {"sort_order", row.SortOrder},
		{"external_source", row.ExternalSource}, {"external_id", row.ExternalID},
		{"progress_snapshot", rawJSON(row.ProgressSnapshot)}, {"archived_at", row.ArchivedAt},
		{"logo_props", rawJSON(row.LogoProps)}, {"timezone", row.Timezone}, {"version", row.Version},
		{"created_by", row.CreatedByID}, {"updated_by", row.UpdatedByID},
		{"project", row.ProjectID}, {"workspace", row.WorkspaceID}, {"owned_by", row.OwnedByID},
	})
	return data, err == nil, err
}

func (tasks *WebhookTasks) moduleData(ctx context.Context, identifier string) (json.RawMessage, bool, error) {
	var rows []struct {
		ID              string     `gorm:"column:id"`
		CreatedAt       time.Time  `gorm:"column:created_at"`
		UpdatedAt       time.Time  `gorm:"column:updated_at"`
		DeletedAt       *time.Time `gorm:"column:deleted_at"`
		Name            string     `gorm:"column:name"`
		Description     string     `gorm:"column:description"`
		DescriptionText []byte     `gorm:"column:description_text"`
		DescriptionHTML []byte     `gorm:"column:description_html"`
		StartDate       *time.Time `gorm:"column:start_date"`
		TargetDate      *time.Time `gorm:"column:target_date"`
		Status          string     `gorm:"column:status"`
		ViewProps       []byte     `gorm:"column:view_props"`
		SortOrder       float64    `gorm:"column:sort_order"`
		ExternalSource  *string    `gorm:"column:external_source"`
		ExternalID      *string    `gorm:"column:external_id"`
		ArchivedAt      *time.Time `gorm:"column:archived_at"`
		LogoProps       []byte     `gorm:"column:logo_props"`
		CreatedByID     *string    `gorm:"column:created_by_id"`
		UpdatedByID     *string    `gorm:"column:updated_by_id"`
		ProjectID       string     `gorm:"column:project_id"`
		WorkspaceID     string     `gorm:"column:workspace_id"`
		LeadID          *string    `gorm:"column:lead_id"`
	}
	err := tasks.db.WithContext(ctx).Table("modules").
		Where("id = ? AND deleted_at IS NULL", identifier).Limit(1).Scan(&rows).Error
	if err != nil || len(rows) == 0 {
		return nil, false, err
	}
	row := rows[0]
	// members is declared write only, so it is not here either.
	data, err := orderedJSON([]field{
		{"id", row.ID},
		{"created_at", row.CreatedAt}, {"updated_at", row.UpdatedAt}, {"deleted_at", row.DeletedAt},
		{"name", row.Name}, {"description", row.Description},
		{"description_text", rawJSON(row.DescriptionText)}, {"description_html", rawJSON(row.DescriptionHTML)},
		{"start_date", dateOrNull(row.StartDate)}, {"target_date", dateOrNull(row.TargetDate)},
		{"status", row.Status}, {"view_props", rawJSON(row.ViewProps)}, {"sort_order", row.SortOrder},
		{"external_source", row.ExternalSource}, {"external_id", row.ExternalID},
		{"archived_at", row.ArchivedAt}, {"logo_props", rawJSON(row.LogoProps)},
		{"created_by", row.CreatedByID}, {"updated_by", row.UpdatedByID},
		{"project", row.ProjectID}, {"workspace", row.WorkspaceID}, {"lead", row.LeadID},
	})
	return data, err == nil, err
}

func (tasks *WebhookTasks) cycleIssueData(ctx context.Context, identifier string) (json.RawMessage, bool, error) {
	return tasks.linkData(ctx, "cycle_issues", "cycle_id", "cycle", identifier)
}

func (tasks *WebhookTasks) moduleIssueData(ctx context.Context, identifier string) (json.RawMessage, bool, error) {
	return tasks.linkData(ctx, "module_issues", "module_id", "module", identifier)
}

// linkData is the shape the two link serializers share. Neither carries the sub-item count it declares, because nothing annotated it.
func (tasks *WebhookTasks) linkData(ctx context.Context, table, column, name, identifier string) (json.RawMessage, bool, error) {
	var rows []struct {
		ID          string     `gorm:"column:id"`
		CreatedAt   time.Time  `gorm:"column:created_at"`
		UpdatedAt   time.Time  `gorm:"column:updated_at"`
		DeletedAt   *time.Time `gorm:"column:deleted_at"`
		CreatedByID *string    `gorm:"column:created_by_id"`
		UpdatedByID *string    `gorm:"column:updated_by_id"`
		ProjectID   string     `gorm:"column:project_id"`
		WorkspaceID string     `gorm:"column:workspace_id"`
		IssueID     string     `gorm:"column:issue_id"`
		Target      string     `gorm:"column:target"`
	}
	err := tasks.db.WithContext(ctx).Table(table).
		Select("id, created_at, updated_at, deleted_at, created_by_id, updated_by_id, project_id, workspace_id, issue_id, "+column+" AS target").
		Where("id = ? AND deleted_at IS NULL", identifier).Limit(1).Scan(&rows).Error
	if err != nil || len(rows) == 0 {
		return nil, false, err
	}
	row := rows[0]
	fields := []field{
		{"id", row.ID},
		{"created_at", row.CreatedAt}, {"updated_at", row.UpdatedAt}, {"deleted_at", row.DeletedAt},
		{"created_by", row.CreatedByID}, {"updated_by", row.UpdatedByID},
		{"project", row.ProjectID}, {"workspace", row.WorkspaceID},
	}
	// The two put their own two ends in opposite orders, which is what their field lists say.
	if name == "cycle" {
		fields = append(fields, field{"issue", row.IssueID}, field{"cycle", row.Target})
	} else {
		fields = append(fields, field{"module", row.Target}, field{"issue", row.IssueID})
	}
	data, err := orderedJSON(fields)
	return data, err == nil, err
}

func (tasks *WebhookTasks) issueCommentData(ctx context.Context, identifier string) (json.RawMessage, bool, error) {
	var rows []struct {
		ID             string     `gorm:"column:id"`
		CreatedAt      time.Time  `gorm:"column:created_at"`
		UpdatedAt      time.Time  `gorm:"column:updated_at"`
		DeletedAt      *time.Time `gorm:"column:deleted_at"`
		CommentHTML    string     `gorm:"column:comment_html"`
		Attachments    []byte     `gorm:"column:attachments"`
		Access         string     `gorm:"column:access"`
		ExternalSource *string    `gorm:"column:external_source"`
		ExternalID     *string    `gorm:"column:external_id"`
		EditedAt       *time.Time `gorm:"column:edited_at"`
		CreatedByID    *string    `gorm:"column:created_by_id"`
		UpdatedByID    *string    `gorm:"column:updated_by_id"`
		ProjectID      string     `gorm:"column:project_id"`
		WorkspaceID    string     `gorm:"column:workspace_id"`
		DescriptionID  *string    `gorm:"column:description_id"`
		IssueID        string     `gorm:"column:issue_id"`
		ActorID        *string    `gorm:"column:actor_id"`
		ParentID       *string    `gorm:"column:parent_id"`
	}
	err := tasks.db.WithContext(ctx).Table("issue_comments").
		Select(`id, created_at, updated_at, deleted_at, comment_html, attachments::text AS attachments,
			access, external_source, external_id, edited_at, created_by_id, updated_by_id,
			project_id, workspace_id, description_id, issue_id, actor_id, parent_id`).
		Where("id = ? AND deleted_at IS NULL", identifier).Limit(1).Scan(&rows).Error
	if err != nil || len(rows) == 0 {
		return nil, false, err
	}
	row := rows[0]
	// is_member is annotated elsewhere and is not here.
	data, err := orderedJSON([]field{
		{"id", row.ID},
		{"created_at", row.CreatedAt}, {"updated_at", row.UpdatedAt}, {"deleted_at", row.DeletedAt},
		{"comment_html", row.CommentHTML}, {"attachments", postgresArray(row.Attachments)},
		{"access", row.Access},
		{"external_source", row.ExternalSource}, {"external_id", row.ExternalID}, {"edited_at", row.EditedAt},
		{"created_by", row.CreatedByID}, {"updated_by", row.UpdatedByID},
		{"project", row.ProjectID}, {"workspace", row.WorkspaceID},
		{"description", row.DescriptionID}, {"issue", row.IssueID},
		{"actor", row.ActorID}, {"parent", row.ParentID},
	})
	return data, err == nil, err
}

func (tasks *WebhookTasks) userData(ctx context.Context, identifier string) (json.RawMessage, bool, error) {
	var rows []struct {
		ID            string  `gorm:"column:id"`
		FirstName     string  `gorm:"column:first_name"`
		LastName      string  `gorm:"column:last_name"`
		Email         string  `gorm:"column:email"`
		Avatar        string  `gorm:"column:avatar"`
		AvatarAssetID *string `gorm:"column:avatar_asset_id"`
		DisplayName   string  `gorm:"column:display_name"`
	}
	err := tasks.db.WithContext(ctx).Table("users").
		Select("id, first_name, last_name, email, avatar, avatar_asset_id, display_name").
		Where("id = ?", identifier).Limit(1).Scan(&rows).Error
	if err != nil || len(rows) == 0 {
		return nil, false, err
	}
	row := rows[0]
	data, err := orderedJSON([]field{
		{"id", row.ID}, {"first_name", row.FirstName}, {"last_name", row.LastName},
		{"email", row.Email}, {"avatar", row.Avatar},
		{"avatar_url", staticAssetURL(row.AvatarAssetID, row.Avatar)},
		{"display_name", row.DisplayName},
	})
	return data, err == nil, err
}

// intakeIssueData carries the whole work item inside it, and that nested copy is the one without the expanded labels and assignees.
func (tasks *WebhookTasks) intakeIssueData(ctx context.Context, identifier string) (json.RawMessage, bool, error) {
	var rows []struct {
		ID             string     `gorm:"column:id"`
		CreatedAt      time.Time  `gorm:"column:created_at"`
		UpdatedAt      time.Time  `gorm:"column:updated_at"`
		DeletedAt      *time.Time `gorm:"column:deleted_at"`
		Status         int        `gorm:"column:status"`
		SnoozedTill    *time.Time `gorm:"column:snoozed_till"`
		Source         *string    `gorm:"column:source"`
		SourceEmail    *string    `gorm:"column:source_email"`
		ExternalSource *string    `gorm:"column:external_source"`
		ExternalID     *string    `gorm:"column:external_id"`
		Extra          []byte     `gorm:"column:extra"`
		CreatedByID    *string    `gorm:"column:created_by_id"`
		UpdatedByID    *string    `gorm:"column:updated_by_id"`
		ProjectID      string     `gorm:"column:project_id"`
		WorkspaceID    string     `gorm:"column:workspace_id"`
		IntakeID       *string    `gorm:"column:intake_id"`
		IssueID        string     `gorm:"column:issue_id"`
		DuplicateToID  *string    `gorm:"column:duplicate_to_id"`
	}
	err := tasks.db.WithContext(ctx).Table("intake_issues").
		Where("id = ? AND deleted_at IS NULL", identifier).Limit(1).Scan(&rows).Error
	if err != nil || len(rows) == 0 {
		return nil, false, err
	}
	row := rows[0]
	issue, found, err := tasks.issueData(ctx, row.IssueID, false)
	if err != nil {
		return nil, false, err
	}
	if !found {
		issue = json.RawMessage("null")
	}
	data, err := orderedJSON([]field{
		{"id", row.ID}, {"issue_detail", issue}, {"inbox", row.IntakeID},
		{"created_at", row.CreatedAt}, {"updated_at", row.UpdatedAt}, {"deleted_at", row.DeletedAt},
		{"status", row.Status}, {"snoozed_till", row.SnoozedTill},
		{"source", row.Source}, {"source_email", row.SourceEmail},
		{"external_source", row.ExternalSource}, {"external_id", row.ExternalID},
		{"extra", rawJSON(row.Extra)},
		{"created_by", row.CreatedByID}, {"updated_by", row.UpdatedByID},
		{"project", row.ProjectID}, {"workspace", row.WorkspaceID},
		{"intake", row.IntakeID}, {"issue", row.IssueID}, {"duplicate_to", row.DuplicateToID},
	})
	return data, err == nil, err
}

// stateLite is the four fields a work item's state is described by.
func (tasks *WebhookTasks) stateLite(ctx context.Context, stateID *string) (json.RawMessage, error) {
	if stateID == nil {
		return json.RawMessage("null"), nil
	}
	var rows []struct {
		ID    string `gorm:"column:id"`
		Name  string `gorm:"column:name"`
		Color string `gorm:"column:color"`
		Group string `gorm:"column:group"`
	}
	err := tasks.db.WithContext(ctx).Table("states").
		Select(`id, name, color, "group"`).
		Where("id = ? AND deleted_at IS NULL", *stateID).Limit(1).Scan(&rows).Error
	if err != nil || len(rows) == 0 {
		return json.RawMessage("null"), err
	}
	return orderedJSON([]field{
		{"id", rows[0].ID}, {"name", rows[0].Name}, {"color", rows[0].Color}, {"group", rows[0].Group},
	})
}

// issueLabels is either the labels in full or their ids, and either way it reads the links rather than the labels so a link that was taken away is not counted.
func (tasks *WebhookTasks) issueLabels(ctx context.Context, issueID string, expand bool) (json.RawMessage, error) {
	var rows []struct {
		ID    string `gorm:"column:id"`
		Name  string `gorm:"column:name"`
		Color string `gorm:"column:color"`
	}
	err := tasks.db.WithContext(ctx).Table("issue_labels il").
		Select("l.id, l.name, l.color").
		Joins("JOIN labels l ON l.id = il.label_id").
		Where("il.issue_id = ? AND il.deleted_at IS NULL", issueID).
		Order("il.created_at DESC").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	items := make([]json.RawMessage, 0, len(rows))
	for _, row := range rows {
		if !expand {
			encoded, err := json.Marshal(row.ID)
			if err != nil {
				return nil, err
			}
			items = append(items, encoded)
			continue
		}
		encoded, err := orderedJSON([]field{{"id", row.ID}, {"name", row.Name}, {"color", row.Color}})
		if err != nil {
			return nil, err
		}
		items = append(items, encoded)
	}
	return orderedList(items), nil
}

func (tasks *WebhookTasks) issueAssigneeList(ctx context.Context, issueID string, expand bool) (json.RawMessage, error) {
	var rows []struct {
		ID            string  `gorm:"column:id"`
		FirstName     string  `gorm:"column:first_name"`
		LastName      string  `gorm:"column:last_name"`
		Email         string  `gorm:"column:email"`
		Avatar        string  `gorm:"column:avatar"`
		AvatarAssetID *string `gorm:"column:avatar_asset_id"`
		DisplayName   string  `gorm:"column:display_name"`
	}
	err := tasks.db.WithContext(ctx).Table("issue_assignees ia").
		Select("u.id, u.first_name, u.last_name, u.email, u.avatar, u.avatar_asset_id, u.display_name").
		Joins("JOIN users u ON u.id = ia.assignee_id").
		Where("ia.issue_id = ? AND ia.deleted_at IS NULL", issueID).
		Order("ia.created_at DESC").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	items := make([]json.RawMessage, 0, len(rows))
	for _, row := range rows {
		if !expand {
			encoded, err := json.Marshal(row.ID)
			if err != nil {
				return nil, err
			}
			items = append(items, encoded)
			continue
		}
		encoded, err := orderedJSON([]field{
			{"id", row.ID}, {"first_name", row.FirstName}, {"last_name", row.LastName},
			{"email", row.Email}, {"avatar", row.Avatar},
			{"avatar_url", staticAssetURL(row.AvatarAssetID, row.Avatar)},
			{"display_name", row.DisplayName},
		})
		if err != nil {
			return nil, err
		}
		items = append(items, encoded)
	}
	return orderedList(items), nil
}

// rawJSON keeps a json column as it is stored, and a null one as the empty object the model defaults to.
func rawJSON(value []byte) json.RawMessage {
	if len(value) == 0 {
		return json.RawMessage("{}")
	}
	return json.RawMessage(value)
}

// base64OrNull renders a binary column the way Django's BinaryField does, and leaves an absent one absent.
func base64OrNull(value []byte) any {
	if value == nil {
		return nil
	}
	return base64.StdEncoding.EncodeToString(value)
}

// dateOrNull renders a date column without its time, which is what a DateField is rendered as.
func dateOrNull(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.Format("2006-01-02")
}

// postgresArray turns a text array read as text into the list a serializer reports.
func postgresArray(value []byte) []string {
	rendered := string(value)
	if len(rendered) < 2 || rendered[0] != '{' || rendered[len(rendered)-1] != '}' {
		return []string{}
	}
	inner := rendered[1 : len(rendered)-1]
	if inner == "" {
		return []string{}
	}
	items := []string{}
	current := ""
	quoted := false
	escaped := false
	for _, character := range inner {
		switch {
		case escaped:
			current += string(character)
			escaped = false
		case character == '\\':
			escaped = true
		case character == '"':
			quoted = !quoted
		case character == ',' && !quoted:
			items = append(items, current)
			current = ""
		default:
			current += string(character)
		}
	}
	return append(items, current)
}

// staticAssetURL is the avatar_url and cover_image_url properties: the uploaded asset first, then whatever url was stored, and null when there is neither.
func staticAssetURL(assetID *string, stored string) any {
	if assetID != nil {
		return "/api/assets/v2/static/" + *assetID + "/"
	}
	if stored != "" {
		return stored
	}
	return nil
}

func stringOrBlank(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
