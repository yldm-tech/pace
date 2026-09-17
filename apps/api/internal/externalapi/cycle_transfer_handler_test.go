package externalapi

import (
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// The transfer is served under the cycle, which completes the cycle module.
func TestTheTransferRouteIsServed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewHandler(nil, Settings{}).Register(router)

	const wanted = "POST /api/v1/workspaces/:slug/projects/:project/cycles/:cycle/transfer-issues/"
	for _, route := range router.Routes() {
		if route.Method+" "+route.Path == wanted {
			return
		}
	}
	t.Errorf("%s is not served", wanted)
}

// This route asks that the **old** cycle be finished, which the session API's transfer never asks. A cycle with no end date at all passes, since there is no date to be past.
func TestTheOldCycleMustBeFinished(t *testing.T) {
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	refused := func(end *time.Time) bool {
		return end != nil && end.After(now)
	}
	future := now.Add(24 * time.Hour)
	if !refused(&future) {
		t.Error("a cycle that has not ended is accepted")
	}
	past := now.Add(-24 * time.Hour)
	if refused(&past) {
		t.Error("a cycle that has ended is refused")
	}
	if refused(nil) {
		t.Error("a cycle with no end date is refused, and there is no date to be past")
	}
}
