package auth

import (
	"net"
	"testing"
)

func TestValidateAvatarURLRejectsNonHTTPAndCredentials(t *testing.T) {
	for _, value := range []string{
		"file:///etc/passwd", "ftp://example.com/avatar.png", "http://user:pass@example.com/avatar.png", "not a URL",
	} {
		if _, err := validateAvatarURL(value); err == nil {
			t.Fatalf("URL %q should be rejected", value)
		}
	}
	if parsed, err := validateAvatarURL("https://cdn.pace.test/avatar.png"); err != nil || parsed.Hostname() != "cdn.pace.test" {
		t.Fatalf("valid avatar URL = %#v, err=%v", parsed, err)
	}
}

func TestIsPublicIPRejectsPrivateAndLoopbackAddresses(t *testing.T) {
	for _, value := range []string{"127.0.0.1", "10.0.0.1", "172.16.0.1", "192.168.0.1", "::1", "fc00::1"} {
		if isPublicIP(parseIP(t, value)) {
			t.Fatalf("private address %s was accepted", value)
		}
	}
}

func parseIP(t *testing.T, value string) net.IP {
	t.Helper()
	parsed := net.ParseIP(value)
	if parsed == nil {
		t.Fatalf("invalid test IP %s", value)
	}
	return parsed
}
