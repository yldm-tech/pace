package externalapi

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api/internal/access"
	"github.com/yldm-tech/pace/apps/api/internal/auth"
	"github.com/yldm-tech/pace/apps/api/internal/drf"
	"gorm.io/gorm"
)

func (handler *Handler) registerMemberRoutes(router gin.IRouter) {
	router.GET("/api/v1/workspaces/:slug/members/", handler.authenticated(handler.workspaceMemberList))
	router.GET("/api/v1/workspaces/:slug/members-lite/", handler.authenticated(handler.workspaceMemberLiteList))
	project := "/api/v1/workspaces/:slug/projects/:project/"
	router.GET(project+"project-members-lite/", handler.authenticated(handler.projectMemberLiteList))
	// The same two endpoints are mounted under both names, and every method is bound on each. Serving only one of the pair would leave half the integrations on Django.
	for _, name := range []string{"members", "project-members"} {
		router.GET(project+name+"/", handler.authenticated(handler.projectMemberList))
		router.POST(project+name+"/", handler.authenticated(handler.projectMemberCreate))
		router.GET(project+name+"/:member/", handler.authenticated(handler.projectMemberRetrieve))
		router.PATCH(project+name+"/:member/", handler.authenticated(handler.projectMemberUpdate))
		router.DELETE(project+name+"/:member/", handler.authenticated(handler.projectMemberDestroy))
	}
}

// memberRow is a membership joined to the person behind it.
type memberRow struct {
	MembershipID string  `gorm:"column:membership_id"`
	Role         int     `gorm:"column:role"`
	IsActive     bool    `gorm:"column:is_active"`
	UserID       string  `gorm:"column:user_id"`
	FirstName    string  `gorm:"column:first_name"`
	LastName     string  `gorm:"column:last_name"`
	Email        string  `gorm:"column:email"`
	Avatar       string  `gorm:"column:avatar"`
	AvatarAsset  *string `gorm:"column:avatar_asset_id"`
	DisplayName  string  `gorm:"column:display_name"`
	IsBot        bool    `gorm:"column:is_bot"`
}

// memberSelection is the join both member lists read through.
const memberSelection = `m.id AS membership_id, m.role, m.is_active,
	u.id AS user_id, u.first_name, u.last_name, u.email, u.avatar, u.avatar_asset_id,
	u.display_name, u.is_bot, m.created_at`

// workspaceMemberList returns every member of a workspace with their role.
//
// It answers a **400** for a workspace that is not there, not a 404, which is the one place in this app that mixes the two up. Its sibling the lite list answers 404 for the same thing.
func (handler *Handler) workspaceMemberList(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireWorkspaceAdmin(c, user) {
		return
	}
	exists, err := handler.workspaceExists(c)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !exists {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Provided workspace does not exist"})
		return
	}
	var rows []memberRow
	err = handler.workspaceMemberScope(c).Select(memberSelection).Scan(&rows).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	// This list is not paginated and not lite: it is the full user serializer with the role appended.
	results := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		data := memberUserJSON(row)
		data["role"] = row.Role
		results = append(results, data)
	}
	drf.Respond(c, http.StatusOK, results)
}

// workspaceMemberLiteList is the paginated picker, flattening the membership and the person into one row.
func (handler *Handler) workspaceMemberLiteList(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireWorkspaceAdmin(c, user) {
		return
	}
	exists, err := handler.workspaceExists(c)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !exists {
		notFoundWithMessage(c, "Provided workspace does not exist")
		return
	}
	var rows []memberRow
	err = handler.workspaceMemberScope(c).Select(memberSelection).Order("m.created_at DESC").Scan(&rows).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	results := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		results = append(results, memberLiteJSON(row))
	}
	handler.respondPaged(c, results)
}

// projectMemberList returns the people in a project.
//
// It reads the **users** rather than the memberships, so it carries no role and no active flag — and it does not filter the inactive out either, so somebody removed from the project is still listed.
func (handler *Handler) projectMemberList(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireProjectMember(c, user, http.MethodGet) {
		return
	}
	exists, err := handler.workspaceExists(c)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !exists {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Provided workspace does not exist"})
		return
	}
	var rows []memberRow
	err = handler.projectMemberScope(c).Select(memberSelection).Scan(&rows).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	results := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		results = append(results, memberUserJSON(row))
	}
	drf.Respond(c, http.StatusOK, results)
}

// projectMemberLiteList is the project picker, which unlike its list sibling does check that the project exists.
func (handler *Handler) projectMemberLiteList(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireProjectMember(c, user, http.MethodGet) {
		return
	}
	exists, err := handler.workspaceExists(c)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !exists {
		notFoundWithMessage(c, "Provided workspace does not exist")
		return
	}
	var projects int64
	err = handler.db.WithContext(c.Request.Context()).Table("projects p").
		Joins("JOIN workspaces w ON w.id = p.workspace_id").
		Where("w.slug = ? AND p.id = ?", c.Param("slug"), c.Param("project")).Count(&projects).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if projects == 0 {
		notFoundWithMessage(c, "Provided project does not exist")
		return
	}
	var rows []memberRow
	err = handler.projectMemberScope(c).Select(memberSelection).Order("m.created_at DESC").Scan(&rows).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	results := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		results = append(results, memberLiteJSON(row))
	}
	handler.respondPaged(c, results)
}

// projectMemberRetrieve answers with the **person**, not the membership, so the role it was looked up by does not appear in the body.
func (handler *Handler) projectMemberRetrieve(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireProjectMember(c, user, http.MethodGet) {
		return
	}
	exists, err := handler.workspaceExists(c)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !exists {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Provided workspace does not exist"})
		return
	}
	var rows []memberRow
	err = handler.projectMemberScope(c).Where("m.id = ?", c.Param("member")).
		Select(memberSelection).Limit(1).Scan(&rows).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if len(rows) == 0 {
		notFound(c)
		return
	}
	drf.Respond(c, http.StatusOK, memberUserJSON(rows[0]))
}

// projectMemberCreate adds somebody to a project.
//
// The person has to be in the **workspace** already — this route does not invite, it only grants. And the role is checked against the three the model names, so a number outside them is refused rather than stored.
func (handler *Handler) projectMemberCreate(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireProjectAdmin(c, user) {
		return
	}
	slug, projectID := c.Param("slug"), c.Param("project")
	var payload struct {
		Member *string `json:"member"`
		Role   *int    `json:"role"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		invalidPayload(c)
		return
	}
	if payload.Member == nil || *payload.Member == "" {
		c.JSON(http.StatusBadRequest, gin.H{"member": []string{"This field is required."}})
		return
	}
	if payload.Role != nil && !validMemberRole(*payload.Role) {
		c.JSON(http.StatusBadRequest, gin.H{"role": []string{"Invalid role"}})
		return
	}
	var members int64
	err := handler.db.WithContext(c.Request.Context()).Table("workspace_members wm").
		Joins("JOIN workspaces w ON w.id = wm.workspace_id").
		Where("w.slug = ? AND wm.member_id = ?", slug, *payload.Member).Count(&members).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if members == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"member": []string{"Member not found in workspace"}})
		return
	}

	var workspaceIDs []string
	err = handler.db.WithContext(c.Request.Context()).Table("workspaces").
		Where("slug = ?", slug).Limit(1).Pluck("id", &workspaceIDs).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if len(workspaceIDs) == 0 {
		notFound(c)
		return
	}
	identifier, err := newUUID()
	if err != nil {
		handler.serverError(c, err)
		return
	}
	role := roleMember
	if payload.Role != nil {
		role = *payload.Role
	}
	now := handler.clock().UTC()
	membership := map[string]any{
		"id": identifier, "created_at": now, "updated_at": now,
		"created_by_id": user.ID, "updated_by_id": user.ID,
		"project_id": projectID, "workspace_id": workspaceIDs[0],
		"member_id": *payload.Member, "role": role, "is_active": true, "sort_order": 65535,
	}
	err = handler.db.WithContext(c.Request.Context()).Table("project_members").Create(membership).Error
	if err != nil {
		if isUniqueViolation(err) {
			invalidPayload(c)
			return
		}
		handler.serverError(c, err)
		return
	}
	drf.Respond(c, http.StatusCreated, gin.H{"id": identifier, "member": *payload.Member, "role": role})
}

// projectMemberUpdate changes somebody's role. Three fields exist on the serializer and only the role is writable.
func (handler *Handler) projectMemberUpdate(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireProjectAdmin(c, user) {
		return
	}
	var payload struct {
		Role *int `json:"role"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		invalidPayload(c)
		return
	}
	if payload.Role != nil && !validMemberRole(*payload.Role) {
		c.JSON(http.StatusBadRequest, gin.H{"role": []string{"Invalid role"}})
		return
	}
	var rows []memberRow
	err := handler.projectMemberScope(c).Where("m.id = ?", c.Param("member")).
		Select(memberSelection).Limit(1).Scan(&rows).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if len(rows) == 0 {
		notFound(c)
		return
	}
	role := rows[0].Role
	if payload.Role != nil {
		role = *payload.Role
	}
	now := handler.clock().UTC()
	err = handler.db.WithContext(c.Request.Context()).Table("project_members").
		Where("id = ?", rows[0].MembershipID).
		Updates(map[string]any{"role": role, "updated_at": now, "updated_by_id": user.ID}).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, gin.H{
		"id": rows[0].MembershipID, "member": rows[0].UserID, "role": role,
	})
}

// projectMemberDestroy takes somebody out of a project by switching the membership off rather than removing it, so their history stays attributable.
func (handler *Handler) projectMemberDestroy(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireProjectAdmin(c, user) {
		return
	}
	var rows []memberRow
	err := handler.projectMemberScope(c).Where("m.id = ?", c.Param("member")).
		Select(memberSelection).Limit(1).Scan(&rows).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if len(rows) == 0 {
		notFound(c)
		return
	}
	now := handler.clock().UTC()
	err = handler.db.WithContext(c.Request.Context()).Table("project_members").
		Where("id = ?", rows[0].MembershipID).
		Updates(map[string]any{"is_active": false, "updated_at": now, "updated_by_id": user.ID}).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// validMemberRole is the three the serializer accepts. A role outside them is refused rather than stored.
func validMemberRole(role int) bool {
	return role == roleAdmin || role == roleMember || role == roleGuest
}

// requireProjectAdmin is ProjectAdminPermission, which the three write routes swap in for the read one.
func (handler *Handler) requireProjectAdmin(c *gin.Context, user *auth.User) bool {
	role, member, err := access.ProjectRole(c.Request.Context(), handler.db, c.Param("slug"), c.Param("project"), user.ID)
	if err != nil {
		handler.serverError(c, err)
		return false
	}
	if !member || role != roleAdmin {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
			"detail": "You do not have permission to perform this action.",
		})
		return false
	}
	return true
}

func (handler *Handler) workspaceExists(c *gin.Context) (bool, error) {
	var count int64
	err := handler.db.WithContext(c.Request.Context()).Table("workspaces").
		Where("slug = ?", c.Param("slug")).Count(&count).Error
	return count > 0, err
}

// workspaceMemberScope is every membership of the workspace, active or not.
func (handler *Handler) workspaceMemberScope(c *gin.Context) *gorm.DB {
	return handler.db.WithContext(c.Request.Context()).Table("workspace_members m").
		Joins("JOIN workspaces w ON w.id = m.workspace_id").
		Joins("JOIN users u ON u.id = m.member_id").
		Where("w.slug = ?", c.Param("slug"))
}

// projectMemberScope is every membership of the project, active or not — the inactive are listed too, which is what makes somebody removed from a project still appear.
func (handler *Handler) projectMemberScope(c *gin.Context) *gorm.DB {
	return handler.db.WithContext(c.Request.Context()).Table("project_members m").
		Joins("JOIN workspaces w ON w.id = m.workspace_id").
		Joins("JOIN users u ON u.id = m.member_id").
		Where("w.slug = ? AND m.project_id = ?", c.Param("slug"), c.Param("project"))
}

// notFoundWithMessage is the 404 the two lite lists answer with, which carries its own message rather than the base view's.
func notFoundWithMessage(c *gin.Context, message string) {
	c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": message})
}

// memberUserJSON is the external API's UserLiteSerializer over a joined row.
func memberUserJSON(row memberRow) gin.H {
	return gin.H{
		"id": row.UserID, "first_name": row.FirstName, "last_name": row.LastName,
		"email": row.Email, "avatar": row.Avatar, "avatar_url": memberAvatarURL(row),
		"display_name": row.DisplayName,
	}
}

// memberLiteJSON is the flattened picker row: the person's fields with the membership's role and active flag beside them.
func memberLiteJSON(row memberRow) gin.H {
	return gin.H{
		"id": row.UserID, "first_name": row.FirstName, "last_name": row.LastName,
		"email": row.Email, "avatar": row.Avatar, "avatar_url": memberAvatarURL(row),
		"display_name": row.DisplayName, "role": row.Role, "is_active": row.IsActive, "is_bot": row.IsBot,
	}
}

func memberAvatarURL(row memberRow) any {
	if row.AvatarAsset != nil {
		return "/api/assets/v2/static/" + *row.AvatarAsset + "/"
	}
	if row.Avatar != "" {
		return row.Avatar
	}
	return nil
}
