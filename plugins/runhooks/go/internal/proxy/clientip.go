package proxy

import (
	"net"
	"net/http"
	"strings"
)

// [real-client-ip] 客户端在上级路由器后面走显式代理时, 上级路由器常做
// SNAT/MASQUERADE, 到达引擎的 TCP 源 IP 变成路由器地址(实锤 10.10.10.1),
// 真实客户端(10.10.10.5)在传输层已丢失。标准转发头是上游把真实 IP 带过来的
// 唯一途径, 这里按 X-Forwarded-For → X-Real-IP → X-Client-IP →
// CF-Connecting-IP → RFC7239 Forwarded 的优先级提取。
//
// 注意: 这些头由客户端直连代理时可伪造, 仅应在可信上游后使用; flow 里始终
// 保留原始请求头, 可追溯。无可用值时返回空串, 调用方回退 req.RemoteAddr。
func clientIPFromRequest(req *http.Request) string {
	if req == nil {
		return ""
	}
	h := req.Header
	// X-Forwarded-For: client, proxy1, proxy2 —— 最左边是原始客户端
	if v := h.Get("X-Forwarded-For"); v != "" {
		for _, part := range strings.Split(v, ",") {
			if ip := parseForwardedIP(part); ip != "" {
				return ip
			}
		}
	}
	for _, k := range []string{"X-Real-IP", "X-Client-IP", "CF-Connecting-IP"} {
		if ip := parseForwardedIP(h.Get(k)); ip != "" {
			return ip
		}
	}
	// RFC 7239: Forwarded: for=1.2.3.4;proto=https, for="[2001:db8::1]:1234"
	if v := h.Get("Forwarded"); v != "" {
		for _, seg := range strings.Split(v, ",") {
			for _, kv := range strings.Split(seg, ";") {
				kv = strings.TrimSpace(kv)
				if len(kv) >= 4 && strings.EqualFold(kv[:4], "for=") {
					if ip := parseForwardedIP(kv[4:]); ip != "" {
						return ip
					}
				}
			}
		}
	}
	return ""
}

func parseForwardedIP(s string) string {
	s = strings.Trim(strings.TrimSpace(s), `"`)
	if s == "" {
		return ""
	}
	if strings.HasPrefix(s, "[") { // IPv6 带方括号, 可能跟端口
		if i := strings.LastIndex(s, "]"); i > 1 {
			s = s[1:i]
		}
	} else if host, _, err := net.SplitHostPort(s); err == nil {
		s = host
	}
	if ip := net.ParseIP(s); ip != nil {
		return ip.String()
	}
	return ""
}

// clientAddrOf 是引擎记录 flow 时唯一的客户端地址入口: 有可信转发头用真实
// IP, 否则回退 TCP 对端。集中在一处, 避免各记录分支(onRequest 短路/WS/错误)
// 口径不一致。
func clientAddrOf(req *http.Request) string {
	if ip := clientIPFromRequest(req); ip != "" {
		return ip
	}
	if req == nil {
		return ""
	}
	return req.RemoteAddr
}
