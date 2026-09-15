package externalapi

import (
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"github.com/yldm-tech/pace/apps/api-go/internal/drf"
	"gorm.io/gorm"
)

func (handler *Handler) registerInviteRoutes(router gin.IRouter) {
	base := "/api/v1/workspaces/:slug/invitations/"
	router.GET(base, handler.authenticated(handler.inviteList))
	router.POST(base, handler.authenticated(handler.inviteCreate))
	router.GET(base+":invite/", handler.authenticated(handler.inviteRetrieve))
	router.PATCH(base+":invite/", handler.authenticated(handler.inviteUpdate))
	router.PUT(base+":invite/", handler.authenticated(handler.inviteReplace))
	router.DELETE(base+":invite/", handler.authenticated(handler.inviteDestroy))
}

// WorkspaceMemberInvite is the db.WorkspaceMemberInvite table: somebody asked to join who has not answered yet.
type WorkspaceMemberInvite struct {
	ID          string     `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt   time.Time  `gorm:"column:created_at"`
	UpdatedAt   time.Time  `gorm:"column:updated_at"`
	CreatedByID *string    `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID *string    `gorm:"column:updated_by_id;type:uuid"`
	DeletedAt   *time.Time `gorm:"column:deleted_at"`
	WorkspaceID string     `gorm:"column:workspace_id;type:uuid"`
	Email       string     `gorm:"column:email"`
	Accepted    bool       `gorm:"column:accepted"`
	Token       string     `gorm:"column:token"`
	Message     *string    `gorm:"column:message"`
	RespondedAt *time.Time `gorm:"column:responded_at"`
	Role        int        `gorm:"column:role"`
}

func (WorkspaceMemberInvite) TableName() string { return "workspace_member_invites" }

// inviteList returns the workspace's outstanding invitations.
//
// Unlike almost everything else in this API it is **not paginated**: the whole list comes back in one body.
func (handler *Handler) inviteList(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireWorkspaceOwner(c, user) {
		return
	}
	var invites []WorkspaceMemberInvite
	err := handler.inviteScope(c).Order("i.created_at DESC").Scan(&invites).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	results := make([]gin.H, 0, len(invites))
	for _, invite := range invites {
		results = append(results, inviteJSON(invite))
	}
	drf.Respond(c, http.StatusOK, results)
}

func (handler *Handler) inviteRetrieve(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireWorkspaceOwner(c, user) {
		return
	}
	invite, found, err := handler.inviteByID(c)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !found {
		notFound(c)
		return
	}
	drf.Respond(c, http.StatusOK, inviteJSON(invite))
}

// inviteCreate asks somebody to join.
//
// The duplicate check looks at the **workspace** rather than at the caller, and it does not care whether the earlier invite was answered — so once an address has been invited it cannot be invited again through this route, accepted or not.
func (handler *Handler) inviteCreate(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireWorkspaceOwner(c, user) {
		return
	}
	slug := c.Param("slug")
	var payload struct {
		Email   *string `json:"email"`
		Role    *int    `json:"role"`
		Message *string `json:"message"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		invalidPayload(c)
		return
	}
	if payload.Email == nil || strings.TrimSpace(*payload.Email) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"email": []string{"This field is required."}})
		return
	}
	if _, err := mail.ParseAddress(*payload.Email); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"email": []string{"Invalid email address"}})
		return
	}
	if payload.Role != nil && !validMemberRole(*payload.Role) {
		c.JSON(http.StatusBadRequest, gin.H{"role": []string{"Invalid role"}})
		return
	}

	var existing int64
	err := handler.db.WithContext(c.Request.Context()).Table("workspace_member_invites i").
		Joins("JOIN workspaces w ON w.id = i.workspace_id").
		Where("w.slug = ? AND i.email = ? AND i.deleted_at IS NULL", slug, *payload.Email).
		Count(&existing).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if existing > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"non_field_errors": []string{"Email already invited"}})
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
	token, err := newUUID()
	if err != nil {
		handler.serverError(c, err)
		return
	}
	now := handler.clock().UTC()
	role := roleGuest
	if payload.Role != nil {
		role = *payload.Role
	}
	invite := WorkspaceMemberInvite{
		ID: identifier, CreatedAt: now, UpdatedAt: now, CreatedByID: &user.ID, UpdatedByID: &user.ID,
		WorkspaceID: workspaceIDs[0], Email: *payload.Email, Role: role, Token: token, Message: payload.Message,
	}
	if err := handler.db.WithContext(c.Request.Context()).Create(&invite).Error; err != nil {
		if isUniqueViolation(err) {
			invalidPayload(c)
			return
		}
		handler.serverError(c, err)
		return
	}
	// This route only records the invitation; the email is somebody else's to send, which is why nothing is queued here.
	drf.Respond(c, http.StatusCreated, inviteJSON(invite))
}

// inviteUpdate changes an invitation's role. The address cannot be changed once the invitation exists, and naming it at all is refused rather than ignored.
func (handler *Handler) inviteUpdate(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireWorkspaceOwner(c, user) {
		return
	}
	var payload map[string]any
	if err := c.ShouldBindJSON(&payload); err != nil {
		invalidPayload(c)
		return
	}
	if value, present := payload["email"]; present && value != nil && value != "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Email cannot be updated after invite is created.", "code": "EMAIL_CANNOT_BE_UPDATED",
		})
		return
	}
	invite, found, err := handler.inviteByID(c)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !found {
		notFound(c)
		return
	}
	if value, ok := payload["role"].(float64); ok {
		if !validMemberRole(int(value)) {
			c.JSON(http.StatusBadRequest, gin.H{"role": []string{"Invalid role"}})
			return
		}
		invite.Role = int(value)
	}
	if value, present := payload["message"]; present {
		if text, ok := value.(string); ok {
			invite.Message = &text
		} else {
			invite.Message = nil
		}
	}
	now := handler.clock().UTC()
	invite.UpdatedAt, invite.UpdatedByID = now, &user.ID
	err = handler.db.WithContext(c.Request.Context()).Model(&WorkspaceMemberInvite{}).Where("id = ?", invite.ID).
		Updates(map[string]any{
			"role": invite.Role, "message": invite.Message,
			"updated_at": invite.UpdatedAt, "updated_by_id": invite.UpdatedByID,
		}).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, inviteJSON(invite))
}

// inviteDestroy withdraws an invitation, refusing one that has already been answered.
//
// The two refusals are separate and ordered: accepted is checked before responded, so an invitation that is both reports the first.
func (handler *Handler) inviteDestroy(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireWorkspaceOwner(c, user) {
		return
	}
	invite, found, err := handler.inviteByID(c)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !found {
		notFound(c)
		return
	}
	if invite.Accepted {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invite already accepted", "code": "INVITE_ALREADY_ACCEPTED"})
		return
	}
	if invite.RespondedAt != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invite already responded", "code": "INVITE_ALREADY_RESPONDED"})
		return
	}
	err = handler.db.WithContext(c.Request.Context()).Model(&WorkspaceMemberInvite{}).
		Where("id = ?", invite.ID).Update("deleted_at", handler.clock().UTC()).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// requireWorkspaceOwner is WorkspaceOwnerPermission: the person who owns the workspace, not merely an admin of it.
func (handler *Handler) requireWorkspaceOwner(c *gin.Context, user *auth.User) bool {
	var owned int64
	err := handler.db.WithContext(c.Request.Context()).Table("workspaces").
		Where("slug = ? AND owner_id = ?", c.Param("slug"), user.ID).Count(&owned).Error
	if err != nil {
		handler.serverError(c, err)
		return false
	}
	if owned == 0 {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
			"detail": "You do not have permission to perform this action.",
		})
		return false
	}
	return true
}

func (handler *Handler) inviteScope(c *gin.Context) *gorm.DB {
	return handler.db.WithContext(c.Request.Context()).Table("workspace_member_invites i").Select("i.*").
		Joins("JOIN workspaces w ON w.id = i.workspace_id").
		Where("w.slug = ? AND i.deleted_at IS NULL", c.Param("slug"))
}

func (handler *Handler) inviteByID(c *gin.Context) (WorkspaceMemberInvite, bool, error) {
	var invites []WorkspaceMemberInvite
	err := handler.inviteScope(c).Where("i.id = ?", c.Param("invite")).Limit(1).Scan(&invites).Error
	if err != nil || len(invites) == 0 {
		return WorkspaceMemberInvite{}, false, err
	}
	return invites[0], true, nil
}

// inviteJSON is WorkspaceInviteSerializer: seven fields, and the token is not among them.
func inviteJSON(invite WorkspaceMemberInvite) gin.H {
	return gin.H{
		"id": invite.ID, "email": invite.Email, "role": invite.Role,
		"created_at": invite.CreatedAt, "updated_at": invite.UpdatedAt,
		"responded_at": invite.RespondedAt, "accepted": invite.Accepted,
	}
}

// inviteReplace is the PUT the router binds, which is the generic update rather than the handler's own.
//
// It behaves very differently from the PATCH beside it, and almost always fails. The email is **required**, because a full update is not partial — and the serializer's validation then refuses any address already invited in this workspace, which includes the invitation's **own**. So the only PUT that succeeds is one naming an address nobody has been invited with, and it does the very thing the PATCH exists to forbid: it changes the address.
func (handler *Handler) inviteReplace(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireWorkspaceOwner(c, user) {
		return
	}
	var payload map[string]any
	if err := c.ShouldBindJSON(&payload); err != nil {
		invalidPayload(c)
		return
	}
	email, _ := payload["email"].(string)
	if strings.TrimSpace(email) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"email": []string{"This field is required."}})
		return
	}
	if _, err := mail.ParseAddress(email); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"email": []string{"Invalid email address"}})
		return
	}
	invite, found, err := handler.inviteByID(c)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !found {
		notFound(c)
		return
	}
	// The duplicate check does not exclude the invitation being replaced, so naming its own address refuses it.
	var existing int64
	err = handler.db.WithContext(c.Request.Context()).Table("workspace_member_invites i").
		Joins("JOIN workspaces w ON w.id = i.workspace_id").
		Where("w.slug = ? AND i.email = ? AND i.deleted_at IS NULL", c.Param("slug"), email).
		Count(&existing).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if existing > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"non_field_errors": []string{"Email already invited"}})
		return
	}
	if value, ok := payload["role"].(float64); ok {
		if !validMemberRole(int(value)) {
			c.JSON(http.StatusBadRequest, gin.H{"role": []string{"Invalid role"}})
			return
		}
		invite.Role = int(value)
	}
	invite.Email = email
	now := handler.clock().UTC()
	invite.UpdatedAt, invite.UpdatedByID = now, &user.ID
	err = handler.db.WithContext(c.Request.Context()).Model(&WorkspaceMemberInvite{}).Where("id = ?", invite.ID).
		Updates(map[string]any{
			"email": invite.Email, "role": invite.Role,
			"updated_at": invite.UpdatedAt, "updated_by_id": invite.UpdatedByID,
		}).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, inviteJSON(invite))
}
