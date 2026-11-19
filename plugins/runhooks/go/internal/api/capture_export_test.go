package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/GitterBois/runhooks/internal/store"
)

func Test_Capture_GET_PUT_paused(t *testing.T) {
	// Given
	sink := store.New(5)
	mux := NewMux(Deps{Version: "t", Store: sink})

	// When GET default
	req := httptest.NewRequest(http.MethodGet, "/api/capture", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	// Then
	if rr.Code != http.StatusOK {
		t.Fatalf("GET status = %d", rr.Code)
	}
	var body capturePausedBody
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Paused {
		t.Fatal("default paused want false")
	}

	// When PUT pause
	req = httptest.NewRequest(http.MethodPut, "/api/capture", strings.NewReader(`{"paused":true}`))
	req.Header.Set("Content-Type", "application/json")
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT status = %d body=%s", rr.Code, rr.Body.String())
	}
	if !sink.CapturePaused() {
		t.Fatal("store not paused after PUT")
	}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if !body.Paused {
		t.Fatal("PUT response paused want true")
	}

	// When PUT resume
	req = httptest.NewRequest(http.MethodPut, "/api/capture", strings.NewReader(`{"paused":false}`))
	req.Header.Set("Content-Type", "application/json")
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if sink.CapturePaused() {
		t.Fatal("store still paused after resume")
	}
}

func Test_Capture_PUT_invalid_json_400(t *testing.T) {
	mux := NewMux(Deps{Version: "t", Store: store.New(1)})
	req := httptest.NewRequest(http.MethodPut, "/api/capture", strings.NewReader(`not-json`))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
}

func Test_ExportHAR_returns_HAR12(t *testing.T) {
	// Given
	sink := store.New(10)
	_, _ = sink.Add(store.Flow{
		State:     store.StateDone,
		StartedAt: time.Unix(1_700_000_000, 0).UTC(),
		Request: store.Message{
			Method: "GET",
			URL:    "https://example.com/",
			Proto:  "HTTP/1.1",
		},
		Response: store.Message{
			Status: 200,
			Body:   []byte("hi"),
			Size:   2,
		},
	})
	mux := NewMux(Deps{Version: "9.9.9", Store: sink})

	// When
	req := httptest.NewRequest(http.MethodGet, "/api/export/har", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	// Then
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	ct := rr.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Fatalf("Content-Type = %q", ct)
	}
	var doc map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&doc); err != nil {
		t.Fatal(err)
	}
	log, ok := doc["log"].(map[string]any)
	if !ok {
		t.Fatalf("no log: %v", doc)
	}
	if log["version"] != "1.2" {
		t.Fatalf("version = %v", log["version"])
	}
	creator, _ := log["creator"].(map[string]any)
	if creator["name"] != "netloom" || creator["version"] != "9.9.9" {
		t.Fatalf("creator = %v", creator)
	}
	entries, _ := log["entries"].([]any)
	if len(entries) != 1 {
		t.Fatalf("entries = %d", len(entries))
	}
}

func Test_ExportHAR_empty_store(t *testing.T) {
	mux := NewMux(Deps{Version: "t", Store: store.New(1)})
	req := httptest.NewRequest(http.MethodGet, "/api/export/har", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	var doc map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&doc); err != nil {
		t.Fatal(err)
	}
	log := doc["log"].(map[string]any)
	entries := log["entries"].([]any)
	if len(entries) != 0 {
		t.Fatalf("want empty entries, got %d", len(entries))
	}
}
