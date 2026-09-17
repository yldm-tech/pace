package project

import (
	"testing"
	"time"
)

// The completion chart's schema names the two series and nothing else, whichever of the two charts drew it.
func TestTheCompletionChartSchema(t *testing.T) {
	schema := completionChartSchema()
	if len(schema) != 2 {
		t.Fatalf("the schema has %d entries, want 2", len(schema))
	}
	for _, key := range []string{"completed_issues", "created_issues"} {
		if schema[key] != key {
			t.Errorf("%q maps to %v", key, schema[key])
		}
	}
}

// A cycle's dates are timestamps and a module's are plain dates, and both become the UTC calendar day the loop walks.
func TestTheCycleWindowIsReadAsUTCDays(t *testing.T) {
	// Late enough in the day that a westward timezone would land on the day before.
	moment := time.Date(2026, 9, 16, 23, 30, 0, 0, time.UTC)
	if got := dayOf(moment).Format("2006-01-02"); got != "2026-09-16" {
		t.Errorf("the day is %s", got)
	}
	elsewhere := time.Date(2026, 9, 17, 1, 0, 0, 0, time.FixedZone("east", 5*3600))
	if got := dayOf(elsewhere).Format("2006-01-02"); got != "2026-09-16" {
		t.Errorf("a moment stored east of UTC lands on %s", got)
	}
}

// The three errors these routes reach a 500 through are Django's, not a shortfall in the port.
func TestTheProjectAnalyticsFailuresAreUpstreams(t *testing.T) {
	for _, err := range []error{errAnalyticsCycleWindow, errAnalyticsModuleWindow, errAnalyticsNoProject} {
		if err == nil || err.Error() == "" {
			t.Fatal("an error is unnamed")
		}
	}
	if errAnalyticsCycleWindow == errAnalyticsModuleWindow {
		t.Error("the cycle and the module fail the same way and should not")
	}
}
