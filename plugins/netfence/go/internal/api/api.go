// Package api exposes the netfence engine over a localhost-only HTTP API.
// The plugin page (html/index.html) is served by the router Web service and
// talks to this API through the firmware /plugin_proxy/<name>/ same-origin
// proxy, so no CORS and no credentials cross the iframe boundary.
//
// [write-plane] v1.1.0: 只读端点对未实现 method 返回 405(不再 200 假成功);
// 补齐 lists/threat 写入端点、审计清理、ACL 受控下发与策略版本冲突检测。
package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ikuai4/netfence/internal/core"
)

type Server struct {
	eng     *core.Engine
	version string
	cfgDir  string
	mux     *http.ServeMux

	mu        sync.Mutex
	policyRev int64 // 每次 Replace/Save/Reload 递增; UI 带 X-Policy-Rev 提交, 落后则 409
	applied   bool  // ACL 是否已下发到内核
}

func New(eng *core.Engine, version, cfgDir string) *Server {
	s := &Server{eng: eng, version: version, cfgDir: cfgDir, mux: http.NewServeMux()}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) routes() {
	s.mux.HandleFunc("/api/health", s.health)
	s.mux.HandleFunc("/api/status", s.status)
	s.mux.HandleFunc("/api/decide", s.decide)
	s.mux.HandleFunc("/api/policy", s.policy)
	s.mux.HandleFunc("/api/audit", s.audit)
	s.mux.HandleFunc("/api/summary", s.summary)
	s.mux.HandleFunc("/api/apps", s.apps)
	s.mux.HandleFunc("/api/lists", s.lists)
	s.mux.HandleFunc("/api/threat", s.threat)
	s.mux.HandleFunc("/api/acl", s.acl)
	s.mux.HandleFunc("/api/acl/apply", s.aclApply)
	s.mux.HandleFunc("/api/reload", s.reload)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

func methodNotAllowed(w http.ResponseWriter, allowed string) {
	w.Header().Set("Allow", allowed)
	http.Error(w, "method not allowed, allowed: "+allowed, http.StatusMethodNotAllowed)
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, "GET")
		return
	}
	writeJSON(w, map[string]any{
		"ok": true, "version": s.version, "uptime_s": int(s.eng.Uptime().Seconds()),
		"time": time.Now().UTC().Format(time.RFC3339), "schedule_tz": "Asia/Shanghai(+08:00)",
	})
}

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, "GET")
		return
	}
	s.mu.Lock()
	applied, rev := s.applied, s.policyRev
	s.mu.Unlock()
	writeJSON(w, map[string]any{
		"version":        s.version,
		"uptime_s":       int(s.eng.Uptime().Seconds()),
		"lists":          s.eng.Filter.Stats(),
		"threat":         s.eng.Threat.Stats(),
		"policy_rules":   len(s.eng.Policy.List()),
		"app_signatures": len(s.eng.AppID.List()),
		"audit":          s.eng.Audit.Summary(time.Now().Add(-24 * time.Hour)),
		"acl_applied":    applied,
		"policy_rev":     rev,
		"schedule_tz":    "Asia/Shanghai(+08:00)",
	})
}

// decide 是"策略预演": 强制 DryRun=true, 绝不写行为审计(D6)。
// 真实流量裁决由 DNS 数据面在引擎内部调用 Decide, 不走这个端点。
func (s *Server) decide(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST")
		return
	}
	var req core.Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	req.DryRun = true // 服务端强制, 客户端无法伪造真实流量记录
	writeJSON(w, s.eng.Decide(req))
}

// policy: GET 返回裸数组(兼容既有页面/测试) + X-Policy-Rev 响应头;
// PUT 接受裸数组, 若带 X-Policy-Rev 则做乐观并发检查, 落后返回 409(D11)。
func (s *Server) policy(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.mu.Lock()
		rev := s.policyRev
		s.mu.Unlock()
		w.Header().Set("X-Policy-Rev", strconv.FormatInt(rev, 10))
		writeJSON(w, s.eng.Policy.List())
	case http.MethodPut, http.MethodPost:
		var rs []core.Rule
		if err := json.NewDecoder(r.Body).Decode(&rs); err != nil {
			http.Error(w, "bad policy: "+err.Error(), http.StatusBadRequest)
			return
		}
		if rs == nil {
			http.Error(w, "rules 不能为空(至少保留一条默认规则)", http.StatusBadRequest)
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if h := r.Header.Get("X-Policy-Rev"); h != "" {
			clientRev, err := strconv.ParseInt(h, 10, 64)
			if err != nil || clientRev != s.policyRev {
				http.Error(w, "策略版本冲突: 请重新载入后再保存", http.StatusConflict)
				return
			}
		}
		s.eng.Policy.Replace(rs...)
		if err := s.eng.Policy.SaveFile(filepath.Join(s.cfgDir, "policy.json")); err != nil {
			http.Error(w, "save failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		s.policyRev++
		w.Header().Set("X-Policy-Rev", strconv.FormatInt(s.policyRev, 10))
		writeJSON(w, map[string]any{"ok": true, "rules": len(rs), "rev": s.policyRev})
	default:
		methodNotAllowed(w, "GET, PUT")
	}
}

func (s *Server) audit(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		q := r.URL.Query()
		limit := 100
		if n, err := strconv.Atoi(q.Get("limit")); err == nil && n > 0 {
			limit = n
		}
		writeJSON(w, s.eng.Audit.Query(core.AuditQuery{
			ClientIP: q.Get("client"), App: q.Get("app"),
			Category: core.Category(q.Get("category")), Action: core.Action(q.Get("action")),
			Source: q.Get("source"), Limit: limit,
		}))
	case http.MethodDelete:
		s.eng.Audit.Clear()
		writeJSON(w, map[string]any{"ok": true})
	default:
		methodNotAllowed(w, "GET, DELETE")
	}
}

func (s *Server) summary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, "GET")
		return
	}
	writeJSON(w, s.eng.Audit.Summary(time.Now().Add(-24*time.Hour)))
}

func (s *Server) apps(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, "GET")
		return
	}
	writeJSON(w, s.eng.AppID.List())
}

// lists: GET 只读; POST 写入 named .txt; DELETE 删除。写入后引擎 Reload 生效。
func (s *Server) lists(w http.ResponseWriter, r *http.Request) {
	dir := filepath.Join(s.cfgDir, "lists")
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, map[string]any{
			"stats": s.eng.Filter.Stats(), "domains": s.eng.Filter.SortedDomains(),
			"files": listTxtFiles(dir),
		})
	case http.MethodPost:
		var body struct {
			Name string `json:"name"`
			Body string `json:"body"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		n, err := core.WriteListFile(dir, body.Name, body.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := s.eng.Reload(s.cfgDir); err != nil {
			http.Error(w, "reload failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{"ok": true, "name": body.Name, "entries": n,
			"domains": len(s.eng.Filter.SortedDomains())})
	case http.MethodDelete:
		name := r.URL.Query().Get("name")
		if err := core.RemoveListFile(dir, name); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := s.eng.Reload(s.cfgDir); err != nil {
			http.Error(w, "reload failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{"ok": true, "deleted": name})
	default:
		methodNotAllowed(w, "GET, POST, DELETE")
	}
}

// threat: GET 只读; POST 写入 named .txt; DELETE 删除。
func (s *Server) threat(w http.ResponseWriter, r *http.Request) {
	dir := filepath.Join(s.cfgDir, "threat")
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, map[string]any{"stats": s.eng.Threat.Stats(),
			"ips": s.eng.Threat.IPs(), "files": listTxtFiles(dir)})
	case http.MethodPost:
		var body struct {
			Name string `json:"name"`
			Body string `json:"body"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		n, err := core.WriteListFile(dir, body.Name, body.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := s.eng.Reload(s.cfgDir); err != nil {
			http.Error(w, "reload failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{"ok": true, "name": body.Name, "entries": n,
			"ips": len(s.eng.Threat.IPs())})
	case http.MethodDelete:
		name := r.URL.Query().Get("name")
		if err := core.RemoveListFile(dir, name); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := s.eng.Reload(s.cfgDir); err != nil {
			http.Error(w, "reload failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{"ok": true, "deleted": name})
	default:
		methodNotAllowed(w, "GET, POST, DELETE")
	}
}

func listTxtFiles(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []string{}
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".txt") {
			out = append(out, e.Name())
		}
	}
	return out
}

// acl: GET 只渲染(applied:false), 不下发。
func (s *Server) acl(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, "GET")
		return
	}
	ruleset, err := s.eng.RenderACL()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if r.URL.Query().Get("format") == "text" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(ruleset))
		return
	}
	s.mu.Lock()
	applied := s.applied
	s.mu.Unlock()
	writeJSON(w, map[string]any{"ruleset": ruleset, "applied": applied,
		"hint": "仅预览; 确认无误后用 POST /api/acl/apply 下发, DELETE 撤销"})
}

// aclApply: POST 带 confirm=true 才真正下发; DELETE 撤销(整表删除)。
func (s *Server) aclApply(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var body struct {
			Confirm bool `json:"confirm"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		if !body.Confirm {
			http.Error(w, "需要二次确认: 提交 confirm=true 才会真正下发内核规则", http.StatusBadRequest)
			return
		}
		ruleset, err := s.eng.RenderACL()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		out, err := core.ApplyRuleset(ruleset)
		if err != nil {
			writeJSON(w, map[string]any{"ok": false, "applied": false, "error": err.Error(), "output": out})
			return
		}
		s.mu.Lock()
		s.applied = true
		s.mu.Unlock()
		writeJSON(w, map[string]any{"ok": true, "applied": true, "output": out})
	case http.MethodDelete:
		cfg := core.DefaultACLConfig()
		out, err := core.FlushRuleset(cfg.Table)
		if err != nil {
			writeJSON(w, map[string]any{"ok": false, "error": err.Error(), "output": out})
			return
		}
		s.mu.Lock()
		s.applied = false
		s.mu.Unlock()
		writeJSON(w, map[string]any{"ok": true, "applied": false, "output": out})
	default:
		methodNotAllowed(w, "POST, DELETE")
	}
}

func (s *Server) reload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST")
		return
	}
	if err := s.eng.Reload(s.cfgDir); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.mu.Lock()
	s.policyRev++
	s.mu.Unlock()
	writeJSON(w, map[string]any{"ok": true})
}
