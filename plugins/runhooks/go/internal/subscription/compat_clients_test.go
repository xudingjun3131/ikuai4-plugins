package subscription

import (
	"strings"
	"testing"
)

// [compat-clients] 5 类客户端资源索引页: HTML 页面提取资源链接, 不把 HTML 当规则。
func TestParseIndexPage(t *testing.T) {
	html := `<!DOCTYPE html><html><head></head><body>
<a href="/config/qx.conf">QX</a>
<a href="https://yfamilys.com/rewrite/4in1.conf">4in1</a>
<a href="/module/baiducloud.sgmodule">sgmodule</a>
<a href="/plugin/4in1.plugin">plugin</a>
<a href="/stoverride/car.stoverride">override</a>
<a href="/config/stash-auto.yaml">yaml</a>
<a href="/rule/AntiAD.list">list</a>
<a href="/rewrite/yfamily.json">json</a>
<script src="https://pagead2.googlesyndication.com/pagead/js/adsbygoogle.js"></script>
<a href="/airport">机场</a>
</body></html>`
	p := Parse(html, FormatAuto)
	if p.Format != string(FormatIndex) {
		t.Fatalf("format=%s want index", p.Format)
	}
	if len(p.RemoteRefs) != 8 {
		t.Fatalf("RemoteRefs=%d want 8: %+v", len(p.RemoteRefs), p.RemoteRefs)
	}
	for _, r := range p.RemoteRefs {
		if strings.Contains(r.URL, "adsbygoogle") || strings.Contains(r.URL, "airport") {
			t.Fatalf("不该收广告/导航链接: %+v", r)
		}
		if r.Kind != "index" {
			t.Fatalf("kind=%s", r.Kind)
		}
	}
	if len(p.Rules) != 0 || len(p.Rewrites) != 0 {
		t.Fatalf("索引页不应解析出规则/改写: %+v %+v", p.Rules, p.Rewrites)
	}
}

// [compat-clients] Stash .stoverride / .yaml: script-providers 转脚本引用, tiles 忽略。
func TestParseStOverride(t *testing.T) {
	raw := `name: demo
desc: 面板
tiles:
  - name: abc
    icon: 'car.fill'
script-providers:
  demo:
    url: https://raw.githubusercontent.com/x/y/master/car.js
rules:
  - DOMAIN-SUFFIX,kbb.com,PROXY
  - IP-ASN,13335,no-resolve
`
	p := Parse(raw, FormatAuto)
	if p.Format != string(FormatStOverride) {
		t.Fatalf("format=%s want stoverride", p.Format)
	}
	if len(p.Scripts) != 1 || p.Scripts[0].URL == "" {
		t.Fatalf("Scripts=%+v", p.Scripts)
	}
	if len(p.Rewrites) != 1 || p.Rewrites[0].Kind != "script-response-body" {
		t.Fatalf("Rewrites=%+v", p.Rewrites)
	}
	if len(p.Rules) != 2 {
		t.Fatalf("Rules=%d want 2: %+v", len(p.Rules), p.Rules)
	}
	found := false
	for _, w := range p.Warnings {
		if strings.Contains(w, "tiles") {
			found = true
		}
	}
	if !found {
		t.Fatalf("缺 tiles 忽略说明: %v", p.Warnings)
	}
}

// [compat-clients] Surge .sgmodule / Loon .plugin 的 [URL Rewrite] 段。
func TestParseClientRewriteSections(t *testing.T) {
	cases := []string{
		`#!name=4in1
[URL Rewrite]
^https?://www\.google\.com/search\?q=x.*$ 302 https://yfamilys.com/shadowrocket`,
		`#!name=baidu
[MITM]
hostname = %APPEND% pan.baidu.com
[Script]
bd = type=http-response,pattern=^https?://pan\.baidu\.com/,script-path=https://x/bd.js,requires-body=true`,
	}
	for _, raw := range cases {
		p := Parse(raw, FormatAuto)
		if len(p.Rewrites) == 0 && len(p.MitmHosts) == 0 {
			t.Fatalf("未解析出改写/MITM: format=%s %+v", p.Format, p)
		}
	}
	// Loon .plugin 的 302 行要落成 redirect
	p := Parse(cases[0], FormatAuto)
	if len(p.Rewrites) != 1 || p.Rewrites[0].Kind != "redirect" {
		t.Fatalf("Loon 302 行未转 redirect: %+v", p.Rewrites)
	}
}

// [compat-clients] 5 类页面判 index; 各资源文件不误判成纯列表。
func TestDetectFormatClientResources(t *testing.T) {
	cases := map[string]Format{
		"<!DOCTYPE html><html><a href='/config/qx.conf'>x</a></html>":    FormatIndex,
		"name: demo\nscript-providers:\n  d:\n    url: https://x/y.js\n": FormatStOverride,
		"[filter_remote]\nhttps://x/a.list, enabled=true\n":              FormatQuanX,
		"[General]\nskip-proxy = 1\n[Rule]\nDOMAIN,a.com,PROXY\n":        FormatSurge,
		"DOMAIN,a.com\nDOMAIN-SUFFIX,b.com\n":                            FormatList,
	}
	for raw, want := range cases {
		if got := DetectFormat(raw); got != want {
			t.Errorf("DetectFormat(%q)=%s want %s", raw[:minIdx(len(raw), 30)], got, want)
		}
	}
}
