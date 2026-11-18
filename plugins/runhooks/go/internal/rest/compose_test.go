package rest_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/GitterBois/runhooks/internal/rest"
)

func Test_Repeat_count(t *testing.T) {
	var hits int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(204)
	}))
	defer ts.Close()

	svc, err := rest.NewService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	res, err := svc.Repeat(context.Background(), rest.SendRequest{Method: "GET", URL: ts.URL}, 3)
	if err != nil {
		t.Fatal(err)
	}
	if res.Count != 3 || hits != 3 {
		t.Fatalf("count=%d hits=%d", res.Count, hits)
	}
}
