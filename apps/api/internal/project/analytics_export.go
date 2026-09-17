package project

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// analyticsRowHeadings is row_mapping: the heading each axis is given in the exported spreadsheet.
//
// It does not name every axis a chart may be drawn on. `state_id` is missing from it while `state__name` — which is not a valid axis at all — is in it, so a chart grouped by state has its first column headed literally "X-Axis". That is reproduced rather than corrected.
var analyticsRowHeadings = map[string]string{
	"state__name":             "State",
	"state__group":            "State Group",
	"labels__id":              "Label",
	"assignees__id":           "Assignee Name",
	"start_date":              "Start Date",
	"target_date":             "Due Date",
	"completed_at":            "Completed At",
	"created_at":              "Created At",
	"issue_count":             "Issue Count",
	"priority":                "Priority",
	"estimate":                "Estimate",
	"issue_cycle__cycle_id":   "Cycle",
	"issue_module__module_id": "Module",
}

// The five axes whose values are ids, and which are therefore replaced with a readable name before they reach the file. Every other axis already reads as itself.
const (
	analyticsAssigneeAxis = "assignees__id"
	analyticsLabelAxis    = "labels__id"
	analyticsStateAxis    = "state_id"
	analyticsCycleAxis    = "issue_cycle__cycle_id"
	analyticsModuleAxis   = "issue_module__module_id"
)

// errAnalyticsExportModuleSegment is the KeyError upstream raises when a chart is segmented by module and grouped by label.
//
// generate_segmented_rows looks the module segments up in label_details rather than module_details. With no label axis that table is empty and the lookup finds nothing, so the segment columns keep their raw uuids. With a label axis it is full of label rows, and asking one of them for its module id raises — which the task's own except swallows, so the export simply never arrives. Both are reproduced.
var errAnalyticsExportModuleSegment = errors.New("analytics export: the module segments are looked up among the labels, which have no module id")

// AnalyticsExportRows is the grid analytic_export_task writes into its csv: a heading row and one row per bucket of the chart the caller described.
//
// It lives beside the analytics endpoint rather than in the worker because it is the same chart. build_graph_plot, the filter translation and the lookup tables are all here already, and a second copy of them in the worker would be a second thing to keep in step.
func AnalyticsExportRows(ctx context.Context, database *gorm.DB, now time.Time, slug string, data map[string]any) ([][]any, error) {
	xAxis, _ := data["x_axis"].(string)
	yAxis, _ := data["y_axis"].(string)
	segment, _ := data["segment"].(string)
	// build_graph_plot refuses an axis it does not know, and the task's own except swallows the refusal — so a bad axis means no file and no email rather than an error anybody sees.
	if message := validateAnalyticsAxes(xAxis, yAxis, segment); message != "" {
		return nil, errors.New(message)
	}

	filters, err := viewQueryFromFilters(data, now)
	if err != nil {
		return nil, err
	}
	joins, conditions, arguments, translatable := analyticsExportFilterSQL(filters)
	if !translatable {
		return nil, errors.New("analytics export: a filter names a field the schema does not have")
	}

	runner := &Handler{db: database, clock: func() time.Time { return now }}
	scope := func() *gorm.DB {
		query := database.WithContext(ctx).Table("issues i").
			Joins("JOIN workspaces w ON w.id = i.workspace_id").
			Where("w.slug = ?", slug).
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

	distribution, order, err := runner.buildGraphPlot(scope(), xAxis, yAxis, segment)
	if err != nil {
		return nil, err
	}
	names, err := analyticsExportNames(ctx, database, scope, slug, xAxis, segment, joins, conditions, arguments)
	if err != nil {
		return nil, err
	}

	key := "estimate"
	if yAxis == "issue_count" {
		key = "count"
	}
	if segment != "" {
		return segmentedAnalyticsRows(distribution, order, xAxis, yAxis, segment, key, names)
	}
	return plainAnalyticsRows(distribution, order, xAxis, yAxis, key, names), nil
}

// analyticsExportFilterSQL translates the lookups issue_filters produced, joins and all. It is storedQueryConditions with the joins kept, because the export's scope can carry them where a saved view's cannot.
func analyticsExportFilterSQL(stored map[string]any) ([]string, []string, []any, bool) {
	filters := map[string]filterValue{}
	for lookup, value := range stored {
		switch typed := value.(type) {
		case bool:
			filters[lookup] = boolValue(typed)
		case string:
			filters[lookup] = textValue(typed)
		case []string:
			filters[lookup] = listValue(typed)
		case []any:
			items := make([]string, 0, len(typed))
			for _, item := range typed {
				text, ok := item.(string)
				if !ok {
					return nil, nil, nil, false
				}
				items = append(items, text)
			}
			filters[lookup] = listValue(items)
		default:
			return nil, nil, nil, false
		}
	}
	return issueFilterSQL(filters)
}

// plainAnalyticsRows is generate_non_segmented_rows: a heading pair and then one row per bucket, carrying only that bucket's own measure.
func plainAnalyticsRows(distribution map[string][]gin.H, order []string, xAxis, yAxis, key string, names analyticsNames) [][]any {
	rows := [][]any{{analyticsHeading(xAxis, "X-Axis"), analyticsHeading(yAxis, "Y-Axis")}}
	for _, bucket := range order {
		entries := distribution[bucket]
		var measure any
		if len(entries) > 0 {
			// Only the first entry is read, which is all a chart with no segment ever has.
			measure = analyticsMeasure(entries[0][key])
		}
		rows = append(rows, []any{names.lookup(xAxis, bucket), measure})
	}
	return rows
}

// segmentedAnalyticsRows is generate_segmented_rows: a heading row naming every segment, then one row per bucket carrying its total and then its share of each segment.
//
// Upstream builds the segment headings out of a python set, so their order is whatever that set iterates in — which changes between runs, since python randomises string hashing. They are sorted here instead. The columns are the same columns; only their order is decided rather than left to chance.
func segmentedAnalyticsRows(distribution map[string][]gin.H, order []string, xAxis, yAxis, segment, key string, names analyticsNames) ([][]any, error) {
	if segment == analyticsModuleAxis && names.has(analyticsLabelAxis) {
		return nil, errAnalyticsExportModuleSegment
	}
	segments := analyticsSegments(distribution)

	heading := []any{analyticsHeading(xAxis, "X-Axis"), analyticsHeading(yAxis, "Y-Axis")}
	for _, value := range segments {
		// A module segment is looked up among the labels upstream, so with no label axis it is never renamed at all.
		if segment == analyticsModuleAxis {
			heading = append(heading, value)
			continue
		}
		heading = append(heading, names.lookup(segment, value))
	}
	rows := [][]any{heading}

	for _, bucket := range order {
		entries := distribution[bucket]
		row := []any{names.lookup(xAxis, bucket), analyticsTotal(entries, key)}
		for _, value := range segments {
			// A bucket with nothing in one segment is written as the string zero rather than as a number or as nothing.
			var share any = "0"
			for _, entry := range entries {
				if analyticsSegmentOf(entry) == value {
					share = analyticsMeasure(entry[key])
					break
				}
			}
			row = append(row, share)
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// analyticsSegments collects the segments the chart has, which become the columns after the first two.
func analyticsSegments(distribution map[string][]gin.H) []string {
	seen := map[string]bool{}
	for _, entries := range distribution {
		for _, entry := range entries {
			seen[analyticsSegmentOf(entry)] = true
		}
	}
	segments := make([]string, 0, len(seen))
	for segment := range seen {
		segments = append(segments, segment)
	}
	sort.Strings(segments)
	return segments
}

// analyticsSegmentOf reads one row's segment, which is empty when the row's own segment column was null.
func analyticsSegmentOf(entry gin.H) string {
	value, _ := entry["segment"].(*string)
	if value == nil {
		return ""
	}
	return *value
}

// analyticsTotal is the sum over a bucket, skipping the entries whose measure is missing. An empty sum is the integer zero, which is what python's sum() starts from even when everything else is a float.
func analyticsTotal(entries []gin.H, key string) any {
	var whole int64
	var fraction float64
	isFloat := false
	for _, entry := range entries {
		switch typed := entry[key].(type) {
		case int64:
			whole += typed
		case *float64:
			if typed != nil {
				fraction += *typed
				isFloat = true
			}
		case float64:
			fraction += typed
			isFloat = true
		}
	}
	if isFloat {
		return fraction + float64(whole)
	}
	return whole
}

// analyticsMeasure is one bucket's measure, left as the number it is so the csv writer decides how to render it — and, more to the point, so the formula sanitiser sees a number rather than text and leaves a negative estimate alone.
func analyticsMeasure(value any) any {
	if typed, isPointer := value.(*float64); isPointer {
		if typed == nil {
			return nil
		}
		return *typed
	}
	return value
}

// analyticsHeading is row_mapping.get(axis, fallback).
func analyticsHeading(axis, fallback string) string {
	if heading, found := analyticsRowHeadings[axis]; found {
		return heading
	}
	return fallback
}

// analyticsNames are the lookup tables an id axis is made readable with, one table per axis. An axis nothing was read for has no table, and an id no table names keeps the id — which is what a reader sees when an assignee has no picture, since that lookup only lists the ones that do.
type analyticsNames map[string]map[string]string

func (names analyticsNames) has(axis string) bool {
	_, found := names[axis]
	return found
}

func (names analyticsNames) lookup(axis, value string) string {
	table, found := names[axis]
	if !found {
		return value
	}
	if name, found := table[value]; found {
		return name
	}
	return value
}
