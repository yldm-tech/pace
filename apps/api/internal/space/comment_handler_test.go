package space

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// The two reads carry no session and the three writes do, which is the one place in this app where that line runs through a single module.
func TestTheCommentReadsAreOpenAndTheWritesAreNot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewHandler(nil).Register(router)

	const base = "/api/public/anchor/:anchor/issues/:issue/comments/"
	wanted := map[string]bool{
		"GET " + base: false, "POST " + base: false,
		"GET " + base + ":comment/": false, "PATCH " + base + ":comment/": false,
		"DELETE " + base + ":comment/": false,
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

	// A write with no session manager is refused; a read is not.
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost,
		"/api/public/anchor/abc/issues/11111111-2222-3333-4444-555555555555/comments/", nil))
	if recorder.Code != http.StatusUnauthorized {
		t.Errorf("an anonymous comment answers %d, want 401", recorder.Code)
	}
}

// The caller is who the is_member flag is about, and the read routes have none.
func TestTheCallerIsNobodyOnARead(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	if got := callerID(c); got != "" {
		t.Errorf("a read has caller %q, want nobody", got)
	}
	c.Set("space.caller", "user-id")
	if got := callerID(c); got != "user-id" {
		t.Errorf("a write has caller %q", got)
	}
}

// A comment with no html has an empty stripped copy rather than a null, which is where it differs from a work item's description.
func TestAnEmptyCommentStripsToAnEmptyString(t *testing.T) {
	if got := stripTags(""); got != "" {
		t.Errorf("empty html strips to %q", got)
	}
	if got := stripTags("<p>hello <b>there</b></p>"); got != "hello there" {
		t.Errorf("the stripped copy is %q", got)
	}
}
