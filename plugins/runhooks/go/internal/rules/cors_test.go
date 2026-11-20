package rules_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/GitterBois/runhooks/internal/proxy"
	"github.com/GitterBois/runhooks/internal/rules"
)

// [cors-fix 回归] 复现「使用代理时部分跨域请求报 CORS 错」。
// 内置 builtin-cors 规则对所有响应无条件 Set:
//   Access-Control-Allow-Origin: *
//   Access-Control-Allow-Methods: *
//   Access-Control-Allow-Headers: *
// 浏览器语义下 * 与 credentials 互斥、且会覆盖源站本来正确的 CORS 头;
// OPTIONS 预检又被转发给源站(常见 403/405) -> 只有「部分」请求报错。
// 期望: 按请求回显 Origin + Allow-Credentials, 预检本地答 204。

func newCORSChain(t *testing.T, mutate func([]rules.Rule) []rules.Rule) *rules.Chain {
	t.Helper()
	c, err := rules.NewChain(t.TempDir())
	if err != nil {
		t.Fatalf("NewChain: %v", err)
	}
	if mutate != nil {
		if err := c.Save(mutate(c.List())); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}
	return c
}

func corsResp(req *http.Request) *http.Response {
	return &http.Response{
		StatusCode: 200, Status: "200 OK", Proto: "HTTP/1.1", ProtoMajor: 1, ProtoMinor: 1,
		Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(nil)), Request: req,
	}
}

func hasVary(h http.Header, token string) bool {
	for _, v := range h.Values("Vary") {
		if strings.EqualFold(strings.TrimSpace(v), token) {
			return true
		}
	}
	return false
}

// 带凭证的跨域响应: ACAO 必须回显具体 Origin 而不是 *, 且带 Allow-Credentials。
func Test_CORS_response_echoesOriginAndCredentials(t *testing.T) {
	c := newCORSChain(t, nil)
	req := httptest.NewRequest(http.MethodGet, "https://api.example.com/data", nil)
	req.Header.Set("Origin", "https://app.example.com")
	req.Header.Set("Cookie", "sid=1")
	req.Header.Set("Authorization", "Bearer x")

	got, action := c.OnResponse(corsResp(req))
	if action != proxy.ResponseContinue {
		t.Fatalf("action=%v want ResponseContinue", action)
	}
	if v := got.Header.Get("Access-Control-Allow-Origin"); v != "https://app.example.com" {
		t.Errorf("ACAO=%q, 期望回显具体 Origin(凭证请求下 * 会被浏览器拒绝)", v)
	}
	if v := got.Header.Get("Access-Control-Allow-Credentials"); v != "true" {
		t.Errorf("ACAC=%q, 凭证请求必须为 true", v)
	}
	if !hasVary(got.Header, "Origin") {
		t.Error("缺少 Vary: Origin(会污染缓存)")
	}
	for _, k := range []string{"Access-Control-Allow-Methods", "Access-Control-Allow-Headers"} {
		if v := got.Header.Get(k); v == "*" || v == "" {
			t.Errorf("%s=%q, 凭证请求下通配 * 不被接受", k, v)
		}
	}
}

// 源站本来就正确设置了具体 Origin + 凭证, 不能被代理用 * 覆盖掉。
func Test_CORS_response_preservesOriginOwnCORS(t *testing.T) {
	c := newCORSChain(t, nil)
	req := httptest.NewRequest(http.MethodGet, "https://api.example.com/data", nil)
	req.Header.Set("Origin", "https://app.example.com")
	resp := corsResp(req)
	resp.Header.Set("Access-Control-Allow-Origin", "https://app.example.com")
	resp.Header.Set("Access-Control-Allow-Credentials", "true")
	resp.Header.Set("Content-Security-Policy", "default-src 'self'")

	got, _ := c.OnResponse(resp)
	if v := got.Header.Get("Access-Control-Allow-Origin"); v != "https://app.example.com" {
		t.Errorf("源站正确的 ACAO 被覆盖成 %q", v)
	}
	// 其它响应头规则(builtin-strip-csp)仍须生效
	if v := got.Header.Get("Content-Security-Policy"); v != "" {
		t.Errorf("builtin-strip-csp 失效, CSP=%q", v)
	}
}

// 非跨域请求(无 Origin)不应被注入任何 CORS 头, 避免污染同源响应。
func Test_CORS_response_noOrigin_skipsInjection(t *testing.T) {
	c := newCORSChain(t, nil)
	req := httptest.NewRequest(http.MethodGet, "https://api.example.com/data", nil)
	got, _ := c.OnResponse(corsResp(req))
	for _, k := range []string{"Access-Control-Allow-Origin", "Access-Control-Allow-Credentials"} {
		if v := got.Header.Get(k); v != "" {
			t.Errorf("无 Origin 却被注入 %s=%q", k, v)
		}
	}
}

// 预检 OPTIONS 必须本地 204 应答, 回显请求的方法/头/私有网络标志, 不再转发源站。
func Test_CORS_preflight_answersLocally(t *testing.T) {
	c := newCORSChain(t, nil)
	req := httptest.NewRequest(http.MethodOptions, "https://api.example.com/data", nil)
	req.Header.Set("Origin", "https://app.example.com")
	req.Header.Set("Access-Control-Request-Method", "PUT")
	req.Header.Set("Access-Control-Request-Headers", "x-token, content-type")
	req.Header.Set("Access-Control-Request-Private-Network", "true")

	_, resp, action := c.OnRequest(req)
	if action != proxy.RequestRespond {
		t.Fatalf("action=%v want RequestRespond(预检不该转发源站)", action)
	}
	if resp == nil {
		t.Fatal("预检应答为 nil")
	}
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("预检 status=%d want 204", resp.StatusCode)
	}
	if v := resp.Header.Get("Access-Control-Allow-Origin"); v != "https://app.example.com" {
		t.Errorf("预检 ACAO=%q", v)
	}
	if v := resp.Header.Get("Access-Control-Allow-Methods"); v != "PUT" {
		t.Errorf("预检 ACAM=%q want PUT", v)
	}
	if v := resp.Header.Get("Access-Control-Allow-Headers"); v != "x-token, content-type" {
		t.Errorf("预检 ACAH=%q", v)
	}
	if v := resp.Header.Get("Access-Control-Allow-Private-Network"); v != "true" {
		t.Errorf("缺 Access-Control-Allow-Private-Network=true (Chrome PNA)")
	}
	if v := resp.Header.Get("Access-Control-Allow-Credentials"); v != "true" {
		t.Errorf("预检 ACAC=%q", v)
	}
	if resp.Body == nil {
		t.Error("预检应答 Body 为 nil, 引擎 captureShortCircuit 会 panic")
	}
}

// 用户在面板里停用 builtin-cors 后, 预检应照常转发(不做本地应答)。
func Test_CORS_preflight_disabled_whenRuleOff(t *testing.T) {
	c := newCORSChain(t, func(rs []rules.Rule) []rules.Rule {
		for i := range rs {
			if rs[i].ID == "builtin-cors" {
				rs[i].Enabled = false
			}
		}
		return rs
	})
	req := httptest.NewRequest(http.MethodOptions, "https://api.example.com/data", nil)
	req.Header.Set("Origin", "https://app.example.com")
	req.Header.Set("Access-Control-Request-Method", "PUT")

	_, resp, action := c.OnRequest(req)
	if action != proxy.RequestContinue || resp != nil {
		t.Errorf("规则停用后仍短路: action=%v resp=%v", action, resp)
	}
}

// 无 Origin 的普通 OPTIONS 不应被误判成预检短路。
func Test_CORS_preflight_noOrigin_notShortCircuited(t *testing.T) {
	c := newCORSChain(t, nil)
	req := httptest.NewRequest(http.MethodOptions, "https://api.example.com/data", nil)
	req.Header.Set("Access-Control-Request-Method", "PUT")

	_, resp, action := c.OnRequest(req)
	if action != proxy.RequestContinue || resp != nil {
		t.Errorf("无 Origin 的 OPTIONS 被误短路: action=%v resp=%v", action, resp)
	}
}
