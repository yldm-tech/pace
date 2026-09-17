package worker

import "context"

// emailIssue is what the notification email says about the work item it is about.
type emailIssue struct {
	ID            string `gorm:"column:id"`
	Name          string `gorm:"column:name"`
	SequenceID    int    `gorm:"column:sequence_id"`
	ProjectID     string `gorm:"column:project_id"`
	ProjectName   string `gorm:"column:project_name"`
	Identifier    string `gorm:"column:identifier"`
	WorkspaceSlug string `gorm:"column:workspace_slug"`
}

// emailPerson is what it says about whoever is reading it and whoever made the change.
type emailPerson struct {
	ID          string  `gorm:"column:id"`
	Email       string  `gorm:"column:email"`
	FirstName   string  `gorm:"column:first_name"`
	LastName    string  `gorm:"column:last_name"`
	DisplayName string  `gorm:"column:display_name"`
	Avatar      string  `gorm:"column:avatar"`
	AvatarAsset *string `gorm:"column:avatar_asset_id"`
}

// AvatarURL is what the email puts after the origin, which is what makes an uploaded avatar show and a linked one break.
func (person emailPerson) AvatarURL() string {
	if person.AvatarAsset != nil {
		return "/api/assets/v2/static/" + *person.AvatarAsset + "/"
	}
	return person.Avatar
}

func (tasks *EmailSendTasks) emailIssue(ctx context.Context, issueID string) (emailIssue, bool, error) {
	var rows []emailIssue
	err := tasks.db.WithContext(ctx).Table("issues i").
		Select(`i.id, i.name, i.sequence_id, i.project_id,
			p.name AS project_name, p.identifier, w.slug AS workspace_slug`).
		Joins("JOIN projects p ON p.id = i.project_id").
		Joins("JOIN workspaces w ON w.id = p.workspace_id").
		Where("i.id = ? AND i.deleted_at IS NULL", issueID).Limit(1).Scan(&rows).Error
	if err != nil || len(rows) == 0 {
		return emailIssue{}, false, err
	}
	return rows[0], true, nil
}

func (tasks *EmailSendTasks) emailUser(ctx context.Context, userID string) (emailPersonWithURL, bool, error) {
	var rows []emailPerson
	err := tasks.db.WithContext(ctx).Table("users").
		Select("id, email, first_name, last_name, display_name, avatar, avatar_asset_id").
		Where("id = ?", userID).Limit(1).Scan(&rows).Error
	if err != nil || len(rows) == 0 {
		return emailPersonWithURL{}, false, err
	}
	row := rows[0]
	return emailPersonWithURL{
		Email: row.Email, FirstName: row.FirstName, LastName: row.LastName,
		DisplayName: row.DisplayName, AvatarURL: row.AvatarURL(),
	}, true, nil
}

// emailPersonWithURL is the person as the template reads them.
type emailPersonWithURL struct {
	Email       string
	FirstName   string
	LastName    string
	DisplayName string
	AvatarURL   string
}

func (tasks *EmailSendTasks) displayName(ctx context.Context, userID string) (string, bool, error) {
	var values []string
	err := tasks.db.WithContext(ctx).Table("users").Where("id = ?", userID).
		Limit(1).Pluck("display_name", &values).Error
	if err != nil || len(values) == 0 {
		return "", false, err
	}
	return values[0], true, nil
}

// stringListArgument reads a list of ids off a task argument.
func stringListArgument(arguments []any, keywords map[string]any, index int, name string) []string {
	value, ok := argument(arguments, keywords, index, name)
	if !ok || value == nil {
		return nil
	}
	list, ok := value.([]any)
	if !ok {
		return nil
	}
	items := make([]string, 0, len(list))
	for _, entry := range list {
		items = append(items, activityTextOrEmpty(entry))
	}
	return items
}
