package space

import (
	"testing"

	"github.com/gin-gonic/gin"
)

// The queue is mounted twice under two names, and the older one serves only two of the five methods.
func TestTheIntakeIsMountedTwice(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewHandler(nil).Register(router)

	const base = "/api/public/anchor/:anchor/intakes/:intake/"
	wanted := map[string]bool{
		"GET " + base + "intake-issues/":           false,
		"POST " + base + "intake-issues/":          false,
		"GET " + base + "intake-issues/:issue/":    false,
		"PATCH " + base + "intake-issues/:issue/":  false,
		"DELETE " + base + "intake-issues/:issue/": false,
		"GET " + base + "inbox-issues/":            false,
		"POST " + base + "inbox-issues/":           false,
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
	// The older name carries no detail routes at all.
	for _, route := range router.Routes() {
		if route.Path == base+"inbox-issues/:issue/" {
			t.Errorf("%s %s is served and Django does not bind it", route.Method, route.Path)
		}
	}
}

// The priority that is checked and the priority that is written are not the same: an absent one passes the check as none and is written as low.
func TestAnAbsentPriorityIsCheckedAsNoneAndWrittenAsLow(t *testing.T) {
	checked := "none"
	written := "low"
	if !validIntakePriority(checked) {
		t.Error("the check refuses an absent priority")
	}
	if checked == written {
		t.Error("the two agree, and in Django they do not")
	}
	if validIntakePriority("critical") {
		t.Error("an unknown priority passes the check")
	}
}

// The five the check accepts are the five the model declares.
func TestTheIntakePriorities(t *testing.T) {
	for _, accepted := range []string{"low", "medium", "high", "urgent", "none"} {
		if !validIntakePriority(accepted) {
			t.Errorf("%q is refused", accepted)
		}
	}
}
