package proxy

// 捕获范围（iKuai 插件版）：一个配置文件决定"抓谁的什么流量"。
//
// 连接层（决定是否 MITM 解密，命中即绕过只隧道）：
//   - 域名      domain_mode(all_except|only) + domains(支持 *.example.com)
//   - 目标IP    dst_ip_mode + dst_ips(CIDR/IP 列表)
//   - GEOIP     geo_mode(off|only|except) + countries(需先下载 geoip.csv)
//
// 记录层（MITM 之后决定是否保存这条流量，命中规则才记录）：
//   - 路径 paths(正则) / 方法 methods / UA user_agents / JA3 ja3
//   - 内容类型 content_types(json|image|video|text|...) / 大小 size_min/size_max
//   - record_mode(all_except|only) 统辖记录层所有过滤器
//
// 配置文件为 JSON，SIGHUP 热重载（抓包不中断）。

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// Scope 是完整捕获范围配置。
type Scope struct {
	// 连接层
	DomainMode string   `json:"domain_mode"` // all_except|only
	Domains    []string `json:"domains"`
	DstIPMode  string   `json:"dst_ip_mode"` // all_except|only
	DstIPs     []string `json:"dst_ips"`     // CIDR 或单 IP
	GeoMode    string   `json:"geo_mode"`    // off|only|except
	Countries  []string `json:"countries"`   // 两位国别码大写, 如 CN/US

	// 记录层
	RecordMode   string   `json:"record_mode"` // all_except|only
	Paths        []string `json:"paths"`       // 正则
	Methods      []string `json:"methods"`     // GET/POST/...
	UserAgents   []string `json:"user_agents"` // 子串(大小写不敏感)
	JA3          []string `json:"ja3"`         // JA3 md5 列表
	ContentTypes []string `json:"content_types"`
	SizeMin      int64    `json:"size_min"`
	SizeMax      int64    `json:"size_max"` // 0=不限
}

// ScopeStore 持有范围配置 + GEOIP 数据，支持热重载。
type ScopeStore struct {
	mu      sync.RWMutex
	path    string
	geoPath string
	cur     Scope

	// 编译缓存(Reload 时重建)
	dstNets []*net.IPNet
	pathREs []*regexp.Regexp

	// GEOIP: 排序后的 [start,end] 区间 + 国别
	geoStart []uint32
	geoEnd   []uint32
	geoCC    []string
}

// NewScopeStore 从 path 加载（文件不存在 = 空范围 = 全抓全解密）。
func NewScopeStore(path, geoPath string) *ScopeStore {
	s := &ScopeStore{path: path, geoPath: geoPath, cur: Scope{DomainMode: "all_except", DstIPMode: "all_except", RecordMode: "all_except"}}
	s.Reload()
	return s
}

// Reload 重新读取配置与 GEOIP 数据（SIGHUP 时调用）。
func (s *ScopeStore) Reload() {
	if s.path != "" {
		if b, err := os.ReadFile(s.path); err == nil {
			var d Scope
			if json.Unmarshal(b, &d) == nil {
				if d.DomainMode != "only" {
					d.DomainMode = "all_except"
				}
				if d.DstIPMode != "only" {
					d.DstIPMode = "all_except"
				}
				if d.RecordMode != "only" {
					d.RecordMode = "all_except"
				}
				if d.GeoMode != "only" && d.GeoMode != "except" {
					d.GeoMode = "off"
				}
				for i := range d.Countries {
					d.Countries[i] = strings.ToUpper(strings.TrimSpace(d.Countries[i]))
				}
				s.mu.Lock()
				s.cur = d
				s.dstNets = parseCIDRs(d.DstIPs)
				s.pathREs = compileREs(d.Paths)
				s.mu.Unlock()
			}
		}
	}
	s.reloadGeo()
}

func (s *ScopeStore) reloadGeo() {
	if s.geoPath == "" {
		return
	}
	f, err := os.Open(s.geoPath)
	if err != nil {
		return
	}
	defer f.Close()
	var starts, ends []uint32
	var ccs []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		// db-ip lite CSV: "1.0.0.0","1.0.0.255","AU"
		line := sc.Text()
		if line == "" || line[0] == '#' {
			continue
		}
		parts := strings.Split(line, ",")
		if len(parts) < 3 {
			continue
		}
		a := net.ParseIP(strings.Trim(parts[0], `"`))
		b := net.ParseIP(strings.Trim(parts[1], `"`))
		if a == nil || b == nil {
			continue
		}
		a4, b4 := a.To4(), b.To4()
		if a4 == nil || b4 == nil {
			continue
		}
		starts = append(starts, ipToU32(a4))
		ends = append(ends, ipToU32(b4))
		ccs = append(ccs, strings.ToUpper(strings.Trim(parts[2], `"`)))
	}
	// 按 start 排序(CSV 通常已有序, 防御性排序)
	idx := make([]int, len(starts))
	for i := range idx {
		idx[i] = i
	}
	sort.Slice(idx, func(i, j int) bool { return starts[idx[i]] < starts[idx[j]] })
	ns, ne, nc := make([]uint32, len(idx)), make([]uint32, len(idx)), make([]string, len(idx))
	for i, j := range idx {
		ns[i], ne[i], nc[i] = starts[j], ends[j], ccs[j]
	}
	s.mu.Lock()
	s.geoStart, s.geoEnd, s.geoCC = ns, ne, nc
	s.mu.Unlock()
}

// Country 查询 IP 所属国别码(无数据返回 "")。
func (s *ScopeStore) Country(ip string) string {
	pip := net.ParseIP(ip)
	if pip == nil {
		return ""
	}
	p4 := pip.To4()
	if p4 == nil {
		return ""
	}
	v := ipToU32(p4)
	s.mu.RLock()
	defer s.mu.RUnlock()
	i := sort.Search(len(s.geoStart), func(i int) bool { return s.geoStart[i] > v }) - 1
	if i >= 0 && v <= s.geoEnd[i] {
		return s.geoCC[i]
	}
	return ""
}

// Get 返回当前配置副本。
func (s *ScopeStore) Get() Scope {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cur
}

// GeoLoaded 返回 GEOIP 数据是否已加载。
func (s *ScopeStore) GeoLoaded() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.geoStart) > 0
}

// BypassConn 连接层：返回该连接是否应绕过解密（只隧道不 MITM）。
func (s *ScopeStore) BypassConn(host, dstIP string) bool {
	d := s.Get()
	// 域名
	if bypassByList(d.DomainMode, d.Domains, func(pat string) bool { return hostMatch(pat, host) }) {
		return true
	}
	// 目标 IP/CIDR
	if len(d.DstIPs) > 0 {
		s.mu.RLock()
		nets := s.dstNets
		s.mu.RUnlock()
		if len(nets) > 0 {
			if pip := net.ParseIP(dstIP); pip != nil {
				matched := false
				for _, n := range nets {
					if n.Contains(pip) {
						matched = true
						break
					}
				}
				if d.DstIPMode == "only" && !matched {
					return true
				}
				if d.DstIPMode != "only" && matched {
					return true
				}
			}
		}
	}
	// GEOIP
	if d.GeoMode == "only" || d.GeoMode == "except" {
		cc := s.Country(dstIP)
		in := false
		for _, c := range d.Countries {
			if c == cc {
				in = true
				break
			}
		}
		if d.GeoMode == "only" && !in {
			return true
		}
		if d.GeoMode == "except" && in {
			return true
		}
	}
	return false
}

// Bypass 兼容旧调用（仅域名维度）。
func (s *ScopeStore) Bypass(host string) bool {
	return s.BypassConn(host, "")
}

// ShouldRecord 记录层：返回这条流量是否应保存。
// method/path/ua/ja3/contentType 来自请求/响应，size 为响应体大小。
func (s *ScopeStore) ShouldRecord(method, path, ua, ja3, contentType string, size int64) bool {
	d := s.Get()
	matched := false // 是否命中任一记录过滤器

	if len(d.Methods) > 0 {
		for _, m := range d.Methods {
			if strings.EqualFold(m, method) {
				matched = true
				break
			}
		}
	}
	if !matched && len(d.Paths) > 0 {
		s.mu.RLock()
		res := s.pathREs
		s.mu.RUnlock()
		for _, re := range res {
			if re != nil && re.MatchString(path) {
				matched = true
				break
			}
		}
	}
	if !matched && len(d.UserAgents) > 0 {
		low := strings.ToLower(ua)
		for _, u := range d.UserAgents {
			if u != "" && strings.Contains(low, strings.ToLower(u)) {
				matched = true
				break
			}
		}
	}
	if !matched && len(d.JA3) > 0 && ja3 != "" {
		for _, j := range d.JA3 {
			if strings.EqualFold(j, ja3) {
				matched = true
				break
			}
		}
	}
	if !matched && len(d.ContentTypes) > 0 {
		for _, ct := range d.ContentTypes {
			if ct != "" && strings.Contains(strings.ToLower(contentType), strings.ToLower(ct)) {
				matched = true
				break
			}
		}
	}
	if !matched && (d.SizeMin > 0 || d.SizeMax > 0) {
		if (d.SizeMin == 0 || size >= d.SizeMin) && (d.SizeMax == 0 || size <= d.SizeMax) {
			matched = true
		}
	}

	// 无任何记录过滤器 → 全记录
	anyFilter := len(d.Methods) > 0 || len(d.Paths) > 0 || len(d.UserAgents) > 0 ||
		len(d.JA3) > 0 || len(d.ContentTypes) > 0 || d.SizeMin > 0 || d.SizeMax > 0
	if !anyFilter {
		return true
	}
	if d.RecordMode == "only" {
		return matched // 白名单: 只记录命中的
	}
	return !matched // 黑名单: 命中的不记录
}

// ---- 工具 ----

func bypassByList(mode string, list []string, match func(string) bool) bool {
	if len(list) == 0 {
		return mode == "only" // only+空=全绕过; all_except+空=全通过
	}
	hit := false
	for _, p := range list {
		if match(p) {
			hit = true
			break
		}
	}
	if mode == "only" {
		return !hit
	}
	return hit
}

func parseCIDRs(list []string) []*net.IPNet {
	var out []*net.IPNet
	for _, s := range list {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if !strings.Contains(s, "/") {
			if ip := net.ParseIP(s); ip != nil {
				s = s + "/32"
			}
		}
		if _, n, err := net.ParseCIDR(s); err == nil {
			out = append(out, n)
		}
	}
	return out
}

func compileREs(list []string) []*regexp.Regexp {
	var out []*regexp.Regexp
	for _, p := range list {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if re, err := regexp.Compile(p); err == nil {
			out = append(out, re)
		}
	}
	return out
}

func ipToU32(b []byte) uint32 {
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}

// stripPort 去掉 host:port 的端口（IPv6 安全）。
func stripPort(h string) string {
	if i := strings.LastIndex(h, ":"); i >= 0 && !strings.Contains(h, "]") {
		return h[:i]
	}
	if i := strings.LastIndex(h, "]"); i >= 0 {
		return h[:i+1]
	}
	return h
}

// hostMatch 通配匹配（*.example.com 匹配子域）。
func hostMatch(pattern, host string) bool {
	host = strings.ToLower(stripPort(host))
	pattern = strings.ToLower(pattern)
	if pattern == "" || pattern == "*" {
		return true
	}
	if strings.HasPrefix(pattern, "*.") {
		suf := pattern[1:]
		return strings.HasSuffix(host, suf) || host == pattern[2:]
	}
	return host == pattern
}
