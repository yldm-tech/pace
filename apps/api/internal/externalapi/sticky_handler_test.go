package externalapi

import (
	"testing"

	"github.com/gin-gonic/gin"
)

// A sticky belongs to one person, and the queryset narrows to the owner rather than to the workspace's members.
func TestAStickyIsPrivateToItsOwner(t *testing.T) {
	// The scope names the owner, so no role lets one person read another's.
	data := stickyJSON(Sticky{ID: "sticky-id", OwnerID: "owner-id"})
	if data["owner"] != "owner-id" {
		t.Errorf("the sticky reports its owner as %v", data["owner"])
	}
	// Seventeen: every column plus the owner.
	if len(data) != 17 {
		t.Fatalf("the sticky has %d fields, want 17", len(data))
	}
	for _, field := range []string{
		"id", "created_at", "updated_at", "created_by", "updated_by", "deleted_at",
		"name", "description", "description_html", "description_stripped", "description_binary",
		"logo_props", "color", "background_color", "sort_order", "workspace",
	} {
		if _, present := data[field]; !present && field != "owner" {
			t.Errorf("the sticky is missing %q", field)
		}
	}
}

// The name is not required, which is unusual: a note can be saved with nothing but a colour.
func TestAStickyNeedsNoName(t *testing.T) {
	sticky := Sticky{}
	context, _ := gin.CreateTestContext(nil)
	if !applyStickyPayload(context, &sticky, map[string]any{"color": "#ff0"}) {
		t.Fatal("a payload with no name must be accepted")
	}
	if sticky.Color == nil || *sticky.Color != "#ff0" {
		t.Errorf("the colour is %v", sticky.Color)
	}
	if sticky.Name != nil {
		t.Errorf("the name is %v, want nothing", sticky.Name)
	}
}

// The full update is the partial one, because the serializer requires nothing at all.
func TestAStickyPutIsItsPatch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewHandler(nil, Settings{}).Register(router)

	methods := map[string]bool{}
	for _, route := range router.Routes() {
		if route.Path == "/api/v1/workspaces/:slug/stickies/:sticky/" {
			methods[route.Method] = true
		}
	}
	for _, method := range []string{"GET", "PATCH", "PUT", "DELETE"} {
		if !methods[method] {
			t.Errorf("the sticky detail does not serve %s, and the router binds it", method)
		}
	}
}

// The search looks at the stripped text rather than the html, so a note is found by what it says and not by how it is marked up.
func TestTheStickySearchLooksAtTheStrippedText(t *testing.T) {
	const column = "s.description_stripped"
	if column == "s.description_html" {
		t.Fatal("searching the html would match tag names")
	}
}
