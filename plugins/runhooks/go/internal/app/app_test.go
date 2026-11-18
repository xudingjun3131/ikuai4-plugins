package app_test

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/GitterBois/runhooks/internal/app"
	"github.com/GitterBois/runhooks/internal/config"
	"github.com/GitterBois/runhooks/internal/store"
)

func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}

func waitFlows(t *testing.T, sink *store.Store, n int) []store.Flow {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
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

func Test_App_capture_HTTP_and_HTTPS_via_List(t *testing.T) {
	// Given: dual-listener app + HTTP origin + TLS origin
	httpOrigin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/http-path" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("http-ok"))
	}))
	t.Cleanup(httpOrigin.Close)

	httpsOrigin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/https-path" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("https-ok"))
	}))
	t.Cleanup(httpsOrigin.Close)

	dataDir := t.TempDir()
	cfg := config.Config{
		ProxyAddr:     freeAddr(t),
		APIAddr:       freeAddr(t),
		DataDir:       dataDir,
		MaxFlows:      100,
		MaxBody:       1 << 20,
		BPMax:         32,
		BPTimeout:     time.Minute,
		ScriptTimeout: 100 * time.Millisecond,
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	a, err := app.New(cfg, log, "test")
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	a.Start()
	t.Cleanup(func() { a.Shutdown(5 * time.Second) })

	// wait for listeners
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		c, err := net.DialTimeout("tcp", cfg.ProxyAddr, 50*time.Millisecond)
		if err == nil {
			_ = c.Close()
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// health still on API
	resp, err := http.Get("http://" + cfg.APIAddr + "/api/health")
	if err != nil {
		t.Fatalf("health: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health status=%d", resp.StatusCode)
	}

	proxyURL, err := url.Parse("http://" + cfg.ProxyAddr)
	if err != nil {
		t.Fatalf("proxy url: %v", err)
	}

	// When: HTTP through MITM proxy
	httpClient := &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)},
		Timeout:   5 * time.Second,
	}
	hresp, err := httpClient.Get(httpOrigin.URL + "/http-path")
	if err != nil {
		t.Fatalf("HTTP GET via proxy: %v", err)
	}
	hb, _ := io.ReadAll(hresp.Body)
	_ = hresp.Body.Close()
	if hresp.StatusCode != http.StatusOK || string(hb) != "http-ok" {
		t.Fatalf("HTTP status=%d body=%q", hresp.StatusCode, hb)
	}

	// When: HTTPS through MITM with local CA
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(a.Authority().RootPEM()) {
		t.Fatal("AppendCertsFromPEM failed")
	}
	httpsClient := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
			TLSClientConfig: &tls.Config{
				RootCAs:    pool,
				MinVersion: tls.VersionTLS12,
			},
		},
		Timeout: 5 * time.Second,
	}
	sresp, err := httpsClient.Get(httpsOrigin.URL + "/https-path")
	if err != nil {
		t.Fatalf("HTTPS GET via proxy: %v", err)
	}
	sb, _ := io.ReadAll(sresp.Body)
	_ = sresp.Body.Close()
	if sresp.StatusCode != http.StatusAccepted || string(sb) != "https-ok" {
		t.Fatalf("HTTPS status=%d body=%q", sresp.StatusCode, sb)
	}

	// Then: List() has ≥1 HTTP and ≥1 HTTPS flow (path markers)
	flows := waitFlows(t, a.Store(), 2)
	var httpN, httpsN int
	for _, f := range flows {
		if strings.Contains(f.Request.URL, "/http-path") {
			httpN++
		}
		if strings.Contains(f.Request.URL, "/https-path") {
			httpsN++
		}
	}
	if httpN < 1 {
		t.Fatalf("want ≥1 HTTP flow with /http-path, flows=%v", flowSummary(flows))
	}
	if httpsN < 1 {
		t.Fatalf("want ≥1 HTTPS flow with /https-path, flows=%v", flowSummary(flows))
	}

	// CA files on disk for smoke --cacert
	caPath := a.Authority().RootCertPath()
	if _, err := os.Stat(caPath); err != nil {
		t.Fatalf("ca.pem missing: %v", err)
	}
}

func flowSummary(flows []store.Flow) string {
	parts := make([]string, 0, len(flows))
	for _, f := range flows {
		parts = append(parts, fmt.Sprintf("{scheme=%q url=%q status=%d}", f.Scheme, f.Request.URL, f.Response.Status))
	}
	return strings.Join(parts, " ")
}
