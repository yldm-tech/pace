package user

import (
	"strings"
	"testing"
)

// The key itself is reported by the create and the update and by nothing else, so a key not written down when it was made cannot be recovered.
func TestOnlyTwoRoutesReportTheKey(t *testing.T) {
	token := APIToken{ID: "token-id", Token: "plane_api_abc"}
	shown := tokenJSON(token, true)
	hidden := tokenJSON(token, false)
	if _, present := shown["token"]; !present {
		t.Error("the create does not report the key")
	}
	if _, present := hidden["token"]; present {
		t.Error("a read reports the key")
	}
	if len(shown) != len(hidden)+1 {
		t.Fatalf("the two shapes are %d and %d fields", len(shown), len(hidden))
	}
	if len(hidden) != 16 {
		t.Fatalf("a read has %d fields, want 16", len(hidden))
	}
}

// A key is a fixed prefix and thirty-two hexadecimal characters, which is what makes one recognisable in a log.
func TestTheKeyShape(t *testing.T) {
	key := "plane_api_" + hexIdentifier()
	if !strings.HasPrefix(key, "plane_api_") {
		t.Errorf("the key is %q", key)
	}
	suffix := strings.TrimPrefix(key, "plane_api_")
	if len(suffix) != 32 {
		t.Errorf("the suffix is %d characters, want 32", len(suffix))
	}
	if strings.Contains(suffix, "-") {
		t.Error("the suffix carries dashes, and hex has none")
	}
	// An unnamed key takes a label of the same shape.
	if len(hexIdentifier()) != 32 {
		t.Error("a label is not thirty-two characters")
	}
}

// The expiry takes what DRF takes, including a day on its own.
func TestTheExpiryFormats(t *testing.T) {
	for _, value := range []string{"2026-01-02T15:04:05Z", "2026-01-02T15:04:05", "2026-01-02T15:04", "2026-01-02"} {
		if _, ok := parseTokenExpiry(value); !ok {
			t.Errorf("%q is refused", value)
		}
	}
	if _, ok := parseTokenExpiry("nonsense"); ok {
		t.Error("nonsense is accepted")
	}
}
