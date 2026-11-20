package api

import (
	"net/http"

	"github.com/GitterBois/runhooks/internal/script"
)

// diagnosticsHandler surfaces script failures that v1.1.7 silently swallowed.
// GET  -> {"scriptTimeoutMs":N,"scriptErrors":[...]}
// DELETE -> clears the surfaced errors (acknowledge them from the console).
func diagnosticsHandler(runner *script.Runner) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if runner == nil {
			http.Error(w, "diagnostics unavailable", http.StatusServiceUnavailable)
			return
		}
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, http.StatusOK, map[string]any{
				"scriptTimeoutMs": runner.Timeout().Milliseconds(),
				"scriptErrors":    runner.Errors(),
			})
		case http.MethodDelete:
			runner.ResetErrors()
			writeJSON(w, http.StatusOK, map[string]any{"cleared": true})
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}
