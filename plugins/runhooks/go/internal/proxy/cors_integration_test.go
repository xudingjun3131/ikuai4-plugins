package proxy_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/GitterBois/runhooks/internal/proxy"
	"github.com/GitterBois/runhooks/internal/rules"
)

// [cors-fix] 端到端: 规则链作为引擎 Rules 钩子, 真发一次「使用代理」的跨域请求,
// 验证 CORS 头不再是无条件 *, 且 OPTIONS 预检不落到源站。
// 复现的是 1.1.8 真机反馈, 而非只测 rules 包的内部函数。

func Test_Proxy_CORS_endToEnd(t *testing.T) {
	var preflightReached atomic.Int64
	var gets atomic.Int64
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodOptions:
			preflightReached.Add(1)
			w.WriteHeader(http.StatusMethodNotAllowed) // 典型源站: 预检直接 405
		case http.MethodGet:
			gets.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
		}
	}))
	defer origin.Close()

	chain, err := rules.NewChain(t.TempDir())
	if err != nil {
		t.Fatalf("rules.NewChain: %v", err)
	}
	eng, _, _ := setupEngine(t, 1<<20, proxy.HookSlots{Rules: chain})
	proxyURL, stop := startProxy(t, eng)
	defer stop()

	u, _ := url.Parse(proxyURL)
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(u)}, Timeout: 5 * time.Second}

	// 1) 带凭证的跨域 GET: 必须回显具体 Origin + Allow-Credentials, 不能是 *。
	req, _ := http.NewRequest(http.MethodGet, origin.URL+"/api", nil)
	req.Header.Set("Origin", "https://app.example.com")
	req.Header.Set("Cookie", "sid=1")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET via proxy: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != `{"ok":true}` {
		t.Fatalf("GET status=%d body=%q", resp.StatusCode, body)
	}
	if v := resp.Header.Get("Access-Control-Allow-Origin"); v != "https://app.example.com" {
		t.Errorf("GET ACAO=%q want 回显具体 Origin", v)
	}
	if v := resp.Header.Get("Access-Control-Allow-Credentials"); v != "true" {
		t.Errorf("GET ACAC=%q want true", v)
	}

	// 2) OPTIONS 预检: 必须本地 204 应答, 源站收不到这次请求(不会 405)。
	pre, _ := http.NewRequest(http.MethodOptions, origin.URL+"/api", nil)
	pre.Header.Set("Origin", "https://app.example.com")
	pre.Header.Set("Access-Control-Request-Method", "PUT")
	pre.Header.Set("Access-Control-Request-Headers", "x-token")
	presp, err := client.Do(pre)
	if err != nil {
		t.Fatalf("OPTIONS via proxy: %v", err)
	}
	_ = presp.Body.Close()
	if presp.StatusCode != http.StatusNoContent {
		t.Errorf("OPTIONS status=%d want 204", presp.StatusCode)
	}
	if v := presp.Header.Get("Access-Control-Allow-Methods"); v != "PUT" {
		t.Errorf("OPTIONS ACAM=%q want PUT", v)
	}
	if v := presp.Header.Get("Access-Control-Allow-Headers"); v != "x-token" {
		t.Errorf("OPTIONS ACAH=%q want x-token", v)
	}
	if n := preflightReached.Load(); n != 0 {
		t.Errorf("预检被转发到源站 %d 次(应本地应答)", n)
	}
	if n := gets.Load(); n != 1 {
		t.Errorf("源站 GET 次数=%d want 1", n)
	}
}
