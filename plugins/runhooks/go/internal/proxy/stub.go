package proxy

import (
	"net/http"
)

// StubHandler returns 404 until main wires Engine (todo 7).
func StubHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not implemented", http.StatusNotFound)
	})
}

// NewStubServer listens on addr with StubHandler.
func NewStubServer(addr string) *http.Server {
	return &http.Server{
		Addr:    addr,
		Handler: StubHandler(),
	}
}
