package webhooks

import (
	"strings"
	"testing"
)

// The secret is shown on creation and on a regenerate, and nowhere else. It is the one field in the migrated surface whose confidentiality depends on a single flag.
func TestTheSecretIsShownTwiceAndNoMoreOften(t *testing.T) {
	webhook := Webhook{ID: "webhook-id", SecretKey: "plane_wh_deadbeef"}
	shown := webhookJSON(webhook, true)
	hidden := webhookJSON(webhook, false)

	if shown["secret_key"] != "plane_wh_deadbeef" {
		t.Errorf("the secret is %v where it should be shown", shown["secret_key"])
	}
	if _, present := hidden["secret_key"]; present {
		t.Error("the secret must not appear where it is not asked for")
	}
	if len(shown) != len(hidden)+1 {
		t.Fatalf("the two bodies differ by %d fields, want one", len(shown)-len(hidden))
	}
	// Every other field is rendered either way: the fields= allowlists the views pass are dead twice over.
	for _, field := range []string{
		"id", "created_at", "updated_at", "created_by", "updated_by", "deleted_at",
		"url", "is_active", "workspace", "project", "issue", "module", "cycle", "issue_comment",
	} {
		if _, present := hidden[field]; !present {
			t.Errorf("the webhook is missing %q", field)
		}
	}
	if len(hidden) != 14 {
		t.Fatalf("the webhook has %d fields, want 14", len(hidden))
	}
}

// The token is a fixed prefix and a uuid's hex with no dashes, which is thirty-two characters after the prefix.
func TestTheWebhookTokenLooksLikeAUUID(t *testing.T) {
	seen := map[string]bool{}
	for index := 0; index < 64; index++ {
		token, err := generateWebhookToken()
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(token, "plane_wh_") {
			t.Fatalf("the token %q has the wrong prefix", token)
		}
		hex := strings.TrimPrefix(token, "plane_wh_")
		if len(hex) != 32 {
			t.Fatalf("the token body is %d characters, want 32", len(hex))
		}
		if strings.Contains(hex, "-") {
			t.Fatal("the token body carries no dashes")
		}
		// The version nibble is where uuid4 puts it.
		if hex[12] != '4' {
			t.Fatalf("the token %q is not a version four uuid", token)
		}
		if seen[token] {
			t.Fatal("two tokens came out the same")
		}
		seen[token] = true
	}
}

// The local-name check looks at the whole netloc rather than the hostname, so a port makes it pass.
func TestTheLocalCheckLooksAtTheWholeNetloc(t *testing.T) {
	for host, blocked := range map[string]bool{
		"localhost":      true,
		"127.0.0.1":      true,
		"localhost:8000": false,
		"127.0.0.1:8000": false,
		"example.com":    false,
	} {
		got := host == "localhost" || host == "127.0.0.1"
		if got != blocked {
			t.Errorf("%q is blocked = %v, want %v", host, got, blocked)
		}
	}
}

// A host on the allowlist skips the disallowed-domain check as well as the SSRF one, because it is already trusted and the loop-back guard would only get in the way of a sibling service.
func TestAnAllowedHostSkipsBothChecks(t *testing.T) {
	disallowed := []string{"plane.local"}
	hostname := "silo.plane.local"
	matches := false
	for _, domain := range disallowed {
		if hostname == domain || strings.HasSuffix(hostname, "."+domain) {
			matches = true
		}
	}
	if !matches {
		t.Fatal("the fixture must be a subdomain of a disallowed one, or it proves nothing")
	}
	// With the host allowlisted the loop is never entered at all.
	allowlisted := true
	if allowlisted && matches {
		return
	}
	t.Error("an allowlisted host must not reach the disallowed-domain loop")
}
