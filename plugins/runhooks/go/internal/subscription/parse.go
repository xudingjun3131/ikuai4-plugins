package subscription

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// DetectFormat 猜测订阅格式(format=auto 时用)。
// [general]/[mitm] 两种格式都有, 只认各自独有的段名; 都认不出时按 QuanX 处理
// (QuanX 是主要目标, 且 Surge conf 几乎必带 [Rule]/[Proxy])。
func DetectFormat(raw string) Format {
	hasMitm := false
	for _, ln := range strings.Split(raw, "\n") {
		s := strings.ToLower(strings.TrimSpace(ln))
		if s == "" || strings.HasPrefix(s, "#") || strings.HasPrefix(s, "//") {
			continue
		}
		if !strings.HasPrefix(s, "[") || !strings.HasSuffix(s, "]") {
			continue
		}
		switch s {
		case "[rewrite]", "[filter_local]", "[policy]", "[server_local]", "[host]",
			"[filter_remote]", "[rewrite_remote]", "[task_local]", "[server_remote]":
			// 后四个是 QuanX 独有段(Surge 无), 尤其只有 [filter_remote] 的轻量 conf
			// 必须判 quanx, 否则会被当纯列表把 URL 行误读成 IP-CIDR。
			return FormatQuanX
		case "[rule]", "[proxy]", "[script]", "[map local]", "[header rewrite]", "[url rewrite]":
			return FormatSurge
		case "[mitm]":
			hasMitm = true
		}
	}
	if hasMitm {
		return FormatQuanX
	}
	return FormatList
}

// Parse 按 format 解析订阅原文(format=auto 时探测)。
func Parse(raw string, format Format) *Parsed {
	if format == FormatAuto || format == "" {
		format = DetectFormat(raw)
	}
	switch format {
	case FormatQuanX:
		return parseQuanX(raw)
	case FormatSurge:
		return parseSurge(raw)
	default:
		return parseList(raw)
	}
}

// ---- 通用小工具 ----

func isComment(s string) bool {
	return s == "" || strings.HasPrefix(s, "#") || strings.HasPrefix(s, ";") || strings.HasPrefix(s, "//")
}

// stripQuotes 去掉成对的引号。
func stripQuotes(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// splitCSVLine 逗号切分(去空白, 丢空段)。
func splitCSVLine(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// isRejectPolicy 判断代理策略是否为"屏蔽"类。
func isRejectPolicy(p string) bool {
	return strings.HasPrefix(strings.ToUpper(p), "REJECT")
}

// normalizePolicy 统一策略名(大小写/别名)。
func normalizePolicy(p string) string {
	p = strings.ToUpper(strings.TrimSpace(p))
	switch p {
	case "PROXY", "节点选择", "":
		return "PROXY"
	case "DIRECT":
		return "DIRECT"
	}
	if isRejectPolicy(p) {
		return "REJECT"
	}
	return p
}

var reHostOnly = regexp.MustCompile(`^[a-z0-9.-]+\.[a-z]{2,}$`)

// hostFromLine 从纯列表行提取主机名(支持 hosts 行/URL/AdGuard ||x^ 形态)。
func hostFromLine(s string) (host string, ok bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "||")
	s = strings.TrimSuffix(s, "^")
	if strings.Contains(s, "://") {
		if u, err := url.Parse(s); err == nil && u.Hostname() != "" {
			s = u.Hostname()
		}
	}
	// hosts 形态: 0.0.0.0 example.com
	if f := strings.Fields(s); len(f) == 2 && (f[0] == "0.0.0.0" || f[0] == "127.0.0.1") {
		s = f[1]
	}
	s = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(s), "."))
	if reHostOnly.MatchString(s) {
		return s, true
	}
	return "", false
}

// ipFromLine 提取 IP/CIDR。
func ipFromLine(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if strings.Contains(s, "://") {
		if u, err := url.Parse(s); err == nil && u.Hostname() != "" {
			s = u.Hostname()
		}
	}
	if strings.Contains(s, "/") {
		if _, err := parseCIDROrIP(s); err == nil {
			return s, true
		}
		return "", false
	}
	if isIP(s) {
		return s, true
	}
	return "", false
}

func isIP(s string) bool {
	parts := strings.Split(s, ".")
	if len(parts) != 4 {
		return strings.Count(s, ":") >= 2 // 粗判 IPv6
	}
	for _, p := range parts {
		if p == "" || len(p) > 3 {
			return false
		}
		for _, c := range p {
			if c < '0' || c > '9' {
				return false
			}
		}
	}
	return true
}

func parseCIDROrIP(s string) (string, error) {
	if isIP(strings.SplitN(s, "/", 2)[0]) {
		return s, nil
	}
	return "", fmt.Errorf("not cidr/ip: %s", s)
}

// ---- QuanX ----

func parseQuanX(raw string) *Parsed {
	p := &Parsed{Format: string(FormatQuanX)}
	section := ""
	for i, ln := range strings.Split(raw, "\n") {
		lineNo := i + 1
		s := strings.TrimSpace(ln)
		if s == "" {
			continue
		}
		if strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") {
			section = strings.ToLower(strings.TrimSpace(s[1 : len(s)-1]))
			continue
		}
		if isComment(s) {
			continue
		}
		switch section {
		case "mitm":
			// hostname = a.com, *.b.com(段内也可能混排规则行)
			if k, v, ok := strings.Cut(s, "="); ok && strings.EqualFold(strings.TrimSpace(k), "hostname") {
				for _, part := range splitCSVLine(v) {
					// 一段里可能混着 %APPEND% 占位符和多个主机名(Surge 惯例)
					for _, h := range strings.Fields(part) {
						// 只跳过 %APPEND%/%INSERT% 这类整体占位符; %.example.com 是通配主机名
						if h == "" || (strings.HasPrefix(h, "%") && strings.HasSuffix(h, "%")) {
							continue
						}
						p.MitmHosts = append(p.MitmHosts, h)
					}
				}
			} else if isRuleLine(s) {
				p.parseRuleLine(s, lineNo)
			}
		case "rewrite":
			p.parseRewriteLine(s, lineNo)
		case "filter_remote", "rewrite_remote":
			// QX 主配置引用远程规则/重写: URL, force-policy=..., enabled=...
			// 由 manager 拉取展开(本解析器保持纯函数、不联网)。
			parts := splitCSVLine(s)
			if len(parts) == 0 || !strings.HasPrefix(strings.ToLower(parts[0]), "http") {
				p.skip(s, lineNo, "remote 引用行格式不识别")
				continue
			}
			ref := RemoteRef{URL: parts[0], Kind: "filter", Line: lineNo}
			if section == "rewrite_remote" {
				ref.Kind = "rewrite"
			}
			enabled := true
			for _, kv := range parts[1:] {
				k, v, ok := strings.Cut(kv, "=")
				if !ok {
					continue
				}
				k, v = strings.TrimSpace(k), strings.TrimSpace(v)
				switch strings.ToLower(k) {
				case "force-policy":
					ref.ForcePolicy = v
				case "enabled":
					enabled = !strings.EqualFold(v, "false")
				}
			}
			if enabled {
				p.RemoteRefs = append(p.RemoteRefs, ref)
			} else {
				p.skip(s, lineNo, "remote 引用 enabled=false, 已跳过")
			}
		case "filter_local":
			// <pattern> <script.js|reject>; final/策略行是 QX 兜底策略, 不是脚本
			if strings.HasPrefix(strings.ToLower(s), "final") {
				continue
			}
			f := strings.Fields(s)
			if len(f) >= 2 {
				pattern, rest := f[0], strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), f[0]))
				switch {
				case strings.HasPrefix(rest, "reject"):
					p.Rewrites = append(p.Rewrites, Rewrite{Pattern: pattern, Kind: "reject", Value: rest, Line: lineNo, Raw: s})
				default:
					p.Scripts = append(p.Scripts, Script{Name: scriptName(rest), Kind: "filter", URL: scriptURL(rest), Line: lineNo})
				}
			} else {
				p.skip(s, lineNo, "filter_local 行格式不识别")
			}
		case "rule", "rules", "":
			// QuanX 规则行在段外(无 section 头)或 [rule] 段
			p.parseRuleLine(s, lineNo)
		case "general", "policy", "host", "server_local":
			// key=value 配置段; 但真实 conf 常把规则行混排在段内, 识别得出就按规则解析
			if isRuleLine(s) {
				p.parseRuleLine(s, lineNo)
			}
		default:
			// 其它段: 忽略
		}
	}
	return p
}

// isRuleLine 判断是否是 TYPE,VALUE,... 规则行(用于段内混排识别)。
func isRuleLine(s string) bool {
	i := strings.Index(s, ",")
	if i <= 0 {
		return false
	}
	return isKnownRuleType(strings.ToUpper(strings.TrimSpace(s[:i])))
}

// parseRewriteLine 解析 [rewrite] 段一行。
// 形态: <pattern> url <action> [value] 或 <pattern> <action> [value]
func (p *Parsed) parseRewriteLine(s string, lineNo int) {
	f := strings.Fields(s)
	if len(f) < 2 {
		p.skip(s, lineNo, "rewrite 行格式不识别")
		return
	}
	pattern := f[0]
	rest := strings.TrimSpace(strings.TrimPrefix(s, pattern))
	if strings.EqualFold(rest, "url") || strings.HasPrefix(rest, "url ") {
		rest = strings.TrimSpace(strings.TrimPrefix(rest, "url"))
	}
	if rest == "" {
		p.skip(s, lineNo, "rewrite 行无动作")
		return
	}
	f2 := strings.Fields(rest)
	action := strings.ToLower(f2[0])
	value := strings.TrimSpace(strings.TrimPrefix(rest, f2[0]))
	switch action {
	case "reject", "reject-200", "reject-dict", "reject-array", "reject-img", "reject-video", "reject-json":
		p.Rewrites = append(p.Rewrites, Rewrite{Pattern: pattern, Kind: "reject", Value: action, Line: lineNo, Raw: s})
	case "307", "302", "301":
		p.Rewrites = append(p.Rewrites, Rewrite{Pattern: pattern, Kind: "redirect", Value: action + " " + value, Line: lineNo, Raw: s})
	case "script-response-body", "script-request-body", "script-response-header", "script-request-header", "script-echo-response", "script-analyze-response", "script-request-done", "script-response-done":
		// 同时记 Script(内容)与 Rewrite(带 pattern, 供转换时生成 URL 守卫)
		p.Scripts = append(p.Scripts, Script{Name: scriptName(value), Kind: strings.TrimPrefix(action, "script-"), URL: scriptURL(value), Line: lineNo})
		p.Rewrites = append(p.Rewrites, Rewrite{Pattern: pattern, Kind: action, Value: strings.TrimSpace(value), Line: lineNo, Raw: s})
	case "header", "request-header":
		p.Rewrites = append(p.Rewrites, Rewrite{Pattern: pattern, Kind: "request-header", Value: value, Line: lineNo, Raw: s})
	case "response-header":
		p.Rewrites = append(p.Rewrites, Rewrite{Pattern: pattern, Kind: "response-header", Value: value, Line: lineNo, Raw: s})
	case "request-body", "response-body":
		p.Rewrites = append(p.Rewrites, Rewrite{Pattern: pattern, Kind: action, Value: stripQuotes(value), Line: lineNo, Raw: s})
	case "echo-response":
		p.Rewrites = append(p.Rewrites, Rewrite{Pattern: pattern, Kind: "echo-response", Value: stripQuotes(value), Line: lineNo, Raw: s})
	default:
		// url <目标URL> 整串顶替(目标可能含查询串, 取整段)
		p.Rewrites = append(p.Rewrites, Rewrite{Pattern: pattern, Kind: "url", Value: strings.TrimSpace(rest), Line: lineNo, Raw: s})
	}
}

// parseRuleLine 解析 TYPE,VALUE,POLICY 规则行(QuanX/Surge 通用)。
func (p *Parsed) parseRuleLine(s string, lineNo int) {
	parts := splitCSVLine(s)
	if len(parts) < 2 {
		p.skip(s, lineNo, "规则行字段不足")
		return
	}
	typ := strings.ToUpper(parts[0])
	val := parts[1]
	policy := ""
	if len(parts) >= 3 {
		policy = normalizePolicy(parts[2])
	}
	noResolve := false
	if len(parts) > 3 {
		for _, extra := range parts[3:] {
			if strings.EqualFold(extra, "no-resolve") {
				noResolve = true
			}
		}
	}
	switch typ {
	case "DOMAIN", "DOMAIN-SUFFIX", "DOMAIN-KEYWORD", "IP-CIDR", "IP-CIDR6", "GEOIP", "URL-REGEX", "USER-AGENT":
		p.Rules = append(p.Rules, Rule{Type: typ, Value: val, Policy: policy, NoResolve: noResolve, Line: lineNo, Raw: s})
	case "IP-ASN":
		// QX 规则集常见类型; NetLoom 无 ASN→IP 数据库, 解析记录但不适配(Adapt 里出 warning)
		p.Rules = append(p.Rules, Rule{Type: typ, Value: val, Policy: policy, NoResolve: noResolve, Line: lineNo, Raw: s})
	case "AND", "NOT", "INCLUDE", "FINAL", "DOMAIN-SET", "DEST-PORT", "IN-PORT", "PROCESS-NAME", "IN-USER", "IN-SYSTEM", "SRC-IP", "SRC-IP-CIDR":
		p.skip(s, lineNo, fmt.Sprintf("规则类型 %s 暂不支持(已跳过)", typ))
	default:
		p.skip(s, lineNo, fmt.Sprintf("未知规则类型 %s", typ))
	}
}

func (p *Parsed) skip(raw string, line int, why string) {
	p.Skipped++
	if len(p.Warnings) < 200 {
		p.Warnings = append(p.Warnings, fmt.Sprintf("L%d: %s — %s", line, truncStr(raw, 80), why))
	}
}

func truncStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func scriptName(ref string) string {
	ref = strings.TrimSpace(ref)
	if i := strings.LastIndex(ref, "/"); i >= 0 {
		return ref[i+1:]
	}
	return ref
}

func scriptURL(ref string) string {
	ref = strings.TrimSpace(ref)
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
		return ref
	}
	return ""
}

// ---- Surge ----

func parseSurge(raw string) *Parsed {
	p := &Parsed{Format: string(FormatSurge)}
	section := ""
	for i, ln := range strings.Split(raw, "\n") {
		lineNo := i + 1
		s := strings.TrimSpace(ln)
		if s == "" || isComment(s) {
			continue
		}
		if strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") {
			section = strings.ToLower(strings.TrimSpace(s[1 : len(s)-1]))
			continue
		}
		switch section {
		case "rule":
			p.parseRuleLine(s, lineNo)
		case "mitm":
			if k, v, ok := strings.Cut(s, "="); ok && strings.EqualFold(strings.TrimSpace(k), "hostname") {
				for _, part := range splitCSVLine(v) {
					// 一段里可能混着 %APPEND% 占位符和多个主机名(Surge 惯例)
					for _, h := range strings.Fields(part) {
						// 只跳过 %APPEND%/%INSERT% 这类整体占位符; %.example.com 是通配主机名
						if h == "" || (strings.HasPrefix(h, "%") && strings.HasSuffix(h, "%")) {
							continue
						}
						p.MitmHosts = append(p.MitmHosts, h)
					}
				}
			}
		case "script":
			// name = type=http-response,pattern=^...,script-path=xxx.js,requires-body=true
			if _, v, ok := strings.Cut(s, "="); ok {
				kv := map[string]string{}
				for _, part := range strings.Split(v, ",") {
					if k, val, ok := strings.Cut(part, "="); ok {
						kv[strings.TrimSpace(k)] = strings.TrimSpace(val)
					}
				}
				typ := kv["type"]
				kind := "filter"
				switch typ {
				case "http-response":
					kind = "response-body"
				case "http-request":
					kind = "request-body"
				case "http-response-header":
					kind = "response-header"
				case "http-request-header":
					kind = "request-header"
				}
				sp := kv["script-path"]
				if sp == "" {
					p.skip(s, lineNo, "Script 段缺 script-path")
					continue
				}
				pat := kv["pattern"]
				if pat == "" {
					pat = ".*"
				}
				p.Rewrites = append(p.Rewrites, Rewrite{Pattern: pat, Kind: "script-" + kind, Value: sp, Line: lineNo, Raw: s})
				p.Scripts = append(p.Scripts, Script{Name: scriptName(sp), Kind: kind, URL: scriptURL(sp), Line: lineNo})
			}
		case "map local":
			f := strings.Fields(s)
			if len(f) >= 2 {
				p.skip(s, lineNo, "Surge Map Local 暂不支持(需本地文件)")
			}
		default:
			// Proxy/General/Header Rewrite/URL Rewrite 等: 忽略
		}
	}
	return p
}

// ---- 纯列表 ----

func parseList(raw string) *Parsed {
	p := &Parsed{Format: string(FormatList)}
	for i, ln := range strings.Split(raw, "\n") {
		lineNo := i + 1
		s := strings.TrimSpace(ln)
		if isComment(s) {
			continue
		}
		// TYPE,VALUE,POLICY 规则行(无段头的 QuanX/Surge 规则混在纯列表里)
		if f := strings.Split(s, ","); len(f) >= 2 && isKnownRuleType(strings.ToUpper(strings.TrimSpace(f[0]))) {
			p.parseRuleLine(s, lineNo)
			continue
		}
		if ip, ok := ipFromLine(s); ok {
			p.Rules = append(p.Rules, Rule{Type: "IP-CIDR", Value: ip, Policy: "PROXY", Line: lineNo, Raw: s})
			continue
		}
		if h, ok := hostFromLine(s); ok {
			p.Rules = append(p.Rules, Rule{Type: "DOMAIN-SUFFIX", Value: h, Policy: "PROXY", Line: lineNo, Raw: s})
			continue
		}
		p.skip(s, lineNo, "无法识别为域名或 IP")
	}
	return p
}

func isKnownRuleType(t string) bool {
	switch t {
	case "DOMAIN", "DOMAIN-SUFFIX", "DOMAIN-KEYWORD", "IP-CIDR", "IP-CIDR6", "GEOIP", "URL-REGEX", "USER-AGENT", "IP-ASN":
		return true
	}
	return false
}
