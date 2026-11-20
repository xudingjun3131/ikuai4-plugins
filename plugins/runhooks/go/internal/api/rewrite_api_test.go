package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/GitterBois/runhooks/internal/api"
	"github.com/GitterBois/runhooks/internal/breakpoint"
	"github.com/GitterBois/runhooks/internal/script"
	"github.com/GitterBois/runhooks/internal/store"
)

func TestBreakpoints_CapacityLimitsAndAbort(t *testing.T) {
	bp := breakpoint.NewManager(2, 5*time.Second)
	mux := api.NewMux(api.Deps{Breakpoints: bp, Store: store.New(4)})

	// GET exposes capacity (v1.1.8: hold pool was previously invisible).
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/breakpoints", nil))
	if rec.Code != 200 {
		t.Fatalf("GET status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Enabled  bool `json:"enabled"`
		Capacity struct {
			Max       int   `json:"max"`
			TimeoutMs int64 `json:"timeoutMs"`
			Holds     int   `json:"holds"`
			Aborted   int64 `json:"aborted"`
		} `json:"capacity"`
		Sessions []struct {
			ID string `json:"id"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Capacity.Max != 2 || body.Capacity.TimeoutMs != 5000 {
		t.Fatalf("capacity missing/wrong: %+v", body.Capacity)
	}

	// PUT tunes limits at runtime.
	put, _ := json.Marshal(map[string]any{"enabled": true, "max": 6, "timeoutMs": 1500})
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/breakpoints", bytes.NewReader(put)))
	if rec.Code != 200 {
		t.Fatalf("PUT status=%d body=%s", rec.Code, rec.Body.String())
	}
	if st := bp.Stats(); st.Max != 6 || st.TimeoutMs != 1500 {
		t.Fatalf("limits not applied: %+v", st)
	}

	// Hold a request, then abort it through the API.
	bp.SetEnabled(true)
	done := make(chan struct{})
	go func() {
		req, _ := http.NewRequest(http.MethodGet, "http://example.com/p", nil)
		bp.OnRequest(req)
		close(done)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && len(bp.List()) == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	if len(bp.List()) == 0 {
		t.Fatal("no hold registered")
	}
	id := bp.List()[0].ID
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/breakpoints/"+id+"/abort", nil))
	if rec.Code != 200 {
		t.Fatalf("abort status=%d body=%s", rec.Code, rec.Body.String())
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("abort did not release promptly")
	}
	if bp.Stats().Aborted != 1 {
		t.Fatalf("aborted counter wrong: %+v", bp.Stats())
	}

	// Unknown action / unknown id are honest 404s.
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/breakpoints/deadbeef/abort", nil))
	if rec.Code != 404 {
		t.Fatalf("unknown abort id status=%d", rec.Code)
	}
	bp.Stop()
}

func TestDiagnostics_SurfacesAndClearsScriptErrors(t *testing.T) {
	dir := t.TempDir()
	scripts := filepath.Join(dir, "scripts")
	if err := os.MkdirAll(scripts, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scripts, "loop.js"), []byte("function onRequest(flow){ while(true){} }"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner, err := script.NewRunner(dir, 30*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodGet, "http://example.com/x", nil)
	runner.OnRequest(req) // timeout -> previously silent

	mux := api.NewMux(api.Deps{ScriptRunner: runner, Store: store.New(4)})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/diagnostics", nil))
	if rec.Code != 200 {
		t.Fatalf("GET diagnostics status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out struct {
		ScriptTimeoutMs int64 `json:"scriptTimeoutMs"`
		ScriptErrors    []struct {
			Script string `json:"script"`
			Phase  string `json:"phase"`
		} `json:"scriptErrors"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.ScriptTimeoutMs != 30 || len(out.ScriptErrors) != 1 || out.ScriptErrors[0].Script != "loop.js" {
		t.Fatalf("script error not surfaced: %+v", out)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/diagnostics", nil))
	if rec.Code != 200 {
		t.Fatalf("DELETE diagnostics status=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/diagnostics", nil))
	var cleared struct {
		ScriptErrors []any `json:"scriptErrors"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &cleared)
	if len(cleared.ScriptErrors) != 0 {
		t.Fatalf("DELETE did not clear: %+v", cleared)
	}
}
