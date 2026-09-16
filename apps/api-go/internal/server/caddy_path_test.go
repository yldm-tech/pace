package server

import "testing"

// Caddy's own path matcher is not a prefix match and not a segment match: a star crosses slashes, and a path without one matches only itself.
func TestTheCaddyPathMatcher(t *testing.T) {
	cases := []struct {
		proxied string
		path    string
		covered bool
	}{
		// A path with no star matches itself and nothing under it.
		{"/api/users/me/", "/api/users/me/", true},
		{"/api/users/me/", "/api/users/me/profile/", false},
		{"/api/users/me/", "/api/users/me", false},
		// A trailing star takes the whole subtree, slashes included.
		{"/auth/*", "/auth/sign-in/", true},
		{"/auth/*", "/auth/spaces/google/callback/", true},
		{"/auth/*", "/auth/", true},
		{"/auth/*", "/authorise/", false},
		// A star in the middle crosses slashes too, which is the part that surprises.
		{"/api/users/me/workspaces/*/dashboard/", "/api/users/me/workspaces/*/dashboard/", true},
		{"/api/users/me/workspaces/*/dashboard/", "/api/users/me/workspaces/a/b/dashboard/", true},
		{"/api/users/me/workspaces/*/dashboard/", "/api/users/me/workspaces/a/dashboard", false},
	}
	for _, testCase := range cases {
		matcher := caddyPathMatcher(t, testCase.proxied)
		if got := matcher.MatchString(testCase.path); got != testCase.covered {
			t.Errorf("%q covering %q is %v rather than %v", testCase.proxied, testCase.path, got, testCase.covered)
		}
	}
}

// The guard reads both ways the Caddyfile cuts a path over, so neither kind can be added without it checking that Go serves every method on it.
func TestTheGuardReadsBothKindsOfProxiedPath(t *testing.T) {
	matchers := communityProxyMatchers(t)
	// One of each: a bare path on the reverse_proxy line, and a named path_regexp matcher.
	for _, path := range []string{"/auth/sign-in/", "/api/users/me/", "/api/workspaces/*/export-issues/", "/api/instances/", "/api/timezones/"} {
		if !anyMatcherCovers(matchers, path) {
			t.Errorf("%q is proxied to Go and the guard does not see it", path)
		}
	}
	// And something the proxy sends elsewhere, so the guard is not matching everything.
	//
	// It cannot be a path under /api/ any more. The fallback there is `reverse_proxy /api/* api-go:8000`, which claims the whole subtree, so the paths that are not cut over are the ones belonging to another service entirely.
	for _, path := range []string{"/live/collaboration/", "/spaces/an-anchor", "/god-mode/general"} {
		if anyMatcherCovers(matchers, path) {
			t.Errorf("%q belongs to another service and the guard thinks the API cuts it over", path)
		}
	}
}
