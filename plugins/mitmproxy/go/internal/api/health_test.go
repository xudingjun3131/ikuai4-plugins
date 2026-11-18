package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func Test_HealthHandler_returns_ok_and_version(t *testing.T) {
	// Given
	const ver = "0.0.0-dev"
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rr := httptest.NewRecorder()

	// When
	HealthHandler(ver).ServeHTTP(rr, req)

	// Then
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var body HealthResponse
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !body.OK {
		t.Error("ok = false, want true")
	}
	if body.Version != ver {
		t.Errorf("version = %q, want %q", body.Version, ver)
	}
}

func Test_NewMux_routes_health(t *testing.T) {
	mux := NewMux(Deps{Version: "1.2.3"})
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
}
