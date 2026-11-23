package subscription

import (
	"fmt"
	"strings"
)

// Adapt 把解析结果适配成引擎配置产物。
// 映射矩阵(与插件页「订阅规则」说明保持一致):
//
//	[mitm]/[MITM] hostname          → scope 域名(白名单, MITM 解密范围)
//	DOMAIN/DOMAIN-SUFFIX(非REJECT)  → scope 域名
//	DOMAIN-KEYWORD(非REJECT)        → scope 域名(*kw* 中间通配)
//	DOMAIN* 策略=REJECT             → ssl_bypass 引擎规则(不解密, 直隧道)
//	IP-CIDR/IP-CIDR6                → scope dst_ips
//	GEOIP                           → scope countries
//	URL-REGEX                       → scope paths(记录层正则; 注意按 path 匹配)
//	USER-AGENT                      → scope user_agents
//	rewrite header/request-header   → header 引擎规则(request 相位)
//	rewrite response-header         → header 引擎规则(response 相位)
//	rewrite request-body/response-body → body_replace 引擎规则(from 空=整包替换)
//	rewrite echo-response           → map_local 引擎规则(内联 body)
//	rewrite reject*                 → host_block 引擎规则(403)
//	rewrite url 30x <target>        → mirror_remote 引擎规则(内容顶替, 非 30x 跳转)
//	rewrite url <target>            → gateway 引擎规则(上游改写)
//	rewrite script-*/Script 段      → 转换后的 JS 脚本(QX→引擎 goja)
func Adapt(p *Parsed) *Adapted {
	a := &Adapted{}
	// [mitm] 主机名
	for _, h := range p.MitmHosts {
		a.Scope.Domains = append(a.Scope.Domains, normalizeHostPattern(h))
	}
	// 域名/IP 类规则
	asnCount := 0
	for _, r := range p.Rules {
		switch r.Type {
		case "DOMAIN":
			if isRejectPolicy(r.Policy) {
				a.Rules = append(a.Rules, AdaptRule{Type: "ssl_bypass", Host: r.Value, Note: "REJECT:" + r.Raw})
			} else {
				a.Scope.Domains = append(a.Scope.Domains, r.Value)
			}
		case "DOMAIN-SUFFIX":
			if isRejectPolicy(r.Policy) {
				a.Rules = append(a.Rules, AdaptRule{Type: "ssl_bypass", Host: "*." + r.Value, Note: "REJECT:" + r.Raw})
			} else {
				a.Scope.Domains = append(a.Scope.Domains, "*."+r.Value)
			}
		case "DOMAIN-KEYWORD":
			if isRejectPolicy(r.Policy) {
				// 通配无法表达 ssl_bypass 的整主机语义, 忽略并提示
				continue
			}
			a.Scope.Domains = append(a.Scope.Domains, "*"+r.Value+"*")
		case "IP-CIDR", "IP-CIDR6":
			a.Scope.DstIPs = append(a.Scope.DstIPs, r.Value)
		case "GEOIP":
			a.Scope.Countries = append(a.Scope.Countries, strings.ToUpper(r.Value))
		case "URL-REGEX":
			if pth := urlRegexToPath(r.Value); pth != "" {
				a.Scope.Paths = append(a.Scope.Paths, pth)
			} else {
				a.Warnings = append(a.Warnings, "URL-REGEX 无法转换为 path 正则, 已跳过: "+truncStr(r.Raw, 60))
			}
		case "USER-AGENT":
			a.Scope.UserAgents = append(a.Scope.UserAgents, r.Value)
		case "IP-ASN":
			asnCount++ // 记录但不适配: 引擎无 ASN→IP 数据库
		}
	}
	if asnCount > 0 {
		a.Warnings = append(a.Warnings, fmt.Sprintf("订阅含 %d 条 IP-ASN 规则: 引擎无 ASN→IP 数据库, 已记录但未生效", asnCount))
	}
	// [scope-conflict] 引擎的域名/IP/国家三个维度是 AND 关系(任一不满足即绕过)。
	// 订阅若同时含域名规则与 IP-CIDR/GEOIP, IP/国家白名单会把域名流量全部挡掉——
	// 此时只保留域名维度, IP/国家规则仅留在解析详情里(适配说明中明示)。
	if len(a.Scope.Domains) > 0 {
		if len(a.Scope.DstIPs) > 0 {
			a.Warnings = append(a.Warnings, "订阅同时含域名与 IP-CIDR 规则: IP 段未应用于解密范围(避免与域名白名单互相冲突), 详见解析详情")
			a.Scope.DstIPs = nil
		}
		if len(a.Scope.Countries) > 0 {
			a.Warnings = append(a.Warnings, "订阅同时含域名与 GEOIP 规则: 国家维度未应用于解密范围(避免与域名白名单互相冲突), 详见解析详情")
			a.Scope.Countries = nil
		}
	}
	// 改写指令
	for _, rw := range p.Rewrites {
		host := hostFromPattern(rw.Pattern)
		switch rw.Kind {
		case "reject":
			if host != "" {
				a.Rules = append(a.Rules, AdaptRule{Type: "host_block", Host: host, Note: "reject:" + rw.Raw})
			}
		case "request-header", "header":
			k, v := splitHeader(rw.Value)
			if k == "" || host == "" {
				continue
			}
			a.Rules = append(a.Rules, AdaptRule{Type: "header", Host: host, Path: pathFromPattern(rw.Pattern), Header: &struct {
				Phase  string
				Set    map[string]string
				Remove []string
			}{Phase: "request", Set: map[string]string{k: v}}, Note: "req-header:" + rw.Raw})
		case "response-header":
			k, v := splitHeader(rw.Value)
			if k == "" || host == "" {
				continue
			}
			a.Rules = append(a.Rules, AdaptRule{Type: "header", Host: host, Path: pathFromPattern(rw.Pattern), Header: &struct {
				Phase  string
				Set    map[string]string
				Remove []string
			}{Phase: "response", Set: map[string]string{k: v}}, Note: "resp-header:" + rw.Raw})
		case "request-body", "response-body":
			phase := "response"
			if rw.Kind == "request-body" {
				phase = "request"
			}
			a.Rules = append(a.Rules, AdaptRule{Type: "body_replace", Host: host, Path: pathFromPattern(rw.Pattern), Body: &struct {
				Phase string
				From  string
				To    string
			}{Phase: phase, From: "", To: rw.Value}, Note: rw.Kind + ":" + rw.Raw})
		case "echo-response":
			a.Rules = append(a.Rules, AdaptRule{Type: "map_local", Host: host, Path: pathFromPattern(rw.Pattern), MapLocal: &struct {
				Status int
				File   string
				Body   string
			}{Status: 200, Body: rw.Value}, Note: "echo:" + rw.Raw})
		case "redirect":
			// 30x <target>: 引擎无 30x 跳转规则, 用 mirror_remote 顶替内容(语义降级, 记 note)
			target := strings.TrimSpace(strings.TrimPrefix(rw.Value, "307"))
			target = strings.TrimSpace(strings.TrimPrefix(target, "302"))
			target = strings.TrimSpace(strings.TrimPrefix(target, "301"))
			if target != "" && host != "" {
				a.Rules = append(a.Rules, AdaptRule{Type: "mirror_remote", Host: host, Path: pathFromPattern(rw.Pattern), Mirror: &struct {
					URL string
				}{URL: target}, Note: "redirect→mirror:" + rw.Raw})
			}
		case "url":
			// url <target>: 整串顶替 → gateway(上游改写, 语义降级见 note)
			if rw.Value != "" && host != "" {
				a.Rules = append(a.Rules, AdaptRule{Type: "gateway", Host: host, Path: pathFromPattern(rw.Pattern), Gateway: &struct {
					Upstream string
				}{Upstream: rw.Value}, Note: "url→gateway:" + rw.Raw})
			}
		case "script-response-body", "script-request-body", "script-response-header", "script-request-header", "script-echo-response", "script-analyze-response", "script-request-done", "script-response-done":
			// 脚本类: 由 manager 统一转换(见 adaptScripts)
		}
	}
	a.dedup()
	return a
}

// adaptScripts 转换订阅脚本(需要联网抓取的脚本内容已在 manager 阶段尝试抓取)。
// subID 用于生成文件名。rewrite 指令带的 URL 匹配式转成入口守卫;
// [filter_local] 脚本无匹配式, 不加强守卫(脚本自身通常有 URL 判断)。
func adaptScripts(p *Parsed, subID string) ([]AdaptScript, []string) {
	var out []AdaptScript
	var warns []string
	byName := make(map[string]*Script, len(p.Scripts))
	for i := range p.Scripts {
		byName[p.Scripts[i].Name] = &p.Scripts[i]
	}
	used := make(map[string]bool)
	n := 0
	for _, rw := range p.Rewrites {
		if !strings.HasPrefix(rw.Kind, "script-") {
			continue
		}
		kind := strings.TrimPrefix(rw.Kind, "script-")
		sc := byName[scriptName(rw.Value)]
		if sc == nil || sc.Source == "" {
			warns = append(warns, fmt.Sprintf("脚本 %s 未取到内容, 已跳过", rw.Value))
			continue
		}
		used[sc.Name] = true
		js, w := ConvertQXScript(sc.Source, kind, rw.Pattern)
		warns = append(warns, w...)
		n++
		out = append(out, AdaptScript{
			FileName: fmt.Sprintf("sub-%s-%d.js", subID, n),
			Source:   js,
			Note:     fmt.Sprintf("订阅脚本 %s(%s)", sc.Name, kind),
		})
	}
	// [filter_local] 等无匹配式脚本
	for i := range p.Scripts {
		sc := &p.Scripts[i]
		if used[sc.Name] {
			continue
		}
		used[sc.Name] = true
		if sc.Source == "" {
			warns = append(warns, fmt.Sprintf("脚本 %s 未取到内容, 已跳过", sc.Name))
			continue
		}
		js, w := ConvertQXScript(sc.Source, sc.Kind, "")
		warns = append(warns, w...)
		n++
		out = append(out, AdaptScript{
			FileName: fmt.Sprintf("sub-%s-%d.js", subID, n),
			Source:   js,
			Note:     fmt.Sprintf("订阅脚本 %s(%s, 无匹配式)", sc.Name, sc.Kind),
		})
	}
	return out, warns
}

// normalizeHostPattern 规范化主机名通配: QuanX 的 %.x.com(含顶级域本身) → *.x.com
// (引擎 hostMatch 的 *. 语义同样匹配顶级域本身)。
func normalizeHostPattern(h string) string {
	h = strings.ToLower(strings.TrimSpace(h))
	if strings.HasPrefix(h, "%.") {
		return "*." + h[2:]
	}
	return h
}

// urlRegexToPath 把 URL 级正则转成引擎的 path 级正则:
// ^https?://api\.example\.com/v1/user → ^/v1/user
// 纯主机正则(无 path 段)返回 ""(无法表达, 调用方记 warning)。
func urlRegexToPath(re string) string {
	s := strings.TrimPrefix(re, "^")
	for _, pre := range []string{"https?://", "http://", "https://", "wss?://"} {
		if strings.HasPrefix(s, pre) {
			s = s[len(pre):]
			break
		}
	}
	if strings.HasPrefix(s, "://") {
		return ""
	}
	i := strings.Index(s, "/")
	if i < 0 {
		return "" // 纯主机匹配, 无 path 段
	}
	return "^" + s[i:]
}

// hostFromPattern 从 rewrite/filter 的 URL 匹配式里猜主机名(猜不出返回 "")。
// ^https?://(www\.)?example\.com/path → example.com
// 处理正则转义(\. 是字面量)与可选分组((www\.)? 整组跳过)。
func hostFromPattern(pattern string) string {
	p := strings.TrimPrefix(pattern, "^")
	for _, pre := range []string{"https?://", "http://", "https://", "wss?://"} {
		if strings.HasPrefix(p, pre) {
			p = p[len(pre):]
			break
		}
	}
	var b strings.Builder
	i := 0
	for i < len(p) {
		c := p[i]
		if c == '\\' && i+1 < len(p) {
			// 转义序列: \. \/ \- 等按字面量收录
			b.WriteByte(p[i+1])
			i += 2
			continue
		}
		switch c {
		case '(':
			if b.Len() == 0 {
				i = skipGroup(p, i) // 主机未开始: 跳过整个分组(含尾随量词)
				continue
			}
			return finishHost(b.String())
		case '*':
			if b.Len() == 0 {
				i++ // 前缀通配(.*), 继续扫
				continue
			}
			return finishHost(b.String())
		case '/', ')', '[', ']', '{', '}', '?', '+', '|', '^', '$':
			if b.Len() > 0 {
				return finishHost(b.String())
			}
			i++
		default:
			b.WriteByte(c)
			i++
		}
	}
	return finishHost(b.String())
}

func finishHost(s string) string {
	s = strings.ToLower(s)
	if reHostOnly.MatchString(s) {
		return s
	}
	return ""
}

// skipGroup 从 p[i]=='(' 跳到配对 ')' 之后(含尾随量词 ?/+/*)。
func skipGroup(p string, i int) int {
	depth := 0
	for i < len(p) {
		c := p[i]
		if c == '\\' && i+1 < len(p) {
			i += 2
			continue
		}
		switch c {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				i++
				for i < len(p) && (p[i] == '?' || p[i] == '+' || p[i] == '*') {
					i++
				}
				return i
			}
		}
		i++
	}
	return i
}

// pathFromPattern 从匹配式里提取 path 段作为引擎 path 正则(取主机名之后到第一个
// 正则边界为止; 含转义的段原样保留)。
func pathFromPattern(pattern string) string {
	host := hostFromPattern(pattern)
	if host == "" {
		return ""
	}
	// 在原始 pattern 里定位主机结束处(跳过转义序列地扫描)
	p := strings.TrimPrefix(pattern, "^")
	for _, pre := range []string{"https?://", "http://", "https://", "wss?://"} {
		if strings.HasPrefix(p, pre) {
			p = p[len(pre):]
			break
		}
	}
	i := 0
	for i < len(p) {
		c := p[i]
		if c == '\\' && i+1 < len(p) {
			i += 2
			continue
		}
		if c == '/' {
			break
		}
		if strings.ContainsRune("()[]{}?*+|^$", rune(c)) {
			return "" // 主机段里还有正则结构, 无法可靠切 path
		}
		i++
	}
	if i >= len(p) || p[i] != '/' {
		return ""
	}
	return p[i:]
}

// splitHeader 解析 "Key: Value" / "Key=Value"。
func splitHeader(v string) (string, string) {
	v = strings.TrimSpace(v)
	if k, val, ok := strings.Cut(v, ":"); ok {
		return strings.TrimSpace(k), strings.TrimSpace(val)
	}
	if k, val, ok := strings.Cut(v, "="); ok {
		return strings.TrimSpace(k), strings.TrimSpace(val)
	}
	return "", ""
}

func (a *Adapted) dedup() {
	seen := make(map[string]struct{})
	domains := a.Scope.Domains[:0]
	for _, d := range a.Scope.Domains {
		if _, ok := seen["d:"+d]; ok {
			continue
		}
		seen["d:"+d] = struct{}{}
		domains = append(domains, d)
	}
	a.Scope.Domains = domains
	seen2 := make(map[string]struct{})
	ips := a.Scope.DstIPs[:0]
	for _, d := range a.Scope.DstIPs {
		if _, ok := seen2[d]; ok {
			continue
		}
		seen2[d] = struct{}{}
		ips = append(ips, d)
	}
	a.Scope.DstIPs = ips
}
