package project

import (
	"errors"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// chartAxis is one of the thirteen things a custom chart can be cut by: what identifies a bar, what it is called, whatever joins that needs, and the soft-delete rule that comes with a relation.
type chartAxis struct {
	key   string
	name  string
	joins []chartJoin
	where string
	// numeric marks the one axis whose key is a number rather than a string, so it is reported as one.
	numeric bool
}

type chartJoin struct {
	alias  string
	clause string
}

// chartAxes is get_x_axis_field. The soft-delete rule on each relation sits on the same join the key is read from, which is what Django produces and what keeps a work item's deleted label out of the chart without hiding the work item.
var chartAxes = map[string]chartAxis{
	"STATES":       {key: "i.state_id", name: "s.name"},
	"STATE_GROUPS": {key: `s."group"`, name: `s."group"`},
	"LABELS": {key: "cl.label_id", name: "clab.name", where: "cl.deleted_at IS NULL", joins: []chartJoin{
		{alias: "cl", clause: "LEFT JOIN issue_labels cl ON cl.issue_id = i.id"},
		{alias: "clab", clause: "LEFT JOIN labels clab ON clab.id = cl.label_id"},
	}},
	"ASSIGNEES": {key: "ca.assignee_id", name: "cau.display_name", where: "ca.deleted_at IS NULL", joins: []chartJoin{
		{alias: "ca", clause: "LEFT JOIN issue_assignees ca ON ca.issue_id = i.id"},
		{alias: "cau", clause: "LEFT JOIN users cau ON cau.id = ca.assignee_id"},
	}},
	"ESTIMATE_POINTS": {key: "cep.key", name: "cep.value", numeric: true, joins: []chartJoin{
		{alias: "cep", clause: "LEFT JOIN estimate_points cep ON cep.id = i.estimate_point_id"},
	}},
	"CYCLES": {key: "cc.cycle_id", name: "ccy.name", where: "cc.deleted_at IS NULL", joins: []chartJoin{
		{alias: "cc", clause: "LEFT JOIN cycle_issues cc ON cc.issue_id = i.id"},
		{alias: "ccy", clause: "LEFT JOIN cycles ccy ON ccy.id = cc.cycle_id"},
	}},
	"MODULES": {key: "cm.module_id", name: "cmo.name", where: "cm.deleted_at IS NULL", joins: []chartJoin{
		{alias: "cm", clause: "LEFT JOIN module_issues cm ON cm.issue_id = i.id"},
		{alias: "cmo", clause: "LEFT JOIN modules cmo ON cmo.id = cm.module_id"},
	}},
	"PRIORITY":     {key: "i.priority", name: "i.priority"},
	"START_DATE":   {key: "i.start_date", name: "i.start_date"},
	"TARGET_DATE":  {key: "i.target_date", name: "i.target_date"},
	"CREATED_AT":   {key: "(i.created_at AT TIME ZONE 'UTC')::date", name: "(i.created_at AT TIME ZONE 'UTC')::date"},
	"COMPLETED_AT": {key: "(i.completed_at AT TIME ZONE 'UTC')::date", name: "(i.completed_at AT TIME ZONE 'UTC')::date"},
	"CREATED_BY": {key: "i.created_by_id", name: "ccb.display_name", joins: []chartJoin{
		{alias: "ccb", clause: "LEFT JOIN users ccb ON ccb.id = i.created_by_id"},
	}},
}

// The two ValidationErrors build_analytics_chart raises, which DRF answers 400 for with the message in a list. They are separate because the message names which of the two fields was wrong.
var (
	errInvalidXAxis   = errors.New("invalid x_axis field")
	errInvalidGroupBy = errors.New("invalid group_by field")
)

// buildAnalyticsChart is build_analytics_chart over a query that already carries the work items the chart is about.
//
// Every bar counts **distinct work items**, so a work item with three labels adds one to each of three bars rather than three to any of them.
func buildAnalyticsChart(scope *gorm.DB, xAxis, groupBy string) (gin.H, error) {
	axis, known := chartAxes[xAxis]
	if !known {
		return nil, errInvalidXAxis
	}
	if groupBy != "" {
		if _, known := chartAxes[groupBy]; !known {
			return nil, errInvalidGroupBy
		}
	}
	applied := map[string]bool{}
	apply := func(query *gorm.DB, axis chartAxis) *gorm.DB {
		for _, join := range axis.joins {
			if applied[join.alias] {
				continue
			}
			applied[join.alias] = true
			query = query.Joins(join.clause)
		}
		if axis.where != "" {
			query = query.Where(axis.where)
		}
		return query
	}
	query := apply(scope, axis)
	if groupBy == "" {
		return buildSimpleChart(query, axis)
	}
	group := chartAxes[groupBy]
	return buildGroupedChart(apply(query, group), axis, group)
}

// chartRow is one row of either shape. The key and the name are read as text so one scan serves a uuid, a word, a date and a number alike; the number is turned back at the end.
type chartRow struct {
	Key       *string `gorm:"column:key"`
	Name      *string `gorm:"column:display_name"`
	GroupKey  *string `gorm:"column:group_key"`
	GroupName *string `gorm:"column:group_name"`
	Count     int64   `gorm:"column:count"`
}

// buildSimpleChart is build_simple_chart_response: one bar per value of the axis, sorted by that value.
func buildSimpleChart(query *gorm.DB, axis chartAxis) (gin.H, error) {
	var rows []chartRow
	err := query.
		Select(axis.key + "::text AS key, " + axis.name + "::text AS display_name, COUNT(DISTINCT i.id) AS count").
		Group(axis.key + ", " + axis.name).
		Order(axis.key + " ASC").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	data := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		data = append(data, gin.H{
			"key": chartValue(row.Key, axis), "name": chartLabel(row.Name), "count": row.Count,
		})
	}
	return gin.H{"data": data, "schema": gin.H{}}, nil
}

// buildGroupedChart is build_grouped_chart_response plus process_grouped_data: one bar per value of the axis, and inside each bar one number per value of the grouping.
//
// The bars come back in the order the rows arrived, which is by descending count — so the order of the bars follows the largest single group inside them rather than their own totals.
func buildGroupedChart(query *gorm.DB, axis, group chartAxis) (gin.H, error) {
	var rows []chartRow
	err := query.
		Select(axis.key + "::text AS key, " + axis.name + "::text AS display_name, " +
			group.key + "::text AS group_key, " + group.name + "::text AS group_name, " +
			"COUNT(DISTINCT i.id) AS count").
		Group(axis.key + ", " + axis.name + ", " + group.key + ", " + group.name).
		Order("count DESC").Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	order := []string{}
	bars := map[string]gin.H{}
	schema := gin.H{}
	for _, row := range rows {
		bucket := ""
		if row.Key != nil {
			bucket = *row.Key
		}
		bar, seen := bars[bucket]
		if !seen {
			// An empty key becomes the lowercase "none" while an empty name becomes the capitalised "None", which is a difference upstream makes and the web client reads.
			bar = gin.H{"key": chartValue(row.Key, axis), "name": chartLabel(row.Name), "count": int64(0)}
			if row.Key == nil || *row.Key == "" {
				bar["key"] = "none"
			}
			bars[bucket] = bar
			order = append(order, bucket)
		}
		groupKey := "none"
		if row.GroupKey != nil && *row.GroupKey != "" {
			groupKey = *row.GroupKey
		}
		schema[groupKey] = chartLabel(row.GroupName)
		existing, _ := bar[groupKey].(int64)
		bar[groupKey] = existing + row.Count
		total, _ := bar["count"].(int64)
		bar["count"] = total + row.Count
	}

	data := make([]gin.H, 0, len(order))
	for _, bucket := range order {
		data = append(data, bars[bucket])
	}
	return gin.H{"data": data, "schema": schema}, nil
}

// chartValue renders a bar's key. Everything is a string except the estimate point, whose key is a number.
func chartValue(value *string, axis chartAxis) any {
	if value == nil || *value == "" {
		return "None"
	}
	if axis.numeric {
		if number, err := strconv.Atoi(*value); err == nil {
			return number
		}
	}
	return *value
}

// chartLabel is what a missing name becomes, which is the word rather than nothing.
func chartLabel(value *string) any {
	if value == nil || *value == "" {
		return "None"
	}
	return *value
}
