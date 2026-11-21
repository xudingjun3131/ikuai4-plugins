package core

import (
	"fmt"
	"sync"
	"time"
)

// Engine is the integrated decision pipeline. One Request goes through all
// layers in a fixed order and yields ONE Decision with provenance:
//
//  1. threat intel (IP)     — CrowdSec/Suricata style, highest precedence
//  2. exception list        — @@ whitelist from AdGuard-style rules
//  3. filter list (domain)  — ads/malware/phishing, hagezi/AWAvenue
//  4. app identification    — nDPI/OAF style, domain/SNI based
//  5. unified policy table  — 山石/OPNsense style subject+target+schedule
//  6. default               — allow + audit
//
// Every decision is recorded for behavior audit. This is the concrete meaning
// of "一体化": a single verdict instead of three uncoordinated products.
type Engine struct {
	mu      sync.RWMutex
	Filter  *FilterList
	AppID   *AppID
	Policy  *Policy
	Threat  *ThreatIntel
	Audit   *Audit
	ACL     ACLConfig
	started time.Time
}

type Config struct {
	Dir          string // config dir on the storage volume
	AuditCap     int
	ACL          ACLConfig
	BlockModeDNS Action // what to return for a blocked DNS name
}

func NewEngine(cfg Config) (*Engine, error) {
	if cfg.AuditCap <= 0 {
		cfg.AuditCap = 2000
	}
	if cfg.ACL.Table == "" {
		cfg.ACL = DefaultACLConfig()
	}
	if cfg.BlockModeDNS == "" {
		cfg.BlockModeDNS = ActionRedirect
	}
	e := &Engine{
		Filter:  NewFilterList(),
		AppID:   NewAppID(),
		Policy:  NewPolicy(DefaultPolicy()...),
		Threat:  NewThreatIntel(),
		Audit:   NewAudit(cfg.AuditCap),
		ACL:     cfg.ACL,
		started: now(),
	}
	if cfg.Dir != "" {
		if err := e.Reload(cfg.Dir); err != nil {
			return nil, err
		}
	}
	return e, nil
}

// Reload re-reads all config files from the storage volume. Safe to call on
// SIGHUP / UI "保存并生效" without restarting the process (hot swap, the same
// property OPNsense's API-driven ruleset has).
func (e *Engine) Reload(dir string) error {
	nf := NewFilterList()
	if _, err := nf.LoadDir(dir + "/lists"); err != nil {
		return fmt.Errorf("过滤列表: %w", err)
	}
	na := NewAppID()
	if err := na.LoadFile(dir + "/app-signatures.json"); err != nil {
		return fmt.Errorf("应用特征库: %w", err)
	}
	np := NewPolicy(DefaultPolicy()...)
	if err := np.LoadFile(dir + "/policy.json"); err != nil {
		return fmt.Errorf("策略表: %w", err)
	}
	if len(np.List()) == 0 {
		np.Replace(DefaultPolicy()...)
	}
	nt := NewThreatIntel()
	if _, _, err := nt.LoadDir(dir + "/threat"); err != nil {
		return fmt.Errorf("威胁情报: %w", err)
	}
	e.mu.Lock()
	e.Filter, e.AppID, e.Policy, e.Threat = nf, na, np, nt
	e.mu.Unlock()
	return nil
}

// Decide adjudicates one request. Pure w.r.t. inputs except for the audit
// record (which is the point of the product).
//
// [dry-run] req.DryRun=true(策略预演/UI 调试)时不写行为审计, 避免 D6 的
// "预演污染真实流量审计"。Source 标记来源, 便于审计按来源过滤/清理。
func (e *Engine) Decide(req Request) Decision {
	if req.At.IsZero() {
		req.At = now()
	}
	src := req.Source
	if src == "" {
		src = "api"
	}
	if req.DryRun {
		src = "probe"
	}
	e.mu.RLock()
	filter, appid, policy, threat := e.Filter, e.AppID, e.Policy, e.Threat
	e.mu.RUnlock()

	// record 只在真实流量下写审计; 预演仍返回完整裁决供 UI 展示。
	record := func(d Decision) Decision {
		if !req.DryRun {
			e.Audit.Record(d)
		}
		return d
	}
	mk := func(a Action, layer, reason, ruleID string, app string, cat Category) Decision {
		return Decision{
			Action: a, Layer: layer, Reason: reason, RuleID: ruleID,
			App: app, Category: cat, ClientIP: req.ClientIP,
			Domain: normalizeDomain(req.Domain), Source: src, At: req.At,
		}
	}

	// 1. threat intel (destination IP)
	if req.DstIP != "" {
		if s, ok := threat.MatchIP(req.DstIP); ok {
			return record(mk(ActionBlock, "threat", "命中威胁情报 "+s, "threat:"+s, "", CatMalware))
		}
	}

	// 2/3. domain filter list (exceptions win inside Lookup)
	if blocked, s, cat := filter.Lookup(req.Domain); blocked {
		return record(mk(ActionBlock, "filterlist", "命中过滤列表 "+s, "list:"+s, "", cat))
	} else if s != "" { // exception
		app, ac := appid.Classify(req.Domain)
		return record(mk(ActionAllow, "exception", s, "exception", app, ac))
	}

	// 4. app identification
	app, cat := appid.Classify(req.Domain)
	if app == "" && req.AppHint != "" {
		app = req.AppHint
	}
	if cat == CatUnknown && catFromHint(req.AppHint) != "" {
		cat = catFromHint(req.AppHint)
	}

	// 5. unified policy table
	if r, ok := policy.Evaluate(req, app, cat); ok {
		return record(mk(r.Action, "policy", "命中策略 "+r.ID+" ("+r.Name+")", "policy:"+r.ID, app, cat))
	}

	// 6. default
	return record(mk(ActionAllow, "default", "无匹配规则, 默认放行", "default", app, cat))
}

// DecideResolvedIP 在拿到上游 DNS 应答的 A/AAAA 记录后做 L3 层威胁情报复核
// (D8: 旧版 DNS 路径不带 DstIP, 威胁 IP 层永远不可达)。
// 返回 (命中的 IP, 是否应拦截)。
func (e *Engine) DecideResolvedIP(req Request, ips []string) (string, bool) {
	e.mu.RLock()
	threat := e.Threat
	e.mu.RUnlock()
	for _, ip := range ips {
		if s, ok := threat.MatchIP(ip); ok {
			d := Decision{
				Action: ActionBlock, Layer: "threat", Reason: "DNS 应答命中威胁情报 " + s + " (" + ip + ")",
				RuleID: "threat-resolved:" + s, ClientIP: req.ClientIP,
				Domain: normalizeDomain(req.Domain), Source: "dns", At: req.At,
			}
			if !req.DryRun {
				e.Audit.Record(d)
			}
			return ip, true
		}
	}
	return "", false
}

func catFromHint(h string) Category {
	switch h {
	case "ads", "malware", "social", "video", "game", "p2p", "vpn", "work":
		return Category(h)
	}
	return ""
}

// RenderACL renders the current L3 enforcement ruleset. Blocked IPs come from
// threat intel; admin-added subnets come from ACL.BlockIPs.
func (e *Engine) RenderACL() (string, error) {
	e.mu.RLock()
	c := e.ACL
	ips := e.Threat.IPs()
	e.mu.RUnlock()
	merged := append([]string{}, c.BlockIPs...)
	merged = append(merged, ips...)
	c.BlockIPs = dedup(merged)
	return RenderACL(c)
}

func dedup(ss []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func (e *Engine) Uptime() time.Duration { return now().Sub(e.started) }
