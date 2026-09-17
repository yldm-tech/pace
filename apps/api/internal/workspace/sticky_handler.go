package workspace

import (
	"encoding/json"
	"net/http"
	"regexp"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api/internal/auth"
	"github.com/yldm-tech/pace/apps/api/internal/drf"
	"github.com/yldm-tech/pace/apps/api/internal/pagination"
)

func (handler *Handler) registerStickyRoutes(router gin.IRouter) {
	const base = "/api/workspaces/:slug/stickies/"
	router.GET(base, handler.authenticated(handler.stickyList))
	router.POST(base, handler.authenticated(handler.stickyCreate))
	router.GET(base+":sticky/", handler.authenticated(handler.stickyRetrieve))
	router.PATCH(base+":sticky/", handler.authenticated(handler.stickyUpdate))
	router.DELETE(base+":sticky/", handler.authenticated(handler.stickyDestroy))
}

// Sticky is a note somebody keeps in a workspace, which nobody else can see.
type Sticky struct {
	ID                string     `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt         time.Time  `gorm:"column:created_at"`
	UpdatedAt         time.Time  `gorm:"column:updated_at"`
	CreatedByID       *string    `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID       *string    `gorm:"column:updated_by_id;type:uuid"`
	DeletedAt         *time.Time `gorm:"column:deleted_at"`
	WorkspaceID       string     `gorm:"column:workspace_id;type:uuid"`
	OwnerID           string     `gorm:"column:owner_id;type:uuid"`
	Name              *string    `gorm:"column:name"`
	Description       []byte     `gorm:"column:description;type:jsonb"`
	DescriptionHTML   string     `gorm:"column:description_html"`
	DescriptionStrpd  *string    `gorm:"column:description_stripped"`
	DescriptionBinary []byte     `gorm:"column:description_binary"`
	LogoProps         []byte     `gorm:"column:logo_props;type:jsonb"`
	Color             *string    `gorm:"column:color"`
	BackgroundColor   *string    `gorm:"column:background_color"`
	SortOrder         float64    `gorm:"column:sort_order"`
}

func (Sticky) TableName() string { return "stickies" }

// stickyList returns the caller's own notes, newest placement first, twenty to a page rather than the paginator's usual thousand.
//
// `query` searches the **stripped** text rather than the html, so a word that only appears inside a tag is not found.
func (handler *Handler) stickyList(c *gin.Context, user *auth.User) {
	if !handler.requireWorkspaceMember(c, user) {
		return
	}
	query := handler.db.WithContext(c.Request.Context()).Table("stickies s").Select("s.*").
		Joins("JOIN workspaces w ON w.id = s.workspace_id").
		Where("w.slug = ? AND s.owner_id = ? AND s.deleted_at IS NULL", c.Param("slug"), user.ID)
	if search := c.Query("query"); search != "" {
		query = query.Where("s.description_stripped ILIKE ?", "%"+escapeLike(search)+"%")
	}
	var rows []Sticky
	if err := query.Order("s.sort_order DESC").Scan(&rows).Error; err != nil {
		handler.internalError(c, err)
		return
	}
	results := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		results = append(results, stickyJSON(row))
	}

	// paginate(default_per_page=20) in plane/app/views/workspace/sticky.py, which leaves max_per_page at the paginator's own 1000. Passing twenty as the ceiling too refused the per_page=30 the sidebar asks for, so the stickies panel answered 400 on every workspace.
	const stickiesPerPage = 20
	perPage, err := pagination.PerPage(c.Query("per_page"), stickiesPerPage, pagination.DefaultPerPage)
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
	// The last argument is OffsetPaginator's max_limit, which is the module-level MAX_LIMIT of 1000 and not the route's page size.
	page := pagination.PlanOffsetPage(perPage, cursor, len(results), len(results), pagination.DefaultPerPage)
	offset := page.Offset
	if offset > len(results) {
		offset = len(results)
	}
	end := offset + page.Limit
	if end > len(results) {
		end = len(results)
	}
	window := results[offset:end]
	drf.Respond(c, http.StatusOK, page.Envelope(window, len(window), nil, nil, nil))
}

func (handler *Handler) stickyRetrieve(c *gin.Context, user *auth.User) {
	if !handler.requireWorkspaceMember(c, user) {
		return
	}
	sticky, found, err := handler.stickyByID(c, user)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		handler.notFound(c)
		return
	}
	drf.Respond(c, http.StatusOK, stickyJSON(sticky))
}

// stickyCreate makes a note. A new one is placed **after** every note the workspace already holds — not the caller's own, the whole workspace's — so two people's notes share one sequence.
func (handler *Handler) stickyCreate(c *gin.Context, user *auth.User) {
	if !handler.requireWorkspaceMember(c, user) {
		return
	}
	var payload map[string]json.RawMessage
	if err := c.ShouldBindJSON(&payload); err != nil {
		handler.invalidDetail(c)
		return
	}
	var workspaceIDs []string
	err := handler.db.WithContext(c.Request.Context()).Table("workspaces").
		Where("slug = ?", c.Param("slug")).Limit(1).Pluck("id", &workspaceIDs).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if len(workspaceIDs) == 0 {
		handler.notFound(c)
		return
	}
	var largest *float64
	err = handler.db.WithContext(c.Request.Context()).Table("stickies").
		Where("workspace_id = ? AND deleted_at IS NULL", workspaceIDs[0]).
		Select("MAX(sort_order)").Scan(&largest).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	sortOrder := 65535.0
	if largest != nil {
		sortOrder = *largest + 10000
	}

	identifier, err := newUUID()
	if err != nil {
		handler.internalError(c, err)
		return
	}
	now := handler.clock().UTC()
	sticky := Sticky{
		ID: identifier, CreatedAt: now, UpdatedAt: now, CreatedByID: &user.ID, UpdatedByID: &user.ID,
		WorkspaceID: workspaceIDs[0], OwnerID: user.ID, SortOrder: sortOrder,
		Description: emptyJSON(), LogoProps: emptyJSON(),
	}
	applyStickyFields(&sticky, payload)
	// The stripped copy follows the html on every save, and empty html leaves no copy at all rather than an empty one.
	sticky.DescriptionStrpd = strippedStickyText(sticky.DescriptionHTML)
	if err := handler.db.WithContext(c.Request.Context()).Create(&sticky).Error; err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusCreated, stickyJSON(sticky))
}

// stickyUpdate edits a note, and only the caller's own — the route carries no role at all, just the rule that the note is theirs.
func (handler *Handler) stickyUpdate(c *gin.Context, user *auth.User) {
	sticky, found, err := handler.stickyByID(c, user)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		handler.notFound(c)
		return
	}
	var payload map[string]json.RawMessage
	if err := c.ShouldBindJSON(&payload); err != nil {
		handler.invalidDetail(c)
		return
	}
	applyStickyFields(&sticky, payload)
	now := handler.clock().UTC()
	sticky.UpdatedAt = now
	sticky.UpdatedByID = &user.ID
	sticky.DescriptionStrpd = strippedStickyText(sticky.DescriptionHTML)
	err = handler.db.WithContext(c.Request.Context()).Table("stickies").Where("id = ?", sticky.ID).
		Updates(map[string]any{
			"name": sticky.Name, "description": auth.JSONValue(sticky.Description),
			"description_html": sticky.DescriptionHTML, "description_stripped": sticky.DescriptionStrpd,
			"logo_props": auth.JSONValue(sticky.LogoProps), "color": sticky.Color,
			"background_color": sticky.BackgroundColor, "sort_order": sticky.SortOrder,
			"updated_at": now, "updated_by_id": user.ID,
		}).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, stickyJSON(sticky))
}

func (handler *Handler) stickyDestroy(c *gin.Context, user *auth.User) {
	sticky, found, err := handler.stickyByID(c, user)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		handler.notFound(c)
		return
	}
	now := handler.clock().UTC()
	err = handler.db.WithContext(c.Request.Context()).Table("stickies").Where("id = ?", sticky.ID).
		Updates(map[string]any{"deleted_at": now, "updated_at": now}).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if handler.tasks != nil {
		if err := handler.tasks.PublishSoftDeleteRelatedObjects(c.Request.Context(), "db", "sticky", sticky.ID); err != nil {
			handler.internalError(c, err)
			return
		}
	}
	c.Status(http.StatusNoContent)
}

func (handler *Handler) stickyByID(c *gin.Context, user *auth.User) (Sticky, bool, error) {
	var rows []Sticky
	err := handler.db.WithContext(c.Request.Context()).Table("stickies s").Select("s.*").
		Joins("JOIN workspaces w ON w.id = s.workspace_id").
		Where("w.slug = ? AND s.owner_id = ? AND s.id = ? AND s.deleted_at IS NULL",
			c.Param("slug"), user.ID, c.Param("sticky")).
		Limit(1).Scan(&rows).Error
	if err != nil || len(rows) == 0 {
		return Sticky{}, false, err
	}
	return rows[0], true, nil
}

// applyStickyFields copies whatever the payload named onto the note, leaving the rest as it was.
func applyStickyFields(sticky *Sticky, payload map[string]json.RawMessage) {
	if raw, given := payload["name"]; given {
		var value string
		if json.Unmarshal(raw, &value) == nil {
			sticky.Name = &value
		}
	}
	if raw, given := payload["description"]; given && json.Valid(raw) {
		sticky.Description = append([]byte(nil), raw...)
	}
	if raw, given := payload["description_html"]; given {
		var value string
		if json.Unmarshal(raw, &value) == nil {
			sticky.DescriptionHTML = value
		}
	}
	if raw, given := payload["logo_props"]; given && json.Valid(raw) {
		sticky.LogoProps = append([]byte(nil), raw...)
	}
	for _, field := range []struct {
		name   string
		target **string
	}{{"color", &sticky.Color}, {"background_color", &sticky.BackgroundColor}} {
		raw, given := payload[field.name]
		if !given {
			continue
		}
		if string(raw) == "null" {
			*field.target = nil
			continue
		}
		var value string
		if json.Unmarshal(raw, &value) == nil {
			*field.target = &value
		}
	}
	if raw, given := payload["sort_order"]; given {
		var value float64
		if json.Unmarshal(raw, &value) == nil {
			sticky.SortOrder = value
		}
	}
}

var stickyTagPattern = regexp.MustCompile(`(?s)<[^>]*>`)

// strippedStickyText is Sticky.save's description_stripped: null when there is no html rather than an empty string.
func strippedStickyText(html string) *string {
	if html == "" {
		return nil
	}
	stripped := stickyTagPattern.ReplaceAllString(html, "")
	return &stripped
}

// escapeLike keeps a search term's own wildcards from reaching the pattern.
func escapeLike(value string) string {
	escaped := ""
	for _, character := range value {
		switch character {
		case '%', '_', '\\':
			escaped += "\\"
		}
		escaped += string(character)
	}
	return escaped
}

// stickyJSON is StickySerializer: seventeen fields, with the binary copy rendered as base64 and the stripped one as it is stored.
func stickyJSON(sticky Sticky) gin.H {
	return gin.H{
		"id": sticky.ID, "created_at": sticky.CreatedAt, "updated_at": sticky.UpdatedAt,
		"deleted_at": sticky.DeletedAt, "name": sticky.Name,
		"description":      decodeJSON(auth.JSONValue(sticky.Description)),
		"description_html": sticky.DescriptionHTML, "description_stripped": sticky.DescriptionStrpd,
		"description_binary": sticky.DescriptionBinary,
		"logo_props":         decodeJSON(auth.JSONValue(sticky.LogoProps)),
		"color":              sticky.Color, "background_color": sticky.BackgroundColor,
		"sort_order": sticky.SortOrder,
		"created_by": sticky.CreatedByID, "updated_by": sticky.UpdatedByID,
		"workspace": sticky.WorkspaceID, "owner": sticky.OwnerID,
	}
}
