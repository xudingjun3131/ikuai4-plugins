package api

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/GitterBois/runhooks/internal/store"
	"github.com/GitterBois/runhooks/internal/webui"
)

func Test_SPA_root_serves_index_html(t *testing.T) {
	// Given: embedded dist has index.html
	if _, err := fs.Stat(webui.DistFS, "dist/index.html"); err != nil {
		t.Fatalf("embed missing dist/index.html: %v", err)
	}

	mux := NewMux(Deps{Version: "t", Store: store.New(1)})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()

	// When
	mux.ServeHTTP(rr, req)

	// Then
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(strings.ToLower(body), "<html") && !strings.Contains(body, "root") {
		// minimal placeholder may be short; must look like HTML
		if !strings.Contains(body, "<!doctype") && !strings.Contains(body, "<!DOCTYPE") && !strings.Contains(body, "<html") {
			t.Fatalf("not html: %q", body)
		}
	}
}

func Test_SPA_unknown_path_falls_back_to_index(t *testing.T) {
	mux := NewMux(Deps{Version: "t", Store: store.New(1)})
	req := httptest.NewRequest(http.MethodGet, "/traffic", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	// SPA fallback serves index, not 404
	if strings.Contains(rr.Body.String(), "404") && rr.Code == http.StatusNotFound {
		t.Fatal("SPA path returned 404")
	}
}
