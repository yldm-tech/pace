package project

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
	"github.com/yldm-tech/pace/apps/api/internal/auth"
	"github.com/yldm-tech/pace/apps/api/internal/drf"
	"gorm.io/gorm"
)

func (handler *Handler) registerIssueActivityRoutes(router gin.IRouter) {
	router.GET("/api/workspaces/:slug/projects/:id/issues/:issue/history/", handler.authenticatedIssueUUID(handler.issueActivityList))
}

// IssueActivity is the db.IssueActivity table.
type IssueActivity struct {
	ID             string         `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt      time.Time      `gorm:"column:created_at"`
	UpdatedAt      time.Time      `gorm:"column:updated_at"`
	CreatedByID    *string        `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID    *string        `gorm:"column:updated_by_id;type:uuid"`
	DeletedAt      *time.Time     `gorm:"column:deleted_at"`
	ProjectID      string         `gorm:"column:project_id;type:uuid"`
	WorkspaceID    string         `gorm:"column:workspace_id;type:uuid"`
	IssueID        *string        `gorm:"column:issue_id;type:uuid"`
	Verb           string         `gorm:"column:verb"`
	Field          *string        `gorm:"column:field"`
	OldValue       *string        `gorm:"column:old_value"`
	NewValue       *string        `gorm:"column:new_value"`
	Comment        string         `gorm:"column:comment"`
	Attachments    pq.StringArray `gorm:"column:attachments;type:text[]"`
	IssueCommentID *string        `gorm:"column:issue_comment_id;type:uuid"`
	ActorID        *string        `gorm:"column:actor_id;type:uuid"`
	OldIdentifier  *string        `gorm:"column:old_identifier;type:uuid"`
	NewIdentifier  *string        `gorm:"column:new_identifier;type:uuid"`
	Epoch          *float64       `gorm:"column:epoch"`
}

func (IssueActivity) TableName() string { return "issue_activities" }

// hiddenActivityFields are the four the endpoint never returns: comments come back through their own serializer, and votes, reactions and draft edits are not history.
var hiddenActivityFields = []string{"comment", "vote", "reaction", "draft"}

func (handler *Handler) issueActivityList(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	slug, projectID, issueID := c.Param("slug"), c.Param("id"), c.Param("issue")
	// created_at__gt is passed straight to the ORM, which parses it and raises on anything it cannot read.
	var since *time.Time
	if raw := c.Query("created_at__gt"); raw != "" {
		parsed, ok := parseDjangoDateTime(raw)
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Please provide valid detail"})
			return
		}
		since = &parsed
	}

	switch c.Query("activity_type") {
	case "issue-property":
		activities, err := handler.issueActivityRows(c.Request.Context(), slug, projectID, issueID, user.ID, since)
		if err != nil {
			handler.internalError(c, err)
			return
		}
		serialized, err := handler.serializeActivities(c.Request.Context(), activities, true)
		if err != nil {
			handler.internalError(c, err)
			return
		}
		drf.Respond(c, http.StatusOK, serialized)
	case "issue-comment":
		serialized, err := handler.issueActivityComments(c.Request.Context(), slug, projectID, issueID, user.ID, since)
		if err != nil {
			handler.internalError(c, err)
			return
		}
		drf.Respond(c, http.StatusOK, serialized)
	default:
		// Django's fall-through sorts the two querysets by instance["created_at"], and a model instance is not subscriptable, so anything that is not exactly one of the two names above raises TypeError and answers 500. The epic variants the web client sends — epic-property and epic-comment — land here too, as does the one caller that sends no activity_type at all.
		handler.internalError(c, errors.New("issue activity: the combined branch indexes a model instance"))
	}
}

// issueActivityRows is the history queryset: the caller must be an active member of the project, the project must not be archived, and the four hidden fields are excluded.
func (handler *Handler) issueActivityRows(ctx context.Context, slug, projectID, issueID, userID string, since *time.Time) ([]IssueActivity, error) {
	query := handler.db.WithContext(ctx).Table("issue_activities ia").
		Joins("JOIN workspaces w ON w.id = ia.workspace_id").
		Joins("JOIN projects p ON p.id = ia.project_id").
		Joins("JOIN project_members pm ON pm.project_id = ia.project_id AND pm.member_id = ? AND pm.is_active = TRUE", userID).
		Where(`ia.issue_id = ? AND ia.deleted_at IS NULL AND w.slug = ? AND p.archived_at IS NULL
			AND (ia.field IS NULL OR ia.field NOT IN ?)`, issueID, slug, hiddenActivityFields)
	if since != nil {
		query = query.Where("ia.created_at > ?", *since)
	}
	// The endpoint sorts oldest first, unlike the model's own ordering.
	var rows []IssueActivity
	if err := query.Order("ia.created_at ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// serializeActivities is IssueActivitySerializer, which is fields = "__all__" plus the four nested details and source_data. The nested rows are read in one query each rather than per activity.
func (handler *Handler) serializeActivities(ctx context.Context, activities []IssueActivity, withSourceData bool) ([]gin.H, error) {
	serialized := make([]gin.H, 0, len(activities))
	if len(activities) == 0 {
		return serialized, nil
	}
	actors, err := handler.activityActors(ctx, activities)
	if err != nil {
		return nil, err
	}
	issues, err := handler.activityIssues(ctx, activities)
	if err != nil {
		return nil, err
	}
	project, err := handler.projectLiteJSON(ctx, activities[0].ProjectID)
	if err != nil {
		return nil, err
	}
	workspace, err := handler.workspaceLiteJSON(ctx, activities[0].WorkspaceID)
	if err != nil {
		return nil, err
	}
	var sources map[string]gin.H
	if withSourceData {
		// Only the issue-property branch prefetches the intake row, and the serializer returns null without it.
		if sources, err = handler.activityIntakeSources(ctx, activities); err != nil {
			return nil, err
		}
	}

	for _, activity := range activities {
		serialized = append(serialized, handler.activityJSON(activity, project, workspace, actors, issues, sources))
	}
	return serialized, nil
}

// activityJSON lays out one activity. The nested details are looked up rather than re-queried, and a null actor or issue serializes as null rather than reaching into an empty map.
func (handler *Handler) activityJSON(activity IssueActivity, project, workspace gin.H, actors, issues, sources map[string]gin.H) gin.H {
	data := gin.H{
		"id": activity.ID, "created_at": activity.CreatedAt, "updated_at": activity.UpdatedAt,
		"created_by": activity.CreatedByID, "updated_by": activity.UpdatedByID,
		"deleted_at": activity.DeletedAt,
		"project":    activity.ProjectID, "workspace": activity.WorkspaceID, "issue": activity.IssueID,
		"verb": activity.Verb, "field": activity.Field,
		"old_value": activity.OldValue, "new_value": activity.NewValue,
		"comment": activity.Comment, "attachments": stringsOrEmpty(activity.Attachments),
		"issue_comment": activity.IssueCommentID, "actor": activity.ActorID,
		"old_identifier": activity.OldIdentifier, "new_identifier": activity.NewIdentifier,
		"epoch":            activity.Epoch,
		"project_detail":   project,
		"workspace_detail": workspace,
		"actor_detail":     nil,
		"issue_detail":     nil,
		"source_data":      nil,
	}
	if activity.ActorID != nil {
		if actor, found := actors[*activity.ActorID]; found {
			data["actor_detail"] = actor
		}
	}
	if activity.IssueID != nil {
		if issue, found := issues[*activity.IssueID]; found {
			data["issue_detail"] = issue
		}
		// Only the issue-property branch prefetches the intake row, and the serializer returns null without it.
		if source, found := sources[*activity.IssueID]; found {
			data["source_data"] = source
		}
	}
	return data
}

func (handler *Handler) activityActors(ctx context.Context, activities []IssueActivity) (map[string]gin.H, error) {
	identifiers := make([]string, 0, len(activities))
	for _, activity := range activities {
		if activity.ActorID != nil {
			identifiers = append(identifiers, *activity.ActorID)
		}
	}
	actors := map[string]gin.H{}
	if len(identifiers) == 0 {
		return actors, nil
	}
	var users []auth.User
	if err := handler.db.WithContext(ctx).Where("id IN ?", identifiers).Find(&users).Error; err != nil {
		return nil, err
	}
	for _, user := range users {
		actors[user.ID] = liteUserJSON(user, false)
	}
	return actors, nil
}

func (handler *Handler) activityIssues(ctx context.Context, activities []IssueActivity) (map[string]gin.H, error) {
	identifiers := map[string]bool{}
	for _, activity := range activities {
		if activity.IssueID != nil {
			identifiers[*activity.IssueID] = true
		}
	}
	if len(identifiers) == 0 {
		return map[string]gin.H{}, nil
	}
	// The per-work-item history points every activity at the same issue, but the workspace activity feeds serialize a page that spans issues, so the whole set is read at once.
	list := make([]string, 0, len(identifiers))
	for identifier := range identifiers {
		list = append(list, identifier)
	}
	issues, err := handler.issueFlatJSONByID(ctx, list)
	if err != nil {
		return nil, err
	}
	if len(issues) < len(identifiers) {
		// The lookup this replaced read one issue at a time and failed the whole response on a missing row, rather than serializing that activity with a null issue_detail.
		return nil, gorm.ErrRecordNotFound
	}
	return issues, nil
}

// activityIntakeSources is the issue__issue_intake prefetch. The serializer reads the first row of it, so an issue that never came through intake has no source data.
func (handler *Handler) activityIntakeSources(ctx context.Context, activities []IssueActivity) (map[string]gin.H, error) {
	identifiers := map[string]bool{}
	for _, activity := range activities {
		if activity.IssueID != nil {
			identifiers[*activity.IssueID] = true
		}
	}
	sources := map[string]gin.H{}
	if len(identifiers) == 0 {
		return sources, nil
	}
	list := make([]string, 0, len(identifiers))
	for identifier := range identifiers {
		list = append(list, identifier)
	}
	var rows []struct {
		IssueID     string  `gorm:"column:issue_id"`
		Source      *string `gorm:"column:source"`
		SourceEmail *string `gorm:"column:source_email"`
		Extra       []byte  `gorm:"column:extra"`
	}
	err := handler.db.WithContext(ctx).Table("intake_issues").
		Select("issue_id, source, source_email, extra").
		Where("issue_id IN ? AND deleted_at IS NULL", list).Find(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if _, already := sources[row.IssueID]; already {
			continue
		}
		sources[row.IssueID] = gin.H{
			"source": row.Source, "source_email": row.SourceEmail, "extra": decodeJSON(row.Extra),
		}
	}
	return sources, nil
}

// issueActivityComments is the comment half of the endpoint, which reuses the comment serializer the comment routes already return.
func (handler *Handler) issueActivityComments(ctx context.Context, slug, projectID, issueID, userID string, since *time.Time) ([]gin.H, error) {
	query := handler.db.WithContext(ctx).Table("issue_comments ic").
		Joins("JOIN workspaces w ON w.id = ic.workspace_id").
		Joins("JOIN projects p ON p.id = ic.project_id").
		Joins("JOIN project_members pm ON pm.project_id = ic.project_id AND pm.member_id = ? AND pm.is_active = TRUE", userID).
		Where("ic.issue_id = ? AND ic.deleted_at IS NULL AND w.slug = ? AND p.archived_at IS NULL", issueID, slug)
	if since != nil {
		query = query.Where("ic.created_at > ?", *since)
	}
	var comments []IssueComment
	if err := query.Order("ic.created_at ASC").Find(&comments).Error; err != nil {
		return nil, err
	}
	serialized := make([]gin.H, 0, len(comments))
	for _, comment := range comments {
		data, err := handler.issueCommentJSON(ctx, comment, nil)
		if err != nil {
			return nil, err
		}
		serialized = append(serialized, data)
	}
	return serialized, nil
}

// projectLiteJSON is ProjectLiteSerializer.
func (handler *Handler) projectLiteJSON(ctx context.Context, projectID string) (gin.H, error) {
	var row struct {
		ID         string  `gorm:"column:id"`
		Identifier string  `gorm:"column:identifier"`
		Name       string  `gorm:"column:name"`
		CoverImage *string `gorm:"column:cover_image"`
		CoverAsset *string `gorm:"column:cover_image_asset_id"`
		LogoProps  []byte  `gorm:"column:logo_props"`
		Descr      string  `gorm:"column:description"`
	}
	err := handler.db.WithContext(ctx).Table("projects").
		Select("id, identifier, name, cover_image, cover_image_asset_id, logo_props, description").
		Where("id = ?", projectID).Take(&row).Error
	if err != nil {
		return nil, err
	}
	coverURL := any(nil)
	if row.CoverAsset != nil {
		coverURL = "/api/assets/v2/static/" + *row.CoverAsset + "/"
	} else if row.CoverImage != nil {
		coverURL = *row.CoverImage
	}
	return gin.H{
		"id": row.ID, "identifier": row.Identifier, "name": row.Name,
		"cover_image": row.CoverImage, "cover_image_url": coverURL,
		"logo_props": decodeJSON(row.LogoProps), "description": row.Descr,
	}, nil
}

// workspaceLiteJSON is WorkspaceLiteSerializer.
func (handler *Handler) workspaceLiteJSON(ctx context.Context, workspaceID string) (gin.H, error) {
	var row struct {
		ID        string  `gorm:"column:id"`
		Name      string  `gorm:"column:name"`
		Slug      string  `gorm:"column:slug"`
		LogoAsset *string `gorm:"column:logo_asset_id"`
	}
	err := handler.db.WithContext(ctx).Table("workspaces").
		Select("id, name, slug, logo_asset_id").Where("id = ?", workspaceID).Take(&row).Error
	if err != nil {
		return nil, err
	}
	logoURL := any(nil)
	if row.LogoAsset != nil {
		logoURL = "/api/assets/v2/static/" + *row.LogoAsset + "/"
	}
	return gin.H{"name": row.Name, "slug": row.Slug, "id": row.ID, "logo_url": logoURL}, nil
}

// parseDjangoDateTime accepts what the ORM accepts for a datetime lookup: an ISO-8601 instant, with or without an offset, and a bare date.
func parseDjangoDateTime(value string) (time.Time, bool) {
	for _, layout := range []string{
		time.RFC3339Nano, time.RFC3339,
		"2006-01-02T15:04:05.999999", "2006-01-02T15:04:05",
		"2006-01-02 15:04:05.999999-07:00", "2006-01-02 15:04:05-07:00",
		"2006-01-02 15:04:05.999999", "2006-01-02 15:04:05",
		"2006-01-02",
	} {
		if parsed, err := time.ParseInLocation(layout, value, time.UTC); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}
