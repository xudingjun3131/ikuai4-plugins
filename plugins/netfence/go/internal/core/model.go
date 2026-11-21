// Package core implements the integrated "firewall + internet-behavior-management"
// decision engine for the netfence iKuai plugin.
//
// Design is an explicit synthesis of the strengths found in the open-source
// projects surveyed (see docs/防火墙插件调研与整合设计-2026-11-20.md):
//
//	DNS/content filter  <- AdGuard Home / Pi-hole / hagezi / AWAvenue  (rule formats)
//	App identification  <- nDPI / OpenAppFilter                       (L7 by domain/SNI)
//	Unified policy      <- OPNsense (MVC/API rules) + 山石 (one policy table)
//	ACL compiler        <- OPNsense / nftables                        (deterministic ruleset)
//	Behavior audit      <- nlbwmon (per-device) / Sniffnet (traffic UI)
//	Threat intel        <- CrowdSec (blocklist feed) / Suricata (IP+domain)
//
// Everything is stdlib-only and pure where possible, so it builds statically
// (CGO_ENABLED=0) for the uClibc firmware and is unit-testable without root,
// without network and without kernel modules.
package core

import "time"

// Action is what the engine does with a request.
type Action string

const (
	ActionAllow    Action = "allow"
	ActionBlock    Action = "block"
	ActionAudit    Action = "audit"    // allow, but force full behavior-audit logging
	ActionRedirect Action = "redirect" // DNS sinkhole / return controlled address
)

// Category is a coarse content/application classification (上网行为管理分类).
type Category string

const (
	CatAds     Category = "ads"
	CatMalware Category = "malware"
	CatSocial  Category = "social"
	CatVideo   Category = "video"
	CatGame    Category = "game"
	CatP2P     Category = "p2p"
	CatVPN     Category = "vpn"
	CatWork    Category = "work"
	CatUnknown Category = "unknown"
)

// Request is one observed client action to be adjudicated. A single Request
// type is deliberately shared by DNS, TLS-SNI and HTTP-Host hooks: that is the
// "一体化" part — one policy table decides all of them instead of three products.
// DryRun=true 表示"策略预演"(UI 调试), 不写行为审计(D6: 预演污染审计)。
type Request struct {
	ClientIP  string    `json:"client_ip"`
	ClientMAC string    `json:"client_mac"`
	Domain    string    `json:"domain"` // DNS qname / TLS SNI / HTTP Host
	DstIP     string    `json:"dst_ip"` // optional resolved address
	AppHint   string    `json:"app_hint"`
	DryRun    bool      `json:"dry_run"` // true=预演, 不落审计
	Source    string    `json:"source"`  // dns|tcp|api|probe, 留空则由引擎按 dry_run 推断
	At        time.Time `json:"at"`
}

// Decision is the engine verdict, carrying provenance so the UI can explain
// "why was this blocked" (a weakness of many single-purpose tools).
// Source 区分数据来源: dns/tcp/api(真实流量) 与 probe(策略预演), 便于审计过滤。
type Decision struct {
	Action   Action    `json:"action"`
	Layer    string    `json:"layer"` // threat|exception|filterlist|policy|default
	Reason   string    `json:"reason"`
	App      string    `json:"app"`
	Category Category  `json:"category"`
	RuleID   string    `json:"rule_id"`
	ClientIP string    `json:"client_ip"`
	Domain   string    `json:"domain"`
	Source   string    `json:"source"` // dns|tcp|api|probe
	At       time.Time `json:"at"`
}

// Blocked reports whether the decision denies the request.
func (d Decision) Blocked() bool { return d.Action == ActionBlock }

// now is a single clock seam so tests are deterministic.
var now = func() time.Time { return time.Now() }

// ScheduleTZ 是时段策略使用的固定时区。设备固件 Go 运行时为 UTC 且常无 tzdata,
// 不能用 time.LoadLocation(会失败回退 UTC)。默认东八区(北京时间), 与设备
// 使用者填写 09:00-18:00 的直觉一致(D7: 差 8 小时)。可由配置覆盖。
var ScheduleTZ = time.FixedZone("CST", 8*3600)
