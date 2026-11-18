package mcp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/GitterBois/runhooks/internal/store"
)

// legacyHTTP is a simple non-protocol helper for the web UI.
type legacyHTTP struct {
	sink *store.Store
}

func (s *legacyHTTP) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/mcp")
		path = strings.Trim(path, "/")
		switch {
		case path == "tools" && r.Method == http.MethodGet:
			writeJSON(w, http.StatusOK, map[string]any{
				"tools": []map[string]string{
					{"name": "list_flows", "description": "List captured flows"},
					{"name": "get_flow", "description": "Get one flow by id"},
					{"name": "clear_flows", "description": "Clear capture buffer"},
					{"name": "set_capture_paused", "description": "Pause/resume capture"},
					{"name": "rest_send", "description": "Send HTTP(S) request"},
					{"name": "list_rules", "description": "List MITM rules"},
					{"name": "add_host_block_rule", "description": "Block a host"},
					{"name": "rest_from_flow", "description": "REST from capture"},
				},
				"mcp": map[string]string{
					"stdio": "proxying -mcp-stdio",
					"http":  "/mcp (Streamable HTTP, official MCP)",
				},
			})
		case path == "call" && r.Method == http.MethodPost:
			s.call(w, r)
		default:
			http.NotFound(w, r)
		}
	}
}

func (s *legacyHTTP) call(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	switch req.Name {
	case "list_flows":
		var args struct {
			Host, Method, Q string
			Status          int
		}
		_ = json.Unmarshal(req.Arguments, &args)
		if s.sink == nil {
			writeJSON(w, http.StatusOK, map[string]any{"flows": []any{}})
			return
		}
		list := s.sink.List(store.Filter{Host: args.Host, Method: args.Method, Status: args.Status, Q: args.Q})
		type brief struct {
			ID, Method, URL string
			Status          int
		}
		out := make([]brief, 0, len(list))
		for _, f := range list {
			out = append(out, brief{ID: string(f.ID), Method: f.Request.Method, URL: f.Request.URL, Status: f.Response.Status})
		}
		writeJSON(w, http.StatusOK, map[string]any{"flows": out})
	case "get_flow":
		var args struct{ ID string }
		_ = json.Unmarshal(req.Arguments, &args)
		if s.sink == nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		f, ok := s.sink.Get(store.FlowID(args.ID))
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK, f)
	case "clear_flows":
		if s.sink != nil {
			s.sink.Clear()
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		http.Error(w, fmt.Sprintf("unknown tool %q (use full MCP at /mcp for all tools)", req.Name), http.StatusBadRequest)
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
