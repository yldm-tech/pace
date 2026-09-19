package webhooks

import (
	"testing"

	"github.com/gin-gonic/gin"
)

func TestWebhookRouteInventory(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewHandler(nil, nil, Settings{}).Register(router)
	expected := map[string]bool{
		"GET /api/workspaces/:slug/webhooks/":                      true,
		"POST /api/workspaces/:slug/webhooks/":                     true,
		"GET /api/workspaces/:slug/webhooks/:webhook/":             true,
		"PATCH /api/workspaces/:slug/webhooks/:webhook/":           true,
		"DELETE /api/workspaces/:slug/webhooks/:webhook/":          true,
		"POST /api/workspaces/:slug/webhooks/:webhook/regenerate/": true,
		"GET /api/workspaces/:slug/webhook-logs/:webhook/":         true,
	}
	for _, route := range router.Routes() {
		key := route.Method + " " + route.Path
		if !expected[key] {
			t.Fatalf("unexpected webhook route %s", key)
		}
		delete(expected, key)
	}
	if len(expected) != 0 {
		t.Fatalf("missing webhook routes: %#v", expected)
	}
}
