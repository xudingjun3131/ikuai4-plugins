package proxy_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/GitterBois/runhooks/internal/proxy"
)

// 端到端: 逐跳头 / 内部标记头不得发给上游; chunked 上游经快照后回写必须是
// 确定的 Content-Length 而非 CL+TE 并存(非法响应, 严格客户端报 400)。
func Test_Compat400_hop_headers_stripped_and_CL_TE_fixed(t *testing.T) {
	var upstream http.Header
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstream = r.Header.Clone()
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("hello"))
		w.(http.Flusher).Flush() // 强制 Transfer-Encoding: chunked
	}))
	defer origin.Close()

	eng, _, _ := setupEngine(t, 1<<20, proxy.HookSlots{})
	pu, cleanup := startProxy(t, eng)
	defer cleanup()
	u, _ := url.Parse(pu)
	cli := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(u)}, Timeout: 10 * time.Second}

	req, _ := http.NewRequest(http.MethodGet, origin.URL+"/", nil)
	req.Header.Set("Proxy-Connection", "keep-alive")
	req.Header.Set("X-Proxying-Highlight", "API")
	req.Header.Set("X-Proxying-Highlight-Color", "#3b82f6")
	req.Header.Set("Accept-Encoding", "identity")
	resp, err := cli.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != "hello" {
		t.Fatalf("响应异常: status=%d body=%q", resp.StatusCode, body)
	}
	for _, k := range []string{"Proxy-Connection", "X-Proxying-Highlight", "X-Proxying-Highlight-Color"} {
		if v := upstream.Get(k); v != "" {
			t.Errorf("上游收到了不该发的头 %s=%q", k, v)
		}
	}
	if v := upstream.Get("Accept-Encoding"); v != "identity" {
		t.Errorf("客户端 Accept-Encoding 被改写为 %q", v)
	}
	if te, cl := resp.Header.Get("Transfer-Encoding"), resp.Header.Get("Content-Length"); te != "" && cl != "" {
		t.Errorf("CL 与 TE 并存(非法响应): TE=%q CL=%q", te, cl)
	}
	if resp.Header.Get("Content-Length") != "5" {
		t.Errorf("响应缺少确定的 Content-Length: %q", resp.Header.Get("Content-Length"))
	}
}
