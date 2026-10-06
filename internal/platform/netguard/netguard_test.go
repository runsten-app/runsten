package netguard

import (
	"net/netip"
	"testing"
)

func TestPublic(t *testing.T) {
	for addr, want := range map[string]bool{
		"1.1.1.1":              true,
		"51.15.0.1":            true,
		"2606:4700:4700::1111": true,
		"127.0.0.1":            false,
		"::1":                  false,
		"10.0.0.1":             false,
		"172.16.5.4":           false,
		"192.168.1.10":         false,
		"fd00::1":              false,
		"169.254.169.254":      false, // the instance's metadata
		"fe80::1":              false,
		"0.0.0.0":              false,
		"::":                   false,
		"0.1.2.3":              false,
		"100.64.0.1":           false,
		"192.0.0.8":            false,
		"198.18.0.1":           false,
		"224.0.0.1":            false,
		"ff02::1":              false,
		"255.255.255.255":      false,
		"::ffff:10.0.0.1":      false, // IPv4-mapped
		"::ffff:1.1.1.1":       true,
		"64:ff9b::a00:1":       false, // NAT64 of 10.0.0.1
		"2002:a00:1::1":        false, // 6to4 of 10.0.0.1
		"2001:0:a00:1::1":      false, // Teredo
	} {
		if got := Public(netip.MustParseAddr(addr)); got != want {
			t.Errorf("Public(%s) = %v, want %v", addr, got, want)
		}
	}
	if Public(netip.Addr{}) {
		t.Error("the zero address is public")
	}
}
