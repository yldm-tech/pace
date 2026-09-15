package externalapi

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"github.com/yldm-tech/pace/apps/api-go/internal/drf"
	"gorm.io/gorm"
)

func (handler *Handler) registerIssueActivityRoutes(router gin.IRouter) {
	base := "/api/v1/workspaces/:slug/projects/:project/"
	for _, name := range []string{"issues", "work-items"} {
		activities := base + name + "/:issue/activities/"
		router.GET(activities, handler.authenticated(handler.issueActivityList))
		router.GET(activities+":activity/", handler.authenticated(handler.issueActivityRetrieve))
	}
}

// IssueActivity is the db.IssueActivity table as the external API sees it.
type IssueActivity struct {
	ID             string     `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt      time.Time  `gorm:"column:created_at"`
	UpdatedAt      time.Time  `gorm:"column:updated_at"`
	DeletedAt      *time.Time `gorm:"column:deleted_at"`
	ProjectID      *string    `gorm:"column:project_id;type:uuid"`
	WorkspaceID    string     `gorm:"column:workspace_id;type:uuid"`
	IssueID        *string    `gorm:"column:issue_id;type:uuid"`
	IssueCommentID *string    `gorm:"column:issue_comment_id;type:uuid"`
	ActorID        *string    `gorm:"column:actor_id;type:uuid"`
	Verb           string     `gorm:"column:verb"`
	Field          *string    `gorm:"column:field"`
	OldValue       *string    `gorm:"column:old_value"`
	NewValue       *string    `gorm:"column:new_value"`
	Comment        *string    `gorm:"column:comment"`
	Attachments    []byte     `gorm:"column:attachments;type:text[]"`
	OldIdentifier  *string    `gorm:"column:old_identifier;type:uuid"`
	NewIdentifier  *string    `gorm:"column:new_identifier;type:uuid"`
	Epoch          *float64   `gorm:"column:epoch"`
}

func (IssueActivity) TableName() string { return "issue_activities" }

// hiddenActivityFields are the four kinds this API never shows.
//
// A comment's own activity is excluded because the comment routes report it; votes and reactions belong to the space app; and a draft's activity describes work that was never published.
var hiddenActivityFields = []string{"comment", "vote", "reaction", "draft"}

// activityOrderByAllowlist is ACTIVITY_ORDER_BY_ALLOWLIST: two fields and nothing else.
var activityOrderByAllowlist = map[string]bool{"created_at": true, "updated_at": true}

// issueActivityList returns a work item's history, oldest first.
//
// The default order is **ascending** here, where every other list in this API is newest first — a history reads forwards.
func (handler *Handler) issueActivityList(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireProjectMember(c, user, http.MethodGet) {
		return
	}
	order := sanitizeOrderBy(c.Query("order_by"), activityOrderByAllowlist, "created_at")
	var activities []IssueActivity
	err := handler.issueActivityScope(c, user).Order(activityOrderColumn(order)).Scan(&activities).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	fields := requestedFields(c)
	results := make([]gin.H, 0, len(activities))
	for _, activity := range activities {
		results = append(results, narrow(issueActivityJSON(activity), fields))
	}
	handler.respondPaged(c, results)
}

// issueActivityRetrieve returns one entry.
//
// It answers a **different 404** from every other route in this app: a message and a code rather than the base view's wording.
func (handler *Handler) issueActivityRetrieve(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireProjectMember(c, user, http.MethodGet) {
		return
	}
	order := sanitizeOrderBy(c.Query("order_by"), activityOrderByAllowlist, "created_at")
	var activities []IssueActivity
	err := handler.issueActivityScope(c, user).Where("a.id = ?", c.Param("activity")).
		Order(activityOrderColumn(order)).Limit(1).Scan(&activities).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if len(activities) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"message": "Activity not found.", "code": "NOT_FOUND"})
		return
	}
	drf.Respond(c, http.StatusOK, narrow(issueActivityJSON(activities[0]), requestedFields(c)))
}

// activityOrderColumn turns an allowlisted order field into the column it names.
func activityOrderColumn(field string) string {
	descending := len(field) > 0 && field[0] == '-'
	bare := field
	if descending {
		bare = field[1:]
	}
	column := "a.created_at"
	if bare == "updated_at" {
		column = "a.updated_at"
	}
	if descending {
		return column + " DESC"
	}
	return column
}

// issueActivityScope is the queryset both routes read through.
func (handler *Handler) issueActivityScope(c *gin.Context, user *auth.User) *gorm.DB {
	return handler.db.WithContext(c.Request.Context()).Table("issue_activities a").Select("a.*").
		Joins("JOIN workspaces w ON w.id = a.workspace_id").
		Joins("JOIN projects p ON p.id = a.project_id AND p.archived_at IS NULL").
		Joins("JOIN project_members pm ON pm.project_id = a.project_id AND pm.member_id = ? AND pm.is_active = TRUE", user.ID).
		Where("w.slug = ? AND a.project_id = ? AND a.issue_id = ? AND a.deleted_at IS NULL",
			c.Param("slug"), c.Param("project"), c.Param("issue")).
		// The exclusion is a NOT IN, so an entry whose field is null survives it: a null is not in any list.
		Where("a.field IS NULL OR a.field NOT IN ?", hiddenActivityFields)
}

// issueActivityJSON is the external API's IssueActivitySerializer.
func issueActivityJSON(activity IssueActivity) gin.H {
	return gin.H{
		"id": activity.ID, "created_at": activity.CreatedAt, "updated_at": activity.UpdatedAt,
		"deleted_at": activity.DeletedAt, "verb": activity.Verb, "field": activity.Field,
		"old_value": activity.OldValue, "new_value": activity.NewValue,
		"comment": activity.Comment, "attachments": decodeJSON(activity.Attachments),
		"old_identifier": activity.OldIdentifier, "new_identifier": activity.NewIdentifier,
		"epoch": activity.Epoch, "actor": activity.ActorID,
		"project": activity.ProjectID, "workspace": activity.WorkspaceID,
		"issue": activity.IssueID, "issue_comment": activity.IssueCommentID,
	}
}
