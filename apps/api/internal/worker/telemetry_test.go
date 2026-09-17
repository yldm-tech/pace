/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

package worker

import (
	"strings"
	"testing"
)

// TestNoEndpointMeansNoDestination covers the thing that used to be wrong here: the endpoint defaulted to the upstream project's collector, so an instance that configured nothing reported to a third party. There is no default now, and both url builders say so.
func TestNoEndpointMeansNoDestination(t *testing.T) {
	t.Setenv("OTLP_ENDPOINT", "")
	if got := otlpGRPCEndpoint(); got != "" {
		t.Errorf("otlpGRPCEndpoint() = %q with nothing configured, want empty", got)
	}
	if got := otlpHTTPMetricsURL(); got != "" {
		t.Errorf("otlpHTTPMetricsURL() = %q with nothing configured, want empty", got)
	}

	// And a configured one is still resolved the way it always was.
	t.Setenv("OTLP_ENDPOINT", "https://collector.example")
	if got := otlpGRPCEndpoint(); got != "collector.example:443" {
		t.Errorf("otlpGRPCEndpoint() = %q", got)
	}
	if got := otlpHTTPMetricsURL(); got != "https://collector.example/v1/metrics" {
		t.Errorf("otlpHTTPMetricsURL() = %q", got)
	}
	t.Setenv("OTLP_ENDPOINT", "collector.internal:4317")
	if got := otlpGRPCEndpoint(); got != "collector.internal:4317" {
		t.Errorf("otlpGRPCEndpoint() = %q", got)
	}
}

// TestEmailsDoNotHotlinkAnotherHost covers the other way this project used to reach out to the upstream one. The mark at the top of every template was fetched from their CDN, so opening a Pace email told them it had been opened, and from where.
func TestEmailsDoNotHotlinkAnotherHost(t *testing.T) {
	t.Setenv("WEB_URL", "https://pace.example")
	rendered, err := renderEmail("emails/auth/magic_signin.html", map[string]any{"code": "424242"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rendered, "plane.so") {
		t.Errorf("a rendered email still points at plane.so:\n%s", rendered[:min(len(rendered), 400)])
	}
	if !strings.Contains(rendered, "https://pace.example/favicon/") {
		t.Error("the rendered email does not load its mark from this deployment")
	}
}
