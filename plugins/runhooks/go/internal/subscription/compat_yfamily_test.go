package subscription

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// [compat-yfamily] @url: 引用前缀(用户从 QX/聚合器复制时常带)
func TestNormalizeSubURL(t *testing.T) {
	cases := map[string]string{
		"@url:https://yfamilys.com/config/qx.conf": "https://yfamilys.com/config/qx.conf",
		"  @url:https://yfamilys.com/rule  ":       "https://yfamilys.com/rule",
		"https://yfamilys.com/config/qx-lite.conf": "https://yfamilys.com/config/qx-lite.conf",
	}
	for in, want := range cases {
		if got := normalizeSubURL(in); got != want {
			t.Fatalf("normalizeSubURL(%q)=%q want %q", in, got, want)
		}
	}
}

const qxSample = `[general]
excluded_routes=192.168.0.0/16
[policy]
static=🚀 策略选择, DIRECT
[server_remote]
订阅链接, tag=机场名, enabled=true
[filter_remote]
https://yfamilys.com/rule/AntiAD.list, force-policy=REJECT, update-interval=172800, enabled=true
https://yfamilys.com/rule/ai.list, force-policy=🤖 AI, enabled=true
https://yfamilys.com/rule/off.list, enabled=false
[filter_local]
final, 🚀 策略选择
[rewrite_remote]
https://yfamilys.com/rewrite/4in1.conf, enabled=true
[mitm]
hostname = a.com, *.b.com
[task_local]
event-interaction https://x/y.js, tag=解锁, enabled=true
`

// [compat-yfamily] QX 主配置: filter_remote/rewrite_remote 收集引用, final 不当脚本,
// server_remote/task_local/policy 不误解析成规则。
func TestParseQuanXRemoteRefs(t *testing.T) {
	p := Parse(qxSample, FormatAuto)
	if p.Format != string(FormatQuanX) {
		t.Fatalf("format=%s want quanx", p.Format)
	}
	if len(p.RemoteRefs) != 3 {
		t.Fatalf("RemoteRefs=%d want 3: %+v", len(p.RemoteRefs), p.RemoteRefs)
	}
	if p.RemoteRefs[0].Kind != "filter" || p.RemoteRefs[0].ForcePolicy != "REJECT" {
		t.Fatalf("ref0=%+v", p.RemoteRefs[0])
	}
	if p.RemoteRefs[2].Kind != "rewrite" {
		t.Fatalf("ref2=%+v", p.RemoteRefs[2])
	}
	// enabled=false 的 off.list 不应出现
	for _, r := range p.RemoteRefs {
		if strings.Contains(r.URL, "off.list") {
			t.Fatalf("enabled=false 引用未被跳过: %+v", r)
		}
	}
	if len(p.Scripts) != 0 {
		t.Fatalf("final 行被误当脚本: %+v", p.Scripts)
	}
	if len(p.Rules) != 0 {
		t.Fatalf("policy/task 行被误当规则: %+v", p.Rules)
	}
	if len(p.MitmHosts) != 2 {
		t.Fatalf("mitm hosts=%v", p.MitmHosts)
	}
}

// [compat-yfamily] IP-ASN 解析记录但不适配(引擎无 ASN 数据库), 且明确告警。
func TestIPASNRecordedWithWarning(t *testing.T) {
	raw := "DOMAIN-SUFFIX,a.com\nIP-ASN,1012,no-resolve\nIP-ASN,14651,no-resolve\n"
	p := Parse(raw, FormatList)
	if len(p.Rules) != 3 {
		t.Fatalf("rules=%d want 3", len(p.Rules))
	}
	if p.Skipped != 0 {
		t.Fatalf("skipped=%d want 0", p.Skipped)
	}
	a := Adapt(p)
	if len(a.Scope.Domains) != 1 {
		t.Fatalf("scope domains=%v", a.Scope.Domains)
	}
	found := false
	for _, w := range a.Warnings {
		if strings.Contains(w, "2 条 IP-ASN") && strings.Contains(w, "未生效") {
			found = true
		}
	}
	if !found {
		t.Fatalf("缺 IP-ASN 告警: %v", a.Warnings)
	}
}

// [compat-yfamily] 16MB 上限是明确拒绝, 不是静默截断。
func TestCheckSubSize(t *testing.T) {
	if err := checkSubSize(9 * 1024 * 1024); err != nil { // AntiAD.list 约 9.7MB, 应放行
		t.Fatalf("9MB 被拒: %v", err)
	}
	if err := checkSubSize(maxSubBytes + 1); err == nil {
		t.Fatal("超限未报错")
	}
	if err := checkSubSize(-1); err != nil { // chunked 无 Content-Length
		t.Fatalf("-1 被拒: %v", err)
	}
}

// [compat-yfamily] manager 展开主配置的远程引用, 合并规则 + REJECT 标记 + 去重防循环。
func TestExpandRemoteRefs(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rules.list":
			fmt.Fprint(w, "DOMAIN,a.com\nDOMAIN-SUFFIX,b.com\nIP-ASN,1012,no-resolve\n")
		case "/self.conf":
			// 自引用(循环), 应被 seen 去重
			fmt.Fprintf(w, "[filter_remote]\n%s/rules.list, enabled=true\n", srv.URL)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	conf := fmt.Sprintf("[filter_remote]\n%s/rules.list, force-policy=REJECT, enabled=true\n%s/rules.list, enabled=true\n%s/self.conf, enabled=true\n", srv.URL, srv.URL, srv.URL)
	p := Parse(conf, FormatQuanX)
	m := &Manager{client: &http.Client{Timeout: 5 * time.Second}}
	m.expandRemoteRefs(p, srv.URL+"/main.conf")

	if len(p.Rules) != 3 { // 同一 URL 去重, 只展开一次
		t.Fatalf("rules=%d want 3: %+v", len(p.Rules), p.Rules)
	}
	if p.Rules[0].Policy != "REJECT" {
		t.Fatalf("force-policy=REJECT 未标记: %+v", p.Rules[0])
	}
	a := Adapt(p)
	// IP-ASN 不适配, 只剩 2 条域名规则转成 ssl_bypass
	if len(a.Rules) != 2 || a.Rules[0].Type != "ssl_bypass" {
		t.Fatalf("REJECT 规则未转成 ssl_bypass: %+v", a.Rules)
	}
	if !strings.Contains(strings.Join(p.Warnings, "|"), "展开") {
		t.Fatalf("缺展开说明: %v", p.Warnings)
	}
}
