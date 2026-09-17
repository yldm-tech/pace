package project

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api/internal/auth"
	"github.com/yldm-tech/pace/apps/api/internal/drf"
	"gorm.io/gorm"
)

func (handler *Handler) registerAnalyticsRoutes(router gin.IRouter) {
	router.GET("/api/workspaces/:slug/analytics/", handler.authenticated(handler.analytics))
	router.GET("/api/workspaces/:slug/saved-analytic-view/:view/", handler.authenticated(handler.savedAnalytics))
}

// analytics draws one chart over the workspace's issues, plus the lookup tables the frontend needs to label it.
func (handler *Handler) analytics(c *gin.Context, user *auth.User) {
	if !handler.requireWorkspaceRole(c, user, roleAdmin, roleMember) {
		return
	}
	xAxis, yAxis, segment := c.Query("x_axis"), c.Query("y_axis"), c.Query("segment")
	if message := validateAnalyticsAxes(xAxis, yAxis, segment); message != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": message})
		return
	}

	filters := issueFilters(queryParams(c), "GET", "", handler.clock().UTC())
	joins, conditions, arguments, translatable := issueFilterSQL(filters)
	if !translatable {
		handler.internalError(c, errors.New("analytics: a filter names a field the schema does not have"))
		return
	}

	scope := func() *gorm.DB {
		query := handler.db.WithContext(c.Request.Context()).Table("issues i").
			Joins("JOIN workspaces w ON w.id = i.workspace_id").
			Where("w.slug = ?", c.Param("slug")).
			Where(issueObjectsPredicate("i"))
		for _, join := range joins {
			query = query.Joins(join)
		}
		consumed := 0
		for _, condition := range conditions {
			count := countPlaceholders(condition)
			query = query.Where(condition, arguments[consumed:consumed+count]...)
			consumed += count
		}
		return query
	}

	var total int64
	if err := scope().Distinct("i.id").Count(&total).Error; err != nil {
		handler.internalError(c, err)
		return
	}
	distribution, order, err := handler.buildGraphPlot(scope(), xAxis, yAxis, segment)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	extras, err := handler.analyticsExtras(c, scope, xAxis, segment)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, gin.H{
		"total": total, "distribution": orderedDistribution(distribution, order), "extras": extras,
	})
}

// savedAnalytics draws the chart a saved view describes.
//
// The axes come from the saved view rather than the request, and the segment comes from the request — so the same saved view draws a different chart depending on what is asked alongside it.
func (handler *Handler) savedAnalytics(c *gin.Context, user *auth.User) {
	if !handler.requireWorkspaceRole(c, user, roleAdmin, roleMember) {
		return
	}
	view, found, err := handler.analyticViewByID(c)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		// Django's unguarded .get raises DoesNotExist.
		c.JSON(http.StatusNotFound, gin.H{"error": "The required object does not exist."})
		return
	}

	saved, _ := decodeJSON(view.QueryDict).(map[string]any)
	xAxis, _ := saved["x_axis"].(string)
	yAxis, _ := saved["y_axis"].(string)
	segment := c.Query("segment")
	if message := validateAnalyticsAxes(xAxis, yAxis, segment); message != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": message})
		return
	}

	// The stored query is applied as it stands rather than reparsed, so a view saved before the filter grammar changed keeps whatever it stored.
	stored, _ := decodeJSON(view.Query).(map[string]any)
	conditions, arguments, translatable := storedQueryConditions(stored)
	if !translatable {
		handler.internalError(c, errors.New("saved analytics: the stored query names a field the schema does not have"))
		return
	}

	scope := func() *gorm.DB {
		query := handler.db.WithContext(c.Request.Context()).Table("issues i").
			Where(issueObjectsPredicate("i"))
		for index, condition := range conditions {
			query = query.Where(condition, arguments[index]...)
		}
		return query
	}
	var total int64
	if err := scope().Distinct("i.id").Count(&total).Error; err != nil {
		handler.internalError(c, err)
		return
	}
	distribution, order, err := handler.buildGraphPlot(scope(), xAxis, yAxis, segment)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, gin.H{
		"total": total, "distribution": orderedDistribution(distribution, order),
	})
}

// buildGraphPlot is analytics_plot.build_graph_plot.
func (handler *Handler) buildGraphPlot(query *gorm.DB, xAxis, yAxis, segment string) (map[string][]gin.H, []string, error) {
	dimension := graphDimension(xAxis)
	if join, needed := analyticsJoins[xAxis]; needed {
		query = query.Joins(join)
	}
	segmented := segment != ""
	if segmented {
		if join, needed := analyticsJoins[segment]; needed && segment != xAxis {
			query = query.Joins(join)
		}
	}

	selection := dimension + " AS dimension"
	grouping := dimension
	if segmented {
		selection += ", " + graphDimension(segment) + " AS segment"
		grouping += ", " + graphDimension(segment)
	}
	if yAxis == "issue_count" {
		selection += ", COUNT(*) AS count"
	} else {
		selection += ", SUM(CAST(ep2.value AS DOUBLE PRECISION)) AS estimate"
		query = query.Joins("LEFT JOIN estimate_points ep2 ON ep2.id = i.estimate_point_id")
	}

	var rows []graphRow
	// The null dimension is excluded before the grouping, which is why an axis whose value is missing contributes nothing rather than a bucket of its own.
	err := query.Where(dimension + " IS NOT NULL").
		Select(selection).Group(grouping).Order(dimension).Scan(&rows).Error
	if err != nil {
		return nil, nil, err
	}
	grouped := groupGraphRows(rows, segmented)
	return grouped, sortGraphData(grouped, xAxis), nil
}

// orderedDistribution renders the groups in the order sort_data put them, which the JSON object preserves because Python's dictionaries are ordered.
func orderedDistribution(grouped map[string][]gin.H, order []string) gin.H {
	distribution := gin.H{}
	for _, key := range order {
		distribution[key] = grouped[key]
	}
	return distribution
}

// storedQueryConditions translates a saved query dictionary, which holds lookups rather than parameters and so goes through the same SQL mapper the live filters do.
func storedQueryConditions(stored map[string]any) ([]string, [][]any, bool) {
	filters := map[string]filterValue{}
	for lookup, value := range stored {
		switch typed := value.(type) {
		case bool:
			filters[lookup] = boolValue(typed)
		case string:
			filters[lookup] = textValue(typed)
		case []any:
			items := make([]string, 0, len(typed))
			for _, item := range typed {
				text, ok := item.(string)
				if !ok {
					return nil, nil, false
				}
				items = append(items, text)
			}
			filters[lookup] = listValue(items)
		default:
			return nil, nil, false
		}
	}
	joins, conditions, arguments, ok := issueFilterSQL(filters)
	if !ok || len(joins) > 0 {
		// A stored query that needs a join is not translatable here, because the scope this runs in has none.
		return nil, nil, ok && len(joins) == 0
	}
	grouped := make([][]any, 0, len(conditions))
	consumed := 0
	for _, condition := range conditions {
		count := countPlaceholders(condition)
		grouped = append(grouped, arguments[consumed:consumed+count])
		consumed += count
	}
	return conditions, grouped, true
}

// analyticsExtras are the lookup tables the frontend labels the chart with. Each is filled only when the axis or the segment names the thing it describes.
func (handler *Handler) analyticsExtras(c *gin.Context, scope func() *gorm.DB, xAxis, segment string) (gin.H, error) {
	extras := gin.H{
		"state_details": []gin.H{}, "assignee_details": []gin.H{}, "label_details": []gin.H{},
		"cycle_details": []gin.H{}, "module_details": []gin.H{},
	}
	names := func(field string) bool { return xAxis == field || segment == field }

	if names("state_id") {
		var rows []struct {
			StateID   *string `gorm:"column:state_id"`
			StateName *string `gorm:"column:state_name"`
			Color     *string `gorm:"column:state_color"`
		}
		err := scope().Distinct("i.state_id").
			Select(`i.state_id,
				(SELECT s.name FROM states s WHERE s.id = i.state_id) AS state_name,
				(SELECT s.color FROM states s WHERE s.id = i.state_id) AS state_color`).
			Order("i.state_id").Scan(&rows).Error
		if err != nil {
			return nil, err
		}
		details := make([]gin.H, 0, len(rows))
		for _, row := range rows {
			details = append(details, gin.H{
				"state_id": row.StateID, "state__name": row.StateName, "state__color": row.Color,
			})
		}
		extras["state_details"] = details
	}

	if names("labels__id") {
		var rows []struct {
			LabelID string  `gorm:"column:label_id"`
			Color   *string `gorm:"column:label_color"`
			Name    string  `gorm:"column:label_name"`
		}
		// The label lookup goes through the **plain** manager rather than issue_objects, so a label is listed even when every issue carrying it is archived or a draft.
		err := handler.db.WithContext(c.Request.Context()).Table("issues i").
			Joins("JOIN workspaces w ON w.id = i.workspace_id").
			Joins("JOIN issue_labels li ON li.issue_id = i.id AND li.deleted_at IS NULL").
			Joins("JOIN labels l ON l.id = li.label_id").
			Where("w.slug = ? AND i.deleted_at IS NULL", c.Param("slug")).
			Distinct("li.label_id").Select("li.label_id, l.color AS label_color, l.name AS label_name").
			Order("li.label_id").Scan(&rows).Error
		if err != nil {
			return nil, err
		}
		details := make([]gin.H, 0, len(rows))
		for _, row := range rows {
			details = append(details, gin.H{
				"labels__id": row.LabelID, "labels__color": row.Color, "labels__name": row.Name,
			})
		}
		extras["label_details"] = details
	}

	if names("assignees__id") {
		var rows []struct {
			AvatarURL   *string `gorm:"column:avatar_url"`
			DisplayName string  `gorm:"column:display_name"`
			FirstName   string  `gorm:"column:first_name"`
			LastName    string  `gorm:"column:last_name"`
			AssigneeID  string  `gorm:"column:assignee_id"`
		}
		// Only an assignee who has a picture at all is listed, which is the filter the endpoint carries and not an obvious one.
		err := scope().
			Joins("JOIN issue_assignees ai ON ai.issue_id = i.id").
			Joins("JOIN users u ON u.id = ai.assignee_id").
			Where("u.avatar IS NOT NULL OR u.avatar_asset_id IS NOT NULL").
			Distinct("ai.assignee_id").
			Select(`CASE WHEN u.avatar_asset_id IS NOT NULL
					THEN '/api/assets/v2/static/' || CAST(u.avatar_asset_id AS TEXT) || '/'
					ELSE u.avatar END AS avatar_url,
				u.display_name, u.first_name, u.last_name, ai.assignee_id`).
			Order("ai.assignee_id").Scan(&rows).Error
		if err != nil {
			return nil, err
		}
		details := make([]gin.H, 0, len(rows))
		for _, row := range rows {
			details = append(details, gin.H{
				"assignees__avatar_url": row.AvatarURL, "assignees__display_name": row.DisplayName,
				"assignees__first_name": row.FirstName, "assignees__last_name": row.LastName,
				"assignees__id": row.AssigneeID,
			})
		}
		extras["assignee_details"] = details
	}

	if names("issue_cycle__cycle_id") {
		var rows []struct {
			CycleID string `gorm:"column:cycle_id"`
			Name    string `gorm:"column:cycle_name"`
		}
		err := scope().
			Joins("JOIN cycle_issues ci ON ci.issue_id = i.id AND ci.deleted_at IS NULL").
			Joins("JOIN cycles cy ON cy.id = ci.cycle_id").
			Distinct("ci.cycle_id").Select("ci.cycle_id, cy.name AS cycle_name").
			Order("ci.cycle_id").Scan(&rows).Error
		if err != nil {
			return nil, err
		}
		details := make([]gin.H, 0, len(rows))
		for _, row := range rows {
			details = append(details, gin.H{
				"issue_cycle__cycle_id": row.CycleID, "issue_cycle__cycle__name": row.Name,
			})
		}
		extras["cycle_details"] = details
	}

	if names("issue_module__module_id") {
		var rows []struct {
			ModuleID string `gorm:"column:module_id"`
			Name     string `gorm:"column:module_name"`
		}
		err := scope().
			Joins("JOIN module_issues mi ON mi.issue_id = i.id AND mi.deleted_at IS NULL").
			Joins("JOIN modules mo ON mo.id = mi.module_id").
			Distinct("mi.module_id").Select("mi.module_id, mo.name AS module_name").
			Order("mi.module_id").Scan(&rows).Error
		if err != nil {
			return nil, err
		}
		details := make([]gin.H, 0, len(rows))
		for _, row := range rows {
			details = append(details, gin.H{
				"issue_module__module_id": row.ModuleID, "issue_module__module__name": row.Name,
			})
		}
		extras["module_details"] = details
	}
	return extras, nil
}
