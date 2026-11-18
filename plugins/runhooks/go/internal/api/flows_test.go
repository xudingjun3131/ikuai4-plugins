package api

import (
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/GitterBois/runhooks/internal/store"
)

func Test_ListFlows_returns_flows_wrapper_json(t *testing.T) {
	// Given: store with one flow
	sink := store.New(10)
	id, ok := sink.Add(store.Flow{
		State:     store.StateDone,
		StartedAt: time.Unix(1_700_000_000, 0).UTC(),
		Scheme:    "https",
		Request: store.Message{
			Method: "GET",
			URL:    "https://example.com/a",
			Proto:  "HTTP/1.1",
			Headers: map[string][]string{
				"Accept": {"*/*"},
			},
			Body: []byte("req"),
			Size: 3,
		},
		Response: store.Message{
			Status: 200,
			Proto:  "HTTP/1.1",
			Body:   []byte("ok"),
			Size:   2,
		},
	})
	if !ok {
		t.Fatal("Add failed")
	}

	mux := NewMux(Deps{Version: "t", Store: sink})
	req := httptest.NewRequest(http.MethodGet, "/api/flows", nil)
	rr := httptest.NewRecorder()

	// When
	mux.ServeHTTP(rr, req)

	// Then
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var body flowsResponse
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Flows) != 1 {
		t.Fatalf("len(flows) = %d, want 1", len(body.Flows))
	}
	f := body.Flows[0]
	if f.ID != string(id) {
		t.Errorf("id = %q, want %q", f.ID, id)
	}
	if f.State != "done" {
		t.Errorf("state = %q, want done", f.State)
	}
	if f.Request.Method != "GET" || f.Request.URL != "https://example.com/a" {
		t.Errorf("request = %+v", f.Request)
	}
	if f.Response.Status != 200 {
		t.Errorf("status = %d", f.Response.Status)
	}
	// body is base64 of raw bytes
	raw, err := base64.StdEncoding.DecodeString(f.Request.Body)
	if err != nil || string(raw) != "req" {
		t.Errorf("request.body base64 = %q decoded=%q err=%v", f.Request.Body, raw, err)
	}
}

func Test_ListFlows_empty_store_returns_empty_array(t *testing.T) {
	mux := NewMux(Deps{Version: "t", Store: store.New(10)})
	req := httptest.NewRequest(http.MethodGet, "/api/flows", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	var body flowsResponse
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Flows == nil {
		t.Fatal("flows is null, want []")
	}
	if len(body.Flows) != 0 {
		t.Fatalf("len = %d", len(body.Flows))
	}
}

func Test_ListFlows_filter_host_method_status_q(t *testing.T) {
	// Given
	sink := store.New(10)
	_, _ = sink.Add(store.Flow{
		Request:  store.Message{Method: "GET", URL: "https://api.example.com/v1"},
		Response: store.Message{Status: 200},
	})
	_, _ = sink.Add(store.Flow{
		Request:  store.Message{Method: "POST", URL: "https://api.example.com/v1"},
		Response: store.Message{Status: 201},
	})
	_, _ = sink.Add(store.Flow{
		Request:  store.Message{Method: "GET", URL: "https://other.test/x"},
		Response: store.Message{Status: 404},
		Error:    "not found",
	})
	mux := NewMux(Deps{Version: "t", Store: sink})

	cases := []struct {
		name string
		url  string
		want int
	}{
		{"method GET", "/api/flows?method=GET", 2},
		{"status 201", "/api/flows?status=201", 1},
		{"host api", "/api/flows?host=api.example.com", 2},
		{"q error", "/api/flows?q=not%20found", 1},
		{"combined", "/api/flows?method=GET&host=api.example.com", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// When
			req := httptest.NewRequest(http.MethodGet, tc.url, nil)
			rr := httptest.NewRecorder()
			mux.ServeHTTP(rr, req)

			// Then
			if rr.Code != http.StatusOK {
				t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
			}
			var body flowsResponse
			if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if len(body.Flows) != tc.want {
				t.Fatalf("len = %d, want %d", len(body.Flows), tc.want)
			}
		})
	}
}

func Test_ListFlows_multi_cond_and_highlight(t *testing.T) {
	sink := store.New(10)
	_, _ = sink.Add(store.Flow{
		Request:        store.Message{Method: "GET", URL: "https://api.example.com/v1"},
		Response:       store.Message{Status: 200},
		HighlightColor: "yellow",
		HighlightLabel: "hot",
	})
	_, _ = sink.Add(store.Flow{
		Request:  store.Message{Method: "POST", URL: "https://api.example.com/v1"},
		Response: store.Message{Status: 201},
	})
	_, _ = sink.Add(store.Flow{
		Request:  store.Message{Method: "GET", URL: "https://other.test/x"},
		Response: store.Message{Status: 200},
	})
	mux := NewMux(Deps{Version: "t", Store: sink})
	url := "/api/flows?op=and&cond=host:contains:api.example.com&cond=method:eq:GET&cond=status:eq:200"
	req := httptest.NewRequest(http.MethodGet, url, nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var body flowsResponse
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Flows) != 1 {
		t.Fatalf("len=%d want 1", len(body.Flows))
	}
	if body.Flows[0].HighlightColor != "yellow" || body.Flows[0].HighlightLabel != "hot" {
		t.Fatalf("highlight=%+v", body.Flows[0])
	}
}

func Test_ListFlows_invalid_status_returns_400(t *testing.T) {
	mux := NewMux(Deps{Version: "t", Store: store.New(5)})
	for _, raw := range []string{"abc", "99", "600", "-1"} {
		t.Run(raw, func(t *testing.T) {
			// Given / When
			req := httptest.NewRequest(http.MethodGet, "/api/flows?status="+raw, nil)
			rr := httptest.NewRecorder()
			mux.ServeHTTP(rr, req)

			// Then
			if rr.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 for %q", rr.Code, raw)
			}
		})
	}
}

func Test_GetFlow_returns_full_flow_or_404(t *testing.T) {
	sink := store.New(10)
	id, _ := sink.Add(store.Flow{
		State: store.StateDone,
		Request: store.Message{
			Method: "POST",
			URL:    "http://x/",
			Body:   []byte("hi"),
		},
	})

	mux := NewMux(Deps{Version: "t", Store: sink})

	t.Run("found", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/flows/"+string(id), nil)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
		}
		var f flowDTO
		if err := json.NewDecoder(rr.Body).Decode(&f); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if f.ID != string(id) {
			t.Errorf("id = %q", f.ID)
		}
	})

	t.Run("missing", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/flows/nope", nil)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rr.Code)
		}
	})
}

func Test_DeleteFlows_clears_store(t *testing.T) {
	sink := store.New(10)
	_, _ = sink.Add(store.Flow{Request: store.Message{Method: "GET", URL: "http://a/"}})
	mux := NewMux(Deps{Version: "t", Store: sink})

	req := httptest.NewRequest(http.MethodDelete, "/api/flows", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rr.Code)
	}
	if n := len(sink.List(store.Filter{})); n != 0 {
		t.Fatalf("store still has %d flows", n)
	}
}

func Test_CAPem_returns_pem_bytes(t *testing.T) {
	pem := []byte("-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n")
	mux := NewMux(Deps{
		Version: "t",
		Store:   store.New(1),
		RootPEM: func() []byte { return pem },
	})
	req := httptest.NewRequest(http.MethodGet, "/api/ca.pem", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "BEGIN CERTIFICATE") {
		t.Fatalf("body missing BEGIN CERTIFICATE: %q", rr.Body.String())
	}
	ct := rr.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/x-pem-file") && !strings.Contains(ct, "application/x-x509") && !strings.Contains(ct, "text/plain") {
		// accept common PEM content types
		if ct == "" {
			t.Error("Content-Type empty")
		}
	}
}

func Test_BodyDisplay_gunzips_when_content_encoding_gzip(t *testing.T) {
	// Given: gzip body "hello"
	var buf strings.Builder
	zw := gzip.NewWriter(&buf)
	if _, err := io.WriteString(zw, "hello"); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	gz := []byte(buf.String())

	// When
	got := bodyDisplay(map[string][]string{
		"Content-Encoding": {"gzip"},
	}, gz)

	// Then
	if got != "hello" {
		t.Fatalf("bodyDisplay = %q, want hello", got)
	}
}

func Test_BodyDisplay_passthrough_plain(t *testing.T) {
	got := bodyDisplay(nil, []byte("plain"))
	if got != "plain" {
		t.Fatalf("got %q", got)
	}
}

func Test_GetFlow_includes_bodyDisplay(t *testing.T) {
	var buf strings.Builder
	zw := gzip.NewWriter(&buf)
	_, _ = io.WriteString(zw, "view-me")
	_ = zw.Close()

	sink := store.New(10)
	id, _ := sink.Add(store.Flow{
		State: store.StateDone,
		Response: store.Message{
			Status: 200,
			Headers: map[string][]string{
				"Content-Encoding": {"gzip"},
			},
			Body: []byte(buf.String()),
			Size: int64(buf.Len()),
		},
	})
	mux := NewMux(Deps{Version: "t", Store: sink})
	req := httptest.NewRequest(http.MethodGet, "/api/flows/"+string(id), nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	var f flowDTO
	if err := json.NewDecoder(rr.Body).Decode(&f); err != nil {
		t.Fatal(err)
	}
	if f.Response.BodyDisplay != "view-me" {
		t.Fatalf("bodyDisplay = %q, want view-me", f.Response.BodyDisplay)
	}
}

func Test_NewMux_health_still_works(t *testing.T) {
	mux := NewMux(Deps{Version: "1.2.3", Store: store.New(1)})
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
}
