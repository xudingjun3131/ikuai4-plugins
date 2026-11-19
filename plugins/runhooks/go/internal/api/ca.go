package api

import "net/http"

func caPEMHandler(rootPEM func() []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if rootPEM == nil {
			http.Error(w, "ca unavailable", http.StatusServiceUnavailable)
			return
		}
		pem := rootPEM()
		if len(pem) == 0 {
			http.Error(w, "ca unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/x-pem-file")
		w.Header().Set("Content-Disposition", `attachment; filename="netloom-ca.pem"`)
		_, _ = w.Write(pem)
	}
}
