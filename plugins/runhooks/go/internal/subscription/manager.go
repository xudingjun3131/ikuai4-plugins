package subscription

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/GitterBois/runhooks/internal/proxy"
	"github.com/GitterBois/runhooks/internal/rules"
	"github.com/GitterBois/runhooks/internal/script"
)

const (
	maxSubBytes = 8 << 20 // 订阅原文上限 8MB
	defaultUA   = "Mozilla/5.0 (compatible; runhooks-subscription/1.0)"
)

// Manager 订阅管理器: CRUD + 拉取 + 解析适配 + 定时更新。
// 适配产物只写三个"订阅层"文件, 与用户配置物理分离:
//   - scope.sub.json   ( ScopeStore 自动合并 )
//   - rules.sub.json   ( Chain.SaveSubRules )
//   - scripts/sub-*.js ( script.Gallery 的 scripts 目录 )
type Manager struct {
	dataDir string // <dataDir>/subscriptions
	subsPath string
	scope   *proxy.ScopeStore
	chain   *rules.Chain
	gallery *script.Gallery
	log     *slog.Logger

	mu   sync.Mutex
	subs []Sub

	client *http.Client

	// 变更回调(app 注入: 重挂引擎 slot + 热重载 scope)
	OnRulesChanged func()
	OnScopeChanged func()

	stopCh chan struct{}
	wg     sync.WaitGroup
}

// NewManager 创建管理器并加载已持久化的订阅列表。
func NewManager(dataDir string, scope *proxy.ScopeStore, chain *rules.Chain, gallery *script.Gallery, log *slog.Logger) (*Manager, error) {
	if log == nil {
		log = slog.Default()
	}
	m := &Manager{
		dataDir:  filepath.Join(dataDir, "subscriptions"),
		scope:    scope,
		chain:    chain,
		gallery:  gallery,
		log:      log,
		client:   &http.Client{Timeout: 30 * time.Second},
		stopCh:   make(chan struct{}),
	}
	m.subsPath = filepath.Join(m.dataDir, "subs.json")
	if err := os.MkdirAll(m.dataDir, 0o700); err != nil {
		return nil, err
	}
	if err := m.load(); err != nil {
		return nil, err
	}
	// 启动时把已持久化的适配产物重新应用一次(引擎重启后自愈)
	m.applyAll()
	return m, nil
}

func (m *Manager) load() error {
	b, err := os.ReadFile(m.subsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var f struct {
		Subs []Sub `json:"subs"`
	}
	if err := json.Unmarshal(b, &f); err != nil {
		return fmt.Errorf("subs.json 解析失败: %w", err)
	}
	m.subs = f.Subs
	return nil
}

func (m *Manager) saveLocked() error {
	f := struct {
		Subs []Sub `json:"subs"`
	}{Subs: m.subs}
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	tmp := m.subsPath + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, m.subsPath)
}

// ---- 查询 ----

func (m *Manager) List() []Sub {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Sub, len(m.subs))
	copy(out, m.subs)
	return out
}

func (m *Manager) Get(id string) (Sub, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.subs {
		if s.ID == id {
			return s, true
		}
	}
	return Sub{}, false
}

// ParsedOf 返回某订阅的解析结果(含 warnings)。
func (m *Manager) ParsedOf(id string) (*Parsed, error) {
	b, err := os.ReadFile(filepath.Join(m.dataDir, id+".parsed.json"))
	if err != nil {
		return nil, err
	}
	var p Parsed
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, err
	}
	// [nil-slice] 空数组在 JSON 里是 null, 前端 .length/.map 会崩——归一成 []
	if p.MitmHosts == nil {
		p.MitmHosts = []string{}
	}
	if p.Rules == nil {
		p.Rules = []Rule{}
	}
	if p.Rewrites == nil {
		p.Rewrites = []Rewrite{}
	}
	if p.Scripts == nil {
		p.Scripts = []Script{}
	}
	if p.Warnings == nil {
		p.Warnings = []string{}
	}
	return &p, nil
}

// RawOf 返回订阅原文(排查看板用)。
func (m *Manager) RawOf(id string) (string, error) {
	b, err := os.ReadFile(filepath.Join(m.dataDir, id+".raw"))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// Effective 返回当前生效的订阅层合并结果(页面"生效范围"展示)。
func (m *Manager) Effective() (ScopePatch, int, int) {
	m.mu.Lock()
	ids := make([]string, 0, len(m.subs))
	for _, s := range m.subs {
		if s.Enabled {
			ids = append(ids, s.ID)
		}
	}
	m.mu.Unlock()
	var patch ScopePatch
	ruleN, scriptN := 0, 0
	for _, id := range ids {
		p, err := m.ParsedOf(id)
		if err != nil {
			continue
		}
		a := Adapt(p)
		patch.Domains = append(patch.Domains, a.Scope.Domains...)
		patch.DstIPs = append(patch.DstIPs, a.Scope.DstIPs...)
		patch.Countries = append(patch.Countries, a.Scope.Countries...)
		patch.Paths = append(patch.Paths, a.Scope.Paths...)
		patch.UserAgents = append(patch.UserAgents, a.Scope.UserAgents...)
		ruleN += len(a.Rules)
		sc, _ := adaptScripts(p, id)
		scriptN += len(sc)
	}
	return patch, ruleN, scriptN
}

// ---- 变更 ----

// Add 新增订阅: 立刻拉取+解析+适配+生效。
func (m *Manager) Add(rawURL, name string, format Format, intervalMin int) (Sub, error) {
	rawURL = strings.TrimSpace(rawURL)
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		return Sub{}, fmt.Errorf("订阅地址必须是 http(s) URL")
	}
	if intervalMin < 0 {
		intervalMin = 0
	}
	if intervalMin > 0 && intervalMin < 5 {
		intervalMin = 5 // 最小 5 分钟, 防把源站打爆
	}
	m.mu.Lock()
	for _, s := range m.subs {
		if s.URL == rawURL {
			m.mu.Unlock()
			return Sub{}, fmt.Errorf("该订阅地址已存在(%s)", s.Name)
		}
	}
	sub := Sub{
		ID:          fmt.Sprintf("sub-%d", time.Now().UnixNano()%1000000000),
		Name:        strings.TrimSpace(name),
		URL:         rawURL,
		Format:      format,
		Enabled:     true,
		IntervalMin: intervalMin,
	}
	if sub.Name == "" {
		sub.Name = rawURL
	}
	m.subs = append(m.subs, sub)
	if err := m.saveLocked(); err != nil {
		m.mu.Unlock()
		return Sub{}, err
	}
	m.mu.Unlock()
	if err := m.Update(sub.ID); err != nil {
		return sub, err
	}
	return sub, nil
}

// Remove 删除订阅并重新应用剩余订阅。
func (m *Manager) Remove(id string) error {
	m.mu.Lock()
	idx := -1
	for i, s := range m.subs {
		if s.ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		m.mu.Unlock()
		return fmt.Errorf("订阅不存在: %s", id)
	}
	m.subs = append(m.subs[:idx], m.subs[idx+1:]...)
	if err := m.saveLocked(); err != nil {
		m.mu.Unlock()
		return err
	}
	m.mu.Unlock()
	_ = os.Remove(filepath.Join(m.dataDir, id+".parsed.json"))
	_ = os.Remove(filepath.Join(m.dataDir, id+".raw"))
	m.applyAll()
	return nil
}

// SetEnabled 启用/暂停订阅(暂停=不参与合并, 保留解析结果)。
func (m *Manager) SetEnabled(id string, on bool) error {
	m.mu.Lock()
	for i := range m.subs {
		if m.subs[i].ID == id {
			m.subs[i].Enabled = on
			if err := m.saveLocked(); err != nil {
				m.mu.Unlock()
				return err
			}
			m.mu.Unlock()
			m.applyAll()
			return nil
		}
	}
	m.mu.Unlock()
	return fmt.Errorf("订阅不存在: %s", id)
}

// SetMeta 改备注名/自动更新间隔。
func (m *Manager) SetMeta(id, name string, intervalMin int) error {
	m.mu.Lock()
	for i := range m.subs {
		if m.subs[i].ID == id {
			if name != "" {
				m.subs[i].Name = name
			}
			if intervalMin >= 0 {
				if intervalMin > 0 && intervalMin < 5 {
					intervalMin = 5
				}
				m.subs[i].IntervalMin = intervalMin
			}
			err := m.saveLocked()
			m.mu.Unlock()
			return err
		}
	}
	m.mu.Unlock()
	return fmt.Errorf("订阅不存在: %s", id)
}

// Update 重新拉取并解析某订阅, 成功后重新应用全部。
func (m *Manager) Update(id string) error {
	m.mu.Lock()
	var sub *Sub
	for i := range m.subs {
		if m.subs[i].ID == id {
			sub = &m.subs[i]
			break
		}
	}
	if sub == nil {
		m.mu.Unlock()
		return fmt.Errorf("订阅不存在: %s", id)
	}
	urlStr := sub.URL
	m.mu.Unlock()

	raw, err := m.fetch(urlStr, "")
	if err != nil {
		m.setError(id, err.Error())
		return err
	}
	p := Parse(string(raw), sub.Format)
	// 抓取订阅引用的脚本(相对路径按订阅 URL 所在目录解析)
	m.fetchScripts(p, urlStr)
	a := Adapt(p)
	sc, scWarns := adaptScripts(p, id)
	p.Warnings = append(p.Warnings, scWarns...)

	// 持久化原文+解析结果
	if err := os.WriteFile(filepath.Join(m.dataDir, id+".raw"), raw, 0o600); err != nil {
		m.setError(id, err.Error())
		return err
	}
	pb, _ := json.MarshalIndent(p, "", "  ")
	if err := os.WriteFile(filepath.Join(m.dataDir, id+".parsed.json"), pb, 0o600); err != nil {
		m.setError(id, err.Error())
		return err
	}

	m.mu.Lock()
	for i := range m.subs {
		if m.subs[i].ID == id {
			m.subs[i].LastUpdate = time.Now().Unix()
			m.subs[i].LastError = ""
			m.subs[i].RuleCount = len(p.Rules)
			m.subs[i].HostCount = countTypes(p.Rules, "DOMAIN", "DOMAIN-SUFFIX", "DOMAIN-KEYWORD")
			m.subs[i].IPCount = countTypes(p.Rules, "IP-CIDR", "IP-CIDR6")
			m.subs[i].RewriteCount = len(p.Rewrites)
			m.subs[i].ScriptCount = len(sc)
			m.subs[i].SkippedCount = p.Skipped
			if err := m.saveLocked(); err != nil {
				m.mu.Unlock()
				return err
			}
			break
		}
	}
	m.mu.Unlock()
	_ = a // 适配产物在 applyAll 统一重算
	m.applyAll()
	return nil
}

// UpdateAll 更新全部启用且到期的订阅(定时器调用)。
func (m *Manager) UpdateAll() {
	m.mu.Lock()
	due := make([]string, 0, len(m.subs))
	now := time.Now().Unix()
	for _, s := range m.subs {
		if !s.Enabled || s.IntervalMin <= 0 {
			continue
		}
		if now-s.LastUpdate >= int64(s.IntervalMin)*60 {
			due = append(due, s.ID)
		}
	}
	m.mu.Unlock()
	for _, id := range due {
		if err := m.Update(id); err != nil {
			m.log.Warn("subscription auto-update failed", "id", id, "err", err)
		}
	}
}

func (m *Manager) setError(id, msg string) {
	m.mu.Lock()
	for i := range m.subs {
		if m.subs[i].ID == id {
			m.subs[i].LastError = msg
			_ = m.saveLocked()
			break
		}
	}
	m.mu.Unlock()
}

// ---- 拉取 ----

func (m *Manager) fetch(urlStr, ua string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, urlStr, nil)
	if err != nil {
		return nil, err
	}
	if ua != "" {
		req.Header.Set("User-Agent", ua)
	} else {
		req.Header.Set("User-Agent", defaultUA)
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("拉取失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxSubBytes))
	if err != nil {
		return nil, fmt.Errorf("读取失败: %w", err)
	}
	return b, nil
}

// fetchScripts 抓取订阅引用的脚本内容; 相对路径按订阅 URL 所在目录解析。
func (m *Manager) fetchScripts(p *Parsed, subURL string) {
	base, err := url.Parse(subURL)
	if err != nil {
		return
	}
	for i := range p.Scripts {
		sc := &p.Scripts[i]
		if sc.Source != "" {
			continue
		}
		target := sc.URL
		if target == "" {
			// 相对路径: 拼到订阅 conf 所在目录
			ref, err := url.Parse(sc.Name)
			if err != nil {
				sc.FetchErr = "文件名无法解析"
				continue
			}
			target = base.ResolveReference(ref).String()
		}
		b, err := m.fetch(target, "")
		if err != nil {
			sc.FetchErr = err.Error()
			continue
		}
		sc.Source = string(b)
	}
}

// ---- 应用(合并订阅层) ----

// applyAll 按当前启用的订阅重算三个订阅层文件并热生效。
func (m *Manager) applyAll() {
	m.mu.Lock()
	type item struct {
		id   string
		pars *Parsed
	}
	var items []item
	for _, s := range m.subs {
		if !s.Enabled {
			continue
		}
		p, err := m.ParsedOf(s.ID)
		if err != nil {
			continue
		}
		items = append(items, item{id: s.ID, pars: p})
	}
	scriptsDir := ""
	if m.gallery != nil {
		scriptsDir = m.gallery.ScriptsDir()
	}
	m.mu.Unlock()

	// 1) scope.sub.json
	var patch ScopePatch
	for _, it := range items {
		a := Adapt(it.pars)
		patch.Domains = append(patch.Domains, a.Scope.Domains...)
		patch.DstIPs = append(patch.DstIPs, a.Scope.DstIPs...)
		patch.Countries = append(patch.Countries, a.Scope.Countries...)
		patch.Paths = append(patch.Paths, a.Scope.Paths...)
		patch.UserAgents = append(patch.UserAgents, a.Scope.UserAgents...)
	}
	if m.scope != nil {
		if err := m.writeSubScope(patch); err != nil {
			m.log.Error("write scope.sub.json", "err", err)
		}
	}

	// 2) rules.sub.json
	var subRules []rules.Rule
	for _, it := range items {
		a := Adapt(it.pars)
		for i, ar := range a.Rules {
			subRules = append(subRules, toEngineRule(it.id, i, ar))
		}
	}
	if m.chain != nil {
		if err := m.chain.SaveSubRules(subRules); err != nil {
			m.log.Error("write rules.sub.json", "err", err)
		}
	}

	// 3) scripts/sub-*.js(先删旧的 sub-* 再写新的)
	if scriptsDir != "" {
		if entries, err := os.ReadDir(scriptsDir); err == nil {
			for _, e := range entries {
				if !e.IsDir() && strings.HasPrefix(e.Name(), "sub-") && strings.HasSuffix(e.Name(), ".js") {
					_ = os.Remove(filepath.Join(scriptsDir, e.Name()))
				}
			}
		}
		for _, it := range items {
			sc, _ := adaptScripts(it.pars, it.id)
			for _, s := range sc {
				if err := os.WriteFile(filepath.Join(scriptsDir, s.FileName), []byte(s.Source), 0o600); err != nil {
					m.log.Error("write sub script", "file", s.FileName, "err", err)
				}
			}
		}
	}

	// 4) 热生效
	if m.OnRulesChanged != nil {
		m.OnRulesChanged()
	}
	if m.OnScopeChanged != nil {
		m.OnScopeChanged()
	}
}

func (m *Manager) writeSubScope(patch ScopePatch) error {
	sp := proxy.Scope{
		Domains:    patch.Domains,
		DstIPs:     patch.DstIPs,
		Countries:  patch.Countries,
		Paths:      patch.Paths,
		UserAgents: patch.UserAgents,
	}
	b, err := json.MarshalIndent(sp, "", "  ")
	if err != nil {
		return err
	}
	path := m.scope.SubPath()
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// toEngineRule 把适配规则转成引擎规则(ID 带 sub-<id>- 前缀, 卸载/更新时整批识别)。
func toEngineRule(subID string, idx int, ar AdaptRule) rules.Rule {
	r := rules.Rule{
		ID:      fmt.Sprintf("%s%s-%d", rules.SubRulePrefix, subID, idx),
		Enabled: true,
		Type:    rules.Type(ar.Type),
		Match:   rules.Match{Host: ar.Host, Path: ar.Path},
	}
	switch ar.Type {
	case "header":
		if ar.Header != nil {
			r.Header = &rules.HeaderMut{Phase: ar.Header.Phase, Set: ar.Header.Set, Remove: ar.Header.Remove}
		}
	case "body_replace":
		if ar.Body != nil {
			r.BodyReplace = &rules.BodyReplace{Phase: ar.Body.Phase, From: ar.Body.From, To: ar.Body.To}
		}
	case "map_local":
		if ar.MapLocal != nil {
			r.MapLocal = &rules.MapLocal{Status: ar.MapLocal.Status, Body: ar.MapLocal.Body}
		}
	case "mirror_remote":
		if ar.Mirror != nil {
			r.MirrorRemote = &rules.MirrorRemote{URL: ar.Mirror.URL}
		}
	case "gateway":
		if ar.Gateway != nil {
			r.Gateway = &rules.Gateway{Upstream: ar.Gateway.Upstream}
		}
	}
	return r
}

func countTypes(rs []Rule, types ...string) int {
	n := 0
	for _, r := range rs {
		for _, t := range types {
			if r.Type == t {
				n++
				break
			}
		}
	}
	return n
}

// ---- 定时器 ----

// Start 启动自动更新调度(每分钟 tick, 到期的订阅才拉)。
func (m *Manager) Start() {
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		t := time.NewTicker(time.Minute)
		defer t.Stop()
		for {
			select {
			case <-m.stopCh:
				return
			case <-t.C:
				m.UpdateAll()
			}
		}
	}()
}

// Stop 停止调度。
func (m *Manager) Stop() {
	select {
	case <-m.stopCh:
	default:
		close(m.stopCh)
	}
	m.wg.Wait()
}
