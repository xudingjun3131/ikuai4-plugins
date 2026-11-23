package subscription

import (
	"reflect"
	"testing"
)

func TestAdaptDomains(t *testing.T) {
	p := &Parsed{
		MitmHosts: []string{"%.api.example.com"},
		Rules: []Rule{
			{Type: "DOMAIN", Value: "exact.com", Policy: "PROXY"},
			{Type: "DOMAIN-SUFFIX", Value: "suffix.com", Policy: "DIRECT"},
			{Type: "DOMAIN-SUFFIX", Value: "blocked.com", Policy: "REJECT"},
			{Type: "DOMAIN", Value: "blocked2.com", Policy: "REJECT"},
			{Type: "DOMAIN-KEYWORD", Value: "kw", Policy: "PROXY"},
			{Type: "IP-CIDR", Value: "1.2.3.0/24", Policy: "PROXY"},
			{Type: "GEOIP", Value: "cn", Policy: "DIRECT"},
			{Type: "URL-REGEX", Value: `^https?://api\.example\.com/v1/user`, Policy: "PROXY"},
			{Type: "USER-AGENT", Value: "MyApp", Policy: "PROXY"},
		},
	}
	a := Adapt(p)
	// %. 前缀规范化为 *.
	wantDomains := []string{"*.api.example.com", "exact.com", "*.suffix.com", "*kw*"}
	if !reflect.DeepEqual(a.Scope.Domains, wantDomains) {
		t.Errorf("Domains = %v, want %v", a.Scope.Domains, wantDomains)
	}
	// 域名与 IP/国家冲突: IP/国家维度丢弃并记 warning
	if len(a.Scope.DstIPs) != 0 || len(a.Scope.Countries) != 0 {
		t.Errorf("域名订阅不应应用 IP/国家维度: %+v", a.Scope)
	}
	if len(a.Warnings) != 2 {
		t.Errorf("Warnings = %v, want 2 (IP/GEOIP 冲突)", a.Warnings)
	}
	// URL-REGEX → path 正则
	if !reflect.DeepEqual(a.Scope.Paths, []string{`^/v1/user`}) {
		t.Errorf("Paths = %v", a.Scope.Paths)
	}
	if !reflect.DeepEqual(a.Scope.UserAgents, []string{"MyApp"}) {
		t.Errorf("UserAgents = %v", a.Scope.UserAgents)
	}
	// REJECT → ssl_bypass
	var bypass []string
	for _, r := range a.Rules {
		if r.Type == "ssl_bypass" {
			bypass = append(bypass, r.Host)
		}
	}
	wantBypass := []string{"*.blocked.com", "blocked2.com"}
	if !reflect.DeepEqual(bypass, wantBypass) {
		t.Errorf("ssl_bypass hosts = %v, want %v", bypass, wantBypass)
	}
}

// 纯 IP 订阅(无域名规则): IP 段白名单生效。
func TestAdaptIPOnly(t *testing.T) {
	p := &Parsed{Rules: []Rule{
		{Type: "IP-CIDR", Value: "1.2.3.0/24", Policy: "PROXY"},
		{Type: "GEOIP", Value: "cn", Policy: "DIRECT"},
	}}
	a := Adapt(p)
	if !reflect.DeepEqual(a.Scope.DstIPs, []string{"1.2.3.0/24"}) {
		t.Errorf("DstIPs = %v", a.Scope.DstIPs)
	}
	if !reflect.DeepEqual(a.Scope.Countries, []string{"CN"}) {
		t.Errorf("Countries = %v", a.Scope.Countries)
	}
	if len(a.Warnings) != 0 {
		t.Errorf("Warnings = %v", a.Warnings)
	}
}

func TestURLRegexToPath(t *testing.T) {
	cases := []struct{ in, want string }{
		{`^https?://api\.example\.com/v1/user`, `^/v1/user`},
		{`https://exact.example.org/a/b`, `^/a/b`},
		{`^https?://api\.example\.com`, ""}, // 纯主机, 无 path
		{`^/api/`, `^/api/`},                // 已是 path 级
	}
	for _, c := range cases {
		if got := urlRegexToPath(c.in); got != c.want {
			t.Errorf("urlRegexToPath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestAdaptRewrites(t *testing.T) {
	p := &Parsed{
		Rewrites: []Rewrite{
			{Pattern: `^https?://api\.example\.com/hdr`, Kind: "request-header", Value: "X-Token: abc"},
			{Pattern: `^https?://api\.example\.com/rh`, Kind: "response-header", Value: "X-Frame-Options: SAMEORIGIN"},
			{Pattern: `^https?://api\.example\.com/rb`, Kind: "request-body", Value: `{"a":1}`},
			{Pattern: `^https?://api\.example\.com/sb`, Kind: "response-body", Value: `{"vip":true}`},
			{Pattern: `^https?://api\.example\.com/echo`, Kind: "echo-response", Value: "hello"},
			{Pattern: `^https?://ads\.example\.com/`, Kind: "reject", Value: "reject"},
			{Pattern: `^https?://api\.example\.com/jump`, Kind: "redirect", Value: "307 https://other.example.com/"},
			{Pattern: `^https?://api\.example\.com/gw`, Kind: "url", Value: "https://gateway.example.com"},
		},
	}
	a := Adapt(p)
	byType := map[string]AdaptRule{}
	for _, r := range a.Rules {
		byType[r.Type] = r
	}
	if _, ok := byType["header"]; !ok {
		t.Fatalf("header rule 缺失: %+v", a.Rules)
	}
	// 逐条验(不用 map, header 有两条)
	var reqHdr, respHdr, reqBody, respBody, echo, block, mirror, gw AdaptRule
	n := 0
	for _, r := range a.Rules {
		switch {
		case r.Type == "header" && r.Header.Phase == "request":
			reqHdr = r
		case r.Type == "header" && r.Header.Phase == "response":
			respHdr = r
		case r.Type == "body_replace" && r.Body.Phase == "request":
			reqBody = r
		case r.Type == "body_replace" && r.Body.Phase == "response":
			respBody = r
		case r.Type == "map_local":
			echo = r
		case r.Type == "host_block":
			block = r
		case r.Type == "mirror_remote":
			mirror = r
		case r.Type == "gateway":
			gw = r
		}
		n++
	}
	if n != 8 {
		t.Fatalf("adapted rules = %d, want 8", n)
	}
	if reqHdr.Header.Set["X-Token"] != "abc" || reqHdr.Host != "api.example.com" {
		t.Errorf("reqHdr = %+v", reqHdr)
	}
	if respHdr.Header.Set["X-Frame-Options"] != "SAMEORIGIN" {
		t.Errorf("respHdr = %+v", respHdr)
	}
	if reqBody.Body.From != "" || reqBody.Body.To != `{"a":1}` {
		t.Errorf("reqBody = %+v (From 必须为空=整包替换)", reqBody.Body)
	}
	if respBody.Body.To != `{"vip":true}` {
		t.Errorf("respBody = %+v", respBody.Body)
	}
	if echo.MapLocal.Body != "hello" || echo.MapLocal.Status != 200 {
		t.Errorf("echo = %+v", echo.MapLocal)
	}
	if block.Host != "ads.example.com" {
		t.Errorf("block = %+v", block)
	}
	if mirror.Mirror.URL != "https://other.example.com/" {
		t.Errorf("mirror = %+v", mirror.Mirror)
	}
	if gw.Gateway.Upstream != "https://gateway.example.com" {
		t.Errorf("gw = %+v", gw.Gateway)
	}
}

func TestAdaptScripts(t *testing.T) {
	p := &Parsed{
		Rewrites: []Rewrite{
			{Pattern: `^https?://api\.example\.com/vip`, Kind: "script-response-body", Value: "unlock.js"},
		},
		Scripts: []Script{
			{Name: "unlock.js", Kind: "response-body", Source: `let obj = JSON.parse($response.body); obj.vip = true; $done({body: JSON.stringify(obj)});`},
			{Name: "missing.js", Kind: "response-body", FetchErr: "404"},
		},
	}
	sc, warns := adaptScripts(p, "sub-1")
	if len(sc) != 1 {
		t.Fatalf("scripts = %d, want 1; warns=%v", len(sc), warns)
	}
	if sc[0].FileName != "sub-sub-1-1.js" {
		t.Errorf("FileName = %q", sc[0].FileName)
	}
	js := sc[0].Source
	for _, want := range []string{"function onResponse(flow)", "flow.response.body", "new RegExp(", "return;"} {
		if !contains(js, want) {
			t.Errorf("转换产物缺 %q:\n%s", want, js)
		}
	}
	if contains(js, "$done") || contains(js, "$response") {
		t.Errorf("转换产物仍含 QX 原语:\n%s", js)
	}
	if len(warns) != 1 {
		t.Errorf("warns = %v, want 1 (missing.js 未取到)", warns)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
