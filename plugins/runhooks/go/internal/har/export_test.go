package har

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/GitterBois/runhooks/internal/store"
)

func Test_FromFlows_HAR12_shape(t *testing.T) {
	// Given
	flows := []store.Flow{{
		ID:        "1",
		State:     store.StateDone,
		StartedAt: time.Unix(1_700_000_000, 0).UTC(),
		DoneAt:    time.Unix(1_700_000_000, 50_000_000).UTC(),
		Request: store.Message{
			Method: "GET",
			URL:    "https://example.com/a?x=1",
			Proto:  "HTTP/1.1",
			Headers: map[string][]string{
				"Accept": {"*/*"},
			},
		},
		Response: store.Message{
			Status: 200,
			Proto:  "HTTP/1.1",
			Headers: map[string][]string{
				"Content-Type": {"text/plain"},
			},
			Body: []byte("ok"),
			Size: 2,
		},
	}}

	// When
	doc := FromFlows(flows, "1.2.3")
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}

	// Then
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	log, ok := got["log"].(map[string]any)
	if !ok {
		t.Fatalf("missing log: %s", raw)
	}
	if log["version"] != "1.2" {
		t.Fatalf("version = %v", log["version"])
	}
	creator, _ := log["creator"].(map[string]any)
	if creator["name"] != "proxying" || creator["version"] != "1.2.3" {
		t.Fatalf("creator = %v", creator)
	}
	entries, _ := log["entries"].([]any)
	if len(entries) != 1 {
		t.Fatalf("entries len = %d", len(entries))
	}
	entry, _ := entries[0].(map[string]any)
	req, _ := entry["request"].(map[string]any)
	if req["method"] != "GET" || req["url"] != "https://example.com/a?x=1" {
		t.Fatalf("request = %v", req)
	}
	resp, _ := entry["response"].(map[string]any)
	if resp["status"] != float64(200) {
		t.Fatalf("status = %v", resp["status"])
	}
	content, _ := resp["content"].(map[string]any)
	if content["encoding"] != "base64" || content["text"] == "" {
		t.Fatalf("content = %v", content)
	}
}

func Test_FromFlows_empty(t *testing.T) {
	doc := FromFlows(nil, "")
	if doc.Log.Version != "1.2" {
		t.Fatalf("version = %q", doc.Log.Version)
	}
	if doc.Log.Creator.Version != "0.0.0-dev" {
		t.Fatalf("creator version = %q", doc.Log.Creator.Version)
	}
	if doc.Log.Entries == nil || len(doc.Log.Entries) != 0 {
		t.Fatalf("entries = %v", doc.Log.Entries)
	}
}
