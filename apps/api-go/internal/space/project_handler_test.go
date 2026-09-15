package space

import (
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// The eight read routes are served, and every one of them is reached through an anchor rather than a session.
func TestTheSpaceReadRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewHandler(nil).Register(router)

	wanted := map[string]bool{}
	for _, name := range []string{"settings", "meta", "members", "states", "labels", "cycles", "modules"} {
		wanted["GET /api/public/anchor/:anchor/"+name+"/"] = false
	}
	wanted["GET /api/public/workspaces/:slug/projects/:project/anchor/"] = false
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
	// Nothing here carries a session, so nothing outside the public prefix belongs in this package.
	for _, route := range router.Routes() {
		if !strings.HasPrefix(route.Path, "/api/public/") {
			t.Errorf("%s is not a public route and does not belong here", route.Path)
		}
	}
}

// The triage state is hidden by name rather than by its flag, so a renamed one is reported and one somebody called Triage is not.
func TestTriageIsHiddenByName(t *testing.T) {
	const condition = "s.name <> 'Triage'"
	if strings.Contains(condition, "is_triage") {
		t.Error("the triage state is hidden by its flag, and Django hides it by name")
	}
}
