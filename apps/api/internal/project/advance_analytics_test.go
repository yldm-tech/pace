package project

import (
	"strings"
	"testing"
	"time"
)

var analyticsNow = time.Date(2026, 9, 16, 11, 30, 0, 0, time.UTC)

// Each analytics window opens at the first instant of its first day and closes at the last microsecond of its last, which is what datetime.min.time() and datetime.max.time() come to.
func TestTheAnalyticsWindowsCoverWholeDays(t *testing.T) {
	for _, testCase := range []struct {
		filter string
		from   string
		to     string
	}{
		{"yesterday", "2026-09-15T00:00:00Z", "2026-09-15T23:59:59.999999Z"},
		{"last_7_days", "2026-09-09T00:00:00Z", "2026-09-16T23:59:59.999999Z"},
		{"last_30_days", "2026-08-17T00:00:00Z", "2026-09-16T23:59:59.999999Z"},
		{"last_3_months", "2026-06-18T00:00:00Z", "2026-09-16T23:59:59.999999Z"},
	} {
		window := analyticsDateRange(testCase.filter, analyticsNow)
		if window == nil {
			t.Errorf("%s produced no window", testCase.filter)
			continue
		}
		if got := window.from.Format(time.RFC3339Nano); got != testCase.from {
			t.Errorf("%s opens at %s, want %s", testCase.filter, got, testCase.from)
		}
		if got := window.to.Format(time.RFC3339Nano); got != testCase.to {
			t.Errorf("%s closes at %s, want %s", testCase.filter, got, testCase.to)
		}
	}
	// An absent or unrecognised filter is not an error; it simply leaves the count unnarrowed.
	for _, filter := range []string{"", "custom", "last_year", "nonsense"} {
		if analyticsDateRange(filter, analyticsNow) != nil {
			t.Errorf("%q produced a window", filter)
		}
	}
}

// The chart range is whole days rather than timestamps, and yesterday is a single day rather than a day and today.
func TestTheChartPeriodIsWholeDays(t *testing.T) {
	period := chartPeriodRange("yesterday", analyticsNow)
	if period == nil || period.start.Format("2006-01-02") != "2026-09-15" || period.end.Format("2006-01-02") != "2026-09-15" {
		t.Fatalf("yesterday is %#v", period)
	}
	period = chartPeriodRange("last_30_days", analyticsNow)
	if period == nil || period.start.Format("2006-01-02") != "2026-08-17" || period.end.Format("2006-01-02") != "2026-09-16" {
		t.Fatalf("last_30_days is %#v", period)
	}
	if chartPeriodRange("nonsense", analyticsNow) != nil {
		t.Error("an unknown filter produced a period")
	}
}

// The two date shapes do not overlap: a route asks for one of them and the other stays empty.
func TestARouteGetsOneDateShapeOnly(t *testing.T) {
	analytics := newAnalyticsFilters("acme", "person", "", "last_7_days", "analytics", analyticsNow)
	if analytics.window == nil || analytics.period != nil {
		t.Errorf("the analytics shape produced %#v and %#v", analytics.window, analytics.period)
	}
	chart := newAnalyticsFilters("acme", "person", "", "last_7_days", "chart", analyticsNow)
	if chart.period == nil || chart.window != nil {
		t.Errorf("the chart shape produced %#v and %#v", chart.window, chart.period)
	}
}

// Naming projects narrows both scopes, and naming none leaves them open.
func TestProjectIDsNarrowBothScopes(t *testing.T) {
	filters := newAnalyticsFilters("acme", "person", "one,two", "", "analytics", analyticsNow)
	if len(filters.projectIDs) != 2 {
		t.Fatalf("project ids = %#v", filters.projectIDs)
	}
	base, baseArguments := filters.baseScope("i")
	if !strings.Contains(base, "i.project_id IN ?") || len(baseArguments) != 3 {
		t.Errorf("base scope = %q with %d arguments", base, len(baseArguments))
	}
	project, projectArguments := filters.projectScope("p")
	if !strings.Contains(project, "p.id IN ?") || len(projectArguments) != 3 {
		t.Errorf("project scope = %q with %d arguments", project, len(projectArguments))
	}

	open := newAnalyticsFilters("acme", "person", "", "", "analytics", analyticsNow)
	if _, arguments := open.baseScope("i"); len(arguments) != 2 {
		t.Errorf("an unnarrowed base scope takes %d arguments, want 2", len(arguments))
	}
}

// A project the caller cannot reach is not counted, and neither is an archived or deleted one.
func TestTheScopesRefuseWhatTheCallerCannotSee(t *testing.T) {
	filters := newAnalyticsFilters("acme", "person", "", "", "analytics", analyticsNow)
	base, _ := filters.baseScope("i")
	for _, fragment := range []string{"apm.member_id = ?", "apm.is_active = TRUE", "ap.deleted_at IS NULL", "ap.archived_at IS NULL", "aw.slug = ?"} {
		if !strings.Contains(base, fragment) {
			t.Errorf("the base scope is missing %q", fragment)
		}
	}
	// A related lookup does not go through the model's manager, so a deleted membership still counts the project in.
	if strings.Contains(base, "apm.deleted_at") {
		t.Error("the base scope checks the membership's soft delete, and Django's join does not")
	}
}

// The projects chart labels each bar by its key with the underscores taken out.
func TestTheChartKeyNames(t *testing.T) {
	for key, want := range map[string]string{
		"work_items": "Work Items", "cycles": "Cycles", "intake": "Intake", "un_started_work_items": "Un Started Work Items",
	} {
		if got := chartKeyName(key); got != want {
			t.Errorf("%q is named %q, want %q", key, got, want)
		}
	}
}

// Thirteen axes and no more; anything else is refused rather than reaching the database.
func TestTheChartAxes(t *testing.T) {
	if len(chartAxes) != 13 {
		t.Fatalf("there are %d axes, want 13", len(chartAxes))
	}
	for _, name := range []string{"STATES", "STATE_GROUPS", "LABELS", "ASSIGNEES", "ESTIMATE_POINTS",
		"CYCLES", "MODULES", "PRIORITY", "START_DATE", "TARGET_DATE", "CREATED_AT", "COMPLETED_AT", "CREATED_BY"} {
		if _, known := chartAxes[name]; !known {
			t.Errorf("%q is not an axis", name)
		}
	}
	// The four relations carry their soft-delete rule on the same join the key is read from.
	for _, name := range []string{"LABELS", "ASSIGNEES", "CYCLES", "MODULES"} {
		if chartAxes[name].where == "" {
			t.Errorf("%q does not exclude a deleted link", name)
		}
	}
	// The estimate point is the one axis whose key is a number.
	numeric := 0
	for _, axis := range chartAxes {
		if axis.numeric {
			numeric++
		}
	}
	if numeric != 1 {
		t.Errorf("%d axes are numeric, want 1", numeric)
	}
}

// A missing key reads as the capitalised word, and the estimate point's key comes back as a number.
func TestTheChartValues(t *testing.T) {
	if got := chartValue(nil, chartAxes["PRIORITY"]); got != "None" {
		t.Errorf("a missing key is %v", got)
	}
	three := "3"
	if got := chartValue(&three, chartAxes["ESTIMATE_POINTS"]); got != 3 {
		t.Errorf("an estimate point key is %v (%T), want the number 3", got, got)
	}
	if got := chartValue(&three, chartAxes["PRIORITY"]); got != "3" {
		t.Errorf("a priority key is %v (%T), want the string", got, got)
	}
	if got := chartLabel(nil); got != "None" {
		t.Errorf("a missing name is %v", got)
	}
}

// The first of the month, whatever moment within it is asked about.
func TestMonthStart(t *testing.T) {
	got := monthStart(time.Date(2026, 9, 16, 23, 59, 59, 0, time.UTC))
	if got.Format(time.RFC3339) != "2026-09-01T00:00:00Z" {
		t.Errorf("the month starts at %s", got.Format(time.RFC3339))
	}
}

// The two refusals name which of the two fields was wrong rather than always blaming the first.
func TestTheChartRefusesEachFieldByName(t *testing.T) {
	if _, err := buildAnalyticsChart(nil, "NONSENSE", ""); err != errInvalidXAxis {
		t.Errorf("an unknown x_axis gives %v", err)
	}
	if _, err := buildAnalyticsChart(nil, "PRIORITY", "NONSENSE"); err != errInvalidGroupBy {
		t.Errorf("an unknown group_by gives %v", err)
	}
}
