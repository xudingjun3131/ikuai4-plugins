package proxy_test

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/GitterBois/proxying/internal/ca"
	"github.com/GitterBois/proxying/internal/proxy"
	"github.com/GitterBois/proxying/internal/store"
)

type countingHooks struct {
	reqN  atomic.Int64
	respN atomic.Int64
}

func (c *countingHooks) OnRequest(r *http.Request) (*http.Request, *http.Response, proxy.RequestAction) {
	c.reqN.Add(1)
	return r, nil, proxy.RequestContinue
}

func (c *countingHooks) OnResponse(r *http.Response) (*http.Response, proxy.ResponseAction) {
	c.respN.Add(1)
	return r, proxy.ResponseContinue
}

func setupEngine(t *testing.T, maxBody int64, hooks proxy.HookSlots) (*proxy.Engine, *store.Store, *ca.Authority) {
	t.Helper()
	dir := t.TempDir()
	auth, err := ca.Load(dir)
	if err != nil {
		t.Fatalf("ca.Load: %v", err)
	}
	sink := store.New(100)
	eng, err := proxy.NewEngine(proxy.EngineConfig{
		Authority: auth,
		Sink:      sink,
		MaxBody:   maxBody,
		Hooks:     hooks,
	})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	return eng, sink, auth
}

func startProxy(t *testing.T, eng *proxy.Engine) (proxyURL string, cleanup func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := &http.Server{Handler: eng.Handler()}
	go func() { _ = srv.Serve(ln) }()
	return "http://" + ln.Addr().String(), func() {
		_ = srv.Close()
		_ = ln.Close()
	}
}

func waitFlows(t *testing.T, sink *store.Store, n int) []store.Flow {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		got := sink.List(store.Filter{})
		if len(got) >= n {
			return got
		}
		time.Sleep(10 * time.Millisecond)
	}
	got := sink.List(store.Filter{})
	t.Fatalf("want >= %d flows, got %d", n, len(got))
	return got
}

func Test_Engine_HTTP_captures_status_and_path(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/hello" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("hi"))
	}))
	defer origin.Close()

	hooks := &countingHooks{}
	eng, sink, _ := setupEngine(t, 1<<20, proxy.HookSlots{Rules: hooks})
	proxyURL, stop := startProxy(t, eng)
	defer stop()

	u, _ := url.Parse(proxyURL)
	client := &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyURL(u)},
		Timeout:   5 * time.Second,
	}
	resp, err := client.Get(origin.URL + "/hello")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusTeapot {
		t.Fatalf("status=%d want 418", resp.StatusCode)
	}
	if string(body) != "hi" {
		t.Fatalf("body=%q", body)
	}

	flows := waitFlows(t, sink, 1)
	f := flows[len(flows)-1]
	if f.Response.Status != http.StatusTeapot {
		t.Fatalf("captured status=%d", f.Response.Status)
	}
	if !strings.Contains(f.Request.URL, "/hello") {
		t.Fatalf("captured url=%q missing /hello", f.Request.URL)
	}
	if hooks.reqN.Load() < 1 || hooks.respN.Load() < 1 {
		t.Fatalf("hook counters req=%d resp=%d", hooks.reqN.Load(), hooks.respN.Load())
	}
}

func Test_Engine_HTTPS_MITM_captures_status_and_path(t *testing.T) {
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/secure" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("ok"))
	}))
	defer origin.Close()

	hooks := &countingHooks{}
	eng, sink, auth := setupEngine(t, 1<<20, proxy.HookSlots{
		Rules:      hooks,
		Script:     hooks,
		Breakpoint: hooks,
	})
	proxyURL, stop := startProxy(t, eng)
	defer stop()

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(auth.RootPEM()) {
		t.Fatal("AppendCertsFromPEM failed")
	}
	u, _ := url.Parse(proxyURL)
	client := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(u),
			TLSClientConfig: &tls.Config{
				RootCAs:    pool,
				MinVersion: tls.VersionTLS12,
			},
		},
		Timeout: 5 * time.Second,
	}
	resp, err := client.Get(origin.URL + "/secure")
	if err != nil {
		t.Fatalf("HTTPS GET: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status=%d want 202", resp.StatusCode)
	}
	if string(body) != "ok" {
		t.Fatalf("body=%q", body)
	}

	flows := waitFlows(t, sink, 1)
	f := flows[len(flows)-1]
	if f.Response.Status != http.StatusAccepted {
		t.Fatalf("captured status=%d", f.Response.Status)
	}
	if !strings.Contains(f.Request.URL, "/secure") {
		t.Fatalf("captured url=%q", f.Request.URL)
	}
	if f.Scheme != "https" && !strings.HasPrefix(f.Request.URL, "https://") {
		// scheme field may be https from MITM TLS
		t.Logf("scheme=%q url=%q (informational)", f.Scheme, f.Request.URL)
	}
	if hooks.reqN.Load() < 1 || hooks.respN.Load() < 1 {
		t.Fatalf("hook counters req=%d resp=%d want >=1", hooks.reqN.Load(), hooks.respN.Load())
	}
}

func Test_Engine_truncates_response_body(t *testing.T) {
	big := strings.Repeat("A", 100)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(big))
	}))
	defer origin.Close()

	const maxBody = 16
	eng, sink, _ := setupEngine(t, maxBody, proxy.HookSlots{})
	proxyURL, stop := startProxy(t, eng)
	defer stop()

	u, _ := url.Parse(proxyURL)
	client := &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyURL(u)},
		Timeout:   5 * time.Second,
	}
	resp, err := client.Get(origin.URL + "/big")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	gotBody, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	// client still receives full body (capture-only cap)
	if string(gotBody) != big {
		t.Fatalf("forwarded body len=%d want %d", len(gotBody), len(big))
	}

	flows := waitFlows(t, sink, 1)
	f := flows[0]
	if !f.Response.Truncated {
		t.Fatalf("want Truncated=true, size=%d bodyLen=%d", f.Response.Size, len(f.Response.Body))
	}
	if int64(len(f.Response.Body)) != maxBody {
		t.Fatalf("captured body len=%d want %d", len(f.Response.Body), maxBody)
	}
	if f.Response.Size != int64(len(big)) {
		t.Fatalf("Size=%d want %d", f.Response.Size, len(big))
	}
}

func Test_Engine_nil_hooks_are_noop(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "x")
	}))
	defer origin.Close()

	eng, sink, _ := setupEngine(t, 1024, proxy.HookSlots{})
	proxyURL, stop := startProxy(t, eng)
	defer stop()

	u, _ := url.Parse(proxyURL)
	client := &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyURL(u)},
		Timeout:   5 * time.Second,
	}
	resp, err := client.Get(origin.URL + "/")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	_ = resp.Body.Close()
	_ = waitFlows(t, sink, 1)
}

func Test_NewEngine_requires_deps(t *testing.T) {
	if _, err := proxy.NewEngine(proxy.EngineConfig{}); err == nil {
		t.Fatal("expected error")
	}
}
