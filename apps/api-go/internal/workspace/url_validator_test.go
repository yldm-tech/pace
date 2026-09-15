package workspace

import (
	"strings"
	"testing"
)

// The expectations below were produced by running Django 5.2.15's URLValidator
// over the same inputs.
func TestURLValidatorMatchesDjango(t *testing.T) {
	for _, test := range []struct {
		value string
		want  bool
	}{
		{value: "http://example.com", want: true},
		{value: "https://example.com/", want: true},
		{value: "http://example.com/path?query=1#frag", want: true},
		{value: "http://www.example.co.uk", want: true},
		{value: "http://example.com.", want: true},
		{value: "http://localhost", want: true},
		{value: "http://localhost:3000/a", want: true},
		{value: "http://127.0.0.1:8000/", want: true},
		{value: "http://256.1.1.1", want: false},
		{value: "http://[::1]:8080/", want: true},
		{value: "http://[::1]", want: true},
		{value: "http://[1.2.3.4]", want: false},
		{value: "http://[zz::1]", want: false},
		{value: "http://user:pass@example.com/x", want: true},
		{value: "http://user@example.com", want: true},
		{value: "http://example", want: false},
		{value: "http://exa_mple.com", want: false},
		{value: "http://-example.com", want: false},
		{value: "http://example-.com", want: false},
		{value: "http://exam-ple.com", want: true},
		{value: "http://example.c", want: false},
		{value: "http://example.-com", want: false},
		{value: "http://example.com-", want: false},
		{value: "http://example.co-m", want: true},
		{value: "http://example.ab-.com", want: false},
		{value: "http://example.xn--p1ai", want: true},
		{value: "http://example.123", want: false},
		{value: "http://xn--80ak6aa92e.com", want: true},
		{value: "http://例え.テスト", want: true},
		{value: "http://例え.com", want: true},
		{value: "http://example.com:99999/", want: true},
		{value: "http://example.com:999999/", want: false},
		{value: "ftp://example.com", want: true},
		{value: "ftps://example.com", want: true},
		{value: "gopher://example.com", want: false},
		{value: "http://example.com/ space", want: false},
		{value: "http://example.com/%20ok", want: true},
		{value: "HTTP://EXAMPLE.COM", want: true},
		{value: "http://" + strings.Repeat("a", 250) + ".com", want: false},
		{value: "http://" + strings.Repeat(strings.Repeat("a", 63)+".", 4) + "com", want: false},
		{value: "http://example.com/" + strings.Repeat("a", 2100), want: false},
		{value: "http://", want: false},
		{value: "http://.com", want: false},
		{value: "http://example..com", want: false},
		{value: "http://example.com?q=1", want: true},
		{value: "http://example.com#f", want: true},
		{value: "http://1.2.3.4.5", want: false},
		{value: "http://0.0.0.0", want: true},
		{value: "http://010.1.1.1", want: false},
		{value: "example.com", want: false},
		{value: "http://example.com\twith-tab", want: false},
	} {
		if got := validURL(test.value); got != test.want {
			label := test.value
			if len(label) > 48 {
				label = label[:48] + "..."
			}
			t.Errorf("validURL(%q) = %v, want %v", label, got, test.want)
		}
	}
}

func TestQuickLinkURLSchemeIsPrefixedLikeDjango(t *testing.T) {
	for _, test := range []struct {
		value string
		want  string
	}{
		{value: "example.com", want: "http://example.com"},
		{value: "http://example.com", want: "http://example.com"},
		{value: "https://example.com", want: "https://example.com"},
		// Django compares against the lowercase prefixes, so an uppercase
		// scheme is prefixed again and then fails validation.
		{value: "HTTP://example.com", want: "http://HTTP://example.com"},
		{value: "", want: ""},
	} {
		if got := prefixQuickLinkURL(test.value); got != test.want {
			t.Errorf("prefixQuickLinkURL(%q) = %q, want %q", test.value, got, test.want)
		}
	}
	if validURL(prefixQuickLinkURL("HTTP://example.com")) {
		t.Error("double-prefixed URL should not validate")
	}
}
