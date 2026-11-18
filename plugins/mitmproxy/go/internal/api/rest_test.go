package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/GitterBois/proxying/internal/api"
	"github.com/GitterBois/proxying/internal/breakpoint"
	"github.com/GitterBois/proxying/internal/rest"
	"github.com/GitterBois/proxying/internal/store"
)

func Test_REST_send_and_file_reject(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("pong"))
	}))
	t.Cleanup(origin.Close)

	svc, err := rest.NewService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	mux := api.NewMux(api.Deps{REST: svc, Store: store.New(10)})

	body, _ := json.Marshal(map[string]any{
		"method": "GET",
		"url":    origin.URL + "/p",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/rest/send", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out["body"] != "pong" {
		t.Fatalf("out=%v", out)
	}

	bad, _ := json.Marshal(map[string]string{"url": "file:///etc/passwd"})
	req2 := httptest.NewRequest(http.MethodPost, "/api/rest/send", bytes.NewReader(bad))
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, req2)
	if rec2.Code != 400 {
		t.Fatalf("file:// want 400 got %d", rec2.Code)
	}
}

func Test_REST_from_flow(t *testing.T) {
	sink := store.New(10)
	id, ok := sink.Add(store.Flow{
		Request: store.Message{
			Method: "GET",
			URL:    "http://example.com/from",
			Body:   []byte("x"),
		},
	})
	if !ok {
		t.Fatal("add")
	}
	svc, _ := rest.NewService(t.TempDir())
	mux := api.NewMux(api.Deps{REST: svc, Store: sink})
	req := httptest.NewRequest(http.MethodPost, "/api/rest/from-flow/"+string(id), nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	var in rest.SendRequest
	_ = json.Unmarshal(rec.Body.Bytes(), &in)
	if in.URL != "http://example.com/from" {
		t.Fatalf("%+v", in)
	}
}

func Test_Breakpoints_API_resume(t *testing.T) {
	bp := breakpoint.NewManager(32, time.Second)
	bp.SetEnabled(true)
	mux := api.NewMux(api.Deps{Breakpoints: bp})

	req, _ := http.NewRequest(http.MethodGet, "http://example.com/bp", nil)
	done := make(chan struct{})
	go func() {
		_, _, _ = bp.OnRequest(req)
		close(done)
	}()
	var id string
	for i := 0; i < 100; i++ {
		list := bp.List()
		if len(list) == 1 {
			id = list[0].ID
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if id == "" {
		t.Fatal("no hold")
	}
	recList := httptest.NewRecorder()
	mux.ServeHTTP(recList, httptest.NewRequest(http.MethodGet, "/api/breakpoints", nil))
	if recList.Code != 200 {
		t.Fatalf("list %d", recList.Code)
	}
	rec := httptest.NewRecorder()
	body, _ := json.Marshal(map[string]any{"headers": map[string]string{"X-R": "1"}})
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/breakpoints/"+id+"/resume", bytes.NewReader(body)))
	if rec.Code != 200 {
		t.Fatalf("resume %d %s", rec.Code, rec.Body.String())
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("hung")
	}
}
