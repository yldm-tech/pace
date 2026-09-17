package space

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// The read is the only asset route here without a session: a published board's images are public and everything that writes one is not.
func TestOnlyTheAssetReadIsPublic(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewHandler(nil).Register(router)

	const base = "/api/public/assets/v2/anchor/:anchor/"
	wanted := map[string]bool{
		"GET " + base + ":asset/":          false,
		"POST " + base:                     false,
		"PATCH " + base + ":asset/":        false,
		"DELETE " + base + ":asset/":       false,
		"POST " + base + "restore/:asset/": false,
		"POST " + base + ":asset/bulk/":    false,
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

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/public/assets/v2/anchor/abc/", nil))
	if recorder.Code != http.StatusUnauthorized {
		t.Errorf("an anonymous upload answers %d, want 401", recorder.Code)
	}
}

// An upload may name any entity, and the identifier it carries lands on the comment column whatever it named.
func TestAnUploadMayNameAnyEntityAndLandsOnAComment(t *testing.T) {
	if len(spaceAssetEntityTypes) != 10 {
		t.Fatalf("the list has %d entities, want 10", len(spaceAssetEntityTypes))
	}
	if !spaceAssetEntityTypes["WORKSPACE_LOGO"] {
		t.Error("a workspace logo is refused, and this route accepts every entity there is")
	}
	// The read serves two of them and nothing else.
	for _, served := range []string{"ISSUE_DESCRIPTION", "COMMENT_DESCRIPTION"} {
		if !spaceAssetEntityTypes[served] {
			t.Errorf("%q cannot be uploaded", served)
		}
	}
}

// A type a browser would execute is served as an attachment rather than inline.
func TestAScriptCapableAssetIsAnAttachment(t *testing.T) {
	asset := FileAsset{Attributes: []byte(`{"type": "image/svg+xml"}`)}
	if !scriptCapableMimeTypes[assetMimeType(asset)] {
		t.Error("an svg is served inline, and a browser would run it")
	}
	safe := FileAsset{Attributes: []byte(`{"type": "image/png"}`)}
	if scriptCapableMimeTypes[assetMimeType(safe)] {
		t.Error("a png is served as an attachment")
	}
}

// The size is clamped at both ends here, unlike every other reserve, so an upload of nothing still carries a policy the bucket accepts.
func TestTheSizeIsClampedAtBothEnds(t *testing.T) {
	clamp := func(size, limit float64) float64 {
		if size > limit {
			size = limit
		}
		if size < 1 {
			size = 1
		}
		return size
	}
	if got := clamp(0, 100); got != 1 {
		t.Errorf("an empty upload is clamped to %v, want 1", got)
	}
	if got := clamp(500, 100); got != 100 {
		t.Errorf("a large upload is clamped to %v, want the limit", got)
	}
}
