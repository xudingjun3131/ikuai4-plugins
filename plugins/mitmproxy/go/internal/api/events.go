package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/GitterBois/proxying/internal/store"
)

const sseHeartbeat = 15 * time.Second

func eventsHandler(sink *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		if sink == nil {
			http.Error(w, "store unavailable", http.StatusServiceUnavailable)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()

		ch := make(chan store.Event, 16)
		unsub := sink.Subscribe(func(ev store.Event) {
			select {
			case ch <- ev:
			default:
				// drop if client is slow; keep store publish non-blocking
			}
		})
		defer unsub()

		tick := time.NewTicker(sseHeartbeat)
		defer tick.Stop()

		ctx := r.Context()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
					return
				}
				flusher.Flush()
			case ev := <-ch:
				if err := writeSSE(w, flusher, ev); err != nil {
					return
				}
			}
		}
	}
}

func writeSSE(w http.ResponseWriter, flusher http.Flusher, ev store.Event) error {
	payload, err := ssePayload(ev)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, payload); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}

func ssePayload(ev store.Event) ([]byte, error) {
	switch ev.Type {
	case store.EventFlowAdded, store.EventFlowUpdated:
		if ev.Flow == nil {
			return []byte("{}"), nil
		}
		return json.Marshal(flowToDTO(*ev.Flow))
	case store.EventCleared:
		return []byte("{}"), nil
	case store.EventCapturePaused:
		paused := false
		if ev.Paused != nil {
			paused = *ev.Paused
		}
		return json.Marshal(map[string]bool{"paused": paused})
	default:
		return []byte("{}"), nil
	}
}
