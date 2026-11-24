package subscription

import (
	"context"
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
	maxSubBytes = 16 << 20 // 订阅原文上限 16MB(yfamilys AntiAD.list 约 9.7MB)
	defaultUA   = "Mozilla/5.0 (compatible; runhooks-subscription/1.0)"
	// [P2-2] 全局在途 fetch 并发上限: 展开远程引用/脚本时的有界并发,
	// 防止多订阅并发添加或大索引页把源站打爆(TLS 限流)。
	maxConcurrentFetches = 6
	// [P2-2] 单次 Update(主订阅 + 展开 + 脚本)整体超时, 防并发下限流时 >240s 无返回。
	// 单条 client 超时仍为 30s; 整体超时兜住"很多慢引用排队"的累积拖尾。
	updateTimeout = 120 * time.Second
)

// Manager 订阅管理器: CRUD + 拉取 + 解析适配 + 定时更新。
// 适配产物只写三个"订阅层"文件, 与用户配置物理分离:
//   - scope.sub.json   ( ScopeStore 自动合并 )
//   - rules.sub.json   ( Chain.SaveSubRules )
//   - scripts/sub-*.js ( script.Gallery 的 scripts 目录 )
type Manager struct {
	dataDir  string // <dataDir>/subscriptions
	subsPath string
	scope    *proxy.ScopeStore
	chain    *rules.Chain
	gallery  *script.Gallery
	log      *slog.Logger

	mu   sync.Mutex
	subs []Sub

	client *http.Client

	// [P2-2] 全局 fetch 并发闸门(有界并发展开远程引用/脚本, 防源站限流)
	fetchSem  chan struct{}
	fetchOnce sync.Once // 惰性初始化, 兼容直接 &Manager{} 构造的测试/调用

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
		fetchSem: make(chan struct{}, maxConcurrentFetches),
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
	if p.RemoteRefs == nil {
		p.RemoteRefs = []RemoteRef{}
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
	rawURL = normalizeSubURL(rawURL)
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
		// 落盘失败: 同步回滚刚 append 的记录, 避免内存/磁盘不一致
		m.subs = m.subs[:len(m.subs)-1]
		m.mu.Unlock()
		return Sub{}, err
	}
	m.mu.Unlock()
	if err := m.Update(sub.ID); err != nil {
		// [P2-1] 首拉失败必须回滚: 不留 rules=0/last_error 的坏订阅,
		// 也不堵住同 URL 的重试(否则下一次 Add 命中"该订阅地址已存在"400)。
		m.rollbackAdd(sub.ID)
		return Sub{}, err
	}
	return sub, nil
}

// rollbackAdd 删除一条刚由 Add 创建、但首拉失败的订阅(记录 + 残留文件 + 重新应用)。
// 只按 ID 精确删除, 不影响并发期间其它 Add 创建的订阅。
func (m *Manager) rollbackAdd(id string) {
	m.mu.Lock()
	for i := range m.subs {
		if m.subs[i].ID == id {
			m.subs = append(m.subs[:i], m.subs[i+1:]...)
			break
		}
	}
	_ = m.saveLocked()
	m.mu.Unlock()
	_ = os.Remove(filepath.Join(m.dataDir, id+".raw"))
	_ = os.Remove(filepath.Join(m.dataDir, id+".parsed.json"))
	m.applyAll()
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

	// [P2-2] 单次更新整体超时, 覆盖主订阅拉取 + 远程引用展开 + 脚本抓取。
	ctx, cancel := context.WithTimeout(context.Background(), updateTimeout)
	defer cancel()

	raw, err := m.fetchCtx(ctx, urlStr, "")
	if err != nil {
		m.setError(id, err.Error())
		return err
	}
	p := Parse(string(raw), sub.Format)
	// 展开 QX 主配置 [filter_remote]/[rewrite_remote] 引用的远程规则/重写(有界并发)
	m.expandRemoteRefsCtx(ctx, p, urlStr)
	// 抓取订阅引用的脚本(相对路径按订阅 URL 所在目录解析, 有界并发)
	m.fetchScriptsCtx(ctx, p, urlStr)
	if err := ctx.Err(); err != nil {
		e := fmt.Errorf("订阅更新超时(%s): %w", updateTimeout, err)
		m.setError(id, e.Error())
		return e
	}
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

// acquireFetch/releaseFetch: 全局有界并发闸门。惰性初始化, 使 &Manager{} 直接构造
// 的轻量测试也自动获得限流; 所有拉取(主订阅/远程引用/脚本/自动更新)共享同一上限,
// 防止多订阅并添加或大索引页把源站打爆(TLS 限流)。
func (m *Manager) acquireFetch() {
	m.fetchOnce.Do(func() {
		if m.fetchSem == nil {
			m.fetchSem = make(chan struct{}, maxConcurrentFetches)
		}
	})
	m.fetchSem <- struct{}{}
}
func (m *Manager) releaseFetch() { <-m.fetchSem }

// fetch 拉取单个 URL(无整体超时上下文, 供脚本抓取 / 旧调用点 / 测试使用)。
func (m *Manager) fetch(urlStr, ua string) ([]byte, error) {
	return m.fetchCtx(context.Background(), urlStr, ua)
}

// fetchCtx 拉取单个 URL; 进入即占用一个全局并发额度, 返回时释放。
// ctx 用于单次 Update 的整体超时: 超时会中断在途请求, 不再让按钮长时间无响应。
func (m *Manager) fetchCtx(ctx context.Context, urlStr, ua string) ([]byte, error) {
	m.acquireFetch()
	defer m.releaseFetch()
	// 防御: subs.json 里可能残留旧格式 URL(如 "@url:`https://x`"), Update() 不经过 Add(),
	// 直接在拉取入口再规范化一次, 避免拿整串 Markdown 标记去发 HTTP 请求。
	urlStr = normalizeSubURL(urlStr)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
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
	if err := checkSubSize(resp.ContentLength); err != nil {
		return nil, err
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxSubBytes+1))
	if err != nil {
		return nil, fmt.Errorf("读取失败: %w", err)
	}
	if err := checkSubSize(int64(len(b))); err != nil {
		return nil, err
	}
	return b, nil
}

// checkSubSize 明确拒绝超限订阅, 不再静默截断(旧版 8MB 会把 AntiAD.list 切掉半行)。
func checkSubSize(n int64) error {
	if n > maxSubBytes {
		return fmt.Errorf("订阅原文 %d 字节超过上限 %d 字节, 拒绝静默截断", n, maxSubBytes)
	}
	return nil
}

// normalizeSubURL 规范化用户从聊天/聚合器复制来的订阅地址。
// 兼容 "@url:`https://x`"、"`@url:https://x`"、尖括号包裹等整体粘贴形态;
// 反复剥离包装(最多 4 轮), 直到不再变化。
func normalizeSubURL(s string) string {
	for i := 0; i < 4; i++ {
		prev := s
		s = strings.TrimSpace(s)
		s = strings.Trim(s, "`\"'<>")
		s = strings.TrimSpace(s)
		if len(s) >= 5 && strings.EqualFold(s[:5], "@url:") {
			s = s[5:]
		} else if len(s) >= 5 && strings.EqualFold(s[:5], "@url=") {
			s = s[5:]
		}
		s = strings.TrimSpace(s)
		if s == prev {
			break
		}
	}
	return s
}

// expandRemoteRefs 兼容包装(无整体超时 ctx, 供测试/旧调用点)。
func (m *Manager) expandRemoteRefs(p *Parsed, subURL string) {
	m.expandRemoteRefsCtx(context.Background(), p, subURL)
}

type refJob struct {
	refs  []RemoteRef
	base  string
	depth int
}
type refTask struct {
	ref    RemoteRef
	target string
	depth  int
}
type refResult struct {
	task refTask
	raw  []byte
	err  error
}

// expandRemoteRefsCtx 展开 QX 主配置 [filter_remote]/[rewrite_remote] 段的远程引用。
// [P2-2] 同一层引用并发展开(每个 fetchCtx 自带全局闸门, 峰值受 maxConcurrentFetches 限制),
// 结果按任务原始顺序合并回 p, 保证规则顺序确定且无数据竞争。
// 迭代队列按层 BFS, 最多 3 层 / 100 个 URL, 防循环引用和打爆源站; 单条失败只记 warning 不中断。
// force-policy=REJECT 的引用把拉回的规则标 REJECT(Adapt 转成 ssl_bypass/host_block)。
func (m *Manager) expandRemoteRefsCtx(ctx context.Context, p *Parsed, subURL string) {
	queue := []refJob{{refs: p.RemoteRefs, base: subURL, depth: 0}}
	seen := make(map[string]bool)
	fetched := 0
	for len(queue) > 0 {
		// 1) 本层: 纯计算, 把 queue 展开成去重后的任务列表(顺序稳定, 无 IO)
		var tasks []refTask
		stopped := false
		var next []refJob
		for _, j := range queue {
			if j.depth > 2 {
				p.Warnings = append(p.Warnings, "remote 引用超过 3 层, 已停止展开(防循环)")
				continue
			}
			base, _ := url.Parse(j.base)
			for _, ref := range j.refs {
				// 5 类客户端资源索引页单个页面就有 400+ 链接; 与 parseIndex 的 100 条上限对齐。
				if fetched >= 100 {
					p.Warnings = append(p.Warnings, "remote 引用超过 100 个(客户端资源索引页), 已停止展开; 需要更多请直接填具体资源 URL")
					stopped = true
					break
				}
				target := ref.URL
				if !strings.HasPrefix(strings.ToLower(target), "http") {
					if base == nil {
						continue
					}
					r, err := url.Parse(target)
					if err != nil {
						continue
					}
					target = base.ResolveReference(r).String()
				}
				if seen[target] {
					continue
				}
				seen[target] = true
				fetched++
				tasks = append(tasks, refTask{ref: ref, target: target, depth: j.depth})
			}
			if stopped {
				break
			}
		}
		if len(tasks) == 0 {
			break
		}

		// 2) 并发抓取本层(结果写各自槽位, 无竞争)
		results := make([]refResult, len(tasks))
		var wg sync.WaitGroup
		for i := range tasks {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				raw, err := m.fetchCtx(ctx, tasks[i].target, "")
				results[i] = refResult{task: tasks[i], raw: raw, err: err}
			}(i)
		}
		wg.Wait()

		// 3) 单 goroutine 按任务顺序合并, 顺序与串行版一致
		for _, r := range results {
			if r.err != nil {
				p.Warnings = append(p.Warnings, fmt.Sprintf("remote 引用 %s 拉取失败: %v", r.task.target, r.err))
				continue
			}
			sub := Parse(string(r.raw), FormatAuto)
			if isRejectPolicy(r.task.ref.ForcePolicy) {
				for i := range sub.Rules {
					sub.Rules[i].Policy = "REJECT"
				}
			}
			p.Rules = append(p.Rules, sub.Rules...)
			p.Rewrites = append(p.Rewrites, sub.Rewrites...)
			p.MitmHosts = append(p.MitmHosts, sub.MitmHosts...)
			p.Scripts = append(p.Scripts, sub.Scripts...)
			p.Warnings = append(p.Warnings, sub.Warnings...)
			p.Skipped += sub.Skipped
			p.Warnings = append(p.Warnings, fmt.Sprintf("remote 引用 %s 展开: %d 条规则 / %d 条改写", r.task.target, len(sub.Rules), len(sub.Rewrites)))
			if len(sub.RemoteRefs) > 0 {
				next = append(next, refJob{refs: sub.RemoteRefs, base: r.task.target, depth: r.task.depth + 1})
			}
		}
		if stopped {
			break
		}
		queue = next
	}
}

// fetchScripts 兼容包装(无 ctx)。
func (m *Manager) fetchScripts(p *Parsed, subURL string) {
	m.fetchScriptsCtx(context.Background(), p, subURL)
}

// fetchScriptsCtx 抓取订阅引用的脚本内容; 相对路径按订阅 URL 所在目录解析。
// [P2-2] 并发展开(受全局闸门限流), 结果按索引写回。
func (m *Manager) fetchScriptsCtx(ctx context.Context, p *Parsed, subURL string) {
	base, err := url.Parse(subURL)
	if err != nil {
		return
	}
	type scTask struct {
		idx    int
		target string
	}
	var tasks []scTask
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
		tasks = append(tasks, scTask{idx: i, target: target})
	}
	if len(tasks) == 0 {
		return
	}
	type scResult struct {
		b   []byte
		err error
	}
	results := make([]scResult, len(tasks))
	var wg sync.WaitGroup
	for k := range tasks {
		wg.Add(1)
		go func(k int) {
			defer wg.Done()
			b, err := m.fetchCtx(ctx, tasks[k].target, "")
			results[k] = scResult{b: b, err: err}
		}(k)
	}
	wg.Wait()
	for k, r := range results {
		sc := &p.Scripts[tasks[k].idx]
		if r.err != nil {
			sc.FetchErr = r.err.Error()
			continue
		}
		sc.Source = string(r.b)
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
