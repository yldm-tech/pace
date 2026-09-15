package httpsafe

import (
	"net/netip"
	"testing"
)

// The expectations are plane.utils.ip_address.is_blocked_ip's own output. The
// full cross-check ran over 67288 addresses, including dense sweeps either side
// of every boundary in the tables; these are the boundaries worth keeping in
// the tree.
func TestIsBlockedMatchesPython(t *testing.T) {
	for _, test := range []struct {
		address string
		blocked bool
	}{
		{address: "8.8.8.8", blocked: false},
		{address: "1.1.1.1", blocked: false},
		{address: "203.0.113.5", blocked: true},
		{address: "198.51.100.7", blocked: true},
		{address: "192.0.2.1", blocked: true},
		{address: "93.184.216.34", blocked: false},
		{address: "0.0.0.0", blocked: true},
		{address: "0.0.0.1", blocked: true},
		{address: "0.255.255.255", blocked: true},
		{address: "1.0.0.0", blocked: false},
		{address: "10.0.0.1", blocked: true},
		{address: "9.255.255.255", blocked: false},
		{address: "11.0.0.0", blocked: false},
		{address: "100.63.255.255", blocked: false},
		{address: "100.64.0.1", blocked: true},
		{address: "100.127.255.255", blocked: true},
		{address: "100.128.0.0", blocked: false},
		{address: "127.0.0.1", blocked: true},
		{address: "126.255.255.255", blocked: false},
		{address: "128.0.0.0", blocked: false},
		{address: "169.254.169.254", blocked: true},
		{address: "169.253.255.255", blocked: false},
		{address: "169.255.0.0", blocked: false},
		{address: "172.15.255.255", blocked: false},
		{address: "172.16.0.1", blocked: true},
		{address: "172.31.255.255", blocked: true},
		{address: "172.32.0.0", blocked: false},
		{address: "192.168.1.1", blocked: true},
		{address: "192.167.255.255", blocked: false},
		{address: "192.169.0.0", blocked: false},
		{address: "198.17.255.255", blocked: false},
		{address: "198.18.0.1", blocked: true},
		{address: "198.19.255.255", blocked: true},
		{address: "198.20.0.0", blocked: false},
		{address: "224.0.0.1", blocked: true},
		{address: "223.255.255.255", blocked: false},
		{address: "239.255.255.255", blocked: true},
		{address: "240.0.0.0", blocked: true},
		{address: "255.255.255.255", blocked: true},
		{address: "255.255.255.254", blocked: true},
		{address: "192.0.0.1", blocked: true},
		{address: "192.0.0.170", blocked: true},
		{address: "192.0.0.8", blocked: true},
		{address: "::1", blocked: true},
		{address: "::", blocked: true},
		{address: "::2", blocked: true},
		{address: "2001:4860:4860::8888", blocked: false},
		{address: "2606:4700:4700::1111", blocked: false},
		{address: "fe80::1", blocked: true},
		{address: "febf:ffff::", blocked: true},
		{address: "fec0::1", blocked: true},
		{address: "feff::", blocked: true},
		{address: "fc00::1", blocked: true},
		{address: "fdff::", blocked: true},
		{address: "fe00::", blocked: true},
		{address: "ff00::1", blocked: true},
		{address: "ff02::1", blocked: true},
		{address: "::ffff:127.0.0.1", blocked: true},
		{address: "::ffff:8.8.8.8", blocked: true},
		{address: "::ffff:169.254.169.254", blocked: true},
		{address: "64:ff9b::7f00:1", blocked: true},
		{address: "64:ff9b::808:808", blocked: true},
		{address: "64:ff9b:1::1", blocked: true},
		{address: "2002:7f00:1::", blocked: true},
		{address: "2002:0808:0808::", blocked: true},
		{address: "2001::1", blocked: true},
		{address: "2001:0:4136:e378:8000:63bf:3fff:fdd2", blocked: true},
		{address: "2001:1::1", blocked: false},
		{address: "2001:db8::1", blocked: true},
		{address: "2001:10::1", blocked: true},
		{address: "2001:2::1", blocked: true},
		{address: "100::1", blocked: true},
		{address: "2000::1", blocked: false},
		{address: "3fff:ffff::", blocked: false},
		{address: "4000::1", blocked: true},
		{address: "efff:ffff::", blocked: true},
		{address: "f000::1", blocked: true},
		{address: "ffff:ffff::", blocked: true},
	} {
		address, err := netip.ParseAddr(test.address)
		if err != nil {
			t.Fatalf("parse %q: %v", test.address, err)
		}
		if got := IsBlocked(address); got != test.blocked {
			t.Errorf("IsBlocked(%s) = %v, want %v", test.address, got, test.blocked)
		}
	}
}

// The metadata endpoint and the transition formats that reach it are the
// reason this package exists, so they get their own named test.
func TestTransitionFormatsCannotReachInternalTargets(t *testing.T) {
	for _, literal := range []string{
		"169.254.169.254",        // cloud metadata, directly
		"::ffff:169.254.169.254", // IPv4-mapped
		"64:ff9b::a9fe:a9fe",     // NAT64 well-known prefix
		"2002:a9fe:a9fe::",       // 6to4
		"::ffff:127.0.0.1",       // loopback, mapped
		"64:ff9b::7f00:1",        // loopback via NAT64
		"2002:7f00:1::",          // loopback via 6to4
		"127.0.0.1", "10.0.0.1", "192.168.1.1", "172.16.0.1",
		"100.64.0.1", "0.0.0.0", "255.255.255.255", "::1", "fe80::1", "fc00::1",
	} {
		address, err := netip.ParseAddr(literal)
		if err != nil {
			t.Fatalf("parse %q: %v", literal, err)
		}
		if !IsBlocked(address) {
			t.Errorf("%s must never be an outbound target", literal)
		}
	}
}

func TestOrdinaryPublicAddressesStayReachable(t *testing.T) {
	for _, literal := range []string{
		"8.8.8.8", "1.1.1.1", "93.184.216.34",
		"2001:4860:4860::8888", "2606:4700:4700::1111",
		// CPython carves these back out of the private ranges.
		"192.0.0.9", "192.0.0.10", "2001:1::1", "2001:1::2",
	} {
		address, err := netip.ParseAddr(literal)
		if err != nil {
			t.Fatalf("parse %q: %v", literal, err)
		}
		if IsBlocked(address) {
			t.Errorf("%s should be reachable", literal)
		}
	}
}

func TestOperatorAllowlistOverridesTheBlock(t *testing.T) {
	address := netip.MustParseAddr("10.1.2.3")
	if !IsBlocked(address) {
		t.Fatal("a private address is blocked by default")
	}
	allowed := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	if !IsAllowed(address, allowed) {
		t.Fatal("the operator allowlist should cover it")
	}
	if IsAllowed(netip.MustParseAddr("192.168.1.1"), allowed) {
		t.Fatal("the allowlist should not cover an unrelated range")
	}
}

func TestHostAllowlistIgnoresCaseAndTrailingDot(t *testing.T) {
	hosts := []string{"internal.pace.test", "Other.Example"}
	for _, candidate := range []string{"internal.pace.test", "INTERNAL.pace.test", "internal.pace.test.", "other.example"} {
		if !HostAllowed(candidate, hosts) {
			t.Errorf("%q should be on the allowlist", candidate)
		}
	}
	if HostAllowed("evil.pace.test", hosts) {
		t.Error("an unlisted host must not be allowed")
	}
}

func TestAllowlistParsingMatchesSettings(t *testing.T) {
	prefixes, skipped := ParseAllowedIPs(" 10.0.0.0/8 , not-a-cidr ,, 192.168.1.5/24 ")
	if len(prefixes) != 2 {
		t.Fatalf("parsed %d prefixes, want 2", len(prefixes))
	}
	// settings.py parses with strict=False, so host bits are tolerated.
	if prefixes[1].String() != "192.168.1.0/24" {
		t.Fatalf("second prefix = %s, want the masked network", prefixes[1])
	}
	if len(skipped) != 1 || skipped[0] != "not-a-cidr" {
		t.Fatalf("skipped = %v, want the one invalid entry", skipped)
	}
	hosts := ParseAllowedHosts(" Internal.Pace.Test. ,, other.example ")
	if len(hosts) != 2 || hosts[0] != "internal.pace.test" || hosts[1] != "other.example" {
		t.Fatalf("hosts = %v", hosts)
	}
}
