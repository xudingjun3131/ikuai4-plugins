package core

import (
	"encoding/json"
	"os"
	"sync"
	"time"
)

// Schedule is a weekly time window. Days uses Go's weekday numbering
// (0=Sunday). Empty Days = every day. "09:00"-"18:00" style HH:MM.
type Schedule struct {
	Days  []int  `json:"days,omitempty"`
	Start string `json:"start,omitempty"`
	End   string `json:"end,omitempty"`
}

func (s Schedule) active(at time.Time) bool {
	if len(s.Days) > 0 {
		hit := false
		wd := int(at.Weekday())
		for _, d := range s.Days {
			if d == wd {
				hit = true
				break
			}
		}
		if !hit {
			return false
		}
	}
	if s.Start == "" || s.End == "" {
		return true
	}
	hm := at.Format("15:04")
	if s.Start <= s.End {
		return hm >= s.Start && hm <= s.End
	}
	// window crossing midnight
	return hm >= s.Start || hm <= s.End
}

// Subject narrows a rule to clients. Empty fields match all.
type Subject struct {
	IPs     []string `json:"ips,omitempty"`     // exact IPv4/IPv6 or CIDR
	MACs    []string `json:"macs,omitempty"`    // case-insensitive
	Groups  []string `json:"groups,omitempty"`  // named client groups
	Exclude []string `json:"exclude,omitempty"` // IPs that never match this rule
}

// Match narrows a rule to targets.
type Match struct {
	Apps       []string   `json:"apps,omitempty"`
	Categories []Category `json:"categories,omitempty"`
	Domains    []string   `json:"domains,omitempty"` // suffix match
}

// Rule is one row of the unified policy table (山石-style: subject + target +
// schedule + action in a single row, instead of three separate products).
type Rule struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Enabled  bool     `json:"enabled"`
	Priority int      `json:"priority"` // lower = evaluated first
	Subject  Subject  `json:"subject,omitempty"`
	Match    Match    `json:"match,omitempty"`
	Schedule Schedule `json:"schedule,omitempty"`
	Action   Action   `json:"action"`
	Note     string   `json:"note,omitempty"`
}

// Policy is an ordered, hot-swappable rule set.
type Policy struct {
	mu    sync.RWMutex
	rules []Rule
}

func NewPolicy(rules ...Rule) *Policy {
	p := &Policy{}
	p.Replace(rules...)
	return p
}

func (p *Policy) Replace(rules ...Rule) {
	cp := make([]Rule, len(rules))
	copy(cp, rules)
	sortRules(cp)
	p.mu.Lock()
	p.rules = cp
	p.mu.Unlock()
}

func sortRules(rs []Rule) {
	// stable: priority, then ID for determinism
	for i := 1; i < len(rs); i++ {
		for j := i; j > 0; j-- {
			a, b := rs[j-1], rs[j]
			if a.Priority > b.Priority || (a.Priority == b.Priority && a.ID > b.ID) {
				rs[j-1], rs[j] = b, a
			} else {
				break
			}
		}
	}
}

func (p *Policy) List() []Rule {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]Rule, len(p.rules))
	copy(out, p.rules)
	return out
}

func (p *Policy) LoadFile(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var rs []Rule
	if err := json.Unmarshal(b, &rs); err != nil {
		return err
	}
	p.Replace(rs...)
	return nil
}

func (p *Policy) SaveFile(path string) error {
	b, err := json.MarshalIndent(p.List(), "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

// Evaluate returns the first matching rule. First-match-wins by priority, the
// same semantics an admin expects from a firewall rule table.
func (p *Policy) Evaluate(req Request, app string, cat Category) (Rule, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	for _, r := range p.rules {
		if !r.Enabled {
			continue
		}
		if !matchSubject(r.Subject, req) {
			continue
		}
		if !r.Schedule.active(req.At) {
			continue
		}
		if !matchTarget(r.Match, req, app, cat) {
			continue
		}
		return r, true
	}
	return Rule{}, false
}

func matchSubject(s Subject, req Request) bool {
	for _, ex := range s.Exclude {
		if ipMatch(ex, req.ClientIP) {
			return false
		}
	}
	if len(s.IPs) == 0 && len(s.MACs) == 0 && len(s.Groups) == 0 {
		return true
	}
	for _, ip := range s.IPs {
		if ipMatch(ip, req.ClientIP) {
			return true
		}
	}
	for _, m := range s.MACs {
		if stringsEqualFold(m, req.ClientMAC) {
			return true
		}
	}
	// Groups resolved by the engine via GroupResolver; here we only accept
	// a literal group tag carried in ClientMAC-free form through AppHint.
	for _, g := range s.Groups {
		if g == req.AppHint {
			return true
		}
	}
	return false
}

func matchTarget(m Match, req Request, app string, cat Category) bool {
	if len(m.Apps) == 0 && len(m.Categories) == 0 && len(m.Domains) == 0 {
		return true
	}
	for _, a := range m.Apps {
		if a == app {
			return true
		}
	}
	for _, c := range m.Categories {
		if c == cat {
			return true
		}
	}
	d := normalizeDomain(req.Domain)
	for _, pat := range m.Domains {
		p := normalizeDomain(pat)
		if d == p || stringsHasSuffix(d, "."+p) {
			return true
		}
	}
	return false
}

func ipMatch(spec, ip string) bool {
	if spec == ip {
		return true
	}
	if stringsContains(spec, "/") {
		return cidrContains(spec, ip)
	}
	return false
}

// DefaultPolicy blocks nothing but audits everything: a safe first-run state
// ("observe first, enforce later") — avoids breaking a network on install.
func DefaultPolicy() []Rule {
	return []Rule{
		{ID: "default-audit", Name: "默认: 全量行为审计", Enabled: true, Priority: 9999, Action: ActionAudit,
			Note: "首次安装只记录不拦截, 在策略页确认后再启用管控规则"},
	}
}
