package project

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api/internal/auth"
	"github.com/yldm-tech/pace/apps/api/internal/drf"
	"github.com/yldm-tech/pace/apps/api/internal/pagination"
	"gorm.io/gorm"
)

func (handler *Handler) registerNotificationRoutes(router gin.IRouter) {
	base := "/api/workspaces/:slug/users/notifications/"
	router.GET(base, handler.authenticated(handler.notificationList))
	router.GET(base+"unread/", handler.authenticated(handler.notificationUnreadCounts))
	router.POST(base+"mark-all-read/", handler.authenticated(handler.notificationMarkAllRead))
	router.GET(base+":notification/", handler.authenticated(handler.notificationRetrieve))
	router.PATCH(base+":notification/", handler.authenticated(handler.notificationUpdate))
	router.DELETE(base+":notification/", handler.authenticated(handler.notificationDestroy))
	router.POST(base+":notification/read/", handler.authenticated(handler.notificationMarkRead))
	router.DELETE(base+":notification/read/", handler.authenticated(handler.notificationMarkUnread))
	router.POST(base+":notification/archive/", handler.authenticated(handler.notificationArchive))
	router.DELETE(base+":notification/archive/", handler.authenticated(handler.notificationUnarchive))
}

// Notification is the db.Notification table, one row per thing a person was told about.
type Notification struct {
	ID               string     `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt        time.Time  `gorm:"column:created_at"`
	UpdatedAt        time.Time  `gorm:"column:updated_at"`
	CreatedByID      *string    `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID      *string    `gorm:"column:updated_by_id;type:uuid"`
	DeletedAt        *time.Time `gorm:"column:deleted_at"`
	WorkspaceID      string     `gorm:"column:workspace_id;type:uuid"`
	ProjectID        *string    `gorm:"column:project_id;type:uuid"`
	Data             []byte     `gorm:"column:data;type:jsonb"`
	EntityIdentifier *string    `gorm:"column:entity_identifier;type:uuid"`
	EntityName       string     `gorm:"column:entity_name"`
	Title            string     `gorm:"column:title"`
	Message          []byte     `gorm:"column:message;type:jsonb"`
	MessageHTML      string     `gorm:"column:message_html"`
	MessageStripped  *string    `gorm:"column:message_stripped"`
	Sender           string     `gorm:"column:sender"`
	TriggeredByID    *string    `gorm:"column:triggered_by_id;type:uuid"`
	ReceiverID       string     `gorm:"column:receiver_id;type:uuid"`
	ReadAt           *time.Time `gorm:"column:read_at"`
	SnoozedTill      *time.Time `gorm:"column:snoozed_till"`
	ArchivedAt       *time.Time `gorm:"column:archived_at"`
}

func (Notification) TableName() string { return "notifications" }

// notificationRow is the notification with the three flags the list annotates on.
type notificationRow struct {
	Notification
	IsIntakeIssue bool `gorm:"column:is_intake_issue"`
	IsMentioned   bool `gorm:"column:is_mentioned_notification"`
}

// notificationList returns the caller's notifications in this workspace.
//
// Only issue notifications appear at all — the queryset pins entity_name — and they are ordered by when they wake from snoozing before how recent they are.
func (handler *Handler) notificationList(c *gin.Context, user *auth.User) {
	if !handler.requireWorkspaceRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	slug := c.Param("slug")
	now := handler.clock().UTC()

	query, empty, err := handler.notificationScope(c, user, slug, now)
	if err != nil {
		if errors.Is(err, errUnknownNotificationFlag) {
			// Django indexes a two-key dictionary with whatever the parameter held, so a third value raises KeyError and answers 500.
			handler.internalError(c, err)
			return
		}
		handler.internalError(c, err)
		return
	}

	var rows []notificationRow
	if !empty {
		err = query.Select(notificationAnnotations(), slug).
			Order("n.snoozed_till, n.created_at DESC").Scan(&rows).Error
		if err != nil {
			handler.internalError(c, err)
			return
		}
	}

	actors, err := handler.notificationActors(c, rows)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	results := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		results = append(results, notificationJSON(row, true, actors[actorKey(row.TriggeredByID)]))
	}
	// The list is paged only when the caller asks for both halves of a cursor; naming one of them alone returns everything.
	if c.Query("per_page") == "" || c.Query("cursor") == "" {
		drf.Respond(c, http.StatusOK, results)
		return
	}
	handler.respondPagedNotifications(c, results)
}

// errUnknownNotificationFlag is the KeyError Django raises for a snoozed or archived parameter that is neither "true" nor "false".
var errUnknownNotificationFlag = errors.New("notifications: the snoozed and archived parameters are indexed into a two-key dictionary")

// notificationScope builds the list's queryset, reporting separately when the result is the empty one a guest asking for created issues gets.
func (handler *Handler) notificationScope(c *gin.Context, user *auth.User, slug string, now time.Time) (*gorm.DB, bool, error) {
	query := handler.db.WithContext(c.Request.Context()).Table("notifications n").
		Joins("JOIN workspaces w ON w.id = n.workspace_id").
		Where("w.slug = ? AND n.receiver_id = ? AND n.deleted_at IS NULL AND n.entity_name = 'issue'", slug, user.ID)

	switch c.DefaultQuery("snoozed", "false") {
	case "true":
		// The first half of the pair is subsumed by the second, so this is simply "is snoozed at all".
		query = query.Where("n.snoozed_till < ? OR n.snoozed_till IS NOT NULL", now)
	case "false":
		query = query.Where("n.snoozed_till >= ? OR n.snoozed_till IS NULL", now)
	default:
		return nil, false, errUnknownNotificationFlag
	}

	switch c.DefaultQuery("archived", "false") {
	case "true":
		query = query.Where("n.archived_at IS NOT NULL")
	case "false":
		query = query.Where("n.archived_at IS NULL")
	default:
		return nil, false, errUnknownNotificationFlag
	}

	switch c.Query("read") {
	case "false":
		query = query.Where("n.read_at IS NULL")
	case "true":
		query = query.Where("n.read_at IS NOT NULL")
	}

	// Any value at all turns this on, because Python reads the parameter as a string and every non-empty string is true. Asking for mentioned=false asks for the mentions.
	if _, asked := c.GetQuery("mentioned"); asked && c.Query("mentioned") != "" {
		query = query.Where("n.sender ILIKE '%mentioned%'")
	} else {
		query = query.Where("n.sender NOT ILIKE '%mentioned%'")
	}

	return handler.narrowNotificationsByType(c, user, slug, query)
}

// narrowNotificationsByType applies the type parameter, which is a comma-separated set rather than one choice: the matching sets are unioned, and naming none of them narrows nothing.
func (handler *Handler) narrowNotificationsByType(c *gin.Context, user *auth.User, slug string, query *gorm.DB) (*gorm.DB, bool, error) {
	types := strings.Split(c.DefaultQuery("type", "all"), ",")
	conditions := []string{}
	arguments := []any{}

	if containsString(types, "subscribed") {
		// A subscription counts only while the person neither made the issue nor was given it.
		conditions = append(conditions, `n.entity_identifier IN (
			SELECT s.issue_id FROM issue_subscribers s
			JOIN workspaces sw ON sw.id = s.workspace_id
			WHERE sw.slug = ? AND s.subscriber_id = ? AND s.deleted_at IS NULL
			AND NOT EXISTS (SELECT 1 FROM issues si WHERE si.id = s.issue_id AND si.created_by_id = ?)
			AND NOT EXISTS (SELECT 1 FROM issue_assignees sa WHERE sa.id = s.issue_id AND sa.assignee_id = ?))`)
		arguments = append(arguments, slug, user.ID, user.ID, user.ID)
	}
	if containsString(types, "assigned") {
		conditions = append(conditions, `n.entity_identifier IN (
			SELECT a.issue_id FROM issue_assignees a
			JOIN workspaces aw ON aw.id = a.workspace_id
			WHERE aw.slug = ? AND a.assignee_id = ? AND a.deleted_at IS NULL)`)
		arguments = append(arguments, slug, user.ID)
	}
	if containsString(types, "created") {
		role, _, err := handler.workspaceMemberRole(c.Request.Context(), slug, user.ID)
		if err != nil {
			return nil, false, err
		}
		if role < roleMember {
			// A guest asking about issues they created gets nothing at all, and the other types they asked for go with it.
			return query, true, nil
		}
		conditions = append(conditions, `n.entity_identifier IN (
			SELECT i.id FROM issues i
			JOIN workspaces iw ON iw.id = i.workspace_id
			WHERE iw.slug = ? AND i.created_by_id = ? AND i.deleted_at IS NULL)`)
		arguments = append(arguments, slug, user.ID)
	}
	if len(conditions) == 0 {
		return query, false, nil
	}
	return query.Where(strings.Join(conditions, " OR "), arguments...), false, nil
}

// notificationAnnotations is the select the list builds. Two of the three flags are the same subquery under different names, which is upstream's doing and is kept.
func notificationAnnotations() string {
	return `n.*,
		EXISTS (SELECT 1 FROM issues ni
			JOIN workspaces nw ON nw.id = ni.workspace_id
			JOIN intake_issues nii ON nii.issue_id = ni.id AND nii.status IN (0, 2, -2) AND nii.deleted_at IS NULL
			WHERE ni.id = n.entity_identifier AND nw.slug = ? AND ni.deleted_at IS NULL) AS is_intake_issue,
		(n.sender ILIKE '%mentioned%') AS is_mentioned_notification`
}

// respondPagedNotifications wraps an already-serialized list in the offset paginator's envelope.
func (handler *Handler) respondPagedNotifications(c *gin.Context, results []gin.H) {
	perPage, err := pagination.PerPage(c.Query("per_page"), pagination.DefaultPerPage, pagination.DefaultPerPage)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	cursor, err := pagination.ParseOffsetCursor(c.Query("cursor"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "Invalid cursor parameter."})
		return
	}
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

// notificationUnreadCounts is the badge: what is waiting, split into mentions and everything else.
//
// Neither count includes anything archived or snoozed, so a notification put off until tomorrow stops showing up today.
//
// The two numbers are halves of one set — what is unread and mentions the caller, and what is unread and does not — so they are two FILTERs over one pass rather than two passes that differ only by a negation. The pass reads notif_receiver_unread_idx, whose predicate is the WHERE below, so a person with years of notifications is scanned over the unread ones alone.
func (handler *Handler) notificationUnreadCounts(c *gin.Context, user *auth.User) {
	if !handler.requireWorkspaceRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	slug := c.Param("slug")
	var counts struct {
		Unread   int64 `gorm:"column:total_unread"`
		Mentions int64 `gorm:"column:mention_unread"`
	}
	err := handler.db.WithContext(c.Request.Context()).Table("notifications n").
		Joins("JOIN workspaces w ON w.id = n.workspace_id").
		Where(`w.slug = ? AND n.receiver_id = ? AND n.deleted_at IS NULL
			AND n.read_at IS NULL AND n.archived_at IS NULL AND n.snoozed_till IS NULL`, slug, user.ID).
		Select(`COUNT(*) FILTER (WHERE n.sender NOT ILIKE '%mentioned%') AS total_unread,
			COUNT(*) FILTER (WHERE n.sender ILIKE '%mentioned%') AS mention_unread`).
		Take(&counts).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, gin.H{
		"total_unread_notifications_count":   counts.Unread,
		"mention_unread_notifications_count": counts.Mentions,
	})
}

// notificationMarkAllRead marks everything still unread within the named slice.
//
// Its parameters are not the list's. They come from the body rather than the query, the type is one choice rather than a set, and its name for the subscribed set is "watching" — which is also the one place the subscription is counted without asking who made or was given the issue.
func (handler *Handler) notificationMarkAllRead(c *gin.Context, user *auth.User) {
	if !handler.requireWorkspaceRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	slug := c.Param("slug")
	var request struct {
		Snoozed  any    `json:"snoozed"`
		Archived any    `json:"archived"`
		Type     string `json:"type"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		handler.invalidDetail(c)
		return
	}
	now := handler.clock().UTC()

	query := handler.db.WithContext(c.Request.Context()).Table("notifications n").
		Joins("JOIN workspaces w ON w.id = n.workspace_id").
		Where("w.slug = ? AND n.receiver_id = ? AND n.deleted_at IS NULL AND n.read_at IS NULL", slug, user.ID)

	// Here the two flags are read for truth rather than looked up, so any value at all works and an unknown one is not an error.
	if truthyWithLength(request.Snoozed) {
		query = query.Where("n.snoozed_till < ? OR n.snoozed_till IS NOT NULL", now)
	} else {
		query = query.Where("n.snoozed_till >= ? OR n.snoozed_till IS NULL", now)
	}
	if truthyWithLength(request.Archived) {
		query = query.Where("n.archived_at IS NOT NULL")
	} else {
		query = query.Where("n.archived_at IS NULL")
	}

	switch request.Type {
	case "watching":
		query = query.Where(`n.entity_identifier IN (
			SELECT s.issue_id FROM issue_subscribers s
			JOIN workspaces sw ON sw.id = s.workspace_id
			WHERE sw.slug = ? AND s.subscriber_id = ? AND s.deleted_at IS NULL)`, slug, user.ID)
	case "assigned":
		query = query.Where(`n.entity_identifier IN (
			SELECT a.issue_id FROM issue_assignees a
			JOIN workspaces aw ON aw.id = a.workspace_id
			WHERE aw.slug = ? AND a.assignee_id = ? AND a.deleted_at IS NULL)`, slug, user.ID)
	case "created":
		role, _, err := handler.workspaceMemberRole(c.Request.Context(), slug, user.ID)
		if err != nil {
			handler.internalError(c, err)
			return
		}
		if role < roleMember {
			drf.Respond(c, http.StatusOK, gin.H{"message": "Successful"})
			return
		}
		query = query.Where(`n.entity_identifier IN (
			SELECT i.id FROM issues i
			JOIN workspaces iw ON iw.id = i.workspace_id
			WHERE iw.slug = ? AND i.created_by_id = ? AND i.deleted_at IS NULL)`, slug, user.ID)
	}

	var identifiers []string
	if err := query.Pluck("n.id", &identifiers).Error; err != nil {
		handler.internalError(c, err)
		return
	}
	if len(identifiers) > 0 {
		// A bulk update writes the named column and nothing else, so the rows keep the timestamps they had.
		err := handler.db.WithContext(c.Request.Context()).Model(&Notification{}).
			Where("id IN ?", identifiers).Update("read_at", now).Error
		if err != nil {
			handler.internalError(c, err)
			return
		}
	}
	drf.Respond(c, http.StatusOK, gin.H{"message": "Successful"})
}

func (handler *Handler) notificationRetrieve(c *gin.Context, user *auth.User) {
	handler.respondWithNotification(c, user, nil)
}

// notificationUpdate is the only write that takes a body, and it reads exactly one field out of it. Everything else the caller sends is dropped — the handler builds its own payload rather than passing the request's through.
func (handler *Handler) notificationUpdate(c *gin.Context, user *auth.User) {
	var request struct {
		SnoozedTill *string `json:"snoozed_till"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		handler.invalidDetail(c)
		return
	}
	var snoozed *time.Time
	if request.SnoozedTill != nil {
		parsed, err := time.Parse(time.RFC3339, *request.SnoozedTill)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"snoozed_till": []string{
				"Datetime has wrong format. Use one of these formats instead: YYYY-MM-DDThh:mm[:ss[.uuuuuu]][+HH:MM|-HH:MM|Z].",
			}})
			return
		}
		snoozed = &parsed
	}
	// A request that names nothing still clears the field, because the handler puts a null there rather than leaving it out.
	handler.respondWithNotification(c, user, map[string]any{"snoozed_till": snoozed})
}

func (handler *Handler) notificationDestroy(c *gin.Context, user *auth.User) {
	if !handler.requireWorkspaceRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	notification, found, err := handler.notificationByID(c, user, c.Param("slug"), c.Param("notification"))
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "The required object does not exist."})
		return
	}
	err = handler.db.WithContext(c.Request.Context()).Model(&Notification{}).
		Where("id = ?", notification.ID).Update("deleted_at", handler.clock().UTC()).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (handler *Handler) notificationMarkRead(c *gin.Context, user *auth.User) {
	handler.respondWithNotification(c, user, map[string]any{"read_at": handler.clock().UTC()})
}

func (handler *Handler) notificationMarkUnread(c *gin.Context, user *auth.User) {
	handler.respondWithNotification(c, user, map[string]any{"read_at": nil})
}

func (handler *Handler) notificationArchive(c *gin.Context, user *auth.User) {
	handler.respondWithNotification(c, user, map[string]any{"archived_at": handler.clock().UTC()})
}

func (handler *Handler) notificationUnarchive(c *gin.Context, user *auth.User) {
	handler.respondWithNotification(c, user, map[string]any{"archived_at": nil})
}

// respondWithNotification is the shape every detail route shares: read the caller's own notification, optionally write one column, and render it.
//
// None of these routes annotate the three flags the list does, and the serializer marks all three read-only, so DRF skips the fields it cannot find rather than failing. A notification read one at a time therefore carries three fewer fields than the same notification in a list.
func (handler *Handler) respondWithNotification(c *gin.Context, user *auth.User, updates map[string]any) {
	if !handler.requireWorkspaceRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	slug := c.Param("slug")
	notification, found, err := handler.notificationByID(c, user, slug, c.Param("notification"))
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		// Django's unguarded .get raises DoesNotExist.
		c.JSON(http.StatusNotFound, gin.H{"error": "The required object does not exist."})
		return
	}

	if updates != nil {
		now := handler.clock().UTC()
		updates["updated_at"] = now
		updates["updated_by_id"] = user.ID
		err = handler.db.WithContext(c.Request.Context()).Model(&Notification{}).
			Where("id = ?", notification.ID).Updates(updates).Error
		if err != nil {
			handler.internalError(c, err)
			return
		}
		applyNotificationUpdates(&notification, updates)
	}
	actors, err := handler.notificationActors(c, []notificationRow{{Notification: notification}})
	if err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, notificationJSON(notificationRow{Notification: notification}, false, actors[actorKey(notification.TriggeredByID)]))
}

// applyNotificationUpdates mirrors the write onto the instance the response is built from, the way saving an instance leaves it holding what it wrote.
func applyNotificationUpdates(notification *Notification, updates map[string]any) {
	for column, value := range updates {
		instant, _ := value.(*time.Time)
		if moment, ok := value.(time.Time); ok {
			instant = &moment
		}
		switch column {
		case "read_at":
			notification.ReadAt = instant
		case "snoozed_till":
			notification.SnoozedTill = instant
		case "archived_at":
			notification.ArchivedAt = instant
		case "updated_at":
			if instant != nil {
				notification.UpdatedAt = *instant
			}
		case "updated_by_id":
			if actor, ok := value.(string); ok {
				notification.UpdatedByID = &actor
			}
		}
	}
}

func (handler *Handler) notificationByID(c *gin.Context, user *auth.User, slug, notificationID string) (Notification, bool, error) {
	var notification Notification
	err := handler.db.WithContext(c.Request.Context()).Table("notifications n").Select("n.*").
		Joins("JOIN workspaces w ON w.id = n.workspace_id").
		Where("w.slug = ? AND n.id = ? AND n.receiver_id = ? AND n.deleted_at IS NULL", slug, notificationID, user.ID).
		Take(&notification).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Notification{}, false, nil
	}
	if err != nil {
		return Notification{}, false, err
	}
	return notification, true, nil
}

// notificationJSON is NotificationSerializer over one row.
//
// annotated says whether the three read-only flags were computed. The list computes them; every detail route does not, and DRF skips a read-only field whose attribute is missing rather than failing, so the same notification has two shapes depending on how it was asked for.
func notificationJSON(row notificationRow, annotated bool, triggeredBy gin.H) gin.H {
	data := gin.H{
		"id": row.ID, "created_at": row.CreatedAt, "updated_at": row.UpdatedAt,
		"created_by": row.CreatedByID, "updated_by": row.UpdatedByID, "deleted_at": row.DeletedAt,
		"data": decodeJSON(row.Data), "entity_identifier": row.EntityIdentifier, "entity_name": row.EntityName,
		"title": row.Title, "message": decodeJSON(row.Message), "message_html": row.MessageHTML,
		"message_stripped": row.MessageStripped, "sender": row.Sender,
		"read_at": row.ReadAt, "snoozed_till": row.SnoozedTill, "archived_at": row.ArchivedAt,
		"workspace": row.WorkspaceID, "project": row.ProjectID,
		"triggered_by": row.TriggeredByID, "receiver": row.ReceiverID,
		"triggered_by_details": triggeredBy,
	}
	if annotated {
		// Two of the three are the same subquery under different names, and both are reported.
		data["is_inbox_issue"] = row.IsIntakeIssue
		data["is_intake_issue"] = row.IsIntakeIssue
		data["is_mentioned_notification"] = row.IsMentioned
	}
	return data
}

// notificationActors loads the people the notifications were triggered by, which the serializer expands through UserLiteSerializer.
func (handler *Handler) notificationActors(c *gin.Context, rows []notificationRow) (map[string]gin.H, error) {
	identifiers := map[string]bool{}
	for _, row := range rows {
		if row.TriggeredByID != nil {
			identifiers[*row.TriggeredByID] = true
		}
	}
	actors := map[string]gin.H{}
	if len(identifiers) == 0 {
		return actors, nil
	}
	wanted := make([]string, 0, len(identifiers))
	for identifier := range identifiers {
		wanted = append(wanted, identifier)
	}
	var users []auth.User
	if err := handler.db.WithContext(c.Request.Context()).Where("id IN ?", wanted).Find(&users).Error; err != nil {
		return nil, err
	}
	for _, user := range users {
		actors[user.ID] = liteUserJSON(user, false)
	}
	return actors, nil
}

// actorKey is the key an absent trigger maps to, which no user ever has.
func actorKey(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
