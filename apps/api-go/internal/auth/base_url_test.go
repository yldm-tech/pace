// Copyright (c) 2023-present Plane Software, Inc. and contributors
// SPDX-License-Identifier: AGPL-3.0-only
// See the LICENSE file for details.

package auth

import "testing"

// TestBaseURLMatchesBaseHost pins baseURL to base_host in plane/authentication/utils/host.py:
//
//	base_origin = settings.WEB_URL or settings.APP_BASE_URL
//	is_space: SPACE_BASE_URL + space_base_path if SPACE_BASE_URL else base_origin + space_base_path
//	is_app:   APP_BASE_URL if APP_BASE_URL else base_origin
//
// The case that matters most is the ordinary one -- WEB_URL set and nothing else -- because neither variables.env nor any compose file in this repository sets APP_BASE_URL or SPACE_BASE_URL. That case used to send every redirect to http://localhost:3000.
func TestBaseURLMatchesBaseHost(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		web       string
		app       string
		spaceURL  string
		spacePath string
		wantApp   string
		wantSpace string
	}{
		{
			name:      "only WEB_URL, which is every installation that ships as it comes",
			web:       "http://localhost",
			spacePath: "/spaces/",
			wantApp:   "http://localhost",
			wantSpace: "http://localhost/spaces/",
		},
		{
			name:      "APP_BASE_URL wins for the app, and stands in for base_origin when WEB_URL is unset",
			app:       "https://app.example.test",
			spacePath: "/spaces/",
			wantApp:   "https://app.example.test",
			wantSpace: "https://app.example.test/spaces/",
		},
		{
			name:      "APP_BASE_URL beats WEB_URL for the app but not for the space origin",
			web:       "https://plane.example.test",
			app:       "https://app.example.test",
			spacePath: "/spaces/",
			wantApp:   "https://app.example.test",
			wantSpace: "https://plane.example.test/spaces/",
		},
		{
			name:      "SPACE_BASE_URL takes the space branch entirely",
			web:       "https://plane.example.test",
			spaceURL:  "https://sites.example.test",
			spacePath: "/spaces/",
			wantApp:   "https://plane.example.test",
			wantSpace: "https://sites.example.test/spaces/",
		},
		{
			name:      "a trailing slash on SPACE_BASE_URL does not double up against the path",
			spaceURL:  "https://sites.example.test/",
			spacePath: "/spaces/",
			wantApp:   "",
			wantSpace: "https://sites.example.test/spaces/",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			handler := &Handler{settings: Settings{
				WebURL: testCase.web, AppBaseURL: testCase.app,
				SpaceBaseURL: testCase.spaceURL, SpaceBasePath: testCase.spacePath,
			}}
			if got := handler.baseURL(false); got != testCase.wantApp {
				t.Errorf("baseURL(false) = %q, want %q", got, testCase.wantApp)
			}
			if got := handler.baseURL(true); got != testCase.wantSpace {
				t.Errorf("baseURL(true) = %q, want %q", got, testCase.wantSpace)
			}
		})
	}
}
