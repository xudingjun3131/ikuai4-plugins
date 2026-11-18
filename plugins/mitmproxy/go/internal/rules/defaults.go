package rules

// 内置规则集: 全新数据目录首次启动时自动写入 rules.json(已存在不覆盖,
// 用户删掉的规则不会被复活); 已装机可用 POST /api/rules/load-defaults
// 或规则面板的「内置规则」按钮随时载入。
//
// 选型原则: 服务"抓包 + 改包再转发"这条主线, 不改变网络的可见性
// (故意不放 host_block —— 抓包分析时看到什么比屏蔽什么更重要)。
// 全部默认启用, 不需要的在面板里一键停用。

// DefaultRules 返回内置规则(深拷贝, 调用方改不会影响下一份)。
func DefaultRules() []Rule {
	out := make([]Rule, len(builtinRules))
	copy(out, builtinRules)
	return out
}

var builtinRules = []Rule{
	// ---- 请求头: 让"改包"这门手艺能真正用上 ----
	{
		// 禁用缓存: 抓包不再吃 304, 每次都是真响应(改内容替换时尤其重要,
		// 否则浏览器拿本地缓存根本不发请求)
		ID:      "builtin-no-cache",
		Enabled: true,
		Type:    TypeHeader,
		Match:   Match{},
		Header: &HeaderMut{
			Phase:  "request",
			Set:    map[string]string{"Cache-Control": "no-cache", "Pragma": "no-cache"},
			Remove: []string{"If-None-Match", "If-Modified-Since"},
		},
	},
	{
		// 关键: hook 层拿到的是上游返回的原始字节, 响应带 gzip/br 时
		// 「内容替换」的明文字符串匹配不上。让上游返回明文, 替换才生效。
		ID:      "builtin-accept-encoding",
		Enabled: true,
		Type:    TypeHeader,
		Match:   Match{},
		Header: &HeaderMut{
			Phase: "request",
			Set:   map[string]string{"Accept-Encoding": "identity"},
		},
	},

	// ---- 响应头: 调试网页/接口时的两把钳子 ----
	{
		// 放宽 CORS: 把抓到的接口拉进自己的页面里调试
		ID:      "builtin-cors",
		Enabled: true,
		Type:    TypeHeader,
		Match:   Match{},
		Header: &HeaderMut{
			Phase: "response",
			Set: map[string]string{
				"Access-Control-Allow-Origin":  "*",
				"Access-Control-Allow-Methods": "*",
				"Access-Control-Allow-Headers": "*",
			},
		},
	},
	{
		// 摘掉 CSP / X-Frame-Options: 抓到的响应能嵌进 iframe 自审
		ID:      "builtin-strip-csp",
		Enabled: true,
		Type:    TypeHeader,
		Match:   Match{},
		Header: &HeaderMut{
			Phase:  "response",
			Remove: []string{"Content-Security-Policy", "Content-Security-Policy-Report-Only", "X-Frame-Options"},
		},
	},

	// ---- 高亮: 138 条流量里一眼分出主次 ----
	{
		ID:      "builtin-hl-api",
		Enabled: true,
		Type:    TypeHighlight,
		Match:   Match{Path: "^/api/"},
		Highlight: &Highlight{
			Color: "#3b82f6",
			Label: "API",
		},
	},
	{
		ID:      "builtin-hl-telemetry-clarity",
		Enabled: true,
		Type:    TypeHighlight,
		Match:   Match{Host: "*.clarity.ms"},
		Highlight: &Highlight{
			Color: "#ef4444",
			Label: "遥测",
		},
	},
	{
		ID:      "builtin-hl-telemetry-ga",
		Enabled: true,
		Type:    TypeHighlight,
		Match:   Match{Host: "*.google-analytics.com"},
		Highlight: &Highlight{
			Color: "#ef4444",
			Label: "遥测",
		},
	},
	{
		ID:      "builtin-hl-telemetry-gtm",
		Enabled: true,
		Type:    TypeHighlight,
		Match:   Match{Host: "*.googletagmanager.com"},
		Highlight: &Highlight{
			Color: "#ef4444",
			Label: "遥测",
		},
	},
	{
		ID:      "builtin-hl-telemetry-dc",
		Enabled: true,
		Type:    TypeHighlight,
		Match:   Match{Host: "*.doubleclick.net"},
		Highlight: &Highlight{
			Color: "#ef4444",
			Label: "遥测",
		},
	},
	{
		ID:      "builtin-hl-static",
		Enabled: true,
		Type:    TypeHighlight,
		Match:   Match{Path: `\.(js|mjs|css|png|jpe?g|gif|svg|webp|ico|woff2?|ttf|map)(\?|$)`},
		Highlight: &Highlight{
			Color: "#6b7280",
			Label: "静态",
		},
	},
}

// IsBuiltinID 报告 id 是否为内置规则(面板可据此给个"内置"角标)。
func IsBuiltinID(id string) bool {
	return len(id) > 8 && id[:8] == "builtin-"
}
