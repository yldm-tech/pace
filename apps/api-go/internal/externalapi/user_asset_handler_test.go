package externalapi

import (
	"testing"

	"github.com/gin-gonic/gin"
)

// The six routes are served, and the two under an id are served twice: once plainly and once with the server suffix.
func TestTheUserAssetRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewHandler(nil, Settings{}).Register(router)

	const base = "/api/v1/assets/user-assets/"
	wanted := map[string]bool{
		"POST " + base: false, "POST " + base + "server/": false,
		"PATCH " + base + ":asset/": false, "DELETE " + base + ":asset/": false,
		"PATCH " + base + ":asset/server/": false, "DELETE " + base + ":asset/server/": false,
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

// A profile image is held to a narrower list of types than a work item attachment.
func TestAProfileImageTakesFiveTypes(t *testing.T) {
	if len(userAvatarTypes) != 5 {
		t.Fatalf("a profile image takes %d types, want 5", len(userAvatarTypes))
	}
	for _, accepted := range []string{"image/jpeg", "image/png", "image/webp", "image/jpg", "image/gif"} {
		if !userAvatarTypes[accepted] {
			t.Errorf("%q is refused", accepted)
		}
	}
	for _, refused := range []string{"application/pdf", "image/svg+xml", "text/plain"} {
		if userAvatarTypes[refused] {
			t.Errorf("%q is accepted", refused)
		}
	}
	// And a work item attachment takes types this one does not, which is why the two lists are apart.
	if userAvatarTypes["application/pdf"] {
		t.Error("the two lists have been merged")
	}
}

// Only the two profile entities are allowed.
func TestAProfileImageIsAnAvatarOrACover(t *testing.T) {
	if len(userAssetEntityTypes) != 2 {
		t.Fatalf("a profile image takes %d entities, want 2", len(userAssetEntityTypes))
	}
	for _, entity := range []string{"USER_AVATAR", "USER_COVER"} {
		if !userAssetEntityTypes[entity] {
			t.Errorf("%q is refused", entity)
		}
	}
	if userAssetEntityTypes["ISSUE_ATTACHMENT"] {
		t.Error("a work item attachment passes as a profile image")
	}
}
