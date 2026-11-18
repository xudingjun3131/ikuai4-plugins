package api

import (
	"io/fs"
	"net/http"
	"strings"

	"github.com/GitterBois/runhooks/internal/webui"
)

// spaHandler serves embedded web/dist with SPA fallback to index.html.
func spaHandler() http.Handler {
	sub, err := fs.Sub(webui.DistFS, "dist")
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "ui embed missing", http.StatusInternalServerError)
		})
	}
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}
		// API paths never reach here if registered first on ServeMux.
		if _, err := fs.Stat(sub, path); err != nil {
			// SPA fallback
			r = r.Clone(r.Context())
			r.URL.Path = "/"
			fileServer.ServeHTTP(w, r)
			return
		}
		fileServer.ServeHTTP(w, r)
	})
}
