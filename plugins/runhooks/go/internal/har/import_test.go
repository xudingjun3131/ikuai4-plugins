package har_test

import (
	"encoding/json"
	"testing"

	"github.com/GitterBois/runhooks/internal/har"
	"github.com/GitterBois/runhooks/internal/store"
)

func Test_HAR_roundtrip_import(t *testing.T) {
	flows := []store.Flow{{
		State: store.StateDone,
		Request: store.Message{
			Method: "GET",
			URL:    "https://example.com/a",
			Proto:  "HTTP/1.1",
			Body:   []byte("req"),
		},
		Response: store.Message{
			Status: 200,
			Proto:  "HTTP/1.1",
			Body:   []byte("ok"),
			Size:   2,
		},
	}}
	doc := har.FromFlows(flows, "test")
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := har.ParseJSON(b)
	if err != nil {
		t.Fatal(err)
	}
	got, err := har.ToFlows(parsed)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("len=%d", len(got))
	}
	if got[0].Request.Method != "GET" || got[0].Request.URL != "https://example.com/a" {
		t.Fatalf("%+v", got[0].Request)
	}
	if got[0].Response.Status != 200 {
		t.Fatalf("status=%d", got[0].Response.Status)
	}
}
