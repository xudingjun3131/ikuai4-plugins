package rules

import (
	"net/http"
	"strings"
)

// [cors-fix] 1.1.8 真机反馈「使用代理时部分跨域请求报 CORS 错」。根因是内置
// builtin-cors 规则对所有响应无条件 Set 三个通配头:
//
//	Access-Control-Allow-Origin:  *
//	Access-Control-Allow-Methods: *
//	Access-Control-Allow-Headers: *
//
// 在浏览器 CORS 语义下这会造成「部分」请求失败(简单 GET 能过, 带凭证/自定义头/
// 非简单方法的接口挂):
//  1. credentials 模式(include / Cookie / Authorization)下 ACAO 不能是 *, 必须
//     回显具体 Origin 且带 Access-Control-Allow-Credentials: true;
//  2. ACAH / ACAM 的 * 通配对凭证请求同样不被接受, 须回显 ACRH / ACRM;
//  3. 无条件 Set 会覆盖源站本来就正确的具体 ACAO —— 把本来能过的跨域请求弄挂;
//  4. 无 Origin 的同源响应也被注入 CORS 头, 且没有 Vary: Origin, 会污染共享缓存;
//  5. OPTIONS 预检被转发给源站, 源站常回 403/405 或不带 CORS 头 -> 预检失败;
//  6. Chrome 私有网络访问(PNA)预检需 Access-Control-Allow-Private-Network: true。
//
// 修法: 把 builtin-cors 里的 "*" 当作「由本文件按请求动态展开」的标记值, 而不是
// 字面写回头。识别条件是「响应阶段 header 规则且 Set 里 ACAO 值为 *」, 因此:
//  - 用户自定义的「具体值 CORS 规则」走原 applyHeader, 语义完全不变;
//  - 老装机 rules.json 里已固化的旧 builtin-cors(*) 同样被识别, 升级即生效
//    (NewChain 不覆盖已存在的 rules.json, 只改 defaults.go 是治不了老设备的)。
//  - 用户在面板停用 builtin-cors 后, 本逻辑整体不启用, 预检照常转发。

const (
	defaultAllowMethods = "GET, HEAD, POST, PUT, PATCH, DELETE, OPTIONS"
	defaultAllowHeaders = "Authorization, Content-Type, Accept, Origin, X-Requested-With"
)

// isWildcardCORS 判定某条响应阶段 header 规则是不是「通配 CORS」标记规则。
func isWildcardCORS(r *Rule) bool {
	if r == nil || r.Type != TypeHeader || r.Header == nil ||
		!strings.EqualFold(r.Header.Phase, "response") {
		return false
	}
	for k, v := range r.Header.Set {
		if strings.EqualFold(k, "Access-Control-Allow-Origin") && v == "*" {
			return true
		}
	}
	return false
}

// wildcardCORSEnabled 判定本请求是否命中任一启用的通配 CORS 规则。
func (c *Chain) wildcardCORSEnabled(rules []Rule, host, path, method string) bool {
	for i := range rules {
		r := &rules[i]
		if isWildcardCORS(r) && c.matchRule(r, host, path, method) {
			return true
		}
	}
	return false
}

// isPreflight 判定是否为 CORS 预检请求。
func isPreflight(req *http.Request) bool {
	return req.Method == http.MethodOptions &&
		req.Header.Get("Origin") != "" &&
		req.Header.Get("Access-Control-Request-Method") != ""
}

// applyWildcardCORS 按请求把通配标记展开成合法的 CORS 响应头。
// 与 applyHeader 一样先执行 Remove, 但不再把 "*" 写回头。
func applyWildcardCORS(h http.Header, m *HeaderMut, req *http.Request) {
	for _, k := range m.Remove {
		h.Del(k)
	}
	// 规则里除三个 CORS 通配头之外的自定义 Set 项照常生效, 不因改走 CORS
	// 分支而被丢掉(用户可能在 builtin-cors 上叠加别的响应头)。
	for k, v := range m.Set {
		switch {
		case strings.EqualFold(k, "Access-Control-Allow-Origin"),
			strings.EqualFold(k, "Access-Control-Allow-Methods"),
			strings.EqualFold(k, "Access-Control-Allow-Headers"):
			continue
		default:
			h.Set(k, v)
		}
	}
	origin := req.Header.Get("Origin")
	if origin == "" {
		return // 非跨域请求: 不注入任何 CORS 头, 避免污染同源响应与缓存
	}

	// 源站已给出具体(非 *)的 ACAO: 尊重上游策略, 不覆盖。
	if cur := h.Get("Access-Control-Allow-Origin"); cur == "" || cur == "*" {
		h.Set("Access-Control-Allow-Origin", origin)
	}
	h.Set("Access-Control-Allow-Credentials", "true")

	if cur := h.Get("Access-Control-Allow-Methods"); cur == "" || cur == "*" {
		if v := req.Header.Get("Access-Control-Request-Method"); v != "" {
			h.Set("Access-Control-Allow-Methods", v)
		} else {
			h.Set("Access-Control-Allow-Methods", defaultAllowMethods)
		}
	}
	if cur := h.Get("Access-Control-Allow-Headers"); cur == "" || cur == "*" {
		if v := req.Header.Get("Access-Control-Request-Headers"); v != "" {
			h.Set("Access-Control-Allow-Headers", v)
		} else {
			h.Set("Access-Control-Allow-Headers", defaultAllowHeaders)
		}
	}
	if req.Header.Get("Access-Control-Request-Private-Network") == "true" {
		h.Set("Access-Control-Allow-Private-Network", "true")
	}

	addVary(h, "Origin")
	addVary(h, "Access-Control-Request-Method")
	addVary(h, "Access-Control-Request-Headers")
}

// addVary 追加 Vary 令牌且不产生重复值。
func addVary(h http.Header, token string) {
	for _, existing := range h.Values("Vary") {
		for _, part := range strings.Split(existing, ",") {
			if strings.EqualFold(strings.TrimSpace(part), token) {
				return
			}
		}
	}
	h.Add("Vary", token)
}

// preflightResponse 本地应答预检(不转发源站), 避免上游 403/405 导致预检失败。
func preflightResponse(req *http.Request) *http.Response {
	resp := bytesResp(req, http.StatusNoContent, nil)
	applyWildcardCORS(resp.Header, &HeaderMut{Phase: "response",
		Set: map[string]string{"Access-Control-Allow-Origin": "*"}}, req)
	resp.Header.Set("Access-Control-Max-Age", "600")
	return resp
}
