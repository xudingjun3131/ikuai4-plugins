package core

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// FilterList is a domain-based block/allow list. It understands the three
// formats used by the projects we borrowed from, without needing their code:
//
//	AdGuard / uBlock:  ||example.com^        /  @@||ok.com^  (exception)
//	hosts:             0.0.0.0 example.com   /  ::  example.com
//	plain:             example.com
//	comments:          # ...   ! ...   // ...
//
// Matching is exact + suffix (subdomains) + one-level wildcard, which covers
// the overwhelming majority of real-world DNS blocklist entries.
type FilterList struct {
	mu        sync.RWMutex
	block     map[string]string // domain -> source list name
	allow     map[string]string // exception (@@) -> source list name
	wildcard  []string          // entries starting with "*." or containing "*"
	cats      map[string]Category
	sourceCnt map[string]int
}

func NewFilterList() *FilterList {
	return &FilterList{
		block: map[string]string{}, allow: map[string]string{},
		cats: map[string]Category{}, sourceCnt: map[string]int{},
	}
}

func normalizeDomain(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimSuffix(s, ".")
	return s
}

// ParseList parses one blocklist body. name is used for provenance; cat is the
// category assigned to hits from this list (e.g. hagezi -> ads/malware).
func (f *FilterList) ParseList(name, body string, cat Category) int {
	sc := bufio.NewScanner(strings.NewReader(body))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	added := 0
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") ||
			strings.HasPrefix(line, "!") || strings.HasPrefix(line, "//") {
			continue
		}
		exc := false
		if strings.HasPrefix(line, "@@") {
			exc = true
			line = strings.TrimPrefix(line, "@@")
		}
		// strip AdGuard options
		if i := strings.Index(line, "$"); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "||")
		line = strings.TrimSuffix(line, "^")
		line = strings.TrimSpace(line)
		// hosts format: "<ip> <domain> [domain2...]"
		if fields := strings.Fields(line); len(fields) >= 2 && isIPLike(fields[0]) {
			line = fields[1]
		}
		line = strings.TrimSuffix(line, "/")
		if strings.Contains(line, "://") || strings.Contains(line, "/") {
			continue // URL rules need a proxy, not DNS; out of scope here
		}
		line = normalizeDomain(line)
		if line == "" || line == "localhost" {
			continue
		}
		f.mu.Lock()
		if strings.Contains(line, "*") {
			if !containsStr(f.wildcard, line) {
				f.wildcard = append(f.wildcard, line)
				added++
			}
		} else if exc {
			f.allow[line] = name
			added++
		} else {
			if _, ok := f.block[line]; !ok {
				f.block[line] = name
				added++
			}
			f.cats[line] = cat
		}
		f.sourceCnt[name]++
		f.mu.Unlock()
	}
	return added
}

func isIPLike(s string) bool {
	if s == "0.0.0.0" || s == "::" || s == "::1" || s == "127.0.0.1" {
		return true
	}
	return len(s) > 0 && s[0] >= '0' && s[0] <= '9' && strings.Count(s, ".") == 3
}

func containsStr(a []string, s string) bool {
	for _, v := range a {
		if v == s {
			return true
		}
	}
	return false
}

// Lookup returns (blocked, source, category) for a domain. Exceptions win.
func (f *FilterList) Lookup(domain string) (bool, string, Category) {
	d := normalizeDomain(domain)
	f.mu.RLock()
	defer f.mu.RUnlock()
	if src, ok := f.allow[d]; ok {
		return false, "exception:" + src, CatUnknown
	}
	// walk parent suffixes: a.b.example.com -> example.com
	parts := strings.Split(d, ".")
	for i := 0; i < len(parts); i++ {
		cand := strings.Join(parts[i:], ".")
		if src, ok := f.allow[cand]; ok {
			return false, "exception:" + src, CatUnknown
		}
		if src, ok := f.block[cand]; ok {
			return true, src, f.cats[cand]
		}
	}
	for _, w := range f.wildcard {
		if wildcardMatch(w, d) {
			return true, "wildcard", CatAds
		}
	}
	return false, "", CatUnknown
}

func wildcardMatch(pattern, s string) bool {
	// simple glob supporting a single leading "*." and "*" segments
	pi, si, star, mark := 0, 0, -1, 0
	for si < len(s) {
		if pi < len(pattern) && (pattern[pi] == '?' || pattern[pi] == s[si]) {
			pi++
			si++
		} else if pi < len(pattern) && pattern[pi] == '*' {
			star, mark = pi, si
			pi++
		} else if star != -1 {
			pi = star + 1
			mark++
			si = mark
		} else {
			return false
		}
	}
	for pi < len(pattern) && pattern[pi] == '*' {
		pi++
	}
	return pi == len(pattern)
}

// LoadDir reads every *.txt under dir as a blocklist. Category is inferred from
// the file name when it contains ads/malware/... hints.
func (f *FilterList) LoadDir(dir string) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	total := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".txt") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return total, err
		}
		total += f.ParseList(e.Name(), string(b), inferCategory(e.Name()))
	}
	return total, nil
}

func inferCategory(name string) Category {
	n := strings.ToLower(name)
	switch {
	case strings.Contains(n, "malware") || strings.Contains(n, "phish") || strings.Contains(n, "threat"):
		return CatMalware
	case strings.Contains(n, "ads") || strings.Contains(n, "ad") || strings.Contains(n, "tracking"):
		return CatAds
	default:
		return CatAds
	}
}

// Stats returns a stable summary for /api/status.
func (f *FilterList) Stats() map[string]int {
	f.mu.RLock()
	defer f.mu.RUnlock()
	out := map[string]int{
		"block": len(f.block), "allow": len(f.allow), "wildcard": len(f.wildcard),
	}
	for k, v := range f.sourceCnt {
		out["src:"+k] = v
	}
	return out
}

// SaveJSON persists a snapshot (used by the policy editor's import/export).
func (f *FilterList) SaveJSON(path string) error {
	f.mu.RLock()
	defer f.mu.RUnlock()
	m := map[string]any{"block": f.block, "allow": f.allow, "wildcard": f.wildcard}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

// SortedDomains returns blocked domains in deterministic order (ACL/UI use).
func (f *FilterList) SortedDomains() []string {
	f.mu.RLock()
	defer f.mu.RUnlock()
	out := make([]string, 0, len(f.block))
	for d := range f.block {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}

func (f *FilterList) String() string {
	s := f.Stats()
	return fmt.Sprintf("FilterList{block=%d allow=%d wildcard=%d}", s["block"], s["allow"], s["wildcard"])
}
