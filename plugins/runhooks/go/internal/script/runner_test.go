package script_test

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/GitterBois/runhooks/internal/proxy"
	"github.com/GitterBois/runhooks/internal/script"
)

func Test_Runner_injects_request_header(t *testing.T) {
	dir := t.TempDir()
	scripts := filepath.Join(dir, "scripts")
	if err := os.MkdirAll(scripts, 0o755); err != nil {
		t.Fatal(err)
	}
	src := `function onRequest(flow) {
  flow.request.headers["X-Script"] = "injected";
}`
	if err := os.WriteFile(filepath.Join(scripts, "inject.js"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := script.NewRunner(dir, 100*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodGet, "http://example.com/x", nil)
	if err != nil {
		t.Fatal(err)
	}
	out, resp, action := r.OnRequest(req)
	if action != proxy.RequestContinue || resp != nil {
		t.Fatalf("action=%v resp=%v", action, resp)
	}
	if got := out.Header.Get("X-Script"); got != "injected" {
		t.Fatalf("header = %q, want injected", got)
	}
}

func Test_Runner_timeout_kills_infinite_loop(t *testing.T) {
	dir := t.TempDir()
	scripts := filepath.Join(dir, "scripts")
	if err := os.MkdirAll(scripts, 0o755); err != nil {
		t.Fatal(err)
	}
	src := `function onRequest(flow) {
  while (true) {}
}`
	if err := os.WriteFile(filepath.Join(scripts, "loop.js"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := script.NewRunner(dir, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodGet, "http://example.com/x", nil)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	out, _, action := r.OnRequest(req)
	elapsed := time.Since(start)
	if action != proxy.RequestContinue {
		t.Fatalf("action=%v", action)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("timeout took too long: %v", elapsed)
	}
	if elapsed < 40*time.Millisecond {
		t.Fatalf("returned too fast (%v); timeout may not have run", elapsed)
	}
	// Request left usable (no hang). A no-op/failing script must NOT rebuild the
	// request: v1.1.8 only writes the snapshot back when a script actually changed
	// it, so a GET with no body keeps Body=nil (valid net/http), rather than the
	// old always-injected empty NopCloser.
	if out == nil {
		t.Fatal("nil request")
	}
	if out.Body != nil {
		_, _ = io.ReadAll(out.Body)
	}
}

func Test_Runner_onResponse_sets_header(t *testing.T) {
	dir := t.TempDir()
	scripts := filepath.Join(dir, "scripts")
	_ = os.MkdirAll(scripts, 0o755)
	src := `function onResponse(flow) {
  flow.response.headers["X-Out"] = "1";
}`
	_ = os.WriteFile(filepath.Join(scripts, "resp.js"), []byte(src), 0o644)
	r, err := script.NewRunner(dir, 100*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodGet, "http://example.com/", nil)
	resp := &http.Response{
		StatusCode: 200,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader("ok")),
		Request:    req,
	}
	out, action := r.OnResponse(resp)
	if action != proxy.ResponseContinue {
		t.Fatalf("action=%v", action)
	}
	if out.Header.Get("X-Out") != "1" {
		t.Fatalf("header missing: %v", out.Header)
	}
}
