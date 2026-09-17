// Package validate holds the Django and DRF validators the API reproduces, for
// the fields where several modules share the same rule.
package validate

import (
	"net"
	"regexp"
	"strings"
	"unicode/utf8"
)

// URL is Django's URLValidator, ported from django/core/validators.py. Its regex uses
// lookaround to keep domain labels from starting or ending with a dash; RE2 has
// no lookaround, so each label is spelled out as "first and last character
// cannot be a dash" instead, which accepts the same strings.
const (
	urlValidatorMaxLength = 2048
	urlHostnameMaxLength  = 253
	// ul in Django: the Unicode letters range, deliberately capped at U+FFFF.
	urlUnicodeLetters = `\x{00a1}-\x{ffff}`
	// Python's \s covers more than Go's, so the extra code points are listed.
	urlNonSpace = `\s\v\x{001c}-\x{001f}\x{0085}\p{Z}`
)

var urlValidatorPattern = regexp.MustCompile(buildURLValidatorPattern())

var urlBracketedHostPattern = regexp.MustCompile(`^\[(.+)\](?::[0-9]{1,5})?$`)

var urlSchemePattern = regexp.MustCompile(`^[a-zA-Z0-9.+-]*$`)

var urlSchemes = map[string]struct{}{"http": {}, "https": {}, "ftp": {}, "ftps": {}}

func buildURLValidatorPattern() string {
	octet := `(?:0|25[0-5]|2[0-4][0-9]|1[0-9]?[0-9]?|[1-9][0-9]?)`
	ipv4 := octet + `(?:\.` + octet + `){3}`
	ipv6 := `\[[0-9a-f:.]+\]`

	letters := `a-z` + urlUnicodeLetters
	hostname := `[` + letters + `0-9](?:[` + letters + `0-9-]{0,61}[` + letters + `0-9])?`
	// Labels are 1-63 characters and may not start or end with a dash.
	label := `(?:[` + letters + `0-9]|[` + letters + `0-9][` + letters + `0-9-]{0,61}[` + letters + `0-9])`
	domain := `(?:\.` + label + `)*`
	// The top-level domain accepts letters and dashes but no digits, or a
	// punycode label; it may carry a trailing dot for a fully qualified name.
	tld := `\.(?:[` + letters + `][` + letters + `-]{0,61}[` + letters + `]|xn--[a-z0-9]{1,59})\.?`
	host := `(?:` + hostname + domain + tld + `|localhost)`

	userInfo := `(?:[^` + urlNonSpace + `:@/]+(?::[^` + urlNonSpace + `:@/]*)?@)?`
	port := `(?::[0-9]{1,5})?`
	path := `(?:[/?#][^` + urlNonSpace + `]*)?`

	return `(?i)^[a-z0-9.+-]*://` + userInfo + `(?:` + ipv4 + `|` + ipv6 + `|` + host + `)` + port + path + `$`
}

// URL mirrors URLValidator.__call__ for the default scheme list. Both the
// workspace quick link and the issue link serializers use it.
func URL(value string) bool {
	if utf8.RuneCountInString(value) > urlValidatorMaxLength {
		return false
	}
	if strings.ContainsAny(value, "\t\r\n") {
		return false
	}
	separator := strings.Index(value, "://")
	if separator < 0 {
		// Django splits on "://" and checks the leading segment against the
		// scheme list, so a URL without the separator never has a valid scheme.
		return false
	}
	if _, ok := urlSchemes[strings.ToLower(value[:separator])]; !ok {
		return false
	}
	if !urlSchemePattern.MatchString(value[:separator]) {
		return false
	}
	if !urlValidatorPattern.MatchString(value) {
		return false
	}
	netloc := value[separator+3:]
	if cut := strings.IndexAny(netloc, "/?#"); cut >= 0 {
		netloc = netloc[:cut]
	}
	// Django validates the bracketed address against the whole netloc, before
	// the user information is stripped.
	if match := urlBracketedHostPattern.FindStringSubmatch(netloc); match != nil {
		if !validIPv6Address(match[1]) {
			return false
		}
	}
	return utf8.RuneCountInString(urlHostname(netloc)) <= urlHostnameMaxLength
}

// urlHostname reproduces urlsplit().hostname: the netloc without its user
// information, brackets, and port, lowercased.
func urlHostname(netloc string) string {
	if at := strings.LastIndex(netloc, "@"); at >= 0 {
		netloc = netloc[at+1:]
	}
	if strings.HasPrefix(netloc, "[") {
		if end := strings.Index(netloc, "]"); end >= 0 {
			return strings.ToLower(netloc[1:end])
		}
		return strings.ToLower(netloc)
	}
	if colon := strings.Index(netloc, ":"); colon >= 0 {
		netloc = netloc[:colon]
	}
	return strings.ToLower(netloc)
}

// validIPv6Address matches Django's validate_ipv6_address, which rejects the
// IPv4 addresses that Go's parser would otherwise accept.
func validIPv6Address(value string) bool {
	return strings.Contains(value, ":") && net.ParseIP(value) != nil
}

// PrefixScheme mirrors the to_internal_value both link serializers run before
// validation: a URL that does not already start with the lowercase http:// or
// https:// gains an http:// prefix. The comparison is case sensitive in Django,
// so an uppercase scheme is prefixed a second time and then fails validation.
func PrefixScheme(value string) string {
	if value == "" || strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		return value
	}
	return "http://" + value
}
