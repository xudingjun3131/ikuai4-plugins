package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ikuai4/netfence/internal/core"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	eng, err := core.NewEngine(core.Config{Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	return New(eng, "1.0.0", t.TempDir())
}

func Test_API_health(t *testing.T) {
	s := newTestServer(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if rec.Code != 200 {
		t.Fatalf("status=%d", rec.Code)
	}
	var m map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &m)
	if m["version"] != "1.0.0" || m["ok"] != true {
		t.Errorf("health=%v", m)
	}
}

func Test_API_decide_reflectsPolicy(t *testing.T) {
	s := newTestServer(t)
	// install a block rule via the policy endpoint
	body := `[{"id":"r1","name":"block video","enabled":true,"priority":1,` +
		`"match":{"categories":["video"]},"action":"block"}]`
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/policy", strings.NewReader(body)))
	if rec.Code != 200 {
		t.Fatalf("put policy status=%d body=%s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	probe := `{"client_ip":"192.168.1.5","domain":"x.douyin.com","app_hint":""}`
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/decide", strings.NewReader(probe)))
	var d core.Decision
	_ = json.Unmarshal(rec.Body.Bytes(), &d)
	if d.Action != core.ActionBlock || d.Layer != "policy" || d.App != "抖音" {
		t.Errorf("decide=%+v", d)
	}
}

func Test_API_acl_isRenderOnly(t *testing.T) {
	s := newTestServer(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/acl", nil))
	var m map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &m)
	if m["applied"] != false {
		t.Errorf("acl must never auto-apply: %v", m)
	}
	if !strings.Contains(m["ruleset"].(string), "table inet netfence") {
		t.Errorf("bad ruleset: %v", m["ruleset"])
	}
}

func Test_API_audit_recordsDecisions(t *testing.T) {
	s := newTestServer(t)
	probe := `{"client_ip":"10.0.0.7","domain":"example.org"}`
	s.Handler().ServeHTTP(httptest.NewRecorder(),
		httptest.NewRequest(http.MethodPost, "/api/decide", strings.NewReader(probe)))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/audit?client=10.0.0.7", nil))
	var ds []core.Decision
	_ = json.Unmarshal(rec.Body.Bytes(), &ds)
	if len(ds) != 1 || ds[0].ClientIP != "10.0.0.7" {
		t.Errorf("audit=%+v", ds)
	}
}
