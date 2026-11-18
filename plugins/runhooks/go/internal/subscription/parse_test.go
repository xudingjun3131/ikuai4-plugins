package subscription

import (
	"reflect"
	"testing"
)

const quanxSample = `# 测试订阅
[general]
resource_parser_url = https://example.com/parser.js
geo_location_checker = http://ip-api.com/json/

[mitm]
hostname = %.api.example.com, cdn.example.com, *.wild.example.com

[policy]
static = PROXY, DIRECT, REJECT, PROXY-A
available = PROXY-A: A, PROXY-B: B

[rewrite]
^https?://api\.example\.com/v1/user info url response-body {"vip":true}
^https?://api\.example\.com/v1/user url script-response-body unlock.js
^https?://ads\.example\.com/ url reject
^https?://api\.example\.com/hdr request-header X-Token: abc123
^https?://api\.example\.com/rh response-header X-Frame-Options: SAMEORIGIN
^https?://api\.example\.com/echo url echo-response hello-world
^https?://api\.example\.com/jump url 307 https://other.example.com/
^https?://api\.example\.com/gw url https://gateway.example.com

[filter_local]
^https?://api\.example\.com/filter filter.js
^https?://blocked\.example\.com/ reject

[host]
example.com = 1.2.3.4

[server_local]
; skip
`

const surgeSample = `[General]
loglevel = notify

[Proxy]
PROXY = custom, 1.2.3.4, 443, encrypt-method=chacha20

[Rule]
DOMAIN-SUFFIX,example.com,PROXY
DOMAIN,exact.example.org,DIRECT
IP-CIDR,192.168.0.0/16,DIRECT,no-resolve
GEOIP,CN,DIRECT
FINAL,DIRECT

[MITM]
hostname = %APPEND% %.api.example.com, exact.example.org
skip-server-cert-verify = true

[Script]
unlock = type=http-response,pattern=^https?://api\.example\.com/vip,script-path=https://example.com/unlock.js,requires-body=true

[Map Local]
^https?://img\.example\.com/logo data-type=image file-path=/local/logo.png
`

const listSample = `# 纯域名列表
0.0.0.0 ads.example.com
127.0.0.1 tracker.example.net
plain.example.com
||adguard.example.org^
8.8.8.8
10.0.0.0/8
`

func TestDetectFormat(t *testing.T) {
	cases := []struct {
		raw  string
		want Format
	}{
		{quanxSample, FormatQuanX},
		{surgeSample, FormatSurge},
		{listSample, FormatList},
		{"example.com\n", FormatList},
	}
	for _, c := range cases {
		if got := DetectFormat(c.raw); got != c.want {
			t.Errorf("DetectFormat(%q...) = %v, want %v", c.raw[:20], got, c.want)
		}
	}
}

func TestParseQuanX(t *testing.T) {
	p := Parse(quanxSample, FormatQuanX)
	if p.Format != string(FormatQuanX) {
		t.Fatalf("format = %v", p.Format)
	}
	// [mitm]
	wantHosts := []string{"%.api.example.com", "cdn.example.com", "*.wild.example.com"}
	if !reflect.DeepEqual(p.MitmHosts, wantHosts) {
		t.Errorf("MitmHosts = %v, want %v", p.MitmHosts, wantHosts)
	}
	// rewrite 指令
	kinds := map[string]string{}
	for _, rw := range p.Rewrites {
		kinds[rw.Kind] = rw.Value
	}
	if kinds["reject"] != "reject" {
		t.Errorf("reject rewrite missing: %v", kinds)
	}
	if kinds["request-header"] != "X-Token: abc123" {
		t.Errorf("request-header = %q", kinds["request-header"])
	}
	if kinds["response-header"] != "X-Frame-Options: SAMEORIGIN" {
		t.Errorf("response-header = %q", kinds["response-header"])
	}
	if kinds["echo-response"] != "hello-world" {
		t.Errorf("echo-response = %q", kinds["echo-response"])
	}
	if kinds["redirect"] == "" || kinds["url"] == "" {
		t.Errorf("redirect/url missing: %v", kinds)
	}
	if kinds["script-response-body"] != "unlock.js" {
		t.Errorf("script ref = %q", kinds["script-response-body"])
	}
	// 脚本引用
	if len(p.Scripts) != 2 {
		t.Fatalf("Scripts = %d, want 2 (unlock.js + filter.js)", len(p.Scripts))
	}
	// filter_local reject
	found := false
	for _, rw := range p.Rewrites {
		if rw.Kind == "reject" && rw.Pattern == "^https?://blocked\\.example\\.com/" {
			found = true
		}
	}
	if !found {
		t.Error("filter_local reject 未记录")
	}
	// [host]/[server_local]/[general] 不产生规则
	if len(p.Rules) != 0 {
		t.Errorf("QuanX 样本不应有域名规则(规则行在 [rule] 段): %v", p.Rules)
	}
}

func TestParseQuanXRules(t *testing.T) {
	raw := `[rule]
DOMAIN,exact.com,PROXY
DOMAIN-SUFFIX,suffix.com,REJECT
DOMAIN-KEYWORD,keyword,PROXY
IP-CIDR,1.2.3.0/24,DIRECT,no-resolve
IP-CIDR6,2001:db8::/32,PROXY
GEOIP,CN,DIRECT
URL-REGEX,^https?://api\.,PROXY
USER-AGENT,MyApp*,PROXY
DOMAIN-SET,https://example.com/list.txt,PROXY
IP-ASN,13335,PROXY
FINAL,DIRECT
`
	p := Parse(raw, FormatQuanX)
	if len(p.Rules) != 8 {
		t.Fatalf("Rules = %d, want 8 (支持的类型): %+v", len(p.Rules), p.Rules)
	}
	byType := map[string]Rule{}
	for _, r := range p.Rules {
		byType[r.Type] = r
	}
	if byType["DOMAIN-SUFFIX"].Policy != "REJECT" {
		t.Errorf("REJECT policy = %q", byType["DOMAIN-SUFFIX"].Policy)
	}
	if !byType["IP-CIDR"].NoResolve {
		t.Error("no-resolve 未解析")
	}
	if byType["GEOIP"].Value != "CN" {
		t.Errorf("GEOIP value = %q", byType["GEOIP"].Value)
	}
	// 不支持的类型进 warnings + skipped 计数
	if p.Skipped != 3 { // DOMAIN-SET, IP-ASN, FINAL
		t.Errorf("Skipped = %d, want 3; warnings=%v", p.Skipped, p.Warnings)
	}
}

func TestParseSurge(t *testing.T) {
	p := Parse(surgeSample, FormatSurge)
	if len(p.Rules) != 4 {
		t.Fatalf("Rules = %d, want 4: %+v", len(p.Rules), p.Rules)
	}
	if p.Rules[0].Type != "DOMAIN-SUFFIX" || p.Rules[0].Value != "example.com" {
		t.Errorf("rule0 = %+v", p.Rules[0])
	}
	if !reflect.DeepEqual(p.MitmHosts, []string{"%.api.example.com", "exact.example.org"}) {
		t.Errorf("MitmHosts = %v (%%APPEND%% 应被剥掉)", p.MitmHosts)
	}
	// [Script] → Rewrite(script-*) + Script
	var sc *Script
	for i := range p.Scripts {
		if p.Scripts[i].Name == "unlock.js" {
			sc = &p.Scripts[i]
		}
	}
	if sc == nil || sc.URL != "https://example.com/unlock.js" {
		t.Fatalf("Script = %+v", sc)
	}
	if sc.Kind != "response-body" {
		t.Errorf("Kind = %q, want response-body", sc.Kind)
	}
	// Map Local + FINAL 均跳过
	if p.Skipped != 2 {
		t.Errorf("Skipped = %d, want 2 (Map Local + FINAL)", p.Skipped)
	}
}
func TestParseList(t *testing.T) {
	p := Parse(listSample, FormatList)
	var hosts, ips []string
	for _, r := range p.Rules {
		switch r.Type {
		case "DOMAIN-SUFFIX":
			hosts = append(hosts, r.Value)
		case "IP-CIDR":
			ips = append(ips, r.Value)
		}
	}
	wantHosts := []string{"ads.example.com", "tracker.example.net", "plain.example.com", "adguard.example.org"}
	if !reflect.DeepEqual(hosts, wantHosts) {
		t.Errorf("hosts = %v, want %v", hosts, wantHosts)
	}
	wantIPs := []string{"8.8.8.8", "10.0.0.0/8"}
	if !reflect.DeepEqual(ips, wantIPs) {
		t.Errorf("ips = %v, want %v", ips, wantIPs)
	}
}

func TestHostFromPattern(t *testing.T) {
	cases := []struct{ pat, want string }{
		{`^https?://api\.example\.com/v1/user`, "api.example.com"},
		{`^https?://(www\.)?example\.com/`, "example.com"},
		{`https://exact.example.org/path`, "exact.example.org"},
		{`^https?://.*\.cn/`, ""},
	}
	for _, c := range cases {
		if got := hostFromPattern(c.pat); got != c.want {
			t.Errorf("hostFromPattern(%q) = %q, want %q", c.pat, got, c.want)
		}
	}
}
