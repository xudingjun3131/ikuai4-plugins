package core

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// ThreatIntel is a CrowdSec-inspired collaborative blocklist feed layer.
// It merges IP/CIDR feeds (community blocklists, Suricata-style IP rep lists)
// into one set that both the decision engine (L3) and the ACL compiler consume.
//
// Feed files live in the plugin config dir under threat/*.txt; service.sh owns
// refreshing them (busybox wget, no Go net/http dependency at runtime). The
// engine only reads — a deliberate split so a network outage can never break
// packet adjudication.
type ThreatIntel struct {
	mu    sync.RWMutex
	ips   map[string]string // spec -> source feed name
	files []string
}

func NewThreatIntel() *ThreatIntel {
	return &ThreatIntel{ips: map[string]string{}}
}

// LoadDir reads threat/*.txt. Each non-comment line is an IP or CIDR; invalid
// lines are skipped (never abort the whole engine on one bad feed row).
func (t *ThreatIntel) LoadDir(dir string) (int, int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, 0, nil
		}
		return 0, 0, err
	}
	added, skipped := 0, 0
	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".txt") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return added, skipped, err
		}
		names = append(names, e.Name())
		for _, line := range strings.Split(string(b), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
				continue
			}
			// allow "1.2.3.4 ; comment" and "1.2.3.4/24,extra" shapes
			if i := strings.IndexAny(line, ";,# \t"); i > 0 {
				line = strings.TrimSpace(line[:i])
			}
			if !IsValidIPSpec(line) {
				skipped++
				continue
			}
			t.mu.Lock()
			if _, ok := t.ips[line]; !ok {
				t.ips[line] = e.Name()
				added++
			}
			t.mu.Unlock()
		}
	}
	sort.Strings(names)
	t.mu.Lock()
	t.files = names
	t.mu.Unlock()
	return added, skipped, nil
}

func (t *ThreatIntel) Contains(spec string) (string, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	src, ok := t.ips[spec]
	return src, ok
}

// MatchIP checks an address against exact entries and CIDR ranges.
func (t *ThreatIntel) MatchIP(ip string) (string, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if src, ok := t.ips[ip]; ok {
		return src, true
	}
	for spec, src := range t.ips {
		if strings.Contains(spec, "/") && cidrContains(spec, ip) {
			return src, true
		}
	}
	return "", false
}

func (t *ThreatIntel) IPs() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make([]string, 0, len(t.ips))
	for s := range t.ips {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func (t *ThreatIntel) Stats() map[string]int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return map[string]int{"ips": len(t.ips), "feeds": len(t.files)}
}
