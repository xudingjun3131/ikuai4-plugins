//go:build alpn

package proxy_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/GitterBois/proxying/internal/proxy"
)

func Test_Engine_ALPN_HTTPS_negotiates_protocol_via_MITM(t *testing.T) {
	// Given: HTTPS origin + MITM engine with local CA
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/alpn" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("alpn-ok"))
	}))
	t.Cleanup(origin.Close)

	eng, sink, auth := setupEngine(t, 1<<20, proxy.HookSlots{})
	proxyURL, stop := startProxy(t, eng)
	t.Cleanup(stop)

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(auth.RootPEM()) {
		t.Fatal("AppendCertsFromPEM failed")
	}
	u, err := url.Parse(proxyURL)
	if err != nil {
		t.Fatalf("parse proxy URL: %v", err)
	}

	var negotiated atomic.Value
	client := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(u),
			TLSClientConfig: &tls.Config{
				RootCAs:    pool,
				MinVersion: tls.VersionTLS12,
				NextProtos: []string{"h2", "http/1.1"},
			},
			ForceAttemptHTTP2: true,
		},
		Timeout: 5 * time.Second,
	}

	// When: HTTPS GET through CONNECT MITM with ALPN ClientHello
	req, err := http.NewRequestWithContext(
		httptrace.WithClientTrace(context.Background(), &httptrace.ClientTrace{
			TLSHandshakeDone: func(cs tls.ConnectionState, err error) {
				if err != nil {
					return
				}
				negotiated.Store(cs.NegotiatedProtocol)
			},
		}),
		http.MethodGet,
		origin.URL+"/alpn",
		nil,
	)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("HTTPS GET via MITM: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d want 200", resp.StatusCode)
	}
	if string(body) != "alpn-ok" {
		t.Fatalf("body=%q want alpn-ok", body)
	}

	// Then: non-empty NegotiatedProtocol (TLS ALPN evidence; bare H1 curl fails this gate)
	proto, _ := negotiated.Load().(string)
	if proto == "" {
		t.Fatal("NegotiatedProtocol empty: need TLS ALPN evidence (path B)")
	}
	switch proto {
	case "http/1.1", "h2", "http/1.0":
	default:
		t.Fatalf("unexpected NegotiatedProtocol %q", proto)
	}
	t.Logf("ALPN_PATH=B ALPN_NEGOTIATED=%s", proto)

	flows := waitFlows(t, sink, 1)
	f := flows[len(flows)-1]
	if f.Scheme != "https" {
		t.Fatalf("scheme=%q want https", f.Scheme)
	}
	if f.Response.Status != http.StatusOK {
		t.Fatalf("captured status=%d", f.Response.Status)
	}
}
