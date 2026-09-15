package project

import (
	"sort"

	"github.com/gin-gonic/gin"
)

// graphDimension is the column a chart is grouped or segmented by, written the way Django annotates it.
//
// A date axis is turned into a year-and-month string rather than grouped by the instant. It is **not** zero padded — March 2026 is `2026-3`, not `2026-03` — because Django concatenates the two extracted numbers as text.
func graphDimension(field string) string {
	if analyticsDateFields[field] {
		column := analyticsColumns[field]
		return `COALESCE((EXTRACT(YEAR FROM ` + column + ` AT TIME ZONE 'UTC'))::text, '') || ` +
			`COALESCE(COALESCE('-', '') || COALESCE((EXTRACT(MONTH FROM ` + column + ` AT TIME ZONE 'UTC'))::text, ''), '')`
	}
	return analyticsColumns[field]
}

// analyticsDateFields are the four axes that become a month rather than a value.
var analyticsDateFields = map[string]bool{
	"created_at": true, "start_date": true, "target_date": true, "completed_at": true,
}

// analyticsColumns maps each allowed axis onto the column or join it names.
var analyticsColumns = map[string]string{
	"state_id":                "i.state_id",
	"state__group":            "(SELECT s.group FROM states s WHERE s.id = i.state_id)",
	"labels__id":              "li.label_id",
	"assignees__id":           "ai.assignee_id",
	"estimate_point__value":   "ep.value",
	"issue_cycle__cycle_id":   "ci.cycle_id",
	"issue_module__module_id": "mi.module_id",
	"priority":                "i.priority",
	"start_date":              "i.start_date",
	"target_date":             "i.target_date",
	"created_at":              "i.created_at",
	"completed_at":            "i.completed_at",
}

// analyticsJoins maps each axis onto the join it needs, which is nothing for the ones that live on the issue itself.
var analyticsJoins = map[string]string{
	"labels__id":              "LEFT JOIN issue_labels li ON li.issue_id = i.id",
	"assignees__id":           "LEFT JOIN issue_assignees ai ON ai.issue_id = i.id",
	"estimate_point__value":   "LEFT JOIN estimate_points ep ON ep.id = i.estimate_point_id",
	"issue_cycle__cycle_id":   "LEFT JOIN cycle_issues ci ON ci.issue_id = i.id",
	"issue_module__module_id": "LEFT JOIN module_issues mi ON mi.issue_id = i.id",
}

// priorityChartOrder is the order a priority axis is reported in, which is not alphabetical and not the ordering the issue lists use either.
var priorityChartOrder = []string{"low", "medium", "high", "urgent", "none"}

// sortGraphData is analytics_plot.sort_data.
//
// A priority axis is reported in a fixed order and everything the order does not name is **dropped**, including any key the data has that the list does not. Every other axis is sorted with the literal key "none" last and the rest in ordinary order.
func sortGraphData(data map[string][]gin.H, axis string) []string {
	if axis == "priority" {
		keys := make([]string, 0, len(priorityChartOrder))
		for _, key := range priorityChartOrder {
			if _, present := data[key]; present {
				keys = append(keys, key)
			}
		}
		return keys
	}
	keys := make([]string, 0, len(data))
	for key := range data {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(first, second int) bool {
		firstIsNone := keys[first] == "none"
		secondIsNone := keys[second] == "none"
		if firstIsNone != secondIsNone {
			return secondIsNone
		}
		return keys[first] < keys[second]
	})
	return keys
}

// groupGraphRows is the itertools.groupby the plot ends with: consecutive rows sharing a dimension become one group.
//
// It groups **runs** rather than values, and the query is ordered by dimension so a run is a whole group. A dimension that somehow appeared twice non-consecutively would have its first run overwritten by its second, which is what the dict comprehension does.
func groupGraphRows(rows []graphRow, segmented bool) map[string][]gin.H {
	grouped := map[string][]gin.H{}
	for index := 0; index < len(rows); {
		key := rows[index].Dimension
		items := []gin.H{}
		for index < len(rows) && rows[index].Dimension == key {
			items = append(items, graphRowJSON(rows[index], segmented))
			index++
		}
		grouped[key] = items
	}
	return grouped
}

// graphRow is one row of the plot: a dimension, optionally a segment, and whichever measure was asked for.
type graphRow struct {
	Dimension string   `gorm:"column:dimension"`
	Segment   *string  `gorm:"column:segment"`
	Count     *int64   `gorm:"column:count"`
	Estimate  *float64 `gorm:"column:estimate"`
}

func graphRowJSON(row graphRow, segmented bool) gin.H {
	data := gin.H{"dimension": row.Dimension}
	if segmented {
		data["segment"] = row.Segment
	}
	if row.Count != nil {
		data["count"] = *row.Count
	} else {
		data["estimate"] = row.Estimate
	}
	return data
}
