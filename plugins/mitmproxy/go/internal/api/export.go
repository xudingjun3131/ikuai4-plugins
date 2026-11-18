package api

import (
	"encoding/json"
	"net/http"

	"github.com/GitterBois/proxying/internal/har"
	"github.com/GitterBois/proxying/internal/store"
)

func exportHARHandler(sink *store.Store, version string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var flows []store.Flow
		if sink != nil {
			flows = sink.List(store.Filter{})
		}
		doc := har.FromFlows(flows, version)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", `attachment; filename="proxying.har"`)
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(doc)
	}
}
