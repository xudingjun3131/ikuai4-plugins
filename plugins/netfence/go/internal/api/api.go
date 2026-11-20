// Package api exposes the netfence engine over a localhost-only HTTP API.
// The plugin page (html/index.html) is served by the router Web service and
// talks to this API through the firmware /plugin_proxy/<name>/ same-origin
// proxy, so no CORS and no credentials cross the iframe boundary.
package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/ikuai4/netfence/internal/core"
)

type Server struct {
	eng     *core.Engine
	version string
	cfgDir  string
	mux     *http.ServeMux
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
	s.mux.HandleFunc("/api/reload", s.reload)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{
		"ok": true, "version": s.version, "uptime_s": int(s.eng.Uptime().Seconds()),
		"time": time.Now().Format(time.RFC3339),
	})
}

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{
		"version":        s.version,
		"uptime_s":       int(s.eng.Uptime().Seconds()),
		"lists":          s.eng.Filter.Stats(),
		"threat":         s.eng.Threat.Stats(),
		"policy_rules":   len(s.eng.Policy.List()),
		"app_signatures": len(s.eng.AppID.List()),
		"audit":          s.eng.Audit.Summary(time.Now().Add(-24 * time.Hour)),
	})
}

// decide is the "would this be blocked" probe used by the UI policy debugger.
func (s *Server) decide(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	var req core.Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, s.eng.Decide(req))
}

func (s *Server) policy(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, s.eng.Policy.List())
	case http.MethodPut, http.MethodPost:
		var rs []core.Rule
		if err := json.NewDecoder(r.Body).Decode(&rs); err != nil {
			http.Error(w, "bad policy: "+err.Error(), http.StatusBadRequest)
			return
		}
		s.eng.Policy.Replace(rs...)
		if err := s.eng.Policy.SaveFile(s.cfgDir + "/policy.json"); err != nil {
			http.Error(w, "save failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{"ok": true, "rules": len(rs)})
	default:
		http.Error(w, "GET/PUT only", http.StatusMethodNotAllowed)
	}
}

func (s *Server) audit(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := 100
	if n, err := strconv.Atoi(q.Get("limit")); err == nil && n > 0 {
		limit = n
	}
	writeJSON(w, s.eng.Audit.Query(core.AuditQuery{
		ClientIP: q.Get("client"), App: q.Get("app"),
		Category: core.Category(q.Get("category")), Action: core.Action(q.Get("action")),
		Limit: limit,
	}))
}

func (s *Server) summary(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.eng.Audit.Summary(time.Now().Add(-24*time.Hour)))
}

func (s *Server) apps(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.eng.AppID.List())
}

func (s *Server) lists(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{
		"stats": s.eng.Filter.Stats(), "domains": s.eng.Filter.SortedDomains(),
	})
}

func (s *Server) threat(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"stats": s.eng.Threat.Stats(), "ips": s.eng.Threat.IPs()})
}

// acl returns the RENDERED nftables ruleset for review. It never applies it.
func (s *Server) acl(w http.ResponseWriter, r *http.Request) {
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
	writeJSON(w, map[string]any{"ruleset": ruleset, "applied": false})
}

func (s *Server) reload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	if err := s.eng.Reload(s.cfgDir); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}
