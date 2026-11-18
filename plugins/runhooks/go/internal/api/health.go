package api

import (
	"encoding/json"
	"net/http"
)

// HealthResponse is GET /api/health JSON body (frozen contract).
type HealthResponse struct {
	OK      bool   `json:"ok"`
	Version string `json:"version"`
}

// HealthHandler returns GET /api/health → {"ok":true,"version":"..."}.
func HealthHandler(version string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(HealthResponse{
			OK:      true,
			Version: version,
		})
	}
}
