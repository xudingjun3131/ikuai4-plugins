package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/GitterBois/proxying/internal/breakpoint"
)

func breakpointsCollection(bp *breakpoint.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if bp == nil {
			http.Error(w, "breakpoints unavailable", http.StatusServiceUnavailable)
			return
		}
		switch r.Method {
		case http.MethodGet:
			enabled, match := bp.Config()
			writeJSON(w, http.StatusOK, map[string]any{
				"enabled":  enabled,
				"match":    match,
				"sessions": bp.List(),
			})
		case http.MethodPut:
			var body struct {
				Enabled *bool             `json:"enabled"`
				Match   *breakpoint.Match `json:"match"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, "invalid json", http.StatusBadRequest)
				return
			}
			if body.Enabled != nil {
				bp.SetEnabled(*body.Enabled)
			}
			if body.Match != nil {
				bp.SetMatch(*body.Match)
			}
			enabled, match := bp.Config()
			writeJSON(w, http.StatusOK, map[string]any{"enabled": enabled, "match": match})
		case http.MethodDelete:
			bp.Stop()
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

func breakpointsItem(bp *breakpoint.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if bp == nil {
			http.Error(w, "breakpoints unavailable", http.StatusServiceUnavailable)
			return
		}
		// /api/breakpoints/{id}/resume
		path := strings.TrimPrefix(r.URL.Path, "/api/breakpoints/")
		parts := strings.Split(strings.Trim(path, "/"), "/")
		if len(parts) != 2 || parts[1] != "resume" {
			http.NotFound(w, r)
			return
		}
		id := parts[0]
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var edit breakpoint.ResumeEdit
		if r.Body != nil && r.ContentLength != 0 {
			if err := json.NewDecoder(r.Body).Decode(&edit); err != nil {
				http.Error(w, "invalid json", http.StatusBadRequest)
				return
			}
		}
		if err := bp.Resume(id, edit); err != nil {
			if err.Error() == "not found" {
				http.Error(w, `{"error":"not_found"}`, http.StatusNotFound)
				return
			}
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "resumed", "id": id})
	}
}
