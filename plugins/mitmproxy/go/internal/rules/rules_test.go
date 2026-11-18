package rules_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/GitterBois/proxying/internal/proxy"
	"github.com/GitterBois/proxying/internal/rules"
)

func TestHostBlock_403(t *testing.T) {
	dir := t.TempDir()
	c, err := rules.NewChain(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Save([]rules.Rule{{
		ID: "b1", Enabled: true, Type: rules.TypeHostBlock,
		Match: rules.Match{Host: "ads.example.com"},
	}}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "http://ads.example.com/x", nil)
	_, resp, act := c.OnRequest(req)
	if act != proxy.RequestRespond || resp == nil || resp.StatusCode != 403 {
		t.Fatalf("want 403 respond, got act=%v code=%v", act, statusOf(resp))
	}
}

func TestHostAllow_blocksNonMatch(t *testing.T) {
	dir := t.TempDir()
	c, err := rules.NewChain(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Save([]rules.Rule{{
		ID: "a1", Enabled: true, Type: rules.TypeHostAllow,
		Match: rules.Match{Host: "ok.example.com"},
	}}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "http://other.example.com/", nil)
	_, resp, act := c.OnRequest(req)
	if act != proxy.RequestRespond || statusOf(resp) != 403 {
		t.Fatalf("want 403, got %v %v", act, statusOf(resp))
	}
	req2 := httptest.NewRequest(http.MethodGet, "http://ok.example.com/", nil)
	_, resp2, act2 := c.OnRequest(req2)
	if act2 != proxy.RequestContinue || resp2 != nil {
		t.Fatalf("allow should continue, act=%v", act2)
	}
}

func TestWildcardHost(t *testing.T) {
	dir := t.TempDir()
	c, err := rules.NewChain(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Save([]rules.Rule{{
		ID: "w", Enabled: true, Type: rules.TypeHostBlock,
		Match: rules.Match{Host: "*.ads.example.com"},
	}}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "http://track.ads.example.com/", nil)
	_, resp, act := c.OnRequest(req)
	if act != proxy.RequestRespond || statusOf(resp) != 403 {
		t.Fatalf("wildcard block failed: %v %v", act, statusOf(resp))
	}
}

func TestHeaderSetRequest(t *testing.T) {
	dir := t.TempDir()
	c, err := rules.NewChain(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Save([]rules.Rule{{
		ID: "h", Enabled: true, Type: rules.TypeHeader,
		Match:  rules.Match{Host: "ex.com"},
		Header: &rules.HeaderMut{Phase: "request", Set: map[string]string{"X-Test": "1"}},
	}}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "http://ex.com/", nil)
	req, _, act := c.OnRequest(req)
	if act != proxy.RequestContinue {
		t.Fatal(act)
	}
	if req.Header.Get("X-Test") != "1" {
		t.Fatalf("header not set: %v", req.Header)
	}
}

func TestBodyReplaceResponse(t *testing.T) {
	dir := t.TempDir()
	c, err := rules.NewChain(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Save([]rules.Rule{{
		ID: "br", Enabled: true, Type: rules.TypeBodyReplace,
		Match:       rules.Match{Host: "ex.com"},
		BodyReplace: &rules.BodyReplace{Phase: "response", From: "foo", To: "bar"},
	}}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "http://ex.com/", nil)
	resp := &http.Response{
		StatusCode: 200,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader([]byte("hello foo end"))),
		Request:    req,
	}
	out, act := c.OnResponse(resp)
	if act != proxy.ResponseContinue {
		t.Fatal(act)
	}
	b, _ := io.ReadAll(out.Body)
	if string(b) != "hello bar end" {
		t.Fatalf("body=%q", b)
	}
}

func TestMapLocal_andEscape(t *testing.T) {
	dir := t.TempDir()
	jail := filepath.Join(dir, "map-local")
	if err := os.MkdirAll(jail, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(jail, "hi.txt"), []byte("mapped"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := rules.NewChain(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Save([]rules.Rule{{
		ID: "m", Enabled: true, Type: rules.TypeMapLocal,
		Match:    rules.Match{Host: "local.test"},
		MapLocal: &rules.MapLocal{Status: 200, File: "hi.txt"},
	}}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "http://local.test/", nil)
	_, resp, act := c.OnRequest(req)
	if act != proxy.RequestRespond || statusOf(resp) != 200 {
		t.Fatalf("map local: %v %v", act, statusOf(resp))
	}
	b, _ := io.ReadAll(resp.Body)
	if string(b) != "mapped" {
		t.Fatalf("body=%q", b)
	}

	// escape
	if _, err := rules.ResolveMapLocal(jail, "../secret"); err == nil {
		t.Fatal("expected escape error")
	}
	if _, err := rules.ResolveMapLocal(jail, "/etc/passwd"); err == nil {
		t.Fatal("expected abs error")
	}
	if err := c.Save([]rules.Rule{{
		ID: "bad", Enabled: true, Type: rules.TypeMapLocal,
		Match:    rules.Match{Host: "local.test"},
		MapLocal: &rules.MapLocal{File: "../secret"},
	}}); err != nil {
		t.Fatal(err)
	}
	_, resp2, act2 := c.OnRequest(req)
	if act2 != proxy.RequestRespond || statusOf(resp2) != 502 {
		t.Fatalf("escape should 502, got %v %v", act2, statusOf(resp2))
	}
}

func statusOf(r *http.Response) int {
	if r == nil {
		return 0
	}
	return r.StatusCode
}
