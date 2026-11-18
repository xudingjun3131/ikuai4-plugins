package api

import (
	"encoding/json"
	"net/http"

	"github.com/GitterBois/runhooks/internal/store"
)

type capturePausedBody struct {
	Paused bool `json:"paused"`
}

func captureHandler(sink *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			paused := false
			if sink != nil {
				paused = sink.CapturePaused()
			}
			writeJSON(w, http.StatusOK, capturePausedBody{Paused: paused})
		case http.MethodPut:
			var body capturePausedBody
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, "invalid json", http.StatusBadRequest)
				return
			}
			if sink != nil {
				sink.SetCapturePaused(body.Paused)
			}
			writeJSON(w, http.StatusOK, capturePausedBody{Paused: body.Paused})
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}
