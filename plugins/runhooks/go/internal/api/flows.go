package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/GitterBois/runhooks/internal/store"
)

func listFlowsHandler(sink *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		filter, err := parseFlowFilter(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if sink == nil {
			writeJSON(w, http.StatusOK, flowsResponse{Flows: []flowDTO{}})
			return
		}
		list := sink.List(filter)
		out := make([]flowDTO, 0, len(list))
		for _, f := range list {
			out = append(out, flowToDTO(f))
		}
		writeJSON(w, http.StatusOK, flowsResponse{Flows: out})
	}
}

func parseFlowFilter(r *http.Request) (store.Filter, error) {
	q := r.URL.Query()
	f := store.Filter{
		Host:   strings.TrimSpace(q.Get("host")),
		Method: strings.TrimSpace(q.Get("method")),
		Q:      strings.TrimSpace(q.Get("q")),
	}
	rawStatus := strings.TrimSpace(q.Get("status"))
	if rawStatus != "" {
		n, err := strconv.Atoi(rawStatus)
		if err != nil || n < 100 || n > 599 {
			return store.Filter{}, fmt.Errorf("invalid status: %q", rawStatus)
		}
		f.Status = n
	}
	switch strings.ToLower(strings.TrimSpace(q.Get("op"))) {
	case "or":
		f.Op = store.CondOR
	default:
		f.Op = store.CondAND
	}
	// cond=field:op:value or field:value (op defaults). Repeatable.
	for _, raw := range q["cond"] {
		c, err := parseCond(raw)
		if err != nil {
			return store.Filter{}, err
		}
		f.Conds = append(f.Conds, c)
	}
	return f, nil
}

func parseCond(raw string) (store.Cond, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return store.Cond{}, fmt.Errorf("empty cond")
	}
	parts := strings.SplitN(raw, ":", 3)
	switch len(parts) {
	case 1:
		return store.Cond{Field: "q", Op: "contains", Value: parts[0]}, nil
	case 2:
		return store.Cond{Field: parts[0], Value: parts[1]}, nil
	default:
		return store.Cond{Field: parts[0], Op: parts[1], Value: parts[2]}, nil
	}
}

func getFlowHandler(sink *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/api/flows/")
		id = strings.Trim(id, "/")
		if id == "" || strings.Contains(id, "/") {
			http.NotFound(w, r)
			return
		}
		if sink == nil {
			http.NotFound(w, r)
			return
		}
		f, ok := sink.Get(store.FlowID(id))
		if !ok {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, http.StatusOK, flowToDTO(f))
	}
}

func clearFlowsHandler(sink *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if sink != nil {
			sink.Clear()
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// flowsCollection dispatches GET list and DELETE clear on /api/flows.
func flowsCollection(sink *store.Store) http.HandlerFunc {
	list := listFlowsHandler(sink)
	clear := clearFlowsHandler(sink)
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			list(w, r)
		case http.MethodDelete:
			clear(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
