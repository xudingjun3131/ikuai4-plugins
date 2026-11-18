package rest_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/GitterBois/runhooks/internal/rest"
	"github.com/GitterBois/runhooks/internal/store"
)

func Test_Send_httptest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Test") != "1" {
			http.Error(w, "missing header", 400)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(srv.Close)

	svc, err := rest.NewService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := svc.Send(ctx, rest.SendRequest{
		Method:  http.MethodPost,
		URL:     srv.URL + "/x",
		Headers: map[string]string{"X-Test": "1"},
		Body:    "hi",
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != http.StatusCreated {
		t.Fatalf("status=%d err=%s", out.Status, out.Error)
	}
	if out.Body != "ok" {
		t.Fatalf("body=%q", out.Body)
	}
	if out.TimingMs < 0 {
		t.Fatalf("timing=%d", out.TimingMs)
	}
}

func Test_Send_file_scheme_rejected(t *testing.T) {
	svc, err := rest.NewService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Send(context.Background(), rest.SendRequest{
		URL: "file:///etc/passwd",
	})
	if err == nil {
		t.Fatal("want error for file://")
	}
}

func Test_Send_env_subst(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/hi" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte("yes"))
	}))
	t.Cleanup(srv.Close)

	svc, err := rest.NewService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.SetEnv(map[string]string{"base": srv.URL, "path": "/v1/hi"}); err != nil {
		t.Fatal(err)
	}
	out, err := svc.Send(context.Background(), rest.SendRequest{
		URL: "{{base}}{{path}}",
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != 200 || out.Body != "yes" {
		t.Fatalf("%+v", out)
	}
}

func Test_FromFlow(t *testing.T) {
	f := store.Flow{
		Request: store.Message{
			Method:  "PUT",
			URL:     "http://example.com/item",
			Headers: map[string][]string{"Content-Type": {"application/json"}},
			Body:    []byte(`{"a":1}`),
		},
	}
	in := rest.FromFlow(f)
	if in.Method != "PUT" || in.URL != "http://example.com/item" || in.Body != `{"a":1}` {
		t.Fatalf("%+v", in)
	}
	if in.Headers["Content-Type"] != "application/json" {
		t.Fatalf("headers=%v", in.Headers)
	}
}

func Test_Collections_CRUD(t *testing.T) {
	dir := t.TempDir()
	svc, err := rest.NewService(dir)
	if err != nil {
		t.Fatal(err)
	}
	c := rest.Collection{
		ID:   "demo",
		Name: "Demo",
		Requests: []rest.CollectionReq{{
			ID:     "r1",
			Name:   "get",
			Method: "GET",
			URL:    "http://example.com",
		}},
	}
	if err := svc.SaveCollection(c); err != nil {
		t.Fatal(err)
	}
	list, err := svc.ListCollections()
	if err != nil || len(list) != 1 {
		t.Fatalf("list=%v err=%v", list, err)
	}
	got, err := svc.GetCollection("demo")
	if err != nil || got.Name != "Demo" {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "collections", "demo.json")); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteCollection("demo"); err != nil {
		t.Fatal(err)
	}
}
