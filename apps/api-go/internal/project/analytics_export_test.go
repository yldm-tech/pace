package project

import (
	"testing"

	"github.com/gin-gonic/gin"
)

func countEntry(dimension string, count int64) gin.H {
	return gin.H{"dimension": dimension, "count": count}
}

func segmentEntry(dimension, segment string, count int64) gin.H {
	value := segment
	return gin.H{"dimension": dimension, "segment": &value, "count": count}
}

// A chart with no segment is a heading pair and one row per bucket, and each row carries only that bucket's own measure.
func TestThePlainExportRows(t *testing.T) {
	distribution := map[string][]gin.H{
		"urgent": {countEntry("urgent", 3)},
		"low":    {countEntry("low", 1)},
	}
	rows := plainAnalyticsRows(distribution, []string{"urgent", "low"}, "priority", "issue_count", "count", analyticsNames{})
	want := [][]any{
		{"Priority", "Issue Count"},
		{"urgent", int64(3)},
		{"low", int64(1)},
	}
	compareExportRows(t, rows, want)
}

// A chart grouped by state is headed "X-Axis", because row_mapping names state__name — which is not a valid axis — and does not name state_id, which is.
func TestAStateAxisHasNoHeading(t *testing.T) {
	rows := plainAnalyticsRows(map[string][]gin.H{}, nil, analyticsStateAxis, "issue_count", "count", analyticsNames{})
	if rows[0][0] != "X-Axis" {
		t.Errorf("the state heading is %v", rows[0][0])
	}
	// The measure's own heading is named, so only the first column is affected.
	if rows[0][1] != "Issue Count" {
		t.Errorf("the measure heading is %v", rows[0][1])
	}
}

// An id axis is replaced with the name its lookup table carries, and an id the table does not name keeps the id — which is what a reader sees when an assignee has no picture, since that table only lists the ones that do.
func TestAnIDAxisIsMadeReadable(t *testing.T) {
	names := analyticsNames{analyticsAssigneeAxis: {"ada": "Ada Lovelace"}}
	distribution := map[string][]gin.H{
		"ada":     {countEntry("ada", 2)},
		"unknown": {countEntry("unknown", 1)},
	}
	rows := plainAnalyticsRows(distribution, []string{"ada", "unknown"}, analyticsAssigneeAxis, "issue_count", "count", names)
	if rows[1][0] != "Ada Lovelace" {
		t.Errorf("the named assignee is %v", rows[1][0])
	}
	if rows[2][0] != "unknown" {
		t.Errorf("the unnamed assignee is %v", rows[2][0])
	}
	if rows[0][0] != "Assignee Name" {
		t.Errorf("the heading is %v", rows[0][0])
	}
}

// A segmented chart carries a total and then each segment's share, and a bucket with nothing in a segment gets the string zero rather than a number.
func TestTheSegmentedExportRows(t *testing.T) {
	distribution := map[string][]gin.H{
		"urgent": {segmentEntry("urgent", "done", 2), segmentEntry("urgent", "todo", 1)},
		"low":    {segmentEntry("low", "todo", 5)},
	}
	rows, err := segmentedAnalyticsRows(distribution, []string{"urgent", "low"}, "priority", "issue_count", "state__group", "count", analyticsNames{})
	if err != nil {
		t.Fatalf("building the rows: %v", err)
	}
	want := [][]any{
		{"Priority", "Issue Count", "done", "todo"},
		{"urgent", int64(3), int64(2), int64(1)},
		{"low", int64(5), "0", int64(5)},
	}
	compareExportRows(t, rows, want)
}

// A chart segmented by module and grouped by label is never delivered, because the module segments are looked up among the label rows and asking a label for its module id raises.
func TestAModuleSegmentBesideALabelAxis(t *testing.T) {
	distribution := map[string][]gin.H{"one": {segmentEntry("one", "mod", 1)}}
	names := analyticsNames{analyticsLabelAxis: {"one": "Backend"}, analyticsModuleAxis: {"mod": "Launch"}}
	if _, err := segmentedAnalyticsRows(distribution, []string{"one"}, analyticsLabelAxis, "issue_count", analyticsModuleAxis, "count", names); err == nil {
		t.Error("the export was built, and upstream raises here")
	}

	// Without a label axis the lookup finds nothing instead of raising, so the segment columns keep their raw ids.
	rows, err := segmentedAnalyticsRows(distribution, []string{"one"}, "priority", "issue_count", analyticsModuleAxis, "count",
		analyticsNames{analyticsModuleAxis: {"mod": "Launch"}})
	if err != nil {
		t.Fatalf("building the rows: %v", err)
	}
	if rows[0][2] != "mod" {
		t.Errorf("the module segment heading is %v, and upstream never renames it", rows[0][2])
	}
}

// An estimate is summed as a float and kept as one, so a whole total still reads with its decimal point in the file.
func TestTheEstimateTotal(t *testing.T) {
	half, whole := 2.5, 1.5
	entries := []gin.H{
		{"dimension": "a", "estimate": &half},
		{"dimension": "a", "estimate": &whole},
		{"dimension": "a", "estimate": (*float64)(nil)},
	}
	if got := analyticsTotal(entries, "estimate"); got != 4.0 {
		t.Errorf("the total is %#v", got)
	}
	// A bucket where every measure is missing sums to the integer zero, which is what python's sum() starts from.
	if got := analyticsTotal([]gin.H{{"dimension": "a", "estimate": (*float64)(nil)}}, "estimate"); got != int64(0) {
		t.Errorf("an empty total is %#v", got)
	}
}

func compareExportRows(t *testing.T, got, want [][]any) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("there are %d rows rather than %d: %#v", len(got), len(want), got)
	}
	for index, wantRow := range want {
		if len(got[index]) != len(wantRow) {
			t.Errorf("row %d has %d cells rather than %d: %#v", index, len(got[index]), len(wantRow), got[index])
			continue
		}
		for column, wantCell := range wantRow {
			if got[index][column] != wantCell {
				t.Errorf("row %d column %d is %#v rather than %#v", index, column, got[index][column], wantCell)
			}
		}
	}
}
