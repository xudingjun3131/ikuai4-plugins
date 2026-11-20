package core

import (
	"net"
	"strings"
)

func stringsEqualFold(a, b string) bool { return strings.EqualFold(a, b) }
func stringsHasSuffix(s, p string) bool { return strings.HasSuffix(s, p) }
func stringsContains(s, p string) bool  { return strings.Contains(s, p) }

// cidrContains is a stdlib-only CIDR check; invalid spec/ip simply returns
// false (never panics on device-supplied config).
func cidrContains(spec, ip string) bool {
	_, network, err := net.ParseCIDR(spec)
	if err != nil {
		return false
	}
	addr := net.ParseIP(ip)
	if addr == nil {
		return false
	}
	return network.Contains(addr)
}

// IsValidIPSpec validates user input before it reaches the ACL compiler,
// preventing injection into the generated nftables ruleset.
func IsValidIPSpec(s string) bool {
	if s == "" {
		return false
	}
	if net.ParseIP(s) != nil {
		return true
	}
	_, _, err := net.ParseCIDR(s)
	return err == nil
}
