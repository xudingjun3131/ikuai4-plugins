package api

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/GitterBois/proxying/internal/bookmark"
	"github.com/GitterBois/proxying/internal/chls"
	"github.com/GitterBois/proxying/internal/diff"
	"github.com/GitterBois/proxying/internal/har"
	"github.com/GitterBois/proxying/internal/netcond"
	"github.com/GitterBois/proxying/internal/proxyterm"
	"github.com/GitterBois/proxying/internal/rest"
	"github.com/GitterBois/proxying/internal/script"
	"github.com/GitterBois/proxying/internal/session"
	"github.com/GitterBois/proxying/internal/store"
)

func registerPhaseB(mux *http.ServeMux, d Deps) {
	if d.Scripts != nil {
		mux.HandleFunc("/api/scripts/templates", scriptTemplatesHandler(d.Scripts, d.OnScriptChange))
		mux.HandleFunc("/api/scripts", scriptListHandler(d.Scripts))
	}
	if d.Sessions != nil {
		mux.HandleFunc("/api/sessions", sessionsHandler(d.Sessions))
		mux.HandleFunc("/api/sessions/", sessionsItemHandler(d.Sessions))
	}
	if d.Bookmarks != nil {
		mux.HandleFunc("/api/bookmarks", bookmarksHandler(d.Bookmarks))
		mux.HandleFunc("/api/bookmarks/", bookmarksItemHandler(d.Bookmarks))
	}
	if d.Store != nil {
		mux.HandleFunc("/api/diff", diffHandler(d.Store))
		mux.HandleFunc("/api/import/har", importHARHandler(d.Store))
		mux.HandleFunc("/api/import/chls", importChlsHandler(d.Store))
	}
	if d.Report != nil {
		mux.HandleFunc("/api/report", d.Report.IngestHandler())
		mux.HandleFunc("/api/agents", d.Report.AgentsHandler())
	}
	if d.NetCond != nil {
		mux.HandleFunc("/api/netcond", netcondHandler(d.NetCond))
	}
	if d.MCP != nil {
		mux.HandleFunc("/api/mcp/", d.MCP.LegacyHandler())
		mux.Handle("/mcp", d.MCP.HTTPHandler())
		mux.Handle("/mcp/", d.MCP.HTTPHandler())
	}
	if d.REST != nil {
		mux.HandleFunc("/api/rest/compose", restComposeHandler(d.REST))
		mux.HandleFunc("/api/rest/repeat", restRepeatHandler(d.REST))
	}
	mux.HandleFunc("/api/proxy-env", proxyEnvHandler(d.ProxyAddr))
	mux.HandleFunc("/api/ui/prefs", uiPrefsHandler(d.DataDir))
}

func scriptTemplatesHandler(g *script.Gallery, onApply func()) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, http.StatusOK, map[string]any{"templates": g.List()})
		case http.MethodPost:
			var body struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, "invalid json", http.StatusBadRequest)
				return
			}
			name, err := g.Apply(body.ID, body.Name)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if onApply != nil {
				onApply()
			}
			writeJSON(w, http.StatusCreated, map[string]any{"file": name})
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

func scriptListHandler(g *script.Gallery) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"scripts": listJS(g.ScriptsDir())})
	}
}

func listJS(dir string) []string {
	entries, err := osReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, name := range entries {
		if strings.HasSuffix(strings.ToLower(name), ".js") {
			out = append(out, name)
		}
	}
	return out
}

func sessionsHandler(s *session.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, http.StatusOK, map[string]any{"sessions": s.List(), "active": s.ActiveID()})
		case http.MethodPost:
			var body struct {
				Name string `json:"name"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			sess, err := s.Create(body.Name)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			writeJSON(w, http.StatusCreated, sess)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

func sessionsItemHandler(s *session.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/api/sessions/")
		id = strings.Trim(id, "/")
		if id == "" || strings.Contains(id, "/") {
			http.NotFound(w, r)
			return
		}
		switch r.Method {
		case http.MethodPost:
			// activate
			if strings.HasSuffix(r.URL.Path, "/activate") || r.URL.Query().Get("action") == "activate" {
				// path may be id/activate
			}
			parts := strings.Split(id, "/")
			sid := parts[0]
			if len(parts) == 2 && parts[1] == "activate" {
				if err := s.Activate(sid); err != nil {
					http.Error(w, err.Error(), http.StatusNotFound)
					return
				}
				writeJSON(w, http.StatusOK, map[string]any{"active": sid})
				return
			}
			if err := s.Activate(sid); err != nil {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"active": sid})
		case http.MethodDelete:
			if err := s.Delete(id); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

func bookmarksHandler(s *bookmark.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, http.StatusOK, map[string]any{"bookmarks": s.List()})
		case http.MethodPost:
			var n bookmark.Node
			if err := json.NewDecoder(r.Body).Decode(&n); err != nil {
				http.Error(w, "invalid json", http.StatusBadRequest)
				return
			}
			out, err := s.Add(n)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			writeJSON(w, http.StatusCreated, out)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

func bookmarksItemHandler(s *bookmark.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/api/bookmarks/")
		id = strings.Trim(id, "/")
		if id == "" || strings.Contains(id, "/") {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodDelete {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := s.Delete(id); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func diffHandler(sink *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var leftID, rightID string
		if r.Method == http.MethodGet {
			leftID = r.URL.Query().Get("left")
			rightID = r.URL.Query().Get("right")
		} else {
			var body struct {
				Left  string `json:"left"`
				Right string `json:"right"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, "invalid json", http.StatusBadRequest)
				return
			}
			leftID, rightID = body.Left, body.Right
		}
		a, okA := sink.Get(store.FlowID(leftID))
		b, okB := sink.Get(store.FlowID(rightID))
		if !okA || !okB {
			http.Error(w, "flow not found", http.StatusNotFound)
			return
		}
		res := diff.ChangedOnly(diff.Compare(a, b))
		writeJSON(w, http.StatusOK, res)
	}
}

func importHARHandler(sink *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		b, err := io.ReadAll(io.LimitReader(r.Body, 32<<20))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		doc, err := har.ParseJSON(b)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		flows, err := har.ToFlows(doc)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		n := 0
		for _, f := range flows {
			if _, ok := sink.Add(f); ok {
				n++
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"imported": n})
	}
}

func importChlsHandler(sink *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		b, err := io.ReadAll(io.LimitReader(r.Body, 32<<20))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		flows, err := chls.ParseBestEffort(b)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		n := 0
		for _, f := range flows {
			if _, ok := sink.Add(f); ok {
				n++
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"imported": n})
	}
}

func netcondHandler(c *netcond.Cond) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, http.StatusOK, c.Get())
		case http.MethodPut:
			var cfg netcond.Config
			if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
				http.Error(w, "invalid json", http.StatusBadRequest)
				return
			}
			c.Set(cfg)
			writeJSON(w, http.StatusOK, c.Get())
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

func restComposeHandler(svc *rest.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var in rest.ComposeRequest
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		out, err := svc.Compose(r.Context(), in)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, out)
	}
}

func restRepeatHandler(svc *rest.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var body struct {
			rest.SendRequest
			Count int `json:"count"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		out, err := svc.Repeat(r.Context(), body.SendRequest, body.Count)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, out)
	}
}

func proxyEnvHandler(proxyAddr string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		env := proxyterm.EnvFor(proxyAddr, nil)
		writeJSON(w, http.StatusOK, map[string]any{
			"env":  env,
			"hint": proxyterm.SystemProxyHint(),
		})
	}
}

// uiPrefsHandler stores explorer column prefs as JSON under dataDir.
func uiPrefsHandler(dataDir string) http.HandlerFunc {
	// lazy file path
	return func(w http.ResponseWriter, r *http.Request) {
		if dataDir == "" {
			writeJSON(w, http.StatusOK, map[string]any{"prefs": map[string]any{}})
			return
		}
		path := dataDir + "/ui-prefs.json"
		switch r.Method {
		case http.MethodGet:
			b, err := readFileOptional(path)
			if err != nil || len(b) == 0 {
				writeJSON(w, http.StatusOK, map[string]any{"prefs": map[string]any{
					"columns": []string{"method", "status", "host", "path"},
					"tab":     "traffic",
				}})
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(b)
		case http.MethodPut:
			b, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if err := writeFileAtomic(path, b); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(b)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

func osReadDir(dir string) ([]string, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(ents))
	for _, e := range ents {
		if !e.IsDir() {
			out = append(out, e.Name())
		}
	}
	return out, nil
}
