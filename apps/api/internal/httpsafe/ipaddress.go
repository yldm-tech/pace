// Package httpsafe is the Go port of plane.utils.ip_address and
// plane.utils.url_security: the SSRF guard every outbound request Plane makes
// on a user-supplied URL goes through.
//
// The rule is not merely "reject private IPs". The Python original resolves the
// hostname, checks every returned address, and then connects to the validated
// IP literal rather than the name, so DNS cannot be rebound between the check
// and the connection. It also decodes IPv4 addresses embedded in IPv6
// transition formats, because that embedded address is what the packet actually
// reaches.
package httpsafe

import (
	"fmt"
	"net"
	"net/netip"
	"strings"
)

// blockedNetworks is _BLOCKED_NETWORKS: ranges the Python stdlib does not
// classify consistently across versions, listed explicitly so the verdict is
// the same everywhere and fails closed.
var blockedNetworks = mustParsePrefixes(
	"0.0.0.0/8",          // this host on this network
	"100.64.0.0/10",      // carrier-grade NAT
	"169.254.0.0/16",     // link-local, including cloud metadata
	"255.255.255.255/32", // limited broadcast
	"::ffff:0:0/96",      // IPv4-mapped IPv6
	"64:ff9b::/96",       // NAT64 well-known prefix
	"64:ff9b:1::/48",     // NAT64 local-use prefix
	"2002::/16",          // 6to4
	"2001::/32",          // Teredo
	"fec0::/10",          // deprecated IPv6 site-local
)

// The tables below are CPython's own, read out of
// ipaddress.IPv4Address._constants and IPv6Address._constants rather than
// transcribed from memory. Getting these by hand is how a hole gets opened: an
// earlier draft of this file used a much narrower IPv6 reserved list and would
// have allowed 791 of the 3298 addresses in the cross-check corpus that Python
// blocks.
var privateNetworksV4 = mustParsePrefixes(
	"0.0.0.0/8", "10.0.0.0/8", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12",
	"192.0.0.0/24", "192.0.0.170/31", "192.0.2.0/24", "192.168.0.0/16",
	"198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4",
	"255.255.255.255/32",
)

// CPython carves these back out of the private ranges above.
var privateExceptionsV4 = mustParsePrefixes("192.0.0.9/32", "192.0.0.10/32")

var privateNetworksV6 = mustParsePrefixes(
	"::1/128", "::/128", "::ffff:0:0/96", "64:ff9b:1::/48", "100::/64",
	"2001::/23", "2001:db8::/32", "2002::/16", "3fff::/20", "fc00::/7", "fe80::/10",
)

var privateExceptionsV6 = mustParsePrefixes(
	"2001:1::1/128", "2001:1::2/128", "2001:3::/32", "2001:4:112::/48",
	"2001:20::/28", "2001:30::/28",
)

var reservedNetworksV4 = mustParsePrefixes("240.0.0.0/4")

// Everything outside 2000::/3 is reserved, which is most of the address space.
var reservedNetworksV6 = mustParsePrefixes(
	"::/8", "100::/8", "200::/7", "400::/6", "800::/5", "1000::/4", "4000::/3",
	"6000::/3", "8000::/3", "a000::/3", "c000::/3", "e000::/4", "f000::/5",
	"f800::/6", "fe00::/9",
)

var multicastNetworks = mustParsePrefixes("224.0.0.0/4", "ff00::/8")
var linkLocalNetworks = mustParsePrefixes("169.254.0.0/16", "fe80::/10")
var loopbackNetworks = mustParsePrefixes("127.0.0.0/8", "::1/128")

func mustParsePrefixes(values ...string) []netip.Prefix {
	prefixes := make([]netip.Prefix, 0, len(values))
	for _, value := range values {
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			panic(fmt.Sprintf("httpsafe: bad prefix %q: %v", value, err))
		}
		prefixes = append(prefixes, prefix)
	}
	return prefixes
}

func withinAny(address netip.Addr, prefixes []netip.Prefix) bool {
	for _, prefix := range prefixes {
		if prefix.Addr().Is4() == address.Is4() && prefix.Contains(address) {
			return true
		}
	}
	return false
}

// IsBlocked is is_blocked_ip: an address that must never be an outbound target.
// It fails closed, and recurses into IPv4 addresses embedded in IPv6 transition
// formats because that embedded address is what the packet ultimately reaches.
func IsBlocked(address netip.Addr) bool {
	if !address.IsValid() {
		return true
	}
	address = address.WithZone("")
	if address.IsUnspecified() ||
		withinAny(address, loopbackNetworks) ||
		isPrivate(address) ||
		isReserved(address) ||
		withinAny(address, linkLocalNetworks) ||
		withinAny(address, multicastNetworks) {
		return true
	}
	if withinAny(address, blockedNetworks) {
		return true
	}
	for _, embedded := range embeddedIPv4(address) {
		if IsBlocked(embedded) {
			return true
		}
	}
	return false
}

// isPrivate is CPython's is_private: inside a private range and outside every
// carve-out.
func isPrivate(address netip.Addr) bool {
	networks, exceptions := privateNetworksV6, privateExceptionsV6
	if address.Is4() {
		networks, exceptions = privateNetworksV4, privateExceptionsV4
	}
	return withinAny(address, networks) && !withinAny(address, exceptions)
}

func isReserved(address netip.Addr) bool {
	if address.Is4() {
		return withinAny(address, reservedNetworksV4)
	}
	return withinAny(address, reservedNetworksV6)
}

// embeddedIPv4 is _embedded_ipv4: the IPv4 addresses hidden inside an IPv6
// transition address.
func embeddedIPv4(address netip.Addr) []netip.Addr {
	if address.Is4() {
		return nil
	}
	var embedded []netip.Addr
	bytes := address.As16()

	if address.Is4In6() {
		if mapped, ok := netip.AddrFromSlice(bytes[12:]); ok {
			embedded = append(embedded, mapped)
		}
	}
	// 6to4: 2002::/16 carries the IPv4 in the next 32 bits.
	if bytes[0] == 0x20 && bytes[1] == 0x02 {
		if sixtofour, ok := netip.AddrFromSlice(bytes[2:6]); ok {
			embedded = append(embedded, sixtofour)
		}
	}
	// Teredo: 2001:0000::/32 carries the server in bits 32-63 and the
	// obfuscated client in the last 32 bits.
	if bytes[0] == 0x20 && bytes[1] == 0x01 && bytes[2] == 0x00 && bytes[3] == 0x00 {
		if server, ok := netip.AddrFromSlice(bytes[4:8]); ok {
			embedded = append(embedded, server)
		}
		client := []byte{^bytes[12], ^bytes[13], ^bytes[14], ^bytes[15]}
		if parsed, ok := netip.AddrFromSlice(client); ok {
			embedded = append(embedded, parsed)
		}
	}
	// NAT64 well-known prefix 64:ff9b::/96 embeds the IPv4 in the low 32 bits.
	if nat64 := netip.MustParsePrefix("64:ff9b::/96"); nat64.Contains(address) {
		if parsed, ok := netip.AddrFromSlice(bytes[12:]); ok {
			embedded = append(embedded, parsed)
		}
	}
	return embedded
}

// IsAllowed is _is_allowed_ip: an operator-trusted network overrides the block.
func IsAllowed(address netip.Addr, allowed []netip.Prefix) bool {
	return withinAny(address, allowed)
}

// ResolveAndValidate is resolve_and_validate: resolve the hostname and, unless
// the host is operator-trusted, require every returned address to be a safe
// target. The returned addresses are what the connection must be pinned to.
func ResolveAndValidate(hostname string, allowed []netip.Prefix, requireSafe bool) ([]netip.Addr, error) {
	addresses, err := net.LookupIP(hostname)
	if err != nil {
		return nil, fmt.Errorf("Hostname could not be resolved")
	}
	if len(addresses) == 0 {
		return nil, fmt.Errorf("No IP addresses found for the hostname")
	}
	validated := make([]netip.Addr, 0, len(addresses))
	seen := map[string]bool{}
	for _, address := range addresses {
		parsed, ok := netip.AddrFromSlice(address)
		if !ok {
			continue
		}
		parsed = parsed.Unmap().WithZone("")
		if requireSafe && !IsAllowed(parsed, allowed) && IsBlocked(parsed) {
			return nil, fmt.Errorf("Access to private/internal networks is not allowed")
		}
		if !seen[parsed.String()] {
			seen[parsed.String()] = true
			validated = append(validated, parsed)
		}
	}
	if len(validated) == 0 {
		return nil, fmt.Errorf("No IP addresses found for the hostname")
	}
	return validated, nil
}

// HostAllowed reports whether the hostname is on the operator's allowlist,
// which skips the block check but not the pinning.
func HostAllowed(hostname string, allowedHosts []string) bool {
	normalized := strings.ToLower(strings.TrimSuffix(hostname, "."))
	for _, candidate := range allowedHosts {
		if normalized == strings.ToLower(strings.TrimSuffix(candidate, ".")) {
			return true
		}
	}
	return false
}
