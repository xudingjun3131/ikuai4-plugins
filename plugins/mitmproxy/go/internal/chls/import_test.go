package chls_test

import (
	"testing"

	"github.com/GitterBois/proxying/internal/chls"
)

func Test_Chls_JSON_import(t *testing.T) {
	raw := []byte(`{
  "entries": [
    {"method":"GET","url":"https://ex.com/a","status":200,"response":{"body":"ok"}}
  ]
}`)
	flows, err := chls.ParseBestEffort(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(flows) != 1 || flows[0].Request.URL != "https://ex.com/a" {
		t.Fatalf("%+v", flows)
	}
}

func Test_Chls_binary_rejected(t *testing.T) {
	_, err := chls.ParseBestEffort([]byte{0x00, 0x01, 0x02})
	if err == nil {
		t.Fatal("want error")
	}
}
