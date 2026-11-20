package script_test

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/GitterBois/runhooks/internal/script"
)

func writeScript(t *testing.T, dir, name, src string) {
	t.Helper()
	scripts := filepath.Join(dir, "scripts")
	if err := os.MkdirAll(scripts, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scripts, name), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
}

// v1.1.7 缺口: timeout 静默跳过, 改包不生效也不报错。现在必须能被
// GET /api/diagnostics 取到(这里直接验 Runner.Errors 的源)。
func TestRunner_SurfacesTimeout(t *testing.T) {
	dir := t.TempDir()
	writeScript(t, dir, "loop.js", "function onRequest(flow){ while(true){} }")
	r, err := script.NewRunner(dir, 40*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodGet, "http://example.com/x", nil)
	r.OnRequest(req)
	errs := r.Errors()
	if len(errs) != 1 {
		t.Fatalf("want 1 surfaced error, got %d: %+v", len(errs), errs)
	}
	if errs[0].Script != "loop.js" || errs[0].Phase != "onRequest" {
		t.Fatalf("wrong script/phase: %+v", errs[0])
	}
	if !strings.Contains(strings.ToLower(errs[0].Message), "timeout") {
		t.Fatalf("message should name timeout: %q", errs[0].Message)
	}
	if errs[0].At.IsZero() || errs[0].Count < 1 {
		t.Fatalf("missing timestamp/count: %+v", errs[0])
	}
}

func TestRunner_SurfacesHookException(t *testing.T) {
	dir := t.TempDir()
	writeScript(t, dir, "throw.js", "function onResponse(flow){ throw new Error('boom'); }")
	r, err := script.NewRunner(dir, 100*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	resp := &http.Response{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": []string{"text/plain"}},
		Body:       io.NopCloser(strings.NewReader("ORIGINAL")),
		Request:    mustReq(t),
	}
	r.OnResponse(resp)
	errs := r.Errors()
	if len(errs) != 1 || errs[0].Phase != "onResponse" || !strings.Contains(errs[0].Message, "boom") {
		t.Fatalf("hook exception not surfaced: %+v", errs)
	}
}

func TestRunner_SurfacesLoadError(t *testing.T) {
	dir := t.TempDir()
	writeScript(t, dir, "bad.js", "function onRequest(flow) { this is not js {")
	r, err := script.NewRunner(dir, 100*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodGet, "http://example.com/x", nil)
	r.OnRequest(req)
	errs := r.Errors()
	if len(errs) != 1 || !strings.Contains(errs[0].Message, "load") {
		t.Fatalf("load error not surfaced: %+v", errs)
	}
}

func TestRunner_NoErrorsOnHealthyScript(t *testing.T) {
	dir := t.TempDir()
	writeScript(t, dir, "ok.js", "function onResponse(flow){ flow.response.body = 'PATCHED'; }")
	r, err := script.NewRunner(dir, 100*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	resp := &http.Response{
		StatusCode: 200,
		Header: http.Header{
			"Content-Type":     []string{"text/plain"},
			"Content-Encoding": []string{"gzip"}, // 明文改写后必须被清掉
			"Etag":             []string{"old"},
		},
		Body:    io.NopCloser(strings.NewReader("ORIGINAL")),
		Request: mustReq(t),
	}
	r.OnResponse(resp)
	if len(r.Errors()) != 0 {
		t.Fatalf("healthy script produced errors: %+v", r.Errors())
	}
	b, _ := io.ReadAll(resp.Body)
	if string(b) != "PATCHED" {
		t.Fatalf("body = %q", b)
	}
	if resp.Header.Get("Content-Encoding") != "" {
		t.Fatal("stale Content-Encoding survived a plain-body script rewrite")
	}
	if resp.Header.Get("Content-Length") != "7" || resp.Header.Get("Etag") != "" {
		t.Fatalf("headers not normalised: CL=%q ETag=%q", resp.Header.Get("Content-Length"), resp.Header.Get("Etag"))
	}
}

func TestRunner_NoOpScriptDoesNotStripContentEncoding(t *testing.T) {
	dir := t.TempDir()
	// 脚本存在但既不改 body 也不改 headers: 不得重建响应头(否则 gzip 响应
	// 的 Content-Encoding 会被清掉, 客户端拿到明文却按压缩解析)。
	writeScript(t, dir, "noop.js", "function onResponse(flow){ /* no-op */ }")
	r, err := script.NewRunner(dir, 100*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	resp := &http.Response{
		StatusCode: 200,
		Header: http.Header{
			"Content-Encoding": []string{"gzip"},
			"Content-Length":   []string{"9"},
		},
		Body:    io.NopCloser(strings.NewReader("GZIPBYTES")),
		Request: mustReq(t),
	}
	r.OnResponse(resp)
	if resp.Header.Get("Content-Encoding") != "gzip" {
		t.Fatalf("no-op script stripped Content-Encoding: %v", resp.Header)
	}
	if len(r.Errors()) != 0 {
		t.Fatalf("no-op script recorded errors: %+v", r.Errors())
	}
}

func TestRunner_DedupesAndResets(t *testing.T) {
	dir := t.TempDir()
	writeScript(t, dir, "loop.js", "function onRequest(flow){ while(true){} }")
	r, err := script.NewRunner(dir, 20*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		req, _ := http.NewRequest(http.MethodGet, "http://example.com/x", nil)
		r.OnRequest(req)
	}
	errs := r.Errors()
	if len(errs) != 1 || errs[0].Count != 3 {
		t.Fatalf("want one error with count=3, got %+v", errs)
	}
	r.ResetErrors()
	if len(r.Errors()) != 0 {
		t.Fatal("ResetErrors did not clear")
	}
	if r.Timeout() != 20*time.Millisecond {
		t.Fatalf("Timeout() = %v", r.Timeout())
	}
}

func mustReq(t *testing.T) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, "http://example.com/x", nil)
	if err != nil {
		t.Fatal(err)
	}
	return req
}
