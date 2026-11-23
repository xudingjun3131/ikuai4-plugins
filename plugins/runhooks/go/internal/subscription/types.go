// Package subscription 实现规则订阅: 拉取 QuanX/Surge/纯列表 格式的规则订阅,
// 解析并适配成引擎的范围(scope.sub.json)/规则(rules.sub.json)/脚本,
// 支持定时自动更新。与用户手写配置物理分离, 永不互相覆盖。
package subscription

// Format 订阅格式。
type Format string

const (
	FormatAuto  Format = "auto"
	FormatQuanX Format = "quanx"
	FormatSurge Format = "surge"
	FormatList  Format = "list"
)

// Sub 是一条订阅(持久化在 subscriptions/subs.json)。
type Sub struct {
	ID          string `json:"id"`           // sub-<rand>
	Name        string `json:"name"`         // 备注名(页面可改)
	URL         string `json:"url"`          // 订阅地址(http/https)
	Format      Format `json:"format"`       // auto|quanx|surge|list
	Enabled     bool   `json:"enabled"`      // false=暂停适配(不参与合并)
	IntervalMin int    `json:"interval_min"` // 自动更新间隔(分钟); 0=仅手动
	UA          string `json:"ua,omitempty"` // 可选自定义 User-Agent
	LastUpdate  int64  `json:"last_update"`  // unix 秒
	LastError   string `json:"last_error,omitempty"`

	// 统计(解析后回填, 随 subs.json 持久化, 页面列表直接读)
	RuleCount    int `json:"rule_count"`
	HostCount    int `json:"host_count"` // 域名类规则数
	IPCount      int `json:"ip_count"`   // IP 类规则数
	RewriteCount int `json:"rewrite_count"`
	ScriptCount  int `json:"script_count"`
	SkippedCount int `json:"skipped_count"` // 不支持/忽略的行数
}

// RemoteRef 是 QX 主配置 [filter_remote]/[rewrite_remote] 段里的远程引用。
// 例: https://x/list, force-policy=REJECT, update-interval=172800, enabled=true
type RemoteRef struct {
	URL         string `json:"url"`
	Kind        string `json:"kind"` // filter|rewrite
	ForcePolicy string `json:"force_policy,omitempty"`
	Line        int    `json:"line"`
}

// Parsed 是订阅原文的解析结果(持久化在 subscriptions/<id>.parsed.json)。
type Parsed struct {
	Format     string      `json:"format"`
	MitmHosts  []string    `json:"mitm_hosts"`  // [mitm]/[MITM] hostname
	Rules      []Rule      `json:"rules"`       // 域名/IP/UA 类规则
	Rewrites   []Rewrite   `json:"rewrites"`    // [rewrite]/[Script] 改写指令
	Scripts    []Script    `json:"scripts"`     // 引用的脚本(内容已尝试抓取)
	RemoteRefs []RemoteRef `json:"remote_refs"` // QX 主配置引用的远程规则/重写(manager 展开)
	Warnings   []string    `json:"warnings"`    // 人类可读的跳过/降级说明
	Skipped    int         `json:"skipped"`     // 跳过行数
}

// Rule 一条域名/IP 类规则(统一 QuanX/Surge 语法)。
type Rule struct {
	Type      string `json:"type"` // DOMAIN/DOMAIN-SUFFIX/DOMAIN-KEYWORD/IP-CIDR/GEOIP/URL-REGEX/USER-AGENT...
	Value     string `json:"value"`
	Policy    string `json:"policy"` // PROXY/DIRECT/REJECT/... (纯列表为 "")
	NoResolve bool   `json:"no_resolve,omitempty"`
	Line      int    `json:"line"`
	Raw       string `json:"raw"`
}

// Rewrite 一条改写指令。
type Rewrite struct {
	Pattern string `json:"pattern"` // URL 匹配式(正则)
	Kind    string `json:"kind"`    // url|header|request-header|response-header|request-body|response-body|reject|script-*
	Value   string `json:"value"`   // 指令值(URL/头值/文件名)
	Line    int    `json:"line"`
	Raw     string `json:"raw"`
}

// Script 一条脚本引用(内容已尝试抓取; FetchErr 非空=抓取失败)。
type Script struct {
	Name     string `json:"name"`   // 文件名或 URL 末段
	Kind     string `json:"kind"`   // response-body|request-header|request-body|echo-response|filter
	Source   string `json:"source"` // JS 原文(抓取成功时)
	URL      string `json:"url,omitempty"`
	FetchErr string `json:"fetch_err,omitempty"`
	Line     int    `json:"line"`
}

// Adapted 是 Parsed 适配成引擎配置的产物。
type Adapted struct {
	Scope    ScopePatch  // → scope.sub.json
	Rules    []AdaptRule // → rules.sub.json
	Scripts  []AdaptScript
	Warnings []string // 适配期的降级/丢弃说明(与 Parsed.Warnings 分开, 面向"为什么没生效")
}

// ScopePatch 订阅贡献的范围(白名单语义, 由 manager 合并)。
type ScopePatch struct {
	Domains    []string
	DstIPs     []string
	Countries  []string
	Paths      []string
	UserAgents []string
}

// AdaptRule 适配出的引擎规则(不含 ID, manager 统一加 sub-<id>- 前缀)。
type AdaptRule struct {
	Type   string // 引擎规则类型(ssl_bypass/header/body_replace/map_local/mirror_remote/highlight)
	Host   string // 匹配 host(通配)
	Path   string // 匹配 path(正则)
	Header *struct {
		Phase  string
		Set    map[string]string
		Remove []string
	}
	Body *struct {
		Phase string
		From  string
		To    string
	}
	MapLocal *struct {
		Status int
		File   string
		Body   string
	}
	Mirror *struct {
		URL string
	}
	Gateway *struct {
		Upstream string
	}
	Highlight *struct {
		Color string
		Label string
	}
	Note string // 来源说明(写进规则 ID/备注, 便于溯源)
}

// AdaptScript 适配出的脚本(manager 写 scripts 目录)。
type AdaptScript struct {
	FileName string // sub-<id>-<n>.js
	Source   string // 转换后的引擎 JS
	Note     string
}
