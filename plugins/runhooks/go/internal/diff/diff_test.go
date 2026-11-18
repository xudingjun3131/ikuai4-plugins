package diff_test

import (
	"testing"

	"github.com/GitterBois/runhooks/internal/diff"
	"github.com/GitterBois/runhooks/internal/store"
)

func Test_Compare_hunks(t *testing.T) {
	a := store.Flow{
		ID: "1",
		Request: store.Message{
			Method: "GET",
			URL:    "https://a/x",
			Body:   []byte("aa"),
		},
		Response: store.Message{Status: 200, Body: []byte("ok")},
	}
	b := store.Flow{
		ID: "2",
		Request: store.Message{
			Method: "POST",
			URL:    "https://a/x",
			Body:   []byte("bb"),
		},
		Response: store.Message{Status: 201, Body: []byte("created")},
	}
	r := diff.ChangedOnly(diff.Compare(a, b))
	if len(r.Hunks) < 3 {
		t.Fatalf("hunks=%d %+v", len(r.Hunks), r.Hunks)
	}
	foundMethod := false
	for _, h := range r.Hunks {
		if h.Field == "method" && h.Left == "GET" && h.Right == "POST" {
			foundMethod = true
		}
	}
	if !foundMethod {
		t.Fatalf("missing method hunk: %+v", r.Hunks)
	}
}
