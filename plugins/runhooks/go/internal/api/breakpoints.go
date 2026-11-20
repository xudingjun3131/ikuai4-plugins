package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/GitterBois/runhooks/internal/breakpoint"
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
			// [v1.1.8] 容量可观测: 旧版只有 enabled/match/sessions, hold 占满
			// 连接时无任何指标(测试报告 §3.6)。
			writeJSON(w, http.StatusOK, map[string]any{
				"enabled":  enabled,
				"match":    match,
				"sessions": bp.List(),
				"capacity": bp.Stats(),
			})
		case http.MethodPut:
			var body struct {
				Enabled   *bool             `json:"enabled"`
				Match     *breakpoint.Match `json:"match"`
				Max       *int              `json:"max"`
				TimeoutMs *int64            `json:"timeoutMs"`
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
			if body.Max != nil || body.TimeoutMs != nil {
				max, timeoutMs := 0, time.Duration(0)
				if body.Max != nil {
					max = *body.Max
				}
				if body.TimeoutMs != nil {
					timeoutMs = time.Duration(*body.TimeoutMs) * time.Millisecond
				}
				bp.SetLimits(max, timeoutMs)
			}
			enabled, match := bp.Config()
			writeJSON(w, http.StatusOK, map[string]any{
				"enabled": enabled, "match": match, "capacity": bp.Stats(),
			})
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
		// /api/breakpoints/{id}/resume | /api/breakpoints/{id}/abort
		path := strings.TrimPrefix(r.URL.Path, "/api/breakpoints/")
		parts := strings.Split(strings.Trim(path, "/"), "/")
		if len(parts) != 2 {
			http.NotFound(w, r)
			return
		}
		id := parts[0]
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		switch parts[1] {
		case "abort":
			if err := bp.Abort(id); err != nil {
				notFoundOrConflict(w, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"status": "aborted", "id": id})
		case "resume":
			var edit breakpoint.ResumeEdit
			if r.Body != nil && r.ContentLength != 0 {
				if err := json.NewDecoder(r.Body).Decode(&edit); err != nil {
					http.Error(w, "invalid json", http.StatusBadRequest)
					return
				}
			}
			if err := bp.Resume(id, edit); err != nil {
				notFoundOrConflict(w, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"status": "resumed", "id": id})
		default:
			http.NotFound(w, r)
		}
	}
}

func notFoundOrConflict(w http.ResponseWriter, err error) {
	if err.Error() == "not found" {
		http.Error(w, `{"error":"not_found"}`, http.StatusNotFound)
		return
	}
	http.Error(w, err.Error(), http.StatusConflict)
}
