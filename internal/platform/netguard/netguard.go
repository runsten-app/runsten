// Package netguard tells the addresses of the Internet from those of a network of its
// own: a server that connects to a host a user names (an MQTT broker) must not reach
// what sits beside it (its database, the cluster's API, the instance's metadata).
package netguard

import "net/netip"

// reserved are the ranges netip's predicates do not name, beside the private,
// loopback, link-local, multicast and unspecified ones.
var reserved = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),       // this network
	netip.MustParsePrefix("100.64.0.0/10"),   // carrier-grade NAT
	netip.MustParsePrefix("192.0.0.0/24"),    // IETF protocol assignments
	netip.MustParsePrefix("198.18.0.0/15"),   // benchmarking
	netip.MustParsePrefix("240.0.0.0/4"),     // reserved, and the broadcast
	netip.MustParsePrefix("64:ff9b::/96"),    // NAT64: an IPv4 address behind it
	netip.MustParsePrefix("64:ff9b:1::/48"),  // local-use NAT64
	netip.MustParsePrefix("2002::/16"),       // 6to4: an IPv4 address within
	netip.MustParsePrefix("2001::/32"),       // Teredo: an IPv4 address within
	netip.MustParsePrefix("100::/64"),        // discard-only
	netip.MustParsePrefix("fec0::/10"),       // site-local, deprecated but routed by some
	netip.MustParsePrefix("::ffff:0:0:0/96"), // IPv4-translated
}

// Public reports whether a is an address of the Internet: neither loopback, private
// (RFC 1918, unique local), link-local, multicast, unspecified, nor reserved, nor one
// that embeds an IPv4 address the network would translate.
func Public(a netip.Addr) bool {
	a = a.Unmap()
	if !a.IsValid() || a.IsLoopback() || a.IsPrivate() || a.IsLinkLocalUnicast() || a.IsLinkLocalMulticast() ||
		a.IsInterfaceLocalMulticast() || a.IsMulticast() || a.IsUnspecified() {
		return false
	}
	for _, p := range reserved {
		if p.Contains(a) {
			return false
		}
	}
	return true
}
