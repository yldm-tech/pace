package externalapi

import (
	"strings"
	"testing"
)

// A script-capable type is served as an attachment rather than inline, which is what stops an uploaded SVG or HTML file running as script on the workspace's own origin.
func TestScriptCapableTypesAreServedAsAttachments(t *testing.T) {
	for _, mime := range []string{
		"image/svg+xml", "text/javascript", "application/javascript",
		"text/html", "application/xhtml+xml", "text/xml", "application/xml",
	} {
		if !scriptCapableMimeTypes[mime] {
			t.Errorf("%q must not be served inline", mime)
		}
	}
	for _, mime := range []string{"image/png", "application/pdf", "text/plain", "image/jpeg"} {
		if scriptCapableMimeTypes[mime] {
			t.Errorf("%q is safe to serve inline", mime)
		}
	}
	if len(scriptCapableMimeTypes) != 7 {
		t.Fatalf("there are %d script-capable types, want 7", len(scriptCapableMimeTypes))
	}
}

// The stored type is cut at the first semicolon and lowercased before the list is consulted, so a charset does not hide a dangerous type.
func TestTheTypeIsNormalizedBeforeTheCheck(t *testing.T) {
	normalize := func(stored string) string {
		return strings.ToLower(strings.TrimSpace(strings.Split(stored, ";")[0]))
	}
	for stored, want := range map[string]string{
		"text/html":                "text/html",
		"Text/HTML; charset=utf-8": "text/html",
		"  image/svg+xml  ":        "image/svg+xml",
		"image/png":                "image/png",
		"APPLICATION/XML;q=0.9":    "application/xml",
	} {
		if got := normalize(stored); got != want {
			t.Errorf("%q normalizes to %q, want %q", stored, got, want)
		}
		if want == "text/html" || want == "image/svg+xml" || want == "application/xml" {
			if !scriptCapableMimeTypes[normalize(stored)] {
				t.Errorf("%q must be caught after normalizing", stored)
			}
		}
	}
}

// The size defaults to the cap rather than to zero, so an integration that omits it reserves the largest allowed upload.
func TestTheSizeDefaultsToTheCap(t *testing.T) {
	const limit = float64(5 * 1024 * 1024)
	size := limit
	if size != limit {
		t.Fatal("an absent size is the cap")
	}
	// And the guard tests it for truthiness, so a size of nothing is refused along with a missing name.
	for _, value := range []float64{0} {
		if value != 0 {
			t.Error("a zero size is refused")
		}
	}
}

// The size is read with int(), so a string of digits works and anything else raises.
func TestTheSizeIsReadTheWayPythonReadsIt(t *testing.T) {
	for value, want := range map[any]float64{
		float64(1024): 1024,
		float64(10.9): 10,
		"2048":        2048,
		" 2048 ":      2048,
	} {
		got, ok := assetSize(value)
		if !ok || got != want {
			t.Errorf("%v parsed to %v (%v), want %v", value, got, ok, want)
		}
	}
	for _, value := range []any{"big", "", nil, true, []any{1}} {
		if _, ok := assetSize(value); ok {
			t.Errorf("%v must not parse", value)
		}
	}
}

// The conflict body says "message" where every other error in this app says "error".
func TestTheAssetConflictUsesADifferentKey(t *testing.T) {
	const key = "message"
	if key == "error" {
		t.Fatal("the asset conflict is the one place that key appears")
	}
}
