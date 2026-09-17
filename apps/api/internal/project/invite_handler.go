package project

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api/internal/auth"
	"github.com/yldm-tech/pace/apps/api/internal/drf"
	"github.com/yldm-tech/pace/apps/api/internal/projects"
	"gorm.io/gorm"
)

func (handler *Handler) registerInviteRoutes(router gin.IRouter) {
	const base = "/api/workspaces/:slug/projects/:id/invitations/"
	router.GET(base, handler.authenticated(handler.projectInviteList))
	router.POST(base, handler.authenticated(handler.projectInviteCreate))
	router.GET(base+":invite/", handler.authenticated(handler.projectInviteRetrieve))
	router.DELETE(base+":invite/", handler.authenticated(handler.projectInviteDestroy))
	// The join routes carry no session of their own: the token in the payload is what stands in for one, and the accept then asks for a session as well.
	router.GET("/api/workspaces/:slug/projects/:id/join/:invite/", handler.projectInvitePublic)
	router.POST("/api/workspaces/:slug/projects/:id/join/:invite/", handler.projectInviteRespond)
	router.GET("/api/users/me/workspaces/:slug/projects/invitations/", handler.authenticated(handler.myProjectInvites))
	router.POST("/api/users/me/workspaces/:slug/projects/invitations/", handler.authenticated(handler.joinProjects))
}

// ProjectMemberInvite is an invitation to one project.
type ProjectMemberInvite struct {
	ID          string     `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt   time.Time  `gorm:"column:created_at"`
	UpdatedAt   time.Time  `gorm:"column:updated_at"`
	CreatedByID *string    `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID *string    `gorm:"column:updated_by_id;type:uuid"`
	DeletedAt   *time.Time `gorm:"column:deleted_at"`
	ProjectID   string     `gorm:"column:project_id;type:uuid"`
	WorkspaceID string     `gorm:"column:workspace_id;type:uuid"`
	Email       string     `gorm:"column:email"`
	Accepted    bool       `gorm:"column:accepted"`
	Token       string     `gorm:"column:token"`
	Message     *string    `gorm:"column:message"`
	RespondedAt *time.Time `gorm:"column:responded_at"`
	Role        int        `gorm:"column:role"`
}

func (ProjectMemberInvite) TableName() string { return "project_member_invites" }

// projectInviteList returns a project's outstanding invitations.
//
// Django asks only that the caller be signed in — the viewset carries no permission class and its queryset is scoped to the project alone, so any account could read any project's invitations. The port asks for an active membership of the project instead, which is what every other project-scoped read here asks and what the workspace's own invitation list already asks.
func (handler *Handler) projectInviteList(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	var invites []ProjectMemberInvite
	err := handler.db.WithContext(c.Request.Context()).Table("project_member_invites i").Select("i.*").
		Joins("JOIN workspaces w ON w.id = i.workspace_id").
		Where("w.slug = ? AND i.project_id = ? AND i.deleted_at IS NULL", c.Param("slug"), c.Param("id")).
		Order("i.created_at DESC").Scan(&invites).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	results, err := handler.projectInviteBodies(c, invites)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, results)
}

func (handler *Handler) projectInviteRetrieve(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	invite, found, err := handler.projectInviteByID(c)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		handler.notFound(c)
		return
	}
	results, err := handler.projectInviteBodies(c, []ProjectMemberInvite{invite})
	if err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, results[0])
}

func (handler *Handler) projectInviteDestroy(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	invite, found, err := handler.projectInviteByID(c)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		handler.notFound(c)
		return
	}
	now := handler.clock().UTC()
	err = handler.db.WithContext(c.Request.Context()).Table("project_member_invites").Where("id = ?", invite.ID).
		Updates(map[string]any{"deleted_at": now, "updated_at": now}).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if handler.tasks != nil {
		err := handler.tasks.PublishSoftDeleteRelatedObjects(c.Request.Context(), "db", "projectmemberinvite", invite.ID)
		if err != nil {
			handler.internalError(c, err)
			return
		}
	}
	c.Status(http.StatusNoContent)
}

// projectInviteCreate has never sent an invitation.
//
// It reads `.role` off a **queryset** rather than off a row, which is an attribute error, so every call that names an email ends in a 500. The line after it is broken in the same way — the list of invitations it just built shadows the task it means to call — but nothing reaches that far. A call naming no emails is refused before either, which is the only answer this route gives that is not a 500.
func (handler *Handler) projectInviteCreate(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin) {
		return
	}
	var request struct {
		Emails []json.RawMessage `json:"emails"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		handler.invalidDetail(c)
		return
	}
	if len(request.Emails) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Emails are required"})
		return
	}
	handler.internalError(c, errInviteReadsRoleOffAQueryset)
}

var errInviteReadsRoleOffAQueryset = &inviteError{"the project invitation create reads role off a queryset, which is where it ends"}

type inviteError struct{ message string }

func (err *inviteError) Error() string { return err.message }

// projectInvitePublic is what the invitation email's link opens. It carries no session and reports only what an invitee needs to decide: the project, the workspace, the role and whether it has been answered — never the token or the email.
func (handler *Handler) projectInvitePublic(c *gin.Context) {
	invite, found, err := handler.projectInviteByParam(c, c.Param("invite"))
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		handler.notFound(c)
		return
	}
	project, err := handler.projectLiteJSON(c.Request.Context(), invite.ProjectID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	workspace, err := handler.workspaceLiteJSON(c.Request.Context(), invite.WorkspaceID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, gin.H{
		"id": invite.ID, "project": project, "workspace": workspace,
		"role": invite.Role, "message": invite.Message,
		"accepted": invite.Accepted, "responded_at": invite.RespondedAt,
	})
}

// projectInviteRespond accepts or declines an invitation.
//
// The token is checked first and the session second, so a caller with the right token and no session is told to sign in rather than that the token is wrong. The signed-in person then has to be the one the invitation names.
func (handler *Handler) projectInviteRespond(c *gin.Context) {
	invite, found, err := handler.projectInviteByParam(c, c.Param("invite"))
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		handler.notFound(c)
		return
	}
	var request struct {
		Token    string          `json:"token"`
		Accepted json.RawMessage `json:"accepted"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		handler.invalidDetail(c)
		return
	}
	if request.Token == "" || invite.Token != request.Token {
		c.JSON(http.StatusForbidden, gin.H{"error": "You do not have permission to join the project"})
		return
	}
	var user *auth.User
	if handler.sessions != nil {
		user, _, _ = handler.sessions.Authenticate(c.Request.Context(), c.Request, c.Writer)
	}
	if user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Authentication required to accept project invitation"})
		return
	}
	if !strings.EqualFold(user.Email, invite.Email) {
		c.JSON(http.StatusForbidden, gin.H{"error": "You do not have permission to accept this invitation"})
		return
	}
	if invite.RespondedAt != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "You have already responded to the invitation request"})
		return
	}

	accepted := false
	if len(request.Accepted) > 0 && string(request.Accepted) != "null" {
		if json.Unmarshal(request.Accepted, &accepted) != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "`accepted` must be a boolean"})
			return
		}
	}
	now := handler.clock().UTC()
	err = handler.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		err := tx.Table("project_member_invites").Where("id = ?", invite.ID).
			Updates(map[string]any{"accepted": accepted, "responded_at": now, "updated_at": now}).Error
		if err != nil || !accepted {
			return err
		}
		return handler.acceptProjectInvite(tx, invite, user.ID, now)
	})
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !accepted {
		drf.Respond(c, http.StatusOK, gin.H{"message": "Project Invitation was not accepted"})
		return
	}
	drf.Respond(c, http.StatusOK, gin.H{"message": "Project Invitation Accepted"})
}

// acceptProjectInvite puts the invitee into the workspace and then into the project.
//
// Two details are upstream's. A workspace membership made here is capped at **member** however high the project role is, so an invitation to administer a project does not hand out the workspace. And the project membership is looked up by workspace and member rather than by project, so somebody already in *another* project of the same workspace is reactivated there rather than added to this one.
func (handler *Handler) acceptProjectInvite(tx *gorm.DB, invite ProjectMemberInvite, userID string, now time.Time) error {
	var workspaceMembers []struct {
		ID string `gorm:"column:id"`
	}
	err := tx.Table("workspace_members wm").Select("wm.id").
		Joins("JOIN workspaces w ON w.id = wm.workspace_id").
		Where("w.slug = (SELECT slug FROM workspaces WHERE id = ?) AND wm.member_id = ?", invite.WorkspaceID, userID).
		Limit(1).Scan(&workspaceMembers).Error
	if err != nil {
		return err
	}
	if len(workspaceMembers) == 0 {
		memberID, err := newUUID()
		if err != nil {
			return err
		}
		role := invite.Role
		if role >= 15 {
			role = 15
		}
		err = tx.Table("workspace_members").Create(map[string]any{
			"id": memberID, "created_at": now, "updated_at": now, "created_by_id": userID,
			"workspace_id": invite.WorkspaceID, "member_id": userID, "role": role, "is_active": true,
			"view_props": projects.DefaultPropsJSON(), "default_props": projects.DefaultPropsJSON(),
			"issue_props": inviteIssuePropsJSON(), "getting_started_checklist": projects.EmptyJSON(),
			"tips": projects.EmptyJSON(), "explored_features": projects.EmptyJSON(),
		}).Error
		if err != nil {
			return err
		}
	} else {
		err := tx.Table("workspace_members").Where("id = ?", workspaceMembers[0].ID).
			Updates(map[string]any{"is_active": true, "updated_at": now}).Error
		if err != nil {
			return err
		}
	}

	var projectMembers []struct {
		ID string `gorm:"column:id"`
	}
	err = tx.Table("project_members").Select("id").
		Where("workspace_id = ? AND member_id = ?", invite.WorkspaceID, userID).
		Limit(1).Scan(&projectMembers).Error
	if err != nil {
		return err
	}
	if len(projectMembers) == 0 {
		return projects.AddMember(tx, invite.ProjectID, invite.WorkspaceID, userID, userID, invite.Role, now, newUUID)
	}
	// The role is written back as the role it already had, which changes nothing and is what the Python does.
	return tx.Table("project_members").Where("id = ?", projectMembers[0].ID).
		Updates(map[string]any{"is_active": true, "updated_at": now}).Error
}

// myProjectInvites lists the invitations addressed to the caller. It is not scoped to the workspace in the url, so it reports every project invitation the caller has anywhere.
func (handler *Handler) myProjectInvites(c *gin.Context, user *auth.User) {
	var invites []ProjectMemberInvite
	err := handler.db.WithContext(c.Request.Context()).Table("project_member_invites").
		Where("email = ? AND deleted_at IS NULL", user.Email).
		Order("created_at DESC").Scan(&invites).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	results, err := handler.projectInviteBodies(c, invites)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, results)
}

// joinProjects puts the caller into the projects they name, at the role they already hold in the workspace.
//
// A secret project may be joined only by a workspace admin. The ids are narrowed to the workspace **before** that check decides anything, so an id from another workspace is dropped rather than refused.
func (handler *Handler) joinProjects(c *gin.Context, user *auth.User) {
	var request struct {
		ProjectIDs []string `json:"project_ids"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		handler.invalidDetail(c)
		return
	}
	role, err := handler.workspaceRole(c.Request.Context(), c.Param("slug"), user.ID)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if role != roleAdmin && role != roleMember {
		c.JSON(http.StatusForbidden, gin.H{"detail": "You do not have permission to perform this action."})
		return
	}
	var workspaceIDs []string
	err = handler.db.WithContext(c.Request.Context()).Table("workspaces").
		Where("slug = ?", c.Param("slug")).Limit(1).Pluck("id", &workspaceIDs).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if len(workspaceIDs) == 0 {
		handler.notFound(c)
		return
	}
	type projectRow struct {
		ID      string `gorm:"column:id"`
		Network int    `gorm:"column:network"`
	}
	var rows []projectRow
	if len(request.ProjectIDs) > 0 {
		err := handler.db.WithContext(c.Request.Context()).Table("projects p").Select("p.id, p.network").
			Joins("JOIN workspaces w ON w.id = p.workspace_id").
			Where("w.slug = ? AND p.id IN ? AND p.deleted_at IS NULL", c.Param("slug"), request.ProjectIDs).
			Scan(&rows).Error
		if err != nil {
			handler.internalError(c, err)
			return
		}
	}
	for _, row := range rows {
		// A secret project is network 0, and only a workspace admin may join one.
		if row.Network == 0 && role != roleAdmin {
			c.JSON(http.StatusForbidden, gin.H{"error": "Only workspace admins can join private project"})
			return
		}
	}

	now := handler.clock().UTC()
	err = handler.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		for _, row := range rows {
			// Somebody who was in the project before is made active again rather than added twice.
			err := tx.Table("project_members").
				Where("project_id = ? AND member_id = ? AND workspace_id = ?", row.ID, user.ID, workspaceIDs[0]).
				Updates(map[string]any{"is_active": true, "updated_at": now}).Error
			if err != nil {
				return err
			}
			memberID, err := newUUID()
			if err != nil {
				return err
			}
			// bulk_create goes around save(), so the membership seeds no ordering of its own — the property is written beside it instead, with the column defaults.
			err = tx.Table("project_members").Clauses(onConflictDoNothing()).Create(map[string]any{
				"id": memberID, "created_at": now, "updated_at": now, "created_by_id": user.ID,
				"project_id": row.ID, "workspace_id": workspaceIDs[0], "member_id": user.ID,
				"role": role, "is_active": true, "sort_order": 65535,
				"view_props": projects.DefaultPropsJSON(), "default_props": projects.DefaultPropsJSON(),
				"preferences": projects.DefaultPreferencesJSON(),
			}).Error
			if err != nil {
				return err
			}
			propertyID, err := newUUID()
			if err != nil {
				return err
			}
			err = tx.Table("project_user_properties").Clauses(onConflictDoNothing()).Create(map[string]any{
				"id": propertyID, "created_at": now, "updated_at": now, "created_by_id": user.ID,
				"project_id": row.ID, "workspace_id": workspaceIDs[0], "user_id": user.ID,
				"filters": projects.DefaultFiltersJSON(), "display_filters": projects.DefaultDisplayFiltersJSON(),
				"display_properties": projects.DefaultDisplayPropertiesJSON(), "rich_filters": projects.EmptyJSON(),
				"preferences": projects.DefaultPreferencesJSON(), "sort_order": 65535,
			}).Error
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusCreated, gin.H{"message": "Projects joined successfully"})
}

// inviteIssuePropsJSON is the column default a workspace membership carries.
func inviteIssuePropsJSON() auth.JSONValue {
	value, _ := json.Marshal(map[string]bool{"subscribed": true, "assigned": true, "created": true, "all_issues": true})
	return auth.JSONValue(value)
}

func (handler *Handler) projectInviteByID(c *gin.Context) (ProjectMemberInvite, bool, error) {
	return handler.projectInviteByParam(c, c.Param("invite"))
}

func (handler *Handler) projectInviteByParam(c *gin.Context, identifier string) (ProjectMemberInvite, bool, error) {
	var invites []ProjectMemberInvite
	err := handler.db.WithContext(c.Request.Context()).Table("project_member_invites i").Select("i.*").
		Joins("JOIN workspaces w ON w.id = i.workspace_id").
		Where("w.slug = ? AND i.project_id = ? AND i.id = ? AND i.deleted_at IS NULL",
			c.Param("slug"), c.Param("id"), identifier).
		Limit(1).Scan(&invites).Error
	if err != nil || len(invites) == 0 {
		return ProjectMemberInvite{}, false, err
	}
	return invites[0], true, nil
}

// projectInviteBodies is ProjectMemberInviteSerializer, which nests the project and the workspace rather than naming them by id.
func (handler *Handler) projectInviteBodies(c *gin.Context, invites []ProjectMemberInvite) ([]gin.H, error) {
	results := make([]gin.H, 0, len(invites))
	for _, invite := range invites {
		project, err := handler.projectLiteJSON(c.Request.Context(), invite.ProjectID)
		if err != nil {
			return nil, err
		}
		workspace, err := handler.workspaceLiteJSON(c.Request.Context(), invite.WorkspaceID)
		if err != nil {
			return nil, err
		}
		results = append(results, gin.H{
			"id": invite.ID, "project": project, "workspace": workspace,
			"created_at": invite.CreatedAt, "updated_at": invite.UpdatedAt, "deleted_at": invite.DeletedAt,
			"email": invite.Email, "accepted": invite.Accepted, "token": invite.Token,
			"message": invite.Message, "responded_at": invite.RespondedAt, "role": invite.Role,
			"created_by": invite.CreatedByID, "updated_by": invite.UpdatedByID,
		})
	}
	return results, nil
}
