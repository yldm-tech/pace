package project

import (
	"encoding/base64"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"github.com/yldm-tech/pace/apps/api-go/internal/drf"
	"github.com/yldm-tech/pace/apps/api-go/internal/pagination"
	"gorm.io/gorm"
)

func (handler *Handler) registerIssueVersionRoutes(router gin.IRouter) {
	router.GET("/api/workspaces/:slug/projects/:id/issues/:issue/versions/", handler.authenticatedIssueUUID(handler.issueVersionList))
	router.GET("/api/workspaces/:slug/projects/:id/issues/:issue/versions/:version/", handler.authenticatedIssueUUID(handler.issueVersionDetail))
	router.GET("/api/workspaces/:slug/projects/:id/work-items/:issue/description-versions/", handler.authenticatedIssueUUID(handler.descriptionVersionList))
	router.GET("/api/workspaces/:slug/projects/:id/work-items/:issue/description-versions/:version/", handler.authenticatedIssueUUID(handler.descriptionVersionDetail))
	// The intake copy of the same two routes. The detail is the same endpoint twice over; the list is not, because this one leaves the ordering off.
	router.GET("/api/workspaces/:slug/projects/:id/intake-work-items/:issue/description-versions/", handler.authenticatedIssueUUID(handler.intakeDescriptionVersionList))
	router.GET("/api/workspaces/:slug/projects/:id/intake-work-items/:issue/description-versions/:version/", handler.authenticatedIssueUUID(handler.descriptionVersionDetail))
}

// IssueVersion is the db.IssueVersion table: a snapshot of an issue's fields at a point in time.
type IssueVersion struct {
	ID             string         `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt      time.Time      `gorm:"column:created_at"`
	UpdatedAt      time.Time      `gorm:"column:updated_at"`
	CreatedByID    *string        `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID    *string        `gorm:"column:updated_by_id;type:uuid"`
	DeletedAt      *time.Time     `gorm:"column:deleted_at"`
	ProjectID      string         `gorm:"column:project_id;type:uuid"`
	WorkspaceID    string         `gorm:"column:workspace_id;type:uuid"`
	IssueID        string         `gorm:"column:issue_id;type:uuid"`
	ActivityID     *string        `gorm:"column:activity_id;type:uuid"`
	OwnedByID      string         `gorm:"column:owned_by_id;type:uuid"`
	Parent         *string        `gorm:"column:parent;type:uuid"`
	State          *string        `gorm:"column:state;type:uuid"`
	EstimatePoint  *string        `gorm:"column:estimate_point;type:uuid"`
	Name           string         `gorm:"column:name"`
	Priority       string         `gorm:"column:priority"`
	StartDate      *time.Time     `gorm:"column:start_date"`
	TargetDate     *time.Time     `gorm:"column:target_date"`
	Assignees      pq.StringArray `gorm:"column:assignees;type:uuid[]"`
	SequenceID     int            `gorm:"column:sequence_id"`
	Labels         pq.StringArray `gorm:"column:labels;type:uuid[]"`
	SortOrder      float64        `gorm:"column:sort_order"`
	CompletedAt    *time.Time     `gorm:"column:completed_at"`
	ArchivedAt     *time.Time     `gorm:"column:archived_at"`
	IsDraft        bool           `gorm:"column:is_draft"`
	ExternalSource *string        `gorm:"column:external_source"`
	ExternalID     *string        `gorm:"column:external_id"`
	Type           *string        `gorm:"column:type;type:uuid"`
	Cycle          *string        `gorm:"column:cycle;type:uuid"`
	Modules        pq.StringArray `gorm:"column:modules;type:uuid[]"`
	Properties     auth.JSONValue `gorm:"column:properties;type:jsonb"`
	Meta           auth.JSONValue `gorm:"column:meta;type:jsonb"`
	LastSavedAt    time.Time      `gorm:"column:last_saved_at"`
}

func (IssueVersion) TableName() string { return "issue_versions" }

// IssueDescriptionVersion is the db.IssueDescriptionVersion table, which holds the body separately from the rest of the snapshot.
type IssueDescriptionVersion struct {
	ID                  string         `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt           time.Time      `gorm:"column:created_at"`
	UpdatedAt           time.Time      `gorm:"column:updated_at"`
	CreatedByID         *string        `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID         *string        `gorm:"column:updated_by_id;type:uuid"`
	DeletedAt           *time.Time     `gorm:"column:deleted_at"`
	ProjectID           string         `gorm:"column:project_id;type:uuid"`
	WorkspaceID         string         `gorm:"column:workspace_id;type:uuid"`
	IssueID             string         `gorm:"column:issue_id;type:uuid"`
	OwnedByID           string         `gorm:"column:owned_by_id;type:uuid"`
	DescriptionBinary   []byte         `gorm:"column:description_binary"`
	DescriptionHTML     string         `gorm:"column:description_html"`
	DescriptionStripped *string        `gorm:"column:description_stripped"`
	DescriptionJSON     auth.JSONValue `gorm:"column:description_json;type:jsonb"`
	LastSavedAt         time.Time      `gorm:"column:last_saved_at"`
}

func (IssueDescriptionVersion) TableName() string { return "issue_description_versions" }

// versionListFields is the values() projection both list routes share. It is the same ten columns for either table, since a list is only ever used to pick a version to open.
func versionListJSON(id, workspaceID, projectID, issueID, ownedByID string, lastSavedAt, createdAt, updatedAt time.Time, createdBy, updatedBy *string, location *time.Location) gin.H {
	return gin.H{
		"id": id, "workspace": workspaceID, "project": projectID, "issue": issueID,
		"last_saved_at": lastSavedAt, "owned_by": ownedByID,
		// Only the two audit timestamps are moved into the caller's timezone; last_saved_at is not in the converter's list.
		"created_at": createdAt.In(location), "updated_at": updatedAt.In(location),
		"created_by": createdBy, "updated_by": updatedBy,
	}
}

func (handler *Handler) issueVersionList(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	location, err := userLocation(user)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	scope := func(query *gorm.DB) *gorm.DB {
		return query.Table("issue_versions v").
			Joins("JOIN workspaces w ON w.id = v.workspace_id").
			Where("v.issue_id = ? AND v.project_id = ? AND w.slug = ? AND v.deleted_at IS NULL",
				c.Param("issue"), c.Param("id"), c.Param("slug"))
	}
	page, ok := handler.planPage(c, scope)
	if !ok {
		return
	}
	var rows []IssueVersion
	if page.End > page.Start {
		// The queryset carries the model's own ordering, newest first.
		err := scope(handler.db.WithContext(c.Request.Context())).
			Order("v.created_at DESC").Offset(page.Start).Limit(page.End - page.Start).
			Find(&rows).Error
		if err != nil {
			handler.internalError(c, err)
			return
		}
	}
	results := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		results = append(results, versionListJSON(row.ID, row.WorkspaceID, row.ProjectID, row.IssueID,
			row.OwnedByID, row.LastSavedAt, row.CreatedAt, row.UpdatedAt, row.CreatedByID, row.UpdatedByID, location))
	}
	drf.Respond(c, http.StatusOK, page.Envelope(results, len(results)))
}

func (handler *Handler) issueVersionDetail(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	var version IssueVersion
	err := handler.db.WithContext(c.Request.Context()).Table("issue_versions v").
		Joins("JOIN workspaces w ON w.id = v.workspace_id").
		Where("v.id = ? AND v.issue_id = ? AND v.project_id = ? AND w.slug = ? AND v.deleted_at IS NULL",
			c.Param("version"), c.Param("issue"), c.Param("id"), c.Param("slug")).
		Take(&version).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "The required object does not exist."})
		return
	}
	if err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, issueVersionJSON(version))
}

// issueVersionJSON is IssueVersionDetailSerializer. Its field list names "name" twice and DRF silently dedupes, so the response carries thirty keys; properties and activity are not among them even though the model has both.
func issueVersionJSON(version IssueVersion) gin.H {
	return gin.H{
		"id": version.ID, "workspace": version.WorkspaceID, "project": version.ProjectID,
		"issue": version.IssueID, "parent": version.Parent, "state": version.State,
		"estimate_point": version.EstimatePoint, "name": version.Name, "priority": version.Priority,
		"start_date": dateOnly(version.StartDate), "target_date": dateOnly(version.TargetDate),
		"assignees": stringsOrEmpty(version.Assignees), "sequence_id": version.SequenceID,
		"labels": stringsOrEmpty(version.Labels), "sort_order": version.SortOrder,
		"completed_at": version.CompletedAt, "archived_at": dateOnly(version.ArchivedAt),
		"is_draft": version.IsDraft, "external_source": version.ExternalSource,
		"external_id": version.ExternalID, "type": version.Type, "cycle": version.Cycle,
		"modules": stringsOrEmpty(version.Modules), "meta": decodeJSON(version.Meta),
		"last_saved_at": version.LastSavedAt, "owned_by": version.OwnedByID,
		"created_at": version.CreatedAt, "updated_at": version.UpdatedAt,
		"created_by": version.CreatedByID, "updated_by": version.UpdatedByID,
	}
}

func (handler *Handler) descriptionVersionList(c *gin.Context, user *auth.User) {
	handler.serveDescriptionVersionList(c, user, true)
}

// intakeDescriptionVersionList is the same list read through the intake's own path, and it differs in one way: it applies no ordering at all.
//
// The work item copy sorts newest first. This one does not, and the model declares no ordering either, so what comes back is whatever order the database chose — which means a second page can repeat or skip a version. Reproduced rather than corrected: adding an order here would change what the endpoint returns.
func (handler *Handler) intakeDescriptionVersionList(c *gin.Context, user *auth.User) {
	handler.serveDescriptionVersionList(c, user, false)
}

func (handler *Handler) serveDescriptionVersionList(c *gin.Context, user *auth.User, ordered bool) {
	if !handler.requireDescriptionVersionAccess(c, user) {
		return
	}
	location, err := userLocation(user)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	scope := func(query *gorm.DB) *gorm.DB {
		return query.Table("issue_description_versions v").
			Joins("JOIN workspaces w ON w.id = v.workspace_id").
			Where("v.issue_id = ? AND v.project_id = ? AND w.slug = ? AND v.deleted_at IS NULL",
				c.Param("issue"), c.Param("id"), c.Param("slug"))
	}
	page, ok := handler.planPage(c, scope)
	if !ok {
		return
	}
	var rows []IssueDescriptionVersion
	if page.End > page.Start {
		query := scope(handler.db.WithContext(c.Request.Context()))
		if ordered {
			// The work item queryset orders explicitly rather than relying on the model, which declares none.
			query = query.Order("v.created_at DESC")
		}
		if err := query.Offset(page.Start).Limit(page.End - page.Start).Find(&rows).Error; err != nil {
			handler.internalError(c, err)
			return
		}
	}
	results := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		results = append(results, versionListJSON(row.ID, row.WorkspaceID, row.ProjectID, row.IssueID,
			row.OwnedByID, row.LastSavedAt, row.CreatedAt, row.UpdatedAt, row.CreatedByID, row.UpdatedByID, location))
	}
	drf.Respond(c, http.StatusOK, page.Envelope(results, len(results)))
}

func (handler *Handler) descriptionVersionDetail(c *gin.Context, user *auth.User) {
	if !handler.requireDescriptionVersionAccess(c, user) {
		return
	}
	var version IssueDescriptionVersion
	err := handler.db.WithContext(c.Request.Context()).Table("issue_description_versions v").
		Joins("JOIN workspaces w ON w.id = v.workspace_id").
		Where("v.id = ? AND v.issue_id = ? AND v.project_id = ? AND w.slug = ? AND v.deleted_at IS NULL",
			c.Param("version"), c.Param("issue"), c.Param("id"), c.Param("slug")).
		Take(&version).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "The required object does not exist."})
		return
	}
	if err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, gin.H{
		"id": version.ID, "workspace": version.WorkspaceID, "project": version.ProjectID,
		"issue": version.IssueID,
		// DRF renders a BinaryField as base64, and as null when the column is.
		"description_binary":   base64OrNil(version.DescriptionBinary),
		"description_html":     version.DescriptionHTML,
		"description_stripped": version.DescriptionStripped,
		"description_json":     decodeJSON(version.DescriptionJSON),
		"last_saved_at":        version.LastSavedAt, "owned_by": version.OwnedByID,
		"created_at": version.CreatedAt, "updated_at": version.UpdatedAt,
		"created_by": version.CreatedByID, "updated_by": version.UpdatedByID,
	})
}

func base64OrNil(value []byte) any {
	if value == nil {
		return nil
	}
	return base64.StdEncoding.EncodeToString(value)
}

// planPage reads the cursor and counts the rows behind it, which is what the paginator needs before it can slice.
func (handler *Handler) planPage(c *gin.Context, scope func(*gorm.DB) *gorm.DB) (pagination.Page, bool) {
	cursor := pagination.DefaultCursor()
	if raw := c.Query("cursor"); raw != "" {
		parsed, err := pagination.ParseCursor(raw)
		if err != nil {
			// Django's from_string raises ValueError, which BaseAPIView turns into this 400.
			c.JSON(http.StatusBadRequest, gin.H{"error": "Please provide valid detail"})
			return pagination.Page{}, false
		}
		cursor = parsed
	}
	var total int64
	if err := scope(handler.db.WithContext(c.Request.Context())).Count(&total).Error; err != nil {
		handler.internalError(c, err)
		return pagination.Page{}, false
	}
	page, err := pagination.Plan(cursor, int(total))
	if err != nil {
		// A page size of zero reaches a division Django does not guard.
		handler.internalError(c, err)
		return pagination.Page{}, false
	}
	return page, true
}

// requireDescriptionVersionAccess is the extra gate that route carries: a guest may only read the body history of an issue they raised, unless the project opens all features to guests.
func (handler *Handler) requireDescriptionVersionAccess(c *gin.Context, user *auth.User) bool {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember, roleGuest) {
		return false
	}
	member, found, err := handler.activeProjectMember(c.Request.Context(), c.Param("slug"), c.Param("id"), user.ID)
	if err != nil {
		handler.internalError(c, err)
		return false
	}
	if !found || member.Role != roleGuest {
		return true
	}
	var row struct {
		GuestViewAllFeatures bool    `gorm:"column:guest_view_all_features"`
		CreatedByID          *string `gorm:"column:created_by_id"`
	}
	err = handler.db.WithContext(c.Request.Context()).Table("issues i").
		Select("p.guest_view_all_features, i.created_by_id").
		Joins("JOIN projects p ON p.id = i.project_id").
		Joins("JOIN workspaces w ON w.id = i.workspace_id").
		Where("i.id = ? AND i.project_id = ? AND w.slug = ? AND i.deleted_at IS NULL",
			c.Param("issue"), c.Param("id"), c.Param("slug")).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// Django reads the issue with an unguarded .get before the check, so a missing issue is a 404 rather than a 403.
		c.JSON(http.StatusNotFound, gin.H{"error": "The required object does not exist."})
		return false
	}
	if err != nil {
		handler.internalError(c, err)
		return false
	}
	if row.GuestViewAllFeatures || (row.CreatedByID != nil && *row.CreatedByID == user.ID) {
		return true
	}
	c.JSON(http.StatusForbidden, gin.H{"error": "You are not allowed to view this issue"})
	return false
}
