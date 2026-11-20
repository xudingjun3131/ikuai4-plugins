package core

import (
	"fmt"
	"sort"
	"strings"
)

// ACL compiles the unified policy + threat IP set into a deterministic
// nftables ruleset. Borrowed strength: OPNsense/pfSense separate "what the
// admin intends" (policy objects) from "what the kernel enforces" (ruleset),
// and expose the rendered ruleset for review BEFORE applying it. Rendering is
// a pure function so it is fully unit-testable without root or nft installed.
//
// Honest boundary: DNS/domain filtering is done in userspace by the DNS proxy;
// IP-level enforcement (threat intel, blocked subnets, DNS hijack redirect)
// is what nftables does. Applying requires root and firmware with nft support,
// so service.sh gates the apply behind an explicit enable flag.

type ACLConfig struct {
	Table       string   `json:"table"`
	ChainV4     bool     `json:"chain_v4"`
	BlockIPs    []string `json:"block_ips"`    // IP or CIDR, v4
	AllowIPs    []string `json:"allow_ips"`    // explicit bypass
	DNSPort     int      `json:"dns_port"`     // redirect LAN UDP/TCP 53 here
	RedirectDNS bool     `json:"redirect_dns"` // hijack LAN DNS to local sinkhole
	LANIfaces   []string `json:"lan_ifaces"`   // optional iifname restriction
}

func DefaultACLConfig() ACLConfig {
	return ACLConfig{Table: "netfence", ChainV4: true, DNSPort: 18110, RedirectDNS: false}
}

func RenderACL(c ACLConfig) (string, error) {
	if c.Table == "" {
		c.Table = "netfence"
	}
	if !validTableName(c.Table) {
		return "", fmt.Errorf("非法表名 %q", c.Table)
	}
	var block, allow []string
	for _, s := range c.BlockIPs {
		if !IsValidIPSpec(s) {
			return "", fmt.Errorf("非法封禁地址 %q", s)
		}
		block = append(block, s)
	}
	for _, s := range c.AllowIPs {
		if !IsValidIPSpec(s) {
			return "", fmt.Errorf("非法放行地址 %q", s)
		}
		allow = append(allow, s)
	}
	sort.Strings(block)
	sort.Strings(allow)
	for _, i := range c.LANIfaces {
		if !validIface(i) {
			return "", fmt.Errorf("非法接口名 %q", i)
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "#!/usr/sbin/nft -f\n")
	fmt.Fprintf(&b, "# netfence generated ruleset — DO NOT EDIT, regenerate from policy UI\n")
	fmt.Fprintf(&b, "table inet %s {\n", c.Table)
	if len(block) > 0 {
		fmt.Fprintf(&b, "\tset block_v4 {\n\t\ttype ipv4_addr\n\t\tflags interval\n")
		fmt.Fprintf(&b, "\t\telements = { %s }\n", joinQuoted(block))
		fmt.Fprintf(&b, "\t}\n")
	}
	if len(allow) > 0 {
		fmt.Fprintf(&b, "\tset allow_v4 {\n\t\ttype ipv4_addr\n\t\tflags interval\n")
		fmt.Fprintf(&b, "\t\telements = { %s }\n", joinQuoted(allow))
		fmt.Fprintf(&b, "\t}\n")
	}

	// forward chain: L3 enforcement
	fmt.Fprintf(&b, "\tchain forward {\n")
	fmt.Fprintf(&b, "\t\ttype filter hook forward priority filter; policy accept;\n")
	if len(allow) > 0 {
		fmt.Fprintf(&b, "\t\tip daddr @allow_v4 accept\n")
		fmt.Fprintf(&b, "\t\tip saddr @allow_v4 accept\n")
	}
	if len(block) > 0 {
		fmt.Fprintf(&b, "\t\tip daddr @block_v4 drop\n")
		fmt.Fprintf(&b, "\t\tip saddr @block_v4 drop\n")
	}
	fmt.Fprintf(&b, "\t}\n")

	// nat chain: force LAN DNS through the local sinkhole (AdGuard/Pi-hole trick)
	if c.RedirectDNS && c.DNSPort > 0 {
		fmt.Fprintf(&b, "\tchain dns_redirect {\n")
		fmt.Fprintf(&b, "\t\ttype nat hook prerouting priority dstnat; policy accept;\n")
		iif := ""
		if len(c.LANIfaces) > 0 {
			parts := make([]string, 0, len(c.LANIfaces))
			for _, n := range c.LANIfaces {
				parts = append(parts, fmt.Sprintf("%q", n))
			}
			iif = "iifname { " + strings.Join(parts, ", ") + " } "
		}
		fmt.Fprintf(&b, "\t\t%stcp dport 53 redirect to :%d\n", iif, c.DNSPort)
		fmt.Fprintf(&b, "\t\t%sudp dport 53 redirect to :%d\n", iif, c.DNSPort)
		fmt.Fprintf(&b, "\t}\n")
	}
	fmt.Fprintf(&b, "}\n")
	return b.String(), nil
}

func joinQuoted(ss []string) string {
	q := make([]string, 0, len(ss))
	for _, s := range ss {
		q = append(q, fmt.Sprintf("%q", s))
	}
	return strings.Join(q, ", ")
}

func validTableName(s string) bool {
	if len(s) == 0 || len(s) > 32 || s[0] < 'a' || s[0] > 'z' {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_') {
			return false
		}
	}
	return true
}

func validIface(s string) bool {
	if len(s) == 0 || len(s) > 15 {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '.' || r == '-') {
			return false
		}
	}
	return true
}
