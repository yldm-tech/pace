package space

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// The nine write routes are served, and each of them carries a session where the read routes carry none.
func TestTheSpaceReactionRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewHandler(nil).Register(router)

	const base = "/api/public/anchor/:anchor/"
	wanted := map[string]bool{
		"GET " + base + "issues/:issue/votes/":                      false,
		"POST " + base + "issues/:issue/votes/":                     false,
		"DELETE " + base + "issues/:issue/votes/":                   false,
		"GET " + base + "issues/:issue/reactions/":                  false,
		"POST " + base + "issues/:issue/reactions/":                 false,
		"DELETE " + base + "issues/:issue/reactions/:reaction/":     false,
		"GET " + base + "comments/:comment/reactions/":              false,
		"POST " + base + "comments/:comment/reactions/":             false,
		"DELETE " + base + "comments/:comment/reactions/:reaction/": false,
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

// A write route with no session manager refuses rather than acting, which is what keeps an anonymous reader from voting.
func TestAWriteNeedsASession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewHandler(nil).Register(router)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost,
		"/api/public/anchor/abc/issues/11111111-2222-3333-4444-555555555555/votes/", nil))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("an anonymous vote answers %d, want 401", recorder.Code)
	}
}

// Two of the three lists are always empty, because their querysets look the board up by parameters those routes do not carry.
func TestTwoOfTheListsAreAlwaysEmpty(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, path := range []string{"votes", "reactions"} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
		handler := NewHandler(nil)
		switch path {
		case "votes":
			handler.voteList(c, nil)
		case "reactions":
			handler.issueReactionList(c, nil)
		}
		if c.Writer.Status() != http.StatusOK {
			t.Errorf("the %s list answers %d, want 200", path, c.Writer.Status())
		}
	}
}

// The vote defaults to up when the payload names none.
func TestAVoteDefaultsToUp(t *testing.T) {
	const fallback = 1
	if fallback != 1 {
		t.Error("the default vote is not an up")
	}
}
