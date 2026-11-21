package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ikuai4/netfence/internal/core"
)

func newTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	eng, err := core.NewEngine(core.Config{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	// Server.cfgDir 与 Engine.Dir 指向同一目录, 写端点落盘后 Reload 才能立刻读到。
	return New(eng, "1.1.0", dir), dir
}

func Test_API_health(t *testing.T) {
	s, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if rec.Code != 200 {
		t.Fatalf("status=%d", rec.Code)
	}
	var m map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &m)
	if m["version"] != "1.1.0" || m["ok"] != true {
		t.Errorf("health=%v", m)
	}
	if m["schedule_tz"] != "Asia/Shanghai(+08:00)" {
		t.Errorf("health 未声明时区: %v", m["schedule_tz"])
	}
}

func Test_API_decide_reflectsPolicy(t *testing.T) {
	s, _ := newTestServer(t)
	body := `[{"id":"r1","name":"block video","enabled":true,"priority":1,` +
		`"match":{"categories":["video"]},"action":"block"}]`
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/policy", strings.NewReader(body)))
	if rec.Code != 200 {
		t.Fatalf("put policy status=%d body=%s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	probe := `{"client_ip":"192.168.1.5","domain":"x.douyin.com","app_hint":""}`
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/decide", strings.NewReader(probe)))
	var d core.Decision
	_ = json.Unmarshal(rec.Body.Bytes(), &d)
	if d.Action != core.ActionBlock || d.Layer != "policy" || d.App != "抖音" {
		t.Errorf("decide=%+v", d)
	}
}

// [D6] 核心回归: /api/decide 是「策略预演」, 服务端强制 DryRun, 绝不写行为审计。
// 真机 P2 缺陷就是「点一次预演, 审计里多一条与真实流量不可区分的记录」。
func Test_API_decide_isDryRun_noAuditPollution_D6(t *testing.T) {
	s, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/decide",
		strings.NewReader(`{"client_ip":"10.0.0.7","domain":"example.org","dry_run":false}`)))
	// 即便客户端伪造 dry_run:false, 服务端也强制预演
	var d core.Decision
	_ = json.Unmarshal(rec.Body.Bytes(), &d)
	if d.Source != "probe" {
		t.Errorf("decide 未标记 probe 来源: %q", d.Source)
	}
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/audit?client=10.0.0.7", nil))
	var ds []core.Decision
	_ = json.Unmarshal(rec.Body.Bytes(), &ds)
	if len(ds) != 0 {
		t.Errorf("预演污染了行为审计: %+v", ds)
	}
}

// 真实流量(非预演)仍写入审计, 且带 Source 便于与预演区分/过滤。
func Test_API_realDecision_recordsAudit_andSourceFilter(t *testing.T) {
	s, _ := newTestServer(t)
	s.eng.Decide(core.Request{ClientIP: "10.0.0.7", Domain: "real.example", Source: "dns"})
	s.eng.Decide(core.Request{ClientIP: "10.0.0.8", Domain: "probe.example", DryRun: true})
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/audit?source=dns", nil))
	var ds []core.Decision
	_ = json.Unmarshal(rec.Body.Bytes(), &ds)
	if len(ds) != 1 || ds[0].Domain != "real.example" || ds[0].Source != "dns" {
		t.Errorf("按来源过滤失败: %+v", ds)
	}
	// DELETE 清空审计(清理历史残留的预演记录)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/audit", nil))
	if rec.Code != 200 {
		t.Errorf("DELETE /api/audit status=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/audit", nil))
	_ = json.Unmarshal(rec.Body.Bytes(), &ds)
	if len(ds) != 0 {
		t.Errorf("清空后仍有记录: %+v", ds)
	}
}

func Test_API_acl_isRenderOnly(t *testing.T) {
	s, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/acl", nil))
	var m map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &m)
	if m["applied"] != false {
		t.Errorf("acl must never auto-apply: %v", m)
	}
	if !strings.Contains(m["ruleset"].(string), "table inet netfence") {
		t.Errorf("bad ruleset: %v", m["ruleset"])
	}
	// 纯文本渲染
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/acl?format=text", nil))
	bt := rec.Body.String()
	if rec.Code != 200 || !strings.Contains(bt, "table inet netfence") {
		t.Errorf("format=text 失败: %d %q", rec.Code, bt)
	}
}

// [D10] 只读端点对未实现的 method 返回 405, 不再 200 假成功。
func Test_API_methodGuard_405_D10(t *testing.T) {
	s, _ := newTestServer(t)
	cases := []struct {
		method, path string
	}{
		{http.MethodPost, "/api/health"},
		{http.MethodPut, "/api/status"},
		{http.MethodDelete, "/api/apps"},
		{http.MethodPost, "/api/summary"},
		{http.MethodGet, "/api/reload"}, // reload 只允许 POST
		{http.MethodGet, "/api/decide"}, // decide 只允许 POST
		{http.MethodPost, "/api/acl"},   // acl 只允许 GET
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest(c.method, c.path, nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s %s: status=%d want 405", c.method, c.path, rec.Code)
		}
		if rec.Header().Get("Allow") == "" {
			t.Errorf("%s %s: 缺 Allow 头", c.method, c.path)
		}
	}
}

// [D3/D4] 过滤列表写入端点: 真落盘 + 立即生效, 不再 200 但内容不变(假成功)。
func Test_API_lists_writePlane_D3_D4(t *testing.T) {
	s, dir := newTestServer(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/lists",
		strings.NewReader(`{"name":"myads.txt","body":"||ad.example^\n||tracker.example^"}`)))
	if rec.Code != 200 {
		t.Fatalf("POST /api/lists status=%d body=%s", rec.Code, rec.Body.String())
	}
	// 文件真的落盘
	if _, err := os.Stat(filepath.Join(dir, "lists", "myads.txt")); err != nil {
		t.Errorf("列表未落盘: %v", err)
	}
	// GET 反映新内容(引擎已 Reload)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/lists", nil))
	var m map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &m)
	domains, _ := m["domains"].([]any)
	if len(domains) < 2 {
		t.Errorf("GET /api/lists 未反映写入: %v", m["domains"])
	}
	// 裁决真正生效(不再只展示)
	d := s.eng.Decide(core.Request{ClientIP: "10.0.0.7", Domain: "x.ad.example"})
	if !d.Blocked() || d.Layer != "filterlist" {
		t.Errorf("写入的列表未进入裁决: %+v", d)
	}
	// 目录穿越被拒
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/lists",
		strings.NewReader(`{"name":"../evil.txt","body":"x"}`)))
	if rec.Code == 200 {
		t.Error("目录穿越文件名未被拒绝")
	}
	// DELETE 删除并重载
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/lists?name=myads.txt", nil))
	if rec.Code != 200 {
		t.Errorf("DELETE /api/lists status=%d", rec.Code)
	}
	if _, err := os.Stat(filepath.Join(dir, "lists", "myads.txt")); !os.IsNotExist(err) {
		t.Errorf("DELETE 后文件仍在: %v", err)
	}
}

// [D3/D4] 威胁情报写入端点: 真落盘 + 立即生效。
func Test_API_threat_writePlane_D3_D4(t *testing.T) {
	s, dir := newTestServer(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/threat",
		strings.NewReader(`{"name":"myfeed.txt","body":"203.0.113.0/24\n198.51.100.7"}`)))
	if rec.Code != 200 {
		t.Fatalf("POST /api/threat status=%d body=%s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "threat", "myfeed.txt")); err != nil {
		t.Errorf("威胁列表未落盘: %v", err)
	}
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/threat", nil))
	var m map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &m)
	ips, _ := m["ips"].([]any)
	if len(ips) < 2 {
		t.Errorf("GET /api/threat 未反映写入: %v", m["ips"])
	}
	d := s.eng.Decide(core.Request{ClientIP: "10.0.0.7", Domain: "whatever.net", DstIP: "203.0.113.7"})
	if !d.Blocked() || d.Layer != "threat" {
		t.Errorf("写入的威胁 IP 未进入裁决: %+v", d)
	}
}

// [D8] DNS 应答里解析到的威胁 IP 也要拦(旧版 handleDNS 不带 DstIP, 威胁层不可达)。
func Test_API_decideResolvedIP_D8(t *testing.T) {
	s, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/threat",
		strings.NewReader(`{"name":"feed.txt","body":"203.0.113.0/24"}`)))
	if rec.Code != 200 {
		t.Fatalf("seed threat status=%d", rec.Code)
	}
	hit, blocked := s.eng.DecideResolvedIP(core.Request{ClientIP: "10.0.0.7", Domain: "cdn.example"},
		[]string{"1.1.1.1", "203.0.113.9"})
	if !blocked || hit != "203.0.113.9" {
		t.Errorf("解析到的威胁 IP 未拦: hit=%q blocked=%v", hit, blocked)
	}
	// 命中写入一条 source=dns 的审计
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/audit?source=dns", nil))
	var ds []core.Decision
	_ = json.Unmarshal(rec.Body.Bytes(), &ds)
	if len(ds) != 1 || ds[0].RuleID == "" {
		t.Errorf("威胁复核未留痕: %+v", ds)
	}
}

// [D11] 策略版本冲突检测: 带落后 X-Policy-Rev 提交返回 409, 不静默覆盖手工改动。
func Test_API_policyRev_conflict_D11(t *testing.T) {
	s, _ := newTestServer(t)
	rules := `[{"id":"r1","enabled":true,"priority":1,"action":"audit"}]`
	// 先正常保存一次, rev 从 0 -> 1
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/policy", strings.NewReader(rules))
	req.Header.Set("X-Policy-Rev", "0")
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("首次保存 status=%d body=%s", rec.Code, rec.Body.String())
	}
	// 再用落后的 rev=0 提交 -> 409
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, "/api/policy", strings.NewReader(rules))
	req.Header.Set("X-Policy-Rev", "0")
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Errorf("落后 rev 应 409, got %d", rec.Code)
	}
	// 不带 rev 头仍兼容(旧 UI)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/policy", strings.NewReader(rules)))
	if rec.Code != 200 {
		t.Errorf("无 rev 头兼容失败: %d", rec.Code)
	}
}

// [D2] ACL 下发需二次确认; 未确认不下发。
func Test_API_aclApply_requiresConfirm_D2(t *testing.T) {
	s, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/acl/apply",
		strings.NewReader(`{"confirm":false}`)))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("未确认应 400, got %d", rec.Code)
	}
}
