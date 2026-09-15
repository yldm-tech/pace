package externalapi

import (
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api-go/internal/cycles"
)

// The five methods are on the two cycle paths, which is what lets the proxy cut them over by path.
func TestTheCycleRoutesCarryEveryMethod(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewHandler(nil, Settings{}).Register(router)

	const base = "/api/v1/workspaces/:slug/projects/:project/cycles/"
	wanted := map[string]bool{
		"GET " + base: false, "POST " + base: false,
		"GET " + base + ":cycle/": false, "PATCH " + base + ":cycle/": false,
		"DELETE " + base + ":cycle/": false,
	}
	for _, route := range router.Routes() {
		key := route.Method + " " + route.Path
		if _, listed := wanted[key]; listed {
			wanted[key] = true
		}
	}
	for route, found := range wanted {
		if !found {
			t.Errorf("%s is not served", route)
		}
	}
}

// The list carries the six counts and not the three estimates, because the queryset does not compute them and DRF drops a read-only field with nothing behind it.
func TestTheCycleListHasCountsAndNoEstimates(t *testing.T) {
	data := cycleListJSON(cycleRow{Cycle: Cycle{ID: "cycle-id"}})
	for _, field := range []string{
		"total_issues", "cancelled_issues", "completed_issues",
		"started_issues", "unstarted_issues", "backlog_issues",
	} {
		if _, present := data[field]; !present {
			t.Errorf("the list is missing %q", field)
		}
	}
	for _, field := range []string{"total_estimates", "completed_estimates", "started_estimates"} {
		if _, present := data[field]; present {
			t.Errorf("the list carries %q, which its queryset never annotates", field)
		}
	}
	if len(data) != 28 {
		t.Fatalf("the list row has %d fields, want 28", len(data))
	}
	// The create and the update answer over a plain instance, so none of the nine numbers is there at all.
	plain := cycleJSON(cycleRow{Cycle: Cycle{ID: "cycle-id"}}, false)
	if len(plain) != 22 {
		t.Fatalf("the create answers with %d fields, want 22", len(plain))
	}
}

// The date pair goes together: both or neither, and a null counts as neither.
func TestTheCycleDatePairMustMatch(t *testing.T) {
	both := rawBody(t, `{"start_date": "2026-04-01T00:00:00Z", "end_date": "2026-04-30T00:00:00Z"}`)
	_, hasStart := nonNullField(both, "start_date")
	_, hasEnd := nonNullField(both, "end_date")
	if !hasStart || !hasEnd {
		t.Error("a pair that is given reads as absent")
	}
	nulls := rawBody(t, `{"start_date": null, "end_date": null}`)
	_, hasStart = nonNullField(nulls, "start_date")
	_, hasEnd = nonNullField(nulls, "end_date")
	if hasStart || hasEnd {
		t.Error("a null date reads as given, and Django counts it as absent")
	}
	half := rawBody(t, `{"start_date": "2026-04-01T00:00:00Z"}`)
	_, hasStart = nonNullField(half, "start_date")
	_, hasEnd = nonNullField(half, "end_date")
	if hasStart == hasEnd {
		t.Error("half a pair does not read as half a pair")
	}
}

// Only the date part of what the caller sends survives: the pair is stored as the project's day boundaries.
func TestOnlyTheDayOfACycleDateSurvives(t *testing.T) {
	// A start becomes the first second of the day and an end its last minute, in the project's zone.
	start, end, ok := cycles.ConvertToUTC("2026-04-01", "2026-04-30", "Asia/Shanghai", time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC))
	if !ok {
		t.Fatal("the pair could not be converted")
	}
	if start.Second() != 1 || start.Minute() != 0 {
		t.Errorf("the start is %s, want the first second of the day", start)
	}
	if end.Hour() != 15 || end.Minute() != 59 {
		// 23:59 in Shanghai is 15:59 UTC.
		t.Errorf("the end is %s, want the last minute of the day", end)
	}
}
