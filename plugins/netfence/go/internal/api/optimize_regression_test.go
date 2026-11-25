package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 本文件覆盖 v1.1.2 优化项: 请求体大小上限、空列表落盘前拒绝,
// 并确认 threat(IP/CIDR)写入未被误伤。

func TestOptimize_OversizedBodyRejected(t *testing.T) {
	s, _ := newTestServer(t)
	big := `{"domain":"` + strings.Repeat("a", 2<<20) + `"}`
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/decide", strings.NewReader(big)))
	if rec.Code != http.StatusBadRequest && rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status=%d want 400/413", rec.Code)
	}
}

func TestOptimize_EmptyListRejectedBeforeWrite(t *testing.T) {
	s, dir := newTestServer(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/lists",
		strings.NewReader(`{"name":"empty.txt","body":"# only comment\n\n"}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d want 400", rec.Code)
	}
	if _, err := os.Stat(filepath.Join(dir, "lists", "empty.txt")); !os.IsNotExist(err) {
		t.Errorf("空列表仍落盘: %v", err)
	}
}

func TestOptimize_ThreatCIDRWriteStillWorks(t *testing.T) {
	s, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/threat",
		strings.NewReader(`{"name":"feed.txt","body":"203.0.113.0/24"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("threat status=%d want 200: %s", rec.Code, rec.Body.String())
	}
}
