package externalapi

import "testing"

// A format suffix is the dot and what follows it on the last segment, with the trailing slash optional — which is what DRF's own pattern says.
func TestStrippingAFormatSuffix(t *testing.T) {
	cases := []struct {
		path     string
		stripped string
		format   string
		carried  bool
	}{
		{"/api/v1/workspaces/acme/stickies.json", "/api/v1/workspaces/acme/stickies/", "json", true},
		{"/api/v1/workspaces/acme/stickies.json/", "/api/v1/workspaces/acme/stickies/", "json", true},
		{"/api/v1/workspaces/acme/stickies/abc.json", "/api/v1/workspaces/acme/stickies/abc/", "json", true},
		{"/api/v1/workspaces/acme/stickies/abc.api", "/api/v1/workspaces/acme/stickies/abc/", "api", true},
		// Digits are part of the pattern too.
		{"/api/v1/workspaces/acme/stickies.j2", "/api/v1/workspaces/acme/stickies/", "j2", true},
		// No dot, a dot in an earlier segment, an empty suffix, and a suffix that is not lowercase letters or digits.
		{"/api/v1/workspaces/acme/stickies/", "", "", false},
		{"/api/v1/work.spaces/acme/stickies/", "", "", false},
		{"/api/v1/workspaces/acme/stickies.", "", "", false},
		{"/api/v1/workspaces/acme/stickies.JSON", "", "", false},
		{"/api/v1/workspaces/acme/stickies.j-son", "", "", false},
	}
	for _, testCase := range cases {
		stripped, format, carried := stripFormatSuffix(testCase.path)
		if carried != testCase.carried || stripped != testCase.stripped || format != testCase.format {
			t.Errorf("%q gave (%q, %q, %v) rather than (%q, %q, %v)",
				testCase.path, stripped, format, carried,
				testCase.stripped, testCase.format, testCase.carried)
		}
	}
}

// Only the two paths a DRF router owns take a suffix. Everything else under /api/v1/ is a plain path and a suffix on one is a 404, which is what DRF does with it too.
func TestOnlyARouterMountedPathTakesASuffix(t *testing.T) {
	for _, path := range []string{
		"/api/v1/workspaces/acme/stickies/",
		"/api/v1/workspaces/acme/stickies/abc/",
		"/api/v1/workspaces/acme/invitations/",
		"/api/v1/workspaces/acme/invitations/abc/",
	} {
		if !formatSuffixCandidate(path) {
			t.Errorf("%q is a router-mounted path and was refused", path)
		}
	}
	for _, path := range []string{
		"/api/v1/workspaces/acme/states/",
		"/api/v1/workspaces/acme/projects/abc/states/",
		"/api/v1/users/me/",
		"/api/workspaces/acme/stickies/",
		// One segment too deep for either of the two shapes a router registers.
		"/api/v1/workspaces/acme/stickies/abc/def/",
	} {
		if formatSuffixCandidate(path) {
			t.Errorf("%q is not a router-mounted path and was accepted", path)
		}
	}
}
