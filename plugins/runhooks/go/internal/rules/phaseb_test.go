package rules_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/GitterBois/runhooks/internal/proxy"
	"github.com/GitterBois/runhooks/internal/rules"
)

func TestSSLBypass(t *testing.T) {
	dir := t.TempDir()
	c, err := rules.NewChain(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Save([]rules.Rule{{
		ID: "s", Enabled: true, Type: rules.TypeSSLBypass,
		Match: rules.Match{Host: "pin.example.com"},
	}}); err != nil {
		t.Fatal(err)
	}
	if !c.SSLBypass("pin.example.com") {
		t.Fatal("expected bypass")
	}
	if c.SSLBypass("other.com") {
		t.Fatal("no bypass")
	}
}

func TestAccessControlCIDR(t *testing.T) {
	dir := t.TempDir()
	c, err := rules.NewChain(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Save([]rules.Rule{{
		ID: "ac", Enabled: true, Type: rules.TypeAccessControl,
		Match: rules.Match{Host: "*"},
		AccessControl: &rules.AccessControl{
			CIDRs:  []string{"127.0.0.1/32"},
			Action: "allow",
		},
	}}); err != nil {
		t.Fatal(err)
	}
	if !c.ClientAllowed("127.0.0.1", "any.com") {
		t.Fatal("loopback allowed")
	}
	if c.ClientAllowed("8.8.8.8", "any.com") {
		t.Fatal("remote denied")
	}
}

func TestGatewayRewrite(t *testing.T) {
	dir := t.TempDir()
	c, err := rules.NewChain(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Save([]rules.Rule{{
		ID: "g", Enabled: true, Type: rules.TypeGateway,
		Match:   rules.Match{Host: "public.test"},
		Gateway: &rules.Gateway{Upstream: "http://127.0.0.1:9"},
	}}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "http://public.test/path", nil)
	req, _, act := c.OnRequest(req)
	if act != proxy.RequestContinue {
		t.Fatal(act)
	}
	if req.URL.Host != "127.0.0.1:9" {
		t.Fatalf("host=%s", req.URL.Host)
	}
	if req.URL.Path != "/path" {
		t.Fatalf("path=%s", req.URL.Path)
	}
}

func TestMirrorRemote(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("mirrored"))
	}))
	t.Cleanup(origin.Close)

	dir := t.TempDir()
	c, err := rules.NewChain(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Save([]rules.Rule{{
		ID: "m", Enabled: true, Type: rules.TypeMirrorRemote,
		Match:        rules.Match{Host: "x.test"},
		MirrorRemote: &rules.MirrorRemote{URL: origin.URL},
	}}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "http://x.test/", nil)
	_, resp, act := c.OnRequest(req)
	if act != proxy.RequestRespond || resp == nil {
		t.Fatalf("act=%v", act)
	}
	b, _ := io.ReadAll(resp.Body)
	if string(b) != "mirrored" {
		t.Fatalf("body=%q", b)
	}
}

func TestSecondaryProxyURL(t *testing.T) {
	dir := t.TempDir()
	c, err := rules.NewChain(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Save([]rules.Rule{{
		ID: "sp", Enabled: true, Type: rules.TypeSecondaryProxy,
		Match:          rules.Match{Host: "via.test"},
		SecondaryProxy: &rules.SecondaryProxy{ProxyURL: "http://127.0.0.1:9999"},
	}}); err != nil {
		t.Fatal(err)
	}
	if u := c.SecondaryProxyURL("via.test"); u != "http://127.0.0.1:9999" {
		t.Fatalf("u=%s", u)
	}
}

func TestReverseProxiesList(t *testing.T) {
	dir := t.TempDir()
	c, err := rules.NewChain(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Save([]rules.Rule{{
		ID: "rp", Enabled: true, Type: rules.TypeReverseProxy,
		ReverseProxy: &rules.ReverseProxy{Listen: "127.0.0.1:0", Target: "http://127.0.0.1:1"},
	}}); err != nil {
		t.Fatal(err)
	}
	if len(c.ReverseProxies()) != 1 {
		t.Fatal(c.ReverseProxies())
	}
}
