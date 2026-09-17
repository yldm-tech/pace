package externalapi

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api/internal/auth"
	"gorm.io/gorm"
)

func (handler *Handler) registerCycleRoutes(router gin.IRouter) {
	handler.registerCycleCrudRoutes(router)
	handler.registerCycleIssueRoutes(router)
	handler.registerCycleTransferRoutes(router)
	base := "/api/v1/workspaces/:slug/projects/:project/"
	router.GET(base+"cycles-lite/", handler.authenticated(handler.cycleLiteList))
	router.GET(base+"archived-cycles/", handler.authenticated(handler.archivedCycleList))
	// One class is mounted at two paths, and an APIView dispatches on the method name rather than on the path — so both handlers are reachable on both. Archiving through the unarchive path is nonsense and it is what Django serves.
	for _, path := range []string{"cycles/:cycle/archive/", "archived-cycles/:cycle/unarchive/"} {
		router.POST(base+path, handler.authenticated(handler.cycleArchive))
		router.DELETE(base+path, handler.authenticated(handler.cycleUnarchive))
	}
}

// Cycle is the db.Cycle table as the external API sees it.
type Cycle struct {
	ID               string     `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt        time.Time  `gorm:"column:created_at"`
	UpdatedAt        time.Time  `gorm:"column:updated_at"`
	CreatedByID      *string    `gorm:"column:created_by_id;type:uuid"`
	UpdatedByID      *string    `gorm:"column:updated_by_id;type:uuid"`
	DeletedAt        *time.Time `gorm:"column:deleted_at"`
	ProjectID        string     `gorm:"column:project_id;type:uuid"`
	WorkspaceID      string     `gorm:"column:workspace_id;type:uuid"`
	OwnedByID        string     `gorm:"column:owned_by_id;type:uuid"`
	Name             string     `gorm:"column:name"`
	Description      string     `gorm:"column:description"`
	StartDate        *time.Time `gorm:"column:start_date"`
	EndDate          *time.Time `gorm:"column:end_date"`
	ViewProps        []byte     `gorm:"column:view_props;type:jsonb"`
	SortOrder        float64    `gorm:"column:sort_order"`
	ExternalSource   *string    `gorm:"column:external_source"`
	ExternalID       *string    `gorm:"column:external_id"`
	ProgressSnapshot []byte     `gorm:"column:progress_snapshot;type:jsonb"`
	ArchivedAt       *time.Time `gorm:"column:archived_at"`
	LogoProps        []byte     `gorm:"column:logo_props;type:jsonb"`
	Timezone         string     `gorm:"column:timezone"`
	Version          int        `gorm:"column:version"`
}

func (Cycle) TableName() string { return "cycles" }

// cycleRow is the cycle with the nine numbers the full serializer reports.
type cycleRow struct {
	Cycle
	TotalIssues        int64    `gorm:"column:total_issues"`
	CancelledIssues    int64    `gorm:"column:cancelled_issues"`
	CompletedIssues    int64    `gorm:"column:completed_issues"`
	StartedIssues      int64    `gorm:"column:started_issues"`
	UnstartedIssues    int64    `gorm:"column:unstarted_issues"`
	BacklogIssues      int64    `gorm:"column:backlog_issues"`
	TotalEstimates     *float64 `gorm:"column:total_estimates"`
	CompletedEstimates *float64 `gorm:"column:completed_estimates"`
	StartedEstimates   *float64 `gorm:"column:started_estimates"`
}

// cycleLiteList is the picker: the project's live cycles with no counts at all.
//
// Its order parameter goes through `sanitize_order_by` with **no allowlist** — the call omits the argument, so the default empty one is used and **every** field falls back. The parameter is accepted and then ignored, whatever it names.
func (handler *Handler) cycleLiteList(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireProjectMember(c, user, http.MethodGet) {
		return
	}
	var cycles []Cycle
	err := handler.db.WithContext(c.Request.Context()).Table("cycles c").Select("c.*").
		Joins("JOIN workspaces w ON w.id = c.workspace_id").
		Where("w.slug = ? AND c.project_id = ? AND c.archived_at IS NULL AND c.deleted_at IS NULL",
			c.Param("slug"), c.Param("project")).
		Distinct().Order("c.created_at DESC").Scan(&cycles).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	results := make([]gin.H, 0, len(cycles))
	for _, cycle := range cycles {
		results = append(results, cycleJSON(cycleRow{Cycle: cycle}, false))
	}
	handler.respondPaged(c, results)
}

// archivedCycleList returns the project's archived cycles with their counts.
func (handler *Handler) archivedCycleList(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireProjectMember(c, user, http.MethodGet) {
		return
	}
	var rows []cycleRow
	err := handler.db.WithContext(c.Request.Context()).Table("cycles c").
		Select(externalCycleAnnotations()).
		Joins("JOIN workspaces w ON w.id = c.workspace_id").
		Joins("JOIN project_members pm ON pm.project_id = c.project_id AND pm.member_id = ? AND pm.is_active = TRUE", user.ID).
		Joins("LEFT JOIN cycle_issues ci ON ci.cycle_id = c.id").
		Joins("LEFT JOIN issues ii ON ii.id = ci.issue_id").
		Joins("LEFT JOIN estimate_points ep ON ep.id = ii.estimate_point_id").
		Where("w.slug = ? AND c.project_id = ? AND c.archived_at IS NOT NULL AND c.deleted_at IS NULL",
			c.Param("slug"), c.Param("project")).
		Group("c.id").Order("c.created_at DESC").Scan(&rows).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	fields := requestedFields(c)
	results := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		results = append(results, narrow(cycleJSON(row, true), fields))
	}
	handler.respondPaged(c, results)
}

// externalCycleAnnotations is the nine numbers the archived list carries.
//
// The estimates sum the **key** rather than the value — the position on the scale, not the points it stands for — so a scale of 1/2/3/5/8 reports 1/2/3/4/5. That is upstream's, and it is why these numbers do not match the session API's estimate sums.
func externalCycleAnnotations() string {
	const liveIssues = `ci.deleted_at IS NULL AND ii.archived_at IS NULL AND ii.is_draft = FALSE`
	stateCount := func(group string) string {
		return `COUNT(*) FILTER (WHERE ` + liveIssues +
			` AND (SELECT s.group FROM states s WHERE s.id = ii.state_id) = '` + group + `')`
	}
	stateEstimate := func(group string) string {
		return `SUM(ep.key) FILTER (WHERE (SELECT s.group FROM states s WHERE s.id = ii.state_id) = '` + group + `')`
	}
	return `c.*,
		COUNT(*) FILTER (WHERE ` + liveIssues + `) AS total_issues,
		` + stateCount("cancelled") + ` AS cancelled_issues,
		` + stateCount("completed") + ` AS completed_issues,
		` + stateCount("started") + ` AS started_issues,
		` + stateCount("unstarted") + ` AS unstarted_issues,
		` + stateCount("backlog") + ` AS backlog_issues,
		SUM(ep.key) AS total_estimates,
		` + stateEstimate("completed") + ` AS completed_estimates,
		` + stateEstimate("started") + ` AS started_estimates`
}

// cycleArchive puts a finished cycle away, and a cycle is finished by its **end date** rather than by any status.
func (handler *Handler) cycleArchive(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireProjectMember(c, user, http.MethodPost) {
		return
	}
	cycle, found, err := handler.externalCycleByID(c, c.Param("cycle"))
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !found {
		notFound(c)
		return
	}
	now := handler.clock().UTC()
	// A cycle with no end date is never finished, and one ending today is not finished until the moment passes.
	if cycle.EndDate == nil || !cycle.EndDate.Before(now) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Only completed cycles can be archived"})
		return
	}
	err = handler.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		err := tx.Table("cycles").Where("id = ?", cycle.ID).
			Updates(map[string]any{"archived_at": now, "updated_at": now}).Error
		if err != nil {
			return err
		}
		// Every member's favourite of the cycle goes, not only the caller's.
		return tx.Table("user_favorites").
			Where(`entity_type = 'cycle' AND entity_identifier = ? AND project_id = ? AND deleted_at IS NULL
				AND workspace_id = (SELECT id FROM workspaces WHERE slug = ?)`,
				cycle.ID, c.Param("project"), c.Param("slug")).
			Update("deleted_at", now).Error
	})
	if err != nil {
		handler.serverError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// cycleUnarchive brings a cycle back. It does not restore the favourites the archive cleared.
func (handler *Handler) cycleUnarchive(c *gin.Context, user *auth.User, _ APIToken) {
	if !handler.requireProjectMember(c, user, http.MethodDelete) {
		return
	}
	cycle, found, err := handler.externalCycleByID(c, c.Param("cycle"))
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !found {
		notFound(c)
		return
	}
	now := handler.clock().UTC()
	err = handler.db.WithContext(c.Request.Context()).Table("cycles").Where("id = ?", cycle.ID).
		Updates(map[string]any{"archived_at": nil, "updated_at": now}).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (handler *Handler) externalCycleByID(c *gin.Context, identifier string) (Cycle, bool, error) {
	var cycles []Cycle
	err := handler.db.WithContext(c.Request.Context()).Table("cycles c").Select("c.*").
		Joins("JOIN workspaces w ON w.id = c.workspace_id").
		Where("w.slug = ? AND c.project_id = ? AND c.id = ? AND c.deleted_at IS NULL",
			c.Param("slug"), c.Param("project"), identifier).
		Limit(1).Scan(&cycles).Error
	if err != nil || len(cycles) == 0 {
		return Cycle{}, false, err
	}
	return cycles[0], true, nil
}

// cycleJSON is the external API's CycleSerializer, or the lite one when the counts are left off.
func cycleJSON(row cycleRow, counted bool) gin.H {
	data := gin.H{
		"id": row.ID, "created_at": row.CreatedAt, "updated_at": row.UpdatedAt,
		"created_by": row.CreatedByID, "updated_by": row.UpdatedByID, "deleted_at": row.DeletedAt,
		"name": row.Name, "description": row.Description,
		"start_date": row.StartDate, "end_date": row.EndDate,
		"view_props": decodeJSON(row.ViewProps), "sort_order": row.SortOrder,
		"external_source": row.ExternalSource, "external_id": row.ExternalID,
		"progress_snapshot": decodeJSON(row.ProgressSnapshot), "archived_at": row.ArchivedAt,
		"logo_props": decodeJSON(row.LogoProps), "timezone": row.Timezone, "version": row.Version,
		"project": row.ProjectID, "workspace": row.WorkspaceID, "owned_by": row.OwnedByID,
	}
	if counted {
		data["total_issues"] = row.TotalIssues
		data["cancelled_issues"] = row.CancelledIssues
		data["completed_issues"] = row.CompletedIssues
		data["started_issues"] = row.StartedIssues
		data["unstarted_issues"] = row.UnstartedIssues
		data["backlog_issues"] = row.BacklogIssues
		data["total_estimates"] = row.TotalEstimates
		data["completed_estimates"] = row.CompletedEstimates
		data["started_estimates"] = row.StartedEstimates
	}
	return data
}
