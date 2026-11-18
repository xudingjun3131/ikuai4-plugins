package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/GitterBois/runhooks/internal/rest"
	"github.com/GitterBois/runhooks/internal/store"
)

func restSendHandler(svc *rest.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if svc == nil {
			http.Error(w, "rest unavailable", http.StatusServiceUnavailable)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var in rest.SendRequest
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		out, err := svc.Send(r.Context(), in)
		if err != nil {
			// scheme / validation
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, out)
	}
}

func restFromFlowHandler(svc *rest.Service, sink *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if svc == nil || sink == nil {
			http.Error(w, "rest unavailable", http.StatusServiceUnavailable)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/api/rest/from-flow/")
		id = strings.Trim(id, "/")
		if id == "" || strings.Contains(id, "/") {
			http.NotFound(w, r)
			return
		}
		f, ok := sink.Get(store.FlowID(id))
		if !ok {
			http.Error(w, `{"error":"not_found"}`, http.StatusNotFound)
			return
		}
		in := rest.FromFlow(f)
		// optional ?send=1 to execute immediately
		if r.URL.Query().Get("send") == "1" {
			out, err := svc.Send(r.Context(), in)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"request": in, "response": out})
			return
		}
		writeJSON(w, http.StatusOK, in)
	}
}

func restEnvHandler(svc *rest.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if svc == nil {
			http.Error(w, "rest unavailable", http.StatusServiceUnavailable)
			return
		}
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, http.StatusOK, map[string]any{"env": svc.Env()})
		case http.MethodPut:
			var body struct {
				Env map[string]string `json:"env"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, "invalid json", http.StatusBadRequest)
				return
			}
			if err := svc.SetEnv(body.Env); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"env": svc.Env()})
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

func restCollectionsHandler(svc *rest.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if svc == nil {
			http.Error(w, "rest unavailable", http.StatusServiceUnavailable)
			return
		}
		path := strings.TrimPrefix(r.URL.Path, "/api/rest/collections")
		path = strings.Trim(path, "/")
		if path == "" {
			switch r.Method {
			case http.MethodGet:
				list, err := svc.ListCollections()
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				if list == nil {
					list = []rest.Collection{}
				}
				writeJSON(w, http.StatusOK, map[string]any{"collections": list})
			case http.MethodPost:
				var c rest.Collection
				if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
					http.Error(w, "invalid json", http.StatusBadRequest)
					return
				}
				if c.ID == "" {
					c.ID = newID()
				}
				if err := svc.SaveCollection(c); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				writeJSON(w, http.StatusCreated, c)
			default:
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			}
			return
		}
		// /api/rest/collections/{id}
		if strings.Contains(path, "/") {
			http.NotFound(w, r)
			return
		}
		id := path
		switch r.Method {
		case http.MethodGet:
			c, err := svc.GetCollection(id)
			if err != nil {
				http.Error(w, `{"error":"not_found"}`, http.StatusNotFound)
				return
			}
			writeJSON(w, http.StatusOK, c)
		case http.MethodPut:
			var c rest.Collection
			if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
				http.Error(w, "invalid json", http.StatusBadRequest)
				return
			}
			c.ID = id
			if err := svc.SaveCollection(c); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			writeJSON(w, http.StatusOK, c)
		case http.MethodDelete:
			if err := svc.DeleteCollection(id); err != nil {
				http.Error(w, `{"error":"not_found"}`, http.StatusNotFound)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}
