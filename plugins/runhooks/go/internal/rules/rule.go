package rules

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// Type is a rule kind (Phase A subset).
type Type string

const (
	TypeHostBlock      Type = "host_block"
	TypeHostAllow      Type = "host_allow"
	TypeHeader         Type = "header"
	TypeBodyReplace    Type = "body_replace"
	TypeMapLocal       Type = "map_local"
	TypeSSLBypass      Type = "ssl_bypass"
	TypeAccessControl  Type = "access_control"
	TypeGateway        Type = "gateway"
	TypeMirrorRemote   Type = "mirror_remote"
	TypeSecondaryProxy Type = "secondary_proxy"
	TypeReverseProxy   Type = "reverse_proxy"
	TypeHighlight      Type = "highlight"
)

// Match selects requests.
type Match struct {
	Host    string   `json:"host,omitempty"`    // wildcard * supported
	Path    string   `json:"path,omitempty"`    // regex if non-empty
	Methods []string `json:"methods,omitempty"` // empty = any
}

// HeaderMut is request/response header mutation.
type HeaderMut struct {
	Phase  string            `json:"phase"` // request|response
	Set    map[string]string `json:"set,omitempty"`
	Remove []string          `json:"remove,omitempty"`
}

// BodyReplace is string replace on body.
type BodyReplace struct {
	Phase string `json:"phase"` // request|response
	From  string `json:"from"`
	To    string `json:"to"`
}

// MapLocal serves a local file instead of upstream.
// [sub-adapt] Body 非空 = 内联响应体(订阅 echo-response 适配用, 不需要落文件)。
type MapLocal struct {
	Status int    `json:"status"`
	File   string `json:"file"` // relative under map-local jail
	Body   string `json:"body,omitempty"`
}

type AccessControl struct {
	CIDRs  []string `json:"cidrs,omitempty"`
	Action string   `json:"action,omitempty"`
}

type Gateway struct {
	Upstream string `json:"upstream"`
}

type MirrorRemote struct {
	URL string `json:"url"`
}

type SecondaryProxy struct {
	ProxyURL string `json:"proxy_url"`
}

type ReverseProxy struct {
	Listen string `json:"listen"`
	Target string `json:"target"`
}

type Highlight struct {
	Color string `json:"color"`
	Label string `json:"label,omitempty"`
}

// Rule is one enabled interception rule.
type Rule struct {
	ID             string          `json:"id"`
	Enabled        bool            `json:"enabled"`
	Type           Type            `json:"type"`
	Match          Match           `json:"match"`
	Header         *HeaderMut      `json:"header,omitempty"`
	BodyReplace    *BodyReplace    `json:"body_replace,omitempty"`
	MapLocal       *MapLocal       `json:"map_local,omitempty"`
	AccessControl  *AccessControl  `json:"access_control,omitempty"`
	Gateway        *Gateway        `json:"gateway,omitempty"`
	MirrorRemote   *MirrorRemote   `json:"mirror_remote,omitempty"`
	SecondaryProxy *SecondaryProxy `json:"secondary_proxy,omitempty"`
	ReverseProxy   *ReverseProxy   `json:"reverse_proxy,omitempty"`
	Highlight      *Highlight      `json:"highlight,omitempty"`
}

// File is the on-disk rules.json shape.
type File struct {
	Rules []Rule `json:"rules"`
}

// [sub-rules] 订阅规则适配层: 订阅生成的引擎规则写在 rules.sub.json(与用户手写的
// rules.json 物理分离), Reload 时合并进内存(用户规则在前, 订阅规则追加在后——
// hooks 按序求值, 用户规则优先)。Save() 只落用户规则, 订阅规则只由
// SaveSubRules() 写——两边永远不会互相覆盖。
const SubRulePrefix = "sub-"

func subRulesPath(dataDir string) string {
	return filepath.Join(dataDir, "rules.sub.json")
}

func loadRulesFile(path string) ([]Rule, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("rules: read: %w", err)
	}
	var f File
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("rules: parse: %w", err)
	}
	return f.Rules, nil
}

// Chain is a hot-swappable rules engine implementing proxy.ProxyHooks.
type Chain struct {
	mu      sync.RWMutex
	rules   []Rule
	dataDir string
	pathRE  map[string]*regexp.Regexp // rule id -> compiled path
}

// NewChain loads rules.json from dataDir (creates empty if missing).
// 首次启动(文件不存在)时写入内置规则集; 文件已存在则不覆盖——
// 用户删掉的规则不会被开机复活, 想要内置规则用 load-defaults。
func NewChain(dataDir string) (*Chain, error) {
	c := &Chain{dataDir: dataDir, pathRE: make(map[string]*regexp.Regexp)}
	if err := c.seedDefaults(); err != nil {
		return nil, err
	}
	if err := c.Reload(); err != nil {
		return nil, err
	}
	return c, nil
}

// seedDefaults 在 rules.json 缺失时写入内置规则(仅此一种情况)。
func (c *Chain) seedDefaults() error {
	path := rulesPath(c.dataDir)
	if _, err := os.Stat(path); err == nil {
		return nil // 已存在: 哪怕是空规则也是用户的意志, 不覆盖
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("rules: stat: %w", err)
	}
	return c.Save(DefaultRules())
}

func rulesPath(dataDir string) string {
	return filepath.Join(dataDir, "rules.json")
}

// Reload reads rules.json + rules.sub.json from disk.
func (c *Chain) Reload() error {
	user, err := loadRulesFile(rulesPath(c.dataDir))
	if err != nil {
		return err
	}
	sub, err := loadRulesFile(subRulesPath(c.dataDir))
	if err != nil {
		return err
	}
	all := append(append([]Rule(nil), user...), sub...)
	re := make(map[string]*regexp.Regexp)
	for i := range all {
		r := &all[i]
		if r.Match.Path == "" {
			continue
		}
		rx, err := regexp.Compile(r.Match.Path)
		if err != nil {
			return fmt.Errorf("rules: path regex %q: %w", r.ID, err)
		}
		re[r.ID] = rx
	}
	c.mu.Lock()
	c.rules = all
	c.pathRE = re
	c.mu.Unlock()
	return nil
}

// Save writes user rules (订阅规则 sub-* 自动剔除, 只落 rules.json) and reloads.
func (c *Chain) Save(rules []Rule) error {
	if err := os.MkdirAll(c.dataDir, 0o700); err != nil {
		return err
	}
	user := make([]Rule, 0, len(rules))
	for _, r := range rules {
		if strings.HasPrefix(r.ID, SubRulePrefix) {
			continue // 订阅规则不落用户文件
		}
		user = append(user, r)
	}
	f := File{Rules: user}
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	path := rulesPath(c.dataDir)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	return c.Reload()
}

// SaveSubRules 写订阅规则层(rules.sub.json)并重载。传空切片=清空订阅规则。
func (c *Chain) SaveSubRules(rules []Rule) error {
	if err := os.MkdirAll(c.dataDir, 0o700); err != nil {
		return err
	}
	sub := make([]Rule, 0, len(rules))
	for _, r := range rules {
		if !strings.HasPrefix(r.ID, SubRulePrefix) {
			r.ID = SubRulePrefix + r.ID
		}
		sub = append(sub, r)
	}
	f := File{Rules: sub}
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	path := subRulesPath(c.dataDir)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	return c.Reload()
}

// List returns a copy of current rules.
func (c *Chain) List() []Rule {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]Rule, len(c.rules))
	copy(out, c.rules)
	return out
}

// MapLocalRoot is the jail directory for map_local files.
func (c *Chain) MapLocalRoot() string {
	return filepath.Join(c.dataDir, "map-local")
}

func hostMatch(pattern, host string) bool {
	host = strings.ToLower(stripPort(host))
	pattern = strings.ToLower(pattern)
	if pattern == "" || pattern == "*" {
		return true
	}
	// [sub-wildcard] 订阅规则适配: DOMAIN-KEYWORD 映射为 *keyword* 中间通配
	if len(pattern) > 2 && strings.HasPrefix(pattern, "*") && strings.HasSuffix(pattern, "*") {
		return strings.Contains(host, pattern[1:len(pattern)-1])
	}
	if strings.HasPrefix(pattern, "*.") {
		suf := pattern[1:] // .example.com
		return strings.HasSuffix(host, suf) || host == pattern[2:]
	}
	return host == pattern
}

func stripPort(host string) string {
	if i := strings.LastIndex(host, ":"); i >= 0 {
		// not IPv6 bare
		if !strings.Contains(host, "]") {
			return host[:i]
		}
	}
	return host
}

func (c *Chain) matchRule(r *Rule, host, path, method string) bool {
	if !r.Enabled {
		return false
	}
	if r.Match.Host != "" && !hostMatch(r.Match.Host, host) {
		return false
	}
	if len(r.Match.Methods) > 0 {
		ok := false
		for _, m := range r.Match.Methods {
			if strings.EqualFold(m, method) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	if r.Match.Path != "" {
		c.mu.RLock()
		rx := c.pathRE[r.ID]
		c.mu.RUnlock()
		if rx == nil || !rx.MatchString(path) {
			return false
		}
	}
	return true
}

// SSLBypass reports whether host should tunnel without MITM.
func (c *Chain) SSLBypass(host string) bool {
	host = stripPort(host)
	c.mu.RLock()
	defer c.mu.RUnlock()
	for i := range c.rules {
		r := &c.rules[i]
		if !r.Enabled || r.Type != TypeSSLBypass {
			continue
		}
		if r.Match.Host == "" || hostMatch(r.Match.Host, host) {
			return true
		}
	}
	return false
}

// ClientAllowed returns false if access_control rules deny clientIP for host.
// No access_control rules → allowed.
func (c *Chain) ClientAllowed(clientIP, host string) bool {
	host = stripPort(host)
	c.mu.RLock()
	defer c.mu.RUnlock()
	var any bool
	for i := range c.rules {
		r := &c.rules[i]
		if !r.Enabled || r.Type != TypeAccessControl {
			continue
		}
		any = true
		if r.Match.Host != "" && !hostMatch(r.Match.Host, host) {
			continue
		}
		if r.AccessControl == nil {
			return false
		}
		if ipInCIDRs(clientIP, r.AccessControl.CIDRs) {
			if strings.EqualFold(r.AccessControl.Action, "deny") {
				return false
			}
			return true
		}
	}
	if !any {
		return true
	}
	return false
}

// SecondaryProxyURL returns upstream proxy URL for host if configured.
func (c *Chain) SecondaryProxyURL(host string) string {
	host = stripPort(host)
	c.mu.RLock()
	defer c.mu.RUnlock()
	for i := range c.rules {
		r := &c.rules[i]
		if !r.Enabled || r.Type != TypeSecondaryProxy || r.SecondaryProxy == nil {
			continue
		}
		if r.Match.Host == "" || hostMatch(r.Match.Host, host) {
			return r.SecondaryProxy.ProxyURL
		}
	}
	return ""
}

// ReverseProxies returns enabled reverse_proxy rules.
// HighlightFor returns the first matching enabled highlight rule for host/path/method.
func (c *Chain) HighlightFor(host, path, method string) (color, label string, ok bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for i := range c.rules {
		r := &c.rules[i]
		if !r.Enabled || r.Type != TypeHighlight || r.Highlight == nil {
			continue
		}
		if !c.matchRule(r, host, path, method) {
			continue
		}
		return r.Highlight.Color, r.Highlight.Label, true
	}
	return "", "", false
}

func (c *Chain) ReverseProxies() []Rule {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var out []Rule
	for i := range c.rules {
		if c.rules[i].Enabled && c.rules[i].Type == TypeReverseProxy && c.rules[i].ReverseProxy != nil {
			out = append(out, c.rules[i])
		}
	}
	return out
}

func ipInCIDRs(ipStr string, cidrs []string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	if len(cidrs) == 0 {
		return false
	}
	for _, c := range cidrs {
		if strings.Contains(c, "/") {
			_, n, err := net.ParseCIDR(c)
			if err == nil && n.Contains(ip) {
				return true
			}
			continue
		}
		if pip := net.ParseIP(c); pip != nil && pip.Equal(ip) {
			return true
		}
	}
	return false
}

// ResolveMapLocal returns absolute path under jail or error if escape.
//
// v1.1.7 测试报告 §3.6 把「map_local path jail 越界」列为质量风险。旧实现只做
// 词法检查(拒绝 ../ 与绝对路径), 但 jail 内的**符号链接**仍能把 os.ReadFile
// 引到 jail 外(如 jail/escape -> /etc/passwd); 父目录是软链时同理。这里在
// 词法检查之后解析「最深存在的祖先」的真实路径, 再用 jail 的真实路径做包含
// 判定 —— jail 自身是软链时也不会把合法文件误判越界。
func ResolveMapLocal(jail, rel string) (string, error) {
	if rel == "" {
		return "", fmt.Errorf("empty file")
	}
	if strings.ContainsRune(rel, 0) {
		return "", fmt.Errorf("NUL in path")
	}
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("absolute path not allowed")
	}
	clean := filepath.Clean(rel)
	if clean == ".." ||
		strings.HasPrefix(clean, ".."+string(filepath.Separator)) ||
		strings.Contains(clean, string(filepath.Separator)+"..") {
		return "", fmt.Errorf("path escape")
	}
	jAbs, err := filepath.Abs(jail)
	if err != nil {
		return "", err
	}
	jReal := jAbs
	if real, err := filepath.EvalSymlinks(jAbs); err == nil {
		jReal = real
	}
	full := filepath.Join(jAbs, clean)
	resolved, err := resolveExistingAncestor(full)
	if err != nil {
		return "", err
	}
	if !pathContained(jReal, resolved) {
		return "", fmt.Errorf("path escape")
	}
	return resolved, nil
}

// resolveExistingAncestor resolves the deepest existing ancestor of p (which
// itself need not exist yet) and re-appends the missing tail components.
// EvalSymlinks(p) alone fails for a not-yet-created file, but a symlinked
// parent directory could still redirect that file outside the jail.
func resolveExistingAncestor(p string) (string, error) {
	cur := p
	var tail []string
	for {
		if real, err := filepath.EvalSymlinks(cur); err == nil {
			for i := len(tail) - 1; i >= 0; i-- {
				real = filepath.Join(real, tail[i])
			}
			return real, nil
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return p, nil // reached filesystem root; caller's ReadFile will report missing
		}
		tail = append(tail, filepath.Base(cur))
		cur = parent
	}
}

func pathContained(root, p string) bool {
	root = filepath.Clean(root)
	p = filepath.Clean(p)
	return p == root || strings.HasPrefix(p, root+string(filepath.Separator))
}
