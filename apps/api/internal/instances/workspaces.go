package instances

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/yldm-tech/pace/apps/api/internal/auth"
	"github.com/yldm-tech/pace/apps/api/internal/drf"
	"github.com/yldm-tech/pace/apps/api/internal/pagination"
	"github.com/yldm-tech/pace/apps/api/internal/workspace"
	"gorm.io/gorm"
)

// workspaceSlugCheck says whether a slug is free.
//
// It answers `true` for free and `false` for taken, and takes the slug case-insensitively — so a workspace called Acme makes acme unavailable too. The restricted names are compared exactly rather than case-insensitively, which is what lets a capitalised one through.
func (handler *Handler) workspaceSlugCheck(c *gin.Context, _ *auth.User, _ *Instance) {
	slug := c.Query("slug")
	if slug == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Workspace Slug is required"})
		return
	}
	var taken int64
	err := handler.db.WithContext(c.Request.Context()).Table("workspaces").
		Where("LOWER(slug) = LOWER(?) AND deleted_at IS NULL", slug).Count(&taken).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	handler.respond(c, http.StatusOK, gin.H{"status": taken == 0 && !workspace.RestrictedSlug(slug)})
}

// instanceWorkspace is one workspace as the console lists it, with the two counts beside it.
type instanceWorkspace struct {
	ID               string     `gorm:"column:id"`
	CreatedAt        time.Time  `gorm:"column:created_at"`
	UpdatedAt        time.Time  `gorm:"column:updated_at"`
	CreatedByID      *string    `gorm:"column:created_by_id"`
	UpdatedByID      *string    `gorm:"column:updated_by_id"`
	DeletedAt        *time.Time `gorm:"column:deleted_at"`
	Name             string     `gorm:"column:name"`
	Logo             *string    `gorm:"column:logo"`
	LogoAssetID      *string    `gorm:"column:logo_asset_id"`
	Slug             string     `gorm:"column:slug"`
	OrganizationSize *string    `gorm:"column:organization_size"`
	Timezone         string     `gorm:"column:timezone"`
	BackgroundColor  string     `gorm:"column:background_color"`
	OwnerID          string     `gorm:"column:owner_id"`
	TotalProjects    *int64     `gorm:"column:total_projects"`
	TotalMembers     *int64     `gorm:"column:total_members"`
}

// workspaceList is every workspace on the installation, ten at a time.
//
// The two counts are correlated subqueries rather than joins, so a workspace with no projects reports null rather than zero — which is what an empty subquery gives. The member count leaves out bots and anybody deactivated; the project count leaves out nothing.
func (handler *Handler) workspaceList(c *gin.Context, _ *auth.User, _ *Instance) {
	perPage, err := pagination.PerPage(c.Query("per_page"), 10, 10)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	cursor := pagination.OffsetCursor{Value: perPage}
	if raw := c.Query("cursor"); raw != "" {
		parsed, err := pagination.ParseOffsetCursor(raw)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"detail": "Invalid cursor parameter."})
			return
		}
		cursor = parsed
	}

	scope := func() *gorm.DB {
		query := handler.db.WithContext(c.Request.Context()).Table("workspaces w").
			Where("w.deleted_at IS NULL")
		if search := c.Query("search"); search != "" {
			query = query.Where("w.name ILIKE ?", "%"+search+"%")
		}
		return query
	}
	var total int64
	if err := scope().Count(&total).Error; err != nil {
		handler.internalError(c, err)
		return
	}
	window := pagination.PlanOffsetPage(perPage, cursor, int(total), 0, 10)

	var rows []instanceWorkspace
	err = scope().Select(`w.*,
		(SELECT COUNT(p.id) FROM projects p WHERE p.workspace_id = w.id AND p.deleted_at IS NULL) AS total_projects,
		(SELECT COUNT(wm.id) FROM workspace_members wm
			JOIN users u ON u.id = wm.member_id
			WHERE wm.workspace_id = w.id AND u.is_bot = FALSE AND wm.is_active = TRUE AND wm.deleted_at IS NULL) AS total_members`).
		Order("w.created_at DESC").Offset(window.Offset).Limit(window.Stop - window.Offset).Scan(&rows).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	page := pagination.PlanOffsetPage(perPage, cursor, int(total), len(rows), 10)
	if len(rows) > page.Limit {
		rows = rows[:page.Limit]
	}

	owners, err := handler.workspaceOwners(c, rows)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	results := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		results = append(results, workspaceJSON(row, owners))
	}
	handler.respond(c, http.StatusOK, page.Envelope(results, len(results), nil, nil, nil))
}

func (handler *Handler) workspaceOwners(c *gin.Context, rows []instanceWorkspace) (map[string]auth.User, error) {
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.OwnerID)
	}
	owners := map[string]auth.User{}
	if len(ids) == 0 {
		return owners, nil
	}
	var users []auth.User
	if err := handler.db.WithContext(c.Request.Context()).Where("id IN ?", ids).Find(&users).Error; err != nil {
		return nil, err
	}
	for _, person := range users {
		owners[person.ID] = person
	}
	return owners, nil
}

// workspaceJSON is the licence app's WorkspaceSerializer, whose owner is expanded and whose two counts are annotations.
func workspaceJSON(row instanceWorkspace, owners map[string]auth.User) gin.H {
	var owner any
	if person, found := owners[row.OwnerID]; found {
		owner = liteUserJSON(person)
	}
	logo := any(nil)
	if row.LogoAssetID != nil {
		logo = "/api/assets/v2/static/" + *row.LogoAssetID + "/"
	} else if row.Logo != nil && *row.Logo != "" {
		logo = *row.Logo
	}
	return gin.H{
		"id": row.ID, "owner": owner, "logo_url": logo,
		"total_projects": row.TotalProjects, "total_members": row.TotalMembers,
		"created_at": drf.Time(row.CreatedAt), "updated_at": drf.Time(row.UpdatedAt),
		"deleted_at": drf.At(row.DeletedAt),
		"name":       row.Name, "logo": row.Logo, "slug": row.Slug,
		"organization_size": row.OrganizationSize, "timezone": row.Timezone,
		"background_color": row.BackgroundColor,
		"created_by":       row.CreatedByID, "updated_by": row.UpdatedByID,
		"logo_asset": row.LogoAssetID,
	}
}

// liteUserJSON is UserLiteSerializer, which is the lite user without the email.
func liteUserJSON(user auth.User) gin.H {
	return gin.H{
		"id": user.ID, "first_name": user.FirstName, "last_name": user.LastName,
		"avatar": user.Avatar, "avatar_url": avatarURL(user), "is_bot": user.IsBot,
		"display_name": user.DisplayName,
	}
}

// workspaceCreate makes a workspace from the console, owned by whoever made it.
//
// The two length checks come before the serializer, so a name of eighty-one characters is refused with one message and a name that is only punctuation with another.
func (handler *Handler) workspaceCreate(c *gin.Context, user *auth.User, _ *Instance) {
	payload := map[string]any{}
	_ = c.ShouldBindJSON(&payload)
	name, _ := payload["name"].(string)
	slug, _ := payload["slug"].(string)
	companyRole, _ := payload["company_role"].(string)

	if name == "" || slug == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Both name and slug are required"})
		return
	}
	if len([]rune(name)) > 80 || len([]rune(slug)) > 48 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "The maximum length for name is 80 and for slug is 48"})
		return
	}
	if workspace.ContainsURL(name) {
		c.JSON(http.StatusBadRequest, gin.H{"name": []string{"Name must not contain URLs"}})
		return
	}
	if !workspace.HasAlphanumeric(name) {
		c.JSON(http.StatusBadRequest, gin.H{"name": []string{"Name must contain at least one letter or number"}})
		return
	}
	if workspace.RestrictedSlug(slug) {
		c.JSON(http.StatusBadRequest, gin.H{"slug": []string{"Slug is not valid"}})
		return
	}
	var taken int64
	err := handler.db.WithContext(c.Request.Context()).Table("workspaces").
		Where("LOWER(slug) = LOWER(?) AND deleted_at IS NULL", slug).Count(&taken).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if taken > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"slug": []string{"Slug is already in use"}})
		return
	}

	now := handler.clock().UTC()
	row := instanceWorkspace{
		ID: uuid.NewString(), CreatedAt: now, UpdatedAt: now,
		Name: name, Slug: slug, OwnerID: user.ID, Timezone: "UTC",
		BackgroundColor: workspace.RandomHexColor(),
	}
	err = handler.db.WithContext(c.Request.Context()).Table("workspaces").Create(map[string]any{
		"id": row.ID, "created_at": now, "updated_at": now,
		"created_by_id": nil, "updated_by_id": nil,
		"name": name, "slug": slug, "owner_id": user.ID,
		"organization_size": nil, "timezone": "UTC", "background_color": row.BackgroundColor,
	}).Error
	if err != nil {
		// A slug taken between the check and the insert is what the unique index refuses, and the view answers 409 for it rather than 400.
		if strings.Contains(strings.ToLower(err.Error()), "already exists") || strings.Contains(strings.ToLower(err.Error()), "duplicate key") {
			c.JSON(http.StatusConflict, gin.H{"slug": "The workspace with the slug already exists"})
			return
		}
		handler.internalError(c, err)
		return
	}
	err = handler.db.WithContext(c.Request.Context()).Table("workspace_members").Create(map[string]any{
		"id": uuid.NewString(), "created_at": now, "updated_at": now,
		"created_by_id": nil, "updated_by_id": nil,
		"workspace_id": row.ID, "member_id": user.ID, "role": 20,
		"company_role": companyRole, "is_active": true,
		"view_props": "{}", "default_props": "{}", "issue_props": "{}",
		"getting_started_checklist": "{}", "tips": "{}", "explored_features": "{}",
	}).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}

	// The response is the serializer's own data, so the two counts are absent rather than zero — they are annotations the create path never adds.
	created := workspaceJSON(row, map[string]auth.User{user.ID: *user})
	delete(created, "total_projects")
	delete(created, "total_members")
	handler.respond(c, http.StatusCreated, created)
}
